package adaptor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lxp/htokenmeter/relay/convert"
	"github.com/lxp/htokenmeter/relay/dto"
)

// OpenAICompat talks to any OpenAI-compatible /v1 endpoint
// (OpenAI, DeepSeek, Moonshot, Groq, Together, OpenRouter, SiliconFlow,
// Ollama, vLLM, ...). Default base: https://api.openai.com.
type OpenAICompat struct{}

func (a *OpenAICompat) Name() string { return "openai" }

func (a *OpenAICompat) base(m *Meta) string {
	if m.BaseURL != "" {
		return trimSlash(m.BaseURL)
	}
	return "https://api.openai.com"
}

func (a *OpenAICompat) ChatURL(m *Meta) string {
	return a.base(m) + "/v1/chat/completions"
}
func (a *OpenAICompat) EmbeddingURL(m *Meta) string { return a.base(m) + "/v1/embeddings" }
func (a *OpenAICompat) ImageURL(m *Meta) string     { return a.base(m) + "/v1/images/generations" }
func (a *OpenAICompat) EndpointURL(m *Meta, ep string) string {
	return a.base(m) + "/v1/" + ep
}

func (a *OpenAICompat) SetupHeaders(h http.Header, m *Meta) {
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer "+m.APIKey)
}

func (a *OpenAICompat) ConvertChat(req *dto.ChatRequest, m *Meta) ([]byte, error) {
	// stream_options.include_usage so billing can use upstream usage.
	if req.Stream && req.StreamOptions == nil {
		req.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	}
	return json.Marshal(req)
}

func (a *OpenAICompat) ConvertEmbedding(req *dto.EmbeddingRequest, m *Meta) ([]byte, error) {
	return json.Marshal(req)
}

func (a *OpenAICompat) ConvertImage(req *dto.ImageRequest, m *Meta) ([]byte, error) {
	if req.N == 0 {
		req.N = 1
	}
	if req.Size == "" {
		req.Size = "1024x1024"
	}
	return json.Marshal(req)
}

// ---------- Anthropic ----------

type Anthropic struct{}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) base(m *Meta) string {
	if m.BaseURL != "" {
		return trimSlash(m.BaseURL)
	}
	return "https://api.anthropic.com"
}

func (a *Anthropic) ChatURL(m *Meta) string      { return a.base(m) + "/v1/messages" }
func (a *Anthropic) EmbeddingURL(m *Meta) string { return "" } // no embeddings API
func (a *Anthropic) ImageURL(m *Meta) string     { return "" }

func (a *Anthropic) SetupHeaders(h http.Header, m *Meta) {
	h.Set("Content-Type", "application/json")
	h.Set("x-api-key", m.APIKey)
	h.Set("anthropic-version", "2023-06-01")
}

func (a *Anthropic) ConvertChat(req *dto.ChatRequest, m *Meta) ([]byte, error) {
	creq, err := convert.OpenAIToClaude(req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(creq)
}

func (a *Anthropic) ConvertEmbedding(req *dto.EmbeddingRequest, m *Meta) ([]byte, error) {
	return nil, fmt.Errorf("anthropic channel does not support embeddings")
}

func (a *Anthropic) ConvertImage(req *dto.ImageRequest, m *Meta) ([]byte, error) {
	return nil, fmt.Errorf("anthropic channel does not support image generation")
}

// ---------- Gemini ----------

type Gemini struct{}

func (g *Gemini) Name() string { return "gemini" }

func (g *Gemini) base(m *Meta) string {
	if m.BaseURL != "" {
		return trimSlash(m.BaseURL)
	}
	return "https://generativelanguage.googleapis.com"
}

// chatURL builds generateContent or streamGenerateContent (alt=sse).
// model must be the upstream (mapped) name.
func (g *Gemini) chatURL(m *Meta, model, method string) string {
	streamQ := ""
	if method == "streamGenerateContent" {
		streamQ = "?alt=sse"
	}
	return fmt.Sprintf("%s/v1beta/models/%s:%s%s", g.base(m), model, method, streamQ)
}

func (g *Gemini) ChatURLStream(m *Meta, model string) string {
	return g.chatURL(m, model, "streamGenerateContent")
}

// The generic interface uses non-stream URL; the relay controller calls
// ChatURLStream for streams when the channel is Gemini.
func (g *Gemini) ChatURL(m *Meta) string { return g.chatURL(m, m.UpstreamModel, "generateContent") }
func (g *Gemini) EmbeddingURLModel(m *Meta, model string) string {
	return g.chatURL(m, model, "embedContent")
}
func (g *Gemini) EmbeddingURL(m *Meta) string { return g.EmbeddingURLModel(m, m.UpstreamModel) }
func (g *Gemini) ImageURL(m *Meta) string {
	return fmt.Sprintf("%s/v1beta/models/%s:predict", g.base(m), m.UpstreamModel)
}

func (g *Gemini) SetupHeaders(h http.Header, m *Meta) {
	h.Set("Content-Type", "application/json")
	h.Set("x-goog-api-key", m.APIKey)
}

func (g *Gemini) ConvertChat(req *dto.ChatRequest, m *Meta) ([]byte, error) {
	body := convert.OpenAIToGemini(req)
	if req.Stream {
		body["stream"] = true // informational; URL decides
	}
	return json.Marshal(body)
}

func (g *Gemini) ConvertEmbedding(req *dto.EmbeddingRequest, m *Meta) ([]byte, error) {
	var input string
	_ = json.Unmarshal(req.Input, &input)
	body := map[string]any{"content": map[string]any{"parts": []map[string]any{{"text": input}}}}
	return json.Marshal(body)
}

func (g *Gemini) ConvertImage(req *dto.ImageRequest, m *Meta) ([]byte, error) {
	return nil, fmt.Errorf("gemini image generation is not supported on this channel type; use an openai-compat channel")
}

// ---------- Azure OpenAI ----------

type Azure struct{ OpenAICompat }

func (a *Azure) Name() string { return "azure" }

func (a *Azure) apiVersion(m *Meta) string {
	if m.APIVersion != "" {
		return m.APIVersion
	}
	return "2024-10-21"
}

func (a *Azure) deployURL(m *Meta, model, task string) string {
	dots := strings.ReplaceAll(model, ".", "")
	return fmt.Sprintf("%s/openai/deployments/%s/%s?api-version=%s",
		trimSlash(m.BaseURL), dots, task, a.apiVersion(m))
}

func (a *Azure) ChatURL(m *Meta) string      { return a.deployURL(m, m.UpstreamModel, "chat/completions") }
func (a *Azure) EmbeddingURL(m *Meta) string { return a.deployURL(m, m.UpstreamModel, "embeddings") }
func (a *Azure) ImageURL(m *Meta) string {
	return a.deployURL(m, m.UpstreamModel, "images/generations")
}
func (a *Azure) EndpointURL(m *Meta, ep string) string {
	return a.deployURL(m, m.UpstreamModel, ep)
}

func (a *Azure) SetupHeaders(h http.Header, m *Meta) {
	h.Set("Content-Type", "application/json")
	h.Set("api-key", m.APIKey)
}
