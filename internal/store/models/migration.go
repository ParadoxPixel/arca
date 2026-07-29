package models

type Migration struct {
	ID        string `gorm:"primaryKey"`
	AppliedAt int64  `gorm:"not null"`
}

func (Migration) TableName() string { return "schema_migrations" }
