package models

const (
	DockerReferenceConfig   = "config"
	DockerReferenceLayer    = "layer"
	DockerReferenceManifest = "manifest"
)

// DockerUpload is one in-flight blob push. It lives in the database rather than
// in memory so a GET on the upload location reports a truthful offset and a
// restart strands no half-written file that nothing will ever clean up.
type DockerUpload struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"index;not null"`
	Image        string `gorm:"not null"`
	Size         int64  `gorm:"not null;default:0"`
	UserID       *string
	CreatedAt    int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64 `gorm:"autoUpdateTime:milli;index"`
}

func (DockerUpload) TableName() string { return "docker_uploads" }

// DockerManifest is the parsed form of a manifest document, keyed by the digest
// of the bytes it was parsed from. The bytes themselves are an asset; this is
// everything about them worth querying without reopening the file.
type DockerManifest struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"not null;uniqueIndex:docker_manifests_digest,priority:1;index:docker_manifests_image,priority:1"`
	Digest       string `gorm:"not null;uniqueIndex:docker_manifests_digest,priority:2"`
	MediaType    string `gorm:"not null;default:''"`
	Size         int64  `gorm:"not null;default:0"`
	Namespace    string `gorm:"not null;default:'';index:docker_manifests_image,priority:2"`
	Name         string `gorm:"not null;default:'';index:docker_manifests_image,priority:3"`
	ConfigDigest string `gorm:"not null;default:''"`
	Architecture string `gorm:"not null;default:''"`
	OS           string `gorm:"column:os;not null;default:''"`
	Variant      string `gorm:"not null;default:''"`
	// ImageCreated is when the image was built, read from its config blob, as
	// opposed to CreatedAt which is when this server first saw it.
	ImageCreated int64  `gorm:"not null;default:0"`
	LayerCount   int    `gorm:"not null;default:0"`
	TotalSize    int64  `gorm:"not null;default:0"`
	Labels       string `gorm:"not null;default:''"`
	Annotations  string `gorm:"not null;default:''"`
	Subject      string `gorm:"not null;default:''"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli"`
}

func (DockerManifest) TableName() string { return "docker_manifests" }

func (m DockerManifest) IsIndex() bool { return m.ConfigDigest == "" }

// DockerReference is one edge from a manifest to a blob or to a child manifest.
// It exists because an asset row can belong to one component, and a layer is
// shared by every tag that references it, so per-version size and safe deletion
// cannot be read off the assets alone.
type DockerReference struct {
	RepositoryID   string `gorm:"primaryKey;index:docker_references_child,priority:1"`
	ManifestDigest string `gorm:"primaryKey"`
	// The reverse index answers "which manifests still need this blob", which is
	// the question both the shared-layer marker and the eventual sweep ask.
	ChildDigest string `gorm:"primaryKey;index:docker_references_child,priority:2"`
	Kind        string `gorm:"not null"`
	MediaType   string `gorm:"not null;default:''"`
	Size        int64  `gorm:"not null;default:0"`
	Position    int64  `gorm:"not null;default:0"`
	Platform    string `gorm:"not null;default:''"`
	// URLs holds the newline-separated sources of a nondistributable layer, whose
	// bytes are never pushed here. Recording them keeps the layer accountable in
	// the UI instead of showing as a blob that is inexplicably missing.
	URLs string `gorm:"column:urls;not null;default:''"`
	// Annotations are the descriptor's own, encoded as JSON. They are what tells an
	// attestation child of an index apart from a real platform.
	Annotations string `gorm:"not null;default:''"`
}

// IsForeign reports a layer whose bytes live outside this registry, which is why
// it has no asset behind it and must not be counted as stored.
func (r DockerReference) IsForeign() bool { return r.URLs != "" }

func (DockerReference) TableName() string { return "docker_references" }
