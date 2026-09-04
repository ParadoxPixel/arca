package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/charmbracelet/log"

	"arca/internal/auth"
	"arca/internal/npm"
	"arca/internal/store/models"
)

const defaultSearchSize = 20

// serveNPMSearch backs "npm search". Results are scoped to the repository the
// client is pointed at, since that is the only thing its registry URL names.
func (s *Server) serveNPMSearch(writer http.ResponseWriter, request *http.Request, repository *models.Repository) {
	query := request.URL.Query()

	size := defaultSearchSize
	if parsed, err := strconv.Atoi(query.Get("size")); err == nil && parsed > 0 {
		size = min(parsed, 250)
	}

	records, err := s.store.SearchComponents([]models.Repository{*repository}, query.Get("text"), size*10)
	if err != nil {
		log.Errorf("searching a registry failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	summaries := limitSummaries(groupComponents(records), size)
	base := s.publicBase(request) + "/repository/" + repository.Name + "/"

	objects := make([]map[string]any, 0, len(summaries))
	for _, summary := range summaries {
		name := npm.PackageName(summary.Namespace, summary.Name)

		version := ""
		if summary.Latest != nil {
			version = *summary.Latest
		}

		route := npm.Route{Namespace: summary.Namespace, Name: summary.Name}
		description, keywords := s.describePackage(repository, route, version)

		objects = append(objects, map[string]any{
			// maintainers and keywords are read unconditionally by the npm CLI's
			// result formatter, so they are always arrays rather than omitted.
			"package": map[string]any{
				"name":        name,
				"version":     version,
				"description": description,
				"keywords":    keywords,
				"maintainers": []any{},
				"date":        time.UnixMilli(summary.UpdatedAt).UTC().Format(time.RFC3339),
				"links":       map[string]string{"npm": base + name},
			},
			// npm renders these columns unconditionally, so they are reported as
			// zero rather than omitted. Nothing here scores packages.
			"score": map[string]any{
				"final":  0,
				"detail": map[string]any{"quality": 0, "popularity": 0, "maintenance": 0},
			},
			"searchScore": 0,
		})
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"objects": objects,
		"total":   len(objects),
		"time":    time.Now().UTC().Format(time.RFC1123),
	})
}

// describePackage reads the blurb a search result carries out of the manifest
// the version was published with. Missing fields are normal: nothing forces a
// package.json to carry a description.
func (s *Server) describePackage(repository *models.Repository, route npm.Route, version string) (string, []string) {
	keywords := []string{}
	if version == "" {
		return "", keywords
	}

	manifest, err := s.readManifest(repository, route, version)
	if err != nil {
		return "", keywords
	}

	var description string
	if raw, ok := manifest["description"]; ok {
		_ = json.Unmarshal(raw, &description)
	}
	if raw, ok := manifest["keywords"]; ok {
		_ = json.Unmarshal(raw, &keywords)
	}
	return description, keywords
}

func (s *Server) serveNPMTokens(writer http.ResponseWriter, request *http.Request) {
	user := currentUser(request)
	if user == nil {
		npmError(writer, http.StatusUnauthorized, "You must be logged in to do that")
		return
	}

	tokens, err := s.store.ListTokens(user.ID)
	if err != nil {
		log.Errorf("listing npm tokens failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	objects := make([]map[string]any, 0, len(tokens))
	for _, token := range tokens {
		objects = append(objects, map[string]any{
			"key":            token.ID,
			"token":          token.Preview,
			"cidr_whitelist": nil,
			"readonly":       false,
			"automation":     false,
			"created":        time.UnixMilli(token.CreatedAt).UTC().Format(time.RFC3339),
			"updated":        time.UnixMilli(token.CreatedAt).UTC().Format(time.RFC3339),
		})
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"objects": objects,
		"total":   len(objects),
		"urls":    map[string]any{},
	})
}

// serveNPMMakeToken backs "npm token create". npm asks for the account password
// again, which is verified rather than ignored so a hijacked session cannot mint
// a long-lived credential.
func (s *Server) serveNPMMakeToken(writer http.ResponseWriter, request *http.Request) {
	user := currentUser(request)
	if user == nil {
		npmError(writer, http.StatusUnauthorized, "You must be logged in to do that")
		return
	}

	var body struct {
		Password string `json:"password"`
		Readonly bool   `json:"readonly"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 8<<10)).Decode(&body); err != nil {
		npmError(writer, http.StatusBadRequest, "Invalid token request")
		return
	}

	if !auth.VerifyPassword(body.Password, user.PasswordHash) {
		log.Warnf("rejected an npm token request for %q", user.Email)
		npmError(writer, http.StatusUnauthorized, "Incorrect password")
		return
	}

	secret, err := auth.GenerateAPIToken()
	if err != nil {
		log.Errorf("generating an npm token failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	token := &models.APIToken{
		UserID:    user.ID,
		Name:      "npm token (" + time.Now().UTC().Format(time.DateOnly) + ")",
		TokenHash: auth.SHA256Hex(secret),
		Preview:   secret[:12],
	}
	if err := s.store.CreateToken(token); err != nil {
		log.Errorf("creating an npm token failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeJSON(writer, http.StatusCreated, map[string]any{
		"key":            token.ID,
		"token":          secret,
		"cidr_whitelist": nil,
		"readonly":       false,
		"created":        time.UnixMilli(token.CreatedAt).UTC().Format(time.RFC3339),
	})
}

func (s *Server) serveNPMDropToken(writer http.ResponseWriter, request *http.Request, route npm.Route) {
	user := currentUser(request)
	if user == nil {
		npmError(writer, http.StatusUnauthorized, "You must be logged in to do that")
		return
	}

	removed, err := s.store.DeleteToken(route.Token, user.ID)
	if err != nil {
		log.Errorf("revoking an npm token failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}
	if removed == 0 {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}
