package maven

import (
	"slices"
	"testing"
)

func TestCompareVersionsOrdering(t *testing.T) {
	cases := [][2]string{
		{"1.0.0", "1.0.1"},
		{"1.0", "1.1"},
		{"1.9", "1.10"},
		{"1.0-SNAPSHOT", "1.0"},
		{"1.0-alpha1", "1.0-beta1"},
		{"1.0-beta1", "1.0-rc1"},
		{"1.0-rc1", "1.0"},
		{"1.0", "1.0.1"},
		{"2.0", "10.0"},
		{"1.0-alpha1", "1.0-alpha2"},
	}

	for _, testCase := range cases {
		lower, higher := testCase[0], testCase[1]
		t.Run(lower+" < "+higher, func(t *testing.T) {
			if CompareVersions(lower, higher) >= 0 {
				t.Fatalf("CompareVersions(%q, %q) = %d, want < 0", lower, higher, CompareVersions(lower, higher))
			}
			if CompareVersions(higher, lower) <= 0 {
				t.Fatalf("CompareVersions(%q, %q) = %d, want > 0", higher, lower, CompareVersions(higher, lower))
			}
		})
	}
}

func TestCompareVersionsEquality(t *testing.T) {
	cases := [][2]string{
		{"1.0", "1.0.0"},
		{"1.0", "1.0-ga"},
		{"1.0-final", "1.0"},
	}

	for _, testCase := range cases {
		left, right := testCase[0], testCase[1]
		t.Run(left+" == "+right, func(t *testing.T) {
			if got := CompareVersions(left, right); got != 0 {
				t.Fatalf("CompareVersions(%q, %q) = %d, want 0", left, right, got)
			}
		})
	}
}

func TestSortVersions(t *testing.T) {
	got := SortVersions([]string{"1.10.0", "1.2.0", "2.0.0-SNAPSHOT", "1.0.0", "2.0.0", "1.0.0-rc1"})
	want := []string{"1.0.0-rc1", "1.0.0", "1.2.0", "1.10.0", "2.0.0-SNAPSHOT", "2.0.0"}

	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLatestAndRelease(t *testing.T) {
	cases := []struct {
		name     string
		versions []string
		latest   string
		release  string
	}{
		{"mixed", []string{"1.0.0", "2.0.0-SNAPSHOT", "1.2.0"}, "2.0.0-SNAPSHOT", "1.2.0"},
		{"snapshots only", []string{"1.0.0-SNAPSHOT", "2.0.0-SNAPSHOT"}, "2.0.0-SNAPSHOT", ""},
		{"releases only", []string{"1.0.0", "1.1.0"}, "1.1.0", "1.1.0"},
		{"empty", nil, "", ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			latest, release := LatestAndRelease(testCase.versions)
			if latest != testCase.latest || release != testCase.release {
				t.Fatalf("got (%q, %q), want (%q, %q)", latest, release, testCase.latest, testCase.release)
			}
		})
	}
}
