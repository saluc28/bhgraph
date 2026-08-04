package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/saluc28/bhgraph"
)

func TestSpoolStaysInMemoryBelowThreshold(t *testing.T) {
	s := newSpool(1024)
	defer s.close()

	payload := bytes.Repeat([]byte("a"), 512)
	if _, err := s.Write(payload); err != nil {
		t.Fatal(err)
	}
	if s.spilled() {
		t.Error("spilled to disk below the threshold")
	}
	if s.size() != 512 {
		t.Errorf("size: got %d, want 512", s.size())
	}

	r, err := s.reader()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	if !bytes.Equal(got, payload) {
		t.Error("content read back does not match what was written")
	}
}

func TestSpoolSpillsAboveThresholdAndKeepsEverything(t *testing.T) {
	s := newSpool(100)
	defer s.close()

	// Written in several chunks that cross the threshold mid-way: the bytes
	// buffered before the spill must end up in the file too, in order.
	var want []byte
	for i := 0; i < 10; i++ {
		chunk := bytes.Repeat([]byte{byte('0' + i)}, 30)
		if _, err := s.Write(chunk); err != nil {
			t.Fatal(err)
		}
		want = append(want, chunk...)
	}

	if !s.spilled() {
		t.Fatal("expected the spool to spill to disk above the threshold")
	}
	if s.size() != int64(len(want)) {
		t.Errorf("size: got %d, want %d", s.size(), len(want))
	}

	r, err := s.reader()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("content lost or reordered across the spill\n got %d bytes\nwant %d bytes", len(got), len(want))
	}
}

func TestSpoolCloseRemovesTheTempFile(t *testing.T) {
	s := newSpool(10)
	if _, err := s.Write(bytes.Repeat([]byte("x"), 100)); err != nil {
		t.Fatal(err)
	}
	if !s.spilled() {
		t.Fatal("expected a spill")
	}
	name := s.file.Name()
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Errorf("the temporary file outlived the spool: %s", name)
	}
}

// BodySigner must agree with Sign for the same inputs: they are two ways to
// compute one thing, and the streaming path is the one that is harder to check
// by eye.
func TestBodySignerAgreesWithSign(t *testing.T) {
	at := time.Date(2026, 8, 3, 14, 31, 7, 0, time.UTC)
	const body = `{"graph":{"nodes":[],"edges":[]}}`

	want, err := Sign(testTokenKey, "POST", "/api/v2/file-upload/1", at, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	signer, err := NewBodySigner(testTokenKey, "POST", "/api/v2/file-upload/1", at)
	if err != nil {
		t.Fatal(err)
	}
	// Written in pieces, as a streaming serializer would.
	for _, chunk := range []string{`{"graph":{"nodes":`, `[],"edges":`, `[]}}`} {
		if _, err := signer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}

	if got := signer.Signature(); got != want {
		t.Errorf("streaming signature differs from Sign\n got: %s\nwant: %s", got, want)
	}
}

func TestBodySignerWithNoBodyMatchesEmptyBody(t *testing.T) {
	at := time.Date(2026, 8, 3, 14, 31, 7, 0, time.UTC)

	want, err := Sign(testTokenKey, "GET", "/api/v2/extensions", at, nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewBodySigner(testTokenKey, "GET", "/api/v2/extensions", at)
	if err != nil {
		t.Fatal(err)
	}
	if got := signer.Signature(); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// The whole reason for the streaming path: a payload larger than the threshold
// must still produce a signature the server accepts, and must arrive intact.
func TestUploadGraphSpillsAndStaysCorrect(t *testing.T) {
	var receivedLen int64
	var receivedBody []byte
	var sigOK bool

	handler := func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		receivedBody = body
		receivedLen = r.ContentLength

		datetime := r.Header.Get(HeaderRequestDate)
		want := serverSignature(r.Method, r.RequestURI, datetime, body)
		sigOK = r.Header.Get(HeaderSignature) == want

		w.WriteHeader(http.StatusAccepted)
	}

	// A threshold of 1 KiB against a graph of a few hundred nodes guarantees the
	// spill path is the one under test.
	c, _ := newTestClient(t, handler, WithSpoolThreshold(1024))

	g := bhgraph.Graph{}
	for i := 0; i < 400; i++ {
		g.AddNode(bhgraph.Node{
			ID:         fmt.Sprintf("NODE-%04d", i),
			Kinds:      []string{"X_Person"},
			Properties: map[string]any{"name": fmt.Sprintf("USER%04d@EXAMPLE.TEST", i)},
		})
	}

	if err := c.UploadGraph(context.Background(), 1, g); err != nil {
		t.Fatalf("upload: %v", err)
	}

	if !sigOK {
		t.Error("the server could not verify the signature of a spilled payload")
	}

	// Content-Length must be set: without it the transport falls back to chunked
	// encoding and the server takes a different path for bodies of unknown size.
	if receivedLen != int64(len(receivedBody)) {
		t.Errorf("Content-Length %d does not match the %d bytes received", receivedLen, len(receivedBody))
	}
	if int(receivedLen) <= 1024 {
		t.Fatalf("the payload did not exceed the threshold, so the spill path was not tested (%d bytes)", receivedLen)
	}

	// And the bytes must be exactly what the graph serializes to.
	want, err := g.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(receivedBody, want) {
		t.Error("the spilled payload differs from the graph's own serialization")
	}
}
