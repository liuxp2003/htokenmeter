// Package common holds process-wide configuration and small shared helpers.
package common

import (
	"os"
	"strconv"
	"strings"
)

var Version = "dev"

// Paths and listen options; set by flags in main.go, overridable by env.
var (
	ConfigDir = "/etc/htokenmeter"
	DataDir   = "/etc/htokenmeter" // SQLite lives with config so it survives reboot on overlay
	LogDir    = ""                 // empty = stderr only
	Port      = 8317
	DBPath    = "" // empty = ConfigDir/htokenmeter.db

	// Relay behaviour.
	RetryTimes    = 3
	RelayTimeout  = 600 // seconds, 0 = no timeout
	IdleTimeout   = 60
	LogRetention  = 30 // days, 0 = keep forever
	QuotaWarnUsd  = 0.0
	TestFrequency = 0 // minutes, 0 = disabled
)

func EnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func EnvStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func EnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return def
}

func EnsureDBPath() string {
	if DBPath != "" {
		return DBPath
	}
	return ConfigDir + "/htokenmeter.db"
}
