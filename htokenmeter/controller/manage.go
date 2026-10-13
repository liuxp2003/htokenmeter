package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/common"
	db "github.com/lxp/htokenmeter/model"
)

// ---------- channels ----------

func ListChannels(c *gin.Context) {
	chs, err := db.GetAllChannels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	// Mask keys in list view.
	for i := range chs {
		keys := chs[i].KeyList()
		n := len(keys)
		if n > 0 {
			masked := keys[0]
			if len(masked) > 8 {
				masked = masked[:8] + "..."
			}
			chs[i].Keys = masked + " (" + strconv.Itoa(n) + ")"
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": chs})
}

type channelPayload struct {
	Id           int    `json:"id"`
	Type         int    `json:"type"`
	Name         string `json:"name"`
	Status       int    `json:"status"`
	BaseURL      string `json:"base_url"`
	Keys         string `json:"keys"`
	Priority     int    `json:"priority"`
	Weight       int    `json:"weight"`
	Models       string `json:"models"`
	Group        string `json:"group"`
	ModelMapping string `json:"model_mapping"`
	Proxy        string `json:"proxy"`
	Config       string `json:"config"`
}

func bindChannel(c *gin.Context) (*db.Channel, bool) {
	var p channelPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid payload: " + err.Error()})
		return nil, false
	}
	if p.Name == "" || p.Models == "" || p.Group == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "name, models, group are required"})
		return nil, false
	}
	ch := &db.Channel{
		Type: p.Type, Name: p.Name, Status: p.Status, BaseURL: p.BaseURL,
		Priority: p.Priority, Weight: p.Weight, Models: p.Models,
		Group: p.Group, ModelMapping: p.ModelMapping, Proxy: p.Proxy, Config: p.Config,
	}
	if p.Keys != "" && !strings.Contains(p.Keys, "...") {
		ch.Keys = p.Keys
	}
	ch.Id = p.Id
	return ch, true
}

func CreateChannel(c *gin.Context) {
	ch, ok := bindChannel(c)
	if !ok {
		return
	}
	if err := ch.Insert(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ch})
}

func UpdateChannel(c *gin.Context) {
	ch, ok := bindChannel(c)
	if !ok {
		return
	}
	existing, err := db.GetChannelById(ch.Id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "channel not found"})
		return
	}
	if ch.Keys == "" {
		ch.Keys = existing.Keys // keep old keys when masked
	}
	if err := ch.Update(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeleteChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	ch, err := db.GetChannelById(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "channel not found"})
		return
	}
	if err := ch.Delete(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func UpdateChannelStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Status int `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid payload"})
		return
	}
	ch, err := db.GetChannelById(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "channel not found"})
		return
	}
	if err := ch.UpdateStatus(req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// TestChannel runs a small chat probe against a channel.
func TestChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	ch, err := db.GetChannelById(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "channel not found"})
		return
	}
	model := c.Query("model")
	if model == "" {
		models := ch.ModelList()
		if len(models) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "channel has no models"})
			return
		}
		model = models[0]
	}
	start := common.Timestamp()
	msg, rerr := probeChannel(c.Request.Context(), ch, model)
	ms := (common.Timestamp() - start) * 1000
	if rerr != nil {
		ch.UpdateTestResult(0)
		c.JSON(http.StatusOK, gin.H{"success": false, "message": rerr.Message, "time_ms": ms})
		return
	}
	ch.UpdateTestResult(ms)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": msg, "time_ms": ms})
}

// ---------- tokens ----------

func ListTokens(c *gin.Context) {
	userId := c.GetInt("user_id")
	var ts []db.Token
	var err error
	if db.IsAdmin(userId) && c.Query("all") == "1" {
		ts, err = db.GetAllTokens()
	} else {
		ts, err = db.GetAllUserTokens(userId)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	// Mask keys in list view; the create response is the only full-key surface.
	for i := range ts {
		if len(ts[i].Key) > 8 {
			ts[i].Key = ts[i].Key[:8] + "..."
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ts})
}

type tokenPayload struct {
	Id          int      `json:"id"`
	Name        string   `json:"name"`
	Status      int      `json:"status"`
	ExpiredTime int64    `json:"expired_time"`
	QuotaUsd    *float64 `json:"quota_usd"` // pointer: distinguish absent (-1) from explicit 0 (frozen)
	Models      string   `json:"models"`
	Subnet      string   `json:"subnet"`
}

func CreateToken(c *gin.Context) {
	userId := c.GetInt("user_id")
	var p tokenPayload
	if err := c.ShouldBindJSON(&p); err != nil || p.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "name is required"})
		return
	}
	quota := -1.0
	if p.QuotaUsd != nil {
		quota = *p.QuotaUsd
	}
	t := &db.Token{
		UserId: userId, Name: p.Name, Status: db.TokenStatusEnabled,
		ExpiredTime: orDefault(p.ExpiredTime, -1), QuotaUsd: quota,
		Models: p.Models, Subnet: p.Subnet,
	}
	if err := t.Insert(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": t})
}

func UpdateToken(c *gin.Context) {
	userId := c.GetInt("user_id")
	var p tokenPayload
	if err := c.ShouldBindJSON(&p); err != nil || p.Id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid payload"})
		return
	}
	t, err := db.GetTokenByIds(p.Id, userId)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "token not found"})
		return
	}
	t.Name = p.Name
	t.Status = p.Status
	t.ExpiredTime = p.ExpiredTime
	if p.QuotaUsd != nil {
		t.QuotaUsd = *p.QuotaUsd
	}
	t.Models = p.Models
	t.Subnet = p.Subnet
	if err := t.Update(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeleteToken(c *gin.Context) {
	userId := c.GetInt("user_id")
	id, _ := strconv.Atoi(c.Param("id"))
	t, err := db.GetTokenByIds(id, userId)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "token not found"})
		return
	}
	if err := t.Delete(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func orDefault(v, def int64) int64 {
	if v == 0 {
		return def
	}
	return v
}

// ---------- users (admin) ----------

func ListUsers(c *gin.Context) {
	us, err := db.GetAllUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": us})
}

func CreateUser(c *gin.Context) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        int    `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "username and 8+ char password required"})
		return
	}
	hash, err := db.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	u := &db.User{
		Username: req.Username, Password: hash, DisplayName: req.DisplayName,
		Role: req.Role, Status: db.UserStatusEnabled, Group: "default",
	}
	if err := u.Insert(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": u})
}

