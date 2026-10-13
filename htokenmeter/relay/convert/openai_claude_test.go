package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lxp/htokenmeter/relay/dto"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

// F023: consecutive OpenAI tool messages must merge into ONE Claude user
// message holding multiple tool_result blocks (Claude requires role
// alternation).
func TestAdjacentToolMessagesMerge(t *testing.T) {
	req := &dto.ChatRequest{
		Model: "claude-x",
		Messages: []dto.Message{
			{Role: "user", Content: dto.StringContent("run both")},
			{Role: "assistant", ToolCalls: []dto.ToolCall{
				{Id: "t1", Type: "function", Function: dto.FunctionCall{Name: "get_weather"}},
				{Id: "t2", Type: "function", Function: dto.FunctionCall{Name: "get_time"}},
			}},
			{Role: "tool", ToolCallId: "t1", Content: dto.StringContent(`{"t":21}`)},
			{Role: "tool", ToolCallId: "t2", Content: dto.StringContent(`{"h":"10:00"}`)},
			{Role: "user", Content: dto.StringContent("thanks")},
		},
	}
	out, err := OpenAIToClaude(req)
	if err != nil {
		t.Fatal(err)
	}
	// Expect: user, assistant, user(2 tool_results + trailing text merged).
	if len(out.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(out.Messages))
	}
	merged := out.Messages[2]
	if merged.Role != "user" {
		t.Fatalf("merged role = %s", merged.Role)
	}
	var blocks []map[string]any
	if json.Unmarshal(merged.Content, &blocks) != nil || len(blocks) != 3 {
		t.Fatalf("merged blocks = %v", merged.Content)
	}
	if blocks[0]["tool_use_id"] != "t1" || blocks[1]["tool_use_id"] != "t2" {
		t.Fatalf("tool ids = %v/%v", blocks[0]["tool_use_id"], blocks[1]["tool_use_id"])
	}
	if blocks[2]["text"] != "thanks" {
		t.Fatalf("trailing text = %v", blocks[2]["text"])
	}
	// No adjacent user-user anywhere.
	for i := 1; i < len(out.Messages); i++ {
		if out.Messages[i].Role == "user" && out.Messages[i-1].Role == "user" {
			t.Fatalf("adjacent user messages at %d", i)
		}
	}
}

// F024: tool_result content as a block array flattens instead of "".
func TestToolResultArrayContent(t *testing.T) {
	creq := &dto.ClaudeRequest{
		Model: "claude-x", MaxTokens: 100,
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: dto.StringContent("hi")},
			{Role: "assistant", Content: raw(`[{"type":"tool_use","id":"t1","name":"f","input":{}}]`)},
			{Role: "user", Content: raw(`[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]}]`)},
		},
	}
	out := ClaudeToOpenAI(creq)
	var toolMsg *dto.Message
	for i := range out.Messages {
		if out.Messages[i].Role == "tool" {
			toolMsg = &out.Messages[i]
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool message in conversion")
	}
	if toolMsg.TextContent() != "line1\nline2" {
		t.Fatalf("tool content = %q, want line1\\nline2", toolMsg.TextContent())
	}
}

// F022: tool_choice maps in both directions.
func TestToolChoiceMapping(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"auto"`, `{"type":"auto"}`},
		{`"required"`, `{"type":"any"}`},
		{`{"type":"function","function":{"name":"weather"}}`, `{"name":"weather","type":"tool"}`},
	}
	for _, c := range cases {
		got := openAIToolChoiceToClaude(raw(c.in))
		if string(got) != c.want {
			t.Fatalf("openai→claude %s = %s, want %s", c.in, got, c.want)
		}
	}
	back := []struct {
		in   string
		want string
	}{
		{`{"type":"auto"}`, `"auto"`},
		{`{"type":"any"}`, `"required"`},
		{`{"type":"tool","name":"weather"}`, `{"function":{"name":"weather"},"type":"function"}`},
	}
	for _, c := range back {
		got := claudeToolChoiceToOpenAI(raw(c.in))
		if string(got) != c.want {
			t.Fatalf("claude→openai %s = %s, want %s", c.in, got, c.want)
		}
	}
}

// F022: tool_choice "none" drops the tools entirely (Claude has no none).
func TestToolChoiceNoneDropsTools(t *testing.T) {
	req := &dto.ChatRequest{
		Model: "claude-x",
		Messages: []dto.Message{{Role: "user", Content: dto.StringContent("hi")}},
		Tools:   raw(`[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}]`),
		ToolChoice: raw(`"none"`),
	}
	out, err := OpenAIToClaude(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Tools != nil {
		t.Fatalf("tools = %s, want nil for tool_choice none", out.Tools)
	}
	if out.ToolChoice != nil {
		t.Fatalf("tool_choice = %s, want nil", out.ToolChoice)
	}
}

// F026: reasoning_effort high must keep budget < max_tokens by raising
// max_tokens.
func TestThinkingBudgetClampedAgainstMaxTokens(t *testing.T) {
	req := &dto.ChatRequest{
		Model:            "claude-x",
		ReasoningEffort:  "high",
		Messages:         []dto.Message{{Role: "user", Content: dto.StringContent("hi")}},
	}
	out, err := OpenAIToClaude(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.MaxTokens != 16384+1024 {
		t.Fatalf("max_tokens = %d, want %d (budget 16384 + 1024)", out.MaxTokens, 16384+1024)
	}
	var think struct {
		BudgetTokens int `json:"budget_tokens"`
	}
	if err := json.Unmarshal(out.Thinking, &think); err != nil || think.BudgetTokens != 16384 {
		t.Fatalf("thinking = %s (%v)", out.Thinking, err)
	}
}

// Regression: single tool call stream still produces one block with the
// right name and id.
func TestSingleToolCallStreamShape(t *testing.T) {
	// Covered via helper-package fixture tests; here we sanity-check the
	// non-stream request conversion keeps tool_use input JSON intact.
	req := &dto.ChatRequest{
		Model: "claude-x",
		Messages: []dto.Message{
			{Role: "assistant", ToolCalls: []dto.ToolCall{
				{Id: "t9", Type: "function", Function: dto.FunctionCall{Name: "f", Arguments: `{"a":1}`}},
			}},
		},
	}
	out, err := OpenAIToClaude(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out.Messages[0].Content)
	if !strings.Contains(s, `"tool_use"`) || !strings.Contains(s, `"input":{"a":1}`) {
		t.Fatalf("assistant tool_use block = %s", s)
	}
}
