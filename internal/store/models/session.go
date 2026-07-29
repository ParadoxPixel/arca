package models

type Session struct {
	ID        string `gorm:"primaryKey"`
	UserID    string `gorm:"index;not null"`
	CreatedAt int64  `gorm:"autoCreateTime:milli"`
	ExpiresAt int64  `gorm:"index"`
}

func (Session) TableName() string { return "sessions" }
