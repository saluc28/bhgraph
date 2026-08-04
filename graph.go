// Package bhgraph builds and validates BloodHound OpenGraph payloads and
// extension definition schemas.
//
// This package has no dependencies outside the standard library and performs no
// network I/O. To upload data to a BloodHound instance, see the client
// subpackage.
package bhgraph

import "strings"

// MatchStrategy tells BloodHound how to resolve an edge endpoint to a node.
type MatchStrategy string

// Strategies for resolving an edge endpoint. MatchByID is the fastest, and is
// what a zero MatchStrategy means.
const (
	MatchByID   MatchStrategy = "id"
	MatchByName MatchStrategy = "name"
)

// Node is a single vertex of the graph.
//
// Kinds carries the node's types, which must be declared in the extension
// schema when one is installed. The first kind is conventionally the display
// kind. Properties are free-form; BloodHound gives special meaning to "name"
// and "displayname" in its UI.
//
// ID becomes the node's objectid, and BloodHound uppercases it on ingest. See
// UppercaseIDs, which keeps the id you hold and the id the server stores from
// drifting apart.
type Node struct {
	ID         string         `json:"id"`
	Kinds      []string       `json:"kinds"`
	Properties map[string]any `json:"properties,omitempty"`
}

// Ref points at one endpoint of an edge.
//
// A zero MatchBy is serialized as MatchByID, which is what callers almost
// always want and what BloodHound resolves fastest.
type Ref struct {
	Value   string        `json:"value"`
	MatchBy MatchStrategy `json:"match_by"`
}

// NodeRef returns a Ref matching a node by its ID.
func NodeRef(id string) Ref {
	return Ref{Value: id, MatchBy: MatchByID}
}

// matchesByID reports whether the ref resolves against node ids, which is what
// a zero MatchBy means. Stated once here because three places ask it and they
// have to agree: validation, uppercasing, and serialization.
func (r Ref) matchesByID() bool {
	return r.MatchBy == "" || r.MatchBy == MatchByID
}

// Edge is a directed relationship between two nodes.
//
// Whether an edge participates in BloodHound's pathfinding is not decided here:
// it is decided by IsTraversable on the matching RelationshipKind of the
// installed extension schema.
type Edge struct {
	Kind       string         `json:"kind"`
	Start      Ref            `json:"start"`
	End        Ref            `json:"end"`
	Properties map[string]any `json:"properties,omitempty"`
}

// Graph is a set of nodes and edges destined for a single ingest job.
//
// Methods that change the graph take a pointer receiver, methods that only read
// it take a value. MarshalJSON has to be one of the readers: on a pointer
// receiver, json.Marshal of a Graph value would quietly ignore it and emit the
// Go field names instead of the shape BloodHound expects.
type Graph struct {
	Nodes []Node
	Edges []Edge
}

// AddNode appends a node to the payload.
func (g *Graph) AddNode(n Node) {
	g.Nodes = append(g.Nodes, n)
}

// AddEdge appends an edge to the payload.
func (g *Graph) AddEdge(e Edge) {
	g.Edges = append(g.Edges, e)
}

// UppercaseIDs rewrites every node ID and every id-matched edge endpoint to
// upper case, so that what you send matches what BloodHound stores.
//
// BloodHound uppercases object ids during ingest, verified against CE v9.5.1: a
// node sent as "bhgs-alice" comes back as "BHGS-ALICE". That matters as soon as
// the id is used again, for pathfinding, a Cypher lookup, or correlating a
// second ingest. Passing it back in the case you sent it answers "not found",
// which reads like a missing node rather than a case mismatch.
//
// Endpoints matched by name are left alone: only ids are normalized.
func (g *Graph) UppercaseIDs() {
	for i := range g.Nodes {
		g.Nodes[i].ID = strings.ToUpper(g.Nodes[i].ID)
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Start.matchesByID() {
			e.Start.Value = strings.ToUpper(e.Start.Value)
		}
		if e.End.matchesByID() {
			e.End.Value = strings.ToUpper(e.End.Value)
		}
	}
}
