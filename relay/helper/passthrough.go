package helper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/lxp/htokenmeter/relay/dto"
)

const maxScanLine = 8 << 20 // 8 MiB per SSE line (base64 frames in streams)

func newSSEScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxScanLine)
	sc.Split(bufio.ScanLines)
	return sc
}

func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

// forwardBody streams an upstream non-SSE body verbatim.
func forwardBody(w http.ResponseWriter, resp *http.Response, body []byte) {
	for k, vs := range resp.Header {
		for i, v := range vs {
			if i == 0 {
				w.Header().Set(k, v)
			} else {
				w.Header().Add(k, v)
			}
		}
	}
	w.Header().Del("Content-Length")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// RelayPassthrough handles the same-format case: upstream format == client
// format. Streams are scanned line-by-line, forwarded, and usage captured.
func RelayPassthrough(w http.ResponseWriter, resp *http.Response, rc *RelayContext) (*dto.Usage, *RelayError) {
	writeNoStoreHeaders(w)
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxScanLine))
		resp.Body.Close()
		return nil, upstreamErrorFromBody(rc.ClientFormat, resp.StatusCode, body)
	}
	if !rc.Stream {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if err != nil {
			return nil, Errorf(http.StatusBadGateway, "read upstream body: %v", err)
		}
		var usage *dto.Usage
		switch rc.UpstreamFormat {
		case FormatOpenAI:
			usage = extractUsageFromOpenAIBody(body)
		case FormatClaude:
			usage = extractUsageFromClaudeBody(body)
		case FormatGemini:
			usage = extractUsageFromGeminiBody(body)
		case FormatResponses:
			usage = ExtractUsageFromResponsesBody(body)
		}
		if usage == nil {
			usage = estimateUsageFromBody(rc.ClientFormat, body)
		}
		forwardBody(w, resp, body)
		return usage, nil
	}

	setSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	sc := newSSEScanner(resp.Body)
	var collected []*dto.Usage
	var buf bytes.Buffer
	for sc.Scan() {
		line := sc.Bytes()
		if u := scanLineUsage(rc.UpstreamFormat, line); u != nil {
			collected = append(collected, u)
		}
		buf.Reset()
		buf.Write(line)
		buf.WriteByte('\n')
		if _, err := w.Write(buf.Bytes()); err != nil {
			break
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	resp.Body.Close()
	// Merge per-field: Claude splits usage across message_start (input
	// side) and message_delta (output side); last-write-wins lost the
	// input tokens. mergeStreamUsage returns a zero Usage when nothing
	// was captured, matching the previous behavior.
	return mergeStreamUsage(collected), nil
}

// scanLineUsage inspects one SSE line for usage-bearing events.
func scanLineUsage(f Format, line []byte) *dto.Usage {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return nil
	}
	data := bytes.TrimSpace(line[5:])
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return nil
	}
	switch f {
	case FormatOpenAI:
		var probe struct {
			Usage *openaiUsage `json:"usage"`
		}
		if json.Unmarshal(data, &probe) == nil && probe.Usage != nil {
			return probe.Usage.toUsage()
		}
	case FormatClaude:
		// message_start carries input tokens; message_delta carries output.
		var probe struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
			Message *struct {
				Usage *struct {
					InputTokens              int64 `json:"input_tokens"`
					CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
					CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(data, &probe) != nil {
			return nil
		}
		switch probe.Type {
		case "message_start":
			if probe.Message != nil && probe.Message.Usage != nil {
				return &dto.Usage{
					PromptTokens:     probe.Message.Usage.InputTokens,
					CacheWriteTokens: probe.Message.Usage.CacheCreationInputTokens,
					CacheReadTokens:  probe.Message.Usage.CacheReadInputTokens,
				}
			}
		case "message_delta":
			if probe.Usage != nil {
				return &dto.Usage{CompletionTokens: probe.Usage.OutputTokens}
			}
		}
	case FormatGemini:
		var probe struct {
			UsageMetadata *dto.GeminiUsage `json:"usageMetadata"`
		}
		if json.Unmarshal(data, &probe) == nil && probe.UsageMetadata != nil {
			return geminiUsageToUsage(probe.UsageMetadata)
		}
	case FormatResponses:
		if u := ExtractUsageFromResponsesBody(data); u != nil {
			return u
		}
	}
	return nil
}

// mergeStreamUsage combines partial usages captured across events
// (Claude message_start + message_delta arrive separately).
func mergeStreamUsage(usages []*dto.Usage) *dto.Usage {
	out := &dto.Usage{}
	for _, u := range usages {
		if u.PromptTokens > 0 {
			out.PromptTokens = u.PromptTokens
		}
		if u.CompletionTokens > 0 {
			out.CompletionTokens = u.CompletionTokens
		}
		if u.CacheReadTokens > 0 {
			out.CacheReadTokens = u.CacheReadTokens
		}
		if u.CacheWriteTokens > 0 {
			out.CacheWriteTokens = u.CacheWriteTokens
		}
		if u.Images > 0 {
			out.Images = u.Images
		}
		if u.PromptIncludesCache {
			out.PromptIncludesCache = true
		}
	}
	return out
}

func geminiUsageToUsage(g *dto.GeminiUsage) *dto.Usage {
	return &dto.Usage{
		PromptTokens:     g.PromptTokenCount,
		CompletionTokens: g.CandidatesTokenCount,
		CacheReadTokens:  g.CachedContentTokenCount,
		// Gemini promptTokenCount counts cached tokens as a subset.
		PromptIncludesCache: true,
	}
}

func extractUsageFromClaudeBody(body []byte) *dto.Usage {
	var probe struct {
		Usage *dto.ClaudeUsage `json:"usage"`
	}
	if json.Unmarshal(body, &probe) != nil || probe.Usage == nil {
		return nil
	}
	u := probe.Usage
	return &dto.Usage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

func extractUsageFromGeminiBody(body []byte) *dto.Usage {
	var probe struct {
		UsageMetadata *dto.GeminiUsage `json:"usageMetadata"`
	}
	if json.Unmarshal(body, &probe) != nil || probe.UsageMetadata == nil {
		return nil
	}
	return geminiUsageToUsage(probe.UsageMetadata)
}

// estimateUsageFromBody is the no-usage fallback: rough char-based estimate.
func estimateUsageFromBody(f Format, body []byte) *dto.Usage {
	var textLen int
	switch f {
	case FormatClaude:
		var probe struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		_ = json.Unmarshal(body, &probe)
		for _, c := range probe.Content {
			textLen += len(c.Text)
		}
	case FormatGemini:
		var probe struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		_ = json.Unmarshal(body, &probe)
		for _, cand := range probe.Candidates {
			for _, p := range cand.Content.Parts {
				textLen += len(p.Text)
			}
		}
	default:
		var probe struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		_ = json.Unmarshal(body, &probe)
		for _, ch := range probe.Choices {
			textLen += len(ch.Message.Content)
		}
	}
	return &dto.Usage{CompletionTokens: dto.EstimateTokens(strings.Repeat("a", textLen))}
}

// upstreamErrorFromBody converts an upstream error body into a RelayError
// with the upstream message preserved.
func upstreamErrorFromBody(clientFormat Format, status int, body []byte) *RelayError {
	msg := string(body)
	var probe struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &probe) == nil && probe.Error != nil && probe.Error.Message != "" {
		if probe.Error.Type != "" {
			return &RelayError{StatusCode: status, Message: probe.Error.Message, ErrType: probe.Error.Type}
		}
		return &RelayError{StatusCode: status, Message: probe.Error.Message, ErrType: "upstream_error"}
	}
	if len(msg) > 512 {
		msg = msg[:512]
	}
	return &RelayError{StatusCode: status, Message: msg, ErrType: "upstream_error"}
}

// UpstreamErrorFromBody exposes upstreamErrorFromBody for controllers.
func UpstreamErrorFromBody(clientFormat Format, status int, body []byte) *RelayError {
	return upstreamErrorFromBody(clientFormat, status, body)
}
