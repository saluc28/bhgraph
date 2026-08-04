package client

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// Where these vectors come from, and what they are worth.
//
// They were NOT produced by running the implementation in sign.go and freezing
// whatever came out. That would only prove the code agrees with itself.
//
// They were produced on 2026-08-03 by running BloodHound's own reference
// implementation over the inputs below: SpecterOps/BloodHound v9.5.1,
// cmd/api/src/api/signature.go, Apache-2.0, which is the same function the
// server uses to verify what we send. sign.go was then written against the
// documented scheme and checked to agree.
//
// A live CE v9.5.1 then accepted requests signed with them, on the same date.
// That is what turns "this code agrees with the reference implementation" into
// "the whole request is well formed", and it is the one thing these vectors
// cannot establish on their own. The run is manual and outside this suite: see
// the end to end section of the README.
const testKey = "bhgraph-test-key"

func TestSignVectors(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		uri      string
		datetime string
		body     *string // nil means no body at all
		want     string
	}{
		{
			name:     "GET with no body",
			method:   "GET",
			uri:      "/api/v2/extensions",
			datetime: "2026-08-03T14:31:07Z",
			want:     "znoFJpYtxzcwG9oy54kD+jUgMr+aKbLMiy8g8LBYlRI=",
		},
		{
			name:     "POST with JSON body",
			method:   "POST",
			uri:      "/api/v2/file-upload/start",
			datetime: "2026-08-03T14:31:07Z",
			body:     ptr(`{"graph":{"nodes":[],"edges":[]}}`),
			want:     "tkFaoXJIMvSYiqET72EyqMAlnZ0CtfqIE1OKh2R4hDI=",
		},
		{
			// Same hour, different minute and second: the date key truncates to
			// the hour, so this must equal the first vector exactly.
			name:     "truncation: same hour, different minute",
			method:   "GET",
			uri:      "/api/v2/extensions",
			datetime: "2026-08-03T14:59:59Z",
			want:     "znoFJpYtxzcwG9oy54kD+jUgMr+aKbLMiy8g8LBYlRI=",
		},
		{
			// One second later, next hour: must differ.
			name:     "truncation: next hour",
			method:   "GET",
			uri:      "/api/v2/extensions",
			datetime: "2026-08-03T15:00:00Z",
			want:     "XNqZTcaB65VCSyTWx/DPlRKH6Og8ldDL5JaPCXr+tMg=",
		},
		{
			// An empty body must be indistinguishable from no body.
			name:     "empty body equals absent body",
			method:   "GET",
			uri:      "/api/v2/extensions",
			datetime: "2026-08-03T14:31:07Z",
			body:     ptr(""),
			want:     "znoFJpYtxzcwG9oy54kD+jUgMr+aKbLMiy8g8LBYlRI=",
		},
		{
			// The truncation is a byte slice, not a parse: an offset instead of
			// Z leaves the first 13 characters untouched.
			name:     "RFC3339 offset instead of Z",
			method:   "GET",
			uri:      "/api/v2/extensions",
			datetime: "2026-08-03T14:31:07+02:00",
			want:     "znoFJpYtxzcwG9oy54kD+jUgMr+aKbLMiy8g8LBYlRI=",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body != nil {
				body = strings.NewReader(*tc.body)
			}
			var got []byte
			var err error
			if body == nil {
				got, err = signature(testKey, tc.method, tc.uri, tc.datetime, nil)
			} else {
				got, err = signature(testKey, tc.method, tc.uri, tc.datetime, body)
			}
			if err != nil {
				t.Fatalf("signature returned error: %v", err)
			}
			if enc := base64.StdEncoding.EncodeToString(got); enc != tc.want {
				t.Errorf("signature mismatch\n got: %s\nwant: %s", enc, tc.want)
			}
		})
	}
}

// The concatenation in step 1 has no delimiter, so ("GE", "T/api/v2/extensions")
// signs identically to ("GET", "/api/v2/extensions").
//
// This is a property of the scheme, not a defect in this library, and it is
// pinned here so that a future change to the chain shows up as a failing test
// rather than as opaque 401s. With real HTTP methods it is not reachable.
func TestSignConcatenationHasNoDelimiter(t *testing.T) {
	a, err := signature(testKey, "GET", "/api/v2/extensions", "2026-08-03T14:31:07Z", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := signature(testKey, "GE", "T/api/v2/extensions", "2026-08-03T14:31:07Z", nil)
	if err != nil {
		t.Fatal(err)
	}
	if base64.StdEncoding.EncodeToString(a) != base64.StdEncoding.EncodeToString(b) {
		t.Error("expected the split of method and URI to be invisible to the signature")
	}
}

func TestSignRejectsShortDatetime(t *testing.T) {
	_, err := signature(testKey, "GET", "/api/v2/extensions", "2026-08-03", nil)
	if err == nil {
		t.Fatal("expected an error for a datetime shorter than 13 characters")
	}
}

// Sign is the exported entry point and must agree with signature for the same
// instant. time.RFC3339 formatting is what feeds the truncation.
func TestSignMatchesSignatureForSameInstant(t *testing.T) {
	at := time.Date(2026, 8, 3, 14, 31, 7, 0, time.UTC)
	got, err := Sign(testKey, "GET", "/api/v2/extensions", at, nil)
	if err != nil {
		t.Fatal(err)
	}
	const want = "znoFJpYtxzcwG9oy54kD+jUgMr+aKbLMiy8g8LBYlRI="
	if got != want {
		t.Errorf("Sign mismatch\n got: %s\nwant: %s", got, want)
	}
}

func ptr(s string) *string { return &s }
