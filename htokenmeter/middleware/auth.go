// Package middleware carries HTTP middlewares: relay token auth with
// protocol header normalization, console session auth, and channel dispatch.
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/common"
	db "github.com/lxp/htokenmeter/model"
)

const (
	CtxUserId       = "user_id"
	CtxTokenId      = "token_id"
	CtxTokenName    = "token_name"
	CtxTokenModels  = "token_models"
	CtxUserGroup    = "user_group"
	CtxChannelId    = "channel_id"
	CtxChannelType  = "channel_type"
	CtxChannelKey   = "channel_key"
	CtxChannelName  = "channel_name"
	CtxRequestModel = "request_model"
)

// SessionAuth validates console sessions created by controller/auth.
func SessionAuth(adminOnly bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		sess, err := c.Cookie("htokenmeter_session")
		if err != nil || sess == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": "not logged in"})
			return
		}
		userId, ok := ValidateSession(sess)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": "session expired"})
			return
		}
		user, err := db.GetUserById(userId)
		if err != nil || user.Status != db.UserStatusEnabled {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "user disabled"})
			return
		}
		if adminOnly && user.Role < db.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "admin required"})
			return
		}
		c.Set(CtxUserId, user.Id)
		c.Set(CtxUserGroup, user.Group)
		c.Next()
	}
}

func UserAuth() gin.HandlerFunc  { return SessionAuth(false) }
func AdminAuth() gin.HandlerFunc { return SessionAuth(true) }

// TokenAuth authenticates relay calls: Authorization Bearer sk-..., with
// Claude (x-api-key) and Gemini (x-goog-api-key / ?key=) header normalization.
func TokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := normalizeRelayKey(c)
		if key == "" {
			abortOpenAI(c, http.StatusUnauthorized, "no api key provided")
			return
		}
		// KEY-CHANNEL_ID pinning: split on last dash segment (keys are hex).
		channelPin := 0
		if idx := strings.LastIndex(key, "-"); idx > 0 {
			if pin, err := parsePositiveInt(key[idx+1:]); err == nil {
				channelPin = pin
				key = key[:idx]
			}
		}
		token, err := db.GetTokenByKey(key)
		if err != nil {
			abortOpenAI(c, http.StatusUnauthorized, "invalid api key")
			return
		}
		if err := token.Validate(); err != nil {
			abortOpenAI(c, http.StatusForbidden, err.Error())
			return
		}
		if token.Subnet != "" && !common.IsIPAllowed(common.ClientIP(c.Request), token.Subnet) {
			abortOpenAI(c, http.StatusForbidden, "ip not in token subnet allowance")
			return
		}
		user, err := db.GetUserById(token.UserId)
		if err != nil || user.Status != db.UserStatusEnabled {
			abortOpenAI(c, http.StatusForbidden, "user disabled")
			return
		}
		if channelPin > 0 && user.Role < db.RoleAdmin {
			abortOpenAI(c, http.StatusForbidden, "channel pinning requires admin token")
			return
		}
		c.Set(CtxUserId, token.UserId)
		c.Set(CtxTokenId, token.Id)
		c.Set(CtxTokenName, token.Name)
		c.Set(CtxTokenModels, token.Models)
		c.Set(CtxUserGroup, user.Group)
		if channelPin > 0 {
			c.Set(CtxChannelId, channelPin)
		}
		if c.GetString(CtxRequestModel) == "" {
			if m := probeRequestModel(c); m != "" {
				c.Set(CtxRequestModel, m)
			}
		}
		c.Next()
	}
}

func normalizeRelayKey(c *gin.Context) string {
	path := c.Request.URL.Path
	// Claude-style: x-api-key on /v1/messages and /v1/models.
	if strings.Contains(path, "/v1/messages") || strings.Contains(path, "/v1/models") {
		if k := c.GetHeader("x-api-key"); k != "" {
			return strings.TrimPrefix(k, "sk-")
		}
	}
	// Gemini-style: query key or x-goog-api-key on /v1beta and /v1/models.
	if strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/v1/models") {
		if k := c.Query("key"); k != "" {
			return strings.TrimPrefix(k, "sk-")
		}
		if k := c.GetHeader("x-goog-api-key"); k != "" {
			return strings.TrimPrefix(k, "sk-")
		}
	}
	key := c.GetHeader("Authorization")
	key = strings.TrimPrefix(key, "Bearer ")
	key = strings.TrimPrefix(key, "bearer ")
	key = strings.TrimPrefix(key, "sk-")
	return strings.TrimSpace(key)
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, errParse
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errParse
		}
		n = n*10 + int(r-'0')
		if n > 1<<30 {
			return 0, errParse
		}
	}
	return n, nil
}

type parseErr struct{}

func (parseErr) Error() string { return "parse error" }

var errParse = parseErr{}

// probeRequestModel extracts the model name for routing without consuming the body.
// Chat-family JSON bodies and Gemini path models are supported.
func probeRequestModel(c *gin.Context) string {
	if m := c.Param("model"); m != "" {
		return m
	}
	path := c.Request.URL.Path
	// Gemini: /v1beta/models/{model}:{action}
	if strings.HasPrefix(path, "/v1beta/models/") {
		rest := strings.TrimPrefix(path, "/v1beta/models/")
		if i := strings.Index(rest, ":"); i > 0 {
			return rest[:i]
		}
		return rest
	}
	if c.Request.Method != http.MethodPost {
		return ""
	}
	body := common.PeekBody(c)
	if len(body) == 0 {
		return ""
	}
	// Scan a 4MiB prefix instead of bailing on oversized bodies: the
	// top-level model sits early in SDK-shaped JSON, and a base64 image
	// pushing past 4MiB must not make the request unroutable (F018).
	if len(body) > 4<<20 {
		body = body[:4<<20]
	}
	// Minimal scan for "model":"..." without full unmarshal.
	return common.ExtractJSONStringField(body, "model")
}

// ValidateSession resolves console sessions; wired to controller/auth's store
// at init time to avoid an import cycle.
var ValidateSession = func(sess string) (int, bool) { return 0, false }

// SetSessionValidator is called from controller/auth.init.
func SetSessionValidator(f func(string) (int, bool)) { ValidateSession = f }

func abortOpenAI(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"message": msg, "type": "gateway_auth_error", "param": "", "code": nil},
	})
}
