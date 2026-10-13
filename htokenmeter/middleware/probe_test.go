package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// F018: bodies larger than 4MiB (e.g. base64 vision payloads) must still
// yield their model via the prefix scan instead of becoming unroutable.
func TestProbeRequestModelLargeBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	big := make([]byte, 4<<20+4096)
	copy(big, []byte(`{"model":"vision-model","messages":[{"role":"user","content":"`))
	for i := len(`{"model":"vision-model","messages":[{"role":"user","content":"`); i < len(big); i++ {
		big[i] = 'a'
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(big))
	c.Request.Header.Set("Content-Type", "application/json")

	if m := probeRequestModel(c); m != "vision-model" {
		t.Fatalf("model = %q, want vision-model (4MiB prefix scan)", m)
	}
}
