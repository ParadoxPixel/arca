package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type categoryPayload struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Position     int64  `json:"position"`
	Repositories int64  `json:"repositories"`
}

func (i *testInstance) createCategory(name string) categoryPayload {
	i.t.Helper()

	body := expectStatus(i.t, i.api(http.MethodPost, "/api/categories", fmt.Sprintf(`{"name":%q}`, name)), http.StatusCreated)

	var category categoryPayload
	if err := json.Unmarshal([]byte(body), &category); err != nil {
		i.t.Fatalf("decoding the category: %v", err)
	}
	return category
}

func (i *testInstance) listCategories() []categoryPayload {
	i.t.Helper()

	body := expectStatus(i.t, i.api(http.MethodGet, "/api/categories", ""), http.StatusOK)

	var payload struct {
		Categories []categoryPayload `json:"categories"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		i.t.Fatalf("decoding categories: %v", err)
	}
	return payload.Categories
}

func (i *testInstance) repositoryCategory(name string) (string, string) {
	i.t.Helper()

	body := expectStatus(i.t, i.api(http.MethodGet, "/api/repositories/"+name, ""), http.StatusOK)

	var payload struct {
		CategoryID   *string `json:"categoryId"`
		CategoryName string  `json:"categoryName"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		i.t.Fatalf("decoding the repository: %v", err)
	}
	if payload.CategoryID == nil {
		return "", payload.CategoryName
	}
	return *payload.CategoryID, payload.CategoryName
}

func TestSeededRepositoriesStartUncategorized(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	for _, name := range []string{"maven-releases", "maven-snapshots"} {
		t.Run(name, func(t *testing.T) {
			id, categoryName := instance.repositoryCategory(name)
			if id != "" || categoryName != "" {
				t.Fatalf("category = %q/%q, want an unsorted repository", id, categoryName)
			}
		})
	}

	if categories := instance.listCategories(); len(categories) != 0 {
		t.Fatalf("categories = %d, want none on a fresh instance", len(categories))
	}
}

func TestCategoryAssignmentLeavesTheMavenURLAlone(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	const artifact = "/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar"
	expectStatus(t, instance.maven(http.MethodPut, artifact, "payload", true), http.StatusCreated)

	category := instance.createCategory("Internal")
	expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/maven-releases",
		fmt.Sprintf(`{"categoryId":%q}`, category.ID)), http.StatusOK)

	if _, name := instance.repositoryCategory("maven-releases"); name != "Internal" {
		t.Fatalf("category name = %q, want Internal", name)
	}

	cases := []struct {
		name string
		path string
		want int
	}{
		{"artifact", artifact, http.StatusOK},
		{"metadata", "/repository/maven-releases/com/example/app/maven-metadata.xml", http.StatusOK},
		{"directory", "/repository/maven-releases/com/example/app/1.0.0/", http.StatusOK},
		{"category is not a path segment", "/repository/internal/maven-releases/com/example/app/1.0.0/app-1.0.0.jar", http.StatusNotFound},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.maven(http.MethodGet, testCase.path, "", true), testCase.want)
		})
	}
}

func TestDeletingACategoryKeepsItsRepositories(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	category := instance.createCategory("Mirrors")
	expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/maven-releases",
		fmt.Sprintf(`{"categoryId":%q}`, category.ID)), http.StatusOK)

	expectStatus(t, instance.api(http.MethodDelete, "/api/categories/"+category.ID, ""), http.StatusNoContent)

	if id, name := instance.repositoryCategory("maven-releases"); id != "" || name != "" {
		t.Fatalf("category = %q/%q, want the repository released", id, name)
	}

	body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories", ""), http.StatusOK)
	if !strings.Contains(body, `"name":"maven-releases"`) {
		t.Fatalf("the repository disappeared with its category: %s", body)
	}
}

