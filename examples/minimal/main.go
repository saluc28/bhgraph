// Command minimal builds a small OpenGraph payload and writes it to disk.
//
// No network, no credentials, no BloodHound instance: BloodHound accepts a
// .json upload from the UI, so this is a complete workflow on its own. It is
// also the fastest way to see what the library produces.
//
//	go run ./examples/minimal
package main

import (
	"fmt"
	"os"

	"github.com/saluc28/bhgraph"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	g := bhgraph.Graph{}

	g.AddNode(bhgraph.Node{
		ID:    "example-alice",
		Kinds: []string{"EX_Person"},
		Properties: map[string]any{
			"name":        "ALICE@EXAMPLE.TEST",
			"displayname": "Alice",
		},
	})
	g.AddNode(bhgraph.Node{
		ID:    "example-repo",
		Kinds: []string{"EX_Repo"},
		Properties: map[string]any{
			"name":        "INFRA@EXAMPLE.TEST",
			"displayname": "infra",
		},
	})

	g.AddEdge(bhgraph.Edge{
		Kind:  "EX_CanWrite",
		Start: bhgraph.NodeRef("example-alice"),
		End:   bhgraph.NodeRef("example-repo"),
	})

	// BloodHound uppercases object ids on ingest. Normalizing now means the ids
	// you hold here are the ones the server will know you by.
	g.UppercaseIDs()

	// Validate before writing, not after uploading: a failed ingest reports
	// errors that are poor and asynchronous.
	if err := g.Validate(); err != nil {
		return err
	}

	f, err := os.Create("payload.json")
	if err != nil {
		return err
	}

	// WriteTo streams, so a graph larger than memory is not a problem here.
	n, err := g.WriteTo(f)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("writing payload.json: %w", err)
	}
	// Closing is checked rather than deferred and ignored: a write can fail on
	// close, and a payload that is silently truncated is worse than no payload.
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing payload.json: %w", err)
	}

	fmt.Printf("wrote payload.json: %d nodes, %d edges, %d bytes\n", len(g.Nodes), len(g.Edges), n)
	fmt.Println("upload it from the BloodHound UI, or see ./examples/with-extension to do it over the API")
	return nil
}
