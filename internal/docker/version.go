package docker

import (
	"strconv"
	"strings"
)

// prereleaseMarkers are matched against whole words in a tag rather than as
// substrings, because a substring test reads "rc" out of "source" and "1.0-dev"
// out of nothing at all. Variant suffixes like "-alpine" or "-slim" name a base
// image and are deliberately absent: they are not prereleases of anything.
var prereleaseMarkers = map[string]bool{
	"alpha":    true,
	"beta":     true,
	"rc":       true,
	"dev":      true,
	"pre":      true,
	"snapshot": true,
	"nightly":  true,
	"edge":     true,
}

// tag is a Docker tag read as loosely as one can be. Tags are arbitrary strings,
// so the only structure worth trusting is a leading dotted number, and whether
// there is one at all.
type tag struct {
	raw       string
	versioned bool
	core      []int64
	suffix    string
}

func parseTag(raw string) tag {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	parsed := tag{raw: lowered}

	if lowered == "" || !isDigit(lowered[0]) {
		return parsed
	}
	parsed.versioned = true

	end := 0
	for {
		start := end
		for end < len(lowered) && isDigit(lowered[end]) {
			end++
		}
		if end == start {
			break
		}

		number, err := strconv.ParseInt(lowered[start:end], 10, 64)
		if err != nil {
			number = 0
		}
		parsed.core = append(parsed.core, number)

		// Only a dot with a digit behind it continues the number. A trailing dot
		// belongs to the suffix, where it cannot be mistaken for a segment.
		if end+1 < len(lowered) && lowered[end] == '.' && isDigit(lowered[end+1]) {
			end++
			continue
		}
		break
	}

	parsed.suffix = lowered[end:]
	return parsed
}

// words splits a suffix into the leading alphabetic run of each token, which is
// what a marker is matched against.
func words(text string) []string {
	var found []string

	index := 0
	for index < len(text) {
		for index < len(text) && !isLowerAlphanumeric(text[index]) {
			index++
		}
		start := index
		for index < len(text) && isLowerAlphanumeric(text[index]) {
			index++
		}
		if start == index {
			continue
		}

		token := text[start:index]
		end := 0
		for end < len(token) && !isDigit(token[end]) {
			end++
		}
		if end > 0 {
			found = append(found, token[:end])
		}
	}
	return found
}

// Compare orders tags. A tag that opens with a number is treated as a version
// and outranks every tag that does not, which puts "latest" and "alpine"
// underneath "1.25" instead of above it on a bytewise sort.
func Compare(a, b string) int {
	left, right := parseTag(a), parseTag(b)

	if left.versioned != right.versioned {
		if left.versioned {
			return 1
		}
		return -1
	}
	if !left.versioned {
		return strings.Compare(left.raw, right.raw)
	}

	for index := 0; index < len(left.core) || index < len(right.core); index++ {
		var leftPart, rightPart int64
		if index < len(left.core) {
			leftPart = left.core[index]
		}
		if index < len(right.core) {
			rightPart = right.core[index]
		}
		if leftPart != rightPart {
			if leftPart < rightPart {
				return -1
			}
			return 1
		}
	}

	// A bare version outranks the same version carrying anything else, whether
	// that is a prerelease marker or a variant.
	switch {
	case left.suffix == "" && right.suffix == "":
		return 0
	case left.suffix == "":
		return 1
	case right.suffix == "":
		return -1
	}
	return strings.Compare(left.suffix, right.suffix)
}

// Prerelease reads a versioned tag's suffix and an unversioned tag whole, so
// "1.0.0-rc1" and a bare "nightly" both report true while "latest" and
// "1.25-alpine" do not.
func Prerelease(raw string) bool {
	parsed := parseTag(raw)

	subject := parsed.suffix
	if !parsed.versioned {
		subject = parsed.raw
	}

	for _, word := range words(subject) {
		if prereleaseMarkers[word] {
			return true
		}
	}
	return false
}

// Core is the dotted number a tag opens with, which is what groups the variants
// and prereleases of one release together. A tag without one is its own base.
func Core(raw string) string {
	parsed := parseTag(raw)
	if !parsed.versioned {
		return parsed.raw
	}

	parts := make([]string, 0, len(parsed.core))
	for _, part := range parsed.core {
		parts = append(parts, strconv.FormatInt(part, 10))
	}
	return strings.Join(parts, ".")
}
