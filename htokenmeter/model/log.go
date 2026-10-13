package model

import (
	"sync"
	"time"

	"github.com/lxp/htokenmeter/common"
)

// Log records one relay consumption with full cost attribution.
// Cost is stored in micro-USD (int64) to keep decimal discipline; 1 USD = 1e6.

// AsyncWG tracks fire-and-forget accounting goroutines so tests can wait
// for them before swapping the global DB handle.
var AsyncWG sync.WaitGroup
type Log struct {
	Id               int    `json:"id"`
	UserId           int    `json:"user_id"`
	TokenId          int    `json:"token_id"`
	TokenName        string `json:"token_name" gorm:"size:64"`
	ChannelId        int    `json:"channel_id"`
	ChannelName      string `json:"channel_name" gorm:"size:64"`
	ModelName        string `json:"model_name" gorm:"size:128;index"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
	CostMicroUsd     int64  `json:"cost_micro_usd"`
	IsStream         bool   `json:"is_stream"`
	ElapsedTimeMs    int64  `json:"elapsed_time_ms"`
	Detail           string `json:"detail" gorm:"size:512;default:''"` // e.g. price snapshot
	CreatedTime      int64  `json:"created_time" gorm:"index"`
}

func RecordConsume(l *Log) {
	l.CreatedTime = common.Timestamp()
	DB.Create(l)
}

func RecordError(userId, channelId int, modelName, errMsg string) {
	DB.Create(&Log{
		UserId: userId, ChannelId: channelId, ModelName: modelName,
		Detail: "error: " + errMsg, CreatedTime: common.Timestamp(),
	})
}

// LogQuery filters for the log API.
type LogQuery struct {
	UserId    int
	ModelName string
	ChannelId int
	Start     int64
	End       int64
	Limit     int
	Offset    int
}

func QueryLogs(q LogQuery) ([]Log, int64, error) {
	tx := DB.Model(&Log{})
	if q.UserId > 0 {
		tx = tx.Where("user_id = ?", q.UserId)
	}
	if q.ModelName != "" {
		tx = tx.Where("model_name = ?", q.ModelName)
	}
	if q.ChannelId > 0 {
		tx = tx.Where("channel_id = ?", q.ChannelId)
	}
	if q.Start > 0 {
		tx = tx.Where("created_time >= ?", q.Start)
	}
	if q.End > 0 {
		tx = tx.Where("created_time <= ?", q.End)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 50
	}
	var logs []Log
	err := tx.Order("id desc").Limit(q.Limit).Offset(q.Offset).Find(&logs).Error
	return logs, total, err
}

// StatsRow is one aggregation bucket.
type StatsRow struct {
	Bucket           string `json:"bucket"` // date or model/channel/token
	ModelName        string `json:"model_name,omitempty"`
	ChannelName      string `json:"channel_name,omitempty"`
	TokenName        string `json:"token_name,omitempty"`
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	CostMicroUsd     int64  `json:"cost_micro_usd"`
}

// StatsByDay aggregates cost per day over the window.
func StatsByDay(userId int, since int64) ([]StatsRow, error) {
	tx := DB.Model(&Log{}).Select(
		"date(created_time, 'unixepoch', 'localtime') as bucket, count(*) as requests, "+
			"sum(prompt_tokens) as prompt_tokens, sum(completion_tokens) as completion_tokens, "+
			"sum(cost_micro_usd) as cost_micro_usd").
		Where("created_time >= ?", since).Group("bucket").Order("bucket asc")
	if userId > 0 {
		tx = tx.Where("user_id = ?", userId)
	}
	var rows []StatsRow
	err := tx.Find(&rows).Error
	return rows, err
}

// StatsByDimension aggregates by model / channel / token over the window.
func StatsByDimension(userId int, since int64, dim string) ([]StatsRow, error) {
	col := "model_name"
	switch dim {
	case "channel":
		col = "channel_name"
	case "token":
		col = "token_name"
	}
	tx := DB.Model(&Log{}).Select(
		col+" as bucket, count(*) as requests, sum(prompt_tokens) as prompt_tokens, "+
			"sum(completion_tokens) as completion_tokens, sum(cost_micro_usd) as cost_micro_usd").
		Where("created_time >= ?", since).Group("bucket").Order("cost_micro_usd desc").Limit(50)
	if userId > 0 {
		tx = tx.Where("user_id = ?", userId)
	}
	var rows []StatsRow
	err := tx.Find(&rows).Error
	return rows, err
}

// CleanupOldLogs removes rows older than retention days; returns removed count.
func CleanupOldLogs(days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Unix() - int64(days)*86400
	res := DB.Where("created_time < ?", cutoff).Delete(&Log{})
	return res.RowsAffected, res.Error
}
