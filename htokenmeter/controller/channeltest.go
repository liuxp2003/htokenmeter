package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/lxp/htokenmeter/common"
	db "github.com/lxp/htokenmeter/model"
	"github.com/lxp/htokenmeter/relay/adaptor"
	"github.com/lxp/htokenmeter/relay/dto"
	"github.com/lxp/htokenmeter/relay/helper"
)

// probeChannel sends a 1-message chat through a channel and reports text.
func probeChannel(ctx context.Context, ch *db.Channel, model string) (string, *helper.RelayError) {
	ad, err := adaptor.Get(ch.Type)
	if err != nil {
		return "", helper.Errorf(http.StatusBadRequest, "%v", err)
	}
	keyIdx, key := ch.NextKey()
	meta := &adaptor.Meta{
		ChannelType: ch.Type, BaseURL: ch.BaseURL, APIKey: key, KeyIndex: keyIdx,
		APIVersion: ch.APIVersion(), Proxy: ch.Proxy, UpstreamModel: model,
	}
	mapped := model
	if m, ok := ch.GetModelMapping()[model]; ok && m != "" {
		mapped = m
		meta.UpstreamModel = mapped
	}
	maxTok := 16
	req := &dto.ChatRequest{
		Model:     mapped,
		Messages:  []dto.Message{{Role: "user", Content: dto.StringContent("Say OK")}},
		MaxTokens: &maxTok,
	}
	body, err := ad.ConvertChat(req, meta)
	if err != nil {
		return "", helper.Errorf(http.StatusBadRequest, "%v", err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, ad.ChatURL(meta), bytes.NewReader(body))
	if err != nil {
		return "", helper.Errorf(http.StatusInternalServerError, "%v", err)
	}
	ad.SetupHeaders(httpReq.Header, meta)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq.WithContext(ctx))
	if err != nil {
		return "", helper.Errorf(http.StatusBadGateway, "%v", err)
	}
	defer resp.Body.Close()
	respBody, _ := readLimited(resp.Body, 1<<20)
	if resp.StatusCode >= 400 {
		rerr := helper.UpstreamErrorFromBody(helper.UpstreamFormatFor(ch.Type), resp.StatusCode, respBody)
		// auth-type failures break this key on multi-key channels
		if rerr.StatusCode == http.StatusUnauthorized && len(ch.KeyList()) > 1 {
			ch.MarkKeyBroken(keyIdx)
		}
		return "", rerr
	}
	return extractFirstText(helper.UpstreamFormatFor(ch.Type), respBody), nil
}

func readLimited(r interface{ Read([]byte) (int, error) }, n int64) ([]byte, error) {
	buf := &bytes.Buffer{}
	_, err := copyLimited(buf, r, n)
	return buf.Bytes(), err
}

func copyLimited(dst *bytes.Buffer, src interface{ Read([]byte) (int, error) }, n int64) (int64, error) {
	var total int64
	tmp := make([]byte, 32*1024)
	for total < n {
		rn, err := src.Read(tmp)
		if rn > 0 {
			dst.Write(tmp[:rn])
			total += int64(rn)
		}
		if err != nil {
			return total, nil
		}
	}
	return total, nil
}

// extractFirstText pulls the assistant text for the channel's format.
func extractFirstText(f helper.Format, body []byte) string {
	switch f {
	case helper.FormatClaude:
		var cr dto.ClaudeResponse
		if json.Unmarshal(body, &cr) == nil {
			for _, b := range cr.Content {
				if b.Type == "text" && b.Text != "" {
					return b.Text
				}
			}
		}
	case helper.FormatGemini:
		var gr dto.GeminiResponse
		if json.Unmarshal(body, &gr) == nil && len(gr.Candidates) > 0 {
			for _, p := range gr.Candidates[0].Content.Parts {
				if p.Text != "" {
					return p.Text
				}
			}
		}
	default:
		var or dto.ChatResponse
		if json.Unmarshal(body, &or) == nil && len(or.Choices) > 0 && or.Choices[0].Message != nil {
			return or.Choices[0].Message.TextContent()
		}
	}
	return "(non-text response)"
}

// StartChannelTester runs periodic channel tests when TEST_FREQUENCY is set.
func StartChannelTester() {
	if common.TestFrequency <= 0 {
		return
	}
	go func() {
		for {
			time.Sleep(time.Duration(common.TestFrequency) * time.Minute)
			common.Info("periodic channel test started")
			chs, err := db.GetAllChannels()
			if err != nil {
				continue
			}
			for i := range chs {
				ch := chs[i]
				if ch.Status != db.ChannelStatusEnabled {
					continue
				}
				models := ch.ModelList()
				if len(models) == 0 {
					continue
				}
				_, rerr := probeChannel(context.Background(), &ch, models[0])
				if rerr != nil && rerr.StatusCode == http.StatusUnauthorized {
					if err := ch.UpdateStatus(db.ChannelStatusAuto); err == nil {
						common.Info("channel %s auto-disabled after test: %s", ch.Name, rerr.Message)
					}
				}
			}
			common.Info("periodic channel test finished")
		}
	}()
}

// StartLogCleaner removes old logs daily (flash protection).
func StartLogCleaner() {
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			if n, err := db.CleanupOldLogs(common.LogRetention); err == nil && n > 0 {
				common.Info("log cleanup removed %d rows older than %d days", n, common.LogRetention)
			}
		}
	}()
}
