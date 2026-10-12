package helper

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/lxp/htokenmeter/relay/dto"
)

// RelayConvert handles cross-format responses. Non-stream converts the body;
// stream re-emits SSE in the client format (see convert_stream.go).
func RelayConvert(w http.ResponseWriter, resp *http.Response, rc *RelayContext) (*dto.Usage, *RelayError) {
	writeNoStoreHeaders(w)
	if resp.StatusCode >= 400 {
		return handleConvertUpstreamError(w, resp, rc)
	}
	if rc.Stream {
		return convertStream(w, resp, rc)
	}
	body := readAll(resp)
	if body == nil {
		return nil, Errorf(http.StatusBadGateway, "read upstream body failed")
	}
	var out []byte
	var usage *dto.Usage
	var err error
	switch rc.UpstreamFormat {
	case FormatOpenAI, FormatResponses:
		usage = extractUsageFromOpenAIBody(body)
		if usage == nil {
			usage = ExtractUsageFromResponsesBody(body)
		}
		switch rc.ClientFormat {
		case FormatClaude:
			out, usage, err = openAIBodyToClaude(body, rc, usage)
		case FormatGemini:
			out, usage, err = openAIBodyToGemini(body, rc, usage)
		default:
			out = body // openai client: forward
		}
	case FormatClaude:
		usage = extractUsageFromClaudeBody(body)
		switch rc.ClientFormat {
		case FormatOpenAI:
			out, usage, err = claudeBodyToOpenAI(body, rc, usage)
		case FormatGemini:
			var mid []byte
			mid, usage, err = claudeBodyToOpenAI(body, rc, usage)
			if err == nil {
				out, usage, err = openAIBodyToGemini(mid, rc, usage)
			}
		default:
			out = body
		}
	case FormatGemini:
		usage = extractUsageFromGeminiBody(body)
		switch rc.ClientFormat {
		case FormatOpenAI:
			out, usage, err = geminiBodyToOpenAI(body, rc, usage)
		case FormatClaude:
			var mid []byte
			mid, usage, err = geminiBodyToOpenAI(body, rc, usage)
			if err == nil {
				out, usage, err = openAIBodyToClaude(mid, rc, usage)
			}
		default:
			out = body
		}
	}
	if err != nil {
		return nil, Errorf(http.StatusBadGateway, "convert response: %v", err)
	}
	if usage == nil {
		usage = estimateUsageFromBody(rc.UpstreamFormat, body)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
	return usage, nil
}

func readAll(resp *http.Response) []byte {
	defer resp.Body.Close()
	buf := make([]byte, 0, 32*1024)
	tmp := make([]byte, 32*1024)
	total := 0
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			total += n
			if total > 64<<20 {
				return nil
			}
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf
			}
			return buf
		}
	}
}

// handleConvertUpstreamError re-renders the upstream error in the client shape.
func handleConvertUpstreamError(w http.ResponseWriter, resp *http.Response, rc *RelayContext) (*dto.Usage, *RelayError) {
	body := readAll(resp)
	rerr := upstreamErrorFromBody(rc.UpstreamFormat, resp.StatusCode, body)
	WriteError(w, rc.ClientFormat, rerr)
	return nil, rerr
}

// ---------- body converters ----------

func claudeBodyToOpenAI(body []byte, rc *RelayContext, usage *dto.Usage) ([]byte, *dto.Usage, error) {
	var cr dto.ClaudeResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, usage, err
	}
	if usage == nil && cr.Usage != nil {
		usage = &dto.Usage{
			PromptTokens:     cr.Usage.InputTokens,
			CompletionTokens: cr.Usage.OutputTokens,
			CacheReadTokens:  cr.Usage.CacheReadInputTokens,
			CacheWriteTokens: cr.Usage.CacheCreationInputTokens,
		}
	}
	out := dto.ChatResponse{
		Id: cr.Id, Object: "chat.completion", Created: time.Now().Unix(), Model: rc.ClientModel,
	}
	msg := dto.Message{Role: "assistant"}
	var text string
	var toolParts []dto.ToolCall
	for _, b := range cr.Content {
		switch b.Type {
		case "text":
			text += b.Text
		case "tool_use":
			toolParts = append(toolParts, dto.ToolCall{
				Id: b.Id, Type: "function",
				Function: dto.FunctionCall{Name: b.Name, Arguments: string(b.Input)},
			})
		}
	}
	msg.Content = dto.StringContent(text)
	msg.ToolCalls = toolParts
	fr := ""
	if cr.StopReason != nil {
		fr = claudeStopToOpenAI(*cr.StopReason)
	}
	out.Choices = []dto.Choice{{Index: 0, Message: &msg, FinishReason: &fr}}
	if usage != nil {
		out.Usage = &dto.Usage{
			PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
			CacheReadTokens: usage.CacheReadTokens, CacheWriteTokens: usage.CacheWriteTokens,
		}
	}
	b, err := json.Marshal(out)
	return b, usage, err
}

