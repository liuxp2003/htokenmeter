// Package controller hosts the HTTP handlers: relay orchestration and the
// management API.
package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/common"
	"github.com/lxp/htokenmeter/middleware"
	db "github.com/lxp/htokenmeter/model"
	"github.com/lxp/htokenmeter/relay/adaptor"
	"github.com/lxp/htokenmeter/relay/dto"
	"github.com/lxp/htokenmeter/relay/helper"
)

// relayTask captures everything needed across the retry loop.
type relayTask struct {
	ClientFormat helper.Format
	Stream       bool
	ClientModel  string // model name the client used (billing identity)

	// One of these is set by the parse phase:
	chat      *dto.ChatRequest // canonical chat
	embedding *dto.EmbeddingRequest
	image     *dto.ImageRequest
	rawBody   []byte // for same-format passthrough / responses
}

// Relay is the single entry for all relay routes.
func Relay(c *gin.Context, clientFormat helper.Format) {
	task, rerr := parseRelayRequest(c, clientFormat)
	if rerr != nil {
		helper.WriteError(c.Writer, clientFormat, rerr)
		return
	}

	for retry := 0; retry <= common.RetryTimes; retry++ {
		channel, rerr2 := pickChannel(c, task, retry)
		if rerr2 != nil {
			helper.WriteError(c.Writer, clientFormat, rerr2)
			return
		}
		usage, rerr3 := executeOnce(c, channel, task)
		if rerr3 == nil {
			recordSuccess(c, channel, task, usage)
			return
		}
		// Channel-side error bookkeeping.
		processChannelError(c, channel, rerr3)
		if !shouldRetry(rerr3.StatusCode) || retry == common.RetryTimes {
			helper.WriteError(c.Writer, clientFormat, rerr3)
			return
		}
		c.Set(middleware.CtxChannelId, 0) // force re-selection
		common.Info("relay retry %d after status %d (channel %s)", retry+1, rerr3.StatusCode, channel.Name)
	}
}

// pickChannel selects a channel honoring the admin pin and retry tier.
func pickChannel(c *gin.Context, task *relayTask, retry int) (*db.Channel, *helper.RelayError) {
	if id := c.GetInt(middleware.CtxChannelId); id > 0 && retry == 0 {
		ch, err := db.GetChannelById(id)
		if err != nil {
			return nil, helper.Errorf(http.StatusBadGateway, "pinned channel missing")
		}
		if ch.Status != db.ChannelStatusEnabled {
			return nil, helper.Errorf(http.StatusForbidden, "pinned channel disabled")
		}
		return ch, nil
	}
	group := c.GetString(middleware.CtxUserGroup)
	if group == "" {
		group = "default"
	}
	ch, err := db.GetRandomSatisfiedChannel(group, task.ClientModel, retry)
	if err != nil {
		return nil, helper.Errorf(http.StatusServiceUnavailable,
			"no available channel for model %s in group %s", task.ClientModel, group)
	}
	c.Set(middleware.CtxChannelId, ch.Id)
	c.Set(middleware.CtxChannelType, ch.Type)
	c.Set(middleware.CtxChannelName, ch.Name)
	return ch, nil
}

