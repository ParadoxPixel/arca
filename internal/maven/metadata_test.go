package maven

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var updated = time.Date(2024, time.January, 15, 10, 15, 0, 0, time.UTC).UnixMilli()

func TestFormatTimestamp(t *testing.T) {
	if got := FormatTimestamp(updated); got != "20240115101500" {
		t.Fatalf("FormatTimestamp = %q, want %q", got, "20240115101500")
	}
}

func TestRenderArtifactMetadataFragments(t *testing.T) {
	xml := RenderArtifactMetadata(ArtifactMetadata{
		GroupID:     "com.example",
		ArtifactID:  "app",
		Versions:    []string{"1.0.0", "2.0.0-SNAPSHOT", "1.2.0"},
		LastUpdated: updated,
	})

	fragments := []string{
		"<groupId>com.example</groupId>",
		"<artifactId>app</artifactId>",
		"<latest>2.0.0-SNAPSHOT</latest>",
		"<release>1.2.0</release>",
		"<lastUpdated>20240115101500</lastUpdated>",
	}

	for _, fragment := range fragments {
		t.Run(fragment, func(t *testing.T) {
			if !strings.Contains(xml, fragment) {
				t.Fatalf("metadata is missing %s:\n%s", fragment, xml)
			}
		})
	}
}

func TestRenderArtifactMetadataOrdersVersions(t *testing.T) {
	xml := RenderArtifactMetadata(ArtifactMetadata{
		GroupID:     "com.example",
		ArtifactID:  "app",
		Versions:    []string{"1.0.0", "2.0.0-SNAPSHOT", "1.2.0"},
		LastUpdated: updated,
	})

	got := regexp.MustCompile(`<version>[^<]+</version>`).FindAllString(xml, -1)
	want := []string{"<version>1.0.0</version>", "<version>1.2.0</version>", "<version>2.0.0-SNAPSHOT</version>"}

	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRenderArtifactMetadataOmitsReleaseForSnapshotsOnly(t *testing.T) {
	xml := RenderArtifactMetadata(ArtifactMetadata{
		GroupID:     "com.example",
		ArtifactID:  "app",
		Versions:    []string{"1.0.0-SNAPSHOT"},
		LastUpdated: updated,
	})

	if strings.Contains(xml, "<release>") {
		t.Fatalf("expected no release element:\n%s", xml)
	}
}

func TestRenderVersionMetadata(t *testing.T) {
	xml := RenderVersionMetadata(VersionMetadata{
		GroupID:    "com.example",
		ArtifactID: "app",
		Version:    "1.0.0-SNAPSHOT",
		Entries: []SnapshotEntry{
			{Extension: "jar", Value: "1.0.0-20240115.101500-3", Updated: updated, Timestamp: "20240115.101500", BuildNumber: 3},
			{Classifier: "sources", Extension: "jar", Value: "1.0.0-20240115.101500-3", Updated: updated, Timestamp: "20240115.101500", BuildNumber: 3},
		},
		LastUpdated: updated,
	})

	fragments := []string{
		"<version>1.0.0-SNAPSHOT</version>",
		"<timestamp>20240115.101500</timestamp>",
		"<buildNumber>3</buildNumber>",
		"<classifier>sources</classifier>",
		"<value>1.0.0-20240115.101500-3</value>",
	}

	for _, fragment := range fragments {
		t.Run(fragment, func(t *testing.T) {
			if !strings.Contains(xml, fragment) {
				t.Fatalf("metadata is missing %s:\n%s", fragment, xml)
			}
		})
	}
}

func TestRenderVersionMetadataWithoutTimestamps(t *testing.T) {
	xml := RenderVersionMetadata(VersionMetadata{
		GroupID:     "com.example",
		ArtifactID:  "app",
		Version:     "1.0.0-SNAPSHOT",
		Entries:     []SnapshotEntry{{Extension: "jar", Value: "1.0.0-SNAPSHOT", Updated: updated, BuildNumber: -1}},
		LastUpdated: updated,
	})

	if !strings.Contains(xml, "<localCopy>true</localCopy>") {
		t.Fatalf("expected a localCopy marker:\n%s", xml)
	}
}
