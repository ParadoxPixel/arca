package server

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"arca/internal/format"
	"arca/internal/link"
	"arca/internal/store"
	"arca/internal/store/models"
)

// filenamePattern guards the name a link offers a file under. It ends up in a
// Content-Disposition header, so a separator or a quote in it has no business
// being stored in the first place.
var filenamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,119}$`)

var matchTypes = []string{link.MatchTemplate, link.MatchRegex}

type resolvedResponse struct {
	Version   string `json:"version"`
	Filename  string `json:"filename"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	UpdatedAt int64  `json:"updatedAt"`
}

type linkResponse struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	URL         string `json:"url"`
	Repository  string `json:"repository"`
	Description string `json:"description"`
	Namespace   string `json:"namespace"`
	Artifact    string `json:"artifact"`
	MatchType   string `json:"matchType"`
	Pattern     string `json:"pattern"`
	Policy      string `json:"policy"`
	Filename    string `json:"filename"`
	Disabled    bool   `json:"disabled"`
	Downloads   int64  `json:"downloads"`
	LastUsedAt  int64  `json:"lastUsedAt"`
	CreatedAt   int64  `json:"createdAt"`
	// Resolved is what the link points at as of this request, or null when
	// nothing published matches it yet.
	Resolved *resolvedResponse `json:"resolved"`
}

// downloadPath is where a slug is served from. It is short on purpose: the URL
// is meant to be pasted into a build script or an installer.
func downloadPath(slug string) string { return "/download/" + slug }

// linkable rejects the two shapes a link cannot mean. A group holds no
// artifacts of its own, and a docker repository holds blobs and manifests that
// are only meaningful to a registry client, not files to hand out one at a time.
func linkable(repository *models.Repository) error {
	if repository.IsGroup() {
		return fail(http.StatusBadRequest, "A group repository holds no artifacts of its own to link to")
	}
	if repository.Format == format.Docker {
		return fail(http.StatusBadRequest, "Docker images are pulled by a registry client, so there is no single file to link to")
	}
	return nil
}

func (s *Server) serializeLink(request *http.Request, repository *models.Repository, deployment models.DeploymentLink) linkResponse {
	response := linkResponse{
		ID:          deployment.ID,
		Slug:        deployment.Slug,
		URL:         s.publicBase(request) + downloadPath(deployment.Slug),
		Repository:  repository.Name,
		Description: deployment.Description,
		Namespace:   deployment.Namespace,
		Artifact:    deployment.Artifact,
		MatchType:   deployment.MatchType,
		Pattern:     deployment.Pattern,
		Policy:      deployment.Policy,
		Filename:    deployment.Filename,
		Disabled:    deployment.Disabled,
		Downloads:   deployment.Downloads,
		LastUsedAt:  deployment.LastUsedAt,
		CreatedAt:   deployment.CreatedAt,
	}

	// A link that cannot be resolved is still a link. Reporting the failure as
	// "nothing matches yet" keeps a broken pattern from failing the listing that
	// is the only place it can be fixed from.
	resolved, err := s.resolveLink(repository, &deployment)
	if err != nil || resolved == nil {
		return response
	}

	response.Resolved = &resolvedResponse{
		Version:   resolved.version,
		Filename:  resolved.filename(deployment.Filename),
		Path:      resolved.asset.Path,
		Size:      resolved.asset.Size,
		UpdatedAt: resolved.asset.UpdatedAt,
	}
	return response
}

// routeListLinks needs read access rather than admin. The URLs it lists are
// public by design, so anyone who can see the repository can see them.
func (s *Server) routeListLinks(writer http.ResponseWriter, request *http.Request) error {
	repository, _, err := s.loadAccessible(request, models.PermissionRead)
	if err != nil {
		return err
	}

	links, err := s.store.ListLinks(repository.ID)
	if err != nil {
		return err
	}

	responses := make([]linkResponse, 0, len(links))
	for _, deployment := range links {
		responses = append(responses, s.serializeLink(request, repository, deployment))
	}

	writeJSON(writer, http.StatusOK, map[string]any{"links": responses})
	return nil
}

