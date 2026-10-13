package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/common"
	"github.com/lxp/htokenmeter/model"
	"github.com/lxp/htokenmeter/router"
)

// ---------- mock upstreams ----------

func mockOpenAI(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-upstream" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "bad key"}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if req["stream"] == true {
				w.Header().Set("Content-Type", "text/event-stream")
				fl := w.(http.Flusher)
				for _, chunk := range []string{
					`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"},"finish_reason":null}]}`,
					`data: {"id":"1","choices":[{"index":0,"delta":{"content":"lo!"},"finish_reason":null}]}`,
					`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
					`data: {"id":"1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
					`data: [DONE]`,
				} {
					fmt.Fprintf(w, "%s\n\n", chunk)
					fl.Flush()
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-1", "object": "chat.completion",
				"choices": []any{map[string]any{
					"index": 0, "finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": "Hello!"},
				}},
				"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/v1/models") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []any{map[string]any{"id": "test-model", "object": "model"}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func mockAnthropic(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-claude" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant",
			"content":     []any{map[string]any{"type": "text", "text": "Bonjour!"}},
			"model":       "claude-test",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 7, "output_tokens": 3},
		})
	}))
}

func mockGemini(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "sk-gem" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Ciao!"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{
				"promptTokenCount": 4, "candidatesTokenCount": 2, "totalTokenCount": 6,
			},
		})
	}))
}

// ---------- harness ----------

type harness struct {
	engine *gin.Engine
	srv    *httptest.Server
	token  string
	cookie string
}

func setup(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	model.AsyncWG.Wait() // let prior tests' async accounting settle
	dir := t.TempDir()
	common.ConfigDir = dir
	common.DBPath = dir + "/test.db"
	common.RetryTimes = 0
	if err := model.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if db, err := model.DB.DB(); err == nil {
			db.Close()
		}
	})
	// capture generated admin password from the seeded admin? We must reset it.
	// Simplest: overwrite admin password to a known value.
	u, err := model.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("admin missing: %v", err)
	}
	hash, _ := model.HashPassword("test-password-1")
	_ = u.UpdatePassword(hash)

	openai := mockOpenAI(t)
	claude := mockAnthropic(t)
	gemini := mockGemini(t)
	t.Cleanup(openai.Close)
	t.Cleanup(claude.Close)
	t.Cleanup(gemini.Close)

	for _, ch := range []model.Channel{
		{Type: model.TypeOpenAICompat, Name: "oai", BaseURL: openai.URL, Keys: "sk-upstream",
			Models: "test-model,gpt-x", Group: "default", Priority: 0, Weight: 0, Status: model.ChannelStatusEnabled},
		{Type: model.TypeAnthropic, Name: "claude", BaseURL: claude.URL, Keys: "sk-claude",
			Models: "claude-test", Group: "default", Priority: 0, Weight: 0, Status: model.ChannelStatusEnabled},
		{Type: model.TypeGemini, Name: "gem", BaseURL: gemini.URL, Keys: "sk-gem",
			Models: "gemini-test", Group: "default", Priority: 0, Weight: 0, Status: model.ChannelStatusEnabled},
	} {
		if err := ch.Insert(); err != nil {
			t.Fatalf("channel insert: %v", err)
		}
	}

	// price rows for billing assertions
	if err := model.DB.Save(&model.ModelPrice{Model: "test-model", PromptUsd: 1, CompletionUsd: 2, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Save(&model.ModelPrice{Model: "claude-test", PromptUsd: 3, CompletionUsd: 15, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}

	engine := gin.New()
	router.SetRouter(engine, nil)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	// login + create token via API
	h := &harness{engine: engine, srv: srv}
	h.login(t)
	h.token = h.createToken(t, "e2e")
	return h
}

func (h *harness) login(t *testing.T) {
	t.Helper()
	resp, err := http.Post(h.srv.URL+"/api/user/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"test-password-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "htokenmeter_session" {
			h.cookie = c.Value
		}
	}
	if h.cookie == "" {
		t.Fatal("no session cookie")
	}
}

func (h *harness) createToken(t *testing.T, name string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/api/token", strings.NewReader(`{"name":"`+name+`"}`))
	req.Header.Set("Cookie", "htokenmeter_session="+h.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data model.Token `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Data.Key == "" {
		t.Fatal("token key empty")
	}
	return out.Data.Key
}

func (h *harness) relay(method, path string, body string, hdr map[string]string) (*http.Response, string) {
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.token)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err.Error()
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

var _ = os.Setenv
var _ = bytes.MinRead
