package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"hash"
	"io"
	"time"
)

// Authorization scheme used by the Authorization header.
const AuthorizationScheme = "bhesignature"

// Header names required on every signed request.
const (
	HeaderAuthorization = "Authorization"
	HeaderRequestDate   = "RequestDate"
	HeaderSignature     = "Signature"
)

// Sign computes the BloodHound request signature.
//
// The scheme is a chain of three HMAC-SHA-256 digests, each one keyed by the
// digest before it:
//
//  1. method and URI concatenated with no delimiter, keyed by the token key
//  2. the RFC3339 datetime truncated to the hour, keyed by digest 1
//  3. the request body, keyed by digest 2
//
// The result is base64-encoded for the Signature header.
//
// Two details are easy to get wrong and are covered by the vectors in
// sign_test.go. The truncation is a slice of the first 13 bytes of the
// formatted string, not a parse-and-reformat: "2026-08-03T14:31:07Z" becomes
// "2026-08-03T14". And the concatenation in step 1 has no separator, so the
// signature does not record where the method ends and the URI begins.
//
// uri must be the full request target, path and query string, which is what Go
// exposes as URL.RequestURI() rather than URL.Path.
//
// BloodHound's own code disagrees with itself here, verified against CE v9.5.1
// on 2026-08-03. The server validates with request.RequestURI
// (cmd/api/src/api/auth.go), which includes the query. The client helper it
// ships signs request.URL.Path (cmd/api/src/api/signature.go), which does not.
// For requests without a query the two are identical and nothing shows; add one
// query parameter and the server answers 401 "signature digest mismatch". The
// Python client published with BloodHound's documentation signs the full URI and
// agrees with the server, so the Go helper is the one that differs. We follow the
// server, and the mismatch is reported as SpecterOps/BloodHound#3098.
//
// A nil body and an empty body produce the same signature: the third digest is
// computed either way, with nothing written to it.
//
// Signatures are time-sensitive; BloodHound accepts them for at most two hours.
func Sign(tokenKey, method, uri string, at time.Time, body io.Reader) (string, error) {
	digest, err := signature(tokenKey, method, uri, at.Format(time.RFC3339), body)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(digest), nil
}

// signature is Sign without the base64 step, taking the datetime pre-formatted.
// Split out so tests can pin the exact string that gets truncated.
func signature(tokenKey, method, uri, datetime string, body io.Reader) ([]byte, error) {
	if len(datetime) < 13 {
		return nil, &SignError{Reason: "datetime is shorter than the 13 characters the scheme truncates to: " + datetime}
	}

	// 1. operation: method and URI, no delimiter, keyed by the token key.
	digester := hmac.New(sha256.New, []byte(tokenKey))
	if _, err := digester.Write([]byte(method + uri)); err != nil {
		return nil, &SignError{Reason: "writing operation key", Err: err}
	}

	// 2. date: truncated to the hour, keyed by the previous digest.
	digester = hmac.New(sha256.New, digester.Sum(nil))
	if _, err := digester.Write([]byte(datetime[:13])); err != nil {
		return nil, &SignError{Reason: "writing date key", Err: err}
	}

	// 3. body, keyed by the previous digest. Computed even when absent.
	digester = hmac.New(sha256.New, digester.Sum(nil))
	if body != nil {
		if _, err := io.Copy(digester, body); err != nil {
			return nil, &SignError{Reason: "reading request body", Err: err}
		}
	}

	return digester.Sum(nil), nil
}

// BodySigner computes a request signature while the body is being produced,
// instead of after it exists.
//
// The first two links of the chain, the operation key and the date key, depend
// only on the method, the URI and the timestamp, so they can be computed up
// front. The third link is keyed by the second and consumes the body, which
// means the body can be streamed into it as it is generated.
//
// This is what lets an ingest keep memory flat: serialize the graph once into
// an io.MultiWriter that feeds both the outgoing payload and the signer, rather
// than building the whole document in memory so it can be hashed and then sent.
//
// A BodySigner is single-use. Writing nothing is valid and produces the
// signature of an empty body.
type BodySigner struct {
	digester hash.Hash
}

// NewBodySigner starts a signature for the given request, ready to accept the
// body through Write.
//
// uri must be the full request target, path and query. See Sign.
func NewBodySigner(tokenKey, method, uri string, at time.Time) (*BodySigner, error) {
	datetime := at.Format(time.RFC3339)
	if len(datetime) < 13 {
		return nil, &SignError{Reason: "datetime is shorter than the 13 characters the scheme truncates to: " + datetime}
	}

	digester := hmac.New(sha256.New, []byte(tokenKey))
	if _, err := digester.Write([]byte(method + uri)); err != nil {
		return nil, &SignError{Reason: "writing operation key", Err: err}
	}

	digester = hmac.New(sha256.New, digester.Sum(nil))
	if _, err := digester.Write([]byte(datetime[:13])); err != nil {
		return nil, &SignError{Reason: "writing date key", Err: err}
	}

	return &BodySigner{digester: hmac.New(sha256.New, digester.Sum(nil))}, nil
}

// Write feeds body bytes into the signature. It never returns an error, so a
// BodySigner is safe to place in an io.MultiWriter.
func (s *BodySigner) Write(p []byte) (int, error) {
	return s.digester.Write(p)
}

// Signature returns the base64-encoded signature for everything written so far.
func (s *BodySigner) Signature() string {
	return base64.StdEncoding.EncodeToString(s.digester.Sum(nil))
}

// SignError reports a failure to compute a request signature.
type SignError struct {
	Reason string
	Err    error
}

func (e *SignError) Error() string {
	if e.Err != nil {
		return "bhgraph/client: " + e.Reason + ": " + e.Err.Error()
	}
	return "bhgraph/client: " + e.Reason
}

func (e *SignError) Unwrap() error { return e.Err }
