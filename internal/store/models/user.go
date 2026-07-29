package models

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

type User struct {
	ID           string `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex;not null"`
	Name         string `gorm:"not null"`
	PasswordHash string `gorm:"not null"`
	Role         string `gorm:"not null;default:user"`
	Disabled     bool   `gorm:"not null;default:false"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
}

func (User) TableName() string { return "users" }

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }
