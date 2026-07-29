package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func newGroupInstance(t *testing.T) *testInstance {
	t.Helper()

	instance := newTestInstance(t)
	instance.setup()

	for _, name := range []string{"p2-releases", "p2-snapshots"} {
		expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
			fmt.Sprintf(`{"name":%q,"format":"p2","type":"hosted","visibility":"public"}`, name)),
			http.StatusCreated)
	}

	return instance
}

func createGroup(t *testing.T, instance *testInstance, name string, members ...string) *http.Response {
	t.Helper()

	quoted := make([]string, 0, len(members))
	for _, member := range members {
		quoted = append(quoted, fmt.Sprintf("%q", member))
	}

	return instance.api(http.MethodPost, "/api/repositories", fmt.Sprintf(
		`{"name":%q,"format":"p2","type":"group","visibility":"public","members":[%s]}`,
		name, strings.Join(quoted, ","),
	))
}

func TestP2GroupServesGeneratedComposites(t *testing.T) {
	instance := newGroupInstance(t)
	expectStatus(t, createGroup(t, instance, "everything", "p2-releases/commons/25.2.5", "p2-snapshots"), http.StatusCreated)

	cases := []struct {
		name     string
		path     string
		contains []string
	}{
		{
			name: "index names the composite factories",
			path: "p2.index",
			contains: []string{
				"metadata.repository.factory.order=compositeContent.xml",
				"artifact.repository.factory.order=compositeArtifacts.xml",
			},
		},
		{
			name: "content composite lists the members in order",
			path: "compositeContent.xml",
			contains: []string{
				"CompositeMetadataRepository",
				"<child location='../p2-releases/commons/25.2.5/'/>",
				"<child location='../p2-snapshots/'/>",
			},
		},
		{
			name: "artifact composite lists the same members",
			path: "compositeArtifacts.xml",
			contains: []string{
				"CompositeArtifactRepository",
				"<child location='../p2-releases/commons/25.2.5/'/>",
				"<child location='../p2-snapshots/'/>",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := expectStatus(t,
				instance.maven(http.MethodGet, "/repository/everything/"+test.path, "", true),
				http.StatusOK)

			for _, expected := range test.contains {
				if !strings.Contains(body, expected) {
					t.Errorf("missing %q in:\n%s", expected, body)
				}
			}
		})
	}
}

// A group holds nothing of its own. Anything but its composite documents is a
// miss, so a client never mistakes it for a site with content.
func TestP2GroupHasNoContentOfItsOwn(t *testing.T) {
	instance := newGroupInstance(t)
	expectStatus(t, createGroup(t, instance, "everything", "p2-releases"), http.StatusCreated)

	for _, path := range []string{
		"content.jar",
		"artifacts.jar",
		"plugins/org.eclipse.jgit_7.1.0.jar",
		"commons/25.2.5/artifacts.jar",
	} {
		t.Run(path, func(t *testing.T) {
			response := instance.maven(http.MethodGet, "/repository/everything/"+path, "", true)
			defer response.Body.Close()

			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("%s = %d, want 404", path, response.StatusCode)
			}
		})
	}
}

