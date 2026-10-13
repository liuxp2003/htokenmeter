package dto

import "encoding/json"

// ---------- Gemini /v1beta wire format ----------

type GeminiRequest struct {
	Contents          []GeminiContent   `json:"contents"`
	SystemInstruction *GeminiContent    `json:"systemInstruction,omitempty"`
	GenerationConfig  *GeminiGenConfig  `json:"generationConfig,omitempty"`
	SafetySettings    []json.RawMessage `json:"-"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"` // user | model (omitted for system)
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text       string         `json:"text,omitempty"`
	InlineData *GeminiInline  `json:"inlineData,omitempty"`
	Raw        map[string]any `json:"-"`
}

// UnmarshalJSON captures non-text/inline fields (functionCall,
// functionResponse, ...) into Raw so tool traffic is not silently dropped.
func (p *GeminiPart) UnmarshalJSON(data []byte) error {
	type alias GeminiPart
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = GeminiPart(a)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err == nil {
		delete(m, "text")
		delete(m, "inlineData")
		if len(m) > 0 {
			p.Raw = m
		}
	}
	return nil
}

// MarshalJSON re-emits text/inline plus whatever Raw captured.
func (p GeminiPart) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, len(p.Raw)+2)
	for k, v := range p.Raw {
		m[k] = v
	}
	if p.Text != "" {
		m["text"] = p.Text
	}
	if p.InlineData != nil {
		m["inlineData"] = p.InlineData
	}
	return json.Marshal(m)
}

type GeminiInline struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiGenConfig struct {
	MaxOutputTokens  int      `json:"maxOutputTokens,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"topP,omitempty"`
	StopSequences    []string `json:"stopSequences,omitempty"`
	ResponseMIMEType string   `json:"responseMimeType,omitempty"`
}

type GeminiResponse struct {
	Candidates    []GeminiCandidate `json:"candidates"`
	UsageMetadata *GeminiUsage      `json:"usageMetadata"`
	ModelVersion  string            `json:"modelVersion"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
}

type GeminiUsage struct {
	PromptTokenCount        int64 `json:"promptTokenCount"`
	CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
	TotalTokenCount         int64 `json:"totalTokenCount"`
	CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
}

// ---------- Unified usage (canonical cost input) ----------

// Usage is the internal normalized usage extracted from any upstream format.
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	Images           int   `json:"images"`
	// PromptIncludesCache reports whether PromptTokens already counts the
	// cache-read tokens (OpenAI/Gemini/Responses semantics). Claude reports
	// cached tokens as disjoint fields, so its extractors leave this false
	// and the full-rate input is the whole PromptTokens.
	PromptIncludesCache bool `json:"-"`
}

// EstimateTokens gives a rough token count for pre-flight estimates:
// ~1 token per 4 bytes of ASCII-ish text, CJK ~1 per 1.5 bytes.
func EstimateTokens(s string) int64 {
	if s == "" {
		return 0
	}
	cjk := 0
	for _, r := range s {
		if r > 0x2E80 {
			cjk++
		}
	}
	rest := len(s) - cjk*3 // rough utf-8 size subtraction
	t := int64(rest)/4 + int64(cjk)*2/3
	if t < 1 {
		t = 1
	}
	return t
}
