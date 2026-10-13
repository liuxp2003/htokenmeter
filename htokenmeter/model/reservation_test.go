package model

import (
	"sync"
	"testing"

	"github.com/lxp/htokenmeter/common"
)

func setupTokenDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	common.ConfigDir = dir
	common.DBPath = dir + "/t.db"
	if err := InitDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := DB.DB(); err == nil {
			sqlDB.Close()
		}
	})
}

func newToken(t *testing.T, quota float64) *Token {
	t.Helper()
	tk := &Token{UserId: 1, Name: "t", Status: TokenStatusEnabled, ExpiredTime: -1, QuotaUsd: quota}
	if err := tk.Insert(); err != nil {
		t.Fatal(err)
	}
	return tk
}

// F016: concurrent reservations against one token must not jointly overspend.
func TestReserveQuotaConcurrentOverspend(t *testing.T) {
	setupTokenDB(t)
	tk := newToken(t, 1.0) // $1 budget
	est := int64(600_000) // 60¢ estimate per request

	var mu sync.Mutex
	passed := 0
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fresh, err := GetTokenByKey(tk.Key)
			if err != nil {
				t.Error(err)
				return
			}
			if err := fresh.ReserveQuota(est); err == nil {
				mu.Lock()
				passed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if passed > 1 {
		t.Fatalf("%d concurrent requests reserved against a $1 budget at 60¢ each", passed)
	}
	// Release and the budget is available again.
	tk.ReleaseQuota(est)
	if err := tk.ReserveQuota(est); err != nil {
		t.Fatalf("after release, reservation failed: %v", err)
	}
}

// F028: broken keys heal — HealthyKeyCount reflects cooldown expiry.
func TestKeyBreakerHeals(t *testing.T) {
	setupTokenDB(t)
	ch := Channel{Type: TypeOpenAICompat, Name: "mk", BaseURL: "http://x",
		Keys: "k1\nk2", Models: "m", Group: "default", Status: ChannelStatusEnabled}
	if err := ch.Insert(); err != nil {
		t.Fatal(err)
	}
	if ch.HealthyKeyCount() != 2 {
		t.Fatalf("healthy = %d, want 2", ch.HealthyKeyCount())
	}
	ch.MarkKeyBroken(0)
	if ch.HealthyKeyCount() != 1 {
		t.Fatalf("healthy after break = %d, want 1", ch.HealthyKeyCount())
	}
	// Force the timestamp into the past to simulate cooldown expiry.
	mkMu.Lock()
	mkBroken[ch.Id][0] -= int64(keyBreakCooldown.Seconds()) + 1
	mkMu.Unlock()
	if ch.HealthyKeyCount() != 2 {
		t.Fatalf("healthy after cooldown = %d, want 2 (healed)", ch.HealthyKeyCount())
	}
	// NextKey returns a usable key rather than being stuck.
	idx, key := ch.NextKey()
	if key == "" || idx < 0 {
		t.Fatalf("NextKey = %d %q", idx, key)
	}
}

// F028: HealthyKeyCount never goes negative even with stale indexes.
func TestHealthyKeyCountClamped(t *testing.T) {
	setupTokenDB(t)
	ch := Channel{Type: TypeOpenAICompat, Name: "mk2", BaseURL: "http://x",
		Keys: "k1\nk2", Models: "m", Group: "default", Status: ChannelStatusEnabled}
	if err := ch.Insert(); err != nil {
		t.Fatal(err)
	}
	ch.MarkKeyBroken(0)
	ch.MarkKeyBroken(1)
	ch.MarkKeyBroken(7) // stale index beyond the key list
	if got := ch.HealthyKeyCount(); got < 0 {
		t.Fatalf("healthy = %d, must be >= 0", got)
	}
}
