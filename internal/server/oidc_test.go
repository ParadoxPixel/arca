package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"arca/internal/config"
	"arca/internal/store/models"
)

// fakeIdP is an OpenID Connect provider with just enough to sign someone in:
// discovery, a key set and a token endpoint. The authorize step is skipped; the
// test reads what Arca would have sent there from the redirect instead.
type fakeIdP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey

	// challenge and claims describe the sign-in in progress.
	challenge string
	claims    map[string]any
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	idp := &fakeIdP{t: t, key: key}

	routes := http.NewServeMux()
	routes.HandleFunc("GET /.well-known/openid-configuration", func(writer http.ResponseWriter, _ *http.Request) {
		base := idp.server.URL
		writeJSON(writer, http.StatusOK, map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	routes.HandleFunc("GET /jwks", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"},
		}})
	})
	routes.HandleFunc("POST /token", idp.token)

	idp.server = httptest.NewServer(routes)
	t.Cleanup(idp.server.Close)
	return idp
}

// token issues an ID token for the sign-in in progress, provided the PKCE
// verifier matches the challenge the authorize redirect carried.
func (idp *fakeIdP) token(writer http.ResponseWriter, request *http.Request) {
	digest := sha256.Sum256([]byte(request.FormValue("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(digest[:]) != idp.challenge {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}

	now := time.Now().Unix()
	claims := map[string]any{"iss": idp.server.URL, "aud": "arca", "iat": now, "exp": now + 300}
	maps.Copy(claims, idp.claims)
	payload, err := json.Marshal(claims)
	if err != nil {
		idp.t.Errorf("encoding claims: %v", err)
		return
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"),
	)
	if err != nil {
		idp.t.Errorf("creating a signer: %v", err)
		return
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		idp.t.Errorf("signing: %v", err)
		return
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		idp.t.Errorf("serializing: %v", err)
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"access_token": "access",
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     raw,
	})
}

// start begins a sign-in and returns the query of the redirect to the provider.
func (idp *fakeIdP) start(instance *testInstance) url.Values {
	idp.t.Helper()

	response := instance.api(http.MethodGet, "/api/auth/oidc/login", "")
	expectStatus(idp.t, response, http.StatusFound)
	target, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		idp.t.Fatalf("parsing the authorize redirect: %v", err)
	}
	return target.Query()
}

// finish calls back with the given state and returns where Arca sends the
// browser next.
func (idp *fakeIdP) finish(instance *testInstance, state string) string {
	idp.t.Helper()

	response := instance.api(http.MethodGet, "/api/auth/oidc/callback?code=code&state="+url.QueryEscape(state), "")
	expectStatus(idp.t, response, http.StatusFound)
	return response.Header.Get("Location")
}

// signIn runs a whole sign-in as the user the claims describe.
func (idp *fakeIdP) signIn(instance *testInstance, claims map[string]any) string {
	idp.t.Helper()

	authorize := idp.start(instance)
	idp.challenge = authorize.Get("code_challenge")
	idp.claims = map[string]any{"nonce": authorize.Get("nonce")}
	maps.Copy(idp.claims, claims)
	return idp.finish(instance, authorize.Get("state"))
}

func person(subject, email string, groups ...string) map[string]any {
	return map[string]any{"sub": subject, "email": email, "name": "Ada Lovelace", "groups": groups}
}

func newOIDCInstance(t *testing.T, idp *fakeIdP, adjust func(*config.OIDC)) *testInstance {
	t.Helper()

	settings := config.OIDC{
		Issuer:       idp.server.URL,
		ClientID:     "arca",
		ClientSecret: "secret",
		Name:         "Test IdP",
		Scopes:       []string{"openid", "profile", "email"},
		GroupsClaim:  "groups",
	}
	if adjust != nil {
		adjust(&settings)
	}
	return newConfiguredInstance(t, config.Config{OIDC: settings})
}

func (i *testInstance) userByEmail(email string) *models.User {
	i.t.Helper()

	user, err := i.app.store.UserByEmail(email)
	if err != nil {
		i.t.Fatalf("UserByEmail(%s): %v", email, err)
	}
	return user
}

func expectLocation(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("redirected to %q, want %q", got, want)
	}
}

