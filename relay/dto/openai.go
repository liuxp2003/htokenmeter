// Package dto defines wire-format DTOs for all supported protocols plus the
// canonical (OpenAI-shaped) internal representation used for cross-format
// conversion. Optional scalars are pointers so explicit zeros survive.
package dto

import "encoding/json"

// ---------- Canonical (internal) chat representation ----------

type ContentPart struct {
	Type     string          `json:"type"` // "text" | "image_url" | "file"
	Text     string          `json:"text,omitempty"`
	ImageURL *ImageURL       `json:"image_url,omitempty"`
	Raw      json.RawMessage `json:"-"` // unknown part types preserved for passthrough
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type Message struct {
	Role       string          `json:"role"` // system | user | assistant | tool
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallId string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type ToolCall struct {
	Id       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatRequest is the canonical request (OpenAI superset).
type ChatRequest struct {
	Model            string          `json:"model"`
	Messages         []Message       `json:"messages"`
	Stream           bool            `json:"stream,omitempty"`
	StreamOptions    *StreamOptions  `json:"stream_options,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	MaxCompletionTok *int            `json:"max_completion_tokens,omitempty"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	Stop             json.RawMessage `json:"stop,omitempty"`
	Tools            json.RawMessage `json:"tools,omitempty"`
	ToolChoice       json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat   json.RawMessage `json:"response_format,omitempty"`
	User             string          `json:"user,omitempty"`
	ReasoningEffort  string          `json:"reasoning_effort,omitempty"`
	Extra            map[string]any  `json:"-"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// TextContent interprets m.Content as plain text when it is a JSON string.
func (m Message) TextContent() string {
	if len(m.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	return ""
}

// Parts interprets m.Content as a part array, mapping to ContentPart with
// unknown types preserved in Raw.
func (m Message) Parts() []ContentPart {
	var arr []json.RawMessage
	if err := json.Unmarshal(m.Content, &arr); err != nil {
		return nil
	}
	var out []ContentPart
	for _, raw := range arr {
		var probe struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &probe)
		switch probe.Type {
		case "text":
			var p struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			_ = json.Unmarshal(raw, &p)
			out = append(out, ContentPart{Type: "text", Text: p.Text})
		case "image_url":
			var p struct {
				Type     string    `json:"type"`
				ImageURL *ImageURL `json:"image_url"`
			}
			_ = json.Unmarshal(raw, &p)
			out = append(out, ContentPart{Type: "image_url", ImageURL: p.ImageURL})
		default:
			out = append(out, ContentPart{Type: probe.Type, Raw: raw})
		}
	}
	return out
}

// SetStringContent / SetPartsContent build Content values.
func StringContent(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func PartsContent(parts []ContentPart) json.RawMessage {
	type part struct {
		Type     string    `json:"type"`
		Text     string    `json:"text,omitempty"`
		ImageURL *ImageURL `json:"image_url,omitempty"`
	}
	// Parts with Raw are marshaled via Raw to preserve unknown fields.
	arr := make([]json.RawMessage, 0, len(parts))
	for _, p := range parts {
		if p.Raw != nil {
			arr = append(arr, p.Raw)
			continue
		}
		if p.Type == "text" {
			b, _ := json.Marshal(part{Type: "text", Text: p.Text})
			arr = append(arr, b)
			continue
		}
		b, _ := json.Marshal(part{Type: p.Type, ImageURL: p.ImageURL})
		arr = append(arr, b)
	}
	out, _ := json.Marshal(arr)
	return out
}

// ---------- OpenAI responses ----------

type ChatResponse struct {
	Id      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Message `json:"delta,omitempty"`
	FinishReason *string  `json:"finish_reason"`
}

// ---------- Embeddings ----------

type EmbeddingRequest struct {
	Model string          `json:"model"`
	Input json.RawMessage `json:"input"`
	Extra map[string]any  `json:"-"`
}

type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   json.RawMessage `json:"data"`
	Model  string          `json:"model"`
	Usage  *Usage          `json:"usage,omitempty"`
}

// ---------- Images ----------

type ImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Style          string `json:"style,omitempty"`
	User           string `json:"user,omitempty"`
}

// ---------- Models ----------

type ModelCard struct {
	Id      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelListResponse struct {
	Object string      `json:"object"`
	Data   []ModelCard `json:"data"`
}
