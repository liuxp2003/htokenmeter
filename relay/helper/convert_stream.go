package helper

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lxp/htokenmeter/relay/dto"
)

// convertStream re-emits upstream SSE in the client's protocol.
// Supported directions: openai->claude, claude->openai, openai->gemini,
// gemini->openai. Claude<->Gemini cross streams are rejected (use non-stream).
func convertStream(w http.ResponseWriter, resp *http.Response, rc *RelayContext) (*dto.Usage, *RelayError) {
	if (rc.UpstreamFormat == FormatClaude && rc.ClientFormat == FormatGemini) ||
		(rc.UpstreamFormat == FormatGemini && rc.ClientFormat == FormatClaude) {
		resp.Body.Close()
		rerr := Errorf(http.StatusBadRequest,
			"cross-format streaming between claude and gemini is not supported; disable stream or use a matching channel")
		WriteError(w, rc.ClientFormat, rerr)
		return nil, rerr
	}
	setSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	sc := newSSEScanner(resp.Body)
	defer resp.Body.Close()

	emit := func(data []byte) {
		_, _ = w.Write(append(append([]byte("data: "), data...), '\n', '\n'))
		if flusher != nil {
			flusher.Flush()
		}
	}
	emitRaw := func(line []byte) {
		_, _ = w.Write(append(append([]byte{}, line...), '\n'))
		if flusher != nil {
			flusher.Flush()
		}
	}

	var usage *dto.Usage
	switch {
	case rc.UpstreamFormat == FormatOpenAI && rc.ClientFormat == FormatClaude:
		usage = streamOpenAIToClaude(sc, emit, emitRaw, rc)
	case rc.UpstreamFormat == FormatClaude && rc.ClientFormat == FormatOpenAI:
		usage = streamClaudeToOpenAI(sc, emit, emitRaw, rc)
	case rc.UpstreamFormat == FormatOpenAI && rc.ClientFormat == FormatGemini:
		usage = streamOpenAIToGemini(sc, emit, rc)
	case rc.UpstreamFormat == FormatGemini && rc.ClientFormat == FormatOpenAI:
		usage = streamGeminiToOpenAI(sc, emit, rc)
	default:
		// Fallback: forward lines and hope for the best.
		for sc.Scan() {
			if u := scanLineUsage(rc.UpstreamFormat, sc.Bytes()); u != nil {
				usage = u
			}
			emitRaw(sc.Bytes())
		}
	}
	if usage == nil {
		usage = &dto.Usage{}
	}
	return usage, nil
}

type sseEmitter func(data []byte)
type rawEmitter func(line []byte)

// ---------- openai -> claude ----------

func streamOpenAIToClaude(sc interface {
	Scan() bool
	Bytes() []byte
}, emit sseEmitter, emitRaw rawEmitter, rc *RelayContext) *dto.Usage {
	var usage *dto.Usage
	started := false
	textStarted := false
	toolIdx := -1
	finish := ""

	startMsg := func() {
		if started {
			return
		}
		started = true
		emit(json.RawMessage(`{"type":"message_start","message":{"id":"msg_` + strconv.FormatInt(nowUnix(), 10) + `","type":"message","role":"assistant","model":"` + jsonEscape(rc.ClientModel) + `","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}`))
	}
	ensureTextBlock := func() {
		startMsg()
		if !textStarted {
			textStarted = true
			emit(json.RawMessage(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`))
		}
	}

	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if len(data) == 0 {
			continue
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string         `json:"content"`
					ToolCalls []dto.ToolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *openaiUsage `json:"usage"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chunk.Usage.toUsage()
		}
		for _, ch := range chunk.Choices {
			if ch.FinishReason != nil {
				finish = *ch.FinishReason
			}
			if ch.Delta.Content != "" {
				ensureTextBlock()
				emit(mustMarshal(map[string]any{
					"type": "content_block_delta", "index": 0,
					"delta": map[string]any{"type": "text_delta", "text": ch.Delta.Content},
				}))
			}
			for _, tc := range ch.Delta.ToolCalls {
				startMsg()
				if toolIdx < 0 {
					toolIdx = 1
					emit(mustMarshal(map[string]any{
						"type": "content_block_start", "index": toolIdx,
						"content_block": map[string]any{"type": "tool_use", "id": tc.Id, "name": tc.Function.Name, "input": map[string]any{}},
					}))
				}
				if tc.Function.Arguments != "" {
					emit(mustMarshal(map[string]any{
						"type": "content_block_delta", "index": toolIdx,
						"delta": map[string]any{"type": "input_json_delta", "partial_json": tc.Function.Arguments},
					}))
				}
			}
		}
	}
	if textStarted {
		emit(json.RawMessage(`{"type":"content_block_stop","index":0}`))
	}
	if toolIdx >= 0 {
		emit(mustMarshal(map[string]any{"type": "content_block_stop", "index": toolIdx}))
	}
	stop := openAIStopToClaude(finish)
	finalUsage := claudeUsageFrom(usage)
	emit(mustMarshal(map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil},
		"usage": finalUsage,
	}))
	emit(json.RawMessage(`{"type":"message_stop"}`))
	return usage
}

// ---------- claude -> openai ----------

