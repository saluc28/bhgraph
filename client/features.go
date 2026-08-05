package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// pathFeatures is the feature flag endpoint.
const pathFeatures = "/api/v2/features"

// FeatureFlagRawObjectIDs governs whether ingest keeps object ids in the case
// they were sent in.
//
// It is off by default and not user updatable on CE v9.5.1, so ingest uppercases
// object ids and a lower case id asked for later is not found. See
// bhgraph.Graph.UppercaseIDs.
const FeatureFlagRawObjectIDs = "use_raw_object_id"

// Feature is one BloodHound feature flag.
//
// The identifier and timestamps the endpoint also returns are left out: the id
// is only useful for the toggle endpoint, which this library does not call
// because turning a flag on is an operator's decision and not a collector's.
type Feature struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Enabled       bool   `json:"enabled"`
	UserUpdatable bool   `json:"user_updatable"`
}

// Features returns the feature flags the instance reports, keyed by flag key.
//
// Two of them decide whether the rest of this library behaves the way its
// documentation says. FeatureFlagExtensions governs whether /api/v2/extensions
// is routed at all, and it also governs whether pathfinding considers the edges
// of an installed schema: with it off the server answers a shortest path query
// from the built-in AD and Azure kinds only. FeatureFlagRawObjectIDs governs
// whether ingest uppercases object ids.
//
// Reading flags needs a token whose role can read the application configuration.
// A 403 here means the token is too narrow, not that the flags are absent.
func (c *Client) Features(ctx context.Context) (map[string]Feature, error) {
	body, err := c.do(ctx, "GET", pathFeatures, nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []Feature `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("bhgraph/client: decoding feature flags: %w", err)
	}
	flags := make(map[string]Feature, len(payload.Data))
	for _, f := range payload.Data {
		flags[f.Key] = f
	}
	return flags, nil
}

// FeatureEnabled reports whether the named flag is on.
//
// A key this instance does not report is an error rather than false. A missing
// flag usually means a typo or a BloodHound older than the flag, and answering
// false would be indistinguishable from a flag that is genuinely switched off.
//
// Each call reads the whole set from the server. Nothing is cached, because a
// flag can be toggled while a collector runs and a stale answer here is worse
// than a second request. Call Features once if you need several.
func (c *Client) FeatureEnabled(ctx context.Context, key string) (bool, error) {
	flags, err := c.Features(ctx)
	if err != nil {
		return false, err
	}
	flag, ok := flags[key]
	if !ok {
		return false, fmt.Errorf("bhgraph/client: this instance reports no feature flag %q", key)
	}
	return flag.Enabled, nil
}
