package models

type RemoteMiss struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"not null;uniqueIndex:remote_misses_path,priority:1"`
	Path         string `gorm:"not null;uniqueIndex:remote_misses_path,priority:2"`
	Status       int    `gorm:"not null"`
	ExpiresAt    int64  `gorm:"not null;index"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
}

func (RemoteMiss) TableName() string { return "remote_misses" }
