package client_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/saluc28/bhgraph"
	"github.com/saluc28/bhgraph/client"
)

// The API uses signed requests rather than bearer tokens. Get the token id and
// key from Settings, then My Profile, then API Key Management; the key is shown
// only once, at creation.
func ExampleNew() {
	c, err := client.New("http://127.0.0.1:8080", os.Getenv("BH_TOKEN_ID"), os.Getenv("BH_TOKEN_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// Every call takes a context and hands back the raw JSON body.
	body, err := c.ListExtensions(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(body), "bytes of installed schemas")
}

// Sign is exported so that a request this library does not wrap can still be
// signed. The uri must be the full request target, path and query: the server
// validates against request.RequestURI, so signing the path alone answers 401
// as soon as a request carries a parameter.
//
// The datetime is truncated to the hour, so every signature in the same hour
// over the same request is identical.
func ExampleSign() {
	at := time.Date(2026, 8, 3, 14, 31, 7, 0, time.UTC)

	sig, err := client.Sign("bhgraph-test-key", "GET", "/api/v2/extensions", at, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(sig)
	// Output:
	// znoFJpYtxzcwG9oy54kD+jUgMr+aKbLMiy8g8LBYlRI=
}

// Installing a schema is what turns a generic graph into a structured one, and
// only structured graphs take part in the UI's pathfinding.
//
// The endpoint sits behind the opengraph_extension_management feature flag,
// which is off by default: with it off the route is not registered at all and
// the answer is a bare 404. This library says so in the error.
func ExampleClient_InstallExtension() {
	c, err := client.New("http://127.0.0.1:8080", os.Getenv("BH_TOKEN_ID"), os.Getenv("BH_TOKEN_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	schema := bhgraph.Extension{
		Schema:    bhgraph.SchemaMeta{Name: "example", Version: "v0.0.1", Namespace: "EX"},
		NodeKinds: []bhgraph.NodeKind{{Name: "EX_Person", IsDisplayKind: true}},
		RelationshipKinds: []bhgraph.RelationshipKind{
			{Name: "EX_CanWrite", IsTraversable: true},
		},
	}

	if err := c.InstallExtension(context.Background(), schema); err != nil {
		log.Fatal(err)
	}
}

// Ingest is three calls, and all three matter: skipping the last one leaves the
// job open and nothing is processed, which looks exactly like an upload that
// succeeded and quietly did nothing.
//
// Processing is asynchronous. A successful Ingest means the payload was
// accepted, not that the graph is queryable yet.
func ExampleClient_Ingest() {
	c, err := client.New("http://127.0.0.1:8080", os.Getenv("BH_TOKEN_ID"), os.Getenv("BH_TOKEN_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	g := bhgraph.Graph{}
	g.AddNode(bhgraph.Node{ID: "alice", Kinds: []string{"EX_Person"}})
	g.AddNode(bhgraph.Node{ID: "infra", Kinds: []string{"EX_Repo"}})
	g.AddEdge(bhgraph.Edge{
		Kind:  "EX_CanWrite",
		Start: bhgraph.NodeRef("alice"),
		End:   bhgraph.NodeRef("infra"),
	})
	g.UppercaseIDs()

	jobID, err := c.Ingest(context.Background(), g)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("job %d accepted\n", jobID)
}

// A schema is easy to install and hard to verify. This is the call that tells
// "I declared the edge traversable" apart from "the edge is traversable": with
// onlyTraversable set, BloodHound walks only the kinds the schema marked
// traversable, which is the same search the UI performs.
//
// The ids are the ones from the payload, uppercased, because BloodHound
// uppercases them on ingest.
func ExampleClient_ShortestPath() {
	c, err := client.New("http://127.0.0.1:8080", os.Getenv("BH_TOKEN_ID"), os.Getenv("BH_TOKEN_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// An empty result is a normal answer: it means no path exists under the
	// constraints given, which for a non-traversable edge is the point.
	path, err := c.ShortestPath(context.Background(), "ALICE", "INFRA", true)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(path), "bytes of path graph")
}

// Two feature flags decide whether the rest of this library behaves the way its
// documentation says, and both are worth checking before blaming the payload.
// With opengraph_extension_management off, a shortest path across your own edges
// comes back empty even when the graph is right, because the server answers from
// the built-in AD and Azure kinds and never looks at an installed schema.
func ExampleClient_FeatureEnabled() {
	c, err := client.New("http://127.0.0.1:8080", os.Getenv("BH_TOKEN_ID"), os.Getenv("BH_TOKEN_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	on, err := c.FeatureEnabled(context.Background(), client.FeatureFlagExtensions)
	if err != nil {
		log.Fatal(err)
	}
	if !on {
		log.Fatalf("turn %s on under Administration, then Early Access Features", client.FeatureFlagExtensions)
	}
	fmt.Println("pathfinding will consider the edges of an installed schema")
}

// WithSpoolThreshold decides where a payload waits while it is signed. The
// signature covers the body, so the body has to be readable in full before the
// request goes out; below the threshold that means memory, above it a temporary
// file that is removed when the upload finishes.
func ExampleWithSpoolThreshold() {
	c, err := client.New(
		"http://127.0.0.1:8080",
		os.Getenv("BH_TOKEN_ID"),
		os.Getenv("BH_TOKEN_KEY"),
		client.WithSpoolThreshold(1<<20), // spill above 1 MiB instead of the default 8
	)
	if err != nil {
		log.Fatal(err)
	}

	// Ingest is the only call it affects: the others have no body worth
	// spooling.
	jobID, err := c.Ingest(context.Background(), collectGraph())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("job %d accepted\n", jobID)
}

// collectGraph stands in for whatever a collector builds.
func collectGraph() bhgraph.Graph {
	g := bhgraph.Graph{}
	g.AddNode(bhgraph.Node{ID: "ALICE", Kinds: []string{"EX_Person"}})
	return g
}
