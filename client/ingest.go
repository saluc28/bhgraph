package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/saluc28/bhgraph"
)

// JobID identifies an ingest job.
type JobID int64

// Ingest uploads a graph and closes the job, returning its id.
//
// Ingest is three calls, and all three matter:
//
//  1. POST /api/v2/file-upload/start   creates the job
//  2. POST /api/v2/file-upload/{id}    uploads the payload
//  3. POST /api/v2/file-upload/{id}/end  closes it and starts processing
//
// Skipping the third leaves the job open and nothing is processed, which looks
// exactly like a successful upload that quietly did nothing.
//
// The graph is validated before the first call. If an extension schema is
// available, prefer validating with ValidateAgainst beforehand: an undeclared
// kind is accepted here and then fails to appear in the UI.
func (c *Client) Ingest(ctx context.Context, g bhgraph.Graph) (JobID, error) {
	if err := g.Validate(); err != nil {
		return 0, fmt.Errorf("bhgraph/client: refusing to ingest an invalid graph: %w", err)
	}

	id, err := c.StartJob(ctx)
	if err != nil {
		return 0, err
	}
	if err := c.UploadGraph(ctx, id, g); err != nil {
		return id, err
	}
	if err := c.EndJob(ctx, id); err != nil {
		return id, err
	}
	return id, nil
}

// StartJob creates an ingest job.
func (c *Client) StartJob(ctx context.Context) (JobID, error) {
	resp, err := c.do(ctx, "POST", "/api/v2/file-upload/start", nil)
	if err != nil {
		return 0, err
	}
	var out struct {
		Data struct {
			ID JobID `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return 0, fmt.Errorf("bhgraph/client: decoding job id from %q: %w", truncate(string(resp), 200), err)
	}
	if out.Data.ID == 0 {
		return 0, fmt.Errorf("bhgraph/client: job started but no id in response: %s", truncate(string(resp), 200))
	}
	return out.Data.ID, nil
}

// UploadGraph sends the payload for an open job.
//
// Content-Type must be application/json (or application/zip); BloodHound
// rejects the upload without it. X-File-Upload-Name is optional but makes
// server-side errors readable, which is worth the one header.
//
// The graph is serialized exactly once, into an io.MultiWriter feeding both a
// spool and a BodySigner. That is what keeps memory flat: the signature covers
// the body, so the body must exist in full before the request goes out, but
// "in full" can mean a temporary file rather than a buffer. Payloads under
// WithSpoolThreshold never touch disk.
//
// Without this, Graph.WriteTo would stream while the upload consuming it
// buffered everything, which would undo the streaming one call later.
func (c *Client) UploadGraph(ctx context.Context, id JobID, g bhgraph.Graph) error {
	path := fmt.Sprintf("/api/v2/file-upload/%d", id)
	at := time.Now()

	signer, err := NewBodySigner(c.tokenKey, "POST", path, at)
	if err != nil {
		return err
	}

	sp := newSpool(c.spoolThreshold)
	// If the temporary file cannot be removed we have leaked a file, which is
	// worth neither failing an otherwise successful upload nor masking the real
	// error when the upload failed.
	defer func() { _ = sp.close() }()

	if _, err := g.WriteTo(io.MultiWriter(sp, signer)); err != nil {
		return fmt.Errorf("bhgraph/client: encoding graph: %w", err)
	}

	body, err := sp.reader()
	if err != nil {
		return err
	}

	_, err = c.doStream(ctx, streamRequest{
		method:      "POST",
		path:        path,
		body:        body,
		size:        sp.size(),
		at:          at,
		signature:   signer.Signature(),
		contentType: "application/json",
		headers:     map[string]string{"X-File-Upload-Name": "bhgraph.json"},
	})
	return err
}

// EndJob closes a job and starts processing.
func (c *Client) EndJob(ctx context.Context, id JobID) error {
	_, err := c.do(ctx, "POST", fmt.Sprintf("/api/v2/file-upload/%d/end", id), nil)
	return err
}

// JobStatus returns the raw JSON of completed ingest tasks.
//
// Processing is asynchronous: a successful Ingest means the payload was
// accepted, not that the graph is queryable yet.
func (c *Client) JobStatus(ctx context.Context, id JobID) ([]byte, error) {
	return c.do(ctx, "GET", fmt.Sprintf("/api/v2/file-upload/%d/completed-tasks", id), nil)
}
