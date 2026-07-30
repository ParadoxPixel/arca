// Package nexus reads a Sonatype Nexus 3 instance over its REST API. Every call
// here is a GET: the package has no way to modify or delete anything, which is
// what makes it safe to point at a production repository manager.
package nexus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const userAgent = "arca-migrate/1"

var ErrUnauthorized = errors.New("nexus: the credentials were rejected")

type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

func New(baseURL, username, password string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("nexus: the URL must be absolute http or https")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")

	return &Client{
		baseURL:  parsed.String(),
		username: username,
		password: password,
		http:     &http.Client{Timeout: timeout},
	}, nil
}

// Repository is the subset of a Nexus repository definition that a migration
// needs. Nexus nests the upstream of a proxy under attributes.
type Repository struct {
	Name       string `json:"name"`
	Format     string `json:"format"`
	Type       string `json:"type"`
	URL        string `json:"url"`
	Attributes struct {
		Proxy struct {
			RemoteURL string `json:"remoteUrl"`
		} `json:"proxy"`
		Group struct {
			MemberNames []string `json:"memberNames"`
		} `json:"group"`
		Maven struct {
			VersionPolicy string `json:"versionPolicy"`
		} `json:"maven"`
	} `json:"attributes"`
}

func (r Repository) VersionPolicy() string { return r.Attributes.Maven.VersionPolicy }

func (r Repository) RemoteURL() string { return r.Attributes.Proxy.RemoteURL }
func (r Repository) Members() []string { return r.Attributes.Group.MemberNames }
func (r Repository) IsProxy() bool     { return r.Type == "proxy" }
func (r Repository) IsGroup() bool     { return r.Type == "group" }
func (r Repository) IsHosted() bool    { return r.Type == "hosted" }

type Asset struct {
	Path        string `json:"path"`
	DownloadURL string `json:"downloadUrl"`
	FileSize    int64  `json:"fileSize"`
	ContentType string `json:"contentType"`
	Checksum    struct {
		SHA1 string `json:"sha1"`
		MD5  string `json:"md5"`
	} `json:"checksum"`
}

// Component is one versioned thing, with its assets nested. For docker this is the
// only endpoint that names tags at all: the asset list reports manifests by digest and
// nothing else, so a migration driven off assets alone copies every byte and produces
// no tags.
type Component struct {
	ID         string  `json:"id"`
	Group      string  `json:"group"`
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	Repository string  `json:"repository"`
	Format     string  `json:"format"`
	Assets     []Asset `json:"assets"`
}

type page[T any] struct {
	Items             []T    `json:"items"`
	ContinuationToken string `json:"continuationToken"`
}

func (c *Client) get(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	if c.username != "" || c.password != "" {
		request.SetBasicAuth(c.username, c.password)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}

	switch response.StatusCode {
	case http.StatusOK:
		return response, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		response.Body.Close()
		return nil, ErrUnauthorized
	default:
		response.Body.Close()
		return nil, fmt.Errorf("nexus answered %d for %s", response.StatusCode, path)
	}
}

func (c *Client) Repositories(ctx context.Context) ([]Repository, error) {
	response, err := c.get(ctx, "/service/rest/v1/repositories", nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	var repositories []Repository
	if err := json.NewDecoder(response.Body).Decode(&repositories); err != nil {
		return nil, fmt.Errorf("nexus: reading the repository list: %w", err)
	}
	return repositories, nil
}

// Assets walks every asset of a repository, following the continuation token
// Nexus pages with. The callback runs per page so a large repository never has
// to be held in memory at once.
func (c *Client) Assets(ctx context.Context, repository string, visit func([]Asset) error) error {
	return walk(ctx, c, "/service/rest/v1/assets", repository, "assets", visit)
}

// Components walks the same repository grouped by version. Both walks are needed for
// docker, whose two endpoints expose disjoint views: assets hold the blobs and the
// digest-addressed manifests, components hold the tags.
func (c *Client) Components(ctx context.Context, repository string, visit func([]Component) error) error {
	return walk(ctx, c, "/service/rest/v1/components", repository, "components", visit)
}

func walk[T any](ctx context.Context, c *Client, path, repository, subject string, visit func([]T) error) error {
	token := ""

	for {
		query := url.Values{"repository": {repository}}
		if token != "" {
			query.Set("continuationToken", token)
		}

		response, err := c.get(ctx, path, query)
		if err != nil {
			return err
		}

		var current page[T]
		err = json.NewDecoder(response.Body).Decode(&current)
		response.Body.Close()
		if err != nil {
			return fmt.Errorf("nexus: reading %s of %s: %w", subject, repository, err)
		}

		if err := visit(current.Items); err != nil {
			return err
		}
		if current.ContinuationToken == "" {
			return nil
		}
		token = current.ContinuationToken
	}
}

// Download opens an asset for reading. The caller closes the returned stream.
func (c *Client) Download(ctx context.Context, asset Asset) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	if c.username != "" || c.password != "" {
		request.SetBasicAuth(c.username, c.password)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("downloading %s answered %d", asset.Path, response.StatusCode)
	}
	return response.Body, nil
}
