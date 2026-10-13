package main_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lxp/htokenmeter/model"
)

// TestOpenAIPassthroughChat: openai client -> openai channel, non-stream.
func TestOpenAIPassthroughChat(t *testing.T) {
	h := setup(t)
	resp, body := h.relay("POST", "/v1/chat/completions",
		`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Content != "Hello!" {
		t.Fatalf("unexpected content: %s", body)
	}
	waitForBilling(t)
	assertBilled(t, "test-model", 10, 5)
}

// TestOpenAIStream: openai client -> openai channel, streaming SSE.
func TestOpenAIStream(t *testing.T) {
	h := setup(t)
	resp, body := h.relay("POST", "/v1/chat/completions",
		`{"model":"test-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content type %s", ct)
	}
	if !strings.Contains(body, "Hel") || !strings.Contains(body, "lo!") || !strings.Contains(body, "[DONE]") {
		t.Fatalf("stream chunks missing: %s", body)
	}
	waitForBilling(t)
	assertBilled(t, "test-model", 5, 2)
}

// TestClaudeClientToOpenAIUpstream: claude SDK shape, openai-compat channel
// (model mapping steers it to the openai mock).
func TestClaudeClientToOpenAIUpstream(t *testing.T) {
	h := setup(t)
	// map claude-alias -> test-model on the openai channel
	ch, _ := model.GetChannelById(1)
	ch.Models = "test-model,gpt-x,claude-alias"
	ch.ModelMapping = `{"claude-alias":"test-model"}`
	if err := ch.Update(); err != nil {
		t.Fatal(err)
	}
	resp, body := h.relay("POST", "/v1/messages",
		`{"model":"claude-alias","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`,
		map[string]string{"x-api-key": h.token, "anthropic-version": "2023-06-01"})
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Content) == 0 || out.Content[0].Text != "Hello!" {
		t.Fatalf("unexpected claude-shaped content: %s", body)
	}
	waitForBilling(t)
	assertBilled(t, "claude-alias", 10, 5)
}

// TestClaudeNativePassthrough: claude client -> anthropic channel.
func TestClaudeNativePassthrough(t *testing.T) {
	h := setup(t)
	resp, body := h.relay("POST", "/v1/messages",
		`{"model":"claude-test","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Content) == 0 || out.Content[0].Text != "Bonjour!" {
		t.Fatalf("unexpected content: %s", body)
	}
	waitForBilling(t)
	// claude-test: $3/1M in, $15/1M out, 7 in 3 out => 21+45 = 66 micro-USD
	assertBilledCost(t, "claude-test", 66)
}

// TestOpenAIToClaudeUpstream: openai client -> anthropic channel via mapping.
func TestOpenAIToClaudeUpstream(t *testing.T) {
	h := setup(t)
	ch, _ := model.GetChannelById(2)
	ch.Models = "claude-test,oai-alias"
	ch.ModelMapping = `{"oai-alias":"claude-test"}`
	if err := ch.Update(); err != nil {
		t.Fatal(err)
	}
	resp, body := h.relay("POST", "/v1/chat/completions",
		`{"model":"oai-alias","messages":[{"role":"system","content":"be nice"},{"role":"user","content":"hi"}]}`, nil)
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content != "Bonjour!" {
		t.Fatalf("unexpected content: %s", body)
	}
}

// TestGeminiNative: gemini client -> gemini channel.
func TestGeminiNative(t *testing.T) {
	h := setup(t)
	resp, body := h.relay("POST", "/v1beta/models/gemini-test:generateContent",
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
		map[string]string{"x-goog-api-key": h.token})
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	if !strings.Contains(body, "Ciao!") {
		t.Fatalf("unexpected content: %s", body)
	}
	waitForBilling(t)
}

// TestGeminiClientOpenAIUpstream: gemini client -> openai-compat channel.
func TestGeminiClientOpenAIUpstream(t *testing.T) {
	h := setup(t)
	resp, body := h.relay("POST", "/v1beta/models/test-model:generateContent",
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, nil)
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v body %s", resp, body)
	}
	if !strings.Contains(body, "Hello!") {
		t.Fatalf("unexpected content: %s", body)
	}
}

// TestModelsList: /v1/models aggregates group models.
func TestModelsList(t *testing.T) {
	h := setup(t)
	resp, body := h.relay("GET", "/v1/models", "", nil)
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("status %v", resp)
	}
	for _, want := range []string{"test-model", "claude-test", "gemini-test"} {
		if !strings.Contains(body, want) {
			t.Fatalf("models missing %s: %s", want, body)
		}
	}
}

// TestTokenModelWhitelist: token restricted to one model.
func TestTokenModelWhitelist(t *testing.T) {
	h := setup(t)
	req, _ := httpNew("POST", h.srv.URL+"/api/token",
		`{"name":"restricted","models":"test-model"}`)
	req.Header.Set("Cookie", "htokenmeter_session="+h.cookie)
	resp, err := httpDefault().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Data model.Token `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	saved := h.token
	h.token = out.Data.Key
	r, b := h.relay("POST", "/v1/chat/completions",
		`{"model":"claude-test","messages":[{"role":"user","content":"hi"}]}`, nil)
	if r == nil || r.StatusCode != 403 {
		t.Fatalf("expected 403, got %v %s", r, b)
	}
	h.token = saved
}

// TestDeleteChannelClearsAbilities: the one-api consistency bug must not exist.
func TestDeleteChannelClearsAbilities(t *testing.T) {
	h := setup(t)
	req, _ := httpNew("DELETE", h.srv.URL+"/api/channel/2", "")
	req.Header.Set("Cookie", "htokenmeter_session="+h.cookie)
	resp, err := httpDefault().Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("delete failed: %v %v", err, resp)
	}
	resp.Body.Close()
	var count int64
	model.DB.Model(&model.Ability{}).Where("channel_id = 2").Count(&count)
	if count != 0 {
		t.Fatalf("orphan abilities: %d", count)
	}
}

// ---------- helpers ----------

func waitForBilling(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var logs []model.Log
		model.DB.Where("cost_micro_usd > 0 OR prompt_tokens > 0").Find(&logs)
		if len(logs) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no billing log appeared")
}

func assertBilled(t *testing.T, modelName string, prompt, completion int) {
	t.Helper()
	var logs []model.Log
	model.DB.Where("model_name = ?", modelName).Find(&logs)
	if len(logs) == 0 {
		t.Fatalf("no log for %s", modelName)
	}
	if logs[0].PromptTokens != prompt || logs[0].CompletionTokens != completion {
		t.Fatalf("usage mismatch: got %d/%d want %d/%d",
			logs[0].PromptTokens, logs[0].CompletionTokens, prompt, completion)
	}
}

func assertBilledCost(t *testing.T, modelName string, costMicro int64) {
	t.Helper()
	var logs []model.Log
	model.DB.Where("model_name = ?", modelName).Find(&logs)
	if len(logs) == 0 {
		t.Fatalf("no log for %s", modelName)
	}
	if logs[0].CostMicroUsd != costMicro {
		t.Fatalf("cost mismatch: got %d want %d", logs[0].CostMicroUsd, costMicro)
	}
}

func httpNew(method, url, body string) (*http.Request, error) {
	return http.NewRequest(method, url, strings.NewReader(body))
}

func httpDefault() *http.Client { return &http.Client{} }
