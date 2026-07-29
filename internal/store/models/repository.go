package models

const (
	PolicyRelease  = "release"
	PolicySnapshot = "snapshot"
	PolicyMixed    = "mixed"

	VisibilityPublic  = "public"
	VisibilityPrivate = "private"

	TypeHosted = "hosted"
	TypeProxy  = "proxy"

	DefaultMetadataTTLSeconds = 1800
	DefaultNegativeTTLSeconds = 1800
)

type Repository struct {
	ID     string `gorm:"primaryKey"`
	Name   string `gorm:"uniqueIndex;not null"`
	Format string `gorm:"not null;default:maven2"`
	Type   string `gorm:"not null;default:hosted"`
	// CategoryID is nullable so repositories that predate categories, and any
	// left deliberately unsorted, need no backfill. The Maven URL is built from
	// Name alone, so moving a repository between categories never moves it.
	CategoryID     *string `gorm:"index"`
	Policy         string  `gorm:"not null;default:release"`
	Visibility     string  `gorm:"not null;default:private"`
	Description    string  `gorm:"not null;default:''"`
	AllowRedeploy  bool    `gorm:"not null;default:false"`
	RemoteURL      string  `gorm:"not null;default:''"`
	RemoteUsername string  `gorm:"not null;default:''"`
	RemotePassword string  `gorm:"not null;default:''"`
	// The TTL columns default to zero rather than to the API defaults because
	// GORM leaves a zero-valued field out of an insert whenever the column
	// carries a default, which would make "recheck every time" unsettable.
	MetadataTTLSeconds int64 `gorm:"column:metadata_ttl_seconds;not null;default:0"`
	NegativeTTLSeconds int64 `gorm:"column:negative_ttl_seconds;not null;default:0"`
	CacheRetentionDays int64 `gorm:"column:cache_retention_days;not null;default:0"`
	CreatedAt          int64 `gorm:"autoCreateTime:milli"`
}

func (Repository) TableName() string { return "repositories" }

func (r Repository) IsProxy() bool { return r.Type == TypeProxy }

func (r Repository) AcceptsPolicy(isSnapshot bool) bool {
	switch r.Policy {
	case PolicyMixed:
		return true
	case PolicySnapshot:
		return isSnapshot
	default:
		return !isSnapshot
	}
}
