package model

import "sync"

// Option is a hot-reloadable KV setting persisted in SQLite.
type Option struct {
	Key   string `json:"key" gorm:"primaryKey;size:64"`
	Value string `json:"value" gorm:"size:4096"`
}

var (
	optMu    sync.RWMutex
	optCache = map[string]string{}
)

func LoadOptions() error {
	var opts []Option
	if err := DB.Find(&opts).Error; err != nil {
		return err
	}
	optMu.Lock()
	defer optMu.Unlock()
	optCache = map[string]string{}
	for _, o := range opts {
		optCache[o.Key] = o.Value
	}
	return nil
}

func GetOption(key, def string) string {
	optMu.RLock()
	defer optMu.RUnlock()
	if v, ok := optCache[key]; ok && v != "" {
		return v
	}
	return def
}

func SetOption(key, value string) error {
	if err := DB.Save(&Option{Key: key, Value: value}).Error; err != nil {
		return err
	}
	optMu.Lock()
	optCache[key] = value
	optMu.Unlock()
	return nil
}

func AllOptions() map[string]string {
	optMu.RLock()
	defer optMu.RUnlock()
	out := make(map[string]string, len(optCache))
	for k, v := range optCache {
		out[k] = v
	}
	return out
}
