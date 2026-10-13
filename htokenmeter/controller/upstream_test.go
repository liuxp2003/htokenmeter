package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/common"
	db "github.com/lxp/htokenmeter/model"
	"github.com/lxp/htokenmeter/relay/adaptor"
)

func ginTestCtx() *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

// slowBodyServer writes headers immediately, then emits the body after delay.
func slowBodyServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(delay)
		_, _ = w.Write([]byte("done"))
	}))
}

// F017: streaming requests cap only time-to-headers — a body lasting longer
// than RelayTimeout must still arrive whole.
func TestDoUpstreamStreamSurvivesBodyLongerThanDeadline(t *testing.T) {
	common.RelayTimeout = 1
	srv := slowBodyServer(t, 1400*time.Millisecond)
	t.Cleanup(srv.Close)

	ad, err := adaptor.Get(db.TypeOpenAICompat)
	if err != nil {
		t.Fatal(err)
	}
	meta := &adaptor.Meta{BaseURL: srv.URL, APIKey: "k"}
	resp, err := doUpstreamRequest(ginTestCtx(), ad, meta, http.MethodPost, srv.URL, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream body cut mid-flight: %v", err)
	}
	if string(b) != "done" {
		t.Fatalf("body = %q, want done", b)
	}
}

// F017: non-streaming requests keep a whole-exchange deadline.
func TestDoUpstreamNonStreamCappedByDeadline(t *testing.T) {
	common.RelayTimeout = 1
	srv := slowBodyServer(t, 2500*time.Millisecond)
	t.Cleanup(srv.Close)

	ad, err := adaptor.Get(db.TypeOpenAICompat)
	if err != nil {
		t.Fatal(err)
	}
	meta := &adaptor.Meta{BaseURL: srv.URL, APIKey: "k"}
	start := time.Now()
	resp, err := doUpstreamRequest(ginTestCtx(), ad, meta, http.MethodPost, srv.URL, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("non-stream body should have hit the deadline")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("deadline not enforced, took %v", elapsed)
	}
}

// F027: retries exclude channels that already failed this request.
func TestPickChannelExcludesTried(t *testing.T) {
	dir := t.TempDir()
	common.ConfigDir = dir
	common.DBPath = dir + "/t.db"
	if err := db.InitDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB.DB(); err == nil {
			sqlDB.Close()
		}
	})

	for _, name := range []string{"a", "b"} {
		ch := db.Channel{Type: db.TypeOpenAICompat, Name: name, BaseURL: "http://x",
			Keys: "sk", Models: "m", Group: "default", Status: db.ChannelStatusEnabled}
		if err := ch.Insert(); err != nil {
			t.Fatal(err)
		}
	}

	// Both channels serve (group, model); excluding one must always yield the other.
	for i := 0; i < 20; i++ {
		ch, err := db.GetRandomSatisfiedChannel("default", "m", 0, map[int]bool{1: true})
		if err != nil {
			t.Fatal(err)
		}
		if ch.Id != 2 {
			t.Fatalf("pick %d: channel %d, want 2 (channel 1 excluded)", i, ch.Id)
		}
	}
	// Excluding everything is a hard no.
	if _, err := db.GetRandomSatisfiedChannel("default", "m", 0, map[int]bool{1: true, 2: true}); err == nil {
		t.Fatal("expected error when all candidates are excluded")
	}
}
