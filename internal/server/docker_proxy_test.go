package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"arca/internal/docker"
	"arca/internal/store/models"
)

// fakeRegistry stands in for Docker Hub: it refuses an unauthenticated request with a
// challenge to a separate token service and only serves content to a bearer token,
// which is the protocol the proxy client has to implement rather than an option.
type fakeRegistry struct {
	server   *httptest.Server
	tokens   *httptest.Server
	manifest []byte
	config   []byte
	layer    []byte

	issued    atomic.Int32
	unauthed  atomic.Int32
	manifests atomic.Int32
	requested []string
}

const fakeToken = "issued-bearer-token"

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()

	registry := &fakeRegistry{
		config: []byte(testImageConfig),
		layer:  bytes.Repeat([]byte("upstream-layer"), 32),
	}
	registry.manifest = imageManifestFor(
		digestOf(registry.config), len(registry.config),
		map[string]int{digestOf(registry.layer): len(registry.layer)},
	)

	registry.tokens = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// The scope names the repository the token is for, which is what makes it
		// worth caching per scope rather than once per remote.
		if request.URL.Query().Get("scope") == "" {
			http.Error(writer, "no scope", http.StatusBadRequest)
			return
		}
		registry.issued.Add(1)
		writeJSON(writer, http.StatusOK, map[string]any{"token": fakeToken, "expires_in": 300})
	}))
	t.Cleanup(registry.tokens.Close)

	registry.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		registry.requested = append(registry.requested, request.URL.Path)

		if request.Header.Get("Authorization") != "Bearer "+fakeToken {
			registry.unauthed.Add(1)
			writer.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s/token",service="fake.registry",scope="repository:library/nginx:pull"`,
				registry.tokens.URL,
			))
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Matched in full rather than by suffix. A lenient fake here hid a missing
		// /v2/ prefix that the real Docker Hub answers with a bare 404.
		switch request.URL.Path {
		case "/v2/nginx/manifests/1.25", "/v2/nginx/manifests/" + digestOf(registry.manifest):
			registry.manifests.Add(1)
			writer.Header().Set("Content-Type", docker.MediaTypeOCIManifest)
			writer.Write(registry.manifest)

		case "/v2/nginx/blobs/" + digestOf(registry.config):
			writer.Write(registry.config)

		case "/v2/nginx/blobs/" + digestOf(registry.layer):
			writer.Write(registry.layer)

		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(registry.server.Close)

	return registry
}

func (i *testInstance) proxyOf(name, remote string) *registryClient {
	i.t.Helper()

	expectStatus(i.t, i.api(http.MethodPost, "/api/repositories",
		`{"name":"`+name+`","format":"docker","type":"proxy","policy":"mixed","remoteUrl":"`+remote+`"}`),
		http.StatusCreated)

	return &registryClient{instance: i, repository: name}
}

