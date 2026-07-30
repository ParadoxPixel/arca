package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"arca/internal/docker"
)

// registryClient drives the registry API the way a Docker client does, so a test
// exercises the wire protocol rather than the handlers directly.
type registryClient struct {
	instance   *testInstance
	repository string
}

func (i *testInstance) registry(repository string) *registryClient {
	i.t.Helper()

	expectStatus(i.t, i.api(http.MethodPost, "/api/repositories",
		`{"name":"`+repository+`","format":"docker","policy":"mixed","allowRedeploy":true}`), http.StatusCreated)

	return &registryClient{instance: i, repository: repository}
}

func (c *registryClient) do(method, path string, body []byte, contentType string) *http.Response {
	c.instance.t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	request, err := http.NewRequest(method, c.instance.server.URL+path, reader)
	if err != nil {
		c.instance.t.Fatalf("building a registry request: %v", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.SetBasicAuth(administratorEmail, administratorPassword)

	return c.instance.do(request)
}

func (c *registryClient) name(image string) string { return c.repository + "/" + image }

func digestOf(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// pushBlob runs the three-request chunked push a Docker client uses, splitting the
// payload so the PATCH path is exercised rather than only the monolithic one.
func (c *registryClient) pushBlob(image string, payload []byte) string {
	c.instance.t.Helper()
	t := c.instance.t

	response := c.do(http.MethodPost, "/v2/"+c.name(image)+"/blobs/uploads/", nil, "")
	expectStatus(t, response, http.StatusAccepted)

	location := response.Header.Get("Location")
	if location == "" {
		t.Fatal("the upload did not report a Location")
	}
	if got := response.Header.Get("Range"); got != "0-0" {
		t.Fatalf("a fresh session reported Range %q, want 0-0", got)
	}

	split := len(payload) / 2
	response = c.do(http.MethodPatch, location, payload[:split], blobContentType)
	expectStatus(t, response, http.StatusAccepted)

	if got, want := response.Header.Get("Range"), fmt.Sprintf("0-%d", split-1); got != want {
		t.Fatalf("after one chunk Range = %q, want %q", got, want)
	}

	digest := digestOf(payload)
	response = c.do(http.MethodPut, location+"?digest="+digest, payload[split:], blobContentType)
	expectStatus(t, response, http.StatusCreated)

	if got := response.Header.Get(docker.ContentDigestHeader); got != digest {
		t.Fatalf("commit reported digest %q, want %q", got, digest)
	}
	return digest
}

func (c *registryClient) pushManifest(image, reference string, manifest []byte) *http.Response {
	return c.do(http.MethodPut, "/v2/"+c.name(image)+"/manifests/"+reference, manifest, docker.MediaTypeOCIManifest)
}

func imageManifestFor(configDigest string, configSize int, layers map[string]int) []byte {
	descriptors := make([]map[string]any, 0, len(layers))
	for digest, size := range layers {
		descriptors = append(descriptors, map[string]any{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
			"digest":    digest,
			"size":      size,
		})
	}

	document, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     docker.MediaTypeOCIManifest,
		"config": map[string]any{
			"mediaType": docker.MediaTypeOCIConfig,
			"digest":    configDigest,
			"size":      configSize,
		},
		"layers": descriptors,
	})
	if err != nil {
		panic(err)
	}
	return document
}

// withAnnotation makes a manifest distinct without changing what it references,
// which is how a test builds two tags that genuinely share one layer.
func withAnnotation(document []byte, key, value string) []byte {
	var manifest map[string]any
	if err := json.Unmarshal(document, &manifest); err != nil {
		panic(err)
	}
	manifest["annotations"] = map[string]string{key: value}

	annotated, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}
	return annotated
}

const testImageConfig = `{"architecture":"amd64","os":"linux","created":"2026-07-30T10:11:12Z",` +
	`"config":{"Entrypoint":["/bin/app"],"Labels":{"owner":"platform"}},` +
	`"rootfs":{"type":"layers","diff_ids":[]}}`

