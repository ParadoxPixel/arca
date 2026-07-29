package auth

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"arca/internal/store"
	"arca/internal/store/models"
)

type contextKey struct{}

var userKey contextKey

func WithUser(parent context.Context, user *models.User) context.Context {
	return context.WithValue(parent, userKey, user)
}

func UserFrom(ctx context.Context) *models.User {
	user, _ := ctx.Value(userKey).(*models.User)
	return user
}

func decodeBasic(header string) (login string, secret string, ok bool) {
	if len(header) < 6 || !strings.EqualFold(header[:6], "basic ") {
		return "", "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[6:]))
	if err != nil {
		return "", "", false
	}

	credentials := string(decoded)
	separator := strings.Index(credentials, ":")
	if separator < 0 {
		return "", "", false
	}
	return credentials[:separator], credentials[separator+1:], true
}

func cutBearer(header string) (secret string, found bool) {
	const scheme = "bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	return strings.TrimSpace(header[len(scheme):]), true
}

type Resolver struct {
	store *store.Store
}

func NewResolver(database *store.Store) *Resolver { return &Resolver{store: database} }

func (r *Resolver) fromToken(secret string) *models.User {
	user, tokenID, err := r.store.UserByTokenHash(SHA256Hex(secret))
	if err != nil || user.Disabled {
		return nil
	}
	_ = r.store.TouchToken(tokenID)
	return user
}

func (r *Resolver) fromPassword(login, password string) *models.User {
	user, err := r.store.UserByEmail(login)
	if err != nil || user.Disabled || !VerifyPassword(password, user.PasswordHash) {
		return nil
	}
	return user
}

func (r *Resolver) fromSession(token string) *models.User {
	user, err := r.store.UserBySession(SHA256Hex(token))
	if err != nil || user.Disabled {
		return nil
	}
	return user
}

func (r *Resolver) Resolve(request *http.Request) *models.User {
	if header := request.Header.Get("Authorization"); header != "" {
		// A bearer value is unambiguously a token, so it skips the prefix check
		// that only exists to tell a token from a password inside Basic.
		if secret, found := cutBearer(header); found {
			return r.fromToken(secret)
		}

		login, secret, ok := decodeBasic(header)
		if !ok {
			return nil
		}
		if strings.HasPrefix(secret, TokenPrefix) {
			return r.fromToken(secret)
		}
		return r.fromPassword(login, secret)
	}

	if cookie, err := request.Cookie(SessionCookie); err == nil {
		return r.fromSession(cookie.Value)
	}
	return nil
}

func (r *Resolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(writer, request.WithContext(WithUser(request.Context(), r.Resolve(request))))
	})
}
