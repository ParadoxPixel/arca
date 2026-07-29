package migrate

import (
	"strings"
	"testing"

	"arca/internal/nexus"
)

func repository(name, nexusFormat, kind string) nexus.Repository {
	return nexus.Repository{Name: name, Format: nexusFormat, Type: kind}
}

func TestPlanDecides(t *testing.T) {
	proxy := repository("maven-central", "maven2", "proxy")
	proxy.Attributes.Proxy.RemoteURL = "https://repo1.maven.org/maven2/"
	proxy.Attributes.Maven.VersionPolicy = "RELEASE"

	snapshots := repository("maven-snapshots", "maven2", "hosted")
	snapshots.Attributes.Maven.VersionPolicy = "SNAPSHOT"

	group := repository("maven-public", "maven2", "group")
	group.Attributes.Group.MemberNames = []string{"maven-central", "maven-snapshots"}

	orphan := repository("broken-proxy", "npm", "proxy")

	cases := []struct {
		name       string
		source     nexus.Repository
		action     string
		wantFormat string
		wantType   string
		wantPolicy string
		copies     bool
		reason     string
	}{
		{
			name: "a maven proxy is recreated without its cache", source: proxy,
			action: ActionCreate, wantFormat: "maven2", wantType: "proxy", wantPolicy: "release",
		},
		{
			name: "a snapshot repository keeps its policy", source: snapshots,
			action: ActionCreate, wantFormat: "maven2", wantType: "hosted", wantPolicy: "snapshot", copies: true,
		},
		{
			name: "a raw repository becomes p2", source: repository("p2-releases", "raw", "hosted"),
			action: ActionCreate, wantFormat: "p2", wantType: "hosted", copies: true,
		},
		{
			name: "an npm repository carries across", source: repository("npm-hosted", "npm", "hosted"),
			action: ActionCreate, wantFormat: "npm", wantType: "hosted", copies: true,
		},
		{
			name: "a group is skipped with its members named", source: group,
			action: ActionSkip, reason: "maven-central, maven-snapshots",
		},
		{
			name: "an unsupported format is skipped", source: repository("docker-hosted", "docker", "hosted"),
			action: ActionSkip, reason: "no docker format",
		},
		{
			name: "a proxy without an upstream is skipped", source: orphan,
			action: ActionSkip, reason: "no remote URL",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := Plan([]nexus.Repository{test.source}, Options{})
			if len(plan) != 1 {
				t.Fatalf("expected one decision, got %d", len(plan))
			}
			decision := plan[0]

			if decision.Action != test.action {
				t.Fatalf("action = %q, want %q (%s)", decision.Action, test.action, decision.Reason)
			}
			if test.action == ActionSkip {
				if !strings.Contains(decision.Reason, test.reason) {
					t.Fatalf("reason = %q, want it to mention %q", decision.Reason, test.reason)
				}
				return
			}

			if decision.Format != test.wantFormat {
				t.Errorf("format = %q, want %q", decision.Format, test.wantFormat)
			}
			if decision.Type != test.wantType {
				t.Errorf("type = %q, want %q", decision.Type, test.wantType)
			}
			if test.wantPolicy != "" && decision.Policy != test.wantPolicy {
				t.Errorf("policy = %q, want %q", decision.Policy, test.wantPolicy)
			}
			if decision.CopyAssets != test.copies {
				t.Errorf("copyAssets = %v, want %v", decision.CopyAssets, test.copies)
			}
		})
	}
}

func TestPlanCarriesTheProxyUpstream(t *testing.T) {
	proxy := repository("central", "maven2", "proxy")
	proxy.Attributes.Proxy.RemoteURL = "https://repo1.maven.org/maven2/"

	decision := Plan([]nexus.Repository{proxy}, Options{})[0]

	if decision.RemoteURL != "https://repo1.maven.org/maven2/" {
		t.Fatalf("remoteURL = %q", decision.RemoteURL)
	}
	// A proxy cache is disposable, so copying it would be wasted transfer.
	if decision.CopyAssets {
		t.Fatal("a proxy cache should not be copied")
	}
}

func TestPlanFiltersAndSorts(t *testing.T) {
	repositories := []nexus.Repository{
		repository("zeta", "npm", "hosted"),
		repository("alpha", "npm", "hosted"),
		repository("beta", "npm", "hosted"),
	}

	plan := Plan(repositories, Options{Only: []string{"beta", "alpha"}})

	if len(plan) != 2 {
		t.Fatalf("expected two decisions, got %d", len(plan))
	}
	if plan[0].Name() != "alpha" || plan[1].Name() != "beta" {
		t.Fatalf("plan order = %s, %s", plan[0].Name(), plan[1].Name())
	}
}

func TestPlanRawFormatIsOverridable(t *testing.T) {
	plan := Plan([]nexus.Repository{repository("raw-files", "raw", "hosted")}, Options{RawFormat: "maven2"})

	if plan[0].Format != "maven2" {
		t.Fatalf("format = %q, want maven2", plan[0].Format)
	}
}
