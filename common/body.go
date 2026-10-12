package common

import (
	"bytes"
	"io"

	"github.com/gin-gonic/gin"
)

const bodyCtxKey = "htokenmeter_raw_body"

// PeekBody reads the request body once and caches it in the context;
// later readers (conversion, forwarding) use BodyFromContext.
func PeekBody(c *gin.Context) []byte {
	if v, ok := c.Get(bodyCtxKey); ok {
		return v.([]byte)
	}
	if c.Request.Body == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 64<<20))
	if err != nil {
		body = nil
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Set(bodyCtxKey, body)
	return body
}

// BodyFromContext returns the cached body (nil when absent).
func BodyFromContext(c *gin.Context) []byte {
	if v, ok := c.Get(bodyCtxKey); ok {
		return v.([]byte)
	}
	return PeekBody(c)
}

// ExtractJSONStringField scans for a top-level-ish "field":"value" without a
// full unmarshal; good enough for model routing on well-formed client JSON.
func ExtractJSONStringField(body []byte, field string) string {
	needle := []byte("\"" + field + "\"")
	idx := bytes.Index(body, needle)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(needle):]
	// skip whitespace and colon
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == ':' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	var out []byte
	for i := 0; i < len(rest); i++ {
		ch := rest[i]
		if ch == '\\' && i+1 < len(rest) {
			out = append(out, rest[i+1])
			i++
			continue
		}
		if ch == '"' {
			break
		}
		out = append(out, ch)
	}
	return string(out)
}
