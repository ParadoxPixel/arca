package docker

import (
	"strings"
	"testing"

	"arca/internal/format"
)

func TestStoragePaths(t *testing.T) {
	digest := SHA256(strings.Repeat("ab", 32))

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"blob", BlobPath(digest), "_blobs/sha256/" + digest.Hex},
		{"manifest", ManifestPath("erc/research-grant", digest), "erc/research-grant/_manifests/sha256/" + digest.Hex},
		{"tag", TagPath("erc/research-grant", "0.1.8"), "erc/research-grant/0.1.8/manifest.json"},
		{"tag directory", TagDirectory("nginx", "latest"), "nginx/latest"},
		{"unscoped tag", TagPath("nginx", "latest"), "nginx/latest/manifest.json"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("got %q, want %q", testCase.got, testCase.want)
			}
		})
	}
}

// Every path this package writes has to survive the server's own path guard, or
// it would be stored under a key the blob store refuses to resolve.
func TestStoragePathsAreSafe(t *testing.T) {
	digest := SHA256(strings.Repeat("ab", 32))

	paths := []string{
		BlobPath(digest),
		ManifestPath("erc/research-grant", digest),
		TagPath("erc/research-grant", "0.1.8-unittest-staging"),
		TagPath("nginx", "_internal"),
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if !format.IsSafe(path) {
				t.Fatalf("format.IsSafe(%q) = false, want true", path)
			}
		})
	}
}

func TestParseTagPath(t *testing.T) {
	digest := SHA256(strings.Repeat("ab", 32))

	cases := []struct {
		path  string
		image string
		tag   string
		ok    bool
	}{
		{"nginx/latest/manifest.json", "nginx", "latest", true},
		{"erc/research-grant/0.1.8-unittest-staging/manifest.json", "erc/research-grant", "0.1.8-unittest-staging", true},
		{"a/b/c/1.0/manifest.json", "a/b/c", "1.0", true},

		{"", "", "", false},
		{"nginx/manifest.json", "", "", false},
		{"manifest.json", "", "", false},
		{"nginx/latest/other.json", "", "", false},
		{"nginx/latest/manifest.json/extra", "", "", false},

		// The reserved stores must not read back as a tag.
		{BlobPath(digest), "", "", false},
		{ManifestPath("nginx", digest), "", "", false},
		{"_blobs/sha256/manifest.json", "", "", false},
		{"nginx/_manifests/manifest.json", "", "", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			image, tag, ok := ParseTagPath(testCase.path)
			if ok != testCase.ok {
				t.Fatalf("ParseTagPath(%q) ok = %v, want %v", testCase.path, ok, testCase.ok)
			}
			if !ok {
				return
			}
			if image != testCase.image || tag != testCase.tag {
				t.Fatalf("ParseTagPath(%q) = (%q, %q), want (%q, %q)", testCase.path, image, tag, testCase.image, testCase.tag)
			}
		})
	}
}

// A round trip is what keeps Coordinates and the write path from drifting apart.
func TestTagPathRoundTrip(t *testing.T) {
	cases := [][2]string{
		{"nginx", "latest"},
		{"erc/research-grant", "0.1.8-unittest-staging"},
		{"a/b/c", "1.25-alpine"},
		{"library/postgres", "16"},
	}

	for _, testCase := range cases {
		image, tag := testCase[0], testCase[1]
		t.Run(image+":"+tag, func(t *testing.T) {
			parsedImage, parsedTag, ok := ParseTagPath(TagPath(image, tag))
			if !ok {
				t.Fatalf("ParseTagPath(TagPath(%q, %q)) reported not ok", image, tag)
			}
			if parsedImage != image || parsedTag != tag {
				t.Fatalf("round trip gave (%q, %q), want (%q, %q)", parsedImage, parsedTag, image, tag)
			}
		})
	}
}

func TestSplitImage(t *testing.T) {
	cases := []struct {
		image     string
		namespace string
		name      string
		ok        bool
	}{
		{"nginx", "", "nginx", true},
		{"erc/research-grant", "erc", "research-grant", true},
		{"a/b/c", "a/b", "c", true},

		{"", "", "", false},
		{"Nginx", "", "", false},
		{"a//b", "", "", false},
		{"_blobs", "", "", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.image, func(t *testing.T) {
			namespace, name, ok := SplitImage(testCase.image)
			if ok != testCase.ok {
				t.Fatalf("SplitImage(%q) ok = %v, want %v", testCase.image, ok, testCase.ok)
			}
			if !ok {
				return
			}
			if namespace != testCase.namespace || name != testCase.name {
				t.Fatalf("SplitImage(%q) = (%q, %q), want (%q, %q)", testCase.image, namespace, name, testCase.namespace, testCase.name)
			}
			if rebuilt := ImageName(namespace, name); rebuilt != testCase.image {
				t.Fatalf("ImageName(%q, %q) = %q, want %q", namespace, name, rebuilt, testCase.image)
			}
		})
	}
}

// The sweep identifies the content-addressed stores by path, because neither has
// coordinates and so neither is reachable through ParseTagPath.
func TestRecognisingTheContentStores(t *testing.T) {
	digest := SHA256(strings.Repeat("ab", 32))

	cases := []struct {
		name     string
		path     string
		blob     bool
		manifest bool
	}{
		{name: "a blob", path: BlobPath(digest), blob: true},
		{name: "a digest manifest", path: ManifestPath("nginx", digest), manifest: true},
		{name: "a namespaced digest manifest", path: ManifestPath("erc/research-grant", digest), manifest: true},

		{name: "a tag manifest", path: TagPath("nginx", "latest")},
		{name: "nothing", path: ""},
		{name: "the blob directory itself", path: BlobsDirectory},
		{name: "a blob without an algorithm", path: BlobsDirectory + "/" + digest.Hex},
		{name: "a blob with a short digest", path: BlobsDirectory + "/sha256/abc"},
		{name: "a manifest store with no image", path: ManifestsDirectory + "/sha256/" + digest.Hex},
		{name: "a manifest with an invalid image", path: "NGINX/" + ManifestsDirectory + "/sha256/" + digest.Hex},
		{name: "an unsupported algorithm", path: BlobsDirectory + "/md5/" + strings.Repeat("ab", 16)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsBlobPath(testCase.path); got != testCase.blob {
				t.Fatalf("IsBlobPath(%q) = %v, want %v", testCase.path, got, testCase.blob)
			}
			if got := IsDigestManifestPath(testCase.path); got != testCase.manifest {
				t.Fatalf("IsDigestManifestPath(%q) = %v, want %v", testCase.path, got, testCase.manifest)
			}
		})
	}
}
