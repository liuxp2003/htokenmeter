package model

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/lxp/htokenmeter/common"
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
	QuotaUsd     float64 `json:"quota_usd" gorm:"default:-1"`    // USD budget, -1 = unlimited
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
