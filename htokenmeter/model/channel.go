package model

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/lxp/htokenmeter/common"
)

const (
	ChannelStatusEnabled  = 1
	ChannelStatusManually = 2 // manually disabled
	ChannelStatusAuto     = 3 // auto disabled

	// Channel types.
	TypeOpenAICompat = 1 // any OpenAI-compatible endpoint (incl. Ollama /v1)
	TypeAnthropic    = 2
	TypeGemini       = 3
	TypeAzure        = 4
)

type Channel struct {
	Id           int     `json:"id"`
	Type         int     `json:"type"`
	Name         string  `json:"name" gorm:"size:64"`
	Status       int     `json:"status" gorm:"default:1"`
	BaseURL      string  `json:"base_url" gorm:"size:256;default:''"`
	Keys         string  `json:"keys" gorm:"type:text"` // multi-key, newline separated
	Priority     int     `json:"priority" gorm:"default:0"`
	Weight       int     `json:"weight" gorm:"default:0"`
	Models       string  `json:"models" gorm:"type:text"`
	Group        string  `json:"group" gorm:"size:128;default:'default'"`
	ModelMapping string  `json:"model_mapping" gorm:"size:2048;default:''"` // JSON {"from":"to"}
	Proxy        string  `json:"proxy" gorm:"size:256;default:''"`          // http/socks5 URL
	Config       string  `json:"config" gorm:"size:1024;default:''"`        // JSON extras (azure api_version etc.)
	UsedUsd      float64 `json:"used_usd" gorm:"default:0"`
	TestTime     int64   `json:"test_time"`
	ResponseMs   int64   `json:"response_ms"`
	CreatedTime  int64   `json:"created_time"`
}

// channelExtras is the typed view of the Config JSON column.
type channelExtras struct {
	APIVersion   string `json:"api_version,omitempty"`    // azure
	APIKeyHeader string `json:"api_key_header,omitempty"` // custom header name for openai-compat
}

func (ch *Channel) extras() channelExtras {
	var e channelExtras
	if ch.Config != "" {
		_ = json.Unmarshal([]byte(ch.Config), &e)
	}
	return e
}

func (ch *Channel) APIVersion() string { return ch.extras().APIVersion }

