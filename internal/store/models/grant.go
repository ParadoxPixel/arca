package models

const (
	PermissionRead  = "read"
	PermissionWrite = "write"
	PermissionAdmin = "admin"
)

type Grant struct {
	RepositoryID string `gorm:"primaryKey"`
	UserID       string `gorm:"primaryKey;index"`
	Permission   string `gorm:"not null"`
}

func (Grant) TableName() string { return "repository_grants" }
