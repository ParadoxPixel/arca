package store

import (
	"gorm.io/gorm/clause"

	"arca/internal/store/models"
)

const (
	SettingInstanceName = "instance_name"
	SettingThemeColor   = "theme_color"
	SettingInitialized  = "initialized"

	DefaultInstanceName = "arca"
	DefaultThemeColor   = "#4f46e5"
)

func (s *Store) Setting(key string) (string, error) {
	var setting models.Setting
	err := s.db.First(&setting, "key = ?", key).Error
	if isNotFound(err) {
		return "", nil
	}
	return setting.Value, err
}

func (s *Store) SetSetting(key, value string) error {
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&models.Setting{Key: key, Value: value}).Error
}

func (s *Store) IsInitialized() (bool, error) {
	value, err := s.Setting(SettingInitialized)
	return value == "true", err
}

func (s *Store) InstanceName() string {
	name, err := s.Setting(SettingInstanceName)
	if err != nil || name == "" {
		return DefaultInstanceName
	}
	return name
}

func (s *Store) ThemeColor() string {
	color, err := s.Setting(SettingThemeColor)
	if err != nil || color == "" {
		return DefaultThemeColor
	}
	return color
}