// KeyList returns the non-empty keys.
func (ch *Channel) KeyList() []string {
	var out []string
	for _, k := range strings.Split(ch.Keys, "\n") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func (ch *Channel) GetModelMapping() map[string]string {
	m := map[string]string{}
	if ch.ModelMapping == "" || ch.ModelMapping == "{}" {
		return m
	}
	_ = json.Unmarshal([]byte(ch.ModelMapping), &m)
	return m
}

func (ch *Channel) ModelList() []string {
	return splitComma(ch.Models)
}

// multiKeyState tracks round-robin index per channel (process local) and
// per-key breaker timestamps (F028: broken keys heal after a cooldown via
// a half-open probe instead of staying dead until every key breaks).
var (
	mkMu     sync.Mutex
	mkIndex  = map[int]int{}
	mkBroken = map[int]map[int]int64{} // channelId -> keyIdx -> broken-at unix
)

// keyBreakCooldown is how long a broken key rests before one probe request
// is allowed through (half-open).
const keyBreakCooldown = 10 * time.Minute

// NextKey returns the key for this request: round-robin over healthy keys,
// half-open probing the longest-broken key when all are cooling down.
func (ch *Channel) NextKey() (int, string) {
	keys := ch.KeyList()
	if len(keys) == 0 {
		return 0, ""
	}
	if len(keys) == 1 {
		return 0, keys[0]
	}
	mkMu.Lock()
	defer mkMu.Unlock()
	now := time.Now().Unix()
	cooldown := int64(keyBreakCooldown.Seconds())
	broken := mkBroken[ch.Id]
	start := mkIndex[ch.Id]
	for i := 0; i < len(keys); i++ {
		idx := (start + i) % len(keys)
		if bt, ok := broken[idx]; ok {
			if now-bt < cooldown {
				continue
			}
			delete(broken, idx) // cooldown served: half-open again
		}
		mkIndex[ch.Id] = (idx + 1) % len(keys)
		return idx, keys[idx]
	}
	// All keys are within cooldown: probe the one broken longest ago.
	oldest, oldestAt := -1, int64(1<<62)
	for idx, bt := range broken {
		if idx >= len(keys) { // stale index after the key list shrank
			delete(broken, idx)
			continue
		}
		if bt < oldestAt {
			oldest, oldestAt = idx, bt
		}
	}
	if oldest < 0 {
		oldest = start % len(keys)
	}
	delete(broken, oldest)
	mkIndex[ch.Id] = (oldest + 1) % len(keys)
	return oldest, keys[oldest]
}

// MarkKeyBroken disables one key after auth-type failures.
func (ch *Channel) MarkKeyBroken(idx int) {
	mkMu.Lock()
	defer mkMu.Unlock()
	if mkBroken[ch.Id] == nil {
		mkBroken[ch.Id] = map[int]int64{}
	}
	mkBroken[ch.Id][idx] = time.Now().Unix()
}

// MarkAllKeysBroken cools down every key (periodic test failure).
func (ch *Channel) MarkAllKeysBroken() {
	mkMu.Lock()
	defer mkMu.Unlock()
	now := time.Now().Unix()
	if mkBroken[ch.Id] == nil {
		mkBroken[ch.Id] = map[int]int64{}
	}
	for idx := range ch.KeyList() {
		mkBroken[ch.Id][idx] = now
	}
}

// HealthyKeyCount reports how many keys are currently unbroken (cooled-down
// keys count as healthy again; clamped to the key-list size).
func (ch *Channel) HealthyKeyCount() int {
	n := len(ch.KeyList())
	mkMu.Lock()
	defer mkMu.Unlock()
	broken := mkBroken[ch.Id]
	if len(broken) == 0 {
		return n
	}
	now := time.Now().Unix()
	cooldown := int64(keyBreakCooldown.Seconds())
	healthy := n
	for idx := range broken {
		if idx < n {
			if now-broken[idx] >= cooldown {
				continue // healed
			}
			healthy--
		}
	}
	if healthy < 0 {
		healthy = 0
	}
	return healthy
}

func GetAllChannels() ([]Channel, error) {
	var cs []Channel
	err := DB.Order("id asc").Find(&cs).Error
	return cs, err
}

func GetChannelById(id int) (*Channel, error) {
	var c Channel
	err := DB.First(&c, id).Error
	return &c, err
}

func (ch *Channel) Insert() error {
	ch.CreatedTime = common.Timestamp()
	if err := DB.Create(ch).Error; err != nil {
		return err
	}
	return ch.UpdateAbilities()
}

func (ch *Channel) Update() error {
	if err := DB.Model(ch).Select("type", "name", "status", "base_url", "keys",
		"priority", "weight", "models", "`group`", "model_mapping", "proxy", "config").Updates(ch).Error; err != nil {
		return err
	}
	return ch.UpdateAbilities()
}

func (ch *Channel) Delete() error {
	// Delete abilities in the same transaction: the one-api consistency bug
	// (orphan abilities after channel delete) must not exist here.
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("channel_id = ?", ch.Id).Delete(&Ability{}).Error; err != nil {
			return err
		}
		return tx.Delete(ch).Error
	})
}

func (ch *Channel) UpdateStatus(status int) error {
	if err := DB.Model(ch).Update("status", status).Error; err != nil {
		return err
	}
	return UpdateAbilityStatus(ch.Id, status == ChannelStatusEnabled)
}

func (ch *Channel) AddUsage(usd float64) {
	DB.Model(&Channel{}).Where("id = ?", ch.Id).
		UpdateColumn("used_usd", gorm.Expr("used_usd + ?", usd))
}

func (ch *Channel) UpdateTestResult(ms int64) {
	DB.Model(ch).Updates(map[string]any{
		"test_time":   common.Timestamp(),
		"response_ms": ms,
	})
}
