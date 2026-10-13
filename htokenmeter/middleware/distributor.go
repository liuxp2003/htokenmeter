package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	db "github.com/lxp/htokenmeter/model"
)

// Distribute selects a channel for the request model and stashes it in the
// context. Relay controllers re-select per retry.
func Distribute() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool("skip_distribute") {
			c.Next()
			return
		}
		selectChannel(c)
	}
}

// selectChannel picks a channel and writes routing fields into the context.
// It aborts with a protocol-shaped error when nothing is available.
func selectChannel(c *gin.Context) {
	if id := c.GetInt(CtxChannelId); id > 0 {
		// Admin-pinned channel from the key suffix.
		ch, err := db.GetChannelById(id)
		if err != nil {
			abortOpenAI(c, http.StatusBadRequest, "pinned channel does not exist")
			return
		}
		if ch.Status != db.ChannelStatusEnabled {
			abortOpenAI(c, http.StatusForbidden, "pinned channel is disabled")
			return
		}
		setChannelCtx(c, ch)
		return
	}
	group := c.GetString(CtxUserGroup)
	if group == "" {
		group = "default"
	}
	model := c.GetString(CtxRequestModel)
	if model == "" {
		abortOpenAI(c, http.StatusBadRequest, "model is required")
		return
	}
	// Token model whitelist.
	if ml := c.GetString(CtxTokenModels); ml != "" {
		allowed := false
		for _, m := range splitList(ml) {
			if m == model {
				allowed = true
				break
			}
		}
		if !allowed {
			abortOpenAI(c, http.StatusForbidden, "token does not allow model: "+model)
			return
		}
	}
	ch, err := db.GetRandomSatisfiedChannel(group, model, 0, nil)
	if err != nil {
		abortOpenAI(c, http.StatusServiceUnavailable,
			"no available channel for model "+model+" in group "+group)
		return
	}
	setChannelCtx(c, ch)
}

func setChannelCtx(c *gin.Context, ch *db.Channel) {
	c.Set(CtxChannelId, ch.Id)
	c.Set(CtxChannelType, ch.Type)
	c.Set(CtxChannelName, ch.Name)
	c.Next()
}

func splitList(s string) []string {
	var out []string
	start := -1
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if start >= 0 {
				item := trimSpaces(s[start:i])
				if item != "" {
					out = append(out, item)
				}
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	return out
}

func trimSpaces(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
