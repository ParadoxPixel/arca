package proxy

import (
	"net"
	"net/http"
	"net/url"
	"testing"
)

func TestTokenCacheKeyDependsOnTheCredentials(t *testing.T) {
	answer := challenge{Realm: "https://auth.example/token", Service: "registry", Scope: "repository:private/app:pull"}

	anonymous := answer.key(Remote{BaseURL: "https://registry.example"})
	credentialled := answer.key(Remote{BaseURL: "https://registry.example", Username: "ci", Password: "secret"})
	other := answer.key(Remote{BaseURL: "https://registry.example", Username: "ci", Password: "different"})

	if anonymous == credentialled || credentialled == other {
		t.Fatal("proxies with different credentials must not share a token")
	}
	if credentialled != answer.key(Remote{BaseURL: "https://registry.example", Username: "ci", Password: "secret"}) {
		t.Fatal("the same credentials share one")
	}
}

func TestCheckRedirect(t *testing.T) {
	first, _ := url.Parse("https://registry.example/v2/app/blobs/sha256:abc")

	redirected := func(target string) *http.Request {
		parsed, _ := url.Parse(target)
		request := &http.Request{URL: parsed, Header: http.Header{"Authorization": {"Bearer token"}}}
		return request
	}
	via := []*http.Request{{URL: first}}

	t.Run("a downgrade to http is refused", func(t *testing.T) {
		if err := checkRedirect(redirected("http://registry.example/blob"), via); err == nil {
			t.Fatal("expected a refusal")
		}
	})

	t.Run("another host loses the credentials", func(t *testing.T) {
		request := redirected("https://cdn.registry.example/blob")
		if err := checkRedirect(request, via); err != nil {
			t.Fatal(err)
		}
		if request.Header.Get("Authorization") != "" {
			t.Fatal("the Authorization header must not follow to a subdomain")
		}
	})

	t.Run("the same host keeps them", func(t *testing.T) {
		request := redirected("https://registry.example/elsewhere")
		if err := checkRedirect(request, via); err != nil {
			t.Fatal(err)
		}
		if request.Header.Get("Authorization") == "" {
			t.Fatal("a same-host redirect keeps the credentials")
		}
	})
}

func TestForbiddenUpstreams(t *testing.T) {
	for _, address := range []string{"169.254.169.254", "169.254.0.1", "fe80::1", "0.0.0.0", "::"} {
		if !IsForbiddenUpstream(net.ParseIP(address)) {
			t.Errorf("%s must be refused", address)
		}
	}
	for _, address := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "151.101.1.1", "2606:4700::1"} {
		if IsForbiddenUpstream(net.ParseIP(address)) {
			t.Errorf("%s must be allowed", address)
		}
	}

	if _, err := NormalizeRemoteURL("http://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("a link-local remote must be refused on the way in")
	}
	if _, err := NormalizeRemoteURL("http://10.0.0.5:8081/repository/maven-public"); err != nil {
		t.Fatalf("a private upstream is legitimate: %v", err)
	}
}
