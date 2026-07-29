package server

import (
	"net/http"
	"regexp"
	"strings"

	"arca/internal/store"
)

// hexColor is deliberately narrow: the value is interpolated into a CSS custom
// property, so anything that is not a plain hex triple must not reach the page.
var hexColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func validateThemeColor(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if !hexColor.MatchString(normalized) {
		return "", fail(http.StatusBadRequest, "themeColor must be a hex colour such as #4f46e5")
	}
	return normalized, nil
}

const maximumWelcomeLength = 8000

func validateWelcomeText(value string) (string, error) {
	text := strings.TrimSpace(value)
	if len(text) > maximumWelcomeLength {
		return "", fail(http.StatusBadRequest, "welcomeText must be at most %d characters", maximumWelcomeLength)
	}
	return text, nil
}

func (s *Server) routeSettings(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"instanceName": s.store.InstanceName(),
		"themeColor":   s.store.ThemeColor(),
		"welcomeText":  s.store.WelcomeText(),
		"logoUrl":      logoURL(s.store.Logo()),
	})
	return nil
}

func (s *Server) routeUpdateSettings(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	var body struct {
		InstanceName *string `json:"instanceName"`
		ThemeColor   *string `json:"themeColor"`
		WelcomeText  *string `json:"welcomeText"`
	}
	if err := decodeBody(request, &body); err != nil {
		return err
	}

	updates := map[string]string{}

	if body.InstanceName != nil {
		name, err := requireText(*body.InstanceName, "instanceName", 80)
		if err != nil {
			return err
		}
		updates[store.SettingInstanceName] = name
	}

	if body.ThemeColor != nil {
		color, err := validateThemeColor(*body.ThemeColor)
		if err != nil {
			return err
		}
		updates[store.SettingThemeColor] = color
	}

	if body.WelcomeText != nil {
		welcome, err := validateWelcomeText(*body.WelcomeText)
		if err != nil {
			return err
		}
		updates[store.SettingWelcomeText] = welcome
	}

	for key, value := range updates {
		if err := s.store.SetSetting(key, value); err != nil {
			return err
		}
	}

	return s.routeSettings(writer, request)
}
