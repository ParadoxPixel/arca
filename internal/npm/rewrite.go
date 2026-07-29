package npm

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const abbreviatedMediaType = "application/vnd.npm.install-v1+json"

var abbreviatedDocumentFields = map[string]bool{
	"name":      true,
	"dist-tags": true,
	"modified":  true,
	"versions":  true,
}

var abbreviatedVersionFields = map[string]bool{
	"name":                 true,
	"version":              true,
	"dependencies":         true,
	"optionalDependencies": true,
	"devDependencies":      true,
	"peerDependencies":     true,
	"peerDependenciesMeta": true,
	"bundleDependencies":   true,
	"bin":                  true,
	"directories":          true,
	"dist":                 true,
	"engines":              true,
	"funding":              true,
	"deprecated":           true,
	"hasInstallScript":     true,
	"_hasShrinkwrap":       true,
	"os":                   true,
	"cpu":                  true,
	"acceptDependencies":   true,
}

var errUnusablePackage = errors.New("npm: the package name is not usable")

type RewriteOptions struct {
	Package     string
	Base        string
	Abbreviated bool
}

// WantsAbbreviated reports whether the client asked for the install document,
// which npm does for every resolution and which is an order of magnitude
// smaller than the full packument.
func WantsAbbreviated(accept string) bool {
	return strings.Contains(accept, abbreviatedMediaType)
}

// Rewrite points an upstream packument at this server. Only dist.tarball is
// touched, and it is rebuilt from the version key rather than string-replaced
// on the upstream base URL: mirrors serve tarballs from a different host than
// the registry itself, where a string replace silently leaves clients pointed
// at a third party.
func Rewrite(source io.Reader, options RewriteOptions) ([]byte, error) {
	_, bare, ok := SplitPackage(options.Package)
	if !ok {
		return nil, errUnusablePackage
	}

	document := map[string]json.RawMessage{}
	if err := json.NewDecoder(source).Decode(&document); err != nil {
		return nil, err
	}

	versions := map[string]json.RawMessage{}
	if raw, present := document["versions"]; present {
		if err := json.Unmarshal(raw, &versions); err != nil {
			return nil, err
		}
	}

	rewritten := make(map[string]json.RawMessage, len(versions))
	for version, raw := range versions {
		if !IsValidVersion(version) {
			continue
		}

		manifest := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			continue
		}

		dist := map[string]json.RawMessage{}
		if raw, present := manifest["dist"]; present {
			if err := json.Unmarshal(raw, &dist); err != nil {
				dist = map[string]json.RawMessage{}
			}
		}
		dist["tarball"] = mustEncode(options.Base + "/" + options.Package + "/-/" + TarballFilename(bare, version))

		encodedDist, err := json.Marshal(dist)
		if err != nil {
			return nil, err
		}
		manifest["dist"] = encodedDist

		if options.Abbreviated {
			manifest = keepOnly(manifest, abbreviatedVersionFields)
		}

		encoded, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}
		rewritten[version] = encoded
	}

	encodedVersions, err := json.Marshal(rewritten)
	if err != nil {
		return nil, err
	}
	document["versions"] = encodedVersions

	if options.Abbreviated {
		document = keepOnly(document, abbreviatedDocumentFields)
	}
	return json.Marshal(document)
}

// DistTagsOf reads the tags of an already rewritten document, which is what a
// proxy can answer "npm dist-tag ls" from.
func DistTagsOf(document []byte) (map[string]string, error) {
	var decoded struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.Unmarshal(document, &decoded); err != nil {
		return nil, err
	}
	if decoded.DistTags == nil {
		decoded.DistTags = map[string]string{}
	}
	return decoded.DistTags, nil
}

// VersionOf extracts the single manifest "npm view pkg@version" reads.
func VersionOf(document []byte, version string) (json.RawMessage, bool) {
	var decoded struct {
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(document, &decoded); err != nil {
		return nil, false
	}
	manifest, ok := decoded.Versions[version]
	return manifest, ok
}

func keepOnly(source map[string]json.RawMessage, allowed map[string]bool) map[string]json.RawMessage {
	filtered := make(map[string]json.RawMessage, len(allowed))
	for key, value := range source {
		if allowed[key] {
			filtered[key] = value
		}
	}
	return filtered
}
