package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/saluc28/bhgraph"
)

const (
	testTokenID  = "11111111-2222-3333-4444-555555555555"
	testTokenKey = "bhgraph-test-key"
)

// serverSignature recomputes a request signature the way BloodHound's server
// does, and is why these tests are worth more than checking that a header is
// present.
//
// It is deliberately a second implementation of the chain rather than a call to
// Sign: a test that used Sign would only prove the code agrees with itself.
//
// It uses r.RequestURI, which covers path and query, because that is what
// BloodHound's ValidateRequestSignature uses. The client helper BloodHound ships
// signs URL.Path instead, and the two agree until a request carries a query
// parameter. Pinning the server's behaviour here means that if this library ever
// drifts back to signing the path only, the shortest-path test fails locally
// instead of turning into a 401 against a real instance.
func serverSignature(method, requestURI, datetime string, body []byte) string {
	digester := hmac.New(sha256.New, []byte(testTokenKey))
	digester.Write([]byte(method + requestURI))
	digester = hmac.New(sha256.New, digester.Sum(nil))
	digester.Write([]byte(datetime[:13]))
	digester = hmac.New(sha256.New, digester.Sum(nil))
	digester.Write(body)
	return base64.StdEncoding.EncodeToString(digester.Sum(nil))
}

// verifyLikeBloodHound checks the three auth headers on an incoming request and
// the signature over its body.
func verifyLikeBloodHound(t *testing.T, r *http.Request) {
	t.Helper()

	auth := r.Header.Get(HeaderAuthorization)
	if want := AuthorizationScheme + " " + testTokenID; auth != want {
		t.Errorf("Authorization header: got %q, want %q", auth, want)
	}
	datetime := r.Header.Get(HeaderRequestDate)
	if datetime == "" {
		t.Fatal("missing RequestDate header")
	}
	if _, err := time.Parse(time.RFC3339, datetime); err != nil {
		t.Errorf("RequestDate is not RFC3339: %q", datetime)
	}
	sig := r.Header.Get(HeaderSignature)
	if sig == "" {
		t.Fatal("missing Signature header")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	if want := serverSignature(r.Method, r.RequestURI, datetime, body); sig != want {
		t.Errorf("signature mismatch for %s %s\n got: %s\nwant: %s", r.Method, r.RequestURI, sig, want)
	}
}

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, testTokenID, testTokenKey, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestRequestsAreSignedTheWayTheServerChecks(t *testing.T) {
	cases := []struct {
		name string
		call func(*Client) error
	}{
		{"GET without body", func(c *Client) error {
			_, err := c.Get(context.Background(), "/api/v2/self")
			return err
		}},
		{"POST with body", func(c *Client) error {
			_, err := c.Post(context.Background(), "/api/v2/graphs/cypher", []byte(`{"query":"MATCH (n) RETURN n"}`))
			return err
		}},
		{"PUT with body", func(c *Client) error {
			_, err := c.Put(context.Background(), "/api/v2/features/1/toggle", []byte(`{}`))
			return err
		}},
		{"DELETE without body", func(c *Client) error {
			_, err := c.Delete(context.Background(), "/api/v2/saved-queries/7")
			return err
		}},
		{
			// The case that broke against a real server: the signature must
			// cover the query string.
			name: "GET with a query string",
			call: func(c *Client) error {
				_, err := c.ShortestPath(context.Background(), "A", "B", true)
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				verifyLikeBloodHound(t, r)
				fmt.Fprint(w, `{"data":{}}`)
			})
			if err := tc.call(c); err != nil {
				t.Fatalf("call failed: %v", err)
			}
		})
	}
}

