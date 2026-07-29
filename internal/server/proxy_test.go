package server

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	upstreamPrefix = "/maven2/"
	jarPath        = "com/example/app/1.0.0/app-1.0.0.jar"
	jarContent     = "upstream-artifact-bytes"
	metadataPath   = "com/example/app/maven-metadata.xml"
)

const upstreamMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>com.example</groupId>
  <artifactId>app</artifactId>
  <versioning>
    <latest>9.9.9</latest>
    <release>9.9.9</release>
    <versions>
      <version>1.0.0</version>
      <version>9.9.9</version>
    </versions>
  </versioning>
</metadata>`

type upstream struct {
	server *httptest.Server

	mutex sync.Mutex
	hits  map[string]int
	files map[string]string
	etags map[string]string
	delay time.Duration
	down  bool
}

func newUpstream(t *testing.T, files map[string]string) *upstream {
	t.Helper()

	remote := &upstream{hits: map[string]int{}, files: files, etags: map[string]string{}}
	remote.server = httptest.NewServer(http.HandlerFunc(remote.serve))
	t.Cleanup(remote.server.Close)

	return remote
}

func (u *upstream) serve(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, upstreamPrefix)

	u.mutex.Lock()
	u.hits[path]++
	content, present := u.files[path]
	etag, delay, down := u.etags[path], u.delay, u.down
	u.mutex.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if down {
		http.Error(writer, "upstream is down", http.StatusInternalServerError)
		return
	}
	if !present {
		http.NotFound(writer, request)
		return
	}

	if etag != "" {
		writer.Header().Set("ETag", etag)
		if request.Header.Get("If-None-Match") == etag {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
	}

	fmt.Fprint(writer, content)
}

func (u *upstream) baseURL() string { return u.server.URL + strings.TrimSuffix(upstreamPrefix, "/") }

func (u *upstream) hitsFor(path string) int {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	return u.hits[path]
}

func (u *upstream) set(mutate func(*upstream)) {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	mutate(u)
}

func (i *testInstance) createProxy(name, remoteURL, extra string) {
	i.t.Helper()

	body := fmt.Sprintf(`{"name":%q,"type":"proxy","visibility":"public","remoteUrl":%q%s}`, name, remoteURL, extra)
	expectStatus(i.t, i.api(http.MethodPost, "/api/repositories", body), http.StatusCreated)
}

// cachedFiles counts what is actually on disk, which is the half of an eviction
// the database cannot vouch for.
func (i *testInstance) cachedFiles() int {
	i.t.Helper()

	count := 0
	err := filepath.WalkDir(i.storage, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".uploads" {
				return fs.SkipDir
			}
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		i.t.Fatalf("walking the storage directory: %v", err)
	}
	return count
}

func newProxyInstance(t *testing.T, files map[string]string, extra string) (*testInstance, *upstream) {
	t.Helper()

	instance := newTestInstance(t)
	instance.setup()

	remote := newUpstream(t, files)
	instance.createProxy("central", remote.baseURL(), extra)

	return instance, remote
}

func TestProxyCachesUpstreamArtifacts(t *testing.T) {
	instance, remote := newProxyInstance(t, map[string]string{jarPath: jarContent}, "")

	const request = "/repository/central/" + jarPath

	first := instance.maven(http.MethodGet, request, "", true)
	if body := expectStatus(t, first, http.StatusOK); body != jarContent {
		t.Fatalf("body = %q, want %q", body, jarContent)
	}
	if got := first.Header.Get("Content-Type"); got != "application/java-archive" {
		t.Fatalf("content type = %q, want application/java-archive", got)
	}

	if body := expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK); body != jarContent {
		t.Fatalf("second body = %q, want %q", body, jarContent)
	}

	if hits := remote.hitsFor(jarPath); hits != 1 {
		t.Fatalf("upstream was asked %d times, want 1", hits)
	}

	t.Run("the cached artifact is browsable", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/central/browse?path=com/example/app", ""), http.StatusOK)
		if !strings.Contains(body, `"name":"app","namespace":"com.example"`) {
			t.Fatalf("browse did not resolve the cached artifact: %s", body)
		}
	})
}

func TestProxyServesUpstreamMetadataInsteadOfGeneratingIt(t *testing.T) {
	instance, _ := newProxyInstance(t, map[string]string{
		jarPath:      jarContent,
		metadataPath: upstreamMetadata,
	}, "")

	expectStatus(t, instance.maven(http.MethodGet, "/repository/central/"+jarPath, "", true), http.StatusOK)

	body := expectStatus(t, instance.maven(http.MethodGet, "/repository/central/"+metadataPath, "", true), http.StatusOK)

	// 9.9.9 was never cached, so a generated document could not mention it.
	if !strings.Contains(body, "<version>9.9.9</version>") {
		t.Fatalf("metadata was generated locally instead of proxied:\n%s", body)
	}
}

func TestProxyRevalidatesExpiredMetadata(t *testing.T) {
	instance, remote := newProxyInstance(t, map[string]string{metadataPath: upstreamMetadata}, `,"metadataTtlSeconds":0`)
	remote.set(func(u *upstream) { u.etags[metadataPath] = `"v1"` })

	const request = "/repository/central/" + metadataPath

	if body := expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK); body != upstreamMetadata {
		t.Fatalf("first body = %q", body)
	}
	if body := expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK); body != upstreamMetadata {
		t.Fatalf("body after revalidation = %q, want the cached copy", body)
	}

	if hits := remote.hitsFor(metadataPath); hits != 2 {
		t.Fatalf("upstream was asked %d times, want 2 (a fetch and a revalidation)", hits)
	}
}

func TestProxyNegativeCachesMissingPaths(t *testing.T) {
	instance, remote := newProxyInstance(t, map[string]string{}, "")

	const missing = "com/example/absent/1.0.0/absent-1.0.0.jar"

	for attempt := range 2 {
		t.Run(fmt.Sprintf("attempt %d", attempt+1), func(t *testing.T) {
			expectStatus(t, instance.maven(http.MethodGet, "/repository/central/"+missing, "", true), http.StatusNotFound)
		})
	}

	if hits := remote.hitsFor(missing); hits != 1 {
		t.Fatalf("upstream was asked %d times, want 1", hits)
	}
}

func TestProxySynthesizesChecksumsTheUpstreamLacks(t *testing.T) {
	instance, _ := newProxyInstance(t, map[string]string{jarPath: jarContent}, "")

	expectStatus(t, instance.maven(http.MethodGet, "/repository/central/"+jarPath, "", true), http.StatusOK)

	body := expectStatus(t, instance.maven(http.MethodGet, "/repository/central/"+jarPath+".sha1", "", true), http.StatusOK)

	digest := sha1.Sum([]byte(jarContent))
	if want := hex.EncodeToString(digest[:]); body != want {
		t.Fatalf("checksum = %q, want %q", body, want)
	}
}

func TestProxyServesStaleCopiesWhenTheUpstreamFails(t *testing.T) {
	instance, remote := newProxyInstance(t, map[string]string{metadataPath: upstreamMetadata}, `,"metadataTtlSeconds":0`)

	const request = "/repository/central/" + metadataPath
	expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK)

	remote.set(func(u *upstream) { u.down = true })

	if body := expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK); body != upstreamMetadata {
		t.Fatalf("body = %q, want the stale cached copy", body)
	}
}

func TestProxyReportsUpstreamFailuresWithoutACachedCopy(t *testing.T) {
	instance, remote := newProxyInstance(t, map[string]string{jarPath: jarContent}, "")
	remote.set(func(u *upstream) { u.down = true })

	expectStatus(t, instance.maven(http.MethodGet, "/repository/central/"+jarPath, "", true), http.StatusBadGateway)
}

func TestProxyCollapsesConcurrentFetches(t *testing.T) {
	instance, remote := newProxyInstance(t, map[string]string{jarPath: jarContent}, "")
	remote.set(func(u *upstream) { u.delay = 100 * time.Millisecond })

	const callers = 8

	var group sync.WaitGroup
	bodies := make([]string, callers)

	for index := range callers {
		group.Add(1)
		go func() {
			defer group.Done()

			response, err := instance.client.Do(instance.request(http.MethodGet, "/repository/central/"+jarPath, ""))
			if err != nil {
				return
			}
			defer response.Body.Close()

			content := make([]byte, len(jarContent))
			read, _ := response.Body.Read(content)
			bodies[index] = string(content[:read])
		}()
	}
	group.Wait()

	if hits := remote.hitsFor(jarPath); hits != 1 {
		t.Fatalf("upstream was asked %d times, want 1", hits)
	}
	for index, body := range bodies {
		if body != jarContent {
			t.Fatalf("caller %d read %q, want %q", index, body, jarContent)
		}
	}
}

func TestProxyRejectsWrites(t *testing.T) {
	instance, _ := newProxyInstance(t, map[string]string{jarPath: jarContent}, "")

	cases := []struct {
		method string
		body   string
	}{
		{http.MethodPut, "payload"},
		{http.MethodPost, "payload"},
		{http.MethodDelete, ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.method, func(t *testing.T) {
			response := instance.maven(testCase.method, "/repository/central/"+jarPath, testCase.body, true)
			expectStatus(t, response, http.StatusMethodNotAllowed)
		})
	}
}

func TestProxyCachePurgeForcesARefetch(t *testing.T) {
	instance, remote := newProxyInstance(t, map[string]string{jarPath: jarContent}, "")

	const request = "/repository/central/" + jarPath
	expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK)

	body := expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/central/cache", ""), http.StatusOK)
	if !strings.Contains(body, `"purged":1`) {
		t.Fatalf("purge reported %s, want one removed file", body)
	}

	expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK)

	if hits := remote.hitsFor(jarPath); hits != 2 {
		t.Fatalf("upstream was asked %d times, want 2", hits)
	}

	t.Run("hosted repositories have no cache", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/maven-releases/cache", ""), http.StatusBadRequest)
	})
}

func TestProxyEvictsIdleCachedFiles(t *testing.T) {
	cases := []struct {
		name          string
		retentionDays int
		elapsed       time.Duration
		wantRemaining int
	}{
		{"unread past the window", 1, 48 * time.Hour, 0},
		{"unread inside the window", 30, 48 * time.Hour, 1},
		{"retention disabled", 0, 365 * 24 * time.Hour, 1},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			instance, remote := newProxyInstance(t, map[string]string{jarPath: jarContent},
				fmt.Sprintf(`,"cacheRetentionDays":%d`, testCase.retentionDays))

			const request = "/repository/central/" + jarPath
			expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK)

			if files := instance.cachedFiles(); files != 1 {
				t.Fatalf("cached files before eviction = %d, want 1", files)
			}

			instance.app.purgeIdleCaches(time.Now().Add(testCase.elapsed))

			if files := instance.cachedFiles(); files != testCase.wantRemaining {
				t.Fatalf("cached files on disk = %d, want %d", files, testCase.wantRemaining)
			}

			body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/central", ""), http.StatusOK)
			for _, fragment := range []string{
				fmt.Sprintf(`"assets":%d`, testCase.wantRemaining),
				fmt.Sprintf(`"components":%d`, testCase.wantRemaining),
			} {
				if !strings.Contains(body, fragment) {
					t.Fatalf("stats are missing %s:\n%s", fragment, body)
				}
			}

			expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK)

			wantHits := 1
			if testCase.wantRemaining == 0 {
				wantHits = 2
			}
			if hits := remote.hitsFor(jarPath); hits != wantHits {
				t.Fatalf("upstream was asked %d times, want %d", hits, wantHits)
			}
		})
	}
}

func TestProxyEvictionKeepsRecentlyPulledFiles(t *testing.T) {
	instance, _ := newProxyInstance(t, map[string]string{
		jarPath:      jarContent,
		metadataPath: upstreamMetadata,
	}, `,"cacheRetentionDays":1`)

	expectStatus(t, instance.maven(http.MethodGet, "/repository/central/"+jarPath, "", true), http.StatusOK)
	expectStatus(t, instance.maven(http.MethodGet, "/repository/central/"+metadataPath, "", true), http.StatusOK)

	if files := instance.cachedFiles(); files != 2 {
		t.Fatalf("cached files = %d, want 2", files)
	}

	// Half a day short of the window, so nothing is old enough to go yet.
	instance.app.purgeIdleCaches(time.Now().Add(12 * time.Hour))

	if files := instance.cachedFiles(); files != 2 {
		t.Fatalf("cached files after an early sweep = %d, want 2", files)
	}
}

func TestProxyRefetchesWhenACachedFileVanishes(t *testing.T) {
	instance, remote := newProxyInstance(t, map[string]string{jarPath: jarContent}, "")

	const request = "/repository/central/" + jarPath
	expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK)

	removed := 0
	err := filepath.WalkDir(instance.storage, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		removed++
		return os.Remove(path)
	})
	if err != nil || removed != 1 {
		t.Fatalf("removing the cached file: removed %d, err %v", removed, err)
	}

	if body := expectStatus(t, instance.maven(http.MethodGet, request, "", true), http.StatusOK); body != jarContent {
		t.Fatalf("body = %q, want the refetched artifact", body)
	}
	if hits := remote.hitsFor(jarPath); hits != 2 {
		t.Fatalf("upstream was asked %d times, want 2", hits)
	}
}

func TestProxyRepositoryValidation(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	cases := []struct {
		name string
		body string
	}{
		{"proxy without a remote", `{"name":"p1","type":"proxy"}`},
		{"unsupported scheme", `{"name":"p2","type":"proxy","remoteUrl":"ftp://example.com/maven2"}`},
		{"not a url", `{"name":"p3","type":"proxy","remoteUrl":"repo1.maven.org"}`},
		{"hosted with a remote", `{"name":"p4","remoteUrl":"https://repo1.maven.org/maven2"}`},
		{"unknown type", `{"name":"p5","type":"group"}`},
		{"negative ttl", `{"name":"p6","type":"proxy","remoteUrl":"https://repo1.maven.org/maven2","metadataTtlSeconds":-1}`},
		{"negative retention", `{"name":"p7","type":"proxy","remoteUrl":"https://repo1.maven.org/maven2","cacheRetentionDays":-1}`},
		{"hosted with retention", `{"name":"p8","cacheRetentionDays":30}`},
		{"unknown format", `{"name":"p9","format":"rubygems"}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodPost, "/api/repositories", testCase.body), http.StatusBadRequest)
		})
	}
}

func TestProxyRemoteURLIsNormalizedAndPasswordIsNeverReturned(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
		`{"name":"central","type":"proxy","remoteUrl":"https://repo1.maven.org/maven2/?x=1","remoteUsername":"ci","remotePassword":"secret"}`),
		http.StatusCreated)

	body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/central", ""), http.StatusOK)

	if !strings.Contains(body, `"remoteUrl":"https://repo1.maven.org/maven2"`) {
		t.Fatalf("remote url was not normalized: %s", body)
	}
	if strings.Contains(body, "secret") {
		t.Fatalf("the remote password leaked into the API: %s", body)
	}
}
