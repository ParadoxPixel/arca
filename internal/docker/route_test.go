package docker

import (
	"net/http"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)

	cases := []struct {
		name      string
		method    string
		path      string
		kind      RouteKind
		imageName string
		tag       string
		upload    string
	}{
		{name: "version check", method: http.MethodGet, path: "/", kind: RouteBase},
		{name: "version check without a slash", method: http.MethodGet, path: "", kind: RouteBase},
		{name: "catalog", method: http.MethodGet, path: "/_catalog", kind: RouteCatalog},

		{
			name: "manifest by tag", method: http.MethodGet, path: "/images/team/api/manifests/1.4.0",
			kind: RouteManifest, imageName: "images/team/api", tag: "1.4.0",
		},
		{
			name: "manifest by digest", method: http.MethodGet, path: "/nginx/manifests/" + digest,
			kind: RouteManifest, imageName: "nginx",
		},
		{
			name: "blob", method: http.MethodGet, path: "/nginx/blobs/" + digest,
			kind: RouteBlob, imageName: "nginx",
		},
		{
			name: "tag list", method: http.MethodGet, path: "/images/team/api/tags/list",
			kind: RouteTags, imageName: "images/team/api",
		},
		{
			name: "referrers", method: http.MethodGet, path: "/nginx/referrers/" + digest,
			kind: RouteReferrers, imageName: "nginx",
		},
		{
			name: "start an upload", method: http.MethodPost, path: "/images/app/blobs/uploads/",
			kind: RouteUploadStart, imageName: "images/app",
		},
		{
			name: "start an upload without the trailing slash", method: http.MethodPost, path: "/images/app/blobs/uploads",
			kind: RouteUploadStart, imageName: "images/app",
		},
		{
			name: "an upload session", method: http.MethodPatch, path: "/images/app/blobs/uploads/abc123",
			kind: RouteUpload, imageName: "images/app", upload: "abc123",
		},

		{name: "an unknown verb", method: http.MethodGet, path: "/nginx/layers/1", kind: RouteUnknown},
		{name: "a blob addressed by tag", method: http.MethodGet, path: "/nginx/blobs/latest", kind: RouteUnknown},
		{name: "a bad tag", method: http.MethodGet, path: "/nginx/manifests/.hidden", kind: RouteUnknown},
		{name: "an invalid name", method: http.MethodGet, path: "/NGINX/manifests/1.0", kind: RouteUnknown},
		{name: "a nameless manifest", method: http.MethodGet, path: "/manifests/1.0", kind: RouteUnknown},
		{name: "an unlisted tag endpoint", method: http.MethodGet, path: "/nginx/tags/all", kind: RouteUnknown},
		{name: "a single segment", method: http.MethodGet, path: "/nginx", kind: RouteUnknown},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			route := Resolve(testCase.method, testCase.path)

			if route.Kind != testCase.kind {
				t.Fatalf("Resolve(%q, %q).Kind = %q, want %q", testCase.method, testCase.path, route.Kind, testCase.kind)
			}
			if testCase.kind == RouteUnknown {
				return
			}
			if route.Name != testCase.imageName {
				t.Fatalf("Name = %q, want %q", route.Name, testCase.imageName)
			}
			if route.Tag != testCase.tag {
				t.Fatalf("Tag = %q, want %q", route.Tag, testCase.tag)
			}
			if route.Upload != testCase.upload {
				t.Fatalf("Upload = %q, want %q", route.Upload, testCase.upload)
			}
		})
	}
}

// A name component may legally be "manifests" or "blobs", so a resolver that
// searched forwards for the verb would cut the name in the wrong place.
func TestResolveMatchesFromTheEnd(t *testing.T) {
	digest := "sha256:" + strings.Repeat("cd", 32)

	cases := []struct {
		name      string
		path      string
		kind      RouteKind
		imageName string
		reference string
	}{
		{
			name: "a name containing manifests", path: "/repo/manifests/app/manifests/1.0",
			kind: RouteManifest, imageName: "repo/manifests/app", reference: "1.0",
		},
		{
			name: "a name containing blobs", path: "/repo/blobs/app/manifests/1.0",
			kind: RouteManifest, imageName: "repo/blobs/app", reference: "1.0",
		},
		{
			name: "a name containing tags", path: "/repo/tags/app/blobs/" + digest,
			kind: RouteBlob, imageName: "repo/tags/app", reference: digest,
		},
		{
			name: "a name ending in uploads", path: "/repo/uploads/blobs/uploads/session1",
			kind: RouteUpload, imageName: "repo/uploads",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			route := Resolve(http.MethodGet, testCase.path)

			if route.Kind != testCase.kind {
				t.Fatalf("Kind = %q, want %q", route.Kind, testCase.kind)
			}
			if route.Name != testCase.imageName {
				t.Fatalf("Name = %q, want %q", route.Name, testCase.imageName)
			}
			if testCase.reference != "" && route.Reference != testCase.reference && route.Digest.String() != testCase.reference {
				t.Fatalf("Reference = %q, want %q", route.Reference, testCase.reference)
			}
		})
	}
}

func TestResolveDigestReferences(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ef", 32)

	t.Run("a manifest addressed by digest reports ByDigest", func(t *testing.T) {
		route := Resolve(http.MethodGet, "/nginx/manifests/"+digest)
		if !route.ByDigest {
			t.Fatal("ByDigest = false, want true")
		}
		if route.Digest.String() != digest {
			t.Fatalf("Digest = %q, want %q", route.Digest.String(), digest)
		}
		if route.Tag != "" {
			t.Fatalf("Tag = %q, want empty", route.Tag)
		}
	})

	t.Run("a manifest addressed by tag does not", func(t *testing.T) {
		route := Resolve(http.MethodGet, "/nginx/manifests/1.25")
		if route.ByDigest {
			t.Fatal("ByDigest = true, want false")
		}
		if route.Tag != "1.25" {
			t.Fatalf("Tag = %q, want 1.25", route.Tag)
		}
	})
}
