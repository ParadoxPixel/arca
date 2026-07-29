package format

import (
	"sort"
	"strings"
)

func IsSafe(path string) bool {
	if path == "" || len(path) > 1024 {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func SortVersions(layout Layout, versions []string) []string {
	sorted := append([]string(nil), versions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return layout.Compare(sorted[i], sorted[j]) < 0
	})
	return sorted
}

func LatestAndRelease(layout Layout, versions []string) (latest string, release string) {
	sorted := SortVersions(layout, versions)
	if len(sorted) > 0 {
		latest = sorted[len(sorted)-1]
	}
	for _, version := range sorted {
		if !layout.Prerelease(version) {
			release = version
		}
	}
	return latest, release
}
