package model

// ModelPrice holds per-model unit prices in USD per 1M tokens.
// Zero price rows are still valid (free local models).
type ModelPrice struct {
	Model         string  `json:"model" gorm:"primaryKey;size:128"`
	PromptUsd     float64 `json:"prompt_usd"`      // per 1M input tokens
	CompletionUsd float64 `json:"completion_usd"`  // per 1M output tokens
	CacheReadUsd  float64 `json:"cache_read_usd"`  // per 1M cache-read tokens
	CacheWriteUsd float64 `json:"cache_write_usd"` // per 1M cache-write tokens
	ImageUsd      float64 `json:"image_usd"`       // per image
	PerRequestUsd float64 `json:"per_request_usd"` // fixed per request
	Enabled       bool    `json:"enabled" gorm:"default:true"`
}

func GetPrice(model string) (ModelPrice, bool) {
	var p ModelPrice
	err := DB.Where("model = ? AND enabled = ?", model, true).First(&p).Error
	return p, err == nil
}

func GetAllPrices() ([]ModelPrice, error) {
	var ps []ModelPrice
	err := DB.Order("model asc").Find(&ps).Error
	return ps, err
}

func (p *ModelPrice) Save() error { return DB.Save(p).Error }

func DeletePrice(model string) error {
	return DB.Where("model = ?", model).Delete(&ModelPrice{}).Error
}

// CostMicroUsd computes cost from usage with prices in USD/1M tokens.
// cacheRead is a discounted subset of promptTokens, so full-rate input =
// prompt - cacheRead (mirrors helper.CostMicroUsd).
func (p ModelPrice) CostMicroUsd(promptTokens, completionTokens, cacheRead int64, images int) int64 {
	fullRateInput := promptTokens - cacheRead
	if fullRateInput < 0 {
		fullRateInput = 0
	}
	cost := p.PromptUsd*float64(fullRateInput)/1e6 +
		p.CacheReadUsd*float64(cacheRead)/1e6 +
		p.CompletionUsd*float64(completionTokens)/1e6 +
		p.ImageUsd*float64(images) +
		p.PerRequestUsd
	// round-half-up to micro-USD
	if cost >= 0 {
		return int64(cost*1e6 + 0.5)
	}
	return int64(cost*1e6 - 0.5)
}

// SeedDefaultPrices inserts a small built-in catalog on first boot.
func SeedDefaultPrices() error {
	var count int64
	DB.Model(&ModelPrice{}).Count(&count)
	if count > 0 {
		return nil
	}
	defaults := []ModelPrice{
		// OpenAI (2026-10 list prices, USD / 1M tokens)
		{Model: "gpt-5.2", PromptUsd: 1.25, CompletionUsd: 10, CacheReadUsd: 0.125},
		{Model: "gpt-5.2-mini", PromptUsd: 0.25, CompletionUsd: 2, CacheReadUsd: 0.025},
		{Model: "gpt-4o", PromptUsd: 2.5, CompletionUsd: 10, CacheReadUsd: 1.25},
		{Model: "gpt-4o-mini", PromptUsd: 0.15, CompletionUsd: 0.6, CacheReadUsd: 0.075},
		{Model: "o3", PromptUsd: 2, CompletionUsd: 8, CacheReadUsd: 0.5},
		{Model: "text-embedding-3-small", PromptUsd: 0.02},
		{Model: "text-embedding-3-large", PromptUsd: 0.13},
		// Anthropic
		{Model: "claude-opus-4-5", PromptUsd: 5, CompletionUsd: 25, CacheReadUsd: 0.5, CacheWriteUsd: 6.25},
		{Model: "claude-sonnet-4-5", PromptUsd: 3, CompletionUsd: 15, CacheReadUsd: 0.3, CacheWriteUsd: 3.75},
		{Model: "claude-haiku-4-5", PromptUsd: 1, CompletionUsd: 5, CacheReadUsd: 0.1, CacheWriteUsd: 1.25},
		// Google
		{Model: "gemini-3-pro", PromptUsd: 2, CompletionUsd: 12, CacheReadUsd: 0.2},
		{Model: "gemini-3-flash", PromptUsd: 0.3, CompletionUsd: 2.5, CacheReadUsd: 0.03},
		// Open source / aggregators
		{Model: "deepseek-chat", PromptUsd: 0.27, CompletionUsd: 1.1, CacheReadUsd: 0.07},
		{Model: "deepseek-reasoner", PromptUsd: 0.55, CompletionUsd: 2.19, CacheReadUsd: 0.14},
		{Model: "grok-4", PromptUsd: 3, CompletionUsd: 15, CacheReadUsd: 0.3},
		{Model: "qwen-max", PromptUsd: 1.6, CompletionUsd: 6.4},
		{Model: "moonshot-v1-8k", PromptUsd: 1.68, CompletionUsd: 1.68},
	}
	for i := range defaults {
		defaults[i].Enabled = true
	}
	return DB.Create(&defaults).Error
}
