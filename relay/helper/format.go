// Package helper implements response handling: passthrough with usage
// extraction and cross-format conversion between OpenAI/Claude/Gemini.
package helper

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/lxp/htokenmeter/relay/dto"
)

// Format identifies a wire protocol on either side of the gateway.
type Format string

const (
	FormatOpenAI    Format = "openai"
	FormatClaude    Format = "claude"
	FormatGemini    Format = "gemini"
	FormatResponses Format = "responses" // client-side only; upstream must be openai-compat
)

// RelayError is a normalized relay failure carrying the status and message;
// rendering into the client's error shape happens in WriteError.
type RelayError struct {
	StatusCode int
	Message    string
	ErrType    string // openai error type / claude error type
}

func Errorf(status int, format string, args ...any) *RelayError {
	return &RelayError{StatusCode: status, Message: fmt.Sprintf(format, args...), ErrType: "gateway_error"}
}

// RelayContext carries everything response handling needs.
type RelayContext struct {
	ClientFormat   Format
	UpstreamFormat Format
	Stream         bool
	ClientModel    string
	UpstreamModel  string
}

// UpstreamFormatFor maps a channel type to its native response format.
func UpstreamFormatFor(channelType int) Format {
	switch channelType {
	case 2:
		return FormatClaude
	case 3:
		return FormatGemini
	default: // openai-compat, azure
		return FormatOpenAI
	}
}

// ---------- usage extraction ----------

// openaiUsageDetails mirrors the fields we bill on.
type openaiUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (u openaiUsage) toUsage() *dto.Usage {
	out := &dto.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
	}
	if u.PromptTokensDetails != nil {
		out.CacheReadTokens = u.PromptTokensDetails.CachedTokens
	}
	return out
}

// extractUsageFromOpenAIBody pulls usage out of any OpenAI-shaped JSON.
func extractUsageFromOpenAIBody(body []byte) *dto.Usage {
	var probe struct {
		Usage *openaiUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Usage == nil {
		return nil
	}
	return probe.Usage.toUsage()
}

// responsesUsage matches the Responses API usage object.
type responsesUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

// toUsage keeps PromptTokens as the total input (cached included);
// cost math treats CacheReadTokens as a discounted subset.
func (u *responsesUsage) toUsage() *dto.Usage {
	if u == nil {
		return nil
	}
	out := &dto.Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens}
	if u.InputTokensDetails != nil {
		out.CacheReadTokens = u.InputTokensDetails.CachedTokens
	}
	if u.OutputTokensDetails != nil {
		out.CompletionTokens += u.OutputTokensDetails.ReasoningTokens
	}
	return out
}

// ExtractUsageFromResponsesBody pulls usage from a Responses API JSON body.
func ExtractUsageFromResponsesBody(body []byte) *dto.Usage {
	var probe struct {
		Usage *responsesUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Usage == nil {
		return nil
	}
	return probe.Usage.toUsage()
}

// writeNoStoreHeaders prevents caches from holding error/API bodies.
func writeNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}
