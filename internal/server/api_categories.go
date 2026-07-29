package server

import (
	"net/http"
	"strings"

	"arca/internal/store/models"
)

type categoryResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Position     int64  `json:"position"`
	Repositories int64  `json:"repositories"`
	CreatedAt    int64  `json:"createdAt"`
}

func serializeCategory(category models.Category, repositories int64) categoryResponse {
	return categoryResponse{
		ID:           category.ID,
		Name:         category.Name,
		Description:  category.Description,
		Position:     category.Position,
		Repositories: repositories,
		CreatedAt:    category.CreatedAt,
	}
}

// routeListCategories counts only the repositories the caller can see, so the
// listing never hints at private repositories through its totals.
func (s *Server) routeListCategories(writer http.ResponseWriter, request *http.Request) error {
	categories, err := s.store.ListCategories()
	if err != nil {
		return err
	}

	repositories, err := s.store.VisibleRepositories(currentUser(request))
	if err != nil {
		return err
	}

	counts := map[string]int64{}
	for _, repository := range repositories {
		if repository.CategoryID != nil {
			counts[*repository.CategoryID]++
		}
	}

	responses := make([]categoryResponse, 0, len(categories))
	for _, category := range categories {
		responses = append(responses, serializeCategory(category, counts[category.ID]))
	}

	writeJSON(writer, http.StatusOK, map[string]any{"categories": responses})
	return nil
}

func (s *Server) routeCreateCategory(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeBody(request, &body); err != nil {
		return err
	}

	name, err := requireText(body.Name, "name", 64)
	if err != nil {
		return err
	}
	if len(body.Description) > 500 {
		return fail(http.StatusBadRequest, "description is too long")
	}
	if _, err := s.store.CategoryByName(name); err == nil {
		return fail(http.StatusConflict, "A category with that name already exists")
	}

	category := &models.Category{Name: name, Description: strings.TrimSpace(body.Description)}
	if err := s.store.CreateCategory(category); err != nil {
		return err
	}

	writeJSON(writer, http.StatusCreated, serializeCategory(*category, 0))
	return nil
}

func (s *Server) routeUpdateCategory(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	category, err := s.store.CategoryByID(routeVar(request, "id"))
	if err != nil {
		return fail(http.StatusNotFound, "Category not found")
	}

	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := decodeBody(request, &body); err != nil {
		return err
	}

	changes := map[string]any{}
	if body.Name != nil {
		name, err := requireText(*body.Name, "name", 64)
		if err != nil {
			return err
		}
		if existing, err := s.store.CategoryByName(name); err == nil && existing.ID != category.ID {
			return fail(http.StatusConflict, "A category with that name already exists")
		}
		changes["name"] = name
	}
	if body.Description != nil {
		if len(*body.Description) > 500 {
			return fail(http.StatusBadRequest, "description is too long")
		}
		changes["description"] = strings.TrimSpace(*body.Description)
	}
	if len(changes) == 0 {
		return fail(http.StatusBadRequest, "No changes supplied")
	}

	if err := s.store.UpdateCategory(category.ID, changes); err != nil {
		return err
	}

	updated, err := s.store.CategoryByID(category.ID)
	if err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, serializeCategory(*updated, 0))
	return nil
}

// routeDeleteCategory leaves the repositories in place and uncategorized.
func (s *Server) routeDeleteCategory(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	category, err := s.store.CategoryByID(routeVar(request, "id"))
	if err != nil {
		return fail(http.StatusNotFound, "Category not found")
	}
	if err := s.store.DeleteCategory(category.ID); err != nil {
		return err
	}

	return noContent(writer)
}

func (s *Server) routeReorderCategories(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	var body struct {
		IDs *[]string `json:"ids"`
	}
	if err := decodeBody(request, &body); err != nil {
		return err
	}
	if body.IDs == nil {
		return fail(http.StatusBadRequest, "ids must be an array")
	}

	for _, id := range *body.IDs {
		if _, err := s.store.CategoryByID(id); err != nil {
			return fail(http.StatusBadRequest, "ids contains an unknown category")
		}
	}
	if err := s.store.ReorderCategories(*body.IDs); err != nil {
		return err
	}

	return noContent(writer)
}

func (s *Server) resolveCategory(id string) (*string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return nil, nil
	}
	if _, err := s.store.CategoryByID(trimmed); err != nil {
		return nil, fail(http.StatusBadRequest, "categoryId does not match a category")
	}
	return &trimmed, nil
}

func (s *Server) categoryNameFor(id *string) string {
	if id == nil {
		return ""
	}
	category, err := s.store.CategoryByID(*id)
	if err != nil {
		return ""
	}
	return category.Name
}
