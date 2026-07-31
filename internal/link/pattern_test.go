package link

import (
	"strings"
	"testing"
)

var release = Fields{
	Namespace:   "dev.pixelib.pixelscript",
	Name:        "pixelscript-paper",
	Version:     "39",
	BaseVersion: "39",
}

func TestTemplateMatchesTheVersionedFilename(t *testing.T) {
	cases := []struct {
		name     string
		pattern  string
		filename string
		want     bool
	}{
		{"the version spelled out", "{name}-{version}.jar", "pixelscript-paper-39.jar", true},
		{"a different version", "{name}-{version}.jar", "pixelscript-paper-38.jar", false},
		{"a classifier the pattern does not name", "{name}-{version}.jar", "pixelscript-paper-39-sources.jar", false},
		{"a classifier the pattern does name", "{name}-{version}-sources.jar", "pixelscript-paper-39-sources.jar", true},
		{"a literal name", "pixelscript-paper-{version}.jar", "pixelscript-paper-39.jar", true},
		{"a wildcard tail", "{name}-{version}*.jar", "pixelscript-paper-39-shaded.jar", true},
		{"a wildcard that does not reach the extension", "{name}-{version}*.jar", "pixelscript-paper-39.pom", false},
		{"a leading wildcard", "*-{version}.jar", "pixelscript-paper-39.jar", true},
		{"the pom rather than the jar", "{name}-{version}.pom", "pixelscript-paper-39.pom", true},
		{"a partial match is not enough", "{name}-{version}.jar", "prefix-pixelscript-paper-39.jar", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			matcher, err := Compile(MatchTemplate, testCase.pattern, release)
			if err != nil {
				t.Fatalf("Compile(%q): %v", testCase.pattern, err)
			}
			if got := matcher.MatchString(testCase.filename); got != testCase.want {
				t.Fatalf("%q matched %q = %v, want %v", testCase.pattern, testCase.filename, got, testCase.want)
			}
		})
	}
}

// A template is literal text apart from '*', so regex syntax in it has to match
// itself rather than compile.
func TestTemplateTreatsRegexSyntaxAsLiteral(t *testing.T) {
	matcher, err := Compile(MatchTemplate, "release+{version}.zip", release)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !matcher.MatchString("release+39.zip") {
		t.Fatal("expected the '+' to match itself")
	}
	if matcher.MatchString("releasee39.zip") {
		t.Fatal("expected the '+' not to act as a repeat")
	}
}

func TestRegexMatchesTheWholeFilename(t *testing.T) {
	matcher, err := Compile(MatchRegex, `pixelscript-paper-\d+\.jar`, release)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if !matcher.MatchString("pixelscript-paper-39.jar") {
		t.Fatal("expected the versioned jar to match")
	}
	if matcher.MatchString("pixelscript-paper-39.jar.sha1") {
		t.Fatal("expected an unanchored tail not to match")
	}
	if matcher.MatchString("other-pixelscript-paper-39.jar") {
		t.Fatal("expected an unanchored head not to match")
	}
}

// A version is data, not syntax. Substituting one into a regex has to quote it,
// or a version containing '.' or '+' would widen what the link resolves to.
func TestSubstitutedValuesAreQuoted(t *testing.T) {
	fields := Fields{Name: "app", Version: "1.0.0+build.1"}

	matcher, err := Compile(MatchRegex, `{name}-{version}\.jar`, fields)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !matcher.MatchString("app-1.0.0+build.1.jar") {
		t.Fatal("expected the exact version to match")
	}
	if matcher.MatchString("app-1x0y0+buildz1.jar") {
		t.Fatal("expected the dots in the version not to act as wildcards")
	}
}

func TestValidateRejectsWhatCannotWork(t *testing.T) {
	cases := []struct {
		name      string
		matchType string
		pattern   string
		message   string
	}{
		{"an empty pattern", MatchTemplate, "  ", "required"},
		{"a path", MatchTemplate, "target/{name}-{version}.jar", "cannot contain"},
		{"an unknown placeholder", MatchTemplate, "{revision}.jar", "not a known placeholder"},
		{"an unparseable regex", MatchRegex, `app-(\d+\.jar`, "not a valid regular expression"},
		{"an unknown match type", "glob", "*.jar", "matchType must be one of"},
		{"an overlong pattern", MatchTemplate, strings.Repeat("a", MaxPatternLength+1), "at most"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := Validate(testCase.matchType, testCase.pattern)
			if err == nil {
				t.Fatalf("Validate(%q, %q) = nil, want an error", testCase.matchType, testCase.pattern)
			}
			if !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error = %q, want it to mention %q", err, testCase.message)
			}
		})
	}
}

func TestValidateAcceptsAPlaceholderInsideARegexGroup(t *testing.T) {
	if err := Validate(MatchRegex, `{name}-{version}(-shaded)?\.jar`); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestExpandFillsPlaceholdersForDisplay(t *testing.T) {
	got := Expand("{name}-{version}.jar", release)
	if want := "pixelscript-paper-39.jar"; got != want {
		t.Fatalf("Expand = %q, want %q", got, want)
	}
}
