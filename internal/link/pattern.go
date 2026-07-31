// Package link matches the filename a deployment link points at. A link names
// an artifact rather than a file, because the filename of a Maven artifact
// carries the version and therefore changes with every release.
package link

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// MatchTemplate is a filename written out with {version} and friends left
	// as placeholders, plus '*' for anything else that varies.
	MatchTemplate = "template"
	// MatchRegex is a regular expression matched against the whole filename,
	// for the cases a template cannot express.
	MatchRegex = "regex"

	MaxPatternLength = 200
	// wildcard stands in for whatever a template leaves open. It stops at a
	// separator so a pattern cannot reach across directories.
	wildcard = `[^/]*`
)

// MatchTypes are the two ways a pattern can be read, in the order the UI offers
// them.
var MatchTypes = []string{MatchTemplate, MatchRegex}

// Fields are the coordinates one candidate version supplies to a pattern.
type Fields struct {
	Namespace   string
	Name        string
	Version     string
	BaseVersion string
}

func (f Fields) value(placeholder string) (string, bool) {
	switch placeholder {
	case "namespace", "groupId", "group":
		return f.Namespace, true
	case "name", "artifactId", "artifact":
		return f.Name, true
	case "version":
		return f.Version, true
	case "baseVersion":
		return f.BaseVersion, true
	default:
		return "", false
	}
}

// Placeholders lists what a pattern may interpolate, in the order the UI offers
// them. The aliases above are accepted but not advertised.
var Placeholders = []string{"version", "baseVersion", "name", "namespace"}

var placeholderPattern = regexp.MustCompile(`\{([A-Za-z]+)\}`)

// Compile turns a stored pattern into a matcher for one version's filenames.
// Placeholders are substituted before compilation and quoted as they go, so a
// version like "1.0.0+build.1" cannot smuggle regex syntax into the expression.
func Compile(matchType, pattern string, fields Fields) (*regexp.Regexp, error) {
	if err := Validate(matchType, pattern); err != nil {
		return nil, err
	}

	expression, err := substitute(matchType, pattern, fields)
	if err != nil {
		return nil, err
	}

	compiled, err := regexp.Compile("^(?:" + expression + ")$")
	if err != nil {
		return nil, fmt.Errorf("pattern is not a valid regular expression: %w", err)
	}
	return compiled, nil
}

// Validate checks a pattern without a version to substitute, which is what
// creating or editing a link has to do before anything has been published.
func Validate(matchType, pattern string) error {
	switch {
	case strings.TrimSpace(pattern) == "":
		return fmt.Errorf("pattern is required")
	case len(pattern) > MaxPatternLength:
		return fmt.Errorf("pattern must be at most %d characters", MaxPatternLength)
	case strings.Contains(pattern, "/"):
		return fmt.Errorf("pattern matches a filename, so it cannot contain '/'")
	}

	for _, match := range placeholderPattern.FindAllStringSubmatch(pattern, -1) {
		if _, ok := (Fields{}).value(match[1]); !ok {
			return fmt.Errorf("%s is not a known placeholder, use one of {%s}",
				match[0], strings.Join(Placeholders, "}, {"))
		}
	}

	switch matchType {
	case MatchTemplate:
		return nil
	case MatchRegex:
		// Substituting a specimen version keeps a placeholder from splitting a
		// group open, so what compiles here is what compiles at download time.
		specimen := Fields{Namespace: "namespace", Name: "name", Version: "1", BaseVersion: "1"}
		expression, err := substitute(MatchRegex, pattern, specimen)
		if err != nil {
			return err
		}
		if _, err := regexp.Compile("^(?:" + expression + ")$"); err != nil {
			return fmt.Errorf("pattern is not a valid regular expression: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("matchType must be one of: %s", strings.Join(MatchTypes, ", "))
	}
}

// Expand renders a pattern for display, with the placeholders filled in and no
// escaping applied. It is what the UI shows a link as currently resolving to.
func Expand(pattern string, fields Fields) string {
	return placeholderPattern.ReplaceAllStringFunc(pattern, func(match string) string {
		value, ok := fields.value(strings.Trim(match, "{}"))
		if !ok {
			return match
		}
		return value
	})
}

// substitute walks the pattern once, quoting each placeholder value as it is
// spliced in. A regex keeps everything between the placeholders verbatim; a
// template has only '*' as syntax, so the rest of it is escaped here too.
func substitute(matchType, pattern string, fields Fields) (string, error) {
	var builder strings.Builder
	var failure error

	between := func(text string) string {
		if matchType == MatchRegex {
			return text
		}
		parts := strings.Split(text, "*")
		for index, part := range parts {
			parts[index] = regexp.QuoteMeta(part)
		}
		return strings.Join(parts, wildcard)
	}

	end := 0
	for _, location := range placeholderPattern.FindAllStringSubmatchIndex(pattern, -1) {
		builder.WriteString(between(pattern[end:location[0]]))

		name := pattern[location[2]:location[3]]
		value, ok := fields.value(name)
		if !ok {
			failure = fmt.Errorf("{%s} is not a known placeholder", name)
			return "", failure
		}
		builder.WriteString(regexp.QuoteMeta(value))
		end = location[1]
	}
	builder.WriteString(between(pattern[end:]))

	return builder.String(), failure
}