func TestOIDCCreatesUserAndSignsIn(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)

	expectLocation(t, idp.signIn(instance, person("sub-1", "Ada@Example.com")), "/")

	user := instance.userByEmail("ada@example.com")
	if user.Role != models.RoleUser || user.Name != "Ada Lovelace" || user.PasswordHash != "" {
		t.Fatalf("unexpected user: %+v", user)
	}

	body := expectStatus(t, instance.api(http.MethodGet, "/api/bootstrap", ""), http.StatusOK)
	var payload struct {
		User *identity              `json:"user"`
		OIDC *struct{ Name string } `json:"oidc"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding bootstrap: %v", err)
	}
	if payload.User == nil || payload.User.ID != user.ID {
		t.Fatalf("expected the session to belong to %s, got %+v", user.ID, payload.User)
	}
	if payload.OIDC == nil || payload.OIDC.Name != "Test IdP" {
		t.Fatalf("oidc = %+v", payload.OIDC)
	}
}

func TestOIDCAccountRefusesPasswordLogin(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)
	idp.signIn(instance, person("sub-1", "ada@example.com"))

	for _, password := range []string{"", "anything-at-all"} {
		response := instance.doAnonymously(instance.request(http.MethodPost, "/api/auth/login",
			`{"email": "ada@example.com", "password": "`+password+`"}`))
		if response.StatusCode == http.StatusOK {
			t.Fatalf("password %q signed in to an OIDC-only account", password)
		}
		response.Body.Close()
	}
}

func TestOIDCMatchesReturningUserBySubject(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)

	idp.signIn(instance, person("sub-1", "ada@example.com"))
	first := instance.userByEmail("ada@example.com")

	// The email follows the provider; the name stays what the account was created with.
	claims := person("sub-1", "ada@new.example.com")
	claims["name"] = "Ada King"
	expectLocation(t, idp.signIn(instance, claims), "/")
	renamed := instance.userByEmail("ada@new.example.com")
	if renamed.ID != first.ID {
		t.Fatalf("a changed email created a second account %s next to %s", renamed.ID, first.ID)
	}
	if renamed.Name != "Ada Lovelace" {
		t.Fatalf("name = %q, want the one set at creation", renamed.Name)
	}
}

func TestOIDCAdminGroupsPromoteAndDemote(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, func(settings *config.OIDC) { settings.AdminGroups = []string{"admins"} })
	instance.setup()

	idp.signIn(instance, person("sub-1", "ada@example.com", "admins"))
	if role := instance.userByEmail("ada@example.com").Role; role != models.RoleAdmin {
		t.Fatalf("role = %s after signing in as an admin group member", role)
	}

	idp.signIn(instance, person("sub-1", "ada@example.com"))
	if role := instance.userByEmail("ada@example.com").Role; role != models.RoleUser {
		t.Fatalf("role = %s after leaving the admin group", role)
	}
}

func TestOIDCKeepsLastAdmin(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, func(settings *config.OIDC) { settings.AdminGroups = []string{"admins"} })

	idp.signIn(instance, person("sub-1", "ada@example.com", "admins"))
	idp.signIn(instance, person("sub-1", "ada@example.com"))
	if role := instance.userByEmail("ada@example.com").Role; role != models.RoleAdmin {
		t.Fatalf("the only administrator was demoted to %s", role)
	}
}

func TestOIDCAllowedGroups(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, func(settings *config.OIDC) { settings.AllowedGroups = []string{"staff"} })

	expectLocation(t, idp.signIn(instance, person("sub-1", "ada@example.com", "guests")), "/login?error=denied")
	if count, _ := instance.app.store.CountUsers(); count != 0 {
		t.Fatalf("a denied sign-in created %d users", count)
	}

	// A single group may arrive as a plain string rather than an array.
	claims := person("sub-1", "ada@example.com")
	claims["groups"] = "staff"
	expectLocation(t, idp.signIn(instance, claims), "/")
}

func TestOIDCEmailCollision(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)
	instance.setup()

	expectLocation(t, idp.signIn(instance, person("sub-1", administratorEmail)), "/login?error=email_taken")

	linking := newOIDCInstance(t, idp, func(settings *config.OIDC) { settings.LinkExisting = true })
	linking.setup()
	expectLocation(t, idp.signIn(linking, person("sub-1", administratorEmail)), "/")

	linked, err := linking.app.store.UserByIdentity(idp.server.URL, "sub-1")
	if err != nil || linked == nil || linked.ID != linking.userByEmail(administratorEmail).ID {
		t.Fatalf("expected the identity to be linked to the administrator, got %+v (%v)", linked, err)
	}
}

func TestOIDCDisabledUser(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)

	idp.signIn(instance, person("sub-1", "ada@example.com"))
	if _, err := instance.app.store.UpdateUser(instance.userByEmail("ada@example.com").ID, map[string]any{"disabled": true}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	expectLocation(t, idp.signIn(instance, person("sub-1", "ada@example.com")), "/login?error=denied")
}

func TestOIDCStateMismatch(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)

	authorize := idp.start(instance)
	idp.challenge = authorize.Get("code_challenge")
	idp.claims = person("sub-1", "ada@example.com")
	idp.claims["nonce"] = authorize.Get("nonce")

	expectLocation(t, idp.finish(instance, "forged"), "/login?error=failed")
	if count, _ := instance.app.store.CountUsers(); count != 0 {
		t.Fatalf("a forged state created %d users", count)
	}
}

func TestOIDCNonceMismatch(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)

	claims := person("sub-1", "ada@example.com")
	claims["nonce"] = "replayed"
	expectLocation(t, idp.signIn(instance, claims), "/login?error=failed")
}

// An empty subject would be one identity shared by everyone who carries it.
func TestOIDCEmptySubject(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)

	expectLocation(t, idp.signIn(instance, person("", "ada@example.com")), "/login?error=failed")
	if taken, _ := instance.app.store.EmailTaken("ada@example.com"); taken {
		t.Fatal("a token without a subject still created an account")
	}
}

func TestDeleteUserRemovesIdentities(t *testing.T) {
	idp := newFakeIdP(t)
	instance := newOIDCInstance(t, idp, nil)
	idp.signIn(instance, person("sub-1", "ada@example.com"))

	if _, err := instance.app.store.DeleteUser(instance.userByEmail("ada@example.com").ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	// A leftover identity row would collide with the one this sign-in creates
	// for the fresh account.
	expectLocation(t, idp.signIn(instance, person("sub-1", "ada@example.com")), "/")
}

func TestOIDCRoutesNotFoundWhenUnconfigured(t *testing.T) {
	instance := newTestInstance(t)

	expectStatus(t, instance.api(http.MethodGet, "/api/auth/oidc/login", ""), http.StatusNotFound)
	expectStatus(t, instance.api(http.MethodGet, "/api/auth/oidc/callback?code=x&state=y", ""), http.StatusNotFound)

	body := expectStatus(t, instance.api(http.MethodGet, "/api/bootstrap", ""), http.StatusOK)
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding bootstrap: %v", err)
	}
	if value, present := payload["oidc"]; !present || value != nil {
		t.Fatalf("oidc = %v, present %v; want null", value, present)
	}
}