// pushImage stores a config, a layer and a manifest, which is the whole of what
// "docker push" does for a single-platform image.
func (c *registryClient) pushImage(image, tag string) (manifestDigest string, layerDigest string) {
	c.instance.t.Helper()

	config := []byte(testImageConfig)
	layer := bytes.Repeat([]byte("layer-bytes"), 64)

	configDigest := c.pushBlob(image, config)
	layerDigest = c.pushBlob(image, layer)

	manifest := imageManifestFor(configDigest, len(config), map[string]int{layerDigest: len(layer)})
	response := c.pushManifest(image, tag, manifest)
	expectStatus(c.instance.t, response, http.StatusCreated)

	return digestOf(manifest), layerDigest
}

func TestDockerVersionCheck(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	t.Run("an anonymous client is challenged when nothing is public", func(t *testing.T) {
		response := instance.doAnonymously(instance.request(http.MethodGet, "/v2/", ""))
		expectStatus(t, response, http.StatusUnauthorized)

		if challenge := response.Header.Get("WWW-Authenticate"); !strings.HasPrefix(challenge, "Basic ") {
			t.Fatalf("WWW-Authenticate = %q, want a Basic challenge so docker login works", challenge)
		}
	})

	t.Run("an authenticated client is accepted", func(t *testing.T) {
		request := instance.request(http.MethodGet, "/v2/", "")
		request.SetBasicAuth(administratorEmail, administratorPassword)

		response := instance.do(request)
		expectStatus(t, response, http.StatusOK)

		if got := response.Header.Get(docker.APIVersionHeader); got != docker.APIVersion {
			t.Fatalf("%s = %q, want %q", docker.APIVersionHeader, got, docker.APIVersion)
		}
	})

	t.Run("an anonymous client is accepted once a docker repository is public", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
			`{"name":"public-images","format":"docker","visibility":"public"}`), http.StatusCreated)

		expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, "/v2/", "")), http.StatusOK)
	})
}

func TestDockerPushAndPull(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	manifestDigest, layerDigest := registry.pushImage("team/api", "1.4.0")

	t.Run("the manifest is pullable by tag", func(t *testing.T) {
		response := registry.do(http.MethodGet, "/v2/images/team/api/manifests/1.4.0", nil, "")
		body := expectStatus(t, response, http.StatusOK)

		if got := response.Header.Get(docker.ContentDigestHeader); got != manifestDigest {
			t.Fatalf("%s = %q, want %q", docker.ContentDigestHeader, got, manifestDigest)
		}
		if got := response.Header.Get("Content-Type"); got != docker.MediaTypeOCIManifest {
			t.Fatalf("Content-Type = %q, want %q", got, docker.MediaTypeOCIManifest)
		}
		if !strings.Contains(body, layerDigest) {
			t.Fatalf("the manifest does not mention its layer: %s", body)
		}
	})

	t.Run("the manifest is pullable by digest", func(t *testing.T) {
		response := registry.do(http.MethodGet, "/v2/images/team/api/manifests/"+manifestDigest, nil, "")
		expectStatus(t, response, http.StatusOK)
	})

	t.Run("a HEAD reports the digest without a body", func(t *testing.T) {
		response := registry.do(http.MethodHead, "/v2/images/team/api/manifests/1.4.0", nil, "")
		body := expectStatus(t, response, http.StatusOK)

		if body != "" {
			t.Fatalf("HEAD returned a body: %q", body)
		}
		if got := response.Header.Get(docker.ContentDigestHeader); got != manifestDigest {
			t.Fatalf("%s = %q, want %q", docker.ContentDigestHeader, got, manifestDigest)
		}
	})

	t.Run("the layer is pullable", func(t *testing.T) {
		response := registry.do(http.MethodGet, "/v2/images/team/api/blobs/"+layerDigest, nil, "")
		body := expectStatus(t, response, http.StatusOK)

		if digestOf([]byte(body)) != layerDigest {
			t.Fatalf("the served layer hashed to %s, want %s", digestOf([]byte(body)), layerDigest)
		}
	})

	t.Run("the tag is listed", func(t *testing.T) {
		body := expectStatus(t, registry.do(http.MethodGet, "/v2/images/team/api/tags/list", nil, ""), http.StatusOK)

		var payload struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("decoding the tag list: %v", err)
		}
		if payload.Name != "images/team/api" {
			t.Fatalf("name = %q, want %q", payload.Name, "images/team/api")
		}
		if len(payload.Tags) != 1 || payload.Tags[0] != "1.4.0" {
			t.Fatalf("tags = %v, want [1.4.0]", payload.Tags)
		}
	})

	t.Run("the tag became a component the rest of the UI can see", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/images/artifacts?namespace=team&name=api", ""), http.StatusOK)

		if !strings.Contains(body, `"version":"1.4.0"`) {
			t.Fatalf("the tag is not a version: %s", body)
		}
		if !strings.Contains(body, `"format":"docker"`) {
			t.Fatalf("the format was not recorded: %s", body)
		}
	})
}

