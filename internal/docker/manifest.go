package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	MediaTypeOCIManifest = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeOCIIndex    = "application/vnd.oci.image.index.v1+json"
	MediaTypeOCIConfig   = "application/vnd.oci.image.config.v1+json"

	MediaTypeDockerManifest   = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerList       = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeDockerManifestV1 = "application/vnd.docker.distribution.manifest.v1+json"
	MediaTypeDockerConfig     = "application/vnd.docker.container.image.v1+json"
)

const (
	ReferenceConfig   = "config"
	ReferenceLayer    = "layer"
	ReferenceManifest = "manifest"
)

var (
	ErrSchemaVersion1 = errors.New("docker: schema 1 manifests are not supported")
	ErrUnknownShape   = errors.New("docker: the document is neither an image manifest nor an index")
)

type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
	OSVersion    string `json:"os.version,omitempty"`
}

func (p Platform) String() string {
	if p.OS == "" && p.Architecture == "" {
		return ""
	}

	label := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		label += "/" + p.Variant
	}
	return label
}

type Descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	// URLs names where a nondistributable layer can be fetched from. It is the
	// only place a manifest points outside the registry holding it.
	URLs         []string          `json:"urls,omitempty"`
	Platform     *Platform         `json:"platform,omitempty"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

// Manifest covers both shapes the registry API serves under one endpoint: an
// image manifest carrying a config and layers, and an index carrying child
// manifests per platform.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	Manifests     []Descriptor      `json:"manifests"`
	Subject       *Descriptor       `json:"subject,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

func (m Manifest) IsIndex() bool { return len(m.Manifests) > 0 }

// Reference is one edge from a manifest to something it needs, flattened so the
// indexer can write the whole set without caring which shape it came from.
type Reference struct {
	Digest    string
	Kind      string
	MediaType string
	Size      int64
	Position  int64
	Platform  string
	// URLs is set only for a nondistributable layer, which is the one kind of
	// reference whose bytes never live here.
	URLs []string
	// Annotations are the descriptor's own, not the child document's. buildkit marks
	// an attestation child here, which is the only way to tell one from a platform.
	Annotations map[string]string
}

const (
	// AnnotationReferenceType is what buildkit sets on the attestation child of a
	// multi-platform index. Such a child declares a platform of unknown/unknown, so
	// without this it would read as a broken architecture rather than as provenance.
	AnnotationReferenceType = "vnd.docker.reference.type"
)

func (m Manifest) References() []Reference {
	references := make([]Reference, 0, len(m.Layers)+len(m.Manifests)+1)

	if m.Config.Digest != "" {
		references = append(references, Reference{
			Digest:    m.Config.Digest,
			Kind:      ReferenceConfig,
			MediaType: m.Config.MediaType,
			Size:      m.Config.Size,
		})
	}
	for index, layer := range m.Layers {
		references = append(references, Reference{
			Digest:    layer.Digest,
			Kind:      ReferenceLayer,
			MediaType: layer.MediaType,
			Size:      layer.Size,
			Position:  int64(index),
			URLs:      layer.URLs,
		})
	}
	for index, child := range m.Manifests {
		reference := Reference{
			Digest:      child.Digest,
			Kind:        ReferenceManifest,
			MediaType:   child.MediaType,
			Size:        child.Size,
			Position:    int64(index),
			Annotations: child.Annotations,
		}
		if child.Platform != nil {
			reference.Platform = child.Platform.String()
		}
		references = append(references, reference)
	}

	return references
}

// DeclaredSize is what the document itself accounts for: the config plus the
// layers of an image, or the child manifest documents of an index. An index's
// real total needs its children resolved, which only the indexer can do.
func (m Manifest) DeclaredSize() int64 {
	total := m.Config.Size
	for _, layer := range m.Layers {
		total += layer.Size
	}
	for _, child := range m.Manifests {
		total += child.Size
	}
	return total
}

// ParseManifest reads a manifest and settles its media type. The field is
// mandatory for Docker schema 2 but was optional in OCI before 1.1, so a
// document without one is classified by its shape instead.
func ParseManifest(document []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(document, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("docker: reading a manifest: %w", err)
	}

	// Schema 1 describes its layers under entirely different keys, so it parses
	// into an empty manifest rather than failing outright.
	if manifest.SchemaVersion == 1 {
		return Manifest{}, ErrSchemaVersion1
	}

	isIndex := len(manifest.Manifests) > 0
	isImage := manifest.Config.Digest != ""
	if isIndex == isImage {
		return Manifest{}, ErrUnknownShape
	}

	if manifest.MediaType == "" {
		if isIndex {
			manifest.MediaType = MediaTypeOCIIndex
		} else {
			manifest.MediaType = MediaTypeOCIManifest
		}
	}

	for _, reference := range manifest.References() {
		if !IsValidDigest(reference.Digest) {
			return Manifest{}, fmt.Errorf("docker: %q is not a digest this server stores", reference.Digest)
		}
	}
	if manifest.Subject != nil && !IsValidDigest(manifest.Subject.Digest) {
		return Manifest{}, fmt.Errorf("docker: the subject digest %q is not one this server stores", manifest.Subject.Digest)
	}

	return manifest, nil
}

// Accepts reports whether a client's Accept header allows a stored manifest to be
// served as the type it was pushed with. A registry cannot convert between the
// Docker and OCI encodings, since either would change the digest the manifest is
// addressed by, so the only honest answers are the stored type or a 404.
//
// It takes every Accept line rather than one, because the docker daemon sends a
// separate header per media type instead of a single comma separated list.
//
// It errs towards serving. A header that names no manifest type at all, or one
// that wildcards, expresses no opinion worth refusing over, and only a client that
// listed manifest types and excluded this one is turned away.
func Accepts(accept []string, mediaType string) bool {
	opinionated := false
	for _, header := range accept {
		for _, entry := range strings.Split(header, ",") {
			candidate := strings.TrimSpace(strings.SplitN(entry, ";", 2)[0])

			switch candidate {
			case "", "*/*", "application/*":
				return true
			case mediaType:
				return true
			}
			opinionated = opinionated || IsManifestMediaType(candidate)
		}
	}
	return !opinionated
}

// IsNondistributable reports whether a layer names content a client fetches from
// its vendor rather than from a registry, which Windows base images rely on.
// Nothing ever pushes one, so a manifest referencing one must not be rejected for
// the blob being absent.
func IsNondistributable(mediaType string) bool {
	return strings.Contains(mediaType, "foreign") || strings.Contains(mediaType, "nondistributable")
}

// IsManifestMediaType reports whether a content type names a manifest rather
// than a blob, which is what tells a client's Accept header and an upload's
// Content-Type apart from layer bytes.
func IsManifestMediaType(mediaType string) bool {
	switch strings.TrimSpace(strings.SplitN(mediaType, ";", 2)[0]) {
	case MediaTypeOCIManifest, MediaTypeOCIIndex,
		MediaTypeDockerManifest, MediaTypeDockerList, MediaTypeDockerManifestV1:
		return true
	default:
		return false
	}
}
