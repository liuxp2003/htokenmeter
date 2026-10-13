package dto

import "encoding/json"

// ---------- Claude /v1/messages wire format ----------

type ClaudeRequest struct {
	Model         string          `json:"model"`
	Messages      []ClaudeMessage `json:"messages"`
	System        json.RawMessage `json:"system,omitempty"` // string or []block
	MaxTokens     int             `json:"max_tokens"`
	Stream        bool            `json:"stream,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Tools         json.RawMessage `json:"tools,omitempty"`
	ToolChoice    json.RawMessage `json:"tool_choice,omitempty"`
	Thinking      json.RawMessage `json:"thinking,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

type ClaudeMessage struct {
	Role       string          `json:"role"` // user | assistant
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCalls  []ClaudeToolUse `json:"-"`
	ToolCallId string          `json:"-"` // filled by converter
}

type ClaudeToolUse struct {
	Id    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ClaudeResponse struct {
	Id           string               `json:"id"`
	Type         string               `json:"type"` // message
	Role         string               `json:"role"`
	Content      []ClaudeContentBlock `json:"content"`
	Model        string               `json:"model"`
	StopReason   *string              `json:"stop_reason"`
	StopSequence *string              `json:"stop_sequence"`
	Usage        *ClaudeUsage         `json:"usage"`
}

type ClaudeContentBlock struct {
	Type  string          `json:"type"` // text | tool_use | thinking
	Text  string          `json:"text,omitempty"`
	Id    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type ClaudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type ClaudeErrorBody struct {
	Type  string            `json:"type"`
	Error ClaudeErrorDetail `json:"error"`
}

type ClaudeErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
