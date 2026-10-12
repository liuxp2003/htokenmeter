// Package model is the GORM data layer: 7 tables, SQLite only.
package model

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/lxp/htokenmeter/common"
)

var DB *gorm.DB

// InitDB opens SQLite (pure-Go driver), applies pragmas and migrations.
func InitDB() error {
	path := common.EnsureDBPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	// modernc sqlite serializes writes; a single conn avoids SQLITE_BUSY churn.
	sqlDB.SetMaxOpenConns(1)
	DB = db

	if err := DB.AutoMigrate(
		&User{}, &Token{}, &Channel{}, &Ability{}, &Option{}, &Log{}, &ModelPrice{},
	); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := FixAbility(); err != nil {
		common.Error("FixAbility: %v", err)
	}
	if err := EnsureAdmin(); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if err := SeedDefaultPrices(); err != nil {
		common.Error("seed prices: %v", err)
	}
	return nil
}

// HashPassword hashes with bcrypt cost 8 (router CPU is slow; 8 is enough
// for a LAN-facing console and keeps login under ~50ms on aarch64).
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 8)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
