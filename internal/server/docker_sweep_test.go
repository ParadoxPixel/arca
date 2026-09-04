package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"arca/internal/docker"
)

type sweepReport struct {
	Repositories []struct {
		Repository string `json:"repository"`
		Blobs      int    `json:"blobs"`
		Manifests  int    `json:"manifests"`
		Bytes      int64  `json:"bytes"`
	} `json:"repositories"`
}

func (i *testInstance) sweep(method string) sweepReport {
	i.t.Helper()

	// Everything these tests push was pushed a moment ago, so the grace period
	// that protects a push in flight is switched off for them.
	noGrace := time.Duration(0)
	i.app.sweepGraceOverride = &noGrace

	body := expectStatus(i.t, i.api(method, "/api/admin/docker/sweep", ""), http.StatusOK)

	var report sweepReport
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		i.t.Fatalf("decoding the sweep report: %v", err)
	}
	return report
}

func (i *testInstance) sweepOf(report sweepReport, repository string) (blobs, manifests int) {
	i.t.Helper()

	for _, entry := range report.Repositories {
		if entry.Repository == repository {
			return entry.Blobs, entry.Manifests
		}
	}
	i.t.Fatalf("%q is missing from the sweep report", repository)
	return 0, 0
}

// A tagged image must survive a sweep untouched. This is the case that would be
// catastrophic to get wrong, so it is asserted before anything about reclaiming.
func TestDockerSweepKeepsTaggedImages(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	manifestDigest, layerDigest := registry.pushImage("team/api", "1.0")

	blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")
	if blobs != 0 || manifests != 0 {
		t.Fatalf("a sweep removed %d blobs and %d manifests from a fully tagged repository", blobs, manifests)
	}

	cases := []struct {
		name string
		path string
	}{
		{"the tag", "/v2/images/team/api/manifests/1.0"},
		{"the manifest by digest", "/v2/images/team/api/manifests/" + manifestDigest},
		{"the layer", "/v2/images/team/api/blobs/" + layerDigest},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, registry.do(http.MethodGet, testCase.path, nil, ""), http.StatusOK)
		})
	}
}

func TestDockerSweepReclaimsAnUntaggedImage(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	// Two tags with layers of their own, so removing one leaves content that only it
	// referenced and content the survivor still needs.
	_, keptLayer := registry.pushImage("team/api", "1.0")

	config := []byte(testImageConfig)
	configDigest := digestOf(config)
	retiredLayer := registry.pushBlob("team/api", bytes.Repeat([]byte("retired"), 32))
	retired := imageManifestFor(configDigest, len(config), map[string]int{retiredLayer: 224})
	expectStatus(t, registry.pushManifest("team/api", "0.9", retired), http.StatusCreated)

	// Deleting the tag leaves the manifest and its exclusive layer behind, which is
	// correct on its own and is exactly what the sweep exists to finish.
	expectStatus(t, registry.do(http.MethodDelete, "/v2/images/team/api/manifests/0.9", nil, ""), http.StatusAccepted)
	expectStatus(t, registry.do(http.MethodGet, "/v2/images/team/api/blobs/"+retiredLayer, nil, ""), http.StatusOK)

	t.Run("the preview reports what would go without removing it", func(t *testing.T) {
		blobs, manifests := instance.sweepOf(instance.sweep(http.MethodGet), "images")
		if blobs != 1 || manifests != 1 {
			t.Fatalf("preview reported %d blobs and %d manifests, want 1 and 1", blobs, manifests)
		}
		expectStatus(t, registry.do(http.MethodGet, "/v2/images/team/api/blobs/"+retiredLayer, nil, ""), http.StatusOK)
	})

	t.Run("the sweep removes them", func(t *testing.T) {
		blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")
		if blobs != 1 || manifests != 1 {
			t.Fatalf("the sweep removed %d blobs and %d manifests, want 1 and 1", blobs, manifests)
		}
		expectStatus(t, registry.do(http.MethodGet, "/v2/images/team/api/blobs/"+retiredLayer, nil, ""), http.StatusNotFound)
	})

	t.Run("the surviving tag is intact", func(t *testing.T) {
		expectStatus(t, registry.do(http.MethodGet, "/v2/images/team/api/manifests/1.0", nil, ""), http.StatusOK)
		expectStatus(t, registry.do(http.MethodGet, "/v2/images/team/api/blobs/"+keptLayer, nil, ""), http.StatusOK)
	})

	t.Run("a second sweep finds nothing left", func(t *testing.T) {
		blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")
		if blobs != 0 || manifests != 0 {
			t.Fatalf("a repeat sweep removed %d blobs and %d manifests, want none", blobs, manifests)
		}
	})
}