// Atomic loading fails the whole composite on one unreadable child, so a child
// the caller cannot read is left out rather than breaking the group for them.
func TestP2GroupHidesMembersTheCallerCannotRead(t *testing.T) {
	instance := newGroupInstance(t)

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
		`{"name":"p2-private","format":"p2","type":"hosted","visibility":"private"}`), http.StatusCreated)
	expectStatus(t, createGroup(t, instance, "everything", "p2-releases", "p2-private"), http.StatusCreated)

	expectStatus(t, instance.api(http.MethodPost, "/api/users",
		`{"email":"member@example.com","name":"Member","password":"membersecret1","role":"user"}`), http.StatusCreated)
	member := instance.signInAs("member@example.com", "membersecret1")

	t.Run("an administrator sees both", func(t *testing.T) {
		body := expectStatus(t, instance.maven(http.MethodGet, "/repository/everything/compositeContent.xml", "", true), http.StatusOK)
		if !strings.Contains(body, "p2-private") || !strings.Contains(body, "p2-releases") {
			t.Fatalf("expected both children:\n%s", body)
		}
		if !strings.Contains(body, "<children size='2'>") {
			t.Fatalf("expected a size of 2:\n%s", body)
		}
	})

	t.Run("a reader without the grant sees only the public one", func(t *testing.T) {
		body := expectStatus(t,
			member.do(member.request(http.MethodGet, "/repository/everything/compositeContent.xml", "")),
			http.StatusOK)
		if strings.Contains(body, "p2-private") {
			t.Fatalf("a private member leaked into the composite:\n%s", body)
		}
		if !strings.Contains(body, "p2-releases") {
			t.Fatalf("the public member is missing:\n%s", body)
		}
		if !strings.Contains(body, "<children size='1'>") {
			t.Fatalf("the size was not corrected:\n%s", body)
		}
	})
}

func TestP2GroupValidation(t *testing.T) {
	instance := newGroupInstance(t)

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
		`{"name":"maven-thing","format":"maven2","type":"hosted"}`), http.StatusCreated)
	expectStatus(t, createGroup(t, instance, "inner", "p2-releases"), http.StatusCreated)

	cases := []struct {
		name string
		body string
		want int
	}{
		{
			name: "a group needs members",
			body: `{"name":"empty","format":"p2","type":"group","members":[]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "an unknown member is rejected",
			body: `{"name":"g1","format":"p2","type":"group","members":["nope"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "a member of another format is rejected",
			body: `{"name":"g2","format":"p2","type":"group","members":["maven-thing"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "a nested group is rejected",
			body: `{"name":"g3","format":"p2","type":"group","members":["inner"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "a duplicate member is rejected",
			body: `{"name":"g4","format":"p2","type":"group","members":["p2-releases","p2-releases"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "a group containing itself is rejected",
			body: `{"name":"g5","format":"p2","type":"group","members":["g5"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "an escaping member path is rejected",
			body: `{"name":"g6","format":"p2","type":"group","members":["p2-releases/../../etc"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "a maven group is rejected",
			body: `{"name":"g7","format":"maven2","type":"group","members":["p2-releases"]}`,
			want: http.StatusBadRequest,
		},
		{
			name: "members on a hosted repository are rejected",
			body: `{"name":"g8","format":"p2","type":"hosted","members":["p2-releases"]}`,
			want: http.StatusBadRequest,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodPost, "/api/repositories", test.body), test.want)
		})
	}
}

func TestP2GroupMembersAreEditable(t *testing.T) {
	instance := newGroupInstance(t)
	expectStatus(t, createGroup(t, instance, "everything", "p2-releases"), http.StatusCreated)

	body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/everything", ""), http.StatusOK)
	if !strings.Contains(body, `"members":["p2-releases"]`) {
		t.Fatalf("members are not reported: %s", body)
	}

	expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/everything",
		`{"members":["p2-snapshots","p2-releases"]}`), http.StatusOK)

	// Order is configuration, so a replacement keeps what was asked for.
	composite := expectStatus(t,
		instance.maven(http.MethodGet, "/repository/everything/compositeContent.xml", "", true), http.StatusOK)

	snapshots := strings.Index(composite, "../p2-snapshots/")
	releases := strings.Index(composite, "../p2-releases/")
	if snapshots < 0 || releases < 0 || snapshots > releases {
		t.Fatalf("the member order was not preserved:\n%s", composite)
	}
}

func TestP2GroupRejectsWrites(t *testing.T) {
	instance := newGroupInstance(t)
	expectStatus(t, createGroup(t, instance, "everything", "p2-releases"), http.StatusCreated)

	for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			response := instance.maven(method, "/repository/everything/compositeContent.xml", "body", true)
			defer response.Body.Close()

			if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated {
				t.Fatalf("%s was accepted with %d", method, response.StatusCode)
			}
		})
	}
}
