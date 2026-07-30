package docker

// ContentTypeFor is only a fallback. A manifest's real media type comes from the
// Content-Type it was pushed with, which the upload path records, because the
// filename cannot tell an OCI manifest from a Docker one or from an index.
func ContentTypeFor(filename string) string {
	if filename == ManifestFilename {
		return MediaTypeOCIManifest
	}
	return "application/octet-stream"
}
