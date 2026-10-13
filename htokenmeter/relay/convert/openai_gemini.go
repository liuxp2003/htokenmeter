package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lxp/htokenmeter/relay/dto"
)

// ---------- OpenAI -> Gemini ----------

// OpenAIToGemini converts a canonical request into Gemini generateContent shape.
func OpenAIToGemini(req *dto.ChatRequest) map[string]any {
	// tool_call_id -> function name registry (Gemini functionResponse uses names).
	toolNameById := map[string]string{}
	for _, m := range req.Messages {
		for _, tc := range m.ToolCalls {
			toolNameById[tc.Id] = tc.Function.Name
		}
	}
	contents := make([]map[string]any, 0, len(req.Messages))
	var systemText []string
	for _, m := range req.Messages {
		role := "user"
		switch m.Role {
		case "system", "developer":
			if t := m.TextContent(); t != "" {
				systemText = append(systemText, t)
			}
			continue
		case "assistant":
			role = "model"
		case "tool":
			name := toolNameById[m.ToolCallId]
			if name == "" {
				name = m.Name
			}
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{{"functionResponse": map[string]any{
					"name":     name,
					"response": map[string]any{"result": m.TextContent()},
				}}},
			})
			continue
		}
		var parts []map[string]any
		// assistant tool_calls -> functionCall parts
		for _, tc := range m.ToolCalls {
			args := json.RawMessage(tc.Function.Arguments)
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			parts = append(parts, map[string]any{"functionCall": map[string]any{
				"name": tc.Function.Name, "args": args,
			}})
		}
		ps := m.Parts()
		if ps == nil {
			if t := m.TextContent(); t != "" {
				parts = append(parts, map[string]any{"text": t})
			}
		} else {
			for _, p := range ps {
				switch p.Type {
				case "text":
					parts = append(parts, map[string]any{"text": p.Text})
				case "image_url":
					if p.ImageURL != nil {
						if mime, data, ok := splitDataURL(p.ImageURL.URL); ok {
							parts = append(parts, map[string]any{"inlineData": map[string]any{
								"mimeType": mime, "data": data,
							}})
						}
					}
				}
			}
		}
		if len(parts) == 0 {
			parts = []map[string]any{{"text": ""}}
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	body := map[string]any{"contents": contents}
	if len(systemText) > 0 {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": strings.Join(systemText, "\n\n")}},
		}
	}
	gc := map[string]any{}
	if req.MaxTokens != nil {
		gc["maxOutputTokens"] = *req.MaxTokens
	} else if req.MaxCompletionTok != nil {
		gc["maxOutputTokens"] = *req.MaxCompletionTok
	}
	if req.Temperature != nil {
		gc["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		gc["topP"] = *req.TopP
	}
	if req.Stop != nil {
		var stops []string
		if err := json.Unmarshal(req.Stop, &stops); err == nil {
			gc["stopSequences"] = stops
		}
	}
	if len(req.Tools) > 0 {
		var in []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		}
		if err := json.Unmarshal(req.Tools, &in); err == nil {
			var decls []map[string]any
			for _, t := range in {
				decl := map[string]any{
					"name": t.Function.Name, "description": t.Function.Description,
				}
				// Without parameters Gemini cannot type the call arguments (F015).
				if len(t.Function.Parameters) > 0 {
					decl["parameters"] = t.Function.Parameters
				}
				decls = append(decls, decl)
			}
			body["tools"] = []map[string]any{{"functionDeclarations": decls}}
		}
	}
	if len(gc) > 0 {
		body["generationConfig"] = gc
	}
	return body
}

// GeminiToOpenAI converts a Gemini request into canonical form.
func GeminiToOpenAI(model string, g *dto.GeminiRequest) *dto.ChatRequest {
	out := &dto.ChatRequest{Model: model, Stream: false}
	callSeq := 0
	toolCallIdByName := map[string]string{}
	if g.SystemInstruction != nil {
		var text strings.Builder
		for _, p := range g.SystemInstruction.Parts {
			text.WriteString(p.Text)
		}
		if text.Len() > 0 {
			out.Messages = append(out.Messages, dto.Message{Role: "system", Content: dto.StringContent(text.String())})
		}
	}
	for _, c := range g.Contents {
		role := "user"
		if c.Role == "model" {
			role = "assistant"
		}
		var parts []dto.ContentPart
		var toolCalls []dto.ToolCall
		for _, p := range c.Parts {
			switch {
			case p.Text != "":
				parts = append(parts, dto.ContentPart{Type: "text", Text: p.Text})
			case p.InlineData != nil:
				parts = append(parts, dto.ContentPart{Type: "image_url", ImageURL: &dto.ImageURL{
					URL: "data:" + p.InlineData.MimeType + ";base64," + p.InlineData.Data,
				}})
			case p.Raw != nil:
				if fc, ok := p.Raw["functionCall"]; ok {
					if m, ok := fc.(map[string]any); ok {
						name, _ := m["name"].(string)
						args := marshalGeminiArgs(m["args"])
						id := fmt.Sprintf("call_%s_%d", name, callSeq)
						callSeq++
						toolCallIdByName[name] = id
						toolCalls = append(toolCalls, dto.ToolCall{
							Id: id, Type: "function",
							Function: dto.FunctionCall{Name: name, Arguments: args},
						})
					}
					continue
				}
				if fr, ok := p.Raw["functionResponse"]; ok {
					if m, ok := fr.(map[string]any); ok {
						name, _ := m["name"].(string)
						resp := marshalGeminiArgs(m["response"])
						out.Messages = append(out.Messages, dto.Message{
							Role: "tool", ToolCallId: toolCallIdByName[name], Name: name,
							Content: dto.StringContent(resp),
						})
					}
					continue
				}
			}
		}
		if len(toolCalls) > 0 {
			om := dto.Message{Role: "assistant", ToolCalls: toolCalls}
			if len(parts) > 0 {
				om.Content = dto.PartsContent(parts)
			}
			out.Messages = append(out.Messages, om)
		} else if len(parts) > 0 {
			out.Messages = append(out.Messages, dto.Message{Role: role, Content: dto.PartsContent(parts)})
		}
	}
	if g.GenerationConfig != nil {
		gc := g.GenerationConfig
		if gc.MaxOutputTokens > 0 {
			mt := gc.MaxOutputTokens
			out.MaxTokens = &mt
		}
		out.Temperature = gc.Temperature
		out.TopP = gc.TopP
		if len(gc.StopSequences) > 0 {
			out.Stop = mustJSON(gc.StopSequences)
		}
	}
	return out
}

// marshalGeminiArgs renders a Gemini args/response value as JSON text,
// defaulting missing args to "{}" instead of "null".
func marshalGeminiArgs(v any) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return "{}"
	}
	return string(b)
}

func splitDataURL(url string) (mime, data string, ok bool) {
	if !strings.HasPrefix(url, "data:") {
		return "", "", false
	}
	rest := url[5:]
	meta, d, _ := strings.Cut(rest, ",")
	mime = meta
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = mime[:i]
	}
	return mime, d, true
}
