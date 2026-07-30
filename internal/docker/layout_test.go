package docker

import (
	"strings"
	"testing"

	"arca/internal/format"
)

func TestLayoutCoordinates(t *testing.T) {
	digest := SHA256(strings.Repeat("ab", 32))

	cases := []struct {
		name string
		path string
		want format.Coordinates
		ok   bool
	}{
		{
			name: "namespaced tag",
			path: "erc/research-grant/0.1.8-unittest-staging/manifest.json",
			want: format.Coordinates{
				Namespace:   "erc",
				Name:        "research-grant",
				Version:     "0.1.8-unittest-staging",
				BaseVersion: "0.1.8",
			},
			ok: true,
		},
		{
			name: "unscoped tag",
			path: "nginx/latest/manifest.json",
			want: format.Coordinates{Namespace: "", Name: "nginx", Version: "latest", BaseVersion: "latest"},
			ok:   true,
		},
		{
			name: "prerelease tag",
			path: "nginx/1.26-rc1/manifest.json",
			want: format.Coordinates{
				Namespace:   "",
				Name:        "nginx",
				Version:     "1.26-rc1",
				BaseVersion: "1.26",
				IsSnapshot:  true,
			},
			ok: true,
		},
		{
			name: "deep namespace",
			path: "a/b/c/1.0/manifest.json",
			want: format.Coordinates{Namespace: "a/b", Name: "c", Version: "1.0", BaseVersion: "1.0"},
			ok:   true,
		},

		// Nothing content-addressed belongs to a version: a blob is shared by
		// every image referencing it and a digest manifest may have no tag.
		{name: "blob", path: BlobPath(digest)},
		{name: "digest manifest", path: ManifestPath("nginx", digest)},
		{name: "bare filename", path: "manifest.json"},
		{name: "no tag directory", path: "nginx/manifest.json"},
		{name: "wrong filename", path: "nginx/latest/config.json"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			coordinates, ok := Layout{}.Coordinates(testCase.path)
			if ok != testCase.ok {
				t.Fatalf("Coordinates(%q) ok = %v, want %v", testCase.path, ok, testCase.ok)
			}
			if !ok {
				return
			}
			if coordinates != testCase.want {
				t.Fatalf("Coordinates(%q) = %+v, want %+v", testCase.path, coordinates, testCase.want)
			}
		})
	}
}

func TestLayoutComponentAt(t *testing.T) {
	cases := []struct {
		prefix    string
		namespace string
		name      string
		ok        bool
	}{
		{"nginx/", "", "nginx", true},
		{"erc/research-grant/", "erc", "research-grant", true},
		{"erc/", "", "erc", true},

		{"", "", "", false},
		{"_blobs/", "", "", false},
		{"nginx/latest/manifest.json", "", "", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.prefix, func(t *testing.T) {
			coordinates, ok := Layout{}.ComponentAt(testCase.prefix)
			if ok != testCase.ok {
				t.Fatalf("ComponentAt(%q) ok = %v, want %v", testCase.prefix, ok, testCase.ok)
			}
			if !ok {
				return
			}
			if coordinates.Namespace != testCase.namespace || coordinates.Name != testCase.name {
				t.Fatalf("ComponentAt(%q) = (%q, %q), want (%q, %q)",
					testCase.prefix, coordinates.Namespace, coordinates.Name, testCase.namespace, testCase.name)
			}
		})
	}
}

// Mutable is the mirror image of what the other formats report, so it is worth
// pinning: a proxy has to recheck tags and may cache content forever.
func TestLayoutMutable(t *testing.T) {
	layout := Layout{}
	digest := SHA256(strings.Repeat("ab", 32))

	cases := []struct {
		path string
		want bool
	}{
		{"nginx/latest/manifest.json", true},
		{"erc/research-grant/0.1.8/manifest.json", true},

		{BlobPath(digest), false},
		{ManifestPath("nginx", digest), false},
		{"", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			if got := layout.Mutable(testCase.path); got != testCase.want {
				t.Fatalf("Mutable(%q) = %v, want %v", testCase.path, got, testCase.want)
			}
		})
	}
}

func TestLayoutContentType(t *testing.T) {
	layout := Layout{}

	cases := []struct {
		filename string
		want     string
	}{
		{ManifestFilename, MediaTypeOCIManifest},
		{strings.Repeat("ab", 32), "application/octet-stream"},
	}

	for _, testCase := range cases {
		t.Run(testCase.filename, func(t *testing.T) {
			if got := layout.ContentType(testCase.filename); got != testCase.want {
				t.Fatalf("ContentType(%q) = %q, want %q", testCase.filename, got, testCase.want)
			}
		})
	}
}

// Layout has to satisfy the seam every other format is served through.
var _ format.Layout = Layout{}
