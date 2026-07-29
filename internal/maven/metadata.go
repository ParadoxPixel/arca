package maven

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

func FormatTimestamp(epochMillis int64) string {
	return time.UnixMilli(epochMillis).UTC().Format("20060102150405")
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func escapeXML(value string) string { return xmlEscaper.Replace(value) }

type ArtifactMetadata struct {
	GroupID     string
	ArtifactID  string
	Versions    []string
	LastUpdated int64
}

func RenderArtifactMetadata(input ArtifactMetadata) string {
	versions := SortVersions(input.Versions)
	latest, release := LatestAndRelease(versions)

	var out strings.Builder
	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	out.WriteString("<metadata>\n")
	out.WriteString("  <groupId>" + escapeXML(input.GroupID) + "</groupId>\n")
	out.WriteString("  <artifactId>" + escapeXML(input.ArtifactID) + "</artifactId>\n")
	out.WriteString("  <versioning>\n")
	if latest != "" {
		out.WriteString("    <latest>" + escapeXML(latest) + "</latest>\n")
	}
	if release != "" {
		out.WriteString("    <release>" + escapeXML(release) + "</release>\n")
	}
	out.WriteString("    <versions>\n")
	for _, version := range versions {
		out.WriteString("      <version>" + escapeXML(version) + "</version>\n")
	}
	out.WriteString("    </versions>\n")
	out.WriteString("    <lastUpdated>" + FormatTimestamp(input.LastUpdated) + "</lastUpdated>\n")
	out.WriteString("  </versioning>\n</metadata>\n")
	return out.String()
}

type SnapshotEntry struct {
	Classifier  string
	Extension   string
	Value       string
	Updated     int64
	Timestamp   string
	BuildNumber int
}

type VersionMetadata struct {
	GroupID     string
	ArtifactID  string
	Version     string
	Entries     []SnapshotEntry
	LastUpdated int64
}

func newestSnapshot(entries []SnapshotEntry) (SnapshotEntry, bool) {
	timestamped := make([]SnapshotEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Timestamp != "" {
			timestamped = append(timestamped, entry)
		}
	}
	if len(timestamped) == 0 {
		return SnapshotEntry{}, false
	}

	sort.SliceStable(timestamped, func(i, j int) bool {
		if timestamped[i].Timestamp != timestamped[j].Timestamp {
			return timestamped[i].Timestamp < timestamped[j].Timestamp
		}
		return timestamped[i].BuildNumber < timestamped[j].BuildNumber
	})
	return timestamped[len(timestamped)-1], true
}

func RenderVersionMetadata(input VersionMetadata) string {
	var out strings.Builder
	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	out.WriteString("<metadata modelVersion=\"1.1.0\">\n")
	out.WriteString("  <groupId>" + escapeXML(input.GroupID) + "</groupId>\n")
	out.WriteString("  <artifactId>" + escapeXML(input.ArtifactID) + "</artifactId>\n")
	out.WriteString("  <version>" + escapeXML(input.Version) + "</version>\n")
	out.WriteString("  <versioning>\n    <snapshot>\n")

	if newest, ok := newestSnapshot(input.Entries); ok {
		out.WriteString("      <timestamp>" + newest.Timestamp + "</timestamp>\n")
		out.WriteString("      <buildNumber>" + strconv.Itoa(newest.BuildNumber) + "</buildNumber>\n")
	} else {
		out.WriteString("      <localCopy>true</localCopy>\n")
	}

	out.WriteString("    </snapshot>\n")
	out.WriteString("    <lastUpdated>" + FormatTimestamp(input.LastUpdated) + "</lastUpdated>\n")

	if len(input.Entries) > 0 {
		entries := append([]SnapshotEntry(nil), input.Entries...)
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Extension != entries[j].Extension {
				return entries[i].Extension < entries[j].Extension
			}
			return entries[i].Classifier < entries[j].Classifier
		})

		out.WriteString("    <snapshotVersions>\n")
		for _, entry := range entries {
			out.WriteString("      <snapshotVersion>\n")
			if entry.Classifier != "" {
				out.WriteString("        <classifier>" + escapeXML(entry.Classifier) + "</classifier>\n")
			}
			out.WriteString("        <extension>" + escapeXML(entry.Extension) + "</extension>\n")
			out.WriteString("        <value>" + escapeXML(entry.Value) + "</value>\n")
			out.WriteString("        <updated>" + FormatTimestamp(entry.Updated) + "</updated>\n")
			out.WriteString("      </snapshotVersion>\n")
		}
		out.WriteString("    </snapshotVersions>\n")
	}

	out.WriteString("  </versioning>\n</metadata>\n")
	return out.String()
}
