package bhgraph

import (
	"errors"
	"strings"
	"testing"
)

// One case per check, plus the cases that must NOT be rejected. A validator
// with only failing cases is half tested: the expensive mistake is rejecting
// something valid, because the caller cannot work around it.
func TestGraphValidate(t *testing.T) {
	validNode := Node{ID: "n1", Kinds: []string{"X_Person"}}

	cases := []struct {
		name    string
		graph   Graph
		wantErr string // substring; empty means the graph must be accepted
	}{
		{
			name: "valid graph",
			graph: Graph{
				Nodes: []Node{validNode, {ID: "n2", Kinds: []string{"X_Repo"}}},
				Edges: []Edge{{Kind: "X_CanWrite", Start: NodeRef("n1"), End: NodeRef("n2")}},
			},
		},
		{
			name:    "node with empty id",
			graph:   Graph{Nodes: []Node{{ID: "", Kinds: []string{"X_Person"}}}},
			wantErr: "empty id",
		},
		{
			name:    "node with no kinds",
			graph:   Graph{Nodes: []Node{{ID: "n1"}}},
			wantErr: "no kinds",
		},
		{
			name:    "node with an empty kind string",
			graph:   Graph{Nodes: []Node{{ID: "n1", Kinds: []string{""}}}},
			wantErr: "empty kind",
		},
		{
			name: "duplicate node id",
			graph: Graph{Nodes: []Node{
				{ID: "n1", Kinds: []string{"X_Person"}},
				{ID: "n1", Kinds: []string{"X_Repo"}},
			}},
			wantErr: "duplicate id",
		},
		{
			name: "edge with empty kind",
			graph: Graph{
				Nodes: []Node{validNode},
				Edges: []Edge{{Kind: "", Start: NodeRef("n1"), End: NodeRef("n1")}},
			},
			wantErr: "empty kind",
		},
		{
			name: "edge pointing at a node that is not in the payload",
			graph: Graph{
				Nodes: []Node{validNode},
				Edges: []Edge{{Kind: "X_CanWrite", Start: NodeRef("n1"), End: NodeRef("ghost")}},
			},
			wantErr: `end "ghost" is not a node in this payload`,
		},
		{
			name: "edge with an empty endpoint value",
			graph: Graph{
				Nodes: []Node{validNode},
				Edges: []Edge{{Kind: "X_CanWrite", Start: NodeRef("n1"), End: Ref{}}},
			},
			wantErr: "empty end value",
		},
		{
			// Matching by name is resolved server-side against data we cannot
			// see, so a name endpoint absent from the payload is not an error.
			name: "endpoint matched by name is not checked locally",
			graph: Graph{
				Nodes: []Node{validNode},
				Edges: []Edge{{
					Kind:  "X_CanWrite",
					Start: NodeRef("n1"),
					End:   Ref{Value: "SOMETHING@ELSEWHERE", MatchBy: MatchByName},
				}},
			},
		},
		{
			// A Ref built without MatchBy means id matching, and must still be
			// checked: leaving it unchecked is how a typo reaches the server.
			name: "zero MatchBy is treated as id matching",
			graph: Graph{
				Nodes: []Node{validNode},
				Edges: []Edge{{Kind: "X_CanWrite", Start: Ref{Value: "n1"}, End: Ref{Value: "ghost"}}},
			},
			wantErr: "is not a node in this payload",
		},
		{
			name:  "empty graph is valid",
			graph: Graph{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.graph.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the graph to be accepted, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error does not mention the problem\n got: %v\nwant substring: %q", err, tc.wantErr)
			}
		})
	}
}

// Every problem must be reported, not just the first: a caller fixing one
// mistake per round trip is why validation feels worse than no validation.
func TestGraphValidateReportsEveryProblem(t *testing.T) {
	g := Graph{
		Nodes: []Node{{ID: "", Kinds: nil}, {ID: "n2"}},
		Edges: []Edge{{Kind: ""}},
	}
	err := g.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a *ValidationError, got %T", err)
	}
	if len(ve.Problems) < 4 {
		t.Errorf("expected at least 4 problems, got %d: %v", len(ve.Problems), ve.Problems)
	}
}

