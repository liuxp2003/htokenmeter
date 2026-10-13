package main_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lxp/htokenmeter/model"
)

// pathCaptureMock records the upstream path of every request and answers a
// minimal OpenAI-shaped body.
func pathCaptureMock(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"x","results":[{"flagged":false}],"usage":{"prompt_tokens":3,"completion_tokens":0}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), paths...)
	}
}

func insertLegacyChannel(t *testing.T, baseURL, models string) {
	t.Helper()
	ch := model.Channel{Type: model.TypeOpenAICompat, Name: "legacy", BaseURL: baseURL,
		Keys: "sk-upstream", Models: models, Group: "default", Status: model.ChannelStatusEnabled}
	if err := ch.Insert(); err != nil {
		t.Fatal(err)
	}
}

// F007: /v1/completions must hit the upstream legacy-completions path,
// not /v1/chat/completions.
func TestLegacyCompletionsHitsOwnEndpoint(t *testing.T) {
	h := setup(t)
	srv, paths := pathCaptureMock(t)
	insertLegacyChannel(t, srv.URL, "legacy-model")

	resp, body := h.relay(http.MethodPost, "/v1/completions",
		`{"model":"legacy-model","prompt":"hello"}`, nil)
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	got := paths()
	if len(got) == 0 || got[0] != "/v1/completions" {
		t.Fatalf("upstream paths = %v, want [/v1/completions]", got)
	}
}

// F007: /v1/moderations hits its own endpoint and a missing model defaults
// to text-moderation-latest.
func TestModerationsEndpointAndDefaultModel(t *testing.T) {
	h := setup(t)
	srv, paths := pathCaptureMock(t)
	insertLegacyChannel(t, srv.URL, "text-moderation-latest")

	resp, body := h.relay(http.MethodPost, "/v1/moderations", `{"input":"check me"}`, nil)
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	got := paths()
	if len(got) == 0 || got[0] != "/v1/moderations" {
		t.Fatalf("upstream paths = %v, want [/v1/moderations]", got)
	}
}

// F007: legacy endpoints through a channel with model mapping must fail
// loudly instead of silently posting the legacy body to chat/completions.
func TestLegacyEndpointWithMappingRejected(t *testing.T) {
	h := setup(t)
	ch := model.Channel{Type: model.TypeOpenAICompat, Name: "mapped", BaseURL: h.srv.URL,
		Keys: "sk-upstream", Models: "legacy-mapped", Group: "default", Status: model.ChannelStatusEnabled,
		ModelMapping: `{"legacy-mapped":"gpt-x"}`}
	if err := ch.Insert(); err != nil {
		t.Fatal(err)
	}

	resp, body := h.relay(http.MethodPost, "/v1/completions",
		`{"model":"legacy-mapped","prompt":"hello"}`, nil)
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %v body %s", resp, body)
	}
}