func claudeStopToOpenAI(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence":
		return "stop"
	}
	return "stop"
}

func geminiBodyToOpenAI(body []byte, rc *RelayContext, usage *dto.Usage) ([]byte, *dto.Usage, error) {
	var gr dto.GeminiResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return nil, usage, err
	}
	if usage == nil && gr.UsageMetadata != nil {
		usage = geminiUsageToUsage(gr.UsageMetadata)
	}
	out := dto.ChatResponse{
		Id: "gem-" + rc.ClientModel, Object: "chat.completion", Created: time.Now().Unix(), Model: rc.ClientModel,
	}
	msg := dto.Message{Role: "assistant"}
	var text string
	var toolCalls []dto.ToolCall
	if len(gr.Candidates) > 0 {
		for _, p := range gr.Candidates[0].Content.Parts {
			if p.Text != "" {
				text += p.Text
			}
			if p.Raw != nil {
				if fc, ok := p.Raw["functionCall"]; ok {
					if m, ok := fc.(map[string]any); ok {
						name, _ := m["name"].(string)
						args, _ := json.Marshal(m["args"])
						toolCalls = append(toolCalls, dto.ToolCall{
							Id: "call_" + name, Type: "function",
							Function: dto.FunctionCall{Name: name, Arguments: string(args)},
						})
					}
				}
			}
		}
	}
	msg.Content = dto.StringContent(text)
	msg.ToolCalls = toolCalls
	fr := "stop"
	if len(gr.Candidates) > 0 && gr.Candidates[0].FinishReason == "MAX_TOKENS" {
		fr = "length"
	}
	out.Choices = []dto.Choice{{Index: 0, Message: &msg, FinishReason: &fr}}
	if usage != nil {
		out.Usage = &dto.Usage{
			PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
			CacheReadTokens: usage.CacheReadTokens,
		}
	}
	b, err := json.Marshal(out)
	return b, usage, err
}

func openAIBodyToClaude(body []byte, rc *RelayContext, usage *dto.Usage) ([]byte, *dto.Usage, error) {
	var or dto.ChatResponse
	if err := json.Unmarshal(body, &or); err != nil {
		return nil, usage, err
	}
	out := dto.ClaudeResponse{
		Id: or.Id, Type: "message", Role: "assistant", Model: rc.ClientModel,
		Usage: &dto.ClaudeUsage{InputTokens: 1, OutputTokens: 1},
	}
	if usage != nil {
		out.Usage = &dto.ClaudeUsage{
			InputTokens:              usage.PromptTokens,
			OutputTokens:             usage.CompletionTokens,
			CacheReadInputTokens:     usage.CacheReadTokens,
			CacheCreationInputTokens: usage.CacheWriteTokens,
		}
	}
	if len(or.Choices) > 0 && or.Choices[0].Message != nil {
		m := or.Choices[0].Message
		if t := m.TextContent(); t != "" {
			out.Content = append(out.Content, dto.ClaudeContentBlock{Type: "text", Text: t})
		}
		for _, tc := range m.ToolCalls {
			input := json.RawMessage(tc.Function.Arguments)
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			out.Content = append(out.Content, dto.ClaudeContentBlock{
				Type: "tool_use", Id: tc.Id, Name: tc.Function.Name, Input: input,
			})
		}
		stop := "end_turn"
		if or.Choices[0].FinishReason != nil {
			switch *or.Choices[0].FinishReason {
			case "length":
				stop = "max_tokens"
			case "tool_calls":
				stop = "tool_use"
			}
		}
		out.StopReason = &stop
	}
	if out.Content == nil {
		out.Content = []dto.ClaudeContentBlock{{Type: "text", Text: ""}}
	}
	b, err := json.Marshal(out)
	return b, usage, err
}

func openAIBodyToGemini(body []byte, rc *RelayContext, usage *dto.Usage) ([]byte, *dto.Usage, error) {
	var or dto.ChatResponse
	if err := json.Unmarshal(body, &or); err != nil {
		return nil, usage, err
	}
	gr := dto.GeminiResponse{ModelVersion: rc.ClientModel}
	part := dto.GeminiPart{}
	if len(or.Choices) > 0 && or.Choices[0].Message != nil {
		part.Text = or.Choices[0].Message.TextContent()
	}
	gr.Candidates = []dto.GeminiCandidate{{
		Content:      dto.GeminiContent{Role: "model", Parts: []dto.GeminiPart{part}},
		FinishReason: "STOP",
	}}
	if usage != nil {
		gr.UsageMetadata = &dto.GeminiUsage{
			PromptTokenCount:        usage.PromptTokens,
			CandidatesTokenCount:    usage.CompletionTokens,
			TotalTokenCount:         usage.PromptTokens + usage.CompletionTokens,
			CachedContentTokenCount: usage.CacheReadTokens,
		}
	}
	b, err := json.Marshal(gr)
	return b, usage, err
}
