package models

type Asset struct {
	ID           string  `gorm:"primaryKey"`
	RepositoryID string  `gorm:"not null;uniqueIndex:assets_path,priority:1"`
	ComponentID  *string `gorm:"index"`
	Path         string  `gorm:"not null;uniqueIndex:assets_path,priority:2"`
	StorageKey   string  `gorm:"not null"`
	Size         int64   `gorm:"not null"`
	ContentType  string  `gorm:"not null"`
	MD5          string  `gorm:"column:md5;not null"`
	SHA1         string  `gorm:"column:sha1;not null"`
	SHA256       string  `gorm:"column:sha256;not null"`
	SHA512       string  `gorm:"column:sha512;not null"`
	UploadedBy   *string

	FetchedAt          int64  `gorm:"not null;default:0"`
	ExpiresAt          int64  `gorm:"not null;default:0"`
	LastAccessedAt     int64  `gorm:"not null;default:0"`
	RemoteETag         string `gorm:"column:remote_etag;not null;default:''"`
	RemoteLastModified string `gorm:"column:remote_last_modified;not null;default:''"`

	CreatedAt int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt int64 `gorm:"autoUpdateTime:milli"`
}

func (Asset) TableName() string { return "assets" }

func (a Asset) IsStale(now int64) bool { return a.ExpiresAt > 0 && a.ExpiresAt <= now }

func (a Asset) Checksum(kind string) string {
	switch kind {
	case "md5":
		return a.MD5
	case "sha1":
		return a.SHA1
	case "sha256":
		return a.SHA256
	case "sha512":
		return a.SHA512
	default:
		return ""
	}
}
