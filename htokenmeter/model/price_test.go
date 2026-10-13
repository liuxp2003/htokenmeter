package model

import "testing"

// F010: cache-write tokens must bill at CacheWriteUsd (Anthropic cache creation).
func TestCostMicroUsdCacheWrite(t *testing.T) {
	p := ModelPrice{PromptUsd: 3, CacheWriteUsd: 3.75}
	// 1M cache-write tokens at 3.75 USD/1M → 3,750,000 micro-USD.
	if got := p.CostMicroUsd(0, 0, 0, 1_000_000, 0, false); got != 3_750_000 {
		t.Fatalf("cacheWrite cost = %d, want 3750000", got)
	}
}

// Prompt semantics: OpenAI counts cached tokens inside prompt (subtract);
// Claude reports them disjoint (no subtraction).
func TestCostMicroUsdPromptCacheSemantics(t *testing.T) {
	p := ModelPrice{PromptUsd: 1, CacheReadUsd: 0.1}
	// OpenAI: prompt 100 includes 40 cached → 60 full + 40 discounted.
	got := p.CostMicroUsd(100, 0, 40, 0, 0, true)
	if got != 64 { // 60µ$ full-rate + 4µ$ discounted, rounded
		t.Fatalf("openai semantics = %d, want 64", got)
	}
	// Claude: prompt 60 disjoint + 40 cache-read separately.
	got = p.CostMicroUsd(60, 0, 40, 0, 0, false)
	if got != 64 {
		t.Fatalf("claude semantics = %d, want 64", got)
	}
}

// Images and per-request fixed fees must reach the total.
func TestCostMicroUsdImagesAndPerRequest(t *testing.T) {
	p := ModelPrice{ImageUsd: 0.004, PerRequestUsd: 0.002}
	if got := p.CostMicroUsd(0, 0, 0, 0, 3, false); got != 14_000 {
		t.Fatalf("images+perRequest = %d, want 14000", got)
	}
}

// Full Claude-shaped matrix: 81µ$ sanity (7 input @3, 2 cache-read @0.3,
// 5 cache-write @3.75, 3 output @15 → 21+0.6+18.75+45 = 85.35 → 85µ$).
func TestCostMicroUsdClaudeMatrix(t *testing.T) {
	p := ModelPrice{PromptUsd: 3, CacheReadUsd: 0.3, CacheWriteUsd: 3.75, CompletionUsd: 15}
	if got := p.CostMicroUsd(7, 3, 2, 5, 0, false); got != 85 {
		t.Fatalf("claude matrix = %d, want 85", got)
	}
}
