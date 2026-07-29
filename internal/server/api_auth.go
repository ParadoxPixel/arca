package server

import (
	"net/http"
	"regexp"
	"strings"

	"arca/internal/auth"
	"arca/internal/format"
	"arca/internal/store"
	"arca/internal/store/models"
	"arca/internal/version"
)

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

const minimumPasswordLength = 10

type identity struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

func identityOf(user *models.User) *identity {
	if user == nil {
		return nil
	}
	return &identity{ID: user.ID, Email: user.Email, Name: user.Name, Role: user.Role}
}

func validateEmail(value string) (string, error) {
	email, err := requireText(value, "email", 254)
	if err != nil {
		return "", err
	}
	email = strings.ToLower(email)
	if !emailPattern.MatchString(email) {
		return "", fail(http.StatusBadRequest, "email is not a valid address")
	}
	return email, nil
}

func validatePassword(value string) (string, error) {
	password, err := requireText(value, "password", 200)
	if err != nil {
		return "", err
	}
	if len(password) < minimumPasswordLength {
		return "", fail(http.StatusBadRequest, "password must be at least %d characters", minimumPasswordLength)
	}
	return password, nil
}

func (s *Server) routeBootstrap(writer http.ResponseWriter, request *http.Request) error {
	initialized, err := s.store.IsInitialized()
	if err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"initialized":  initialized,
		"instanceName": s.store.InstanceName(),
		"themeColor":   s.store.ThemeColor(),
		"build":        version.Current(),
		"user":         identityOf(currentUser(request)),
	})
	return nil
}

var seedRepositories = []struct {
	name        string
	format      string
	policy      string
	description string
}{
	{"maven-releases", format.Maven2, models.PolicyRelease, "Release artifacts"},
	{"maven-snapshots", format.Maven2, models.PolicySnapshot, "Snapshot artifacts"},
	{"npm-hosted", format.NPM, models.PolicyMixed, "npm packages"},
}

func (s *Server) routeSetup(writer http.ResponseWriter, request *http.Request) error {
	initialized, err := s.store.IsInitialized()
	if err != nil {
		return err
	}
	if initialized {
		return fail(http.StatusConflict, "This instance is already initialized")
	}

	var body struct {
		InstanceName     string `json:"instanceName"`
		ThemeColor       string `json:"themeColor"`
		Name             string `json:"name"`
		Email            string `json:"email"`
		Password         string `json:"password"`
		SeedRepositories *bool  `json:"seedRepositories"`
		PublicByDefault  bool   `json:"publicByDefault"`
	}
	if err := decodeBody(request, &body); err != nil {
		return err
	}

	instanceName, err := requireText(body.InstanceName, "instanceName", 80)
	if err != nil {
		return err
	}
	themeColor, err := validateThemeColor(orDefault(body.ThemeColor, store.DefaultThemeColor))
	if err != nil {
		return err
	}
	name, err := requireText(body.Name, "name", 120)
	if err != nil {
		return err
	}
	email, err := validateEmail(body.Email)
	if err != nil {
		return err
	}
	password, err := validatePassword(body.Password)
	if err != nil {
		return err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	administrator := &models.User{
		ID:           store.NewID(),
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		Role:         models.RoleAdmin,
	}

	visibility := models.VisibilityPrivate
	if body.PublicByDefault {
		visibility = models.VisibilityPublic
	}

	err = s.store.Transaction(func(tx *store.Store) error {
		if err := tx.CreateUser(administrator); err != nil {
			return err
		}
		if err := tx.SetSetting(store.SettingInstanceName, instanceName); err != nil {
			return err
		}
		if err := tx.SetSetting(store.SettingThemeColor, themeColor); err != nil {
			return err
		}
		if err := tx.SetSetting(store.SettingInitialized, "true"); err != nil {
			return err
		}
		if body.SeedRepositories != nil && !*body.SeedRepositories {
			return nil
		}
		for _, seed := range seedRepositories {
			repository := &models.Repository{
				Name:          seed.name,
				Format:        seed.format,
				Policy:        seed.policy,
				Visibility:    visibility,
				Description:   seed.description,
				AllowRedeploy: seed.policy == models.PolicySnapshot,
			}
			if err := tx.CreateRepository(repository); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := s.startSession(writer, request, administrator.ID); err != nil {
		return err
	}

	writeJSON(writer, http.StatusCreated, identityOf(administrator))
	return nil
}

func (s *Server) startSession(writer http.ResponseWriter, request *http.Request, userID string) error {
	token, err := auth.CreateSession(s.store, userID)
	if err != nil {
		return err
	}
	auth.AttachSessionCookie(writer, request, token)
	return nil
}

func (s *Server) routeLogin(writer http.ResponseWriter, request *http.Request) error {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(request, &body); err != nil {
		return err
	}

	email, err := requireText(body.Email, "email", 254)
	if err != nil {
		return err
	}
	password, err := requireText(body.Password, "password", 200)
	if err != nil {
		return err
	}

	invalid := fail(http.StatusUnauthorized, "Invalid email or password")

	user, err := s.store.UserByEmail(strings.ToLower(email))
	if err != nil {
		return invalid
	}
	if user.Disabled || !auth.VerifyPassword(password, user.PasswordHash) {
		return invalid
	}

	if err := s.startSession(writer, request, user.ID); err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, identityOf(user))
	return nil
}

func (s *Server) routeLogout(writer http.ResponseWriter, request *http.Request) error {
	if token := auth.SessionToken(request); token != "" {
		if err := auth.DestroySession(s.store, token); err != nil {
			return err
		}
	}
	auth.ClearSessionCookie(writer, request)
	return noContent(writer)
}

func (s *Server) routeMe(writer http.ResponseWriter, request *http.Request) error {
	user, err := requireUser(request)
	if err != nil {
		return err
	}
	writeJSON(writer, http.StatusOK, identityOf(user))
	return nil
}

func (s *Server) routeChangePassword(writer http.ResponseWriter, request *http.Request) error {
	user, err := requireUser(request)
	if err != nil {
		return err
	}

	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeBody(request, &body); err != nil {
		return err
	}

	current, err := requireText(body.CurrentPassword, "currentPassword", 200)
	if err != nil {
		return err
	}
	next, err := validatePassword(body.NewPassword)
	if err != nil {
		return err
	}

	fresh, err := s.store.UserByID(user.ID)
	if err != nil {
		return err
	}
	if !auth.VerifyPassword(current, fresh.PasswordHash) {
		return fail(http.StatusUnauthorized, "Current password is incorrect")
	}

	hash, err := auth.HashPassword(next)
	if err != nil {
		return err
	}
	if _, err := s.store.UpdateUser(user.ID, map[string]any{"password_hash": hash}); err != nil {
		return err
	}
	if err := s.store.DeleteUserSessions(user.ID); err != nil {
		return err
	}
	if err := s.startSession(writer, request, user.ID); err != nil {
		return err
	}

	return noContent(writer)
}
