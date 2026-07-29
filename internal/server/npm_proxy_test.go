package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	npmProxyRepository = "npm-proxy"
	proxiedPackage     = "@types/node"
	proxiedVersion     = "20.1.0"
	proxiedTarball     = "upstream-tarball-bytes"
)

// npmUpstream stands in for a registry that serves its tarballs from a
// different host than its packuments, which is the case a string replace of the
// upstream base URL would silently fail to rewrite.
type npmUpstream struct {
	server *httptest.Server

	mutex   sync.Mutex
	hits    map[string]int
	accepts map[string]string
	files   map[string]string
}

func newNPMUpstream(t *testing.T, files map[string]string) *npmUpstream {
	t.Helper()

	remote := &npmUpstream{hits: map[string]int{}, accepts: map[string]string{}, files: files}
	remote.server = httptest.NewServer(http.HandlerFunc(remote.serve))
	t.Cleanup(remote.server.Close)

	return remote
}

func (u *npmUpstream) serve(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, "/")

	u.mutex.Lock()
	u.hits[path]++
	u.accepts[path] = request.Header.Get("Accept")
	content, present := u.files[path]
	u.mutex.Unlock()

	if !present {
		http.NotFound(writer, request)
		return
	}
	fmt.Fprint(writer, content)
}

func (u *npmUpstream) hitsFor(path string) int {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	return u.hits[path]
}

func (u *npmUpstream) acceptFor(path string) string {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	return u.accepts[path]
}

func upstreamPackument(cdn string) string {
	return fmt.Sprintf(`{
		"name": %q,
		"dist-tags": {"latest": %q, "next": "21.0.0-rc.1"},
		"readme": "an upstream readme nobody installs",
		"versions": {
			%q: {
				"name": %q,
				"version": %q,
				"license": "MIT",
				"scripts": {"build": "tsc"},
				"dist": {
					"tarball": %q,
					"shasum": "irrelevant"
				}
			}
		}
	}`,
		proxiedPackage, proxiedVersion,
		proxiedVersion, proxiedPackage, proxiedVersion,
		cdn+"/downloads/"+proxiedPackage+"/-/node-"+proxiedVersion+".tgz",
	)
}

func newNPMProxyInstance(t *testing.T, extra string) (*testInstance, *npmUpstream) {
	t.Helper()

	instance := newTestInstance(t)
	instance.setup()

	remote := newNPMUpstream(t, map[string]string{
		proxiedPackage: upstreamPackument("https://cdn.example.test"),
		proxiedPackage + "/-/node-" + proxiedVersion + ".tgz": proxiedTarball,
	})

	body := fmt.Sprintf(
		`{"name":%q,"format":"npm","type":"proxy","policy":"mixed","visibility":"public","remoteUrl":%q%s}`,
		npmProxyRepository, remote.server.URL, extra,
	)
	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", body), http.StatusCreated)

	return instance, remote
}

func (i *testInstance) npmProxy(method, path string, headers map[string]string) *http.Response {
	i.t.Helper()

	request := i.request(method, "/repository/"+npmProxyRepository+"/"+path, "")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return i.do(request)
}

func decodePackument(t *testing.T, body string) packument {
	t.Helper()

	var decoded packument
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decoding a packument: %v (%s)", err, body)
	}
	return decoded
}

func TestNPMProxyRewritesTarballURLsToThisServer(t *testing.T) {
	instance, _ := newNPMProxyInstance(t, "")

	body := expectStatus(t, instance.npmProxy(http.MethodGet, proxiedPackage, nil), http.StatusOK)
	decoded := decodePackument(t, body)

	want := instance.server.URL + "/repository/" + npmProxyRepository + "/" + proxiedPackage + "/-/node-" + proxiedVersion + ".tgz"
	if got := decoded.Versions[proxiedVersion].Dist.Tarball; got != want {
		t.Fatalf("tarball = %q, want %q", got, want)
	}
	if strings.Contains(body, "cdn.example.test") {
		t.Fatalf("the upstream tarball host survived: %s", body)
	}

	t.Run("dist-tags are passed through", func(t *testing.T) {
		if got := decoded.DistTags["latest"]; got != proxiedVersion {
			t.Fatalf("latest = %q, want %q", got, proxiedVersion)
		}
	})
}

