package helper

import (
	"strings"
	"testing"
)

// F015: streaming gemini functionCall parts become tool_calls deltas with
// a tool_calls finish.
func TestStreamGeminiToOpenAIToolCall(t *testing.T) {
	sse := []string{
		`data: {"candidates":[{"content":{"parts":[{"text":"let me check"}]}},{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"SF"}}}]}}]}`,
		`data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}`,
	}
	emitted, emit := collectEmits()
	u := streamGeminiToOpenAI(&lineScanner{lines: sse}, emit, testRC("m"))
	_ = u
	var all strings.Builder
	for _, e := range *emitted {
		all.WriteString(e)
		all.WriteString("\n")
	}
	s := all.String()
	if !strings.Contains(s, `"tool_calls"`) {
		t.Fatalf("no tool_calls delta:\n%s", s)
	}
	if !strings.Contains(s, `"name":"get_weather"`) {
		t.Fatalf("tool name lost:\n%s", s)
	}
	if !strings.Contains(s, `\"city\":\"SF\"`) {
		t.Fatalf("args lost:\n%s", s)
	}
	if !strings.Contains(s, `"finish_reason":"tool_calls"`) {
		t.Fatalf("finish_reason missing:\n%s", s)
	}
}

// F021: a mid-stream Claude error event surfaces as an OpenAI error chunk
// and the stream is NOT wrapped in a fake clean finish.
func TestStreamClaudeToOpenAIErrorEvent(t *testing.T) {
	sse := []string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
		`data: {"type":"error","error":{"type":"overloaded_error","message":"upstream exploded"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
	}
	emitted, emit := collectEmits()
	var raws []string
	u := streamClaudeToOpenAI(&lineScanner{lines: sse}, emit, func(b []byte) { raws = append(raws, string(b)) }, testRC("m"))
	_ = u
	var all strings.Builder
	for _, e := range *emitted {
		all.WriteString(e)
		all.WriteString("\n")
	}
	s := all.String()
	if !strings.Contains(s, "upstream exploded") {
		t.Fatalf("error message not surfaced:\n%s", s)
	}
	for _, r := range raws {
		if strings.Contains(r, "[DONE]") {
			t.Fatalf("fake [DONE] after error: %s", r)
		}
	}
	if strings.Contains(s, `"finish_reason":"`) {
		t.Fatalf("fabricated finish after error:\n%s", s)
	}
}

// F021: max_tokens stop_reason maps to finish_reason length.
func TestStreamClaudeToOpenAIMaxTokens(t *testing.T) {
	sse := []string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"abc"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":2}}`,
		`data: {"type":"message_stop"}`,
	}
	emitted, emit := collectEmits()
	u := streamClaudeToOpenAI(&lineScanner{lines: sse}, emit, func([]byte) {}, testRC("m"))
	_ = u
	var all strings.Builder
	for _, e := range *emitted {
		all.WriteString(e)
		all.WriteString("\n")
	}
	if !strings.Contains(all.String(), `"finish_reason":"length"`) {
		t.Fatalf("max_tokens not mapped to length:\n%s", all.String())
	}
}

func testRC(model string) *RelayContext {
	return &RelayContext{ClientModel: model, UpstreamFormat: FormatGemini, ClientFormat: FormatOpenAI}
}
