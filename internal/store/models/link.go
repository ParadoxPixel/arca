package models

// DeploymentLink is a stable URL that resolves to one file of whatever the
// newest version of an artifact happens to be. It is served without
// authentication so a build or an installer can follow it, which is the whole
// point: the repository stays private while one file of it is publishable.
type DeploymentLink struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"not null;index"`
	// Slug is the last segment of the public URL and is unique across the
	// instance, because the URL carries no repository name of its own.
	Slug        string `gorm:"not null;uniqueIndex"`
	Description string `gorm:"not null;default:''"`
	Namespace   string `gorm:"not null;default:''"`
	// Artifact is the component name: an artifactId in Maven, a package name in
	// npm. The column is not called "name" because Slug is what names the link.
	Artifact string `gorm:"not null"`
	// MatchType is one of the values the link package defines: a template with
	// placeholders, or a regular expression over the filename.
	MatchType string `gorm:"not null;default:template"`
	Pattern   string `gorm:"not null"`
	// Policy picks which versions are eligible, using the same three values a
	// repository policy does: releases, prereleases, or both.
	Policy string `gorm:"not null;default:release"`
	// Filename overrides the name the file is offered under. Empty serves the
	// stored filename, which carries the version and so changes every release.
	Filename string `gorm:"not null;default:''"`
	// Disabled rather than Enabled, because GORM leaves a zero-valued field out
	// of an insert whenever the column carries a default, which would make a
	// link impossible to create switched off.
	Disabled   bool  `gorm:"not null;default:false"`
	Downloads  int64 `gorm:"not null;default:0"`
	LastUsedAt int64 `gorm:"not null;default:0"`
	CreatedBy  *string
	CreatedAt  int64 `gorm:"autoCreateTime:milli"`
}

func (DeploymentLink) TableName() string { return "deployment_links" }
