package dto

import (
	"encoding/json"
	"testing"
)

// F025: unknown OpenAI fields survive the canonical struct round trip.
func TestChatRequestExtraRoundTrip(t *testing.T) {
	in := `{"model":"m","messages":[{"role":"user","content":"hi"}],
		"n":3,"frequency_penalty":0.5,"presence_penalty":0.1,"seed":42,
		"logit_bias":{"50256":-100},"parallel_tool_calls":true,"temperature":0.7}`
	var r ChatRequest
	if err := json.Unmarshal([]byte(in), &r); err != nil {
		t.Fatal(err)
	}
	if r.Model != "m" || len(r.Messages) != 1 {
		t.Fatalf("known fields lost: %+v", r)
	}
	for _, k := range []string{"n", "frequency_penalty", "presence_penalty", "seed", "logit_bias", "parallel_tool_calls"} {
		if _, ok := r.Extra[k]; !ok {
			t.Fatalf("Extra missing %q: %+v", k, r.Extra)
		}
	}
	out, err := json.Marshal(&r)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		N                int    `json:"n"`
		Seed             int    `json:"seed"`
		FrequencyPenalty float64 `json:"frequency_penalty"`
		ParallelTool     bool   `json:"parallel_tool_calls"`
		Temperature      float64 `json:"temperature"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.N != 3 || probe.Seed != 42 || probe.FrequencyPenalty != 0.5 || !probe.ParallelTool {
		t.Fatalf("round trip lost extras: %s", out)
	}
	if probe.Temperature != 0.7 {
		t.Fatalf("typed field lost: %s", out)
	}
}

// Known fields must not leak into Extra.
func TestChatRequestExtraExcludesKnown(t *testing.T) {
	var r ChatRequest
	if err := json.Unmarshal([]byte(`{"model":"m","stream":true,"messages":[]}`), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Extra) != 0 {
		t.Fatalf("Extra = %v, want empty", r.Extra)
	}
}
