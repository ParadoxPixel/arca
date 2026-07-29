package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"arca/internal/migrate"
	"arca/internal/nexus"
)

// fakeNexus serves the two REST endpoints a migration reads, plus the raw
// content paths, and records every request so the test can prove it only read.
type fakeNexus struct {
	server *httptest.Server

	mutex   sync.Mutex
	methods map[string]int
	assets  map[string]map[string]string
	repos   string
}

func newFakeNexus(t *testing.T, repos string, assets map[string]map[string]string) *fakeNexus {
	t.Helper()

	remote := &fakeNexus{methods: map[string]int{}, assets: assets, repos: repos}
	remote.server = httptest.NewServer(http.HandlerFunc(remote.serve))
	t.Cleanup(remote.server.Close)

	return remote
}

func (n *fakeNexus) serve(writer http.ResponseWriter, request *http.Request) {
	n.mutex.Lock()
	n.methods[request.Method]++
	n.mutex.Unlock()

	switch {
	case request.URL.Path == "/service/rest/v1/repositories":
		fmt.Fprint(writer, n.repos)

	case request.URL.Path == "/service/rest/v1/assets":
		n.serveAssets(writer, request.URL.Query().Get("repository"))

	case strings.HasPrefix(request.URL.Path, "/repository/"):
		n.serveContent(writer, strings.TrimPrefix(request.URL.Path, "/repository/"))

	default:
		http.NotFound(writer, request)
	}
}

func (n *fakeNexus) serveAssets(writer http.ResponseWriter, repository string) {
	items := []map[string]any{}
	for path, content := range n.assets[repository] {
		items = append(items, map[string]any{
			"path":        path,
			"downloadUrl": n.server.URL + "/repository/" + repository + "/" + path,
			"fileSize":    len(content),
		})
	}
	json.NewEncoder(writer).Encode(map[string]any{"items": items, "continuationToken": ""})
}

func (n *fakeNexus) serveContent(writer http.ResponseWriter, target string) {
	repository, path, _ := strings.Cut(target, "/")
	content, ok := n.assets[repository][path]
	if !ok {
		http.NotFound(writer, nil)
		return
	}
	fmt.Fprint(writer, content)
}

func (n *fakeNexus) writeRequests() int {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	writes := 0
	for method, count := range n.methods {
		if method != http.MethodGet && method != http.MethodHead {
			writes += count
		}
	}
	return writes
}

// maven-snapshots is deliberately one of the names arca seeds itself, so the
// migration meets a repository that already exists.
const fakeRepositories = `[
  {"name":"p2-releases","format":"raw","type":"hosted"},
  {"name":"bi-snapshots","format":"maven2","type":"hosted","attributes":{"maven":{"versionPolicy":"SNAPSHOT"}}},
  {"name":"maven-snapshots","format":"maven2","type":"hosted","attributes":{"maven":{"versionPolicy":"SNAPSHOT"}}},
  {"name":"maven-central","format":"maven2","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://repo1.maven.org/maven2/"}}},
  {"name":"maven-public","format":"maven2","type":"group","attributes":{"group":{"memberNames":["maven-central","maven-snapshots"]}}},
  {"name":"docker-hosted","format":"docker","type":"hosted"}
]`

func migrationFixture(t *testing.T) (*testInstance, *fakeNexus, *migrate.Runner, []migrate.Decision) {
	t.Helper()

	instance := newTestInstance(t)
	instance.setup()

	remote := newFakeNexus(t, fakeRepositories, map[string]map[string]string{
		"p2-releases": {
			"logging/2.0/artifacts.jar":                "artifacts-bytes",
			"logging/2.0/content.jar":                  "content-bytes",
			"logging/2.0/plugins/slf4j.api_2.0.16.jar": "slf4j-bytes",
		},
		"bi-snapshots": {
			"com/example/app/1.0.0-SNAPSHOT/app-1.0.0-SNAPSHOT.jar": "app-bytes",
		},
		"maven-snapshots": {
			"com/example/seeded/1.0.0-SNAPSHOT/seeded-1.0.0-SNAPSHOT.jar": "seeded-bytes",
		},
	})

	source, err := nexus.New(remote.server.URL, "reader", "secret", 30*time.Second)
	if err != nil {
		t.Fatalf("nexus client: %v", err)
	}
	options := migrate.Options{Visibility: "public"}
	repositories, err := source.Repositories(context.Background())
	if err != nil {
		t.Fatalf("listing repositories: %v", err)
	}

	runner := &migrate.Runner{
		Source:  source,
		Target:  &localDestination{server: instance.app},
		Options: options,
		Workers: 3,
	}
	return instance, remote, runner, migrate.Plan(repositories, options)
}

