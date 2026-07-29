package models

type APIToken struct {
	ID         string `gorm:"primaryKey"`
	UserID     string `gorm:"index;not null"`
	Name       string `gorm:"not null"`
	TokenHash  string `gorm:"uniqueIndex;not null"`
	Preview    string `gorm:"not null"`
	CreatedAt  int64  `gorm:"autoCreateTime:milli"`
	LastUsedAt *int64
	ExpiresAt  *int64
}

func (APIToken) TableName() string { return "api_tokens" }
