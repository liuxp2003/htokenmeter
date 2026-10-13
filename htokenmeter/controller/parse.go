package controller

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/common"
	"github.com/lxp/htokenmeter/relay/convert"
	"github.com/lxp/htokenmeter/relay/dto"
	"github.com/lxp/htokenmeter/relay/helper"
)

// parseRelayRequest reads the body once and builds the relay task
// (canonical chat, typed embeddings/images, or raw passthrough payload).
func parseRelayRequest(c *gin.Context, format helper.Format) (*relayTask, *helper.RelayError) {
	body := common.PeekBody(c)
	if len(body) == 0 && c.Request.Method == http.MethodPost {
		return nil, helper.Errorf(http.StatusBadRequest, "empty request body")
	}
	task := &relayTask{ClientFormat: format, rawBody: body}
	path := c.Request.URL.Path

	switch {
	case path == "/v1/embeddings" || strings.HasSuffix(path, "/embeddings"):
		var req dto.EmbeddingRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, helper.Errorf(http.StatusBadRequest, "invalid embeddings request: %v", err)
		}
		task.embedding = &req
		task.ClientModel = req.Model

	case path == "/v1/images/generations":
		var req dto.ImageRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, helper.Errorf(http.StatusBadRequest, "invalid image request: %v", err)
		}
		task.image = &req
		task.ClientModel = req.Model

	case format == helper.FormatResponses:
		if c.Request.Method != http.MethodPost {
			// GET /v1/responses/:id has no body; route to any openai channel.
			task.ClientModel = ""
			return task, nil
		}
		var probe struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &probe); err != nil || probe.Model == "" {
			return nil, helper.Errorf(http.StatusBadRequest, "responses request needs a model field")
		}
		task.ClientModel = probe.Model
		task.Stream = strings.Contains(string(body), `"stream":true`) || c.Query("stream") == "true"

	case format == helper.FormatClaude:
		var creq dto.ClaudeRequest
		if err := json.Unmarshal(body, &creq); err != nil {
			return nil, helper.Errorf(http.StatusBadRequest, "invalid claude request: %v", err)
		}
		task.ClientModel = creq.Model
		task.Stream = creq.Stream
		task.chat = convert.ClaudeToOpenAI(&creq)

	case format == helper.FormatGemini:
		model := geminiPathModel(path)
		var greq dto.GeminiRequest
		if err := json.Unmarshal(body, &greq); err != nil {
			return nil, helper.Errorf(http.StatusBadRequest, "invalid gemini request: %v", err)
		}
		task.ClientModel = model
		task.Stream = strings.Contains(path, ":streamGenerateContent")
		task.chat = convert.GeminiToOpenAI(model, &greq)

	default: // openai chat family
		var oreq dto.ChatRequest
		if err := json.Unmarshal(body, &oreq); err != nil {
			return nil, helper.Errorf(http.StatusBadRequest, "invalid request: %v", err)
		}
		if oreq.Model == "" && path == "/v1/moderations" {
			// OpenAI makes the model optional for moderations; mirror the
			// documented default (Distribute applies the same for routing).
			oreq.Model = common.DefaultModerationModel
		}
		if oreq.Model == "" {
			return nil, helper.Errorf(http.StatusBadRequest, "model is required")
		}
		task.ClientModel = oreq.Model
		task.Stream = oreq.Stream
		task.chat = &oreq
	}

	if task.ClientModel == "" {
		return nil, helper.Errorf(http.StatusBadRequest, "model is required")
	}
	c.Set("request_model", task.ClientModel)
	return task, nil
}

// geminiPathModel extracts the model from /v1beta/models/{model}:{action}.
func geminiPathModel(path string) string {
	const prefix = "/v1beta/models/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimPrefix(rest, "v1beta/models/")
	if i := strings.Index(rest, ":"); i > 0 {
		return rest[:i]
	}
	return rest
}