// executeOnce runs one attempt against one channel.
func executeOnce(c *gin.Context, ch *db.Channel, task *relayTask) (*dto.Usage, *helper.RelayError) {
	ad, err := adaptor.Get(ch.Type)
	if err != nil {
		return nil, helper.Errorf(http.StatusBadGateway, "%v", err)
	}
	keyIdx, key := ch.NextKey()
	c.Set("relay_key_index", keyIdx)
	meta := &adaptor.Meta{
		ChannelType: ch.Type, BaseURL: ch.BaseURL, APIKey: key, KeyIndex: keyIdx,
		APIVersion: ch.APIVersion(), Proxy: ch.Proxy,
	}
	mapping := ch.GetModelMapping()
	meta.ModelMapping = mapping
	upstreamModel := task.ClientModel
	if mapped, ok := mapping[task.ClientModel]; ok && mapped != "" {
		upstreamModel = mapped
	}
	meta.UpstreamModel = upstreamModel

	rc := &helper.RelayContext{
		ClientFormat:   task.ClientFormat,
		UpstreamFormat: helper.UpstreamFormatFor(ch.Type),
		Stream:         task.Stream,
		ClientModel:    task.ClientModel,
		UpstreamModel:  upstreamModel,
	}

	// Responses protocol: upstream must be openai-compatible; passthrough body.
	if task.ClientFormat == helper.FormatResponses {
		if ch.Type != db.TypeOpenAICompat {
			return nil, helper.Errorf(http.StatusBadRequest,
				"model %s is not served by an OpenAI-compatible channel; /v1/responses requires one", task.ClientModel)
		}
		return doResponsesPassthrough(c, ch, meta, rc, task)
	}

	// Same-format passthrough optimization when no model mapping applies.
	if canPassthrough(task, rc, upstreamModel) {
		return doPassthrough(c, ad, ch, meta, rc, task)
	}

	// Convert canonical -> upstream body.
	var reqURL string
	var body []byte
	switch {
	case task.chat != nil:
		task.chat.Model = upstreamModel
		if task.Stream && rc.UpstreamFormat == helper.FormatGemini {
			reqURL = streamChatURL(ad, meta)
		} else {
			reqURL = ad.ChatURL(meta)
		}
		body, err = ad.ConvertChat(task.chat, meta)
	case task.embedding != nil:
		task.embedding.Model = upstreamModel
		reqURL = ad.EmbeddingURL(meta)
		if reqURL == "" {
			return nil, helper.Errorf(http.StatusBadRequest, "channel type does not support embeddings")
		}
		body, err = ad.ConvertEmbedding(task.embedding, meta)
	case task.image != nil:
		task.image.Model = upstreamModel
		reqURL = ad.ImageURL(meta)
		if reqURL == "" {
			return nil, helper.Errorf(http.StatusBadRequest, "channel type does not support image generation")
		}
		body, err = ad.ConvertImage(task.image, meta)
	}
	if err != nil {
		return nil, helper.Errorf(http.StatusBadRequest, "convert request: %v", err)
	}

	resp, err := doUpstreamRequest(c, ad, meta, http.MethodPost, reqURL, body)
	if err != nil {
		return nil, helper.Errorf(http.StatusBadGateway, "upstream request failed: %v", err)
	}

	// Images: read body, count, forward (usage = image count).
	if task.image != nil {
		return relayImage(c, resp, task)
	}

	if rc.ClientFormat == rc.UpstreamFormat {
		return helper.RelayPassthrough(c.Writer, resp, rc)
	}
	return helper.RelayConvert(c.Writer, resp, rc)
}

// streamChatURL returns the stream URL for adaptors that split it.
func streamChatURL(ad adaptor.Adaptor, meta *adaptor.Meta) string {
	if s, ok := ad.(interface {
		ChatURLStream(m *adaptor.Meta) string
	}); ok {
		return s.ChatURLStream(meta)
	}
	return ad.ChatURL(meta)
}

// relayImage reads the (small) image response, counts images, forwards it.
func relayImage(c *gin.Context, resp *http.Response, task *relayTask) (*dto.Usage, *helper.RelayError) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	resp.Body.Close()
	if err != nil {
		return nil, helper.Errorf(http.StatusBadGateway, "read image response: %v", err)
	}
	if resp.StatusCode >= 400 {
		return nil, helper.UpstreamErrorFromBody(helper.FormatOpenAI, resp.StatusCode, body)
	}
	n := countImagesInBody(body)
	for k, vs := range resp.Header {
		for i, v := range vs {
			if i == 0 {
				c.Writer.Header().Set(k, v)
			} else {
				c.Writer.Header().Add(k, v)
			}
		}
	}
	c.Writer.Header().Del("Content-Length")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(body)
	return &dto.Usage{Images: n}, nil
}

var _ = fmt.Sprintf

func canPassthrough(task *relayTask, rc *helper.RelayContext, upstreamModel string) bool {
	if task.chat == nil || task.ClientFormat != rc.UpstreamFormat {
		return false
	}
	return upstreamModel == task.ClientModel // no mapping applied
}

func doPassthrough(c *gin.Context, ad adaptor.Adaptor, ch *db.Channel, meta *adaptor.Meta, rc *helper.RelayContext, task *relayTask) (*dto.Usage, *helper.RelayError) {
	body := task.rawBody
	// Ensure stream_options for openai streams so usage comes back.
	if rc.UpstreamFormat == helper.FormatOpenAI && task.Stream {
		body = ensureIncludeUsage(body)
	}
	reqURL := ad.ChatURL(meta)
	if task.Stream && rc.UpstreamFormat == helper.FormatGemini {
		reqURL = streamChatURL(ad, meta)
	}
	resp, err := doUpstreamRequest(c, ad, meta, http.MethodPost, reqURL, body)
	if err != nil {
		return nil, helper.Errorf(http.StatusBadGateway, "upstream request failed: %v", err)
	}
	return helper.RelayPassthrough(c.Writer, resp, rc)
}