func TestNPMProxyCachesTarballsUnderTheirCoordinates(t *testing.T) {
	instance, remote := newNPMProxyInstance(t, "")

	const tarball = proxiedPackage + "/-/node-" + proxiedVersion + ".tgz"

	for attempt := range 2 {
		t.Run(fmt.Sprintf("attempt %d", attempt+1), func(t *testing.T) {
			if body := expectStatus(t, instance.npmProxy(http.MethodGet, tarball, nil), http.StatusOK); body != proxiedTarball {
				t.Fatalf("body = %q, want %q", body, proxiedTarball)
			}
		})
	}

	if hits := remote.hitsFor(tarball); hits != 1 {
		t.Fatalf("upstream was asked %d times, want 1", hits)
	}

	t.Run("the cached tarball is browsable as a component", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/"+npmProxyRepository+"/browse?path="+proxiedPackage, ""), http.StatusOK)
		if !strings.Contains(body, proxiedVersion) {
			t.Fatalf("browse did not resolve the cached version: %s", body)
		}
	})

	t.Run("the cached tarball is searchable", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet, "/api/search?q=node", ""), http.StatusOK)
		if !strings.Contains(body, `"namespace":"types"`) {
			t.Fatalf("search did not find the cached package: %s", body)
		}
	})
}

func TestNPMProxyServesTheAbbreviatedInstallDocument(t *testing.T) {
	instance, _ := newNPMProxyInstance(t, "")

	body := expectStatus(t, instance.npmProxy(http.MethodGet, proxiedPackage, map[string]string{
		"Accept": "application/vnd.npm.install-v1+json",
	}), http.StatusOK)

	testCases := []struct {
		name  string
		field string
		want  bool
	}{
		{"the install fields are kept", "dist-tags", true},
		{"the readme is dropped", "readme", false},
		{"per-version scripts are dropped", "scripts", false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if strings.Contains(body, testCase.field) != testCase.want {
				t.Fatalf("%q present = %v, want %v (body: %s)", testCase.field, !testCase.want, testCase.want, body)
			}
		})
	}

	t.Run("tarballs are still rewritten", func(t *testing.T) {
		want := instance.server.URL + "/repository/" + npmProxyRepository
		if got := decodePackument(t, body).Versions[proxiedVersion].Dist.Tarball; !strings.HasPrefix(got, want) {
			t.Fatalf("tarball = %q, want a URL under %q", got, want)
		}
	})
}

