// Package convert implements the chat-semantics conversion matrix between
// OpenAI (canonical), Claude and Gemini formats. Only the chat subset is
// guaranteed lossless (system/messages/tools/stream/multimodal text+image);
// provider-specific concepts stay on their native path via passthrough.
package convert

import (
	"encoding/json"
	"strings"

	"github.com/lxp/htokenmeter/relay/dto"
)

// ---------- OpenAI -> Claude ----------

// OpenAIToClaude converts a canonical request into Claude /v1/messages shape.
func OpenAIToClaude(req *dto.ChatRequest) (*dto.ClaudeRequest, error) {
	out := &dto.ClaudeRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Tools:       openAIToolsToClaude(req.Tools),
		ToolChoice:  req.ToolChoice,
		Thinking:    reasoningEffortToThinking(req.ReasoningEffort),
	}
	maxTok := 8192
	if req.MaxTokens != nil {
		maxTok = *req.MaxTokens
	} else if req.MaxCompletionTok != nil {
		maxTok = *req.MaxCompletionTok
	}
	out.MaxTokens = maxTok

	var systemParts []string
	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			if t := m.TextContent(); t != "" {
				systemParts = append(systemParts, t)
			}
		case "tool":
			out.Messages = append(out.Messages, dto.ClaudeMessage{
				Role: "user",
				Content: mustJSON([]map[string]any{{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallId,
					"content":     m.TextContent(),
				}}),
			})
		case "assistant":
			cm := dto.ClaudeMessage{Role: "assistant"}
			var blocks []map[string]any
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.Id,
					"name":  tc.Function.Name,
					"input": json.RawMessage(tc.Function.Arguments),
				})
			}
			if t := m.TextContent(); t != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": t})
			}
			if len(blocks) == 0 {
				blocks = []map[string]any{{"type": "text", "text": ""}}
			}
			cm.Content = mustJSON(blocks)
			out.Messages = append(out.Messages, cm)
		default: // user
			cm := dto.ClaudeMessage{Role: "user"}
			parts := m.Parts()
			if parts == nil {
				cm.Content = dto.StringContent(m.TextContent())
			} else {
				var blocks []map[string]any
				for _, p := range parts {
					switch p.Type {
					case "text":
						blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
					case "image_url":
						if p.ImageURL != nil {
							blocks = append(blocks, imageURLToClaudeBlock(p.ImageURL.URL))
						}
					default:
						if p.Raw != nil {
							blocks = append(blocks, rawBlock(p.Raw))
						}
					}
				}
				cm.Content = mustJSON(blocks)
			}
			out.Messages = append(out.Messages, cm)
		}
	}
	if len(systemParts) > 0 {
		out.System = dto.StringContent(strings.Join(systemParts, "\n\n"))
	}
	if req.Stop != nil {
		var stops []string
		if err := json.Unmarshal(req.Stop, &stops); err == nil {
			out.StopSequences = stops
		}
	}
	return out, nil
}

func imageURLToClaudeBlock(url string) map[string]any {
	if strings.HasPrefix(url, "data:") {
		mime, data, _ := strings.Cut(url[5:], ",")
		if i := strings.Index(mime, ";"); i >= 0 {
			mime = mime[:i]
		}
		return map[string]any{
			"type": "image", "source": map[string]any{
				"type": "base64", "media_type": mime, "data": data,
			},
		}
	}
	// Remote URL: Claude source type url requires beta; fetch by gateway is out
	// of scope for v1, pass through as document-less text hint is lossy; keep url block.
	return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": url}}
}

func openAIToolsToClaude(tools json.RawMessage) json.RawMessage {
	if len(tools) == 0 {
		return nil
	}
	var in []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(tools, &in); err != nil {
		return nil
	}
	var out []map[string]any
	for _, t := range in {
		if t.Type != "function" && t.Type != "" {
			continue
		}
		out = append(out, map[string]any{
			"name":         t.Function.Name,
			"description":  t.Function.Description,
			"input_schema": t.Function.Parameters,
		})
	}
	if len(out) == 0 {
		return nil
	}
	b, _ := json.Marshal(out)
	return b
}

