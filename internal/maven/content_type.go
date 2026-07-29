package maven

import "strings"

var contentTypes = map[string]string{
	"jar":    "application/java-archive",
	"war":    "application/java-archive",
	"ear":    "application/java-archive",
	"aar":    "application/java-archive",
	"zip":    "application/zip",
	"gz":     "application/gzip",
	"tgz":    "application/gzip",
	"pom":    "application/xml",
	"xml":    "application/xml",
	"module": "application/json",
	"json":   "application/json",
	"txt":    "text/plain",
	"asc":    "text/plain",
	"sha1":   "text/plain",
	"sha256": "text/plain",
	"sha512": "text/plain",
	"md5":    "text/plain",
}

func ContentTypeFor(filename string) string {
	extension := ""
	if index := strings.LastIndex(filename, "."); index >= 0 {
		extension = strings.ToLower(filename[index+1:])
	}
	if contentType, ok := contentTypes[extension]; ok {
		return contentType
	}
	return "application/octet-stream"
}
