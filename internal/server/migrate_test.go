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
	// components maps a repository to the versions it holds and the asset paths of
	// each. A real Nexus lists a docker tag here and nowhere else.
	components map[string][]fakeComponent
	// componentOnly are paths the asset endpoint withholds while still serving their
	// content. That is exactly how a real Nexus behaves for a docker tag, and it is the
	// whole reason a migration has to walk components as well as assets.
	componentOnly map[string]bool
	repos         string
}

// fakeComponent mirrors what /service/rest/v1/components returns: a name, a version and
// the assets nested under it.
type fakeComponent struct {
	name    string
	version string
	paths   []string
}

func newFakeNexus(t *testing.T, repos string, assets map[string]map[string]string) *fakeNexus {
	t.Helper()

	remote := &fakeNexus{
		methods:       map[string]int{},
		assets:        assets,
		components:    map[string][]fakeComponent{},
		componentOnly: map[string]bool{},
		repos:         repos,
	}
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

	case request.URL.Path == "/service/rest/v1/components":
		n.serveComponents(writer, request.URL.Query().Get("repository"))

	case strings.HasPrefix(request.URL.Path, "/repository/"):
		n.serveContent(writer, strings.TrimPrefix(request.URL.Path, "/repository/"))

	default:
		http.NotFound(writer, request)
	}
}

func (n *fakeNexus) serveAssets(writer http.ResponseWriter, repository string) {
	items := []map[string]any{}
	for path, content := range n.assets[repository] {
		if n.componentOnly[repository+"/"+path] {
			continue
		}
		items = append(items, map[string]any{
			"path":        path,
			"downloadUrl": n.server.URL + "/repository/" + repository + "/" + path,
			"fileSize":    len(content),
		})
	}
	json.NewEncoder(writer).Encode(map[string]any{"items": items, "continuationToken": ""})
}

