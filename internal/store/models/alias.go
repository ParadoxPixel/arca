package models

// RepositoryAlias is a name a repository used to answer to. Renaming leaves one
// behind so builds that still ask for the old name keep resolving, and the name
// is the primary key because only one repository can own a name at a time: when
// a new repository claims it, the alias is dropped rather than shadowed.
type RepositoryAlias struct {
	Name         string `gorm:"primaryKey"`
	RepositoryID string `gorm:"not null;index"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
}

func (RepositoryAlias) TableName() string { return "repository_aliases" }
