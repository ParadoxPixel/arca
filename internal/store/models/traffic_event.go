package models

const (
	TrafficDownload = "download"
	TrafficUpload   = "upload"
	TrafficVisit    = "visit"
)

type TrafficEvent struct {
	ID           string `gorm:"primaryKey"`
	At           int64  `gorm:"not null;index:traffic_at,sort:desc;index:traffic_kind_at,priority:2,sort:desc"`
	Kind         string `gorm:"not null;index:traffic_kind_at,priority:1"`
	RepositoryID string `gorm:"not null;default:'';index:traffic_repository"`
	Namespace    string `gorm:"not null;default:''"`
	Name         string `gorm:"not null;default:''"`
	Version      string `gorm:"not null;default:''"`
	Bytes        int64  `gorm:"not null;default:0"`
	UserID       *string
}

func (TrafficEvent) TableName() string { return "traffic_events" }
