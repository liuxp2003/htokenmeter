package model

import (
	"errors"

	"gorm.io/gorm"

	"github.com/lxp/htokenmeter/common"
)

const (
	RoleAdmin = 10
	RoleUser  = 1

	UserStatusEnabled  = 1
	UserStatusDisabled = 2
)

type User struct {
	Id           int    `json:"id"`
	Username     string `json:"username" gorm:"uniqueIndex;size:64"`
	Password     string `json:"-" gorm:"size:128"`
	DisplayName  string `json:"display_name" gorm:"size:64"`
	Role         int    `json:"role" gorm:"default:1"`
	Status       int    `json:"status" gorm:"default:1"`
	Group        string `json:"group" gorm:"size:32;default:'default'"`
	CreatedTime  int64  `json:"created_time"`
	AccesssedTim int64  `json:"accessed_time"`
}

// EnsureAdmin creates the first admin with a random password printed to log.
func EnsureAdmin() error {
	var count int64
	DB.Model(&User{}).Count(&count)
	if count > 0 {
		return nil
	}
	pw := common.RandomPassword()
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	admin := User{
		Username:    "admin",
		Password:    hash,
		DisplayName: "Administrator",
		Role:        RoleAdmin,
		Status:      UserStatusEnabled,
		CreatedTime: common.Timestamp(),
	}
	if err := DB.Create(&admin).Error; err != nil {
		return err
	}
	common.Info("first boot: admin created, username=admin password=%s (change it after login)", pw)
	return nil
}

func GetUserByUsername(username string) (*User, error) {
	var u User
	err := DB.Where("username = ?", username).First(&u).Error
	return &u, err
}

func GetUserById(id int) (*User, error) {
	var u User
	err := DB.First(&u, id).Error
	return &u, err
}

func GetAllUsers() ([]User, error) {
	var us []User
	err := DB.Order("id asc").Find(&us).Error
	return us, err
}

func (u *User) Insert() error {
	u.CreatedTime = common.Timestamp()
	return DB.Create(u).Error
}

func (u *User) Update() error {
	return DB.Model(u).Select("username", "display_name", "role", "status", "group").Updates(u).Error
}

func (u *User) UpdatePassword(hash string) error {
	return DB.Model(u).Update("password", hash).Error
}

func (u *User) Delete() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", u.Id).Delete(&Token{}).Error; err != nil {
			return err
		}
		return tx.Delete(u).Error
	})
}

func IsAdmin(userId int) bool {
	var u User
	if err := DB.Select("role").First(&u, userId).Error; err != nil {
		return false
	}
	return u.Role >= RoleAdmin
}

func GetUserGroup(userId int) (string, error) {
	var u User
	err := DB.Select("`group`").First(&u, userId).Error
	return u.Group, err
}

var ErrUserNotFound = errors.New("user not found")
