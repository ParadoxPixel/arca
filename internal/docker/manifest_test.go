package docker

import (
	"errors"
	"strings"
	"testing"
)

func digestOf(seed string) string {
	return "sha256:" + strings.Repeat(seed, 64/len(seed))
}

const imageManifest = `{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {
    "mediaType": "application/vnd.oci.image.config.v1+json",
    "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
    "size": 2048
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
      "size": 100
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:3333333333333333333333333333333333333333333333333333333333333333",
      "size": 200
    }
  ]
}`

const indexManifest = `{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:4444444444444444444444444444444444444444444444444444444444444444",
      "size": 500,
      "platform": {"os": "linux", "architecture": "amd64"}
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:5555555555555555555555555555555555555555555555555555555555555555",
      "size": 600,
      "platform": {"os": "linux", "architecture": "arm64", "variant": "v8"}
    }
  ]
}`

func TestParseImageManifest(t *testing.T) {
	manifest, err := ParseManifest([]byte(imageManifest))
	if err != nil {
		t.Fatalf("ParseManifest returned %v", err)
	}
	if manifest.IsIndex() {
		t.Fatal("IsIndex() = true, want false")
	}
	if manifest.MediaType != MediaTypeOCIManifest {
		t.Fatalf("MediaType = %q, want %q", manifest.MediaType, MediaTypeOCIManifest)
	}
	if got := manifest.DeclaredSize(); got != 2348 {
		t.Fatalf("DeclaredSize() = %d, want 2348", got)
	}

	references := manifest.References()
	if len(references) != 3 {
		t.Fatalf("References() returned %d entries, want 3", len(references))
	}
	if references[0].Kind != ReferenceConfig {
		t.Fatalf("first reference kind = %q, want %q", references[0].Kind, ReferenceConfig)
	}
	for index, reference := range references[1:] {
		if reference.Kind != ReferenceLayer {
			t.Fatalf("reference %d kind = %q, want %q", index+1, reference.Kind, ReferenceLayer)
		}
		if reference.Position != int64(index) {
			t.Fatalf("layer %d position = %d, want %d", index, reference.Position, index)
		}
	}
}

func TestParseIndexManifest(t *testing.T) {
	manifest, err := ParseManifest([]byte(indexManifest))
	if err != nil {
		t.Fatalf("ParseManifest returned %v", err)
	}
	if !manifest.IsIndex() {
		t.Fatal("IsIndex() = false, want true")
	}
	if got := manifest.DeclaredSize(); got != 1100 {
		t.Fatalf("DeclaredSize() = %d, want 1100", got)
	}

	references := manifest.References()
	if len(references) != 2 {
		t.Fatalf("References() returned %d entries, want 2", len(references))
	}

	platforms := []string{"linux/amd64", "linux/arm64/v8"}
	for index, reference := range references {
		if reference.Kind != ReferenceManifest {
			t.Fatalf("reference %d kind = %q, want %q", index, reference.Kind, ReferenceManifest)
		}
		if reference.Platform != platforms[index] {
			t.Fatalf("reference %d platform = %q, want %q", index, reference.Platform, platforms[index])
		}
	}
}

// A media type is mandatory in Docker schema 2 but was optional in OCI before
// 1.1, so a document without one has to be classified by its shape.
func TestParseManifestInfersMediaType(t *testing.T) {
	cases := []struct {
		name     string
		document string
		want     string
	}{
		{
			name:     "image",
			document: `{"schemaVersion":2,"config":{"digest":"` + digestOf("1") + `","size":1},"layers":[]}`,
			want:     MediaTypeOCIManifest,
		},
		{
			name:     "index",
			document: `{"schemaVersion":2,"manifests":[{"digest":"` + digestOf("4") + `","size":1}]}`,
			want:     MediaTypeOCIIndex,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			manifest, err := ParseManifest([]byte(testCase.document))
			if err != nil {
				t.Fatalf("ParseManifest returned %v", err)
			}
			if manifest.MediaType != testCase.want {
				t.Fatalf("MediaType = %q, want %q", manifest.MediaType, testCase.want)
			}
		})
	}
}

func TestParseManifestRejections(t *testing.T) {
	cases := []struct {
		name     string
		document string
		want     error
	}{
		{
			name:     "schema 1",
			document: `{"schemaVersion":1,"name":"nginx","tag":"latest","fsLayers":[]}`,
			want:     ErrSchemaVersion1,
		},
		{
			name:     "neither shape",
			document: `{"schemaVersion":2}`,
			want:     ErrUnknownShape,
		},
		{
			name:     "both shapes",
			document: `{"schemaVersion":2,"config":{"digest":"` + digestOf("1") + `","size":1},"manifests":[{"digest":"` + digestOf("4") + `","size":1}]}`,
			want:     ErrUnknownShape,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(testCase.document)); !errors.Is(err, testCase.want) {
				t.Fatalf("ParseManifest returned %v, want %v", err, testCase.want)
			}
		})
	}
}