func TestDockerManifestRequiresItsBlobs(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	config := []byte(testImageConfig)
	configDigest := registry.pushBlob("solo", config)

	missing := digestOf([]byte("never pushed"))
	manifest := imageManifestFor(configDigest, len(config), map[string]int{missing: 11})

	body := expectStatus(t, registry.pushManifest("solo", "1.0", manifest), http.StatusNotFound)
	if !strings.Contains(body, docker.ErrorManifestBlobUnknown) {
		t.Fatalf("expected %s, got %s", docker.ErrorManifestBlobUnknown, body)
	}
}

func TestDockerBlobUploadRejections(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	payload := []byte("some layer content")

	t.Run("a commit whose digest does not match the bytes is refused", func(t *testing.T) {
		response := registry.do(http.MethodPost, "/v2/images/app/blobs/uploads/", nil, "")
		expectStatus(t, response, http.StatusAccepted)
		location := response.Header.Get("Location")

		wrong := digestOf([]byte("different content"))
		body := expectStatus(t, registry.do(http.MethodPut, location+"?digest="+wrong, payload, blobContentType), http.StatusBadRequest)

		if !strings.Contains(body, docker.ErrorDigestInvalid) {
			t.Fatalf("expected %s, got %s", docker.ErrorDigestInvalid, body)
		}
	})

	t.Run("a chunk that does not continue the session is refused", func(t *testing.T) {
		response := registry.do(http.MethodPost, "/v2/images/app/blobs/uploads/", nil, "")
		expectStatus(t, response, http.StatusAccepted)
		location := response.Header.Get("Location")

		request, err := http.NewRequest(http.MethodPatch, instance.server.URL+location, bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("building a PATCH: %v", err)
		}
		request.Header.Set("Content-Range", "500-600")
		request.SetBasicAuth(administratorEmail, administratorPassword)

		expectStatus(t, instance.do(request), http.StatusRequestedRangeNotSatisfiable)
	})

	t.Run("an unknown session is not found", func(t *testing.T) {
		expectStatus(t, registry.do(http.MethodPatch, "/v2/images/app/blobs/uploads/nosuchsession", payload, blobContentType), http.StatusNotFound)
	})

	t.Run("a monolithic push in one POST is accepted", func(t *testing.T) {
		digest := digestOf(payload)
		response := registry.do(http.MethodPost, "/v2/images/app/blobs/uploads/?digest="+digest, payload, blobContentType)
		expectStatus(t, response, http.StatusCreated)

		if got := response.Header.Get(docker.ContentDigestHeader); got != digest {
			t.Fatalf("%s = %q, want %q", docker.ContentDigestHeader, got, digest)
		}
		expectStatus(t, registry.do(http.MethodGet, "/v2/images/app/blobs/"+digest, nil, ""), http.StatusOK)
	})
}

