package common

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	infoLog  *log.Logger
	errorLog *log.Logger
	initOnce sync.Once
)

func ensureLoggers() {
	initOnce.Do(func() {
		infoLog = log.New(os.Stdout, "[INFO] ", log.LstdFlags)
		errorLog = log.New(os.Stderr, "[ERR ] ", log.LstdFlags)
	})
}

// SetupLogger routes logs to stderr and, when LogDir is set, to a daily file.
func SetupLogger() {
	ensureLoggers()
	if LogDir != "" {
		if err := os.MkdirAll(LogDir, 0o755); err == nil {
			path := filepath.Join(LogDir, "htokenmeter.log")
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err == nil {
				infoLog.SetOutput(f)
				errorLog.SetOutput(f)
			}
		}
	}
}

func Info(format string, args ...any) {
	ensureLoggers()
	infoLog.Output(2, fmt.Sprintf(format, args...))
}

func Error(format string, args ...any) {
	ensureLoggers()
	errorLog.Output(2, fmt.Sprintf(format, args...))
}

// Fatal logs and exits; used only during startup.
func Fatal(format string, args ...any) {
	errorLog.Output(2, fmt.Sprintf(format, args...))
	os.Exit(1)
}

// Timestamp returns unix seconds.
func Timestamp() int64 { return time.Now().Unix() }
