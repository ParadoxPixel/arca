package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func decode[T any](t *testing.T, body string) T {
	t.Helper()

	var payload T
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding %T: %v (body: %s)", payload, err, body)
	}
	return payload
}

func (i *testInstance) signInAs(email, password string) *testInstance {
	i.t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		i.t.Fatalf("cookiejar.New: %v", err)
	}

	member := &testInstance{
		t:      i.t,
		app:    i.app,
		server: i.server,
		client: &http.Client{
			Jar:           jar,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	expectStatus(i.t, member.api(http.MethodPost, "/api/auth/login", `{"email":"`+email+`","password":"`+password+`"}`), http.StatusOK)
	return member
}

func (i *testInstance) publish(path, content string) {
	i.t.Helper()
	expectStatus(i.t, i.maven(http.MethodPut, path, content, true), http.StatusCreated)
}

type trafficResponse struct {
	Range   string `json:"range"`
	Buckets []struct {
		At        int64 `json:"at"`
		Downloads int64 `json:"downloads"`
		Uploads   int64 `json:"uploads"`
		Visits    int64 `json:"visits"`
	} `json:"buckets"`
	Totals struct {
		Downloads int64 `json:"downloads"`
		Uploads   int64 `json:"uploads"`
		Visits    int64 `json:"visits"`
		Bytes     int64 `json:"bytes"`
	} `json:"totals"`
	TopArtifacts []struct {
		Repository string `json:"repository"`
		Namespace  string `json:"namespace"`
		Name       string `json:"name"`
		Downloads  int64  `json:"downloads"`
	} `json:"topArtifacts"`
	Repositories []struct {
		Repository string `json:"repository"`
		Downloads  int64  `json:"downloads"`
		Uploads    int64  `json:"uploads"`
	} `json:"repositories"`
}

func TestTrafficReportsDownloadsAndUploads(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	const artifactPath = "/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar"
	instance.publish(artifactPath, "artifact-bytes")

	for range 3 {
		expectStatus(t, instance.maven(http.MethodGet, artifactPath, "", true), http.StatusOK)
	}
	expectStatus(t, instance.maven(http.MethodHead, artifactPath, "", true), http.StatusOK)

	instance.app.traffic.sync()

	traffic := decode[trafficResponse](t, expectStatus(t, instance.api(http.MethodGet, "/api/admin/traffic?range=24h", ""), http.StatusOK))

	t.Run("totals", func(t *testing.T) {
		if traffic.Totals.Downloads != 3 {
			t.Fatalf("downloads = %d, want 3 (head requests must not count)", traffic.Totals.Downloads)
		}
		if traffic.Totals.Uploads != 1 {
			t.Fatalf("uploads = %d, want 1", traffic.Totals.Uploads)
		}
		if traffic.Totals.Bytes == 0 {
			t.Fatal("expected transferred bytes to be recorded")
		}
	})

	t.Run("buckets span the window", func(t *testing.T) {
		if len(traffic.Buckets) != 24 {
			t.Fatalf("buckets = %d, want 24 hourly buckets", len(traffic.Buckets))
		}
		var downloads int64
		for _, bucket := range traffic.Buckets {
			downloads += bucket.Downloads
		}
		if downloads != traffic.Totals.Downloads {
			t.Fatalf("bucketed downloads = %d, want %d", downloads, traffic.Totals.Downloads)
		}
	})

	t.Run("top artifacts", func(t *testing.T) {
		if len(traffic.TopArtifacts) != 1 {
			t.Fatalf("top artifacts = %d, want 1: %+v", len(traffic.TopArtifacts), traffic.TopArtifacts)
		}
		top := traffic.TopArtifacts[0]
		if top.Namespace != "com.example" || top.Name != "app" || top.Downloads != 3 {
			t.Fatalf("top artifact = %+v, want com.example:app with 3 downloads", top)
		}
	})

	t.Run("per repository", func(t *testing.T) {
		if len(traffic.Repositories) != 1 {
			t.Fatalf("repositories = %+v, want one entry", traffic.Repositories)
		}
		if entry := traffic.Repositories[0]; entry.Repository != "maven-releases" || entry.Uploads != 1 {
			t.Fatalf("repository traffic = %+v, want maven-releases with 1 upload", entry)
		}
	})
}

func TestVisitsCountDocumentLoadsOnly(t *testing.T) {
	cases := []struct {
		name   string
		accept string
		want   int64
	}{
		{"browser document", "text/html,application/xhtml+xml", 1},
		{"bundled asset", "text/css,*/*;q=0.1", 0},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			instance := newTestInstance(t)
			instance.setup()

			request := instance.request(http.MethodGet, "/", "")
			request.Header.Set("Accept", testCase.accept)
			instance.do(request).Body.Close()

			instance.app.traffic.sync()

			traffic := decode[trafficResponse](t, expectStatus(t, instance.api(http.MethodGet, "/api/admin/traffic?range=24h", ""), http.StatusOK))
			if traffic.Totals.Visits != testCase.want {
				t.Fatalf("visits = %d, want %d", traffic.Totals.Visits, testCase.want)
			}
		})
	}
}

