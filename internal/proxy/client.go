package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	userAgent     = "arca/1"
	defaultAccept = "*/*"
)

var (
	ErrUnsupportedScheme = errors.New("proxy: remote must be an absolute http or https URL")
	ErrForbiddenUpstream = errors.New("proxy: remote must not be a link-local address")
)

type Remote struct {
	BaseURL  string
	Username string
	Password string
	Accept   string
}

type Conditional struct {
	ETag         string
	LastModified string
}

type Response struct {
	Status       int
	Body         io.ReadCloser
	ContentType  string
	ETag         string
	LastModified string
}

func (r *Response) Close() {
	if r != nil && r.Body != nil {
		r.Body.Close()
	}
}

type Client struct {
	http   *http.Client
	tokens *tokenCache
}

func NewClient(timeout time.Duration) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 16
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.DialContext = guardedDialer()

	// Redirects are followed, which container registries rely on: a blob GET is
	// answered with a 307 to object storage. Go strips the Authorization header when
	// a redirect crosses hosts, except onto a subdomain, so checkRedirect strips it
	// on any change of host and refuses to leave https at all.
	return &Client{
		http:   &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: checkRedirect},
		tokens: newTokenCache(),
	}
}

func checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("proxy: too many redirects")
	}
	previous := via[len(via)-1].URL
	if previous.Scheme == "https" && request.URL.Scheme != "https" {
		return fmt.Errorf("proxy: refusing a redirect from https to %s", request.URL.Redacted())
	}
	if !strings.EqualFold(request.URL.Host, via[0].URL.Host) {
		request.Header.Del("Authorization")
	}
	return nil
}

// guardedDialer resolves the upstream itself and refuses link-local addresses,
// which is where cloud instance metadata lives and where no artifact repository
// ever does. Private ranges stay reachable: an upstream inside the same network
// is the usual reason to run a proxy at all. The check is at dial time rather
// than on the configured URL so a hostname that resolves there is caught too.
func guardedDialer() func(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}

		var last error
		for _, candidate := range addresses {
			if IsForbiddenUpstream(candidate.IP) {
				last = fmt.Errorf("proxy: refusing to connect to %s, which resolves to the link-local address %s", host, candidate.IP)
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = fmt.Errorf("proxy: %s resolves to no address", host)
		}
		return nil, last
	}
}

// IsForbiddenUpstream reports an address a proxy must never fetch from.
func IsForbiddenUpstream(ip net.IP) bool {
	return ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast()
}

func NormalizeRemoteURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ErrUnsupportedScheme
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrUnsupportedScheme
	}
	if parsed.Host == "" {
		return "", ErrUnsupportedScheme
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && IsForbiddenUpstream(ip) {
		return "", ErrForbiddenUpstream
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func (c *Client) Fetch(ctx context.Context, remote Remote, path string, conditional Conditional) (*Response, error) {
	target, err := url.Parse(remote.BaseURL)
	if err != nil {
		return nil, ErrUnsupportedScheme
	}
	target.Path = strings.TrimSuffix(target.Path, "/") + "/" + path

	response, err := c.send(ctx, remote, target, conditional, "")
	if err != nil {
		return nil, err
	}

	// A container registry answers an unauthenticated request with a challenge to a
	// separate token service rather than accepting credentials itself. One retry with
	// the issued token is the whole of that protocol.
	if response.StatusCode == http.StatusUnauthorized {
		answer, parseErr := parseChallenge(response.Header.Get("WWW-Authenticate"))
		if parseErr == nil {
			drain(response)

			token, tokenErr := c.token(ctx, remote, answer)
			if tokenErr != nil {
				return nil, tokenErr
			}
			if response, err = c.send(ctx, remote, target, conditional, token); err != nil {
				return nil, err
			}
		}
	}

	result := &Response{
		Status:       response.StatusCode,
		ContentType:  response.Header.Get("Content-Type"),
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
	}

	if response.StatusCode == http.StatusOK {
		result.Body = response.Body
	} else {
		drain(response)
	}

	return result, nil
}

func (c *Client) send(ctx context.Context, remote Remote, target *url.URL, conditional Conditional, token string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	accept := remote.Accept
	if accept == "" {
		accept = defaultAccept
	}

	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", accept)
	switch {
	case token != "":
		request.Header.Set("Authorization", "Bearer "+token)
	case remote.Username != "" || remote.Password != "":
		request.SetBasicAuth(remote.Username, remote.Password)
	}
	if conditional.ETag != "" {
		request.Header.Set("If-None-Match", conditional.ETag)
	}
	if conditional.LastModified != "" {
		request.Header.Set("If-Modified-Since", conditional.LastModified)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", target.Redacted(), err)
	}
	return response, nil
}

func drain(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
}
