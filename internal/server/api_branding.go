package server

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"arca/internal/store"
)

const maximumLogoBytes = 512 << 10

var logoTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml"}

func logoURL(logo store.Logo) string {
	if logo.ContentType == "" {
		return ""
	}
	return "/api/branding/logo?v=" + strconv.FormatInt(logo.UpdatedAt, 10)
}

// detectLogoType trusts the bytes rather than the upload's content type. SVG is
// recognised by hand because DetectContentType reports it as plain text or XML.
func detectLogoType(payload []byte) (string, error) {
	head := payload
	if len(head) > 512 {
		head = head[:512]
	}

	if index := bytes.Index(head, []byte("<svg")); index >= 0 {
		return "image/svg+xml", nil
	}

	detected, _, _ := strings.Cut(http.DetectContentType(head), ";")
	for _, allowed := range logoTypes {
		if detected == allowed {
			return detected, nil
		}
	}
	return "", fail(http.StatusBadRequest, "The logo must be a PNG, JPEG, GIF, WebP or SVG image")
}

func (s *Server) routeLogo(writer http.ResponseWriter, request *http.Request) error {
	logo := s.store.Logo()
	if logo.ContentType == "" {
		return fail(http.StatusNotFound, "No logo has been uploaded")
	}

	file, info, err := s.blobs.Open(store.LogoStorageKey)
	if err != nil {
		return fail(http.StatusNotFound, "No logo has been uploaded")
	}
	defer file.Close()

	header := writer.Header()
	header.Set("Content-Type", logo.ContentType)
	header.Set("Cache-Control", "public, max-age=300")
	header.Set("X-Content-Type-Options", "nosniff")
	// An uploaded SVG is markup this server hands back on its own origin, so it
	// is served under a policy that lets it draw itself and nothing else.
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")

	http.ServeContent(writer, request, "logo", info.ModTime(), file)
	return nil
}

func (s *Server) routeUploadLogo(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	payload, err := io.ReadAll(io.LimitReader(request.Body, maximumLogoBytes+1))
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return fail(http.StatusBadRequest, "The request body is empty")
	}
	if len(payload) > maximumLogoBytes {
		return fail(http.StatusRequestEntityTooLarge, "The logo must be at most %d KB", maximumLogoBytes>>10)
	}

	contentType, err := detectLogoType(payload)
	if err != nil {
		return err
	}

	if _, _, err := s.blobs.Put(store.LogoStorageKey, bytes.NewReader(payload), maximumLogoBytes); err != nil {
		return err
	}
	if err := s.store.SetSetting(store.SettingLogoType, contentType); err != nil {
		return err
	}
	if err := s.store.SetSetting(store.SettingLogoUpdatedAt, strconv.FormatInt(time.Now().UnixMilli(), 10)); err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, map[string]any{"logoUrl": logoURL(s.store.Logo())})
	return nil
}

func (s *Server) routeDeleteLogo(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	if err := s.blobs.Delete(store.LogoStorageKey); err != nil {
		return err
	}
	if err := s.store.DeleteSetting(store.SettingLogoType); err != nil {
		return err
	}
	if err := s.store.DeleteSetting(store.SettingLogoUpdatedAt); err != nil {
		return err
	}

	return noContent(writer)
}
