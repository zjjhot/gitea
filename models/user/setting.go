// Copyright 2021 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package user

import (
	"context"
	"fmt"
	"strings"

	"code.gitea.io/gitea/models/db"

	"xorm.io/builder"
)

// Setting is a key value store of user settings
type Setting struct {
	ID           int64  `xorm:"pk autoincr"`
	UserID       int64  `xorm:"index unique(key_userid)"`              // to load all of someone's settings
	SettingKey   string `xorm:"varchar(255) index unique(key_userid)"` // ensure key is always lowercase
	SettingValue string `xorm:"text"`
}

// TableName sets the table name for the settings struct
func (s *Setting) TableName() string {
	return "user_setting"
}

func init() {
	db.RegisterModel(new(Setting))
}

// GetUserSettings returns specific settings from user
func GetUserSettings(uid int64, keys []string) (map[string]*Setting, error) {
	settings := make([]*Setting, 0, len(keys))
	if err := db.GetEngine(db.DefaultContext).
		Where("user_id=?", uid).
		And(builder.In("setting_key", keys)).
		Find(&settings); err != nil {
		return nil, err
	}
	settingsMap := make(map[string]*Setting)
	for _, s := range settings {
		settingsMap[s.SettingKey] = s
	}
	return settingsMap, nil
}

// GetUserAllSettings returns all settings from user
func GetUserAllSettings(uid int64) (map[string]*Setting, error) {
	settings := make([]*Setting, 0, 5)
	if err := db.GetEngine(db.DefaultContext).
		Where("user_id=?", uid).
		Find(&settings); err != nil {
		return nil, err
	}
	settingsMap := make(map[string]*Setting)
	for _, s := range settings {
		settingsMap[s.SettingKey] = s
	}
	return settingsMap, nil
}

func validateUserSettingKey(key string) error {
	if len(key) == 0 {
		return fmt.Errorf("setting key must be set")
	}
	if strings.ToLower(key) != key {
		return fmt.Errorf("setting key should be lowercase")
	}
	return nil
}

// GetUserSetting gets a specific setting for a user
func GetUserSetting(userID int64, key string, def ...string) (string, error) {
	if err := validateUserSettingKey(key); err != nil {
		return "", err
	}
	setting := &Setting{UserID: userID, SettingKey: key}
	has, err := db.GetEngine(db.DefaultContext).Get(setting)
	if err != nil {
		return "", err
	}
	if !has {
		if len(def) == 1 {
			return def[0], nil
		}
		return "", nil
	}
	return setting.SettingValue, nil
}

// DeleteUserSetting deletes a specific setting for a user
func DeleteUserSetting(userID int64, key string) error {
	if err := validateUserSettingKey(key); err != nil {
		return err
	}
	_, err := db.GetEngine(db.DefaultContext).Delete(&Setting{UserID: userID, SettingKey: key})
	return err
}

// SetUserSetting updates a users' setting for a specific key
func SetUserSetting(userID int64, key, value string) error {
	if err := validateUserSettingKey(key); err != nil {
		return err
	}
	return upsertUserSettingValue(userID, key, value)
}

func upsertUserSettingValue(userID int64, key, value string) error {
	return db.WithTx(func(ctx context.Context) error {
		e := db.GetEngine(ctx)

		// here we use a general method to do a safe upsert for different databases (and most transaction levels)
		// 1. try to UPDATE the record and acquire the transaction write lock
		//    if UPDATE returns non-zero rows are changed, OK, the setting is saved correctly
		//    if UPDATE returns "0 rows changed", two possibilities: (a) record doesn't exist  (b) value is not changed
		// 2. do a SELECT to check if the row exists or not (we already have the transaction lock)
		// 3. if the row doesn't exist, do an INSERT (we are still protected by the transaction lock, so it's safe)
		//
		// to optimize the SELECT in step 2, we can use an extra column like `revision=revision+1`
		//    to make sure the UPDATE always returns a non-zero value for existing (unchanged) records.

		res, err := e.Exec("UPDATE user_setting SET setting_value=? WHERE setting_key=? AND user_id=?", value, key, userID)
		if err != nil {
			return err
		}
		rows, _ := res.RowsAffected()
		if rows > 0 {
			// the existing row is updated, so we can return
			return nil
		}

		// in case the value isn't changed, update would return 0 rows changed, so we need this check
		has, err := e.Exist(&Setting{UserID: userID, SettingKey: key})
		if err != nil {
			return err
		}
		if has {
			return nil
		}

		// if no existing row, insert a new row
		_, err = e.Insert(&Setting{UserID: userID, SettingKey: key, SettingValue: value})
		return err
	})
}
