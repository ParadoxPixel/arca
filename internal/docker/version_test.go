package docker

import (
	"testing"

	"arca/internal/format"
)

func TestCompareOrdering(t *testing.T) {
	cases := []struct {
		lower  string
		higher string
	}{
		{"1.0.0", "1.0.1"},
		{"1.0.0", "1.1.0"},
		{"1.0.0", "2.0.0"},
		{"1.2", "1.10"},
		{"1", "1.1"},
		{"1.9", "1.10"},

		// The real tags from the Nexus instance this format was designed against.
		{"0.1.8-unittest-staging", "0.1.9-swaggerui-staging"},

		// A bare version outranks the same version with anything appended,
		// whether that is a prerelease or a variant.
		{"1.0.0-rc1", "1.0.0"},
		{"1.25-alpine", "1.25"},

		// A tag that does not open with a number is not a version, so it sorts
		// under every tag that is. A bytewise sort would put "latest" on top.
		{"latest", "0.0.1"},
		{"alpine", "1.0"},
		{"stable", "2"},

		// Two names order against each other bytewise, having nothing better.
		{"alpine", "latest"},
	}

	for _, testCase := range cases {
		t.Run(testCase.lower+" < "+testCase.higher, func(t *testing.T) {
			if got := Compare(testCase.lower, testCase.higher); got >= 0 {
				t.Fatalf("Compare(%q, %q) = %d, want < 0", testCase.lower, testCase.higher, got)
			}
			if got := Compare(testCase.higher, testCase.lower); got <= 0 {
				t.Fatalf("Compare(%q, %q) = %d, want > 0", testCase.higher, testCase.lower, got)
			}
		})
	}
}

func TestCompareEquality(t *testing.T) {
	cases := [][2]string{
		{"1.0.0", "1.0.0"},
		{"1.25-ALPINE", "1.25-alpine"},
		{"LATEST", "latest"},
		{" 1.0 ", "1.0"},
	}

	for _, testCase := range cases {
		t.Run(testCase[0]+" == "+testCase[1], func(t *testing.T) {
			if got := Compare(testCase[0], testCase[1]); got != 0 {
				t.Fatalf("Compare(%q, %q) = %d, want 0", testCase[0], testCase[1], got)
			}
		})
	}
}

func TestPrerelease(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"1.0.0-rc1", true},
		{"1.0.0-rc.1", true},
		{"1.0.0-alpha", true},
		{"1.0.0-beta.2", true},
		{"1.0.0-SNAPSHOT", true},
		{"2.1-dev", true},
		{"1.0.0-pre", true},
		{"nightly", true},
		{"edge", true},

		{"1.0.0", false},
		{"latest", false},
		{"stable", false},
		{"1", false},

		// A variant names a base image rather than a prerelease of anything.
		{"1.25-alpine", false},
		{"3.20-slim", false},
		{"1.0-bookworm", false},

		// The tags from the live Nexus instance. No marker list classifies these,
		// which is why a docker repository defaults to the mixed policy.
		{"0.1.8-unittest-staging", false},
		{"0.1.9-swaggerui-staging", false},

		// A substring test would read "rc" out of the first and "dev" out of the
		// second. Markers are matched as whole words for exactly this reason.
		{"1.0-source", false},
		{"1.0-bundev", false},
		{"1.0-searchable", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.tag, func(t *testing.T) {
			if got := Prerelease(testCase.tag); got != testCase.want {
				t.Fatalf("Prerelease(%q) = %v, want %v", testCase.tag, got, testCase.want)
			}
		})
	}
}

func TestCore(t *testing.T) {
	cases := []struct {
		tag  string
		want string
	}{
		{"1.0.0", "1.0.0"},
		{"1.25-alpine", "1.25"},
		{"0.1.8-unittest-staging", "0.1.8"},
		{"1.0.0-rc.1", "1.0.0"},
		{"16", "16"},
		{"latest", "latest"},
		{"alpine", "alpine"},
		{"1.", "1"},
	}

	for _, testCase := range cases {
		t.Run(testCase.tag, func(t *testing.T) {
			if got := Core(testCase.tag); got != testCase.want {
				t.Fatalf("Core(%q) = %q, want %q", testCase.tag, got, testCase.want)
			}
		})
	}
}

// The version list and the badges on the artifact page are driven through the
// format seam, so the comparator has to hold up under the mix of versions,
// variants and moving tags one real image carries all at once.
func TestSortingAMixedTagSet(t *testing.T) {
	tags := []string{"latest", "1.25", "1.25-alpine", "1.26-rc1", "1.24", "alpine"}

	sorted := format.SortVersions(Layout{}, tags)
	want := []string{"alpine", "latest", "1.24", "1.25-alpine", "1.25", "1.26-rc1"}

	if len(sorted) != len(want) {
		t.Fatalf("sorted %d tags, want %d", len(sorted), len(want))
	}
	for index := range want {
		if sorted[index] != want[index] {
			t.Fatalf("sorted = %v, want %v", sorted, want)
		}
	}

	latest, release := format.LatestAndRelease(Layout{}, tags)
	if latest != "1.26-rc1" {
		t.Fatalf("latest = %q, want %q", latest, "1.26-rc1")
	}
	if release != "1.25" {
		t.Fatalf("release = %q, want %q", release, "1.25")
	}
}
