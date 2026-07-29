package models

// RepositoryMember is one child of a group repository. Target is a repository
// name, optionally followed by a path inside it, because a p2 update site is
// usually a directory of a repository rather than the whole of one.
type RepositoryMember struct {
	RepositoryID string `gorm:"primaryKey"`
	Position     int64  `gorm:"primaryKey"`
	Target       string `gorm:"not null"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli"`
}

func (RepositoryMember) TableName() string { return "repository_members" }
