package server

import (
	"net/http"

	"arca/internal/auth"
	"arca/internal/store"
	"arca/internal/store/models"
)

type tokenResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Preview    string `json:"preview"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
	ExpiresAt  *int64 `json:"expires_at"`
}

const millisecondsPerDay = 24 * 60 * 60 * 1000

func (s *Server) routeListTokens(writer http.ResponseWriter, request *http.Request) error {
	user, err := requireUser(request)
	if err != nil {
		return err
	}

	tokens, err := s.store.ListTokens(user.ID)
	if err != nil {
		return err
	}

	responses := make([]tokenResponse, 0, len(tokens))
	for _, token := range tokens {
		responses = append(responses, tokenResponse{
			ID:         token.ID,
			Name:       token.Name,
			Preview:    token.Preview,
			CreatedAt:  token.CreatedAt,
			LastUsedAt: token.LastUsedAt,
			ExpiresAt:  token.ExpiresAt,
		})
	}

	writeJSON(writer, http.StatusOK, map[string]any{"tokens": responses})
	return nil
}

func (s *Server) routeCreateToken(writer http.ResponseWriter, request *http.Request) error {
	user, err := requireUser(request)
	if err != nil {
		return err
	}

	var body struct {
		Name          string  `json:"name"`
		ExpiresInDays float64 `json:"expiresInDays"`
	}
	if err := s.decodeBody(request, &body); err != nil {
		return err
	}

	name, err := requireText(body.Name, "name", 80)
	if err != nil {
		return err
	}

	secret, err := auth.GenerateAPIToken()
	if err != nil {
		return err
	}

	var expiresAt *int64
	if body.ExpiresInDays > 0 {
		expiry := store.NowMillis() + int64(body.ExpiresInDays)*millisecondsPerDay
		expiresAt = &expiry
	}

	token := &models.APIToken{
		UserID:    user.ID,
		Name:      name,
		TokenHash: auth.SHA256Hex(secret),
		Preview:   secret[:12],
		ExpiresAt: expiresAt,
	}
	if err := s.store.CreateToken(token); err != nil {
		return err
	}

	writeJSON(writer, http.StatusCreated, map[string]any{
		"id":        token.ID,
		"name":      token.Name,
		"token":     secret,
		"username":  user.Email,
		"expiresAt": expiresAt,
	})
	return nil
}

func (s *Server) routeDeleteToken(writer http.ResponseWriter, request *http.Request) error {
	user, err := requireUser(request)
	if err != nil {
		return err
	}

	deleted, err := s.store.DeleteToken(routeVar(request, "id"), user.ID)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return fail(http.StatusNotFound, "Token not found")
	}

	return noContent(writer)
}
