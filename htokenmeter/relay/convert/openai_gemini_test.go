package convert

import (
	"encoding/json"
	"testing"

	"github.com/lxp/htokenmeter/relay/dto"
)

// F015: functionDeclarations must carry parameters.
func TestGeminiToolsCarryParameters(t *testing.T) {
	req := &dto.ChatRequest{
		Model:    "gem",
		Messages: []dto.Message{{Role: "user", Content: dto.StringContent("hi")}},
		Tools:    raw(`[{"type":"function","function":{"name":"get_weather","description":"w","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]`),
	}
	body := OpenAIToGemini(req)
	tools, _ := json.Marshal(body["tools"])
	if !json.Valid(tools) {
		t.Fatalf("tools json invalid: %s", tools)
	}
	var parsed []struct {
		FunctionDeclarations []map[string]any `json:"functionDeclarations"`
	}
	if err := json.Unmarshal(tools, &parsed); err != nil || len(parsed) == 0 {
		t.Fatalf("tools parse: %v %s", err, tools)
	}
	decl := parsed[0].FunctionDeclarations[0]
	params, ok := decl["parameters"].(map[string]any)
	if !ok || params["type"] != "object" {
		t.Fatalf("declaration parameters missing: %v", decl)
	}
}

// F015: GeminiPart captures functionCall/functionResponse into Raw and
// re-marshals them.
func TestGeminiPartRawRoundTrip(t *testing.T) {
	var c dto.GeminiContent
	in := `{"role":"model","parts":[{"text":"calling"},{"functionCall":{"name":"f","args":{"a":1}}}]}`
	if err := json.Unmarshal([]byte(in), &c); err != nil {
		t.Fatal(err)
	}
	if c.Parts[1].Raw == nil || c.Parts[1].Raw["functionCall"] == nil {
		t.Fatalf("functionCall not captured: %+v", c.Parts[1])
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) || !contains(out, `"functionCall"`) {
		t.Fatalf("round trip lost functionCall: %s", out)
	}
}

func contains(b []byte, sub string) bool {
	return len(sub) == 0 || indexOf(string(b), sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// F015: gemini request with tool traffic converts to canonical tool calls
// and tool results with matching ids.
func TestGeminiRequestToolRoundTrip(t *testing.T) {
	in := `{
		"contents":[
			{"role":"user","parts":[{"text":"weather?"}]},
			{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"SF"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"get_weather","response":{"temp":21}}}]}
		]}`
	var g dto.GeminiRequest
	if err := json.Unmarshal([]byte(in), &g); err != nil {
		t.Fatal(err)
	}
	out := GeminiToOpenAI("gem", &g)
	var asst *dto.Message
	var tool *dto.Message
	for i := range out.Messages {
		switch out.Messages[i].Role {
		case "assistant":
			asst = &out.Messages[i]
		case "tool":
			tool = &out.Messages[i]
		}
	}
	if asst == nil || len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant tool calls = %+v", asst)
	}
	tc := asst.ToolCalls[0]
	if tc.Function.Name != "get_weather" || tc.Function.Arguments != `{"city":"SF"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if tc.Id == "" {
		t.Fatal("tool call id empty")
	}
	if tool == nil {
		t.Fatal("no tool message")
	}
	if tool.ToolCallId != tc.Id {
		t.Fatalf("tool_call_id mismatch: %q vs %q", tool.ToolCallId, tc.Id)
	}
	if tool.TextContent() != `{"temp":21}` {
		t.Fatalf("tool content = %q", tool.TextContent())
	}
}

// args absent must default to "{}" not "null".
func TestGeminiArgsNilDefaults(t *testing.T) {
	in := `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f"}}]}]}`
	var g dto.GeminiRequest
	if err := json.Unmarshal([]byte(in), &g); err != nil {
		t.Fatal(err)
	}
	out := GeminiToOpenAI("gem", &g)
	for _, m := range out.Messages {
		for _, tc := range m.ToolCalls {
			if tc.Function.Arguments != "{}" {
				t.Fatalf("args = %q, want {}", tc.Function.Arguments)
			}
		}
	}
}