func streamClaudeToOpenAI(sc interface {
	Scan() bool
	Bytes() []byte
}, emit sseEmitter, emitRaw rawEmitter, rc *RelayContext) *dto.Usage {
	var usage *dto.Usage
	id := "chatcmpl-" + strconv.FormatInt(nowUnixNano(), 10)
	toolIdx := -1
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if len(data) == 0 {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock *struct {
				Type string `json:"type"`
				Id   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Message *struct {
				Usage *dto.ClaudeUsage `json:"usage"`
			} `json:"message"`
			Usage *dto.ClaudeUsage `json:"usage"`
		}
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				u := ev.Message.Usage
				usage = &dto.Usage{
					PromptTokens: u.InputTokens, CacheReadTokens: u.CacheReadInputTokens,
					CacheWriteTokens: u.CacheCreationInputTokens,
				}
			}
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				toolIdx = ev.Index
				chunk := map[string]any{
					"id": id, "object": "chat.completion.chunk", "created": nowUnix(), "model": rc.ClientModel,
					"choices": []map[string]any{{
						"index": 0,
						"delta": map[string]any{"tool_calls": []map[string]any{{
							"index": ev.Index, "id": ev.ContentBlock.Id, "type": "function",
							"function": map[string]any{"name": ev.ContentBlock.Name, "arguments": ""},
						}}},
						"finish_reason": nil,
					}},
				}
				emit(mustMarshal(chunk))
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			var delta map[string]any
			switch ev.Delta.Type {
			case "text_delta":
				delta = map[string]any{"content": ev.Delta.Text}
			case "input_json_delta":
				delta = map[string]any{"tool_calls": []map[string]any{{
					"index": toolIdx, "function": map[string]any{"arguments": ev.Delta.PartialJSON},
				}}}
			default:
				continue
			}
			chunk := map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": nowUnix(), "model": rc.ClientModel,
				"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}},
			}
			emit(mustMarshal(chunk))
		case "message_delta":
			if ev.Usage != nil && usage != nil {
				usage.CompletionTokens = ev.Usage.OutputTokens
			}
		}
	}
	fr := "stop"
	if toolIdx >= 0 {
		fr = "tool_calls"
	}
	done := map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": nowUnix(), "model": rc.ClientModel,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
	}
	emit(mustMarshal(done))
	if usage != nil {
		emit(mustMarshal(map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": nowUnix(), "model": rc.ClientModel,
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens,
			},
		}))
	}
	emitRaw([]byte("data: [DONE]"))
	return usage
}

// ---------- openai -> gemini ----------

func streamOpenAIToGemini(sc interface {
	Scan() bool
	Bytes() []byte
}, emit sseEmitter, rc *RelayContext) *dto.Usage {
	var usage *dto.Usage
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *openaiUsage `json:"usage"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chunk.Usage.toUsage()
		}
		out := map[string]any{"candidates": []map[string]any{}}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content == "" && ch.FinishReason == nil {
				continue
			}
			cand := map[string]any{
				"content": map[string]any{"role": "model", "parts": []map[string]any{{"text": ch.Delta.Content}}},
			}
			if ch.FinishReason != nil {
				cand["finishReason"] = "STOP"
			}
			out["candidates"] = append(out["candidates"].([]map[string]any), cand)
		}
		if usage != nil {
			out["usageMetadata"] = map[string]any{
				"promptTokenCount":     usage.PromptTokens,
				"candidatesTokenCount": usage.CompletionTokens,
				"totalTokenCount":      usage.PromptTokens + usage.CompletionTokens,
			}
		}
		if len(out["candidates"].([]map[string]any)) > 0 || usage != nil {
			emit(mustMarshal(out))
		}
	}
	return usage
}

// ---------- gemini -> openai ----------

func streamGeminiToOpenAI(sc interface {
	Scan() bool
	Bytes() []byte
}, emit sseEmitter, rc *RelayContext) *dto.Usage {
	var usage *dto.Usage
	id := "chatcmpl-" + strconv.FormatInt(nowUnixNano(), 10)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if len(data) == 0 {
			continue
		}
		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata *dto.GeminiUsage `json:"usageMetadata"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			continue
		}
		if chunk.UsageMetadata != nil {
			usage = geminiUsageToUsage(chunk.UsageMetadata)
		}
		for _, cand := range chunk.Candidates {
			for _, p := range cand.Content.Parts {
				if p.Text == "" {
					continue
				}
				emit(mustMarshal(map[string]any{
					"id": id, "object": "chat.completion.chunk", "created": nowUnix(), "model": rc.ClientModel,
					"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": p.Text}, "finish_reason": nil}},
				}))
			}
		}
	}
	fr := "stop"
	emit(mustMarshal(map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": nowUnix(), "model": rc.ClientModel,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
	}))
	if usage != nil {
		emit(mustMarshal(map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": nowUnix(), "model": rc.ClientModel,
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens,
			},
		}))
	}
	emit([]byte("[DONE]"))
	return usage
}

// ---------- small helpers ----------

func openAIStopToClaude(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	}
	return "end_turn"
}

func claudeUsageFrom(u *dto.Usage) map[string]any {
	if u == nil {
		return map[string]any{"output_tokens": 0}
	}
	return map[string]any{"output_tokens": u.CompletionTokens}
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
