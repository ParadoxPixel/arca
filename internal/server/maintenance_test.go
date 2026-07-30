package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func migrationBody(t *testing.T, remote *fakeNexus) string {
	t.Helper()
	return fmt.Sprintf(`{"url":%q,"username":"reader","password":"secret","visibility":"public"}`, remote.server.URL)
}

func maintenanceFixture(t *testing.T) (*testInstance, *fakeNexus) {
	t.Helper()

	instance := newTestInstance(t)
	instance.setup()

	return instance, newFakeNexus(t, fakeRepositories, map[string]map[string]string{
		"p2-releases": {
			"logging/2.0/artifacts.jar":                "artifacts-bytes",
			"logging/2.0/plugins/slf4j.api_2.0.16.jar": "slf4j-bytes",
		},
		"bi-snapshots": {
			"com/example/app/1.0.0-SNAPSHOT/app-1.0.0-SNAPSHOT.jar": "app-bytes",
		},
		"maven-snapshots": {
			"com/example/seeded/1.0.0-SNAPSHOT/seeded-1.0.0-SNAPSHOT.jar": "seeded-bytes",
		},
	})
}

// awaitMigration polls the status the way the admin page does.
func (i *testInstance) awaitMigration(t *testing.T) migrationStatus {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		body := expectStatus(t, i.api(http.MethodGet, "/api/admin/migration", ""), http.StatusOK)

		var status migrationStatus
		if err := json.Unmarshal([]byte(body), &status); err != nil {
			t.Fatalf("decoding the status: %v", err)
		}
		if status.State != MigrationRunning {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("the migration did not finish in time")
	return migrationStatus{}
}

func TestMigrationPreviewWritesNothing(t *testing.T) {
	instance, remote := maintenanceFixture(t)

	body := expectStatus(t,
		instance.api(http.MethodPost, "/api/admin/migration/preview", migrationBody(t, remote)),
		http.StatusOK)

	for _, expected := range []string{
		`"name":"p2-releases"`,
		`"format":"p2"`,
		`"action":"create"`,
		`"action":"skip"`,
		"arca has no rubygems format",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("the preview is missing %s: %s", expected, body)
		}
	}

	// A preview is a read on both sides.
	response := instance.api(http.MethodGet, "/api/repositories/p2-releases", "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("the preview created a repository (%d)", response.StatusCode)
	}
	if writes := remote.writeRequests(); writes != 0 {
		t.Fatalf("the preview sent %d writes to nexus", writes)
	}
}

func TestMigrationRunsInTheBackground(t *testing.T) {
	instance, remote := maintenanceFixture(t)

	expectStatus(t,
		instance.api(http.MethodPost, "/api/admin/migration", migrationBody(t, remote)),
		http.StatusAccepted)

	status := instance.awaitMigration(t)

	if status.State != MigrationDone {
		t.Fatalf("state = %q, message %q, errors %v", status.State, status.Message, status.Errors)
	}

	byName := map[string]migrationRepository{}
	for _, entry := range status.Repositories {
		byName[entry.Name] = entry
	}

	t.Run("hosted repositories are filled", func(t *testing.T) {
		if copied := byName["p2-releases"].Copied; copied != 2 {
			t.Fatalf("p2-releases copied %d, want 2", copied)
		}
		body := expectStatus(t, instance.maven(http.MethodGet,
			"/repository/p2-releases/logging/2.0/plugins/slf4j.api_2.0.16.jar", "", true), http.StatusOK)
		if body != "slf4j-bytes" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("the proxy is recreated without its cache", func(t *testing.T) {
		if entry := byName["maven-central"]; entry.Copied != 0 || entry.State != repositoryDone {
			t.Fatalf("maven-central = %+v", entry)
		}
	})

	t.Run("skipped repositories carry their reason", func(t *testing.T) {
		if entry := byName["maven-public"]; !strings.Contains(entry.Reason, "group") {
			t.Fatalf("maven-public = %+v", entry)
		}
		if entry := byName["gems"]; entry.State != repositorySkipped {
			t.Fatalf("gems = %+v", entry)
		}
	})

	t.Run("nexus was only ever read", func(t *testing.T) {
		if writes := remote.writeRequests(); writes != 0 {
			t.Fatalf("the migration sent %d writes to nexus", writes)
		}
	})
}

func TestMigrationRefusesToRunTwice(t *testing.T) {
	instance, remote := maintenanceFixture(t)

	expectStatus(t,
		instance.api(http.MethodPost, "/api/admin/migration", migrationBody(t, remote)),
		http.StatusAccepted)

	// Whether the first run is still going is a race, so either answer is
	// correct; what must never happen is a second run starting alongside it.
	second := instance.api(http.MethodPost, "/api/admin/migration", migrationBody(t, remote))
	defer second.Body.Close()
	if second.StatusCode != http.StatusConflict && second.StatusCode != http.StatusAccepted {
		t.Fatalf("second start = %d, want 409 or 202", second.StatusCode)
	}

	instance.awaitMigration(t)
}

func TestMigrationEndpointsNeedAnAdministrator(t *testing.T) {
	instance, remote := maintenanceFixture(t)

	expectStatus(t, instance.api(http.MethodPost, "/api/users",
		`{"email":"member@example.com","name":"Member","password":"membersecret1","role":"user"}`), http.StatusCreated)
	member := instance.signInAs("member@example.com", "membersecret1")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "status", method: http.MethodGet, path: "/api/admin/migration"},
		{name: "preview", method: http.MethodPost, path: "/api/admin/migration/preview", body: migrationBody(t, remote)},
		{name: "start", method: http.MethodPost, path: "/api/admin/migration", body: migrationBody(t, remote)},
		{name: "cancel", method: http.MethodDelete, path: "/api/admin/migration"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			expectStatus(t, member.api(test.method, test.path, test.body), http.StatusForbidden)
			expectStatus(t,
				instance.doAnonymously(instance.request(test.method, test.path, test.body)),
				http.StatusUnauthorized)
		})
	}
}

func TestCancellingWithoutARunIsRejected(t *testing.T) {
	instance, _ := maintenanceFixture(t)

	response := instance.api(http.MethodDelete, "/api/admin/migration", "")
	defer response.Body.Close()

	if response.StatusCode != http.StatusConflict {
		t.Fatalf("cancel = %d, want 409", response.StatusCode)
	}
}

func TestMigrationRejectsABadSource(t *testing.T) {
	instance, _ := maintenanceFixture(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{name: "no url", body: `{}`, want: http.StatusBadRequest},
		{name: "relative url", body: `{"url":"nexus.example.com"}`, want: http.StatusBadRequest},
		{name: "unknown visibility", body: `{"url":"https://n.example.com","visibility":"secret"}`, want: http.StatusBadRequest},
		{name: "unknown raw format", body: `{"url":"https://n.example.com","rawFormat":"docker"}`, want: http.StatusBadRequest},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := instance.api(http.MethodPost, "/api/admin/migration/preview", test.body)
			defer response.Body.Close()

			if response.StatusCode != test.want {
				t.Fatalf("preview = %d, want %d", response.StatusCode, test.want)
			}
		})
	}
}