func UpdateUser(c *gin.Context) {
	var req struct {
		Id          int    `json:"id"`
		DisplayName string `json:"display_name"`
		Role        int    `json:"role"`
		Status      int    `json:"status"`
		Group       string `json:"group"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid payload"})
		return
	}
	u, err := db.GetUserById(req.Id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "user not found"})
		return
	}
	u.DisplayName = req.DisplayName
	u.Role = req.Role
	u.Status = req.Status
	if req.Group != "" {
		u.Group = req.Group
	}
	if err := u.Update(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeleteUser(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == c.GetInt("user_id") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "cannot delete yourself"})
		return
	}
	u, err := db.GetUserById(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "user not found"})
		return
	}
	if err := u.Delete(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ---------- prices ----------

func ListPrices(c *gin.Context) {
	ps, err := db.GetAllPrices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ps})
}

func SavePrice(c *gin.Context) {
	var p db.ModelPrice
	if err := c.ShouldBindJSON(&p); err != nil || p.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model is required"})
		return
	}
	p.Enabled = true
	if err := p.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeletePrice(c *gin.Context) {
	if err := db.DeletePrice(c.Param("model")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ---------- logs & stats ----------

func ListLogs(c *gin.Context) {
	q := db.LogQuery{
		UserId:    c.GetInt("user_id"),
		ModelName: c.Query("model"),
		Limit:     atoiDefault(c.Query("limit"), 50),
		Offset:    atoiDefault(c.Query("offset"), 0),
	}
	if db.IsAdmin(q.UserId) {
		q.UserId = atoiDefault(c.Query("user_id"), q.UserId)
		if c.Query("user_id") == "0" {
			q.UserId = 0
		}
		q.ChannelId = atoiDefault(c.Query("channel_id"), 0)
	}
	logs, total, err := db.QueryLogs(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": logs, "total": total})
}

func GetStats(c *gin.Context) {
	userId := c.GetInt("user_id")
	if db.IsAdmin(userId) && c.Query("global") == "1" {
		userId = 0
	}
	days := atoiDefault(c.Query("days"), 14)
	since := common.Timestamp() - int64(days)*86400
	byDay, err := db.StatsByDay(userId, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	byModel, _ := db.StatsByDimension(userId, since, "model")
	byChannel, _ := db.StatsByDimension(userId, since, "channel")
	byToken, _ := db.StatsByDimension(userId, since, "token")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"by_day": byDay, "by_model": byModel, "by_channel": byChannel, "by_token": byToken,
	}})
}

// ---------- options ----------

func GetOptions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": db.AllOptions()})
}

func SetOption(c *gin.Context) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "key is required"})
		return
	}
	if err := db.SetOption(req.Key, req.Value); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
