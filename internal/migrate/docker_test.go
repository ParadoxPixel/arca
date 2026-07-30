package migrate

import (
	"strings"
	"testing"
)

func TestDockerPath(t *testing.T) {
	hex := strings.Repeat("ab", 32)
	digest := "sha256:" + hex

	cases := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		// The shared blob store is what a real Nexus 3.70 uses, and the only shape its
		// asset listing showed.
		{name: "a shared blob", path: "v2/-/blobs/" + digest, want: "_blobs/sha256/" + hex, ok: true},
		{name: "a leading slash is tolerated", path: "/v2/-/blobs/" + digest, want: "_blobs/sha256/" + hex, ok: true},

		// Kept because other versions may file blobs per image, and a digest names the
		// same bytes either way.
		{name: "a per-image blob", path: "v2/nginx/blobs/" + digest, want: "_blobs/sha256/" + hex, ok: true},
		{name: "a per-image blob under a namespace", path: "v2/team/api/blobs/" + digest, want: "_blobs/sha256/" + hex, ok: true},

		{
			name: "a manifest by digest", path: "v2/team/api/manifests/" + digest,
			want: "team/api/_manifests/sha256/" + hex, ok: true,
		},
		{
			name: "a manifest by tag", path: "v2/team/api/manifests/1.0",
			want: "team/api/1.0/manifest.json", ok: true,
		},
		{
			name: "a deep image name", path: "v2/a/b/c/manifests/latest",
			want: "a/b/c/latest/manifest.json", ok: true,
		},

		{name: "nothing", path: ""},
		{name: "no v2 prefix", path: "team/api/manifests/1.0"},
		{name: "the tag list", path: "v2/team/api/tags/list"},
		{name: "an unknown verb", path: "v2/team/api/layers/1"},
		{name: "a nameless manifest", path: "v2/manifests/1.0"},
		{name: "an uppercase image", path: "v2/TEAM/api/manifests/1.0"},
		{name: "an unusable digest", path: "v2/-/blobs/sha256:short"},
		{name: "an unusable algorithm", path: "v2/-/blobs/md5:" + strings.Repeat("ab", 16)},
		{name: "an invalid tag", path: "v2/team/api/manifests/.hidden"},
		{name: "a traversal attempt", path: "v2/team/api/manifests/sha256:../../etc/passwd"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := DockerPath(testCase.path)
			if ok != testCase.ok {
				t.Fatalf("DockerPath(%q) ok = %v, want %v", testCase.path, ok, testCase.ok)
			}
			if ok && got != testCase.want {
				t.Fatalf("DockerPath(%q) = %q, want %q", testCase.path, got, testCase.want)
			}
		})
	}
}

// Only docker rewrites paths. Every other format stores what Nexus stored, and quietly
// changing one of those would move artifacts out from under the builds that resolve them.
func TestTranslatePathLeavesOtherFormatsAlone(t *testing.T) {
	cases := []struct {
		format string
		path   string
	}{
		{"maven2", "com/example/app/1.0/app-1.0.jar"},
		{"npm", "@scope/package/1.0.0/package-1.0.0.tgz"},
		{"p2", "logging/2.0/plugins/slf4j.api_2.0.16.jar"},
	}

	for _, testCase := range cases {
		t.Run(testCase.format, func(t *testing.T) {
			decision := Decision{Format: testCase.format}

			got, ok := decision.TranslatePath(testCase.path)
			if !ok || got != testCase.path {
				t.Fatalf("TranslatePath(%q) = (%q, %v), want it unchanged", testCase.path, got, ok)
			}
			if decision.CopyComponents() {
				t.Fatal("a non-docker format asked for the component walk")
			}
		})
	}
}
