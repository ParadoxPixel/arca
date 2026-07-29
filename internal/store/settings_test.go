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

func seedSettings(t *testing.T, path string, settings []models.Setting) {
	t.Helper()

	pragmas := url.Values{}
	pragmas.Add("_pragma", "journal_mode(WAL)")

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?%s", path, pragmas.Encode())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}

	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("building the settings table: %v", err)
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seeding settings: %v", err)
	}

	pool, err := db.DB()
	if err != nil {
		t.Fatalf("accessing the connection pool: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}
}

func TestOpenSeedsWelcomeText(t *testing.T) {
	testCases := []struct {
		name     string
		existing []models.Setting
		want     string
	}{
		{
			name:     "an installation from before the landing page",
			existing: []models.Setting{{Key: SettingInstanceName, Value: "acme"}},
			want:     DefaultWelcomeText,
		},
		{
			name: "an installation that already has one",
			existing: []models.Setting{
				{Key: SettingInstanceName, Value: "acme"},
				{Key: SettingWelcomeText, Value: "written by hand"},
			},
			want: "written by hand",
		},
		{
			name: "an administrator who cleared it",
			existing: []models.Setting{
				{Key: SettingInstanceName, Value: "acme"},
				{Key: SettingWelcomeText, Value: ""},
			},
			want: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "arca.db")
			seedSettings(t, path, testCase.existing)

			store, err := Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { store.Close() })

			if got := store.WelcomeText(); got != testCase.want {
				t.Fatalf("WelcomeText() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// A database created by this build has no settings table when the migration
// runs, and the setup wizard writes the welcome text instead.
func TestOpenLeavesAFreshDatabaseAlone(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "arca.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if got := store.WelcomeText(); got != "" {
		t.Fatalf("WelcomeText() = %q, want it empty", got)
	}
}
