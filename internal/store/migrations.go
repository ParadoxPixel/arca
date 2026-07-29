package store

import (
	"gorm.io/gorm"

	"arca/internal/store/models"
)

type migration struct {
	id string
	up func(*gorm.DB) error
}

// Migrations run before AutoMigrate. Renaming a column in place keeps the
// indexes that already cover it, which AutoMigrate cannot do: it would add the
// new column empty and then fail to build a unique index over the duplicates.
var migrations = []migration{
	{"0001_component_namespace", renameCoordinateColumns},
}

func (s *Store) runMigrations() error {
	for _, entry := range migrations {
		var applied int64
		if err := s.db.Model(&models.Migration{}).Where("id = ?", entry.id).Count(&applied).Error; err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		if err := entry.up(s.db); err != nil {
			return err
		}
		if err := s.db.Create(&models.Migration{ID: entry.id, AppliedAt: NowMillis()}).Error; err != nil {
			return err
		}
	}
	return nil
}

func renameCoordinateColumns(db *gorm.DB) error {
	renames := []struct {
		table string
		from  string
		to    string
	}{
		{"components", "group_id", "namespace"},
		{"components", "artifact_id", "name"},
		{"traffic_events", "group_id", "namespace"},
		{"traffic_events", "artifact_id", "name"},
	}

	for _, rename := range renames {
		if !db.Migrator().HasTable(rename.table) {
			continue
		}
		if !db.Migrator().HasColumn(rename.table, rename.from) {
			continue
		}
		statement := "ALTER TABLE " + rename.table + " RENAME COLUMN " + rename.from + " TO " + rename.to
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}

	return db.Exec("DROP INDEX IF EXISTS components_ga").Error
}
