package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/saluc28/bhgraph"
)

// pathExtensions is the extension definition schema endpoint, marked
// experimental in BloodHound's OpenAPI specification: the shape of the request
// may change between releases. Everything that touches it lives in this file so
// that a break stays local, and is re-verified on every BloodHound bump.
const pathExtensions = "/api/v2/extensions"

// FeatureFlagExtensions is the BloodHound feature flag that governs
// /api/v2/extensions.
//
// It is OFF by default, verified on CE v9.5.1. With it off the route is not
// registered at all, so requests come back 404 "resource not found" rather than
// with anything pointing at the cause. Being tagged Community means the
// endpoint is available in CE, not that it is enabled.
//
// Turn it on in the UI under Administration, Feature Management, or with
// PUT /api/v2/features/{id}/toggle after finding the id in GET /api/v2/features.
const FeatureFlagExtensions = "opengraph_extension_management"

// InstallExtension installs or updates an extension definition schema.
//
// The verb is PUT, not POST: the operation is an upsert. Installing a schema is
// what turns a generic graph into a structured one, and structured graphs are
// the only ones whose edges take part in the UI's pathfinding.
//
// The extension is validated locally first. Sending a schema BloodHound rejects
// produces errors that are harder to read than the ones Validate gives.
func (c *Client) InstallExtension(ctx context.Context, e bhgraph.Extension) error {
	if err := e.Validate(); err != nil {
		return fmt.Errorf("bhgraph/client: refusing to install an invalid extension: %w", err)
	}
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("bhgraph/client: encoding extension: %w", err)
	}
	if _, err := c.do(ctx, "PUT", pathExtensions, body); err != nil {
		return err
	}
	return nil
}

// ListExtensions returns the raw JSON of the installed extension schemas.
//
// The response shape is not modelled on purpose: the endpoint is experimental,
// and pinning a struct to it would break on a field rename that callers may not
// even care about.
func (c *Client) ListExtensions(ctx context.Context) ([]byte, error) {
	return c.do(ctx, "GET", pathExtensions, nil)
}

// DeleteExtension removes an installed extension schema by id.
func (c *Client) DeleteExtension(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("bhgraph/client: empty extension id")
	}
	_, err := c.do(ctx, "DELETE", pathExtensions+"/"+id, nil)
	return err
}
