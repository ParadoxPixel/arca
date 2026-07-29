package models

type Component struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"not null;uniqueIndex:components_coordinates,priority:1;index:components_ns_name,priority:1"`
	Format       string `gorm:"not null;default:maven2"`
	Namespace    string `gorm:"not null;uniqueIndex:components_coordinates,priority:2;index:components_ns_name,priority:2"`
	Name         string `gorm:"not null;uniqueIndex:components_coordinates,priority:3;index:components_ns_name,priority:3"`
	Version      string `gorm:"not null;uniqueIndex:components_coordinates,priority:4"`
	BaseVersion  string `gorm:"not null"`
	IsSnapshot   bool   `gorm:"not null;default:false"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli;index:components_updated,priority:2,sort:desc"`
}

func (Component) TableName() string { return "components" }
