package npm

import (
	"encoding/json"
	"strings"
	"testing"
)

const mirroredPackument = `{
	"name": "@types/node",
	"dist-tags": {"latest": "20.1.0"},
	"readme": "a very long readme",
	"versions": {
		"20.1.0": {
			"name": "@types/node",
			"version": "20.1.0",
			"dependencies": {"undici-types": "~5.26.4"},
			"scripts": {"test": "tsc"},
			"dist": {
				"tarball": "https://cdn.example.test/downloads/@types/node/-/node-20.1.0.tgz",
				"integrity": "sha512-abc",
				"shasum": "deadbeef"
			}
		},
		"19.0.0": {
			"name": "@types/node",
			"version": "19.0.0",
			"dist": {"tarball": "https://cdn.example.test/downloads/@types/node/-/node-19.0.0.tgz"}
		}
	}
}`

func rewriteFixture(t *testing.T, options RewriteOptions) map[string]json.RawMessage {
	t.Helper()

	rewritten, err := Rewrite(strings.NewReader(mirroredPackument), options)
	if err != nil {
		t.Fatalf("rewriting: %v", err)
	}

	document := map[string]json.RawMessage{}
	if err := json.Unmarshal(rewritten, &document); err != nil {
		t.Fatalf("decoding the rewritten document: %v", err)
	}
	return document
}