func (n *fakeNexus) serveComponents(writer http.ResponseWriter, repository string) {
	items := []map[string]any{}
	for _, component := range n.components[repository] {
		assets := []map[string]any{}
		for _, path := range component.paths {
			assets = append(assets, map[string]any{
				"path":        path,
				"downloadUrl": n.server.URL + "/repository/" + repository + "/" + path,
				"fileSize":    len(n.assets[repository][path]),
			})
		}
		items = append(items, map[string]any{
			"name":    component.name,
			"version": component.version,
			"assets":  assets,
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
  {"name":"docker-hosted","format":"docker","type":"hosted"},
  {"name":"gems","format":"rubygems","type":"hosted"}
]`

// dockerSource is a Nexus docker repository as one really looks: blobs on the shared
// path, manifests addressed by digest in the asset listing, and the tag reachable only
// through the component listing. The digests are real so the reindex can read the config
// blob the manifest points at.
type dockerSource struct {
	assets     map[string]string
	components []fakeComponent
	manifest   string
	config     string
	layer      string
}

func newDockerSource() dockerSource {
	config := testImageConfig
	layer := "a-compressed-layer"

	manifest := string(imageManifestFor(
		digestOf([]byte(config)), len(config),
		map[string]int{digestOf([]byte(layer)): len(layer)},
	))

	source := dockerSource{
		manifest: manifest,
		config:   config,
		layer:    layer,
		components: []fakeComponent{
			{name: "team/api", version: "1.0", paths: []string{"v2/team/api/manifests/1.0"}},
		},
	}
	source.assets = map[string]string{
		"v2/-/blobs/" + digestOf([]byte(config)):              config,
		"v2/-/blobs/" + digestOf([]byte(layer)):               layer,
		"v2/team/api/manifests/" + digestOf([]byte(manifest)): manifest,
		// Downloadable, but withheld from the asset listing below, which is what a real
		// Nexus does with a docker tag.
		"v2/team/api/manifests/1.0": manifest,
		// Nexus exposes paths that mean nothing to this server. They have to be counted
		// rather than dropped in silence, or a copy claims to be complete when it is not.
		"v2/team/api/tags/list": "{}",
	}
	return source
}

func migrationFixture(t *testing.T) (*testInstance, *fakeNexus, *migrate.Runner, []migrate.Decision) {
	t.Helper()

	instance := newTestInstance(t)
	instance.setup()

	images := newDockerSource()

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
		"docker-hosted": images.assets,
	})
	remote.components["docker-hosted"] = images.components
	remote.componentOnly["docker-hosted/v2/team/api/manifests/1.0"] = true

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
		for _, name := range []string{"maven-public", "gems"} {
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

// A docker migration is the case that cannot work off the asset listing alone: Nexus
// reports manifests there by digest only, so a copy that ignored the component listing
// would transfer every byte and leave nothing pullable.
func TestMigrationReproducesDockerImages(t *testing.T) {
	instance, remote, runner, plan := migrationFixture(t)
	images := newDockerSource()

	results := runner.Apply(context.Background(), plan)

	var docker migrate.Result
	for _, result := range results {
		if result.Decision.Name() == "docker-hosted" {
			docker = result
		}
	}

	t.Run("the repository was created with a mixed policy", func(t *testing.T) {
		if !docker.Created {
			t.Fatalf("docker-hosted was not created: %+v", docker)
		}
		body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/docker-hosted", ""), http.StatusOK)
		if !strings.Contains(body, `"policy":"mixed"`) {
			t.Fatalf("policy is not mixed: %s", body)
		}
	})

	t.Run("the paths this server has no place for are counted, not dropped in silence", func(t *testing.T) {
		if docker.Untranslatable != 1 {
			t.Fatalf("untranslatable = %d, want the one tags/list path", docker.Untranslatable)
		}
		if docker.Failed != 0 {
			t.Fatalf("failed = %d: %v", docker.Failed, docker.Errors)
		}
	})

	registry := &registryClient{instance: instance, repository: "docker-hosted"}

	t.Run("the tag is pullable, which only the component walk makes possible", func(t *testing.T) {
		response := registry.do(http.MethodGet, "/v2/docker-hosted/team/api/manifests/1.0", nil, "")
		body := expectStatus(t, response, http.StatusOK)

		if body != images.manifest {
			t.Fatalf("the migrated manifest differs from the source")
		}
		if got := response.Header.Get("Docker-Content-Digest"); got != digestOf([]byte(images.manifest)) {
			t.Fatalf("digest = %q, want %q", got, digestOf([]byte(images.manifest)))
		}
	})

	t.Run("the manifest is also pullable by digest", func(t *testing.T) {
		expectStatus(t, registry.do(http.MethodGet,
			"/v2/docker-hosted/team/api/manifests/"+digestOf([]byte(images.manifest)), nil, ""), http.StatusOK)
	})

	t.Run("the shared blob path was translated to this layout", func(t *testing.T) {
		for _, content := range []string{images.config, images.layer} {
			body := expectStatus(t, registry.do(http.MethodGet,
				"/v2/docker-hosted/team/api/blobs/"+digestOf([]byte(content)), nil, ""), http.StatusOK)
			if body != content {
				t.Fatalf("a migrated blob differs from the source")
			}
		}
	})

	t.Run("the tag became a version the UI can list", func(t *testing.T) {
		body := expectStatus(t, registry.do(http.MethodGet, "/v2/docker-hosted/team/api/tags/list", nil, ""), http.StatusOK)
		if !strings.Contains(body, `"1.0"`) {
			t.Fatalf("tags = %s", body)
		}
	})

	t.Run("settling indexed the manifest, platform and all", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/docker-hosted/docker/manifests?namespace=team&name=api&reference=1.0", ""), http.StatusOK)

		var payload struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
			LayerCount   int    `json:"layerCount"`
			Layers       []struct {
				Stored bool `json:"stored"`
			} `json:"layers"`
			Config *struct {
				Entrypoint []string `json:"entrypoint"`
			} `json:"config"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("decoding the manifest: %v", err)
		}

		// The platform comes from the config blob, which is what makes this prove the
		// copy finished before the metadata was built.
		if payload.OS != "linux" || payload.Architecture != "amd64" {
			t.Fatalf("platform = %s/%s, want linux/amd64", payload.OS, payload.Architecture)
		}
		if payload.LayerCount != 1 || len(payload.Layers) != 1 || !payload.Layers[0].Stored {
			t.Fatalf("layers = %+v", payload.Layers)
		}
		if payload.Config == nil || len(payload.Config.Entrypoint) != 1 {
			t.Fatalf("config = %+v", payload.Config)
		}
	})

	t.Run("the migration only ever read from Nexus", func(t *testing.T) {
		if writes := remote.writeRequests(); writes != 0 {
			t.Fatalf("the migration made %d write requests to Nexus", writes)
		}
	})
}

// Rerunning must not re-transfer anything, which for docker means the translated path
// is what gets checked rather than the source path.
func TestMigrationOfDockerIsResumable(t *testing.T) {
	_, _, runner, plan := migrationFixture(t)

	first := runner.Apply(context.Background(), plan)
	second := runner.Apply(context.Background(), plan)

	find := func(results []migrate.Result) migrate.Result {
		for _, result := range results {
			if result.Decision.Name() == "docker-hosted" {
				return result
			}
		}
		return migrate.Result{}
	}

	if find(first).Copied == 0 {
		t.Fatalf("the first run copied nothing: %+v", find(first))
	}
	if copied := find(second).Copied; copied != 0 {
		t.Fatalf("the second run copied %d assets again", copied)
	}
	if find(second).Skipped != find(first).Copied {
		t.Fatalf("the second run skipped %d, want the %d the first copied",
			find(second).Skipped, find(first).Copied)
	}
	if find(second).Untranslatable != find(first).Untranslatable {
		t.Fatalf("the untranslatable count changed between runs: %d then %d",
			find(first).Untranslatable, find(second).Untranslatable)
	}
}
