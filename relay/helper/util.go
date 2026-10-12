package helper

import (
	"net/http"
	"time"

	"github.com/lxp/htokenmeter/relay/dto"
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

// PriceSource resolves unit prices for billing.
type PriceSource interface {
	PriceFor(model string) (prompt, completion, cacheRead float64, ok bool)
}

// CostMicroUsd applies prices to usage. cacheRead is a discounted subset of
// prompt tokens, so billable full-rate input = prompt - cacheRead.
func CostMicroUsd(ps PriceSource, model string, u *dto.Usage) int64 {
	if u == nil {
		return 0
	}
	prompt, completion, cacheRead, _ := ps.PriceFor(model)
	fullRateInput := u.PromptTokens - u.CacheReadTokens
	if fullRateInput < 0 {
		fullRateInput = 0
	}
	cost := prompt*float64(fullRateInput)/1e6 +
		cacheRead*float64(u.CacheReadTokens)/1e6 +
		completion*float64(u.CompletionTokens)/1e6
	if cost >= 0 {
		return int64(cost*1e6 + 0.5)
	}
	return int64(cost*1e6 - 0.5)
}