type linkBody struct {
	Slug        *string `json:"slug"`
	Description *string `json:"description"`
	Namespace   *string `json:"namespace"`
	Artifact    *string `json:"artifact"`
	MatchType   *string `json:"matchType"`
	Pattern     *string `json:"pattern"`
	Policy      *string `json:"policy"`
	Filename    *string `json:"filename"`
	Disabled    *bool   `json:"disabled"`
}

func (s *Server) routeCreateLink(writer http.ResponseWriter, request *http.Request) error {
	repository, err := s.loadAdministered(request)
	if err != nil {
		return err
	}
	if err := linkable(repository); err != nil {
		return err
	}

	var body linkBody
	if err := s.decodeBody(request, &body); err != nil {
		return err
	}

	artifact, err := requireText(orBlank(body.Artifact), "artifact", 200)
	if err != nil {
		return err
	}

	matchType, err := requireOneOf(orDefault(orBlank(body.MatchType), link.MatchTemplate), matchTypes, "matchType")
	if err != nil {
		return err
	}
	pattern := strings.TrimSpace(orBlank(body.Pattern))
	if err := link.Validate(matchType, pattern); err != nil {
		return fail(http.StatusBadRequest, "%s", err.Error())
	}

	policy, err := requireOneOf(orDefault(orBlank(body.Policy), models.PolicyRelease), policies, "policy")
	if err != nil {
		return err
	}

	namespace, err := requireNamespace(orBlank(body.Namespace))
	if err != nil {
		return err
	}
	filename, err := requireFilename(orBlank(body.Filename))
	if err != nil {
		return err
	}
	description, err := requireDescription(orBlank(body.Description))
	if err != nil {
		return err
	}

	slug, err := s.resolveSlug(orBlank(body.Slug))
	if err != nil {
		return err
	}

	deployment := &models.DeploymentLink{
		RepositoryID: repository.ID,
		Slug:         slug,
		Description:  description,
		Namespace:    namespace,
		Artifact:     artifact,
		MatchType:    matchType,
		Pattern:      pattern,
		Policy:       policy,
		Filename:     filename,
		Disabled:     body.Disabled != nil && *body.Disabled,
		CreatedBy:    userIDFrom(request),
	}
	if err := s.store.CreateLink(deployment); err != nil {
		return err
	}

	writeJSON(writer, http.StatusCreated, s.serializeLink(request, repository, *deployment))
	return nil
}

