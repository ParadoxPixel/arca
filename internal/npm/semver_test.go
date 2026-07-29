package npm

import "testing"

func TestCompareOrdering(t *testing.T) {
	cases := []struct {
		lower  string
		higher string
	}{
		{"1.0.0", "1.0.1"},
		{"1.0.0", "1.1.0"},
		{"1.0.0", "2.0.0"},
		{"1.2.0", "1.10.0"},
		{"1.0.0-alpha", "1.0.0"},
		{"1.0.0-alpha", "1.0.0-alpha.1"},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta"},
		{"1.0.0-alpha.beta", "1.0.0-beta"},
		{"1.0.0-beta", "1.0.0-beta.2"},
		{"1.0.0-beta.2", "1.0.0-beta.11"},
		{"1.0.0-beta.11", "1.0.0-rc.1"},
		{"1.0.0-rc.1", "1.0.0"},

		// Maven ordering gets this pair backwards, which is why npm needs its
		// own comparator rather than reusing the Maven one.
		{"1.0.0-1", "1.0.0"},
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
		{"1.0.0+build.1", "1.0.0+build.2"},
		{"1.0.0-rc.1+a", "1.0.0-rc.1+b"},
		{"v1.0.0", "1.0.0"},
	}

	for _, testCase := range cases {
		t.Run(testCase[0]+" == "+testCase[1], func(t *testing.T) {
			if got := Compare(testCase[0], testCase[1]); got != 0 {
				t.Fatalf("Compare(%q, %q) = %d, want 0", testCase[0], testCase[1], got)
			}
		})
	}
}

func TestIsPrerelease(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"1.0.0", false},
		{"1.0.0+build", false},
		{"1.0.0-rc.1", true},
		{"1.0.0-0", true},
		{"1.0.0-beta+build", true},
	}

	for _, testCase := range cases {
		t.Run(testCase.version, func(t *testing.T) {
			if got := IsPrerelease(testCase.version); got != testCase.want {
				t.Fatalf("IsPrerelease(%q) = %v, want %v", testCase.version, got, testCase.want)
			}
		})
	}
}

func TestCore(t *testing.T) {
	cases := []struct {
		version string
		want    string
	}{
		{"1.2.3", "1.2.3"},
		{"1.2.3-rc.1", "1.2.3"},
		{"1.2.3-rc.1+build", "1.2.3"},
		{"v2.0.0", "2.0.0"},
	}

	for _, testCase := range cases {
		t.Run(testCase.version, func(t *testing.T) {
			if got := Core(testCase.version); got != testCase.want {
				t.Fatalf("Core(%q) = %q, want %q", testCase.version, got, testCase.want)
			}
		})
	}
}

func TestIsValidVersion(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"1.0.0", true},
		{"1.0.0-rc.1", true},
		{"1.0.0+build", true},
		{"1.0", false},
		{"1.0.0.0", false},
		{"latest", false},
		{"", false},
		{" 1.0.0", false},
		{"1.0.x", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.version, func(t *testing.T) {
			if got := IsValidVersion(testCase.version); got != testCase.want {
				t.Fatalf("IsValidVersion(%q) = %v, want %v", testCase.version, got, testCase.want)
			}
		})
	}
}

func TestLatestVersion(t *testing.T) {
	cases := []struct {
		name     string
		versions []string
		want     string
	}{
		{"releases only", []string{"1.0.0", "1.2.0", "1.10.0"}, "1.10.0"},
		{"a prerelease never wins over a release", []string{"1.0.0", "2.0.0-rc.1"}, "1.0.0"},
		{"prereleases only fall back to the highest", []string{"1.0.0-rc.1", "1.0.0-rc.2"}, "1.0.0-rc.2"},
		{"nothing published", nil, ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := LatestVersion(testCase.versions); got != testCase.want {
				t.Fatalf("LatestVersion(%v) = %q, want %q", testCase.versions, got, testCase.want)
			}
		})
	}
}