// ClaudeToOpenAI converts a Claude request into canonical form.
func ClaudeToOpenAI(req *dto.ClaudeRequest) *dto.ChatRequest {
	out := &dto.ChatRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Tools:       claudeToolsToOpenAI(req.Tools),
		ToolChoice:  req.ToolChoice,
	}
	mt := req.MaxTokens
	out.MaxTokens = &mt
	if len(req.StopSequences) > 0 {
		out.Stop = mustJSON(req.StopSequences)
	}
	if req.System != nil {
		var sys string
		if err := json.Unmarshal(req.System, &sys); err == nil {
			out.Messages = append(out.Messages, dto.Message{Role: "system", Content: dto.StringContent(sys)})
		} else {
			var blocks []struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(req.System, &blocks); err == nil {
				var texts []string
				for _, b := range blocks {
					texts = append(texts, b.Text)
				}
				out.Messages = append(out.Messages, dto.Message{Role: "system", Content: dto.StringContent(strings.Join(texts, "\n\n"))})
			}
		}
	}
	var pendingToolResults []dto.Message
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			om := dto.Message{Role: "assistant"}
			var blocks []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				Id    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			_ = json.Unmarshal(m.Content, &blocks)
			var textBuf strings.Builder
			for _, b := range blocks {
				switch b.Type {
				case "text":
					textBuf.WriteString(b.Text)
				case "tool_use":
					args := b.Input
					if args == nil {
						args = json.RawMessage("{}")
					}
					om.ToolCalls = append(om.ToolCalls, dto.ToolCall{
						Id: b.Id, Type: "function",
						Function: dto.FunctionCall{Name: b.Name, Arguments: string(args)},
					})
				}
			}
			if textBuf.Len() > 0 {
				om.Content = dto.StringContent(textBuf.String())
			}
			out.Messages = append(out.Messages, om)
			out.Messages = append(out.Messages, pendingToolResults...)
			pendingToolResults = nil
		default: // user
			var blocks []struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				ToolUseId string          `json:"tool_use_id"`
				Content   json.RawMessage `json:"content"`
				Source    *struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
					URL       string `json:"url"`
				} `json:"source"`
			}
			if err := json.Unmarshal(m.Content, &blocks); err == nil && len(blocks) > 0 {
				var parts []dto.ContentPart
				isToolResult := false
				for _, b := range blocks {
					switch b.Type {
					case "text":
						parts = append(parts, dto.ContentPart{Type: "text", Text: b.Text})
					case "image":
						if b.Source != nil && b.Source.Type == "base64" {
							parts = append(parts, dto.ContentPart{Type: "image_url", ImageURL: &dto.ImageURL{
								URL: "data:" + b.Source.MediaType + ";base64," + b.Source.Data,
							}})
						}
					case "tool_result":
						isToolResult = true
						var contentText string
						_ = json.Unmarshal(b.Content, &contentText)
						pendingToolResults = append(pendingToolResults, dto.Message{
							Role: "tool", ToolCallId: b.ToolUseId, Content: dto.StringContent(contentText),
						})
					}
				}
				if !isToolResult && len(parts) > 0 {
					out.Messages = append(out.Messages, dto.Message{Role: "user", Content: dto.PartsContent(parts)})
				}
			} else {
				out.Messages = append(out.Messages, dto.Message{Role: "user", Content: m.Content})
			}
		}
	}
	out.Messages = append(out.Messages, pendingToolResults...)
	return out
}

func claudeToolsToOpenAI(tools json.RawMessage) json.RawMessage {
	if len(tools) == 0 {
		return nil
	}
	var in []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal(tools, &in); err != nil {
		return nil
	}
	var out []map[string]any
	for _, t := range in {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": t.InputSchema,
			},
		})
	}
	b, _ := json.Marshal(out)
	return b
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// rawBlock adapts a RawMessage for appends into []map[string]any via
// re-marshaling through json.RawMessage passthrough.
func rawBlock(raw json.RawMessage) map[string]any {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		return m
	}
	return map[string]any{"type": "text", "text": string(raw)}
}

// reasoningEffortToThinking maps OpenAI reasoning_effort to Claude thinking
// budget tokens; empty effort returns nil (thinking off).
func reasoningEffortToThinking(effort string) json.RawMessage {
	var budget int
	switch effort {
	case "low":
		budget = 2048
	case "medium", "":
		if effort == "" {
			return nil
		}
		budget = 8192
	case "high":
		budget = 16384
	default:
		return nil
	}
	return mustJSON(map[string]any{"type": "enabled", "budget_tokens": budget})
}
