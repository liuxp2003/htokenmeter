package controller

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/middleware"
	db "github.com/lxp/htokenmeter/model"
)

// Session store: HMAC-less random token -> userId with expiry, kept in memory.
// Single-process gateway on a router: process restart = re-login, acceptable
// for a home console and avoids a sessions table.
var (
	sessMu   sync.Mutex
	sessions = map[string]sessEntry{}
)

type sessEntry struct {
	userId  int
	expires time.Time
}

func newSessionToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashSession(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// CreateSession stores a session and returns the cookie value.
func CreateSession(userId int) string {
	tok := newSessionToken()
	sessMu.Lock()
	defer sessMu.Unlock()
	// opportunistic cleanup
	now := time.Now()
	for k, v := range sessions {
		if v.expires.Before(now) {
			delete(sessions, k)
		}
	}
	sessions[hashSession(tok)] = sessEntry{userId: userId, expires: now.Add(7 * 24 * time.Hour)}
	return tok
}

// DestroySession removes one session.
func DestroySession(tok string) {
	sessMu.Lock()
	defer sessMu.Unlock()
	delete(sessions, hashSession(tok))
}

// ValidateSessionToken resolves a cookie value to a live userId.
func ValidateSessionToken(tok string) (int, bool) {
	sessMu.Lock()
	defer sessMu.Unlock()
	e, ok := sessions[hashSession(tok)]
	if !ok || e.expires.Before(time.Now()) {
		return 0, false
	}
	return e.userId, true
}

func init() {
	// Wire the middleware to this store.
	middleware.SetSessionValidator(ValidateSessionToken)
}

// Login handles POST /api/user/login {username,password}.
func Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	user, err := db.GetUserByUsername(req.Username)
	if err != nil || !db.CheckPassword(user.Password, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "wrong username or password"})
		return
	}
	if user.Status != db.UserStatusEnabled {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "user disabled"})
		return
	}
	tok := CreateSession(user.Id)
	c.SetCookie("htokenmeter_session", tok, 7*86400, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"id": user.Id, "username": user.Username, "display_name": user.DisplayName, "role": user.Role,
	}})
}

// Logout handles POST /api/user/logout.
func Logout(c *gin.Context) {
	if tok, err := c.Cookie("htokenmeter_session"); err == nil && tok != "" {
		DestroySession(tok)
	}
	c.SetCookie("htokenmeter_session", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ChangePassword handles POST /api/user/password {old_password,new_password}.
func ChangePassword(c *gin.Context) {
	var req struct {
		Old string `json:"old_password"`
		New string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.New) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "new password must be at least 8 chars"})
		return
	}
	userId := c.GetInt("user_id")
	user, err := db.GetUserById(userId)
	if err != nil || !db.CheckPassword(user.Password, req.Old) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "wrong old password"})
		return
	}
	hash, err := db.HashPassword(req.New)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	_ = user.UpdatePassword(hash)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
