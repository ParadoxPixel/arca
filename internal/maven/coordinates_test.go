package maven

import "testing"

func TestSplitChecksum(t *testing.T) {
	cases := []struct {
		filename string
		base     string
		checksum string
	}{
		{"app-1.0.jar", "app-1.0.jar", ""},
		{"app-1.0.jar.sha1", "app-1.0.jar", "sha1"},
		{"app-1.0.jar.md5", "app-1.0.jar", "md5"},
		{"app-1.0.jar.sha256", "app-1.0.jar", "sha256"},
		{"app-1.0.jar.sha512", "app-1.0.jar", "sha512"},
		{"app-1.0.jar.asc", "app-1.0.jar.asc", ""},
		{"maven-metadata.xml.sha1", "maven-metadata.xml", "sha1"},
	}

	for _, testCase := range cases {
		t.Run(testCase.filename, func(t *testing.T) {
			base, checksum := SplitChecksum(testCase.filename)
			if base != testCase.base || checksum != testCase.checksum {
				t.Fatalf("got (%q, %q), want (%q, %q)", base, checksum, testCase.base, testCase.checksum)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	cases := []struct {
		path string
		want ParsedPath
	}{
		{
			path: "com/example/app/1.0.0/app-1.0.0.jar",
			want: ParsedPath{Kind: KindArtifact, GroupID: "com.example", ArtifactID: "app", Version: "1.0.0", BaseVersion: "1.0.0"},
		},
		{
			path: "org/acme/tools/core/2.1/core-2.1-sources.jar",
			want: ParsedPath{Kind: KindArtifact, GroupID: "org.acme.tools", ArtifactID: "core", Version: "2.1", BaseVersion: "2.1"},
		},
		{
			path: "com/example/app/1.0.0-SNAPSHOT/app-1.0.0-20240115.101500-3.jar",
			want: ParsedPath{Kind: KindArtifact, GroupID: "com.example", ArtifactID: "app", Version: "1.0.0-SNAPSHOT", BaseVersion: "1.0.0", IsSnapshot: true},
		},
		{
			path: "com/example/app/maven-metadata.xml",
			want: ParsedPath{Kind: KindArtifactMetadata, GroupID: "com.example", ArtifactID: "app"},
		},
		{
			path: "com/example/app/1.0.0-SNAPSHOT/maven-metadata.xml",
			want: ParsedPath{Kind: KindVersionMetadata, GroupID: "com.example", ArtifactID: "app", Version: "1.0.0-SNAPSHOT", BaseVersion: "1.0.0"},
		},
		{path: "com/example/app/1.0.0/unrelated.txt", want: ParsedPath{Kind: KindRaw}},
		{path: "maven-metadata.xml", want: ParsedPath{Kind: KindRaw}},
	}

	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			got := ParsePath(testCase.path)
			if got.Kind != testCase.want.Kind {
				t.Fatalf("kind = %q, want %q", got.Kind, testCase.want.Kind)
			}
			if got.GroupID != testCase.want.GroupID || got.ArtifactID != testCase.want.ArtifactID {
				t.Fatalf("coordinates = %q:%q, want %q:%q", got.GroupID, got.ArtifactID, testCase.want.GroupID, testCase.want.ArtifactID)
			}
			if got.Version != testCase.want.Version || got.BaseVersion != testCase.want.BaseVersion {
				t.Fatalf("version = %q (base %q), want %q (base %q)", got.Version, got.BaseVersion, testCase.want.Version, testCase.want.BaseVersion)
			}
			if got.IsSnapshot != testCase.want.IsSnapshot {
				t.Fatalf("isSnapshot = %v, want %v", got.IsSnapshot, testCase.want.IsSnapshot)
			}
		})
	}
}

func TestParseFilename(t *testing.T) {
	cases := []struct {
		artifactID string
		version    string
		filename   string
		want       FileIdentity
	}{
		{"app", "1.0.0", "app-1.0.0.jar", FileIdentity{VersionPart: "1.0.0", Extension: "jar", BuildNumber: -1}},
		{"app", "1.0.0", "app-1.0.0.pom", FileIdentity{VersionPart: "1.0.0", Extension: "pom", BuildNumber: -1}},
		{"app", "1.0.0", "app-1.0.0-sources.jar", FileIdentity{VersionPart: "1.0.0", Classifier: "sources", Extension: "jar", BuildNumber: -1}},
		{"app", "1.0.0", "app-1.0.0-bin.tar.gz", FileIdentity{VersionPart: "1.0.0", Classifier: "bin", Extension: "tar.gz", BuildNumber: -1}},
		{
			"app", "1.0.0-SNAPSHOT", "app-1.0.0-20240115.101500-3.jar",
			FileIdentity{VersionPart: "1.0.0-20240115.101500-3", Extension: "jar", Timestamp: "20240115.101500", BuildNumber: 3},
		},
		{
			"app", "1.0.0-SNAPSHOT", "app-1.0.0-20240115.101500-3-javadoc.jar",
			FileIdentity{VersionPart: "1.0.0-20240115.101500-3", Classifier: "javadoc", Extension: "jar", Timestamp: "20240115.101500", BuildNumber: 3},
		},
		{"app", "1.0.0-SNAPSHOT", "app-1.0.0-SNAPSHOT.jar", FileIdentity{VersionPart: "1.0.0-SNAPSHOT", Extension: "jar", BuildNumber: -1}},
	}

	for _, testCase := range cases {
		t.Run(testCase.filename, func(t *testing.T) {
			got, ok := ParseFilename(testCase.artifactID, testCase.version, testCase.filename)
			if !ok {
				t.Fatal("expected the filename to parse")
			}
			if got != testCase.want {
				t.Fatalf("got %+v, want %+v", got, testCase.want)
			}
		})
	}
}

func TestParseFilenameRejects(t *testing.T) {
	cases := []struct {
		artifactID string
		version    string
		filename   string
	}{
		{"app", "1.0.0", "other-1.0.0.jar"},
		{"app", "1.0.0", "app-2.0.0.jar"},
		{"app", "1.0.0", "app-1.0.0"},
	}

	for _, testCase := range cases {
		t.Run(testCase.filename, func(t *testing.T) {
			if _, ok := ParseFilename(testCase.artifactID, testCase.version, testCase.filename); ok {
				t.Fatal("expected the filename to be rejected")
			}
		})
	}
}
