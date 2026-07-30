package docker

import (
	"strings"

	"arca/internal/format"
)

type Layout struct{}

// Coordinates names a tag. Only a tag manifest carries coordinates: a blob is
// shared by every image that references it and a digest-addressed manifest may
// have no tag at all, so neither belongs to one version.
func (Layout) Coordinates(path string) (format.Coordinates, bool) {
	image, tag, ok := ParseTagPath(path)
	if !ok {
		return format.Coordinates{}, false
	}

	namespace, name, ok := SplitImage(image)
	if !ok {
		return format.Coordinates{}, false
	}

	return format.Coordinates{
		Namespace:   namespace,
		Name:        name,
		Version:     tag,
		BaseVersion: Core(tag),
		IsSnapshot:  Prerelease(tag),
	}, true
}

func (Layout) ComponentAt(prefix string) (format.Coordinates, bool) {
	namespace, name, ok := SplitImage(strings.Trim(prefix, "/"))
	// ManifestFilename is itself a legal name component, so a prefix ending in it
	// is a stored file rather than an image directory.
	if !ok || name == ManifestFilename {
		return format.Coordinates{}, false
	}
	return format.Coordinates{Namespace: namespace, Name: name}, true
}

func (Layout) ContentType(filename string) string { return ContentTypeFor(filename) }

// Mutable is the mirror image of what the other formats report. Everything
// content-addressed is immutable by construction, and a tag is the one thing a
// registry lets move, so a proxy rechecks tags and caches blobs forever.
func (Layout) Mutable(path string) bool {
	_, _, ok := ParseTagPath(path)
	return ok
}

func (Layout) Compare(a, b string) int { return Compare(a, b) }

func (Layout) Prerelease(version string) bool { return Prerelease(version) }
