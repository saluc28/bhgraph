package bhgraph_test

import (
	"fmt"
	"os"

	"github.com/saluc28/bhgraph"
)

// Building a payload takes no credentials and no server: BloodHound accepts a
// .json upload from the UI, so this is already a complete workflow.
func Example() {
	g := bhgraph.Graph{}
	g.AddNode(bhgraph.Node{
		ID:         "alice",
		Kinds:      []string{"EX_Person"},
		Properties: map[string]any{"name": "ALICE@EXAMPLE.TEST"},
	})
	g.AddNode(bhgraph.Node{
		ID:         "infra",
		Kinds:      []string{"EX_Repo"},
		Properties: map[string]any{"name": "INFRA@EXAMPLE.TEST"},
	})
	g.AddEdge(bhgraph.Edge{
		Kind:  "EX_CanWrite",
		Start: bhgraph.NodeRef("alice"),
		End:   bhgraph.NodeRef("infra"),
	})

	// BloodHound uppercases object ids on ingest, so do it here and keep both
	// sides using the same ids.
	g.UppercaseIDs()

	if err := g.Validate(); err != nil {
		fmt.Println(err)
		return
	}
	if _, err := g.WriteTo(os.Stdout); err != nil {
		fmt.Println(err)
	}
	// Output:
	// {"graph":{"nodes":[{"id":"ALICE","kinds":["EX_Person"],"properties":{"name":"ALICE@EXAMPLE.TEST"}},{"id":"INFRA","kinds":["EX_Repo"],"properties":{"name":"INFRA@EXAMPLE.TEST"}}],"edges":[{"kind":"EX_CanWrite","start":{"value":"ALICE","match_by":"id"},"end":{"value":"INFRA","match_by":"id"}}]}}
}

// An edge endpoint that no node in the payload matches is accepted by the
// ingest endpoint and then silently produces nothing. Every problem is
// reported, not just the first, so one round trip is enough to fix them all.
func ExampleGraph_Validate() {
	g := bhgraph.Graph{
		Nodes: []bhgraph.Node{
			{ID: "alice", Kinds: []string{"EX_Person"}},
			{ID: "", Kinds: []string{"EX_Repo"}},
		},
		Edges: []bhgraph.Edge{
			{Kind: "EX_CanWrite", Start: bhgraph.NodeRef("alice"), End: bhgraph.NodeRef("ghost")},
		},
	}

	fmt.Println(g.Validate())
	// Output:
	// bhgraph: 2 validation problems:
	//   - node 1: empty id
	//   - edge 0 (EX_CanWrite): end "ghost" is not a node in this payload
}

// A kind the schema does not declare is accepted by the ingest endpoint and
// then fails to appear in the UI, with nothing to say why. Checking against the
// schema turns that into an error before anything is sent.
func ExampleGraph_ValidateAgainst() {
	schema := bhgraph.Extension{
		Schema:            bhgraph.SchemaMeta{Name: "example", Version: "v0.0.1", Namespace: "EX"},
		NodeKinds:         []bhgraph.NodeKind{{Name: "EX_Person", IsDisplayKind: true}},
		RelationshipKinds: []bhgraph.RelationshipKind{{Name: "EX_CanWrite", IsTraversable: true}},
	}

	g := bhgraph.Graph{
		Nodes: []bhgraph.Node{{ID: "alice", Kinds: []string{"EX_Persno"}}}, // typo
	}

	fmt.Println(g.ValidateAgainst(schema))
	// Output:
	// bhgraph: node "alice": kind "EX_Persno" is not declared in the schema
}

// Object ids are uppercased during ingest, which matters as soon as an id is
// used again: reusing it in its original case reads as a missing node rather
// than as a case mismatch. Endpoints matched by name are left alone, since
// their case is resolved server side.
func ExampleGraph_UppercaseIDs() {
	g := bhgraph.Graph{
		Nodes: []bhgraph.Node{{ID: "bhgs-alice", Kinds: []string{"EX_Person"}}},
		Edges: []bhgraph.Edge{{
			Kind:  "EX_MemberOf",
			Start: bhgraph.NodeRef("bhgs-alice"),
			End:   bhgraph.Ref{Value: "platform@example.test", MatchBy: bhgraph.MatchByName},
		}},
	}

	g.UppercaseIDs()

	fmt.Println(g.Nodes[0].ID)
	fmt.Println(g.Edges[0].Start.Value)
	fmt.Println(g.Edges[0].End.Value)
	// Output:
	// BHGS-ALICE
	// BHGS-ALICE
	// platform@example.test
}

// Declaring a schema is what makes a graph structured rather than generic, and
// only structured graphs take part in the UI's pathfinding. IsTraversable is
// the whole mechanism: EX_Watches connects two nodes but grants nothing, so
// marking it traversable would invent a path that does not exist.
func ExampleExtension() {
	schema := bhgraph.Extension{
		Schema: bhgraph.SchemaMeta{
			Name:        "example",
			DisplayName: "Example",
			Version:     "v0.0.1",
			Namespace:   "EX",
		},
		NodeKinds: []bhgraph.NodeKind{
			{Name: "EX_Person", DisplayName: "Person", IsDisplayKind: true, Icon: "user", Color: "#4287f5"},
			{Name: "EX_Repo", DisplayName: "Repository", IsDisplayKind: true, Icon: "code-branch", Color: "#f5a742"},
		},
		RelationshipKinds: []bhgraph.RelationshipKind{
			{Name: "EX_CanWrite", Description: "Can push to the repository", IsTraversable: true},
			{Name: "EX_Watches", Description: "Subscribed to notifications", IsTraversable: false},
		},
		Environments: []bhgraph.Environment{
			{EnvironmentKind: "EX_Repo", SourceKind: "EX", PrincipalKinds: []string{"EX_Person"}},
		},
		RelationshipFindings: []any{},
	}

	if err := schema.Validate(); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("pathfinding will walk:", schema.TraversableKinds())
	// Output:
	// pathfinding will walk: [EX_CanWrite]
}

// Reading the traversable kinds back is the cheapest way to check a schema
// before installing it, since that set is the entire difference between a graph
// the UI can walk and one it cannot.
func ExampleExtension_TraversableKinds() {
	schema := bhgraph.Extension{
		RelationshipKinds: []bhgraph.RelationshipKind{
			{Name: "EX_MemberOf", IsTraversable: true},
			{Name: "EX_Watches", IsTraversable: false},
			{Name: "EX_CanWrite", IsTraversable: true},
		},
	}

	fmt.Println(schema.TraversableKinds())
	// Output:
	// [EX_MemberOf EX_CanWrite]
}
