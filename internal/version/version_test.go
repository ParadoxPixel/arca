package version

import "testing"

func TestShort(t *testing.T) {
	testCases := []struct {
		name   string
		commit string
		want   string
	}{
		{"a full sha", "0123456789abcdef0123456789abcdef01234567", "0123456"},
		{"already short", "0123456", "0123456"},
		{"shorter than seven", "0123", "0123"},
		{"absent", "", ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := (Build{Commit: testCase.commit}).Short(); got != testCase.want {
				t.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

// Current has to answer even without -ldflags, because that is how every
// developer build runs.
func TestCurrentAlwaysReportsSomething(t *testing.T) {
	build := Current()

	if build.Version == "" {
		t.Fatal("version is empty")
	}
	if build.BuildTime == "" {
		t.Fatal("build time is empty")
	}
}

func TestCurrentPrefersLinkerValues(t *testing.T) {
	Version, Commit, BuildTime = "v1.2.3", "abc1234", "2026-01-01T00:00:00Z"
	t.Cleanup(func() { Version, Commit, BuildTime = "dev", "", "" })

	build := Current()

	testCases := []struct {
		name string
		got  string
		want string
	}{
		{"version", build.Version, "v1.2.3"},
		{"commit", build.Commit, "abc1234"},
		{"build time", build.BuildTime, "2026-01-01T00:00:00Z"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("got %q, want %q", testCase.got, testCase.want)
			}
		})
	}
}
