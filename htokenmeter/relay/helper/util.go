package helper

import (
	"net/http"
	"time"
)

func nowUnix() int64     { return time.Now().Unix() }
func nowUnixNano() int64 { return time.Now().UnixNano() }

// WriteError renders an error in the client's protocol error shape.
func WriteError(w http.ResponseWriter, format Format, e *RelayError) {
	writeNoStoreHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.StatusCode)
	switch format {
	case FormatClaude:
		typ := "api_error"
		switch {
		case e.StatusCode == http.StatusUnauthorized:
			typ = "authentication_error"
		case e.StatusCode == http.StatusTooManyRequests:
			typ = "rate_limit_error"
		case e.StatusCode >= 400 && e.StatusCode < 500:
			typ = "invalid_request_error"
		}
		_ = writeJSON(w, map[string]any{
			"type":  "error",
			"error": map[string]any{"type": typ, "message": e.Message},
		})
	case FormatGemini:
		_ = writeJSON(w, map[string]any{
			"error": map[string]any{
				"code": e.StatusCode, "message": e.Message, "status": http.StatusText(e.StatusCode),
			},
		})
	default: // openai / responses
		_ = writeJSON(w, map[string]any{
			"error": map[string]any{
				"message": e.Message, "type": e.ErrType, "param": "", "code": nil,
			},
		})
	}
}

func writeJSON(w http.ResponseWriter, v any) error {
	b := mustMarshal(v)
	_, err := w.Write(b)
	return err
}

// ---------- pricing ----------

// (Billing math lives in model.ModelPrice.CostMicroUsd; keep a single
// implementation so the formula cannot drift between call sites.)