func TestTrafficWindowsSelectTheirInterval(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	cases := []struct {
		query   string
		want    string
		buckets int
	}{
		{"?range=24h", "24h", 24},
		{"?range=7d", "7d", 28},
		{"?range=30d", "30d", 30},
		{"?range=90d", "90d", 90},
		{"", "7d", 28},
		{"?range=nonsense", "7d", 28},
	}

	for _, testCase := range cases {
		t.Run(testCase.want+testCase.query, func(t *testing.T) {
			traffic := decode[trafficResponse](t, expectStatus(t, instance.api(http.MethodGet, "/api/admin/traffic"+testCase.query, ""), http.StatusOK))
			if traffic.Range != testCase.want {
				t.Fatalf("range = %q, want %q", traffic.Range, testCase.want)
			}
			if len(traffic.Buckets) != testCase.buckets {
				t.Fatalf("buckets = %d, want %d", len(traffic.Buckets), testCase.buckets)
			}
		})
	}
}

type storageResponse struct {
	Totals struct {
		Repositories  int64 `json:"repositories"`
		Components    int64 `json:"components"`
		Assets        int64 `json:"assets"`
		Bytes         int64 `json:"bytes"`
		SnapshotBytes int64 `json:"snapshotBytes"`
		ReleaseBytes  int64 `json:"releaseBytes"`
	} `json:"totals"`
	Repositories []struct {
		Repository    string `json:"repository"`
		Components    int64  `json:"components"`
		Assets        int64  `json:"assets"`
		Bytes         int64  `json:"bytes"`
		SnapshotBytes int64  `json:"snapshotBytes"`
	} `json:"repositories"`
	Extensions []struct {
		Extension string `json:"extension"`
		Files     int64  `json:"files"`
		Bytes     int64  `json:"bytes"`
	} `json:"extensions"`
	Largest []struct {
		Repository string `json:"repository"`
		Name       string `json:"name"`
		Version    string `json:"version"`
		Files      int64  `json:"files"`
		Bytes      int64  `json:"bytes"`
	} `json:"largest"`
	Disk struct {
		Files         int64 `json:"files"`
		Bytes         int64 `json:"bytes"`
		TrackedBytes  int64 `json:"trackedBytes"`
		OrphanBytes   int64 `json:"orphanBytes"`
		DatabaseBytes int64 `json:"databaseBytes"`
	} `json:"disk"`
}

func TestStorageBreakdown(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	instance.publish("/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar", "jar-payload")
	instance.publish("/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.pom", "<project/>")
	instance.publish("/repository/maven-snapshots/com/example/lib/2.0.0-SNAPSHOT/lib-2.0.0-20240115.101500-1.jar", "snapshot-payload")

	storage := decode[storageResponse](t, expectStatus(t, instance.api(http.MethodGet, "/api/admin/storage", ""), http.StatusOK))

	t.Run("totals", func(t *testing.T) {
		if storage.Totals.Assets != 3 {
			t.Fatalf("assets = %d, want 3", storage.Totals.Assets)
		}
		if storage.Totals.Components != 2 {
			t.Fatalf("components = %d, want 2", storage.Totals.Components)
		}
		if storage.Totals.SnapshotBytes+storage.Totals.ReleaseBytes != storage.Totals.Bytes {
			t.Fatalf("snapshot %d + release %d does not add up to %d",
				storage.Totals.SnapshotBytes, storage.Totals.ReleaseBytes, storage.Totals.Bytes)
		}
	})

	t.Run("by extension", func(t *testing.T) {
		byExtension := map[string]int64{}
		for _, entry := range storage.Extensions {
			byExtension[entry.Extension] = entry.Files
		}
		for extension, want := range map[string]int64{"jar": 2, "pom": 1} {
			if byExtension[extension] != want {
				t.Fatalf("%s files = %d, want %d (all: %+v)", extension, byExtension[extension], want, storage.Extensions)
			}
		}
	})

	t.Run("by repository", func(t *testing.T) {
		byRepository := map[string]int64{}
		for _, entry := range storage.Repositories {
			byRepository[entry.Repository] = entry.Assets
		}
		for repository, want := range map[string]int64{"maven-releases": 2, "maven-snapshots": 1} {
			if byRepository[repository] != want {
				t.Fatalf("%s assets = %d, want %d", repository, byRepository[repository], want)
			}
		}
	})

	t.Run("largest components", func(t *testing.T) {
		if len(storage.Largest) != 2 {
			t.Fatalf("largest = %d entries, want 2", len(storage.Largest))
		}
		if storage.Largest[0].Bytes < storage.Largest[1].Bytes {
			t.Fatal("largest components are not ordered by size")
		}
	})

	t.Run("disk usage matches the database", func(t *testing.T) {
		if storage.Disk.Files != 3 {
			t.Fatalf("files on disk = %d, want 3", storage.Disk.Files)
		}
		if storage.Disk.Bytes != storage.Totals.Bytes {
			t.Fatalf("disk bytes = %d, want the tracked %d", storage.Disk.Bytes, storage.Totals.Bytes)
		}
		if storage.Disk.OrphanBytes != 0 {
			t.Fatalf("orphan bytes = %d, want 0", storage.Disk.OrphanBytes)
		}
		if storage.Disk.DatabaseBytes == 0 {
			t.Fatal("expected the database file to report a size")
		}
	})
}

