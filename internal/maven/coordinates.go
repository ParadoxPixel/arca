package maven

import (
	"regexp"
	"strconv"
	"strings"
)

const SnapshotSuffix = "-SNAPSHOT"

const MetadataFilename = "maven-metadata.xml"

var ChecksumExtensions = []string{"sha512", "sha256", "sha1", "md5"}

type PathKind string

const (
	KindRaw              PathKind = "raw"
	KindArtifact         PathKind = "artifact"
	KindArtifactMetadata PathKind = "artifact-metadata"
	KindVersionMetadata  PathKind = "version-metadata"
)

type FileIdentity struct {
	VersionPart string
	Classifier  string
	Extension   string
	Timestamp   string
	BuildNumber int
}

type ParsedPath struct {
	Kind        PathKind
	GroupID     string
	ArtifactID  string
	Version     string
	BaseVersion string
	IsSnapshot  bool
	Filename    string
	File        FileIdentity
}

func SplitChecksum(filename string) (base string, checksum string) {
	for _, extension := range ChecksumExtensions {
		suffix := "." + extension
		if strings.HasSuffix(filename, suffix) && len(filename) > len(suffix) {
			return filename[:len(filename)-len(suffix)], extension
		}
	}
	return filename, ""
}

func StripSnapshot(version string) string {
	return strings.TrimSuffix(version, SnapshotSuffix)
}

func ComponentDirectory(groupID, artifactID, version string) string {
	return strings.ReplaceAll(groupID, ".", "/") + "/" + artifactID + "/" + version
}

func ParseFilename(artifactID, version, filename string) (FileIdentity, bool) {
	prefix := artifactID + "-"
	if !strings.HasPrefix(filename, prefix) {
		return FileIdentity{}, false
	}

	rest := filename[len(prefix):]
	identity := FileIdentity{BuildNumber: -1}

	if strings.HasSuffix(version, SnapshotSuffix) {
		pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(StripSnapshot(version)) + `-(\d{8}\.\d{6})-(\d+)`)
		switch match := pattern.FindStringSubmatch(rest); {
		case match != nil:
			identity.VersionPart = match[0]
			identity.Timestamp = match[1]
			identity.BuildNumber, _ = strconv.Atoi(match[2])
		case strings.HasPrefix(rest, version):
			identity.VersionPart = version
		default:
			return FileIdentity{}, false
		}
	} else {
		if !strings.HasPrefix(rest, version) {
			return FileIdentity{}, false
		}
		identity.VersionPart = version
	}

	tail := rest[len(identity.VersionPart):]

	if strings.HasPrefix(tail, "-") {
		dot := strings.Index(tail, ".")
		if dot < 0 {
			return FileIdentity{}, false
		}
		identity.Classifier = tail[1:dot]
		tail = tail[dot:]
	}

	if !strings.HasPrefix(tail, ".") || len(tail) == 1 {
		return FileIdentity{}, false
	}
	identity.Extension = tail[1:]

	return identity, true
}

func ParsePath(path string) ParsedPath {
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return ParsedPath{Kind: KindRaw}
	}

	filename := segments[len(segments)-1]
	base, _ := SplitChecksum(filename)

	if base == MetadataFilename {
		parent := segments[len(segments)-2]
		if len(segments) >= 4 && strings.HasSuffix(parent, SnapshotSuffix) {
			return ParsedPath{
				Kind:        KindVersionMetadata,
				GroupID:     strings.Join(segments[:len(segments)-3], "."),
				ArtifactID:  segments[len(segments)-3],
				Version:     parent,
				BaseVersion: StripSnapshot(parent),
			}
		}
		if len(segments) >= 3 {
			return ParsedPath{
				Kind:       KindArtifactMetadata,
				GroupID:    strings.Join(segments[:len(segments)-2], "."),
				ArtifactID: parent,
			}
		}
		return ParsedPath{Kind: KindRaw}
	}

	if len(segments) < 4 {
		return ParsedPath{Kind: KindRaw}
	}

	version := segments[len(segments)-2]
	artifactID := segments[len(segments)-3]
	file, ok := ParseFilename(artifactID, version, filename)
	if !ok {
		return ParsedPath{Kind: KindRaw}
	}

	return ParsedPath{
		Kind:        KindArtifact,
		GroupID:     strings.Join(segments[:len(segments)-3], "."),
		ArtifactID:  artifactID,
		Version:     version,
		BaseVersion: StripSnapshot(version),
		IsSnapshot:  strings.HasSuffix(version, SnapshotSuffix),
		Filename:    filename,
		File:        file,
	}
}
