package client

import (
	"context"
	"fmt"
	"net/url"
)

// ShortestPath returns the shortest path graph between two nodes, identified by
// their object ids.
//
// The ids are the ones from the ingested payload, UPPERCASED, because
// BloodHound uppercases them on ingest. Sending one in its original case
// answers 500 "not found". See bhgraph.Graph.UppercaseIDs.
//
// onlyTraversable is the reason this method is here in a library that otherwise
// does not wrap the BloodHound API. A schema is easy to install and hard to
// verify: with onlyTraversable set BloodHound walks only the kinds the schema
// marked traversable, which is the same search the UI performs, and that is
// what separates "I declared the edge traversable" from "the edge is
// traversable".
//
// An empty result is a normal answer, not an error: it means no path exists
// under the constraints given.
func (c *Client) ShortestPath(ctx context.Context, startID, endID string, onlyTraversable bool) ([]byte, error) {
	if startID == "" || endID == "" {
		return nil, fmt.Errorf("bhgraph/client: shortest path needs both a start and an end node id")
	}
	q := url.Values{}
	q.Set("start_node", startID)
	q.Set("end_node", endID)
	if onlyTraversable {
		q.Set("only_traversable", "true")
	}
	// The query string is part of what gets signed: the server validates against
	// request.RequestURI. This endpoint is the reason we found that out, since
	// it is the first one here that takes parameters at all.
	return c.do(ctx, "GET", "/api/v2/graphs/shortest-path?"+q.Encode(), nil)
}