func TestCategoryAssignmentCanBeCleared(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	category := instance.createCategory("Internal")

	cases := []struct {
		name     string
		body     string
		wantName string
	}{
		{"assign", fmt.Sprintf(`{"categoryId":%q}`, category.ID), "Internal"},
		{"clear", `{"categoryId":""}`, ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/maven-releases", testCase.body), http.StatusOK)

			if _, name := instance.repositoryCategory("maven-releases"); name != testCase.wantName {
				t.Fatalf("category name = %q, want %q", name, testCase.wantName)
			}
		})
	}
}

func TestCategoriesAreOrderedAndReorderable(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	first := instance.createCategory("Alpha")
	second := instance.createCategory("Beta")
	third := instance.createCategory("Gamma")

	names := func() []string {
		ordered := []string{}
		for _, category := range instance.listCategories() {
			ordered = append(ordered, category.Name)
		}
		return ordered
	}

	if got := names(); strings.Join(got, ",") != "Alpha,Beta,Gamma" {
		t.Fatalf("creation order = %v, want Alpha,Beta,Gamma", got)
	}

	expectStatus(t, instance.api(http.MethodPut, "/api/categories/order",
		fmt.Sprintf(`{"ids":[%q,%q,%q]}`, third.ID, first.ID, second.ID)), http.StatusNoContent)

	if got := names(); strings.Join(got, ",") != "Gamma,Alpha,Beta" {
		t.Fatalf("order after reordering = %v, want Gamma,Alpha,Beta", got)
	}
}

func TestCategoryCountsOnlyIncludeVisibleRepositories(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	category := instance.createCategory("Internal")
	expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/maven-releases",
		fmt.Sprintf(`{"categoryId":%q,"visibility":"private"}`, category.ID)), http.StatusOK)

	if categories := instance.listCategories(); categories[0].Repositories != 1 {
		t.Fatalf("admin count = %d, want 1", categories[0].Repositories)
	}

	body := expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, "/api/categories", "")), http.StatusOK)
	if !strings.Contains(body, `"repositories":0`) {
		t.Fatalf("anonymous count leaked a private repository: %s", body)
	}
}

func TestCategoryValidation(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	existing := instance.createCategory("Internal")
	instance.createCategory("Mirrors")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"missing name", http.MethodPost, "/api/categories", `{}`, http.StatusBadRequest},
		{"duplicate name", http.MethodPost, "/api/categories", `{"name":"Internal"}`, http.StatusConflict},
		{"rename onto a duplicate", http.MethodPatch, "/api/categories/" + existing.ID, `{"name":"Mirrors"}`, http.StatusConflict},
		{"rename to itself", http.MethodPatch, "/api/categories/" + existing.ID, `{"name":"Internal"}`, http.StatusOK},
		{"unknown category", http.MethodPatch, "/api/categories/nope", `{"name":"x"}`, http.StatusNotFound},
		{"unknown category on a repository", http.MethodPatch, "/api/repositories/maven-releases", `{"categoryId":"nope"}`, http.StatusBadRequest},
		{"reorder with an unknown id", http.MethodPut, "/api/categories/order", `{"ids":["nope"]}`, http.StatusBadRequest},
		{"reorder without ids", http.MethodPut, "/api/categories/order", `{}`, http.StatusBadRequest},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.api(testCase.method, testCase.path, testCase.body), testCase.want)
		})
	}
}

func TestCategoryWritesRequireAdministrator(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	category := instance.createCategory("Internal")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, "/api/categories", `{"name":"Sneaky"}`},
		{"update", http.MethodPatch, "/api/categories/" + category.ID, `{"name":"Sneaky"}`},
		{"delete", http.MethodDelete, "/api/categories/" + category.ID, ""},
		{"reorder", http.MethodPut, "/api/categories/order", `{"ids":[]}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := instance.doAnonymously(instance.request(testCase.method, testCase.path, testCase.body))
			expectStatus(t, response, http.StatusUnauthorized)
		})
	}
}
