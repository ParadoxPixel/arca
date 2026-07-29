package npm

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrNoAttachment  = errors.New("npm: the request carries no tarball")
	ErrManyVersions  = errors.New("npm: only one version can be published at a time")
	ErrBadPackage    = errors.New("npm: the package name is not usable")
	ErrBadVersion    = errors.New("npm: the version is not valid semver")
	ErrNameMismatch  = errors.New("npm: the manifest name does not match the document")
	ErrBadAttachment = errors.New("npm: the attachment is not decodable")
)

type Publish struct {
	Namespace string
	Name      string
	Version   string
	Manifest  map[string]json.RawMessage
	Integrity string
	Shasum    string
	DistTags  map[string]string

	data string
}

// Tarball streams the decoded attachment. The base64 payload is held as a
// string because encoding/json hands over a string token whole; the decode
// itself is streamed so only one inflated copy is ever in memory.
func (p *Publish) Tarball() io.Reader {
	return base64.NewDecoder(base64.StdEncoding, strings.NewReader(p.data))
}

// Integrity converts a hex SHA-512 into the subresource form npm records. The
// blob store keeps hex because the Maven checksum endpoints read it directly.
func Integrity(hexSHA512 string) string {
	raw, err := hex.DecodeString(hexSHA512)
	if err != nil {
		return ""
	}
	return "sha512-" + base64.StdEncoding.EncodeToString(raw)
}

type attachment struct {
	Data string `json:"data"`
}

type publishBody struct {
	Name        string                                `json:"name"`
	DistTags    map[string]string                     `json:"dist-tags"`
	Versions    map[string]map[string]json.RawMessage `json:"versions"`
	Attachments map[string]attachment                 `json:"_attachments"`
}

// Update is a PUT on a package. npm sends the same document shape to publish a
// version and to deprecate one; only the attachment tells them apart.
type Update struct {
	Namespace string
	Name      string
	DistTags  map[string]string
	Versions  map[string]map[string]json.RawMessage

	attachments map[string]attachment
}

func ParseUpdate(source io.Reader) (*Update, error) {
	var body publishBody
	if err := json.NewDecoder(source).Decode(&body); err != nil {
		return nil, fmt.Errorf("npm: decoding the publish request: %w", err)
	}

	namespace, name, ok := SplitPackage(body.Name)
	if !ok {
		return nil, ErrBadPackage
	}
	if len(body.Versions) == 0 {
		return nil, ErrNoAttachment
	}

	return &Update{
		Namespace:   namespace,
		Name:        name,
		DistTags:    body.DistTags,
		Versions:    body.Versions,
		attachments: body.Attachments,
	}, nil
}

func (u *Update) CarriesTarball() bool { return len(u.attachments) > 0 }

// Deprecations maps each version the document marks to its message. An empty
// message is npm lifting a deprecation.
func (u *Update) Deprecations() map[string]string {
	messages := map[string]string{}
	for version, manifest := range u.Versions {
		raw, ok := manifest["deprecated"]
		if !ok {
			continue
		}

		var message string
		if err := json.Unmarshal(raw, &message); err != nil {
			continue
		}
		messages[version] = message
	}
	return messages
}

func (u *Update) Publish() (*Publish, error) {
	if len(u.Versions) > 1 {
		return nil, ErrManyVersions
	}
	if len(u.attachments) != 1 {
		return nil, ErrNoAttachment
	}

	published := &Publish{
		Namespace: u.Namespace,
		Name:      u.Name,
		DistTags:  u.DistTags,
	}
	for version, manifest := range u.Versions {
		published.Version, published.Manifest = version, manifest
	}
	if !IsValidVersion(published.Version) {
		return nil, ErrBadVersion
	}

	if err := published.readManifestName(); err != nil {
		return nil, err
	}
	published.readDist()

	// The attachment key is a client-supplied filename, so it is read for its
	// payload only and never used to build a path.
	for _, entry := range u.attachments {
		published.data = entry.Data
	}
	if published.data == "" {
		return nil, ErrNoAttachment
	}
	if _, err := base64.StdEncoding.DecodeString(published.data[:min(64, len(published.data))]); err != nil {
		return nil, ErrBadAttachment
	}

	return published, nil
}

func (p *Publish) readManifestName() error {
	raw, ok := p.Manifest["name"]
	if !ok {
		return nil
	}

	var declared string
	if err := json.Unmarshal(raw, &declared); err != nil {
		return ErrNameMismatch
	}
	if declared != PackageName(p.Namespace, p.Name) {
		return ErrNameMismatch
	}
	return nil
}

func (p *Publish) readDist() {
	raw, ok := p.Manifest["dist"]
	if !ok {
		return
	}

	var dist Dist
	if err := json.Unmarshal(raw, &dist); err != nil {
		return
	}
	p.Integrity, p.Shasum = dist.Integrity, dist.Shasum
}
