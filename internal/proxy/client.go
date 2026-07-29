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
	http *http.Client
}

func NewClient(timeout time.Duration) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 16
	transport.ResponseHeaderTimeout = 30 * time.Second

	return &Client{http: &http.Client{Timeout: timeout, Transport: transport}}
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
	if remote.Username != "" || remote.Password != "" {
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

	result := &Response{
		Status:       response.StatusCode,
		ContentType:  response.Header.Get("Content-Type"),
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
	}

	if response.StatusCode == http.StatusOK {
		result.Body = response.Body
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		response.Body.Close()
	}

	return result, nil
}
