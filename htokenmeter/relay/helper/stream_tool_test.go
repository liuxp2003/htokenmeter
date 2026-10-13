package helper

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lxp/htokenmeter/relay/dto"
)

type lineScanner struct {
	lines []string
	i     int
}

func (s *lineScanner) Scan() bool { s.i++; return s.i-1 < len(s.lines) }
func (s *lineScanner) Bytes() []byte {
	return []byte(s.lines[s.i-1])
}

func collectEmits() (*[]string, sseEmitter) {
	var out []string
	return &out, func(data []byte) { out = append(out, string(data)) }
}

// F014: parallel tool calls stream as separate content blocks with deltas
// routed by upstream index.
func TestStreamOpenAIToClaudeParallelTools(t *testing.T) {
	sse := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"weather","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"time","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"UTC\""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"SF\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	emitted, emit := collectEmits()
	sc := &lineScanner{lines: sse}
	u := streamOpenAIToClaude(sc, emit, func([]byte) {}, &RelayContext{ClientModel: "m"})
	_ = u

	var text strings.Builder
	for _, e := range *emitted {
		text.WriteString(e)
		text.WriteString("\n")
	}
	all := text.String()

	// Two distinct block starts with their own ids/names.
	if !strings.Contains(all, `"id":"call_a"`) || !strings.Contains(all, `"name":"weather"`) {
		t.Fatalf("missing call_a/weather announcement:\n%s", all)
	}
	if !strings.Contains(all, `"id":"call_b"`) || !strings.Contains(all, `"name":"time"`) {
		t.Fatalf("missing call_b/time announcement:\n%s", all)
	}
	// Deltas must land on distinct block indices (marshal sorts keys):
	// "index":1 and "index":2 as content_block_delta.
	if !strings.Contains(all, `"index":1,"type":"content_block_delta"`) {
		t.Fatalf("no delta routed to block 1:\n%s", all)
	}
	if !strings.Contains(all, `"index":2,"type":"content_block_delta"`) {
		t.Fatalf("no delta routed to block 2:\n%s", all)
	}
	// Both blocks stopped.
	if strings.Count(all, `"type":"content_block_stop"`) != 2 {
		t.Fatalf("expected 2 block stops:\n%s", all)
	}
	// Interleaved arguments must not be concatenated into one stream:
	// call_a args = {"city":"SF"} (JSON-escaped inside partial_json).
	if !strings.Contains(all, `\"city\":`) {
		t.Fatalf("call_a arguments lost:\n%s", all)
	}
}

// Single unindexed tool call keeps working (index defaults to 0).
func TestStreamOpenAIToClaudeSingleTool(t *testing.T) {
	sse := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x","function":{"name":"f","arguments":"{\"a\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	emitted, emit := collectEmits()
	u := streamOpenAIToClaude(&lineScanner{lines: sse}, emit, func([]byte) {}, &RelayContext{ClientModel: "m"})
	_ = u
	var text bytes.Buffer
	for _, e := range *emitted {
		text.WriteString(e)
	}
	if !strings.Contains(text.String(), `\"a\":`) {
		t.Fatalf("single tool args mangled: %s", text.String())
	}
	if strings.Count(text.String(), `"type":"content_block_stop"`) != 1 {
		t.Fatalf("expected exactly 1 stop: %s", text.String())
	}
}

var _ = dto.Usage{}