func tarballOf(t *testing.T, document map[string]json.RawMessage, version string) string {
	t.Helper()

	var versions map[string]struct {
		Dist struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	if err := json.Unmarshal(document["versions"], &versions); err != nil {
		t.Fatalf("decoding versions: %v", err)
	}
	return versions[version].Dist.Tarball
}

func TestRewritePointsTarballsAtThisServer(t *testing.T) {
	const base = "https://repo.example.test/repository/npm-proxy"
	document := rewriteFixture(t, RewriteOptions{Package: "@types/node", Base: base})

	testCases := []struct {
		version string
		want    string
	}{
		{"20.1.0", base + "/@types/node/-/node-20.1.0.tgz"},
		{"19.0.0", base + "/@types/node/-/node-19.0.0.tgz"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.version, func(t *testing.T) {
			if got := tarballOf(t, document, testCase.version); got != testCase.want {
				t.Fatalf("tarball = %q, want %q", got, testCase.want)
			}
		})
	}
}

// The upstream tarball host differs from the registry host here, which is what a
// string replace of the base URL would silently miss.
func TestRewriteLeavesNoUpstreamTarballHost(t *testing.T) {
	rewritten, err := Rewrite(strings.NewReader(mirroredPackument), RewriteOptions{
		Package: "@types/node",
		Base:    "https://repo.example.test/repository/npm-proxy",
	})
	if err != nil {
		t.Fatalf("rewriting: %v", err)
	}
	if strings.Contains(string(rewritten), "cdn.example.test") {
		t.Fatalf("the upstream host survived the rewrite: %s", rewritten)
	}
}

func TestRewriteKeepsTheRestOfDist(t *testing.T) {
	document := rewriteFixture(t, RewriteOptions{Package: "@types/node", Base: "https://repo.example.test"})

	var versions map[string]struct {
		Dist map[string]string `json:"dist"`
	}
	if err := json.Unmarshal(document["versions"], &versions); err != nil {
		t.Fatalf("decoding versions: %v", err)
	}

	dist := versions["20.1.0"].Dist
	if dist["integrity"] != "sha512-abc" {
		t.Fatalf("integrity = %q, want sha512-abc", dist["integrity"])
	}
	if dist["shasum"] != "deadbeef" {
		t.Fatalf("shasum = %q, want deadbeef", dist["shasum"])
	}
}

func TestRewriteFullDocumentKeepsUnrelatedFields(t *testing.T) {
	document := rewriteFixture(t, RewriteOptions{Package: "@types/node", Base: "https://repo.example.test"})

	for _, field := range []string{"name", "dist-tags", "readme", "versions"} {
		t.Run(field, func(t *testing.T) {
			if _, present := document[field]; !present {
				t.Fatalf("%q was dropped from a full packument", field)
			}
		})
	}
}

func TestRewriteAbbreviatedKeepsOnlyTheInstallFields(t *testing.T) {
	document := rewriteFixture(t, RewriteOptions{
		Package:     "@types/node",
		Base:        "https://repo.example.test",
		Abbreviated: true,
	})

	testCases := []struct {
		field string
		want  bool
	}{
		{"name", true},
		{"dist-tags", true},
		{"versions", true},
		{"readme", false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.field, func(t *testing.T) {
			if _, present := document[testCase.field]; present != testCase.want {
				t.Fatalf("%q present = %v, want %v", testCase.field, present, testCase.want)
			}
		})
	}

	var versions map[string]map[string]json.RawMessage
	if err := json.Unmarshal(document["versions"], &versions); err != nil {
		t.Fatalf("decoding versions: %v", err)
	}

	manifest := versions["20.1.0"]
	for _, field := range []string{"name", "version", "dependencies", "dist"} {
		t.Run("version."+field, func(t *testing.T) {
			if _, present := manifest[field]; !present {
				t.Fatalf("%q was dropped from an abbreviated manifest", field)
			}
		})
	}
	t.Run("version.scripts", func(t *testing.T) {
		if _, present := manifest["scripts"]; present {
			t.Fatalf("scripts survived into an abbreviated manifest")
		}
	})
}

func TestRewriteDropsVersionsItCannotServe(t *testing.T) {
	const document = `{"name":"left-pad","versions":{"1.0.0":{"dist":{}},"../escape":{"dist":{}}}}`

	rewritten, err := Rewrite(strings.NewReader(document), RewriteOptions{
		Package: "left-pad",
		Base:    "https://repo.example.test",
	})
	if err != nil {
		t.Fatalf("rewriting: %v", err)
	}

	var decoded struct {
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(rewritten, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(decoded.Versions) != 1 {
		t.Fatalf("kept %d versions, want 1: %s", len(decoded.Versions), rewritten)
	}
	if _, present := decoded.Versions["1.0.0"]; !present {
		t.Fatalf("the valid version was dropped: %s", rewritten)
	}
}

func TestRewriteRejectsAnUnusablePackageName(t *testing.T) {
	if _, err := Rewrite(strings.NewReader(`{}`), RewriteOptions{Package: "@scope"}); err == nil {
		t.Fatal("rewriting accepted an unusable package name")
	}
}

func TestWantsAbbreviated(t *testing.T) {
	testCases := []struct {
		name   string
		accept string
		want   bool
	}{
		{"install document", "application/vnd.npm.install-v1+json", true},
		{"install document with a fallback", "application/vnd.npm.install-v1+json; q=1.0, */*; q=0.1", true},
		{"plain json", "application/json", false},
		{"anything", "*/*", false},
		{"absent", "", false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := WantsAbbreviated(testCase.accept); got != testCase.want {
				t.Fatalf("got %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestDistTagsOf(t *testing.T) {
	testCases := []struct {
		name     string
		document string
		want     string
	}{
		{"present", `{"dist-tags":{"latest":"1.2.3"}}`, "1.2.3"},
		{"absent", `{"name":"left-pad"}`, ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tags, err := DistTagsOf([]byte(testCase.document))
			if err != nil {
				t.Fatalf("reading dist-tags: %v", err)
			}
			if got := tags["latest"]; got != testCase.want {
				t.Fatalf("latest = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestVersionOf(t *testing.T) {
	const document = `{"versions":{"1.0.0":{"name":"left-pad"}}}`

	testCases := []struct {
		version string
		want    bool
	}{
		{"1.0.0", true},
		{"2.0.0", false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.version, func(t *testing.T) {
			_, ok := VersionOf([]byte(document), testCase.version)
			if ok != testCase.want {
				t.Fatalf("found = %v, want %v", ok, testCase.want)
			}
		})
	}
}