// A layer shared by a surviving tag must not be reclaimed when the tag that also used
// it goes. This is the whole reason the reference table exists.
func TestDockerSweepKeepsSharedLayers(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	_, layer := registry.pushImage("shared/app", "1.0")

	config := []byte(testImageConfig)
	second := withAnnotation(
		imageManifestFor(digestOf(config), len(config), map[string]int{layer: 704}),
		"org.opencontainers.image.revision", "second",
	)
	expectStatus(t, registry.pushManifest("shared/app", "1.1", second), http.StatusCreated)

	expectStatus(t, registry.do(http.MethodDelete, "/v2/images/shared/app/manifests/1.0", nil, ""), http.StatusAccepted)

	blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")

	// The retired manifest goes, but its layer stays because 1.1 still needs it.
	if manifests != 1 {
		t.Fatalf("the sweep removed %d manifests, want the one that lost its tag", manifests)
	}
	if blobs != 0 {
		t.Fatalf("the sweep removed %d blobs, want none since the survivor shares them", blobs)
	}

	expectStatus(t, registry.do(http.MethodGet, "/v2/images/shared/app/blobs/"+layer, nil, ""), http.StatusOK)
	expectStatus(t, registry.do(http.MethodGet, "/v2/images/shared/app/manifests/1.1", nil, ""), http.StatusOK)
}

// An index's children carry no tag of their own, so a sweep that only looked at tags
// would delete every platform of every multi-arch image.
func TestDockerSweepKeepsIndexChildren(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	config := []byte(testImageConfig)
	configDigest := registry.pushBlob("multi/app", config)

	children := []map[string]any{}
	layers := []string{}
	for index := range 2 {
		layer := bytes.Repeat([]byte{byte('a' + index)}, 128)
		layerDigest := registry.pushBlob("multi/app", layer)
		layers = append(layers, layerDigest)

		child := imageManifestFor(configDigest, len(config), map[string]int{layerDigest: len(layer)})
		expectStatus(t, registry.pushManifest("multi/app", digestOf(child), child), http.StatusCreated)

		children = append(children, map[string]any{
			"mediaType": docker.MediaTypeOCIManifest,
			"digest":    digestOf(child),
			"size":      len(child),
			"platform":  map[string]any{"os": "linux", "architecture": []string{"amd64", "arm64"}[index]},
		})
	}

	index, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     docker.MediaTypeOCIIndex,
		"manifests":     children,
	})
	if err != nil {
		t.Fatalf("building the index: %v", err)
	}
	expectStatus(t, registry.do(http.MethodPut, "/v2/images/multi/app/manifests/1.0", index, docker.MediaTypeOCIIndex), http.StatusCreated)

	blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")
	if blobs != 0 || manifests != 0 {
		t.Fatalf("the sweep removed %d blobs and %d manifests reachable only through an index", blobs, manifests)
	}

	for _, layer := range layers {
		t.Run("layer "+layer[7:19], func(t *testing.T) {
			expectStatus(t, registry.do(http.MethodGet, "/v2/images/multi/app/blobs/"+layer, nil, ""), http.StatusOK)
		})
	}
	for _, child := range children {
		digest := child["digest"].(string)
		t.Run("child "+digest[7:19], func(t *testing.T) {
			expectStatus(t, registry.do(http.MethodGet, "/v2/images/multi/app/manifests/"+digest, nil, ""), http.StatusOK)
		})
	}

	t.Run("removing the index tag retires the whole tree", func(t *testing.T) {
		expectStatus(t, registry.do(http.MethodDelete, "/v2/images/multi/app/manifests/1.0", nil, ""), http.StatusAccepted)

		blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")
		// Two children plus the index itself, and the config plus both layers.
		if manifests != 3 {
			t.Fatalf("the sweep removed %d manifests, want 3", manifests)
		}
		if blobs != 3 {
			t.Fatalf("the sweep removed %d blobs, want 3", blobs)
		}
	})
}

