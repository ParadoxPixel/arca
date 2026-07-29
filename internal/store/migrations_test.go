package store

import (
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"arca/internal/store/models"
)

// The legacy models are the pre-rename shapes, kept verbatim so the fixture is
// the schema GORM actually produced rather than a hand-written approximation.
type legacyComponent struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"not null;uniqueIndex:components_coordinates,priority:1;index:components_ga,priority:1"`
	GroupID      string `gorm:"not null;uniqueIndex:components_coordinates,priority:2;index:components_ga,priority:2"`
	ArtifactID   string `gorm:"not null;uniqueIndex:components_coordinates,priority:3;index:components_ga,priority:3"`
	Version      string `gorm:"not null;uniqueIndex:components_coordinates,priority:4"`
	BaseVersion  string `gorm:"not null"`
	IsSnapshot   bool   `gorm:"not null;default:false"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli;index:components_updated,priority:2,sort:desc"`
}

func (legacyComponent) TableName() string { return "components" }

type legacyTrafficEvent struct {
	ID           string `gorm:"primaryKey"`
	At           int64  `gorm:"not null;index:traffic_at,sort:desc;index:traffic_kind_at,priority:2,sort:desc"`
	Kind         string `gorm:"not null;index:traffic_kind_at,priority:1"`
	RepositoryID string `gorm:"not null;default:'';index:traffic_repository"`
	GroupID      string `gorm:"not null;default:''"`
	ArtifactID   string `gorm:"not null;default:''"`
	Version      string `gorm:"not null;default:''"`
	Bytes        int64  `gorm:"not null;default:0"`
	UserID       *string
}

func (legacyTrafficEvent) TableName() string { return "traffic_events" }

func seedLegacySchema(t *testing.T, path string) {
	t.Helper()

	pragmas := url.Values{}
	pragmas.Add("_pragma", "journal_mode(WAL)")

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?%s", path, pragmas.Encode())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("opening the legacy database: %v", err)
	}

	if err := db.AutoMigrate(&legacyComponent{}, &legacyTrafficEvent{}); err != nil {
		t.Fatalf("building the legacy schema: %v", err)
	}

	// Two artifacts sharing a version: a unique index built over empty namespace
	// and name columns would collide on exactly this pair.
	components := []legacyComponent{
		{ID: "c1", RepositoryID: "r1", GroupID: "com.example", ArtifactID: "app", Version: "1.0.0", BaseVersion: "1.0.0"},
		{ID: "c2", RepositoryID: "r1", GroupID: "com.example", ArtifactID: "lib", Version: "1.0.0", BaseVersion: "1.0.0"},
	}
	if err := db.Create(&components).Error; err != nil {
		t.Fatalf("seeding legacy components: %v", err)
	}

	event := legacyTrafficEvent{
		ID: "t1", At: 1, Kind: models.TrafficDownload, RepositoryID: "r1",
		GroupID: "com.example", ArtifactID: "app", Version: "1.0.0", Bytes: 10,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("seeding a legacy traffic event: %v", err)
	}

	pool, err := db.DB()
	if err != nil {
		t.Fatalf("accessing the legacy connection pool: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("closing the legacy database: %v", err)
	}
}

func TestOpenMigratesLegacyCoordinateColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacySchema(t, path)

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a legacy database: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	migrator := store.db.Migrator()

	t.Run("coordinate columns are renamed", func(t *testing.T) {
		columns := []struct {
			table  string
			column string
			want   bool
		}{
			{"components", "namespace", true},
			{"components", "name", true},
			{"components", "format", true},
			{"components", "group_id", false},
			{"components", "artifact_id", false},
			{"traffic_events", "namespace", true},
			{"traffic_events", "name", true},
			{"traffic_events", "group_id", false},
			{"traffic_events", "artifact_id", false},
		}

		for _, expected := range columns {
			t.Run(expected.table+"."+expected.column, func(t *testing.T) {
				if got := migrator.HasColumn(expected.table, expected.column); got != expected.want {
					t.Fatalf("HasColumn(%s, %s) = %v, want %v", expected.table, expected.column, got, expected.want)
				}
			})
		}
	})

	t.Run("indexes follow the rename", func(t *testing.T) {
		indexes := []struct {
			name string
			want bool
		}{
			{"components_coordinates", true},
			{"components_ns_name", true},
			{"components_ga", false},
		}

		for _, expected := range indexes {
			t.Run(expected.name, func(t *testing.T) {
				if got := migrator.HasIndex(&models.Component{}, expected.name); got != expected.want {
					t.Fatalf("HasIndex(%s) = %v, want %v", expected.name, got, expected.want)
				}
			})
		}
	})

	t.Run("existing rows keep their coordinates", func(t *testing.T) {
		components, err := store.ComponentVersions("r1", "com.example", "app")
		if err != nil {
			t.Fatalf("reading migrated components: %v", err)
		}
		if len(components) != 1 || components[0].Version != "1.0.0" {
			t.Fatalf("components = %+v, want one row at 1.0.0", components)
		}
		if components[0].Format != "maven2" {
			t.Fatalf("format = %q, want maven2", components[0].Format)
		}
	})

	t.Run("traffic keeps its coordinates", func(t *testing.T) {
		artifacts, err := store.TopArtifacts(0, 100, 10)
		if err != nil {
			t.Fatalf("reading migrated traffic: %v", err)
		}
		if len(artifacts) != 0 {
			t.Fatalf("artifacts = %+v, want none without a matching repository row", artifacts)
		}

		var event models.TrafficEvent
		if err := store.db.First(&event, "id = ?", "t1").Error; err != nil {
			t.Fatalf("reading the migrated traffic event: %v", err)
		}
		if event.Namespace != "com.example" || event.Name != "app" {
			t.Fatalf("event coordinates = %s/%s, want com.example/app", event.Namespace, event.Name)
		}
	})

	t.Run("running twice is a no-op", func(t *testing.T) {
		if err := store.Close(); err != nil {
			t.Fatalf("closing before reopening: %v", err)
		}

		reopened, err := Open(path)
		if err != nil {
			t.Fatalf("reopening a migrated database: %v", err)
		}
		t.Cleanup(func() { reopened.Close() })
	})
}
