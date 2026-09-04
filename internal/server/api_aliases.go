package server

import (
	"net/http"
	"strings"

	"arca/internal/store"
)

type aliasResponse struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Server) routeListAliases(writer http.ResponseWriter, request *http.Request) error {
	repository, err := s.loadAdministered(request)
	if err != nil {
		return err
	}

	aliases, err := s.store.Aliases(repository.ID)
	if err != nil {
		return err
	}

	responses := make([]aliasResponse, 0, len(aliases))
	for _, alias := range aliases {
		responses = append(responses, aliasResponse{Name: alias.Name, CreatedAt: alias.CreatedAt})
	}

	writeJSON(writer, http.StatusOK, map[string]any{"aliases": responses})
	return nil
}

// routeCreateAlias adds a redirect by hand, which is how a name that was never
// this repository's own is pointed at it: a rename in some other tool, or a
// repository that was deleted and recreated under a different name.
func (s *Server) routeCreateAlias(writer http.ResponseWriter, request *http.Request) error {
	repository, err := s.loadAdministered(request)
	if err != nil {
		return err
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := s.decodeBody(request, &body); err != nil {
		return err
	}

	name, err := requireText(body.Name, "name", 64)
	if err != nil {
		return err
	}
	name = strings.ToLower(name)

	if !store.IsValidRepositoryName(name) {
		return fail(http.StatusBadRequest, "name must be lowercase alphanumeric with dots, dashes or underscores")
	}
	if name == repository.Name {
		return fail(http.StatusBadRequest, "a repository cannot redirect to itself")
	}
	// A live repository owns its name outright. Letting an alias shadow it would
	// make which one answers depend on lookup order.
	if _, err := s.store.RepositoryByName(name); err == nil {
		return fail(http.StatusConflict, "%q is the name of a repository", name)
	}

	if err := s.store.CreateAlias(name, repository.ID); err != nil {
		return err
	}

	writeJSON(writer, http.StatusCreated, aliasResponse{Name: name, CreatedAt: store.NowMillis()})
	return nil
}

func (s *Server) routeDeleteAlias(writer http.ResponseWriter, request *http.Request) error {
	repository, err := s.loadAdministered(request)
	if err != nil {
		return err
	}

	deleted, err := s.store.DeleteAlias(routeVar(request, "alias"), repository.ID)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return fail(http.StatusNotFound, "Alias not found")
	}

	return noContent(writer)
}
