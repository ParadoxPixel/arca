package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"

	"arca/internal/auth"
	"arca/internal/store/models"
)

type statusError struct {
	status  int
	message string
}

func (e statusError) Error() string { return e.message }

func fail(status int, format string, arguments ...any) error {
	return statusError{status: status, message: fmt.Sprintf(format, arguments...)}
}

type handler func(http.ResponseWriter, *http.Request) error

func (h handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	err := h(writer, request)
	if err == nil {
		return
	}

	var known statusError
	if !errors.As(err, &known) {
		log.Errorf("%s %s: %v", request.Method, request.URL.Path, err)
		known = statusError{status: http.StatusInternalServerError, message: "Internal server error"}
	}

	writeJSON(writer, known.status, map[string]string{"error": known.message})
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)

	if payload == nil {
		return
	}
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		log.Errorf("writing a JSON response failed: %v", err)
	}
}

func noContent(writer http.ResponseWriter) error {
	writer.WriteHeader(http.StatusNoContent)
	return nil
}

// decodeBody reads a JSON request. Two checks come first, and together they are
// what keeps a page on another site from driving this API with the session
// cookie a browser would attach. The body has to be declared as JSON, which a
// form cannot do without a preflight this server never answers; and a browser
// that names its Origin has to name this server, which shuts out a page on a
// sibling subdomain that the cookie's SameSite rule would otherwise let in.
// Clients that send no Origin, such as build tools with an API token, are
// unaffected.
func (s *Server) decodeBody(request *http.Request, target any) error {
	if err := s.checkOrigin(request); err != nil {
		return err
	}
	if !isJSONContentType(request.Header.Get("Content-Type")) {
		return fail(http.StatusBadRequest, "Content-Type must be application/json")
	}

	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		return fail(http.StatusBadRequest, "Invalid JSON")
	}
	return nil
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}

func (s *Server) checkOrigin(request *http.Request) error {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return nil
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return fail(http.StatusForbidden, "Cross-origin requests are not accepted")
	}
	for _, host := range s.publicHosts(request) {
		if strings.EqualFold(parsed.Host, host) {
			return nil
		}
	}
	return fail(http.StatusForbidden, "Cross-origin requests are not accepted")
}

func currentUser(request *http.Request) *models.User {
	return auth.UserFrom(request.Context())
}

func requireUser(request *http.Request) (*models.User, error) {
	user := currentUser(request)
	if user == nil {
		return nil, fail(http.StatusUnauthorized, "Authentication required")
	}
	return user, nil
}

func requireAdmin(request *http.Request) (*models.User, error) {
	user, err := requireUser(request)
	if err != nil {
		return nil, err
	}
	if !user.IsAdmin() {
		return nil, fail(http.StatusForbidden, "Administrator access required")
	}
	return user, nil
}

func requireText(value, field string, maximum int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fail(http.StatusBadRequest, "%s is required", field)
	}
	if len(trimmed) > maximum {
		return "", fail(http.StatusBadRequest, "%s is too long", field)
	}
	return trimmed, nil
}

func requireOneOf(value string, allowed []string, field string) (string, error) {
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", fail(http.StatusBadRequest, "%s must be one of: %s", field, strings.Join(allowed, ", "))
}
