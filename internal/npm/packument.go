package npm

import (
	"encoding/json"
	"sort"
	"time"
)

type Dist struct {
	Tarball   string `json:"tarball"`
	Integrity string `json:"integrity,omitempty"`
	Shasum    string `json:"shasum,omitempty"`
	FileCount int    `json:"fileCount,omitempty"`
	Size      int64  `json:"unpackedSize,omitempty"`
}

type Release struct {
	Version   string
	Manifest  map[string]json.RawMessage
	Dist      Dist
	Published int64
}

type Packument struct {
	Namespace string
	Name      string
	DistTags  map[string]string
	Releases  []Release
	Created   int64
	Modified  int64
}

func timestamp(epochMillis int64) string {
	return time.UnixMilli(epochMillis).UTC().Format(time.RFC3339)
}

// Render assembles the document npm reads. Each release keeps the manifest it
// was published with, except for dist, which is always recomputed from what was
// actually stored rather than echoed back from the publish request.
func Render(input Packument) ([]byte, error) {
	full := PackageName(input.Namespace, input.Name)

	versions := map[string]json.RawMessage{}
	times := map[string]string{
		"created":  timestamp(input.Created),
		"modified": timestamp(input.Modified),
	}

	for _, release := range input.Releases {
		manifest := map[string]json.RawMessage{}
		for key, value := range release.Manifest {
			manifest[key] = value
		}

		encodedDist, err := json.Marshal(release.Dist)
		if err != nil {
			return nil, err
		}
		manifest["dist"] = encodedDist
		manifest["name"] = mustEncode(full)
		manifest["version"] = mustEncode(release.Version)

		encoded, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}
		versions[release.Version] = encoded
		times[release.Version] = timestamp(release.Published)
	}

	document := map[string]any{
		"_id":       full,
		"name":      full,
		"dist-tags": input.DistTags,
		"versions":  versions,
		"time":      times,
	}
	if latest := input.DistTags["latest"]; latest != "" {
		if manifest, ok := input.releaseNamed(latest); ok {
			document["description"] = rawOrNull(manifest.Manifest["description"])
		}
	}

	return json.Marshal(document)
}

func (p Packument) releaseNamed(version string) (Release, bool) {
	for _, release := range p.Releases {
		if release.Version == version {
			return release, true
		}
	}
	return Release{}, false
}

func rawOrNull(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func mustEncode(value string) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return encoded
}

// LatestVersion is the version a bare "npm install <package>" resolves to: the
// highest release, falling back to the highest prerelease when that is all
// there is.
func LatestVersion(versions []string) string {
	sorted := append([]string(nil), versions...)
	sort.SliceStable(sorted, func(i, j int) bool { return Compare(sorted[i], sorted[j]) < 0 })

	latest := ""
	for _, version := range sorted {
		if !IsPrerelease(version) {
			latest = version
		}
	}
	if latest == "" && len(sorted) > 0 {
		latest = sorted[len(sorted)-1]
	}
	return latest
}

// RenderVersion is what "npm view <package>@<version>" reads.
func RenderVersion(input Packument, release Release) ([]byte, error) {
	document, err := Render(Packument{
		Namespace: input.Namespace,
		Name:      input.Name,
		DistTags:  input.DistTags,
		Releases:  []Release{release},
		Created:   input.Created,
		Modified:  input.Modified,
	})
	if err != nil {
		return nil, err
	}

	var decoded struct {
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(document, &decoded); err != nil {
		return nil, err
	}
	return decoded.Versions[release.Version], nil
}