// ensureIncludeUsage injects stream_options.include_usage into an OpenAI body.
func ensureIncludeUsage(body []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	so, _ := m["stream_options"].(map[string]any)
	if so == nil {
		so = map[string]any{}
	}
	so["include_usage"] = true
	m["stream_options"] = so
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func doResponsesPassthrough(c *gin.Context, ch *db.Channel, meta *adaptor.Meta, rc *helper.RelayContext, task *relayTask) (*dto.Usage, *helper.RelayError) {
	ad := &adaptor.OpenAICompat{}
	base := ch.BaseURL
	if base == "" {
		base = "https://api.openai.com"
	}
	reqURL := strings.TrimRight(base, "/") + c.Request.URL.Path
	if q := c.Request.URL.RawQuery; q != "" {
		reqURL += "?" + q
	}
	method := c.Request.Method
	if method != http.MethodPost {
		method = http.MethodGet
	}
	resp, err := doUpstreamRequest(c, ad, meta, method, reqURL, task.rawBody)
	if err != nil {
		return nil, helper.Errorf(http.StatusBadGateway, "upstream request failed: %v", err)
	}
	rc.UpstreamFormat = helper.FormatResponses
	return helper.RelayPassthrough(c.Writer, resp, rc)
}

// doUpstreamRequest performs the HTTP call with channel headers.
func doUpstreamRequest(c *gin.Context, ad adaptor.Adaptor, meta *adaptor.Meta, method, url string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), method, url, reader)
	if err != nil {
		return nil, err
	}
	ad.SetupHeaders(req.Header, meta)
	client := helper.HTTPClient(meta.Proxy)
	return client.Do(req)
}

// shouldRetry mirrors the one-api policy: 429/5xx retry, 4xx no.
func shouldRetry(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if status >= 500 {
		return true
	}
	return false
}

// processChannelError classifies failures for auto-disable / key breaking.
func processChannelError(c *gin.Context, ch *db.Channel, rerr *helper.RelayError) {
	msg := strings.ToLower(rerr.Message)
	disable := rerr.StatusCode == http.StatusUnauthorized ||
		rerr.ErrType == "insufficient_quota" || rerr.ErrType == "authentication_error" ||
		strings.Contains(msg, "insufficient_quota") ||
		strings.Contains(msg, "credit") || strings.Contains(msg, "balance") ||
		strings.Contains(msg, "invalid api key") || strings.Contains(msg, "invalid_api_key") ||
		strings.Contains(msg, "已欠费")
	if !disable {
		return
	}
	// Multi-key channels break the single key first.
	if keyIdx := c.GetInt("relay_key_index"); keyIdx >= 0 && len(ch.KeyList()) > 1 {
		ch.MarkKeyBroken(keyIdx)
		if ch.HealthyKeyCount() > 0 {
			common.Info("channel %s key #%d marked broken (%d healthy left)", ch.Name, keyIdx, ch.HealthyKeyCount())
			return
		}
	}
	if ch.Status == db.ChannelStatusEnabled {
		if err := ch.UpdateStatus(db.ChannelStatusAuto); err == nil {
			common.Info("channel %s (#%d) auto-disabled: %s", ch.Name, ch.Id, rerr.Message)
		}
	}
}

// recordSuccess bills and logs after a successful relay.
func recordSuccess(c *gin.Context, ch *db.Channel, task *relayTask, usage *dto.Usage) {
	if usage == nil {
		return
	}
	cost := int64(0)
	if p, ok := db.GetPrice(task.ClientModel); ok {
		cost = p.CostMicroUsd(usage.PromptTokens, usage.CompletionTokens, usage.CacheReadTokens, usage.Images)
	}
	userId := c.GetInt(middleware.CtxUserId)
	tokenId := c.GetInt(middleware.CtxTokenId)
	tokenName := c.GetString(middleware.CtxTokenName)
	go func() {
		db.RecordConsume(&db.Log{
			UserId: userId, TokenId: tokenId, TokenName: tokenName,
			ChannelId: ch.Id, ChannelName: ch.Name, ModelName: task.ClientModel,
			PromptTokens: int(usage.PromptTokens), CompletionTokens: int(usage.CompletionTokens),
			CacheReadTokens: int(usage.CacheReadTokens), CostMicroUsd: cost,
			IsStream: task.Stream, ElapsedTimeMs: 0,
		})
		var tk db.Token
		if err := db.DB.First(&tk, tokenId).Error; err == nil {
			tk.AddUsage(float64(cost) / 1e6)
		}
		ch.AddUsage(float64(cost) / 1e6)
	}()
}

// countImagesInBody counts data[] entries of an OpenAI image response.
func countImagesInBody(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	var probe struct {
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &probe) != nil {
		return 0
	}
	return len(probe.Data)
}
