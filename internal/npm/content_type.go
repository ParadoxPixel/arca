package npm

import "strings"

func ContentTypeFor(filename string) string {
	switch {
	case strings.HasSuffix(filename, TarballSuffix):
		return "application/octet-stream"
	case strings.HasSuffix(filename, ".json"):
		return "application/json"
	default:
		return "application/octet-stream"
	}
}
