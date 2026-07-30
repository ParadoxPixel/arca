// Package docker addresses OCI and Docker registry content: image names, tags,
// digests, the storage layout they map onto and the manifest documents that tie
// them together. Nothing here speaks HTTP.
package docker

import "strings"

const (
	MaxNameLength = 255
	MaxTagLength  = 128

	digestSHA256 = "sha256"
	digestSHA512 = "sha512"
)

func isLowerAlphanumeric(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
}

func isDigit(character byte) bool { return character >= '0' && character <= '9' }

func isSeparator(character byte) bool {
	return character == '.' || character == '_' || character == '-'
}

func isHex(character byte) bool {
	return isDigit(character) || (character >= 'a' && character <= 'f')
}

// isValidSeparatorRun holds the odd corner of the distribution spec's name
// grammar: a run between two alphanumerics may be one dot, one or two
// underscores, or any number of dashes, and it may not mix the three.
func isValidSeparatorRun(run string) bool {
	for index := 1; index < len(run); index++ {
		if run[index] != run[0] {
			return false
		}
	}

	switch run[0] {
	case '.':
		return len(run) == 1
	case '_':
		return len(run) <= 2
	case '-':
		return true
	default:
		return false
	}
}

func isValidPathComponent(component string) bool {
	index := 0
	for {
		start := index
		for index < len(component) && isLowerAlphanumeric(component[index]) {
			index++
		}
		if index == start {
			return false
		}
		if index == len(component) {
			return true
		}

		start = index
		for index < len(component) && isSeparator(component[index]) {
			index++
		}
		// Neither an unseparated illegal byte nor a trailing separator can be
		// followed by the alphanumeric run the grammar requires.
		if index == start || index == len(component) || !isValidSeparatorRun(component[start:index]) {
			return false
		}
	}
}

// IsValidName reports whether an image name is one a registry client can
// address. Every component is lowercase, which is why pushing an image built
// with a capital letter in its name fails before it reaches any registry.
func IsValidName(name string) bool {
	if name == "" || len(name) > MaxNameLength {
		return false
	}
	for _, component := range strings.Split(name, "/") {
		if !isValidPathComponent(component) {
			return false
		}
	}
	return true
}

// IsValidTag also rejects ManifestsDirectory. A tag is stored as a path segment
// beside that directory, and unlike an image component a tag may begin with an
// underscore, so it is the one name that could collide with the digest store.
func IsValidTag(tag string) bool {
	if tag == "" || len(tag) > MaxTagLength || tag == ManifestsDirectory {
		return false
	}

	first := tag[0]
	if !isLowerAlphanumeric(first) && !(first >= 'A' && first <= 'Z') && first != '_' {
		return false
	}

	for index := 1; index < len(tag); index++ {
		character := tag[index]
		if isLowerAlphanumeric(character) || (character >= 'A' && character <= 'Z') || isSeparator(character) {
			continue
		}
		return false
	}
	return true
}

// Digest is a content address. Only the algorithms this server can store are
// accepted, so an exotic one is rejected on the way in rather than producing a
// storage path nothing will ever read back.
type Digest struct {
	Algorithm string
	Hex       string
}

func hexLengthFor(algorithm string) int {
	switch algorithm {
	case digestSHA256:
		return 64
	case digestSHA512:
		return 128
	default:
		return 0
	}
}

func ParseDigest(raw string) (Digest, bool) {
	algorithm, encoded, found := strings.Cut(raw, ":")
	if !found {
		return Digest{}, false
	}

	expected := hexLengthFor(algorithm)
	if expected == 0 || len(encoded) != expected {
		return Digest{}, false
	}
	for index := 0; index < len(encoded); index++ {
		if !isHex(encoded[index]) {
			return Digest{}, false
		}
	}
	return Digest{Algorithm: algorithm, Hex: encoded}, true
}

func IsValidDigest(raw string) bool {
	_, ok := ParseDigest(raw)
	return ok
}

func (d Digest) String() string { return d.Algorithm + ":" + d.Hex }

// Short is the abbreviation used in the UI and in log lines. Twelve characters
// is what the Docker CLI shows and it is long enough to stay unambiguous.
func (d Digest) Short() string {
	if len(d.Hex) <= 12 {
		return d.Hex
	}
	return d.Hex[:12]
}

// SHA256 wraps a hex digest this server computed itself. Asset rows already
// store one per blob, and for a manifest that digest is its content address.
func SHA256(hex string) Digest { return Digest{Algorithm: digestSHA256, Hex: hex} }
