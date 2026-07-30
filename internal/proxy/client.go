package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	userAgent     = "arca/1"
	defaultAccept = "*/*"
)

var ErrUnsupportedScheme = errors.New("proxy: remote must be an absolute http or https URL")

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

	// Redirects are followed, which container registries rely on: a blob GET is
	// answered with a 307 to object storage. Go strips the Authorization header when
	// a redirect crosses hosts, so the token never reaches the CDN.
	return &Client{
		http:   &http.Client{Timeout: timeout, Transport: transport},
		tokens: newTokenCache(),
	}
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