// A descriptor digest that cannot be stored has to be caught at parse time,
// because every one of them becomes a storage path later.
func TestParseManifestRejectsBadDigests(t *testing.T) {
	cases := []struct {
		name     string
		document string
	}{
		{"config", `{"schemaVersion":2,"config":{"digest":"sha256:short","size":1},"layers":[]}`},
		{"layer", `{"schemaVersion":2,"config":{"digest":"` + digestOf("1") + `","size":1},"layers":[{"digest":"nonsense","size":1}]}`},
		{"index child", `{"schemaVersion":2,"manifests":[{"digest":"md5:abc","size":1}]}`},
		{"subject", `{"schemaVersion":2,"config":{"digest":"` + digestOf("1") + `","size":1},"layers":[],"subject":{"digest":"bad","size":1}}`},
		{"traversal", `{"schemaVersion":2,"config":{"digest":"sha256:../../etc/passwd","size":1},"layers":[]}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(testCase.document)); err == nil {
				t.Fatal("ParseManifest accepted a digest it cannot store")
			}
		})
	}
}

func TestParseManifestRejectsGarbage(t *testing.T) {
	for _, document := range []string{"", "not json", "[]", "null"} {
		t.Run(document, func(t *testing.T) {
			if _, err := ParseManifest([]byte(document)); err == nil {
				t.Fatal("ParseManifest accepted a document that is not a manifest")
			}
		})
	}
}

func TestIsManifestMediaType(t *testing.T) {
	cases := []struct {
		mediaType string
		want      bool
	}{
		{MediaTypeOCIManifest, true},
		{MediaTypeOCIIndex, true},
		{MediaTypeDockerManifest, true},
		{MediaTypeDockerList, true},
		{MediaTypeDockerManifestV1, true},
		{MediaTypeOCIManifest + "; charset=utf-8", true},

		{MediaTypeOCIConfig, false},
		{"application/octet-stream", false},
		{"application/json", false},
		{"", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.mediaType, func(t *testing.T) {
			if got := IsManifestMediaType(testCase.mediaType); got != testCase.want {
				t.Fatalf("IsManifestMediaType(%q) = %v, want %v", testCase.mediaType, got, testCase.want)
			}
		})
	}
}

func TestAccepts(t *testing.T) {
	const dockerList = "application/vnd.docker.distribution.manifest.list.v2+json"

	cases := []struct {
		name      string
		accept    []string
		mediaType string
		want      bool
	}{
		{name: "no header at all", accept: nil, mediaType: MediaTypeOCIManifest, want: true},
		{name: "an empty header", accept: []string{""}, mediaType: MediaTypeOCIManifest, want: true},
		{name: "blank header", accept: []string{"   "}, mediaType: MediaTypeOCIManifest, want: true},
		{name: "an exact match", accept: []string{MediaTypeOCIManifest}, mediaType: MediaTypeOCIManifest, want: true},
		{name: "a wildcard", accept: []string{"*/*"}, mediaType: MediaTypeOCIManifest, want: true},
		{name: "a type wildcard", accept: []string{"application/*"}, mediaType: MediaTypeOCIManifest, want: true},

		{
			name:      "a modern client listing both encodings",
			accept:    []string{MediaTypeDockerManifest + ", " + dockerList + ", " + MediaTypeOCIManifest + ", " + MediaTypeOCIIndex},
			mediaType: MediaTypeOCIManifest,
			want:      true,
		},
		{
			name:      "quality values are ignored",
			accept:    []string{MediaTypeOCIManifest + ";q=0.9"},
			mediaType: MediaTypeOCIManifest,
			want:      true,
		},

		// The docker daemon sends one Accept line per media type, so the match can
		// live in any of them and not only in the first.
		{
			name:      "a header per type with the index last",
			accept:    []string{MediaTypeDockerManifest, dockerList, MediaTypeOCIManifest, MediaTypeOCIIndex},
			mediaType: MediaTypeOCIIndex,
			want:      true,
		},
		{
			name:      "a header per type with the index first",
			accept:    []string{MediaTypeOCIIndex, MediaTypeDockerManifest, dockerList},
			mediaType: MediaTypeOCIIndex,
			want:      true,
		},
		{
			name:      "a header per type with a wildcard in a later line",
			accept:    []string{MediaTypeDockerManifest, "*/*"},
			mediaType: MediaTypeOCIIndex,
			want:      true,
		},

		// A header that expressed no opinion about manifests is not grounds to
		// refuse, which is what keeps an odd client working.
		{name: "an unrelated type only", accept: []string{"application/json"}, mediaType: MediaTypeOCIManifest, want: true},
		{name: "an unrelated type and a wildcard", accept: []string{"text/html, */*"}, mediaType: MediaTypeOCIIndex, want: true},

		// Only a client that listed manifest types and left this one out is refused.
		{
			name:      "an old client that knows only the docker encoding",
			accept:    []string{MediaTypeDockerManifest + ", " + dockerList},
			mediaType: MediaTypeOCIManifest,
			want:      false,
		},
		{
			name:      "an old client sending a header per type",
			accept:    []string{MediaTypeDockerManifest, dockerList},
			mediaType: MediaTypeOCIIndex,
			want:      false,
		},
		{
			name:      "an image manifest offered where an index is stored",
			accept:    []string{MediaTypeOCIManifest},
			mediaType: MediaTypeOCIIndex,
			want:      false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Accepts(testCase.accept, testCase.mediaType); got != testCase.want {
				t.Fatalf("Accepts(%q, %q) = %v, want %v", testCase.accept, testCase.mediaType, got, testCase.want)
			}
		})
	}
}

func TestIsNondistributable(t *testing.T) {
	cases := []struct {
		mediaType string
		want      bool
	}{
		{"application/vnd.docker.image.rootfs.foreign.diff.tar.gzip", true},
		{"application/vnd.oci.image.layer.nondistributable.v1.tar+gzip", true},

		{"application/vnd.oci.image.layer.v1.tar+gzip", false},
		{"application/vnd.docker.image.rootfs.diff.tar.gzip", false},
		{"", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.mediaType, func(t *testing.T) {
			if got := IsNondistributable(testCase.mediaType); got != testCase.want {
				t.Fatalf("IsNondistributable(%q) = %v, want %v", testCase.mediaType, got, testCase.want)
			}
		})
	}
}

// A foreign layer's sources are the one thing in a manifest that points outside
// the registry, so they have to survive parsing to be recorded.
func TestParseManifestKeepsForeignLayerURLs(t *testing.T) {
	document := `{
	  "schemaVersion": 2,
	  "config": {"digest": "` + digestOf("1") + `", "size": 1},
	  "layers": [{
	    "mediaType": "application/vnd.oci.image.layer.nondistributable.v1.tar+gzip",
	    "digest": "` + digestOf("2") + `",
	    "size": 100,
	    "urls": ["https://vendor.example.com/layer.tar.gz"]
	  }]
	}`

	manifest, err := ParseManifest([]byte(document))
	if err != nil {
		t.Fatalf("ParseManifest returned %v", err)
	}

	references := manifest.References()
	layer := references[len(references)-1]

	if len(layer.URLs) != 1 || layer.URLs[0] != "https://vendor.example.com/layer.tar.gz" {
		t.Fatalf("URLs = %v, want the vendor source", layer.URLs)
	}
}

func TestPlatformString(t *testing.T) {
	cases := []struct {
		platform Platform
		want     string
	}{
		{Platform{OS: "linux", Architecture: "amd64"}, "linux/amd64"},
		{Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}, "linux/arm64/v8"},
		{Platform{}, ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.want, func(t *testing.T) {
			if got := testCase.platform.String(); got != testCase.want {
				t.Fatalf("String() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestParseManifestRefusesAMediaTypeItWouldNotServe(t *testing.T) {
	document := []byte(`{"schemaVersion":2,"mediaType":"text/html","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` +
		"0000000000000000000000000000000000000000000000000000000000000000" + `","size":1},"layers":[]}`)

	if _, err := ParseManifest(document); !errors.Is(err, ErrMediaType) {
		t.Fatalf("err = %v, want ErrMediaType", err)
	}

	for _, mediaType := range []string{MediaTypeOCIManifest, MediaTypeDockerManifest} {
		if !IsStorableManifestMediaType(mediaType) {
			t.Errorf("%s must be storable", mediaType)
		}
	}
	for _, mediaType := range []string{"text/html", "image/svg+xml", MediaTypeDockerManifestV1, ""} {
		if IsStorableManifestMediaType(mediaType) {
			t.Errorf("%q must not be storable", mediaType)
		}
	}
}
