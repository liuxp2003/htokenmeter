// Package adaptor defines the upstream channel abstraction: URL building,
// auth headers, and canonical→native request conversion per channel type.
package adaptor

import (
	"fmt"
	"net/http"

	"github.com/lxp/htokenmeter/relay/dto"
)

// Meta carries per-request channel context into adaptors.
type Meta struct {
	ChannelType   int
	BaseURL       string
	APIKey        string
	KeyIndex      int
	APIVersion    string // azure
	UpstreamModel string // model name after channel mapping
	ModelMapping  map[string]string
	Proxy         string
}

// Adaptor converts canonical requests into one upstream format.
type Adaptor interface {
	Name() string
	ChatURL(m *Meta) string
	EmbeddingURL(m *Meta) string
	ImageURL(m *Meta) string
	SetupHeaders(h http.Header, m *Meta)
	ConvertChat(req *dto.ChatRequest, m *Meta) ([]byte, error)
	ConvertEmbedding(req *dto.EmbeddingRequest, m *Meta) ([]byte, error)
	ConvertImage(req *dto.ImageRequest, m *Meta) ([]byte, error)
}

// Get returns the adaptor for a channel type.
func Get(channelType int) (Adaptor, error) {
	switch channelType {
	case 1:
		return &OpenAICompat{}, nil
	case 2:
		return &Anthropic{}, nil
	case 3:
		return &Gemini{}, nil
	case 4:
		return &Azure{}, nil
	}
	return nil, fmt.Errorf("unsupported channel type %d", channelType)
}

// EndpointRouter is implemented by adaptors that can serve non-chat
// endpoints (legacy "completions", "moderations") at their native upstream
// path. Adaptors without it cannot serve those endpoints.
type EndpointRouter interface {
	EndpointURL(m *Meta, endpoint string) string
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
