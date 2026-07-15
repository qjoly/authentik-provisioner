// Package authentik provides a thin, idempotency-friendly client for the
// authentik REST API (/api/v3). It exposes generic verbs plus a few helpers
// used to resolve objects by their natural key (slug, name, scope name, ...).
package authentik

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a single authentik instance.
type Client struct {
	baseURL string // e.g. https://auth.example.com
	apiURL  string // e.g. https://auth.example.com/api/v3
	token   string
	http    *http.Client
}

// New builds a client. baseURL must be the public authentik URL without a
// trailing "/api/v3" (that suffix is appended internally).
func New(baseURL, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: baseURL,
		apiURL:  baseURL + "/api/v3",
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError carries the HTTP status and body for a failed API call.
type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("authentik %s %s -> %d: %s", e.Method, e.Path, e.Status, e.Body)
}

// do performs a request against /api/v3<path>. body may be nil. It returns the
// raw response body on any 2xx, and an *APIError otherwise.
func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Method: method, Path: path, Status: resp.StatusCode, Body: string(data)}
	}
	return data, nil
}

// Get issues a GET and unmarshals the response into out (may be nil).
func (c *Client) Get(ctx context.Context, path string, out any) error {
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return decode(data, out)
}

// Post issues a POST and unmarshals the response into out (may be nil).
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	data, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	return decode(data, out)
}

// Patch issues a PATCH and unmarshals the response into out (may be nil).
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	data, err := c.do(ctx, http.MethodPatch, path, body)
	if err != nil {
		return err
	}
	return decode(data, out)
}

// Delete issues a DELETE.
func (c *Client) Delete(ctx context.Context, path string) error {
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func decode(data []byte, out any) error {
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// listResponse is the shape of authentik's paginated list endpoints.
type listResponse struct {
	Results []json.RawMessage `json:"results"`
}

// FirstPK returns the pk of the first result of a list endpoint, or "" when the
// list is empty. path is the full query, e.g. "/core/users/?username=akadmin".
func (c *Client) FirstPK(ctx context.Context, path string) (string, error) {
	var list listResponse
	if err := c.Get(ctx, path, &list); err != nil {
		return "", err
	}
	if len(list.Results) == 0 {
		return "", nil
	}
	var obj struct {
		PK json.RawMessage `json:"pk"`
	}
	if err := json.Unmarshal(list.Results[0], &obj); err != nil {
		return "", err
	}
	return pkString(obj.PK), nil
}

// pkString normalises a pk that may be encoded as a JSON string or number.
func pkString(raw json.RawMessage) string {
	s := strings.Trim(string(raw), `"`)
	return s
}

// WaitReady polls the health endpoint until authentik answers or the context is
// cancelled. It hits <baseURL>/-/health/ready/.
func (c *Client) WaitReady(ctx context.Context, interval time.Duration) error {
	healthURL := c.baseURL + "/-/health/ready/"
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// QueryEscape is a small helper so callers can build query strings safely.
func QueryEscape(s string) string { return url.QueryEscape(s) }
