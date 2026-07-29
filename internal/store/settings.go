package store

import (
	"strconv"

	"gorm.io/gorm/clause"

	"arca/internal/store/models"
)

const (
	SettingInstanceName  = "instance_name"
	SettingThemeColor    = "theme_color"
	SettingWelcomeText   = "welcome_text"
	SettingLogoType      = "logo_type"
	SettingLogoUpdatedAt = "logo_updated_at"
	SettingInitialized   = "initialized"

	DefaultInstanceName = "arca"
	DefaultThemeColor   = "#4f46e5"

	LogoStorageKey = ".branding/logo"
)

const DefaultWelcomeText = `## Artifacts for every build here

This server hosts the Maven, npm and Eclipse p2 packages our projects publish and consume.
Point your build at a repository below and it can resolve them straight away.

- **Browse** a repository to get its URL and a ready made snippet
- **Search** if you know the artifact but not where it lives
- Ask an administrator for access to anything you cannot see`

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

func (s *Store) DeleteSetting(key string) error {
	return s.db.Delete(&models.Setting{}, "key = ?", key).Error
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

// WelcomeText has no fallback: an administrator who clears it means the landing
// page should carry no hero at all.
func (s *Store) WelcomeText() string {
	text, err := s.Setting(SettingWelcomeText)
	if err != nil {
		return ""
	}
	return text
}

type Logo struct {
	ContentType string
	UpdatedAt   int64
}

func (s *Store) Logo() Logo {
	contentType, err := s.Setting(SettingLogoType)
	if err != nil || contentType == "" {
		return Logo{}
	}

	stamp, err := s.Setting(SettingLogoUpdatedAt)
	if err != nil {
		return Logo{}
	}
	updatedAt, _ := strconv.ParseInt(stamp, 10, 64)
	return Logo{ContentType: contentType, UpdatedAt: updatedAt}
}