// BloodHound rejects a body without an explicit Content-Type, and sends one
// only when there is a body. The header is derived from the body rather than
// passed in, so it is pinned here for every call that carries one.
func TestContentTypeFollowsTheBody(t *testing.T) {
	cases := []struct {
		name string
		call func(*Client) error
		want string
	}{
		{"GET has no body and no content type", func(c *Client) error {
			_, err := c.Get(context.Background(), "/api/v2/self")
			return err
		}, ""},
		{"POST with a nil body sends no content type", func(c *Client) error {
			_, err := c.Post(context.Background(), "/api/v2/file-upload/start", nil)
			return err
		}, ""},
		{"POST with a body is JSON", func(c *Client) error {
			_, err := c.Post(context.Background(), "/api/v2/graphs/cypher", []byte(`{}`))
			return err
		}, "application/json"},
		{"PUT with a body is JSON", func(c *Client) error {
			_, err := c.Put(context.Background(), "/api/v2/extensions", []byte(`{}`))
			return err
		}, "application/json"},
		{"DELETE has no body and no content type", func(c *Client) error {
			_, err := c.Delete(context.Background(), "/api/v2/saved-queries/7")
			return err
		}, ""},
		{"Cypher is JSON", func(c *Client) error {
			_, err := c.Cypher(context.Background(), "MATCH (n) RETURN n")
			return err
		}, "application/json"},
		{"InstallExtension is JSON", func(c *Client) error {
			return c.InstallExtension(context.Background(), bhgraph.Extension{
				Schema:            bhgraph.SchemaMeta{Name: "t", Version: "v1", Namespace: "T"},
				NodeKinds:         []bhgraph.NodeKind{{Name: "T_Node", IsDisplayKind: true}},
				RelationshipKinds: []bhgraph.RelationshipKind{{Name: "T_Edge"}},
			})
		}, "application/json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("Content-Type")
				fmt.Fprint(w, `{"data":{"id":1}}`)
			})
			if err := tc.call(c); err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("Content-Type: got %q, want %q", got, tc.want)
			}
		})
	}
}

// Skipping the third call leaves the job open and nothing is processed, which
// looks exactly like a successful upload that quietly did nothing. The order
// matters as much as the presence.
func TestIngestMakesThreeCallsInOrder(t *testing.T) {
	var seen []string
	var uploadContentType, uploadName string

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		verifyLikeBloodHound(t, r)
		seen = append(seen, r.Method+" "+r.URL.Path)

		switch r.URL.Path {
		case "/api/v2/file-upload/start":
			fmt.Fprint(w, `{"data":{"id":42}}`)
		case "/api/v2/file-upload/42":
			uploadContentType = r.Header.Get("Content-Type")
			uploadName = r.Header.Get("X-File-Upload-Name")
			w.WriteHeader(http.StatusAccepted)
		case "/api/v2/file-upload/42/end":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	g := bhgraph.Graph{
		Nodes: []bhgraph.Node{{ID: "A", Kinds: []string{"X_Person"}}},
	}
	id, err := c.Ingest(context.Background(), g)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if id != 42 {
		t.Errorf("job id: got %d, want 42", id)
	}

	want := []string{
		"POST /api/v2/file-upload/start",
		"POST /api/v2/file-upload/42",
		"POST /api/v2/file-upload/42/end",
	}
	if len(seen) != len(want) {
		t.Fatalf("expected %d calls, got %d: %v", len(want), len(seen), seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("call %d: got %q, want %q", i, seen[i], want[i])
		}
	}

	// BloodHound rejects the upload without an explicit Content-Type.
	if uploadContentType != "application/json" {
		t.Errorf("upload Content-Type: got %q, want application/json", uploadContentType)
	}
	if uploadName == "" {
		t.Error("expected X-File-Upload-Name to be set, it makes server-side errors readable")
	}
}

// An invalid graph must not reach the network at all: the point of validating
// locally is to avoid an ingest job whose errors arrive later and say less.
func TestIngestRefusesInvalidGraphWithoutCallingTheServer(t *testing.T) {
	called := false
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	g := bhgraph.Graph{
		Nodes: []bhgraph.Node{{ID: "A", Kinds: []string{"X_Person"}}},
		Edges: []bhgraph.Edge{{Kind: "X_Edge", Start: bhgraph.NodeRef("A"), End: bhgraph.NodeRef("ghost")}},
	}
	if _, err := c.Ingest(context.Background(), g); err == nil {
		t.Fatal("expected the ingest to be refused")
	}
	if called {
		t.Error("the server was contacted despite the graph being invalid")
	}
}

func TestIngestStopsAtTheFailingStep(t *testing.T) {
	var calls int
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/api/v2/file-upload/start" {
			fmt.Fprint(w, `{"data":{"id":7}}`)
			return
		}
		http.Error(w, `{"errors":[{"message":"nope"}]}`, http.StatusBadRequest)
	})

	g := bhgraph.Graph{Nodes: []bhgraph.Node{{ID: "A", Kinds: []string{"X_Person"}}}}
	id, err := c.Ingest(context.Background(), g)
	if err == nil {
		t.Fatal("expected an error")
	}
	// The job id is still returned so the caller can look the job up or clean up.
	if id != 7 {
		t.Errorf("expected the job id to be reported even on failure, got %d", id)
	}
	if calls != 2 {
		t.Errorf("expected the sequence to stop at the failing upload, got %d calls", calls)
	}
}

