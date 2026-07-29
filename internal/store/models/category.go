package models

type Category struct {
	ID          string `gorm:"primaryKey"`
	Name        string `gorm:"uniqueIndex;not null"`
	Description string `gorm:"not null;default:''"`
	Position    int64  `gorm:"not null;default:0"`
	CreatedAt   int64  `gorm:"autoCreateTime:milli"`
}

func (Category) TableName() string { return "categories" }