func TestDockerLayersAreSharedBetweenTags(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	first, layer := registry.pushImage("shared/app", "1.0")

	// The same layer is referenced by a second tag through an otherwise identical
	// manifest, which is what makes a per-version size need the reference table
	// rather than the assets under the tag.
	config := []byte(testImageConfig)
	configDigest := digestOf(config)
	second := withAnnotation(
		imageManifestFor(configDigest, len(config), map[string]int{layer: 704}),
		"org.opencontainers.image.revision", "second",
	)
	expectStatus(t, registry.pushManifest("shared/app", "1.1", second), http.StatusCreated)

	if first == digestOf(second) {
		t.Fatal("the two manifests are identical, so this proves nothing about sharing")
	}

	body := expectStatus(t, registry.do(http.MethodGet, "/v2/images/shared/app/tags/list", nil, ""), http.StatusOK)
	for _, tag := range []string{"1.0", "1.1"} {
		if !strings.Contains(body, `"`+tag+`"`) {
			t.Fatalf("tag %s is missing from %s", tag, body)
		}
	}

	t.Run("deleting one tag leaves the layer for the other", func(t *testing.T) {
		expectStatus(t, registry.do(http.MethodDelete, "/v2/images/shared/app/manifests/1.0", nil, ""), http.StatusAccepted)

		expectStatus(t, registry.do(http.MethodGet, "/v2/images/shared/app/manifests/1.0", nil, ""), http.StatusNotFound)
		expectStatus(t, registry.do(http.MethodGet, "/v2/images/shared/app/blobs/"+layer, nil, ""), http.StatusOK)
		expectStatus(t, registry.do(http.MethodGet, "/v2/images/shared/app/manifests/1.1", nil, ""), http.StatusOK)
	})
}

func TestDockerRepositoryResolution(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")
	registry.pushImage("nginx", "1.25")

	cases := []struct {
		name   string
		path   string
		status int
	}{
		{"the leading segment names the repository", "/v2/images/nginx/manifests/1.25", http.StatusOK},
		{"an unknown repository is not found", "/v2/nowhere/nginx/manifests/1.25", http.StatusNotFound},
		{"an unknown image in a known repository is not found", "/v2/images/absent/manifests/1.25", http.StatusNotFound},
		{"an unknown tag is not found", "/v2/images/nginx/manifests/9.9", http.StatusNotFound},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, registry.do(http.MethodGet, testCase.path, nil, ""), testCase.status)
		})
	}

	t.Run("a bare image resolves through the default repository", func(t *testing.T) {
		if err := instance.app.store.SetSetting("docker_repository", "images"); err != nil {
			t.Fatalf("setting the default repository: %v", err)
		}
		expectStatus(t, registry.do(http.MethodGet, "/v2/nginx/manifests/1.25", nil, ""), http.StatusOK)
	})
}