// An upload session leaves a file that is not an asset. The sweep walks assets, so it
// must not confuse itself over one, and the session sweep is what collects them.
func TestDockerSweepIgnoresOpenUploads(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")
	registry.pushImage("app", "1.0")

	response := registry.do(http.MethodPost, "/v2/images/app/blobs/uploads/", nil, "")
	expectStatus(t, response, http.StatusAccepted)
	location := response.Header.Get("Location")
	expectStatus(t, registry.do(http.MethodPatch, location, []byte("half a layer"), blobContentType), http.StatusAccepted)

	blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")
	if blobs != 0 || manifests != 0 {
		t.Fatalf("the sweep removed %d blobs and %d manifests with an upload open", blobs, manifests)
	}

	// The session is still usable, which is the point: a sweep is not allowed to
	// interfere with a push in flight.
	expectStatus(t, registry.do(http.MethodGet, location, nil, ""), http.StatusNoContent)
}

func TestDockerSweepRequiresAnAdministrator(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			expectStatus(t, instance.doAnonymously(instance.request(method, "/api/admin/docker/sweep", "")), http.StatusUnauthorized)
		})
	}
}

// A proxy's content is refetchable, and a pull by digest caches no tag for
// reachability to start from, so a reachability sweep would delete exactly what is in
// use. Proxies age out by last access instead.
func TestDockerSweepSkipsProxies(t *testing.T) {
	upstream := newFakeRegistry(t)

	instance := newTestInstance(t)
	instance.setup()
	proxy := instance.proxyOf("hub", upstream.server.URL)

	// Pulled by digest only, so nothing here is reachable from a tag.
	layer := digestOf(upstream.layer)
	expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/blobs/"+layer, nil, ""), http.StatusOK)

	report := instance.sweep(http.MethodPost)
	for _, entry := range report.Repositories {
		if entry.Repository == "hub" {
			t.Fatalf("the sweep considered a proxy repository: %+v", entry)
		}
	}

	expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/blobs/"+layer, nil, ""), http.StatusOK)
}

// Rebuilding reads every stored manifest afresh. It is how a repository copied before
// this server knew how to index, or one whose migration stopped halfway, is repaired
// without recopying a byte.
func TestDockerReindexRebuildsMetadata(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	manifestDigest, _ := registry.pushImage("team/api", "1.0")

	// Wiping the parsed form leaves the bytes in place, which is the state a copy that
	// never settled leaves behind.
	if err := instance.app.store.DeleteDockerManifest(dockerRepositoryID(t, instance, "images"), manifestDigest); err != nil {
		t.Fatalf("clearing the index: %v", err)
	}
	expectStatus(t, instance.api(http.MethodGet,
		"/api/repositories/images/docker/manifests?namespace=team&name=api&reference=1.0", ""), http.StatusNotFound)

	body := expectStatus(t, instance.api(http.MethodPost, "/api/admin/docker/reindex", ""), http.StatusOK)

	var report struct {
		Repositories []struct {
			Repository string `json:"repository"`
			Manifests  int    `json:"manifests"`
			Failed     int    `json:"failed"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}

	found := false
	for _, entry := range report.Repositories {
		if entry.Repository != "images" {
			continue
		}
		found = true
		// The tag copy and the digest copy are both manifests on disk.
		if entry.Manifests != 2 || entry.Failed != 0 {
			t.Fatalf("reindexed %d manifests, %d failed", entry.Manifests, entry.Failed)
		}
	}
	if !found {
		t.Fatalf("images is missing from the report: %s", body)
	}

	t.Run("the platform is back", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/images/docker/manifests?namespace=team&name=api&reference=1.0", ""), http.StatusOK)

		var payload struct {
			Architecture string `json:"architecture"`
			LayerCount   int    `json:"layerCount"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("decoding the manifest: %v", err)
		}
		if payload.Architecture != "amd64" || payload.LayerCount != 1 {
			t.Fatalf("architecture = %q, layers = %d", payload.Architecture, payload.LayerCount)
		}
	})

	t.Run("a sweep after rebuilding still keeps the tagged image", func(t *testing.T) {
		blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")
		if blobs != 0 || manifests != 0 {
			t.Fatalf("the sweep removed %d blobs and %d manifests after a rebuild", blobs, manifests)
		}
	})
}

func dockerRepositoryID(t *testing.T, instance *testInstance, name string) string {
	t.Helper()

	repository, err := instance.app.store.RepositoryByName(name)
	if err != nil {
		t.Fatalf("looking up %s: %v", name, err)
	}
	return repository.ID
}

func TestDockerReindexRequiresAnAdministrator(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.doAnonymously(instance.request(http.MethodPost, "/api/admin/docker/reindex", "")), http.StatusUnauthorized)
}