func TestMigrationReproducesNexusRepositories(t *testing.T) {
	instance, remote, runner, plan := migrationFixture(t)

	results := runner.Apply(context.Background(), plan)

	byName := map[string]migrate.Result{}
	for _, result := range results {
		byName[result.Decision.Name()] = result
	}

	t.Run("hosted assets are copied", func(t *testing.T) {
		for _, name := range []string{"p2-releases", "bi-snapshots"} {
			result := byName[name]
			if result.Failed != 0 {
				t.Fatalf("%s reported failures: %v", name, result.Errors)
			}
			if !result.Created {
				t.Fatalf("%s was not created", name)
			}
		}
		if copied := byName["p2-releases"].Copied; copied != 3 {
			t.Fatalf("p2-releases copied %d assets, want 3", copied)
		}
	})

	// An existing repository keeps its own configuration. Reconfiguring one
	// mid-migration would be a surprising way to lose a setting.
	t.Run("an existing repository is filled but not reconfigured", func(t *testing.T) {
		result := byName["maven-snapshots"]
		if !result.Existed {
			t.Fatal("the seeded repository was not recognised as existing")
		}
		if result.Copied != 1 {
			t.Fatalf("copied %d assets into the existing repository, want 1", result.Copied)
		}

		body := expectStatus(t, instance.maven(http.MethodGet,
			"/repository/maven-snapshots/com/example/seeded/1.0.0-SNAPSHOT/seeded-1.0.0-SNAPSHOT.jar", "", true),
			http.StatusOK)
		if body != "seeded-bytes" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("the copied files are readable from arca", func(t *testing.T) {
		cases := map[string]string{
			"/repository/p2-releases/logging/2.0/artifacts.jar":                              "artifacts-bytes",
			"/repository/p2-releases/logging/2.0/plugins/slf4j.api_2.0.16.jar":               "slf4j-bytes",
			"/repository/bi-snapshots/com/example/app/1.0.0-SNAPSHOT/app-1.0.0-SNAPSHOT.jar": "app-bytes",
		}
		for path, want := range cases {
			t.Run(path, func(t *testing.T) {
				if body := expectStatus(t, instance.maven(http.MethodGet, path, "", true), http.StatusOK); body != want {
					t.Fatalf("body = %q, want %q", body, want)
				}
			})
		}
	})

	t.Run("the proxy is recreated without copying its cache", func(t *testing.T) {
		result := byName["maven-central"]
		if !result.Created {
			t.Fatal("the proxy was not created")
		}
		if result.Copied != 0 {
			t.Fatalf("the proxy cache was copied (%d assets)", result.Copied)
		}

		body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/maven-central", ""), http.StatusOK)
		if !strings.Contains(body, `"remoteUrl":"https://repo1.maven.org/maven2"`) {
			t.Fatalf("the upstream did not carry across: %s", body)
		}
	})

	t.Run("the snapshot policy carries across", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/bi-snapshots", ""), http.StatusOK)
		if !strings.Contains(body, `"policy":"snapshot"`) {
			t.Fatalf("the version policy did not carry across: %s", body)
		}
	})

	t.Run("unsupported repositories are skipped, not invented", func(t *testing.T) {
		for _, name := range []string{"maven-public", "docker-hosted"} {
			if byName[name].Created {
				t.Fatalf("%s should not have been created", name)
			}
			response := instance.api(http.MethodGet, "/api/repositories/"+name, "")
			defer response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("%s exists in arca with %d", name, response.StatusCode)
			}
		}
	})

	t.Run("nexus was only ever read", func(t *testing.T) {
		if writes := remote.writeRequests(); writes != 0 {
			t.Fatalf("the migration sent %d non-read requests to nexus", writes)
		}
	})
}

// A migration of thousands of files will be interrupted. The second run has to
// pick up where the first stopped rather than transfer everything again.
func TestMigrationIsResumable(t *testing.T) {
	_, _, runner, plan := migrationFixture(t)

	first := runner.Apply(context.Background(), plan)
	second := runner.Apply(context.Background(), plan)

	byName := map[string]migrate.Result{}
	for _, result := range second {
		byName[result.Decision.Name()] = result
	}

	for _, result := range first {
		if result.Failed != 0 {
			t.Fatalf("%s failed on the first run: %v", result.Decision.Name(), result.Errors)
		}
	}

	for _, name := range []string{"p2-releases", "bi-snapshots"} {
		result := byName[name]
		if !result.Existed {
			t.Errorf("%s was not recognised as already created", name)
		}
		if result.Copied != 0 {
			t.Errorf("%s copied %d assets again", name, result.Copied)
		}
		if result.Skipped == 0 {
			t.Errorf("%s skipped nothing, so it did not detect the existing files", name)
		}
		if result.Failed != 0 {
			t.Errorf("%s failed on the second run: %v", name, result.Errors)
		}
	}
}
