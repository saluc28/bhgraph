// Command with-extension installs an extension definition schema, ingests a
// graph, and then proves that the schema took effect by asking BloodHound for a
// path.
//
// This is the whole point of the library in one file. Installing a schema is
// easy; knowing that "I declared this edge traversable" and "this edge is
// traversable" are the same thing is not. The three pathfinding queries at the
// end are what tells them apart.
//
//	export BH_TOKEN_ID=...
//	export BH_TOKEN_KEY=...
//	go run ./examples/with-extension
//
// Requires a BloodHound CE instance and the opengraph_extension_management
// feature flag turned on. It is off by default, and with it off the extensions
// endpoint answers 404. See the README.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/saluc28/bhgraph"
	"github.com/saluc28/bhgraph/client"
)

func schema() bhgraph.Extension {
	return bhgraph.Extension{
		Schema: bhgraph.SchemaMeta{
			Name:        "bhgraph_example",
			DisplayName: "bhgraph example",
			Version:     "v0.0.1",
			Namespace:   "EX",
		},
		NodeKinds: []bhgraph.NodeKind{
			{Name: "EX_Person", DisplayName: "Person", Description: "A person",
				IsDisplayKind: true, Icon: "user", Color: "#4287f5"},
			{Name: "EX_Team", DisplayName: "Team", Description: "A team",
				IsDisplayKind: true, Icon: "users", Color: "#42f584"},
			{Name: "EX_Repo", DisplayName: "Repository", Description: "A repository",
				IsDisplayKind: true, Icon: "code-branch", Color: "#f5a742"},
		},
		RelationshipKinds: []bhgraph.RelationshipKind{
			// Traversable: these are capabilities, and a chain of them is a path
			// someone can actually walk.
			{Name: "EX_MemberOf", Description: "Belongs to the team", IsTraversable: true},
			{Name: "EX_CanWrite", Description: "Can push to the repository", IsTraversable: true},
			// Not traversable: watching a repository grants nothing. Marking it
			// traversable would invent paths that do not exist.
			{Name: "EX_Watches", Description: "Subscribed to notifications", IsTraversable: false},
		},
		Environments: []bhgraph.Environment{
			{EnvironmentKind: "EX_Team", SourceKind: "EX",
				PrincipalKinds: []string{"EX_Person", "EX_Team"}},
		},
		RelationshipFindings: []any{},
	}
}

func graph() bhgraph.Graph {
	g := bhgraph.Graph{}
	g.AddNode(bhgraph.Node{ID: "ex-alice", Kinds: []string{"EX_Person"},
		Properties: map[string]any{"name": "ALICE@EXAMPLE.TEST", "displayname": "Alice"}})
	g.AddNode(bhgraph.Node{ID: "ex-platform", Kinds: []string{"EX_Team"},
		Properties: map[string]any{"name": "PLATFORM@EXAMPLE.TEST", "displayname": "Platform"}})
	g.AddNode(bhgraph.Node{ID: "ex-repo-infra", Kinds: []string{"EX_Repo"},
		Properties: map[string]any{"name": "INFRA@EXAMPLE.TEST", "displayname": "infra"}})
	g.AddNode(bhgraph.Node{ID: "ex-repo-secret", Kinds: []string{"EX_Repo"},
		Properties: map[string]any{"name": "SECRET@EXAMPLE.TEST", "displayname": "secret"}})

	// alice reaches infra through the team: two traversable hops.
	g.AddEdge(bhgraph.Edge{Kind: "EX_MemberOf",
		Start: bhgraph.NodeRef("ex-alice"), End: bhgraph.NodeRef("ex-platform")})
	g.AddEdge(bhgraph.Edge{Kind: "EX_CanWrite",
		Start: bhgraph.NodeRef("ex-platform"), End: bhgraph.NodeRef("ex-repo-infra")})
	// alice touches secret, but through an edge that grants nothing.
	g.AddEdge(bhgraph.Edge{Kind: "EX_Watches",
		Start: bhgraph.NodeRef("ex-alice"), End: bhgraph.NodeRef("ex-repo-secret")})

	g.UppercaseIDs()
	return g
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	tokenID, tokenKey := os.Getenv("BH_TOKEN_ID"), os.Getenv("BH_TOKEN_KEY")
	if tokenID == "" || tokenKey == "" {
		return fmt.Errorf("set BH_TOKEN_ID and BH_TOKEN_KEY (Settings, My Profile, API Key Management)")
	}

	c, err := client.New(envOr("BH_URL", "http://127.0.0.1:8080"), tokenID, tokenKey)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	ext := schema()
	if err := c.InstallExtension(ctx, ext); err != nil {
		return fmt.Errorf("installing the schema: %w", err)
	}
	fmt.Printf("schema installed. traversable kinds: %v\n", ext.TraversableKinds())

	g := graph()
	// Validating against the schema catches an undeclared kind here, rather
	// than as a node that silently fails to appear in the UI later.
	if err := g.ValidateAgainst(ext); err != nil {
		return err
	}

	jobID, err := c.Ingest(ctx, g)
	if err != nil {
		return fmt.Errorf("ingesting: %w", err)
	}
	fmt.Printf("ingested %d nodes and %d edges as job %d\n", len(g.Nodes), len(g.Edges), jobID)

	fmt.Print("waiting for processing")
	if !waitForNode(ctx, c, "EX_Person") {
		return fmt.Errorf("nodes did not appear within the timeout")
	}
	fmt.Println()

	// The point of the whole exercise.
	checks := []struct {
		what            string
		start, end      string
		onlyTraversable bool
		wantPath        bool
	}{
		{"alice -> infra, traversable only", "EX-ALICE", "EX-REPO-INFRA", true, true},
		{"alice -> secret, traversable only", "EX-ALICE", "EX-REPO-SECRET", true, false},
		{"alice -> secret, anything goes", "EX-ALICE", "EX-REPO-SECRET", false, true},
	}
	for _, ch := range checks {
		found, detail := hasPath(ctx, c, ch.start, ch.end, ch.onlyTraversable)
		status := "OK "
		if found != ch.wantPath {
			status = "BAD"
		}
		fmt.Printf("  [%s] %-36s %s\n", status, ch.what, detail)
	}
	fmt.Println("\nEX_Watches connects alice to secret, and is invisible to pathfinding because")
	fmt.Println("the schema says it is not traversable. That single flag is what a structured")
	fmt.Println("graph buys you over a generic one.")
	return nil
}

func hasPath(ctx context.Context, c *client.Client, start, end string, onlyTraversable bool) (bool, string) {
	body, err := c.ShortestPath(ctx, start, end, onlyTraversable)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, "no path"
		}
		return false, "error: " + err.Error()
	}
	var out struct {
		Data struct {
			Nodes map[string]struct {
				Label string `json:"label"`
			} `json:"nodes"`
			Edges []struct {
				Label string `json:"label"`
			} `json:"edges"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return false, "undecodable response"
	}
	if len(out.Data.Nodes) == 0 {
		return false, "no path"
	}
	var kinds []string
	for _, e := range out.Data.Edges {
		kinds = append(kinds, e.Label)
	}
	return true, fmt.Sprintf("path of %d nodes via %s", len(out.Data.Nodes), strings.Join(kinds, " + "))
}

func waitForNode(ctx context.Context, c *client.Client, kind string) bool {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		fmt.Print(".")
		if body, err := c.Cypher(ctx, "MATCH (n:"+kind+") RETURN n"); err == nil {
			if strings.Contains(string(body), "EX-ALICE") {
				return true
			}
		}
	}
	return false
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