func TestExtensionValidate(t *testing.T) {
	valid := func() Extension {
		return Extension{
			Schema:    SchemaMeta{Name: "test", DisplayName: "Test", Version: "v0.0.1", Namespace: "TST"},
			NodeKinds: []NodeKind{{Name: "TST_Person", IsDisplayKind: true}},
			RelationshipKinds: []RelationshipKind{
				{Name: "TST_CanWrite", IsTraversable: true},
			},
			Environments: []Environment{
				{EnvironmentKind: "TST_Person", SourceKind: "TST", PrincipalKinds: []string{"TST_Person"}},
			},
		}
	}

	cases := []struct {
		name    string
		mutate  func(*Extension)
		wantErr string
	}{
		{name: "valid extension", mutate: func(*Extension) {}},
		{
			name:    "empty schema name",
			mutate:  func(e *Extension) { e.Schema.Name = "" },
			wantErr: "empty name",
		},
		{
			name:    "empty version",
			mutate:  func(e *Extension) { e.Schema.Version = "" },
			wantErr: "empty version",
		},
		{
			name:    "empty namespace",
			mutate:  func(e *Extension) { e.Schema.Namespace = "" },
			wantErr: "empty namespace",
		},
		{
			name:    "no node kinds",
			mutate:  func(e *Extension) { e.NodeKinds = nil },
			wantErr: "no node kinds",
		},
		{
			// BloodHound refuses a schema over a single kind outside the
			// namespace, and names only the first one it finds.
			name:    "node kind outside the declared namespace",
			mutate:  func(e *Extension) { e.NodeKinds[0].Name = "Person" },
			wantErr: "does not carry the declared namespace",
		},
		{
			name:    "relationship kind outside the declared namespace",
			mutate:  func(e *Extension) { e.RelationshipKinds[0].Name = "CanWrite" },
			wantErr: "does not carry the declared namespace",
		},
		{
			// The server appends the underscore to the namespace, so a namespace
			// that already ends with one makes it look for TST__Person.
			name:    "namespace declared with the underscore",
			mutate:  func(e *Extension) { e.Schema.Namespace = "TST_" },
			wantErr: `does not carry the declared namespace "TST_" followed by an underscore`,
		},
		{
			name:    "kind that starts with the namespace but not the underscore",
			mutate:  func(e *Extension) { e.RelationshipKinds[0].Name = "TSTCanWrite" },
			wantErr: "does not carry the declared namespace",
		},
		{
			name:    "kind with nothing after the namespace prefix",
			mutate:  func(e *Extension) { e.RelationshipKinds[0].Name = "TST_" },
			wantErr: "nothing follows the namespace prefix",
		},
		{
			name:    "environment kind outside the declared namespace",
			mutate:  func(e *Extension) { e.Environments[0].EnvironmentKind = "Person" },
			wantErr: `environment_kind "Person": does not carry the declared namespace`,
		},
		{
			name: "duplicate kind name",
			mutate: func(e *Extension) {
				e.NodeKinds = append(e.NodeKinds, NodeKind{Name: "TST_Person", IsDisplayKind: true})
			},
			wantErr: "duplicate kind name",
		},
		{
			name:    "no display kind",
			mutate:  func(e *Extension) { e.NodeKinds[0].IsDisplayKind = false },
			wantErr: "is_display_kind",
		},
		{
			name:    "environment referencing an undeclared principal kind",
			mutate:  func(e *Extension) { e.Environments[0].PrincipalKinds = []string{"TST_Ghost"} },
			wantErr: "is not a declared node kind",
		},
		{
			name:    "environment with no principal kinds",
			mutate:  func(e *Extension) { e.Environments[0].PrincipalKinds = nil },
			wantErr: "no principal kinds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := valid()
			tc.mutate(&e)
			err := e.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the extension to be accepted, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error does not mention the problem\n got: %v\nwant substring: %q", err, tc.wantErr)
			}
		})
	}
}

// Every kind used in the payload must be declared in the schema. An undeclared
// kind is accepted by the ingest endpoint and then fails to show up in the UI,
// which is a slow way to find a typo.
func TestGraphValidateAgainstSchema(t *testing.T) {
	ext := Extension{
		Schema:            SchemaMeta{Name: "test", Version: "v0.0.1", Namespace: "TST"},
		NodeKinds:         []NodeKind{{Name: "TST_Person", IsDisplayKind: true}, {Name: "TST_Repo"}},
		RelationshipKinds: []RelationshipKind{{Name: "TST_CanWrite", IsTraversable: true}},
	}

	t.Run("declared kinds pass", func(t *testing.T) {
		g := Graph{
			Nodes: []Node{{ID: "a", Kinds: []string{"TST_Person"}}, {ID: "b", Kinds: []string{"TST_Repo"}}},
			Edges: []Edge{{Kind: "TST_CanWrite", Start: NodeRef("a"), End: NodeRef("b")}},
		}
		if err := g.ValidateAgainst(ext); err != nil {
			t.Fatalf("expected the graph to be accepted, got: %v", err)
		}
	})

	t.Run("undeclared node kind is rejected", func(t *testing.T) {
		g := Graph{Nodes: []Node{{ID: "a", Kinds: []string{"TST_Ghost"}}}}
		err := g.ValidateAgainst(ext)
		if err == nil || !strings.Contains(err.Error(), `kind "TST_Ghost" is not declared`) {
			t.Fatalf("expected an undeclared node kind error, got: %v", err)
		}
	})

	t.Run("undeclared edge kind is rejected", func(t *testing.T) {
		g := Graph{
			Nodes: []Node{{ID: "a", Kinds: []string{"TST_Person"}}, {ID: "b", Kinds: []string{"TST_Repo"}}},
			Edges: []Edge{{Kind: "TST_Ghost", Start: NodeRef("a"), End: NodeRef("b")}},
		}
		err := g.ValidateAgainst(ext)
		if err == nil || !strings.Contains(err.Error(), `kind "TST_Ghost" is not declared`) {
			t.Fatalf("expected an undeclared edge kind error, got: %v", err)
		}
	})

	t.Run("structural problems are reported alongside schema problems", func(t *testing.T) {
		g := Graph{
			Nodes: []Node{{ID: "", Kinds: []string{"TST_Ghost"}}},
		}
		err := g.ValidateAgainst(ext)
		if err == nil {
			t.Fatal("expected errors")
		}
		if !strings.Contains(err.Error(), "empty id") || !strings.Contains(err.Error(), "not declared") {
			t.Errorf("expected both a structural and a schema problem, got: %v", err)
		}
	})
}

func TestTraversableKinds(t *testing.T) {
	e := Extension{RelationshipKinds: []RelationshipKind{
		{Name: "TST_A", IsTraversable: true},
		{Name: "TST_B", IsTraversable: false},
		{Name: "TST_C", IsTraversable: true},
	}}
	got := e.TraversableKinds()
	want := []string{"TST_A", "TST_C"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
