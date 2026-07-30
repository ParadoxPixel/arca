package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Container registries do not accept credentials directly. They answer an
// unauthenticated request with a Bearer challenge naming a separate token service,
// and the client exchanges its credentials there for a short-lived token scoped to
// one repository. Docker Hub is the common case: registry-1.docker.io challenges to
// auth.docker.io.
//
// The exchange is what makes anonymous pulls work too: with no credentials the token
// service still issues a token, it just carries fewer rights.

const (
	// tokenSkew expires a cached token early, so one is never presented in the
	// moment it stops being valid.
	tokenSkew = 30 * time.Second
	// tokenLifetime is what a service that reports no expiry is assumed to grant.
	tokenLifetime = 5 * time.Minute
	maxTokenBytes = 1 << 20
)

var errNoChallenge = errors.New("proxy: the response carries no bearer challenge")

type challenge struct {
	Realm   string
	Service string
	Scope   string
}

// parseChallenge reads a WWW-Authenticate header. Only the Bearer scheme is handled:
// a Basic challenge means the credentials were already sent and rejected, which is not
// something a retry can fix.
func parseChallenge(header string) (challenge, error) {
	rest, found := cutPrefixFold(header, "bearer ")
	if !found {
		return challenge{}, errNoChallenge
	}

	parsed := challenge{}
	for _, parameter := range splitParameters(rest) {
		key, value, found := strings.Cut(parameter, "=")
		if !found {
			continue
		}

		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "realm":
			parsed.Realm = value
		case "service":
			parsed.Service = value
		case "scope":
			parsed.Scope = value
		}
	}

	if parsed.Realm == "" {
		return challenge{}, errNoChallenge
	}
	return parsed, nil
}

func cutPrefixFold(value, prefix string) (string, bool) {
	if len(value) < len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return "", false
	}
	return value[len(prefix):], true
}

// splitParameters splits on commas that are not inside a quoted value, because a
// scope routinely contains one: "repository:a:pull,push".
func splitParameters(value string) []string {
	var parameters []string
	quoted := false
	start := 0

	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				parameters = append(parameters, value[start:index])
				start = index + 1
			}
		}
	}
	return append(parameters, value[start:])
}

func (c challenge) key(remote Remote) string {
	return remote.BaseURL + "|" + c.Realm + "|" + c.Service + "|" + c.Scope
}

type cachedToken struct {
	token   string
	expires time.Time
}

type tokenCache struct {
	mutex  sync.Mutex
	tokens map[string]cachedToken
}

func newTokenCache() *tokenCache { return &tokenCache{tokens: map[string]cachedToken{}} }

func (t *tokenCache) get(key string) (string, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	entry, ok := t.tokens[key]
	if !ok || time.Now().After(entry.expires) {
		return "", false
	}
	return entry.token, true
}

func (t *tokenCache) put(key, token string, lifetime time.Duration) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.tokens[key] = cachedToken{token: token, expires: time.Now().Add(lifetime - tokenSkew)}
}

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (t tokenResponse) value() string {
	if t.Token != "" {
		return t.Token
	}
	return t.AccessToken
}

func (t tokenResponse) lifetime() time.Duration {
	if t.ExpiresIn <= 0 {
		return tokenLifetime
	}
	return time.Duration(t.ExpiresIn) * time.Second
}

// token answers a challenge, from cache when it can. Credentials are only ever sent
// to an HTTPS realm: the realm is named by the upstream's own response, so anything
// less would let a compromised or impersonated registry collect them in the clear.
func (c *Client) token(ctx context.Context, remote Remote, answer challenge) (string, error) {
	key := answer.key(remote)
	if cached, ok := c.tokens.get(key); ok {
		return cached, nil
	}

	realm, err := url.Parse(answer.Realm)
	if err != nil || realm.Host == "" {
		return "", fmt.Errorf("proxy: %q is not a usable token realm", answer.Realm)
	}
	credentialled := remote.Username != "" || remote.Password != ""
	if credentialled && realm.Scheme != "https" {
		return "", fmt.Errorf("proxy: refusing to send credentials to the plain-text token realm %s", realm.Host)
	}

	query := realm.Query()
	if answer.Service != "" {
		query.Set("service", answer.Service)
	}
	if answer.Scope != "" {
		query.Set("scope", answer.Scope)
	}
	realm.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", fmt.Errorf("proxy: build token request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/json")
	if credentialled {
		request.SetBasicAuth(remote.Username, remote.Password)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("proxy: fetch a token from %s: %w", realm.Host, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy: %s answered %d for a token", realm.Host, response.StatusCode)
	}

	var issued tokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, maxTokenBytes)).Decode(&issued); err != nil {
		return "", fmt.Errorf("proxy: reading a token from %s: %w", realm.Host, err)
	}
	if issued.value() == "" {
		return "", fmt.Errorf("proxy: %s issued an empty token", realm.Host)
	}

	c.tokens.put(key, issued.value(), issued.lifetime())
	return issued.value(), nil
}