func TestStartJobRejectsAResponseWithoutAnID(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{}}`)
	})
	if _, err := c.StartJob(context.Background()); err == nil {
		t.Fatal("expected an error when the response carries no job id")
	}
}

func TestJobStatus(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		verifyLikeBloodHound(t, r)
		if r.URL.Path != "/api/v2/file-upload/9/completed-tasks" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		fmt.Fprint(w, `{"data":[{"id":9,"status":2}]}`)
	})

	body, err := c.JobStatus(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"status":2`) {
		t.Errorf("unexpected body: %s", body)
	}
}

// A 404 on the extensions path almost always means the feature flag is off.
// Saying so in the error is the difference between a five-minute fix and an
// afternoon spent checking the path, the method and the signature.
func TestExtensionsNotFoundMentionsTheFeatureFlag(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"message":"resource not found"}]}`, http.StatusNotFound)
	})

	_, err := c.ListExtensions(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), FeatureFlagExtensions) {
		t.Errorf("the 404 should name the feature flag, got: %v", err)
	}
}

func TestUnauthorizedExplainsTheLikelyCause(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"message":"signature digest mismatch"}]}`, http.StatusUnauthorized)
	})

	_, err := c.Get(context.Background(), "/api/v2/self")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "two hours") {
		t.Errorf("a 401 should hint at the signature and the clock, got: %v", err)
	}
}

func TestInstallExtensionRefusesAnInvalidSchema(t *testing.T) {
	called := false
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { called = true })

	// No namespace, no node kinds.
	err := c.InstallExtension(context.Background(), bhgraph.Extension{})
	if err == nil {
		t.Fatal("expected the install to be refused")
	}
	if called {
		t.Error("the server was contacted despite the schema being invalid")
	}
}

// A delete sent as anything else answers 200 or 405 and removes nothing, and a
// caller that only looks at the error reads that as done. The verb and the path
// are pinned, and a 204 with no body is a success rather than a short read.
func TestDeleteUsesDELETEAndAcceptsAnEmptyBody(t *testing.T) {
	var method, path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		verifyLikeBloodHound(t, r)
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	body, err := c.Delete(context.Background(), "/api/v2/saved-queries/7")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want nothing", body)
	}
	if method != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", method)
	}
	if path != "/api/v2/saved-queries/7" {
		t.Errorf("unexpected path %q", path)
	}
}

func TestInstallExtensionUsesPUT(t *testing.T) {
	var method, path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		verifyLikeBloodHound(t, r)
		method, path = r.Method, r.URL.Path
		fmt.Fprint(w, `{"data":{}}`)
	})

	ext := bhgraph.Extension{
		Schema:            bhgraph.SchemaMeta{Name: "t", Version: "v1", Namespace: "T"},
		NodeKinds:         []bhgraph.NodeKind{{Name: "T_Node", IsDisplayKind: true}},
		RelationshipKinds: []bhgraph.RelationshipKind{{Name: "T_Edge", IsTraversable: true}},
	}
	if err := c.InstallExtension(context.Background(), ext); err != nil {
		t.Fatal(err)
	}
	// The verb is an upsert, and getting it wrong is a 404 or a 405 rather than
	// anything descriptive.
	if method != http.MethodPut {
		t.Errorf("expected PUT, got %s", method)
	}
	if path != "/api/v2/extensions" {
		t.Errorf("unexpected path %q", path)
	}
}

func TestNewRejectsBadArguments(t *testing.T) {
	cases := []struct{ name, url, id, key string }{
		{"empty url", "", "id", "key"},
		{"no scheme", "127.0.0.1:8080", "id", "key"},
		{"empty token id", "http://x", "", "key"},
		{"empty token key", "http://x", "id", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.url, tc.id, tc.key); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestShortestPathRequiresBothEnds(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the server should not have been contacted")
	})
	if _, err := c.ShortestPath(context.Background(), "", "B", true); err == nil {
		t.Error("expected an error for an empty start node")
	}
}
