package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/charmbracelet/log"

	"arca/internal/docker"
	"arca/internal/format"
	"arca/internal/store/models"
)

type dockerLayerResponse struct {
	Digest    string `json:"digest"`
	Short     string `json:"short"`
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Position  int64  `json:"position"`
	// SharedWith counts the other manifests here that need this layer, so the UI
	// can explain why removing one tag reclaims less than its total size.
	SharedWith int      `json:"sharedWith"`
	Foreign    bool     `json:"foreign"`
	URLs       []string `json:"urls"`
	Stored     bool     `json:"stored"`
}

type dockerChildResponse struct {
	Digest       string `json:"digest"`
	Short        string `json:"short"`
	MediaType    string `json:"mediaType"`
	Platform     string `json:"platform"`
	Size         int64  `json:"size"`
	TotalSize    int64  `json:"totalSize"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
	LayerCount   int    `json:"layerCount"`
	Indexed      bool   `json:"indexed"`
	// ReferenceType names a child that is not a platform at all. buildkit attaches
	// provenance and SBOM attestations to every multi-platform build, and they
	// declare a platform of unknown/unknown, so without this they read as broken.
	ReferenceType string `json:"referenceType"`
}

type dockerHistoryResponse struct {
	Created    int64  `json:"created"`
	CreatedBy  string `json:"createdBy"`
	Comment    string `json:"comment"`
	EmptyLayer bool   `json:"emptyLayer"`
}

type dockerConfigResponse struct {
	Digest       string                  `json:"digest"`
	Size         int64                   `json:"size"`
	User         string                  `json:"user"`
	WorkingDir   string                  `json:"workingDir"`
	Entrypoint   []string                `json:"entrypoint"`
	Cmd          []string                `json:"cmd"`
	Env          []string                `json:"env"`
	ExposedPorts []string                `json:"exposedPorts"`
	History      []dockerHistoryResponse `json:"history"`
}

type dockerManifestResponse struct {
	Repository   string                `json:"repository"`
	Namespace    string                `json:"namespace"`
	Name         string                `json:"name"`
	Image        string                `json:"image"`
	Reference    string                `json:"reference"`
	Digest       string                `json:"digest"`
	Short        string                `json:"short"`
	MediaType    string                `json:"mediaType"`
	Size         int64                 `json:"size"`
	TotalSize    int64                 `json:"totalSize"`
	LayerCount   int                   `json:"layerCount"`
	IsIndex      bool                  `json:"isIndex"`
	OS           string                `json:"os"`
	Architecture string                `json:"architecture"`
	Variant      string                `json:"variant"`
	ImageCreated int64                 `json:"imageCreated"`
	Labels       map[string]string     `json:"labels"`
	Annotations  map[string]string     `json:"annotations"`
	Layers       []dockerLayerResponse `json:"layers"`
	Children     []dockerChildResponse `json:"children"`
	Config       *dockerConfigResponse `json:"config"`
	PullBy       map[string]string     `json:"pullBy"`
}

// routeDockerManifest describes one tag or digest: its layers, how much of each is
// shared, the platforms of an index, and the config the image was built with. It is
// what the file listing cannot be for docker, where a version's only file is a
// manifest of a few kilobytes and its real weight lives in shared blobs.
func (s *Server) routeDockerManifest(writer http.ResponseWriter, request *http.Request) error {
	repository, _, err := s.loadAccessible(request, models.PermissionRead)
	if err != nil {
		return err
	}
	if repository.Format != format.Docker {
		return fail(http.StatusBadRequest, "This is not a docker repository")
	}

	namespace, name, err := requireCoordinates(request)
	if err != nil {
		return err
	}
	reference, err := requireText(request.URL.Query().Get("reference"), "reference", 200)
	if err != nil {
		return err
	}

	image := docker.ImageName(namespace, name)
	digest, err := s.resolveDockerReference(repository, image, reference)
	if err != nil {
		return err
	}

	record, err := s.store.DockerManifest(repository.ID, digest.String())
	if err != nil {
		return fail(http.StatusNotFound, "That manifest has not been indexed")
	}
	var config *docker.Config

	references, err := s.store.DockerReferences(repository.ID, digest.String())
	if err != nil {
		return err
	}

	response := dockerManifestResponse{
		Repository:   repository.Name,
		Namespace:    namespace,
		Name:         name,
		Image:        image,
		Reference:    reference,
		Digest:       record.Digest,
		Short:        digest.Short(),
		MediaType:    record.MediaType,
		Size:         record.Size,
		TotalSize:    record.TotalSize,
		LayerCount:   record.LayerCount,
		IsIndex:      record.IsIndex(),
		OS:           record.OS,
		Architecture: record.Architecture,
		Variant:      record.Variant,
		ImageCreated: record.ImageCreated,
		Labels:       decodeJSONMap(record.Labels),
		Annotations:  decodeJSONMap(record.Annotations),
		Layers:       []dockerLayerResponse{},
		Children:     []dockerChildResponse{},
		PullBy: map[string]string{
			"tag":    repository.Name + "/" + image + ":" + reference,
			"digest": repository.Name + "/" + image + "@" + record.Digest,
		},
	}

	if response.IsIndex {
		response.Children, err = s.describeDockerChildren(repository, references)
		if err == nil {
			s.backfillDockerIndexTime(repository, record, &response)
		}
	} else {
		response.Layers, err = s.describeDockerLayers(repository, references)
		if err == nil {
			response.Config, config = s.describeDockerConfig(repository, references)
		}
	}
	if err != nil {
		return err
	}

	// A proxy fetches a manifest before the config blob it points at, so indexing had
	// nothing to read the platform from. Filling it in here, and writing it back, is
	// what stops a proxied image reporting an unknown architecture for ever.
	if config != nil {
		s.backfillDockerPlatform(repository, record, *config, &response)
	}

	writeJSON(writer, http.StatusOK, response)
	return nil
}

// resolveDockerReference turns a tag or a digest into the digest to look up. A tag
// resolves through the asset that holds its manifest, whose own SHA256 is that
// digest, so no tag table is needed.
func (s *Server) resolveDockerReference(repository *models.Repository, image, reference string) (docker.Digest, error) {
	if digest, ok := docker.ParseDigest(reference); ok {
		return digest, nil
	}
	if !docker.IsValidTag(reference) {
		return docker.Digest{}, fail(http.StatusBadRequest, "reference must be a tag or a digest")
	}

	asset, err := s.store.FindAsset(repository.ID, docker.TagPath(image, reference))
	if err != nil {
		return docker.Digest{}, fail(http.StatusNotFound, "There is no tag %q on %s", reference, image)
	}
	return docker.SHA256(asset.SHA256), nil
}

func (s *Server) describeDockerLayers(repository *models.Repository, references []models.DockerReference) ([]dockerLayerResponse, error) {
	layers := make([]dockerLayerResponse, 0, len(references))

	digests := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.Kind == models.DockerReferenceLayer {
			digests = append(digests, reference.ChildDigest)
		}
	}

	sharing, err := s.store.DockerBlobSharing(repository.ID, digests)
	if err != nil {
		return nil, err
	}

	for _, reference := range references {
		if reference.Kind != models.DockerReferenceLayer {
			continue
		}

		layer := dockerLayerResponse{
			Digest:    reference.ChildDigest,
			MediaType: reference.MediaType,
			Size:      reference.Size,
			Position:  reference.Position,
			Foreign:   reference.IsForeign(),
			URLs:      splitLines(reference.URLs),
			// The count includes this manifest, so what the UI wants is the rest.
			SharedWith: max(sharing[reference.ChildDigest]-1, 0),
		}
		if digest, ok := docker.ParseDigest(reference.ChildDigest); ok {
			layer.Short = digest.Short()
			layer.Stored = s.dockerBlobStored(repository, digest)
		}

		layers = append(layers, layer)
	}
	return layers, nil
}

// dockerBlobStored separates a layer this server holds from one it only knows
// about, which is the difference between a foreign layer and a broken one.
func (s *Server) dockerBlobStored(repository *models.Repository, digest docker.Digest) bool {
	_, err := s.store.FindAsset(repository.ID, docker.BlobPath(digest))
	return err == nil
}

func (s *Server) describeDockerChildren(repository *models.Repository, references []models.DockerReference) ([]dockerChildResponse, error) {
	digests := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.Kind == models.DockerReferenceManifest {
			digests = append(digests, reference.ChildDigest)
		}
	}

	indexed, err := s.store.DockerManifestsByDigest(repository.ID, digests)
	if err != nil {
		return nil, err
	}

	children := make([]dockerChildResponse, 0, len(digests))
	for _, reference := range references {
		if reference.Kind != models.DockerReferenceManifest {
			continue
		}

		child := dockerChildResponse{
			Digest:        reference.ChildDigest,
			MediaType:     reference.MediaType,
			Platform:      reference.Platform,
			Size:          reference.Size,
			ReferenceType: decodeJSONMap(reference.Annotations)[docker.AnnotationReferenceType],
		}
		if digest, ok := docker.ParseDigest(reference.ChildDigest); ok {
			child.Short = digest.Short()
		}

		// A child that has not been indexed is normal while an index is still being
		// pushed, and permanent in a partially migrated repository, so it is
		// reported as unindexed rather than left out.
		if record, ok := indexed[reference.ChildDigest]; ok {
			child.Indexed = true
			child.TotalSize = record.TotalSize
			child.OS = record.OS
			child.Architecture = record.Architecture
			child.Variant = record.Variant
			child.LayerCount = record.LayerCount
		}

		children = append(children, child)
	}
	return children, nil
}

// describeDockerConfig reads the build-time settings out of the config blob. It is
// read here rather than stored, because it is small, already local, and duplicating
// it into a column would only give it a second chance to go stale.
// backfillDockerPlatform repairs a record indexed before its config blob arrived. It
// writes during a read, which is worth it: the alternative is a permanently blank
// platform on every proxied image, and the value is derived rather than authored so
// recomputing it costs nothing but the one update.
func (s *Server) backfillDockerPlatform(repository *models.Repository, record *models.DockerManifest, config docker.Config, response *dockerManifestResponse) {
	if record.Architecture != "" || config.Architecture == "" {
		return
	}

	record.Architecture = config.Architecture
	record.OS = config.OS
	record.Variant = config.Variant
	record.ImageCreated = config.Created
	record.Labels = encodeJSONMap(config.Labels)

	response.Architecture = record.Architecture
	response.OS = record.OS
	response.Variant = record.Variant
	response.ImageCreated = record.ImageCreated
	response.Labels = config.Labels

	if err := s.store.UpdateDockerManifestPlatform(record); err != nil {
		log.Warnf("recording the platform of %s/%s failed: %v", repository.Name, record.Digest, err)
	}
}

// backfillDockerIndexTime is the same repair one level up. An index has no config, so
// it takes its build time from its children, and on a proxy those are fetched after the
// index itself and so were not indexed when it was.
func (s *Server) backfillDockerIndexTime(repository *models.Repository, record *models.DockerManifest, response *dockerManifestResponse) {
	if record.ImageCreated != 0 {
		return
	}

	var newest int64
	for _, child := range response.Children {
		if indexed, err := s.store.DockerManifest(repository.ID, child.Digest); err == nil {
			newest = max(newest, indexed.ImageCreated)
		}
	}
	if newest == 0 {
		return
	}

	record.ImageCreated = newest
	response.ImageCreated = newest

	if err := s.store.UpdateDockerManifestPlatform(record); err != nil {
		log.Warnf("recording the build time of %s/%s failed: %v", repository.Name, record.Digest, err)
	}
}

func (s *Server) describeDockerConfig(repository *models.Repository, references []models.DockerReference) (*dockerConfigResponse, *docker.Config) {
	var entry models.DockerReference
	for _, reference := range references {
		if reference.Kind == models.DockerReferenceConfig {
			entry = reference
			break
		}
	}
	if entry.ChildDigest == "" {
		return nil, nil
	}

	digest, ok := docker.ParseDigest(entry.ChildDigest)
	if !ok {
		return nil, nil
	}

	asset, err := s.store.FindAsset(repository.ID, docker.BlobPath(digest))
	if err != nil {
		return nil, nil
	}

	file, _, err := s.blobs.Open(asset.StorageKey)
	if err != nil {
		return nil, nil
	}
	defer file.Close()

	document, err := io.ReadAll(io.LimitReader(file, maxManifestBytes))
	if err != nil {
		return nil, nil
	}

	config, err := docker.ParseConfig(document)
	if err != nil {
		return nil, nil
	}

	response := &dockerConfigResponse{
		Digest:       entry.ChildDigest,
		Size:         entry.Size,
		User:         config.User,
		WorkingDir:   config.WorkingDir,
		Entrypoint:   orEmpty(config.Entrypoint),
		Cmd:          orEmpty(config.Cmd),
		Env:          orEmpty(config.Env),
		ExposedPorts: orEmpty(config.ExposedPorts),
		History:      []dockerHistoryResponse{},
	}
	for _, step := range config.History {
		response.History = append(response.History, dockerHistoryResponse{
			Created:    step.Created,
			CreatedBy:  step.CreatedBy,
			Comment:    step.Comment,
			EmptyLayer: step.EmptyLayer,
		})
	}
	return response, &config
}

func decodeJSONMap(encoded string) map[string]string {
	values := map[string]string{}
	if encoded == "" {
		return values
	}
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return map[string]string{}
	}
	return values
}

func splitLines(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, "\n")
}

// orEmpty keeps a JSON array out of being null, which the UI would have to guard
// every read against.
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
