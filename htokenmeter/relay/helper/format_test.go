package helper

import "testing"

const flatResponses = `{"id":"resp_1","usage":{"input_tokens":10,"output_tokens":5,
"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":3}}}`

const nestedResponses = `{"type":"response.completed","response":{"id":"resp_1",
"usage":{"input_tokens":8,"output_tokens":6}}}`

// F012: output_tokens already contains reasoning_tokens — must not be added.
func TestResponsesUsageNoReasoningDoubleCount(t *testing.T) {
	u := ExtractUsageFromResponsesBody([]byte(flatResponses))
	if u == nil {
		t.Fatal("flat usage not extracted")
	}
	if u.CompletionTokens != 5 {
		t.Fatalf("completion = %d, want 5 (reasoning is a subset, not an addend)", u.CompletionTokens)
	}
	if u.PromptTokens != 10 || u.CacheReadTokens != 4 {
		t.Fatalf("prompt=%d cacheRead=%d, want 10/4", u.PromptTokens, u.CacheReadTokens)
	}
	if !u.PromptIncludesCache {
		t.Fatal("responses prompt must include cache")
	}
}

// F013: streaming response.completed nests usage under "response".
func TestResponsesUsageNestedUnderResponse(t *testing.T) {
	u := ExtractUsageFromResponsesBody([]byte(nestedResponses))
	if u == nil {
		t.Fatal("nested usage not extracted")
	}
	if u.PromptTokens != 8 || u.CompletionTokens != 6 {
		t.Fatalf("usage = %d/%d, want 8/6", u.PromptTokens, u.CompletionTokens)
	}
}

func TestResponsesUsageGarbage(t *testing.T) {
	if u := ExtractUsageFromResponsesBody([]byte(`{"type":"response.output_text.delta"}`)); u != nil {
		t.Fatalf("non-usage event extracted %+v", u)
	}
	if u := ExtractUsageFromResponsesBody([]byte(`not json`)); u != nil {
		t.Fatalf("garbage extracted %+v", u)
	}
}
