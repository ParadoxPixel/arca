package docker

import "strings"

const (
	// BlobsDirectory and ManifestsDirectory both begin with an underscore, which
	// no image name component may, so neither can ever collide with a real
	// image path. Blobs sit at the repository root because one blob is shared by
	// every image that references it.
	BlobsDirectory     = "_blobs"
	ManifestsDirectory = "_manifests"
	ManifestFilename   = "manifest.json"
)

func BlobPath(digest Digest) string {
	return BlobsDirectory + "/" + digest.Algorithm + "/" + digest.Hex
}

func ManifestPath(image string, digest Digest) string {
	return image + "/" + ManifestsDirectory + "/" + digest.Algorithm + "/" + digest.Hex
}

func TagDirectory(image, tag string) string { return image + "/" + tag }

// TagPath holds the manifest bytes a tag resolves to. Storing the document
// rather than a pointer to it means the asset row's own SHA256 is the manifest
// digest, so a tag pull needs one lookup and no indirection.
func TagPath(image, tag string) string {
	return TagDirectory(image, tag) + "/" + ManifestFilename
}

// ParseTagPath reads the image and tag back out of a stored tag manifest path.
// The name and tag grammars do the guarding: an image component cannot begin
// with an underscore and a tag cannot be ManifestsDirectory, so no path under
// the blob or digest stores can be mistaken for a tag.
func ParseTagPath(path string) (image string, tag string, ok bool) {
	rest, found := strings.CutSuffix(path, "/"+ManifestFilename)
	if !found {
		return "", "", false
	}

	cut := strings.LastIndexByte(rest, '/')
	if cut <= 0 {
		return "", "", false
	}

	image, tag = rest[:cut], rest[cut+1:]
	if !IsValidName(image) || !IsValidTag(tag) {
		return "", "", false
	}
	return image, tag, true
}

// IsBlobPath and IsDigestManifestPath recognise the two content-addressed stores.
// The sweep needs them because neither has coordinates and so neither is reachable
// through ParseTagPath, which is how everything else is identified.
func IsBlobPath(path string) bool {
	rest, found := strings.CutPrefix(path, BlobsDirectory+"/")
	if !found {
		return false
	}
	return isDigestSuffix(rest)
}

func IsDigestManifestPath(path string) bool {
	marker := "/" + ManifestsDirectory + "/"

	cut := strings.LastIndex(path, marker)
	if cut <= 0 {
		return false
	}
	if !IsValidName(path[:cut]) {
		return false
	}
	return isDigestSuffix(path[cut+len(marker):])
}

// ImageOfManifestPath reads the image out of a digest-addressed manifest path. The
// document itself never names the image it belongs to, so the path is the only source.
func ImageOfManifestPath(path string) string {
	cut := strings.LastIndex(path, "/"+ManifestsDirectory+"/")
	if cut <= 0 {
		return ""
	}
	return path[:cut]
}

// isDigestSuffix matches the "<algorithm>/<hex>" tail both stores end with.
func isDigestSuffix(rest string) bool {
	algorithm, encoded, found := strings.Cut(rest, "/")
	if !found {
		return false
	}
	return IsValidDigest(algorithm + ":" + encoded)
}

// SplitImage maps an image name onto arca coordinates: the leading path is the
// namespace and the last component is the name, so "erc/research-grant" reads
// the way "@scope/package" does for npm and "group:artifact" does for Maven. A
// single-component image has no namespace.
func SplitImage(image string) (namespace string, name string, ok bool) {
	if !IsValidName(image) {
		return "", "", false
	}
	if cut := strings.LastIndexByte(image, '/'); cut >= 0 {
		return image[:cut], image[cut+1:], true
	}
	return "", image, true
}

func ImageName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}
