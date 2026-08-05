// Package client signs and sends requests to a BloodHound CE instance.
//
// It covers only what a collector needs: installing an extension schema,
// running an ingest job, and issuing Cypher queries. Wrapping the whole
// BloodHound API would age badly with every release.
package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a BloodHound CE instance, signing every request with the
// bhesignature scheme.
//
// A Client is safe for concurrent use if the underlying http.Client is.
type Client struct {
	baseURL        *url.URL
	tokenID        string
	tokenKey       string
	httpClient     *http.Client
	spoolThreshold int
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient supplies the http.Client used for all requests. Use it to set
// timeouts, proxies, or a custom TLS configuration.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithSpoolThreshold sets the payload size in bytes above which an ingest is
// written to a temporary file rather than held in memory. Zero selects
// DefaultSpoolThreshold.
//
// The signature covers the request body, so the body must be fully available
// before the request goes out. This is the knob that decides whether "fully
// available" means in RAM or on disk.
func WithSpoolThreshold(size int) Option {
	return func(c *Client) { c.spoolThreshold = size }
}

// New returns a Client for the BloodHound instance at baseURL.
//
// tokenID and tokenKey come from a BloodHound API token: Settings, My Profile,
// API Key Management. The key is shown once at creation.
func New(baseURL, tokenID, tokenKey string, opts ...Option) (*Client, error) {
	if tokenID == "" || tokenKey == "" {
		return nil, fmt.Errorf("bhgraph/client: token ID and key are both required")
	}
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("bhgraph/client: parsing base URL: %w", err)
	}
	// This also rejects an empty base URL, which parses without complaint.
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("bhgraph/client: base URL needs a scheme and a host, got %q", baseURL)
	}

	c := &Client{
		baseURL:    u,
		tokenID:    tokenID,
		tokenKey:   tokenKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Get issues a signed GET against an arbitrary path and returns the raw body.
//
// Get, Post and Put are exported because this library deliberately does not
// wrap the whole BloodHound API: a caller who needs an endpoint we do not model
// should not have to reimplement the signature to reach it.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, "GET", path, nil)
}

// Post issues a signed POST. A nil body sends no content, which is what several
// BloodHound endpoints expect.
func (c *Client) Post(ctx context.Context, path string, body []byte) ([]byte, error) {
	return c.do(ctx, "POST", path, body)
}

// Put issues a signed PUT.
func (c *Client) Put(ctx context.Context, path string, body []byte) ([]byte, error) {
	return c.do(ctx, "PUT", path, body)
}

// APIError reports a non-2xx response from BloodHound.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("bhgraph/client: %s %s: HTTP %d", e.Method, e.Path, e.StatusCode)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	switch {
	case e.StatusCode == http.StatusUnauthorized:
		msg += " (a 401 with a correct token usually means the signature chain or the RequestDate header is wrong; signatures are valid for at most two hours)"
	case e.StatusCode == http.StatusNotFound && strings.HasPrefix(e.Path, pathExtensions):
		msg += " (a 404 here usually means the " + FeatureFlagExtensions +
			" feature flag is off, which is the default: with it off the route is not registered at all." +
			" Enable it under Administration, then Early Access Features, or via PUT /api/v2/features/{id}/toggle)"
	case e.StatusCode == http.StatusForbidden && strings.HasPrefix(e.Path, pathFeatures):
		msg += " (reading feature flags needs a token whose role can read the application configuration)"
	}
	return msg
}

// do signs and sends a request, returning the response body on success.
//
// The body has to be read twice, once by the signature and once by the
// transport, so it is held as a buffer. An ingest payload is large enough for
// that to matter and takes doStream instead.
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL.String()+path, bodyReader(body))
	if err != nil {
		return nil, fmt.Errorf("bhgraph/client: building request: %w", err)
	}

	at := time.Now()
	// RequestURI, not Path: the signature covers the query string too, or the
	// server answers 401 as soon as a request carries a parameter. See Sign.
	sig, err := Sign(c.tokenKey, method, req.URL.RequestURI(), at, bodyReader(body))
	if err != nil {
		return nil, err
	}

	c.setAuthHeaders(req, at, sig)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.send(req, method, path)
}

// bodyReader returns a reader over body, or a nil io.Reader for a nil body so
// that net/http sends no content at all.
func bodyReader(body []byte) io.Reader {
	if body == nil {
		return nil
	}
	return bytes.NewReader(body)
}

// streamRequest is a request whose body was signed while it was being produced
// rather than after it existed. The fields are a struct because eight
// positional arguments at the call site say nothing about which is which.
type streamRequest struct {
	method      string
	path        string
	body        io.Reader
	size        int64
	at          time.Time
	signature   string
	contentType string
	headers     map[string]string
}

// doStream sends a request whose body has already been signed.
//
// It is the path an ingest takes: the payload is serialized once into both a
// spool and a BodySigner, so the signature exists before the body is re-read
// for transmission and the graph is never held in memory in full.
func (c *Client) doStream(ctx context.Context, r streamRequest) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, r.method, c.baseURL.String()+r.path, r.body)
	if err != nil {
		return nil, fmt.Errorf("bhgraph/client: building request: %w", err)
	}
	// Without this the transport falls back to chunked encoding, and the server
	// takes a different path for bodies of unknown length.
	req.ContentLength = r.size

	c.setAuthHeaders(req, r.at, r.signature)
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}

	return c.send(req, r.method, r.path)
}

func (c *Client) setAuthHeaders(req *http.Request, at time.Time, signature string) {
	req.Header.Set(HeaderAuthorization, AuthorizationScheme+" "+c.tokenID)
	req.Header.Set(HeaderRequestDate, at.Format(time.RFC3339))
	req.Header.Set(HeaderSignature, signature)
}

func (c *Client) send(req *http.Request, method, path string) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bhgraph/client: %s %s: %w", method, path, err)
	}
	// A failure to close a response body we have finished reading is not worth
	// surfacing over the result of the call.
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bhgraph/client: reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &APIError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			Body:       truncate(string(respBody), 512),
		}
	}
	return respBody, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
