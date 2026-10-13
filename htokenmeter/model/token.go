package model

import (
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/lxp/htokenmeter/common"
	"github.com/lxp/htokenmeter/relay/dto"
)

const (
	TokenStatusEnabled  = 1
	TokenStatusDisabled = 2
	TokenStatusExpired  = 3
)

type Token struct {
	Id           int     `json:"id"`
	UserId       int     `json:"user_id"`
	Key          string  `json:"key" gorm:"uniqueIndex;size:96"`
	Name         string  `json:"name" gorm:"size:64"`
	Status       int     `json:"status" gorm:"default:1"`
	CreatedTime  int64   `json:"created_time"`
	AccessedTime int64   `json:"accessed_time"`
	ExpiredTime  int64   `json:"expired_time" gorm:"default:-1"` // -1 = never
	QuotaUsd     float64 `json:"quota_usd"` // USD budget, -1 = unlimited, 0 = frozen; no gorm default — zero must survive inserts
	UsedUsd      float64 `json:"used_usd" gorm:"default:0"`
	Models       string  `json:"models" gorm:"size:2048;default:''"` // comma list, empty = all
	Subnet       string  `json:"subnet" gorm:"size:256;default:''"`
}

func GetTokenByKey(key string) (*Token, error) {
	var t Token
	err := DB.Where("`key` = ?", key).First(&t).Error
	return &t, err
}

func GetTokenByIds(id, userId int) (*Token, error) {
	var t Token
	err := DB.Where("id = ? and user_id = ?", id, userId).First(&t).Error
	return &t, err
}

func GetAllUserTokens(userId int) ([]Token, error) {
	var ts []Token
	err := DB.Where("user_id = ?", userId).Order("id desc").Find(&ts).Error
	return ts, err
}

func GetAllTokens() ([]Token, error) {
	var ts []Token
	err := DB.Order("id desc").Find(&ts).Error
	return ts, err
}

func (t *Token) Insert() error {
	t.Key = common.GenKey(24)
	t.CreatedTime = common.Timestamp()
	return DB.Create(t).Error
}

func (t *Token) Update() error {
	return DB.Model(t).Select("name", "status", "expired_time", "quota_usd", "models", "subnet").Updates(t).Error
}

func (t *Token) Delete() error { return DB.Delete(t).Error }

// Validate performs expiry / status / budget checks for relay use.
func (t *Token) Validate() error {
	if t.Status == TokenStatusDisabled {
		return errors.New("token disabled")
	}
	if t.ExpiredTime != -1 && t.ExpiredTime < time.Now().Unix() {
		DB.Model(t).Updates(map[string]any{"status": TokenStatusExpired})
		return errors.New("token expired")
	}
	if t.QuotaUsd >= 0 && t.UsedUsd >= t.QuotaUsd {
		return errors.New("token budget exhausted")
	}
	return nil
}

// In-flight budget reservation (F016): consumption is booked to the DB only
// after a response, so N concurrent requests all validated against the same
// snapshot could jointly overspend a shared key. Reserving an upfront
// estimate per request closes that window to estimation error.
var (
	resMu  sync.Mutex
	resMap = map[int]int64{} // token id -> reserved micro-USD
)

// ReserveQuota atomically checks the budget including in-flight estimates
// and reserves estMicro for this request.
func (t *Token) ReserveQuota(estMicro int64) error {
	if t.QuotaUsd < 0 {
		return nil // unlimited: nothing to reserve against
	}
	resMu.Lock()
	defer resMu.Unlock()
	reserved := resMap[t.Id]
	if t.UsedUsd+float64(reserved+estMicro)/1e6 >= t.QuotaUsd {
		return errors.New("token budget exhausted")
	}
	resMap[t.Id] = reserved + estMicro
	return nil
}

// ReleaseQuota gives a reservation back once the request settles.
func (t *Token) ReleaseQuota(estMicro int64) {
	if t.QuotaUsd < 0 {
		return
	}
	resMu.Lock()
	defer resMu.Unlock()
	resMap[t.Id] -= estMicro
	if resMap[t.Id] <= 0 {
		delete(resMap, t.Id)
	}
}

// EstimateRequestMicro prices a rough upper bound for the request body so
// the budget reservation has something to hold (F016). No price row or no
// model means no reservation — the plain snapshot check still applies.
func EstimateRequestMicro(model string, body []byte) int64 {
	if model == "" {
		return 0
	}
	p, ok := GetPrice(model)
	if !ok {
		return 0
	}
	tok := dto.EstimateTokens(string(body))
	if tok <= 0 {
		return 0
	}
	// Prompt+completion allowance: price as if completion ≈ prompt.
	return p.CostMicroUsd(tok, tok, 0, 0, 0, true)
}

// AllowsModel checks the token model whitelist (empty = all).
func (t *Token) AllowsModel(model string) bool {
	if t.Models == "" {
		return true
	}
	for _, m := range splitComma(t.Models) {
		if m == model {
			return true
		}
	}
	return false
}

// AddUsage records consumed USD against the token (best-effort).
func (t *Token) AddUsage(usd float64) {
	DB.Model(&Token{}).Where("id = ?", t.Id).Updates(map[string]any{
		"used_usd":      gorm.Expr("used_usd + ?", usd),
		"accessed_time": common.Timestamp(),
	})
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' || r == '，' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else if r != ' ' {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