func TestDockerCatalogListsPrefixedImages(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	registry := instance.registry("images")
	registry.pushImage("team/api", "1.0")
	registry.pushImage("nginx", "1.25")

	body := expectStatus(t, registry.do(http.MethodGet, "/v2/_catalog", nil, ""), http.StatusOK)

	var payload struct {
		Repositories []string `json:"repositories"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding the catalog: %v", err)
	}

	// Every entry carries the repository prefix, because without it a client
	// could not turn a catalog entry back into something it can pull.
	want := map[string]bool{"images/team/api": false, "images/nginx": false}
	for _, name := range payload.Repositories {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("%q is missing from the catalog: %v", name, payload.Repositories)
		}
	}
}

func TestDockerPermissions(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("private-images")
	registry.pushImage("app", "1.0")

	t.Run("an anonymous pull from a private repository is challenged", func(t *testing.T) {
		response := instance.doAnonymously(instance.request(http.MethodGet, "/v2/private-images/app/manifests/1.0", ""))
		expectStatus(t, response, http.StatusUnauthorized)

		if challenge := response.Header.Get("WWW-Authenticate"); !strings.HasPrefix(challenge, "Basic ") {
			t.Fatalf("WWW-Authenticate = %q, want a Basic challenge", challenge)
		}
	})

	t.Run("a proxy repository refuses writes", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
			`{"name":"hub","format":"docker","type":"proxy","remoteUrl":"https://registry-1.docker.io"}`), http.StatusCreated)

		proxy := &registryClient{instance: instance, repository: "hub"}
		expectStatus(t, proxy.do(http.MethodPost, "/v2/hub/nginx/blobs/uploads/", nil, ""), http.StatusMethodNotAllowed)
	})
}

func TestDockerTagPolicyAndImmutability(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
		`{"name":"releases","format":"docker","policy":"release","allowRedeploy":false}`), http.StatusCreated)

	registry := &registryClient{instance: instance, repository: "releases"}

	config := []byte(testImageConfig)
	configDigest := registry.pushBlob("app", config)
	manifest := imageManifestFor(configDigest, len(config), nil)

	t.Run("a release tag is accepted", func(t *testing.T) {
		expectStatus(t, registry.pushManifest("app", "1.0.0", manifest), http.StatusCreated)
	})

	t.Run("a prerelease tag is rejected by the release policy", func(t *testing.T) {
		body := expectStatus(t, registry.pushManifest("app", "1.1.0-rc1", manifest), http.StatusBadRequest)
		if !strings.Contains(body, docker.ErrorTagInvalid) {
			t.Fatalf("expected %s, got %s", docker.ErrorTagInvalid, body)
		}
	})

	t.Run("a locked repository refuses to move an existing tag", func(t *testing.T) {
		body := expectStatus(t, registry.pushManifest("app", "1.0.0", manifest), http.StatusConflict)
		if !strings.Contains(body, docker.ErrorDenied) {
			t.Fatalf("expected %s, got %s", docker.ErrorDenied, body)
		}
	})

	t.Run("a variant tag is not a prerelease", func(t *testing.T) {
		expectStatus(t, registry.pushManifest("app", "1.0.0-alpine", manifest), http.StatusCreated)
	})
}

func TestDockerManifestMetadata(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	manifestDigest, layerDigest := registry.pushImage("team/api", "1.4.0")

	body := expectStatus(t, instance.api(http.MethodGet,
		"/api/repositories/images/docker/manifests?namespace=team&name=api&reference=1.4.0", ""), http.StatusOK)

	var payload struct {
		Digest       string `json:"digest"`
		MediaType    string `json:"mediaType"`
		TotalSize    int64  `json:"totalSize"`
		LayerCount   int    `json:"layerCount"`
		IsIndex      bool   `json:"isIndex"`
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		ImageCreated int64  `json:"imageCreated"`
		Layers       []struct {
			Digest     string `json:"digest"`
			Size       int64  `json:"size"`
			SharedWith int    `json:"sharedWith"`
			Stored     bool   `json:"stored"`
			Foreign    bool   `json:"foreign"`
		} `json:"layers"`
		Config *struct {
			Entrypoint []string `json:"entrypoint"`
			History    []struct {
				CreatedBy  string `json:"createdBy"`
				EmptyLayer bool   `json:"emptyLayer"`
			} `json:"history"`
		} `json:"config"`
		PullBy map[string]string `json:"pullBy"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding the manifest: %v", err)
	}

	t.Run("the summary describes the image", func(t *testing.T) {
		if payload.Digest != manifestDigest {
			t.Fatalf("digest = %q, want %q", payload.Digest, manifestDigest)
		}
		if payload.IsIndex {
			t.Fatal("a single-platform image reported itself as an index")
		}
		if payload.OS != "linux" || payload.Architecture != "amd64" {
			t.Fatalf("platform = %s/%s, want linux/amd64", payload.OS, payload.Architecture)
		}
		if payload.ImageCreated == 0 {
			t.Fatal("the config timestamp was not read")
		}
	})

	t.Run("the total size is the layers rather than the manifest", func(t *testing.T) {
		// A manifest is a couple of hundred bytes; its config and layer are more.
		if payload.TotalSize != int64(len(testImageConfig))+704 {
			t.Fatalf("totalSize = %d, want %d", payload.TotalSize, len(testImageConfig)+704)
		}
	})

	t.Run("the layer is listed as stored and unshared", func(t *testing.T) {
		if payload.LayerCount != 1 || len(payload.Layers) != 1 {
			t.Fatalf("layers = %v, want one", payload.Layers)
		}
		layer := payload.Layers[0]
		if layer.Digest != layerDigest {
			t.Fatalf("layer digest = %q, want %q", layer.Digest, layerDigest)
		}
		if !layer.Stored {
			t.Fatal("the layer was pushed but is not reported as stored")
		}
		if layer.Foreign {
			t.Fatal("an ordinary layer was reported as foreign")
		}
		if layer.SharedWith != 0 {
			t.Fatalf("sharedWith = %d, want 0 for the only image using it", layer.SharedWith)
		}
	})

	t.Run("the config is read from its blob", func(t *testing.T) {
		if payload.Config == nil {
			t.Fatal("the config was not read")
		}
		if len(payload.Config.Entrypoint) != 1 || payload.Config.Entrypoint[0] != "/bin/app" {
			t.Fatalf("entrypoint = %v, want [/bin/app]", payload.Config.Entrypoint)
		}
		if len(payload.Config.History) != 0 {
			t.Fatalf("history = %v, want none for a config without one", payload.Config.History)
		}
	})

	t.Run("the pull references carry the repository prefix", func(t *testing.T) {
		if payload.PullBy["tag"] != "images/team/api:1.4.0" {
			t.Fatalf("pullBy.tag = %q", payload.PullBy["tag"])
		}
		if payload.PullBy["digest"] != "images/team/api@"+manifestDigest {
			t.Fatalf("pullBy.digest = %q", payload.PullBy["digest"])
		}
	})

	t.Run("a second image sharing the layer reports it as shared", func(t *testing.T) {
		config := []byte(testImageConfig)
		second := withAnnotation(
			imageManifestFor(digestOf(config), len(config), map[string]int{layerDigest: 704}),
			"org.opencontainers.image.revision", "second",
		)
		expectStatus(t, registry.pushManifest("team/api", "1.5.0", second), http.StatusCreated)

		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/images/docker/manifests?namespace=team&name=api&reference=1.4.0", ""), http.StatusOK)

		var reread struct {
			Layers []struct {
				SharedWith int `json:"sharedWith"`
			} `json:"layers"`
		}
		if err := json.Unmarshal([]byte(body), &reread); err != nil {
			t.Fatalf("decoding the manifest: %v", err)
		}
		if reread.Layers[0].SharedWith != 1 {
			t.Fatalf("sharedWith = %d, want 1 now that a second image needs it", reread.Layers[0].SharedWith)
		}
	})
}

