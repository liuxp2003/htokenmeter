package controller

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/common"
	db "github.com/lxp/htokenmeter/model"
	"github.com/lxp/htokenmeter/relay/dto"
)

// GetStatus reports basic gateway info for the console and health checks.
func GetStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"version":    common.Version,
			"start_time": startTime,
		},
	})
}

var startTime = time.Now().Unix()

// ListModels aggregates channel models for the calling token, in OpenAI shape.
// Claude SDKs also read this endpoint (data[] form).
func ListModels(c *gin.Context) {
	models := visibleModels(c)
	data := make([]dto.ModelCard, 0, len(models))
	now := time.Now().Unix()
	for _, m := range models {
		data = append(data, dto.ModelCard{Id: m, Object: "model", Created: now, OwnedBy: "htokenmeter"})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

// RetrieveModel returns one card or 404.
func RetrieveModel(c *gin.Context) {
	want := c.Param("model")
	for _, m := range visibleModels(c) {
		if m == want {
			c.JSON(http.StatusOK, dto.ModelCard{
				Id: m, Object: "model", Created: time.Now().Unix(), OwnedBy: "htokenmeter",
			})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
		"message": "model not found: " + want, "type": "invalid_request_error",
	}})
}

// ListGeminiModels lists models in Gemini shape for /v1beta/models.
func ListGeminiModels(c *gin.Context) {
	models := visibleModels(c)
	modelsArr := make([]gin.H, 0, len(models))
	for _, m := range models {
		modelsArr = append(modelsArr, gin.H{
			"name":        "models/" + m,
			"version":     "1",
			"displayName": m,
			"description": m + " via htokenmeter",
		})
	}
	c.JSON(http.StatusOK, gin.H{"models": modelsArr})
}

// visibleModels = user-group models ∩ token whitelist.
func visibleModels(c *gin.Context) []string {
	group := c.GetString("user_group")
	if group == "" {
		group = "default"
	}
	models, err := db.GroupModels(group)
	if err != nil {
		return nil
	}
	ml := c.GetString("token_models")
	if ml == "" {
		return models
	}
	allowed := map[string]bool{}
	for _, m := range splitCSV(ml) {
		allowed[m] = true
	}
	var out []string
	for _, m := range models {
		if allowed[m] {
			out = append(out, m)
		}
	}
	return out
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else if r != ' ' {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