func TestDockerProxyPullThrough(t *testing.T) {
	upstream := newFakeRegistry(t)

	instance := newTestInstance(t)
	instance.setup()
	proxy := instance.proxyOf("hub", upstream.server.URL)

	t.Run("a manifest is fetched and cached", func(t *testing.T) {
		response := proxy.do(http.MethodGet, "/v2/hub/nginx/manifests/1.25", nil, "")
		body := expectStatus(t, response, http.StatusOK)

		if digestOf([]byte(body)) != digestOf(upstream.manifest) {
			t.Fatal("the served manifest is not the one upstream holds")
		}
		if got := response.Header.Get(docker.ContentDigestHeader); got != digestOf(upstream.manifest) {
			t.Fatalf("%s = %q, want %q", docker.ContentDigestHeader, got, digestOf(upstream.manifest))
		}
	})

	t.Run("the token was obtained rather than the credentials being sent", func(t *testing.T) {
		if upstream.issued.Load() == 0 {
			t.Fatal("no token was ever requested from the token service")
		}
		if upstream.unauthed.Load() == 0 {
			t.Fatal("the first request already carried a token, so no challenge was answered")
		}
	})

	t.Run("the layer is fetched and cached", func(t *testing.T) {
		layer := digestOf(upstream.layer)

		body := expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/blobs/"+layer, nil, ""), http.StatusOK)
		if digestOf([]byte(body)) != layer {
			t.Fatal("the served layer does not match its digest")
		}
	})

	t.Run("the token is reused rather than refetched per request", func(t *testing.T) {
		issued := upstream.issued.Load()

		expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/blobs/"+digestOf(upstream.config), nil, ""), http.StatusOK)

		if upstream.issued.Load() != issued {
			t.Fatalf("a cached token was not reused: issued went from %d to %d", issued, upstream.issued.Load())
		}
	})

	t.Run("the cached image appears in the UI", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/hub/artifacts?name=nginx", ""), http.StatusOK)

		if !strings.Contains(body, `"version":"1.25"`) {
			t.Fatalf("the cached tag is not a version: %s", body)
		}
	})

	t.Run("the cached manifest was indexed, so its layers are browsable", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/hub/docker/manifests?name=nginx&reference=1.25", ""), http.StatusOK)

		var payload struct {
			LayerCount int `json:"layerCount"`
			Layers     []struct {
				Digest string `json:"digest"`
				Stored bool   `json:"stored"`
			} `json:"layers"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("decoding the manifest: %v", err)
		}
		if payload.LayerCount != 1 || len(payload.Layers) != 1 {
			t.Fatalf("layers = %v, want one", payload.Layers)
		}
		if payload.Layers[0].Digest != digestOf(upstream.layer) {
			t.Fatalf("layer digest = %q, want %q", payload.Layers[0].Digest, digestOf(upstream.layer))
		}
		// It was pulled in an earlier subtest, so it is present locally too.
		if !payload.Layers[0].Stored {
			t.Fatal("the pulled layer is not reported as stored")
		}
	})
}

// A blob is immutable by construction, so a proxy must never recheck one. A tag must,
// because upstream can move it.
func TestDockerProxyCachesContentForever(t *testing.T) {
	upstream := newFakeRegistry(t)

	instance := newTestInstance(t)
	instance.setup()
	proxy := instance.proxyOf("hub", upstream.server.URL)

	layer := digestOf(upstream.layer)
	expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/blobs/"+layer, nil, ""), http.StatusOK)

	before := len(upstream.requested)
	for range 3 {
		expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/blobs/"+layer, nil, ""), http.StatusOK)
	}

	if len(upstream.requested) != before {
		t.Fatalf("the upstream was asked again for an immutable blob: %v", upstream.requested[before:])
	}
}

func TestDockerProxyIsReadOnly(t *testing.T) {
	upstream := newFakeRegistry(t)

	instance := newTestInstance(t)
	instance.setup()
	proxy := instance.proxyOf("hub", upstream.server.URL)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"opening an upload", http.MethodPost, "/v2/hub/nginx/blobs/uploads/"},
		{"pushing a manifest", http.MethodPut, "/v2/hub/nginx/manifests/1.0"},
		{"deleting a manifest", http.MethodDelete, "/v2/hub/nginx/manifests/1.0"},
		{"deleting a blob", http.MethodDelete, "/v2/hub/nginx/blobs/" + digestOf(upstream.layer)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, proxy.do(testCase.method, testCase.path, nil, ""), http.StatusMethodNotAllowed)
		})
	}
}

func TestDockerProxyReportsAnUpstreamMiss(t *testing.T) {
	upstream := newFakeRegistry(t)

	instance := newTestInstance(t)
	instance.setup()
	proxy := instance.proxyOf("hub", upstream.server.URL)

	expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/manifests/9.9", nil, ""), http.StatusNotFound)
}

// Docker Hub keeps its official images under an implicit library/ scope, which a
// single-segment name has to be expanded into. No other registry does, and applying it
// everywhere would break every private registry's top-level image.
func TestUpstreamImageNaming(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		image  string
		want   string
	}{
		{"a bare name on Docker Hub", "https://registry-1.docker.io", "nginx", "library/nginx"},
		{"a bare name on index.docker.io", "https://index.docker.io", "nginx", "library/nginx"},
		{"a scoped name on Docker Hub", "https://registry-1.docker.io", "bitnami/nginx", "bitnami/nginx"},
		{"a bare name elsewhere", "https://ghcr.io", "nginx", "nginx"},
		{"a bare name on a private registry", "https://registry.example.com", "internal", "internal"},
		{"a deep name elsewhere", "https://quay.io", "a/b/c", "a/b/c"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &models.Repository{RemoteURL: testCase.remote}
			if got := upstreamImage(repository, testCase.image); got != testCase.want {
				t.Fatalf("upstreamImage(%q, %q) = %q, want %q", testCase.remote, testCase.image, got, testCase.want)
			}
		})
	}
}

// A proxy always fetches a manifest before the config blob it points at, so indexing
// had nothing to read the platform from. It has to be repaired once the blob arrives,
// or every proxied image reports an unknown architecture for ever.
func TestDockerProxyBackfillsThePlatform(t *testing.T) {
	upstream := newFakeRegistry(t)

	instance := newTestInstance(t)
	instance.setup()
	proxy := instance.proxyOf("hub", upstream.server.URL)

	expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/manifests/1.25", nil, ""), http.StatusOK)

	read := func() (os, architecture string, created int64) {
		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/hub/docker/manifests?name=nginx&reference=1.25", ""), http.StatusOK)

		var payload struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			ImageCreated int64  `json:"imageCreated"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("decoding the manifest: %v", err)
		}
		return payload.OS, payload.Architecture, payload.ImageCreated
	}

	t.Run("the platform is unknown while only the manifest is cached", func(t *testing.T) {
		os, architecture, _ := read()
		if os != "" || architecture != "" {
			t.Fatalf("platform = %s/%s, want empty before the config is pulled", os, architecture)
		}
	})

	t.Run("pulling the config fills it in", func(t *testing.T) {
		expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/blobs/"+digestOf(upstream.config), nil, ""), http.StatusOK)

		os, architecture, created := read()
		if os != "linux" || architecture != "amd64" {
			t.Fatalf("platform = %s/%s, want linux/amd64", os, architecture)
		}
		if created == 0 {
			t.Fatal("the build time was not read from the config")
		}
	})
}