func TestDockerManifestMetadataForAnIndex(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	// Two single-platform images become the children of one index, which is what a
	// buildx push of a multi-arch image produces.
	config := []byte(testImageConfig)
	configDigest := registry.pushBlob("multi/app", config)

	children := []map[string]any{}
	for index, platform := range []map[string]any{
		{"os": "linux", "architecture": "amd64"},
		{"os": "linux", "architecture": "arm64", "variant": "v8"},
	} {
		layer := bytes.Repeat([]byte{byte('a' + index)}, 128)
		layerDigest := registry.pushBlob("multi/app", layer)

		child := imageManifestFor(configDigest, len(config), map[string]int{layerDigest: len(layer)})
		expectStatus(t, registry.pushManifest("multi/app", digestOf(child), child), http.StatusCreated)

		children = append(children, map[string]any{
			"mediaType": docker.MediaTypeOCIManifest,
			"digest":    digestOf(child),
			"size":      len(child),
			"platform":  platform,
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

	response := registry.do(http.MethodPut, "/v2/images/multi/app/manifests/1.0", index, docker.MediaTypeOCIIndex)
	expectStatus(t, response, http.StatusCreated)

	body := expectStatus(t, instance.api(http.MethodGet,
		"/api/repositories/images/docker/manifests?namespace=multi&name=app&reference=1.0", ""), http.StatusOK)

	var payload struct {
		IsIndex   bool  `json:"isIndex"`
		TotalSize int64 `json:"totalSize"`
		Children  []struct {
			Platform   string `json:"platform"`
			TotalSize  int64  `json:"totalSize"`
			LayerCount int    `json:"layerCount"`
			Indexed    bool   `json:"indexed"`
		} `json:"children"`
		Layers []any `json:"layers"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding the index: %v", err)
	}

	if !payload.IsIndex {
		t.Fatal("IsIndex = false, want true")
	}
	if len(payload.Layers) != 0 {
		t.Fatalf("an index reported %d layers of its own, want none", len(payload.Layers))
	}
	if len(payload.Children) != 2 {
		t.Fatalf("children = %v, want two platforms", payload.Children)
	}

	platforms := []string{"linux/amd64", "linux/arm64/v8"}
	for position, child := range payload.Children {
		if child.Platform != platforms[position] {
			t.Fatalf("child %d platform = %q, want %q", position, child.Platform, platforms[position])
		}
		// The children were pushed first, so the index knows their real weight
		// rather than only the size of their manifest documents.
		if !child.Indexed || child.LayerCount != 1 {
			t.Fatalf("child %d was not indexed: %+v", position, child)
		}
	}

	var childTotal int64
	for _, child := range payload.Children {
		childTotal += child.TotalSize
	}
	if payload.TotalSize != childTotal {
		t.Fatalf("totalSize = %d, want the sum of its children %d", payload.TotalSize, childTotal)
	}
}

// Every buildkit multi-platform build attaches an attestation child, which declares
// a platform of unknown/unknown. Without the descriptor annotation it would read as a
// broken architecture rather than as provenance.
func TestDockerIndexAttestationChild(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	config := []byte(testImageConfig)
	configDigest := registry.pushBlob("multi/app", config)

	platform := imageManifestFor(configDigest, len(config), nil)
	expectStatus(t, registry.pushManifest("multi/app", digestOf(platform), platform), http.StatusCreated)

	attestation := withAnnotation(
		imageManifestFor(configDigest, len(config), nil),
		"vnd.docker.reference.type", "attestation-manifest",
	)
	expectStatus(t, registry.pushManifest("multi/app", digestOf(attestation), attestation), http.StatusCreated)

	index, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     docker.MediaTypeOCIIndex,
		"manifests": []map[string]any{
			{
				"mediaType": docker.MediaTypeOCIManifest,
				"digest":    digestOf(platform),
				"size":      len(platform),
				"platform":  map[string]any{"os": "linux", "architecture": "amd64"},
			},
			{
				"mediaType": docker.MediaTypeOCIManifest,
				"digest":    digestOf(attestation),
				"size":      len(attestation),
				// buildkit really does declare unknown/unknown here.
				"platform":    map[string]any{"os": "unknown", "architecture": "unknown"},
				"annotations": map[string]string{"vnd.docker.reference.type": "attestation-manifest"},
			},
		},
	})
	if err != nil {
		t.Fatalf("building the index: %v", err)
	}

	expectStatus(t, registry.do(http.MethodPut, "/v2/images/multi/app/manifests/1.0", index, docker.MediaTypeOCIIndex), http.StatusCreated)

	body := expectStatus(t, instance.api(http.MethodGet,
		"/api/repositories/images/docker/manifests?namespace=multi&name=app&reference=1.0", ""), http.StatusOK)

	var payload struct {
		ImageCreated int64 `json:"imageCreated"`
		Children     []struct {
			Platform      string `json:"platform"`
			ReferenceType string `json:"referenceType"`
		} `json:"children"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding the index: %v", err)
	}
	if len(payload.Children) != 2 {
		t.Fatalf("children = %v, want two", payload.Children)
	}

	if payload.Children[0].ReferenceType != "" {
		t.Fatalf("the real platform carries a reference type: %q", payload.Children[0].ReferenceType)
	}
	if payload.Children[1].ReferenceType != "attestation-manifest" {
		t.Fatalf("the attestation reference type = %q, want attestation-manifest", payload.Children[1].ReferenceType)
	}

	// An index has no config of its own, so its build time comes from its children
	// rather than being reported as unknown.
	if payload.ImageCreated == 0 {
		t.Fatal("the index reported no build time, want the newest child's")
	}
}

func TestDockerManifestMetadataRejections(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")
	registry.pushImage("app", "1.0")

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"jars","format":"maven2"}`), http.StatusCreated)

	cases := []struct {
		name   string
		path   string
		status int
	}{
		{"an unknown tag", "/api/repositories/images/docker/manifests?name=app&reference=9.9", http.StatusNotFound},
		{"an unknown image", "/api/repositories/images/docker/manifests?name=absent&reference=1.0", http.StatusNotFound},
		{"a missing reference", "/api/repositories/images/docker/manifests?name=app", http.StatusBadRequest},
		{"a missing name", "/api/repositories/images/docker/manifests?reference=1.0", http.StatusBadRequest},
		{"a bad reference", "/api/repositories/images/docker/manifests?name=app&reference=.hidden", http.StatusBadRequest},
		{"a repository of another format", "/api/repositories/jars/docker/manifests?name=app&reference=1.0", http.StatusBadRequest},
		{"an unknown repository", "/api/repositories/nowhere/docker/manifests?name=app&reference=1.0", http.StatusNotFound},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodGet, testCase.path, ""), testCase.status)
		})
	}
}