func TestNPMProxyNegotiatesJSONForPackumentsOnly(t *testing.T) {
	instance, remote := newNPMProxyInstance(t, "")

	const tarball = proxiedPackage + "/-/node-" + proxiedVersion + ".tgz"
	expectStatus(t, instance.npmProxy(http.MethodGet, proxiedPackage, map[string]string{
		"Accept": "application/vnd.npm.install-v1+json",
	}), http.StatusOK)
	expectStatus(t, instance.npmProxy(http.MethodGet, tarball, nil), http.StatusOK)

	testCases := []struct {
		name string
		path string
		want string
	}{
		// The cache holds one full packument whatever the client asked for.
		{"packument", proxiedPackage, "application/json"},
		{"tarball", tarball, "*/*"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := remote.acceptFor(testCase.path); got != testCase.want {
				t.Fatalf("upstream Accept = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestNPMProxyServesSingleVersionsAndDistTags(t *testing.T) {
	instance, remote := newNPMProxyInstance(t, "")

	t.Run("a single version", func(t *testing.T) {
		body := expectStatus(t, instance.npmProxy(http.MethodGet, proxiedPackage+"/"+proxiedVersion, nil), http.StatusOK)

		var manifest struct {
			Version string `json:"version"`
			Dist    struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
		}
		if err := json.Unmarshal([]byte(body), &manifest); err != nil {
			t.Fatalf("decoding a version manifest: %v (%s)", err, body)
		}
		if manifest.Version != proxiedVersion {
			t.Fatalf("version = %q, want %q", manifest.Version, proxiedVersion)
		}
		if !strings.HasPrefix(manifest.Dist.Tarball, instance.server.URL) {
			t.Fatalf("tarball = %q, want a URL on this server", manifest.Dist.Tarball)
		}
	})

	t.Run("an unknown version", func(t *testing.T) {
		expectStatus(t, instance.npmProxy(http.MethodGet, proxiedPackage+"/9.9.9", nil), http.StatusNotFound)
	})

	t.Run("dist-tags", func(t *testing.T) {
		body := expectStatus(t, instance.npmProxy(http.MethodGet, "-/package/"+proxiedPackage+"/dist-tags", nil), http.StatusOK)

		tags := map[string]string{}
		if err := json.Unmarshal([]byte(body), &tags); err != nil {
			t.Fatalf("decoding dist-tags: %v (%s)", err, body)
		}
		if tags["latest"] != proxiedVersion || tags["next"] != "21.0.0-rc.1" {
			t.Fatalf("tags = %v, want the upstream tags", tags)
		}
	})

	// Every read above came out of the one cached document.
	if hits := remote.hitsFor(proxiedPackage); hits != 1 {
		t.Fatalf("upstream was asked %d times, want 1", hits)
	}
}

func TestNPMProxyNegativeCachesUnknownPackages(t *testing.T) {
	instance, remote := newNPMProxyInstance(t, "")

	for attempt := range 2 {
		t.Run(fmt.Sprintf("attempt %d", attempt+1), func(t *testing.T) {
			expectStatus(t, instance.npmProxy(http.MethodGet, "absent-package", nil), http.StatusNotFound)
		})
	}

	if hits := remote.hitsFor("absent-package"); hits != 1 {
		t.Fatalf("upstream was asked %d times, want 1", hits)
	}
}

func TestNPMProxyRevalidatesExpiredPackuments(t *testing.T) {
	instance, remote := newNPMProxyInstance(t, `,"metadataTtlSeconds":0`)

	for attempt := range 2 {
		t.Run(fmt.Sprintf("attempt %d", attempt+1), func(t *testing.T) {
			body := expectStatus(t, instance.npmProxy(http.MethodGet, proxiedPackage, nil), http.StatusOK)
			if got := decodePackument(t, body).DistTags["latest"]; got != proxiedVersion {
				t.Fatalf("latest = %q, want %q", got, proxiedVersion)
			}
		})
	}

	if hits := remote.hitsFor(proxiedPackage); hits != 2 {
		t.Fatalf("upstream was asked %d times, want 2 (a fetch and a revalidation)", hits)
	}

	t.Run("a cached tarball is not revalidated", func(t *testing.T) {
		const tarball = proxiedPackage + "/-/node-" + proxiedVersion + ".tgz"

		expectStatus(t, instance.npmProxy(http.MethodGet, tarball, nil), http.StatusOK)
		expectStatus(t, instance.npmProxy(http.MethodGet, tarball, nil), http.StatusOK)

		if hits := remote.hitsFor(tarball); hits != 1 {
			t.Fatalf("upstream was asked %d times, want 1: a published version is immutable", hits)
		}
	})
}

func TestNPMProxyRejectsWrites(t *testing.T) {
	instance, _ := newNPMProxyInstance(t, "")

	testCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"publish", http.MethodPut, proxiedPackage, publishBody(proxiedPackage, "1.0.0", "bytes", "")},
		{"set a dist-tag", http.MethodPut, "-/package/" + proxiedPackage + "/dist-tags/beta", `"1.0.0"`},
		{"unpublish", http.MethodDelete, proxiedPackage + "/-rev/1", ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := instance.request(testCase.method, "/repository/"+npmProxyRepository+"/"+testCase.path, testCase.body)
			request.SetBasicAuth(administratorEmail, administratorPassword)
			expectStatus(t, instance.do(request), http.StatusMethodNotAllowed)
		})
	}
}