func (s *Server) routeUpdateLink(writer http.ResponseWriter, request *http.Request) error {
	repository, err := s.loadAdministered(request)
	if err != nil {
		return err
	}

	deployment, err := s.store.LinkByID(routeVar(request, "id"), repository.ID)
	if err != nil {
		return fail(http.StatusNotFound, "Link not found")
	}

	var body linkBody
	if err := s.decodeBody(request, &body); err != nil {
		return err
	}

	changes := map[string]any{}

	// The pattern and how to read it are validated together, because changing
	// either one alone can still leave a pattern that does not compile.
	matchType, pattern := deployment.MatchType, deployment.Pattern
	if body.MatchType != nil {
		matchType, err = requireOneOf(*body.MatchType, matchTypes, "matchType")
		if err != nil {
			return err
		}
	}
	if body.Pattern != nil {
		pattern = strings.TrimSpace(*body.Pattern)
	}
	if matchType != deployment.MatchType || pattern != deployment.Pattern {
		if err := link.Validate(matchType, pattern); err != nil {
			return fail(http.StatusBadRequest, "%s", err.Error())
		}
		changes["match_type"] = matchType
		changes["pattern"] = pattern
	}

	if body.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*body.Slug))
		if slug != deployment.Slug {
			resolved, err := s.resolveSlug(slug)
			if err != nil {
				return err
			}
			changes["slug"] = resolved
		}
	}
	if body.Artifact != nil {
		artifact, err := requireText(*body.Artifact, "artifact", 200)
		if err != nil {
			return err
		}
		changes["artifact"] = artifact
	}
	if body.Namespace != nil {
		namespace, err := requireNamespace(*body.Namespace)
		if err != nil {
			return err
		}
		changes["namespace"] = namespace
	}
	if body.Policy != nil {
		policy, err := requireOneOf(*body.Policy, policies, "policy")
		if err != nil {
			return err
		}
		changes["policy"] = policy
	}
	if body.Filename != nil {
		filename, err := requireFilename(*body.Filename)
		if err != nil {
			return err
		}
		changes["filename"] = filename
	}
	if body.Description != nil {
		description, err := requireDescription(*body.Description)
		if err != nil {
			return err
		}
		changes["description"] = description
	}
	if body.Disabled != nil {
		changes["disabled"] = *body.Disabled
	}

	if len(changes) == 0 {
		return fail(http.StatusBadRequest, "No changes supplied")
	}
	if err := s.store.UpdateLink(deployment.ID, changes); err != nil {
		return err
	}

	updated, err := s.store.LinkByID(deployment.ID, repository.ID)
	if err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, s.serializeLink(request, repository, *updated))
	return nil
}

func (s *Server) routeDeleteLink(writer http.ResponseWriter, request *http.Request) error {
	repository, err := s.loadAdministered(request)
	if err != nil {
		return err
	}

	deleted, err := s.store.DeleteLink(routeVar(request, "id"), repository.ID)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return fail(http.StatusNotFound, "Link not found")
	}

	return noContent(writer)
}

// resolveSlug takes the slug the caller asked for, or mints a random one when
// they left it out. A link is served without authentication, so the slug is
// the only thing standing between the public and a file of a private
// repository: a derived one such as "<artifact>-latest" would be guessable by
// anyone who knows what the repository holds. Slugs are unique across the
// instance because the download URL carries no repository name to
// disambiguate them.
func (s *Server) resolveSlug(requested string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(requested))
	if slug == "" {
		return s.randomSlug()
	}
	if !store.IsValidSlug(slug) {
		return "", fail(http.StatusBadRequest, "slug must be lowercase alphanumeric with dots, dashes or underscores")
	}
	if _, err := s.store.LinkBySlug(slug); err == nil {
		return "", fail(http.StatusConflict, "A link with that slug already exists")
	}
	return slug, nil
}

// randomSlugLength is in base32 characters, which is 80 bits of entropy: more
// than a session token needs to be unguessable and short enough to paste.
const randomSlugLength = 16

func (s *Server) randomSlug() (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		buffer := make([]byte, randomSlugLength)
		if _, err := rand.Read(buffer); err != nil {
			return "", err
		}
		slug := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buffer))[:randomSlugLength]
		if _, err := s.store.LinkBySlug(slug); err != nil {
			return slug, nil
		}
	}
	return "", errors.New("server: could not find an unused slug")
}

func requireNamespace(raw string) (string, error) {
	namespace := strings.TrimSpace(raw)
	if len(namespace) > 300 {
		return "", fail(http.StatusBadRequest, "namespace is too long")
	}
	return namespace, nil
}

func requireFilename(raw string) (string, error) {
	filename := strings.TrimSpace(raw)
	if filename == "" {
		return "", nil
	}
	if !filenamePattern.MatchString(filename) {
		return "", fail(http.StatusBadRequest, "filename must be a plain filename, without a path")
	}
	return filename, nil
}

func requireDescription(raw string) (string, error) {
	description := strings.TrimSpace(raw)
	if len(description) > 500 {
		return "", fail(http.StatusBadRequest, "description is too long")
	}
	return description, nil
}

func orBlank(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
