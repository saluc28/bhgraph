package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// Cypher runs a read-only Cypher query and returns the raw JSON response.
//
// This is how you check that an ingest produced what you expected, and how you
// confirm that traversable edges actually connect: a path that the UI finds is
// a path this query finds too.
func (c *Client) Cypher(ctx context.Context, query string) ([]byte, error) {
	if query == "" {
		return nil, fmt.Errorf("bhgraph/client: empty query")
	}
	body, err := json.Marshal(struct {
		Query             string `json:"query"`
		IncludeProperties bool   `json:"include_properties"`
	}{Query: query, IncludeProperties: true})
	if err != nil {
		return nil, fmt.Errorf("bhgraph/client: encoding query: %w", err)
	}
	return c.do(ctx, "POST", "/api/v2/graphs/cypher", body)
}
