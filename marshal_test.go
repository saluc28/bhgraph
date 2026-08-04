package bhgraph

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// sampleGraph is the graph that BloodHound CE v9.5.1 actually accepted and
// processed on 2026-08-03, in exactly the form it was sent.
//
//	alice --MemberOf--> platform --CanWrite--> repo-infra   (traversable)
//	alice --Watches--> repo-secret                          (not traversable)
//
// The ids are lower case here because that is what was uploaded. The server
// stored them upper cased, which is what Graph.UppercaseIDs and the note on
// Node.ID are about. The golden file records what was sent, not what we would
// recommend sending, because its job is to be evidence.
func sampleGraph() Graph {
	g := Graph{}
	g.AddNode(Node{ID: "bhgs-alice", Kinds: []string{"BHGS_Person"},
		Properties: map[string]any{"name": "ALICE@BHGRAPH.TEST", "displayname": "Alice"}})
	g.AddNode(Node{ID: "bhgs-team-platform", Kinds: []string{"BHGS_Team"},
		Properties: map[string]any{"name": "PLATFORM@BHGRAPH.TEST", "displayname": "Platform"}})
	g.AddNode(Node{ID: "bhgs-repo-infra", Kinds: []string{"BHGS_Repo"},
		Properties: map[string]any{"name": "INFRA@BHGRAPH.TEST", "displayname": "infra"}})
	g.AddNode(Node{ID: "bhgs-repo-secret", Kinds: []string{"BHGS_Repo"},
		Properties: map[string]any{"name": "SECRET@BHGRAPH.TEST", "displayname": "secret"}})

	g.AddEdge(Edge{Kind: "BHGS_MemberOf", Start: NodeRef("bhgs-alice"), End: NodeRef("bhgs-team-platform")})
	g.AddEdge(Edge{Kind: "BHGS_CanWrite", Start: NodeRef("bhgs-team-platform"), End: NodeRef("bhgs-repo-infra")})
	g.AddEdge(Edge{Kind: "BHGS_Watches", Start: NodeRef("bhgs-alice"), End: NodeRef("bhgs-repo-secret")})
	return g
}

// The golden payload is not "whatever our serializer produces": it is a payload
// a real BloodHound accepted and processed. Regenerate it with -update only
// after a fresh acceptance, otherwise the file stops being evidence and becomes
// a tautology.
func TestMarshalMatchesGolden(t *testing.T) {
	got, err := sampleGraph().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join("testdata", "golden", "payload.json")
	if *update {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, got, "", "  "); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(pretty.Bytes(), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden file rewritten: %s", path)
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	// Compare semantically, so that gofmt-style whitespace in the golden file
	// does not make the test about formatting.
	var gotAny, wantAny any
	if err := json.Unmarshal(got, &gotAny); err != nil {
		t.Fatalf("our own output is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(want, &wantAny); err != nil {
		t.Fatalf("golden file is not valid JSON: %v", err)
	}
	gotNorm, _ := json.Marshal(gotAny)
	wantNorm, _ := json.Marshal(wantAny)
	if !bytes.Equal(gotNorm, wantNorm) {
		t.Errorf("payload does not match the golden file\n got: %s\nwant: %s", gotNorm, wantNorm)
	}
}

// WriteTo is the streaming path and MarshalJSON is built on it; they must not
// be able to drift apart.
func TestWriteToMatchesMarshalJSON(t *testing.T) {
	g := sampleGraph()
	marshalled, err := g.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	n, err := g.WriteTo(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), marshalled) {
		t.Error("WriteTo and MarshalJSON produced different bytes")
	}
	if n != int64(buf.Len()) {
		t.Errorf("WriteTo reported %d bytes, wrote %d", n, buf.Len())
	}
}

// A Ref left without MatchBy must serialize as id matching: callers build refs
// with a bare Value and expect the common case to work.
func TestMarshalDefaultsMatchByToID(t *testing.T) {
	g := Graph{
		Nodes: []Node{{ID: "a", Kinds: []string{"X"}}},
		Edges: []Edge{{Kind: "X_Edge", Start: Ref{Value: "a"}, End: Ref{Value: "a"}}},
	}
	out, err := g.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), `"match_by":"id"`) != 2 {
		t.Errorf("expected both endpoints to default to id matching, got: %s", out)
	}
}

func TestMarshalEmptyGraph(t *testing.T) {
	out, err := Graph{}.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"graph":{"nodes":[],"edges":[]}}`
	if string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}

func TestUppercaseIDs(t *testing.T) {
	g := Graph{
		Nodes: []Node{{ID: "bhgs-alice", Kinds: []string{"X"}}},
		Edges: []Edge{{
			Kind:  "X_Edge",
			Start: NodeRef("bhgs-alice"),
			End:   Ref{Value: "keep@this.case", MatchBy: MatchByName},
		}},
	}
	g.UppercaseIDs()

	if g.Nodes[0].ID != "BHGS-ALICE" {
		t.Errorf("node id not uppercased: %q", g.Nodes[0].ID)
	}
	if g.Edges[0].Start.Value != "BHGS-ALICE" {
		t.Errorf("id-matched endpoint not uppercased: %q", g.Edges[0].Start.Value)
	}
	// Name matching resolves against server-side data whose case we do not
	// control, so leaving it alone is the whole point.
	if g.Edges[0].End.Value != "keep@this.case" {
		t.Errorf("name-matched endpoint should be left alone, got %q", g.Edges[0].End.Value)
	}
}
