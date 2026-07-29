package models

type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value string `gorm:"not null"`
}

func (Setting) TableName() string { return "settings" }