func TestDeleteArtifact(t *testing.T) {
	publishAll := func(instance *testInstance) {
		instance.publish("/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar", "one")
		instance.publish("/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.pom", "one-pom")
		instance.publish("/repository/maven-releases/com/example/app/2.0.0/app-2.0.0.jar", "two")
	}

	t.Run("a single version", func(t *testing.T) {
		instance := newTestInstance(t)
		instance.setup()
		publishAll(instance)

		body := expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/maven-releases/artifacts?namespace=com.example&name=app&version=1.0.0", ""), http.StatusOK)
		removed := decode[struct {
			Versions int `json:"versions"`
			Files    int `json:"files"`
		}](t, body)

		if removed.Versions != 1 || removed.Files != 2 {
			t.Fatalf("removed = %+v, want 1 version and 2 files", removed)
		}

		expectStatus(t, instance.maven(http.MethodGet, "/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar", "", true), http.StatusNotFound)
		expectStatus(t, instance.maven(http.MethodGet, "/repository/maven-releases/com/example/app/2.0.0/app-2.0.0.jar", "", true), http.StatusOK)

		artifact := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/maven-releases/artifacts?namespace=com.example&name=app", ""), http.StatusOK)
		if want := `"latest":"2.0.0"`; !strings.Contains(artifact, want) {
			t.Fatalf("artifact detail = %s, want %s", artifact, want)
		}
	})

	t.Run("every version", func(t *testing.T) {
		instance := newTestInstance(t)
		instance.setup()
		publishAll(instance)

		body := expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/maven-releases/artifacts?namespace=com.example&name=app", ""), http.StatusOK)
		removed := decode[struct {
			Versions int `json:"versions"`
			Files    int `json:"files"`
		}](t, body)

		if removed.Versions != 2 || removed.Files != 3 {
			t.Fatalf("removed = %+v, want 2 versions and 3 files", removed)
		}

		expectStatus(t, instance.api(http.MethodGet, "/api/repositories/maven-releases/artifacts?namespace=com.example&name=app", ""), http.StatusNotFound)

		storage := decode[storageResponse](t, expectStatus(t, instance.api(http.MethodGet, "/api/admin/storage", ""), http.StatusOK))
		if storage.Disk.Files != 0 {
			t.Fatalf("files on disk = %d, want the stored objects to be gone", storage.Disk.Files)
		}
	})

	t.Run("a neighbouring group is left alone", func(t *testing.T) {
		instance := newTestInstance(t)
		instance.setup()
		instance.publish("/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar", "artifact")
		instance.publish("/repository/maven-releases/com/example/app/nested/1.0.0/nested-1.0.0.jar", "neighbour")

		expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/maven-releases/artifacts?namespace=com.example&name=app", ""), http.StatusOK)
		expectStatus(t, instance.maven(http.MethodGet, "/repository/maven-releases/com/example/app/nested/1.0.0/nested-1.0.0.jar", "", true), http.StatusOK)
	})

	t.Run("rejected requests", func(t *testing.T) {
		instance := newTestInstance(t)
		instance.setup()
		publishAll(instance)

		expectStatus(t, instance.api(http.MethodPost, "/api/users", `{"email":"member@example.com","name":"Member","password":"membersecret1","role":"user"}`), http.StatusCreated)
		expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/maven-releases", `{"visibility":"public"}`), http.StatusOK)
		member := instance.signInAs("member@example.com", "membersecret1")

		cases := []struct {
			name   string
			client *testInstance
			query  string
			want   int
		}{
			{"unknown artifact", instance, "?namespace=com.example&name=missing", http.StatusNotFound},
			{"unknown version", instance, "?namespace=com.example&name=app&version=9.9.9", http.StatusNotFound},
			{"missing name", instance, "?namespace=com.example", http.StatusBadRequest},
			{"reader without repository admin", member, "?namespace=com.example&name=app", http.StatusForbidden},
		}

		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				expectStatus(t, testCase.client.api(http.MethodDelete, "/api/repositories/maven-releases/artifacts"+testCase.query, ""), testCase.want)
			})
		}
	})
}

func TestAdminRoutesRequireAdministrator(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/users", `{"email":"member@example.com","name":"Member","password":"membersecret1","role":"user"}`), http.StatusCreated)
	member := instance.signInAs("member@example.com", "membersecret1")

	paths := []string{"/api/admin/health", "/api/admin/traffic", "/api/admin/storage", "/api/admin/components"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			expectStatus(t, member.api(http.MethodGet, path, ""), http.StatusForbidden)
			expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, path, "")), http.StatusUnauthorized)
			expectStatus(t, instance.api(http.MethodGet, path, ""), http.StatusOK)
		})
	}
}
