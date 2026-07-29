package models

type DistTag struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"not null;uniqueIndex:dist_tags_key,priority:1"`
	Namespace    string `gorm:"not null;uniqueIndex:dist_tags_key,priority:2"`
	Name         string `gorm:"not null;uniqueIndex:dist_tags_key,priority:3"`
	Tag          string `gorm:"not null;uniqueIndex:dist_tags_key,priority:4"`
	Version      string `gorm:"not null"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli"`
}

func (DistTag) TableName() string { return "dist_tags" }
