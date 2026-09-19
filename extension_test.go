package bhgraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// MSSQLHound is SpecterOps' own OpenGraph collector, written in Go, and its
// schema.json is the only real reference implementation of an extension
// definition schema that exists publicly.
//
// Round-tripping it through our types is the sharpest test available for them:
// if SpecterOps declares a field we do not model, the re-serialized document
// loses it and this fails. A hand-written golden file would only ever confirm
// our own assumptions.
func TestExtensionRoundTripsMSSQLHoundSchema(t *testing.T) {
	path := filepath.Join("testdata", "golden", "mssqlhound-schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the reference schema: %v", err)
	}

	var ours Extension
	if err := json.Unmarshal(raw, &ours); err != nil {
		t.Fatalf("our types cannot parse a real schema: %v", err)
	}

	// What the document actually contains, with nothing filtered by our types.
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}

	// What survives a trip through our types.
	reserialized, err := json.Marshal(ours)
	if err != nil {
		t.Fatal(err)
	}
	var roundTripped map[string]any
	if err := json.Unmarshal(reserialized, &roundTripped); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(generic, roundTripped) {
		t.Errorf("the schema does not survive a round trip through our types\n"+
			"a field SpecterOps declares is probably missing from Extension\n"+
			"original:      %v\nround-tripped: %v", keysOf(generic), keysOf(roundTripped))
		reportFieldDrift(t, generic, roundTripped, "")
	}
}

// The counts this library records about the reference schema, checked rather
// than trusted. If MSSQLHound changes shape, the assumptions built on it should
// stop being quietly wrong.
func TestMSSQLHoundSchemaShape(t *testing.T) {
	path := filepath.Join("testdata", "golden", "mssqlhound-schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e Extension
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}

	if got := len(e.NodeKinds); got != 7 {
		t.Errorf("node kinds: got %d, want 7", got)
	}
	if got := len(e.RelationshipKinds); got != 35 {
		t.Errorf("relationship kinds: got %d, want 35", got)
	}
	if got := len(e.TraversableKinds()); got != 22 {
		t.Errorf("traversable kinds: got %d, want 22", got)
	}
	if e.Schema.Namespace == "" {
		t.Error("the reference schema declares a namespace; we failed to read it")
	}

	// Every kind name starts with the namespace and an underscore, the rule
	// BloodHound applies on install, so SpecterOps' own schema has to pass.
	if err := e.Validate(); err != nil {
		t.Errorf("SpecterOps' own schema does not pass our validation, so our rules are too strict: %v", err)
	}
}

// The Entity Panel section of BloodHound's own documentation, as the extension
// definition schema carries it (docs/opengraph/developer/entity-panel-content.mdx
// in SpecterOps/bloodhound-docs), round-trips through a node kind unchanged. The
// field is left out of a kind that has no sections, so a schema written before
// it existed marshals the way it always did.
func TestKindInfoRoundTripsTheDocumentedShape(t *testing.T) {
	const documented = `{
		"name": "TST_Person",
		"display_name": "Person",
		"description": "",
		"is_display_kind": true,
		"icon": "user",
		"color": "#ffffff",
		"info": {
			"overview": {
				"title": "Overview",
				"position": 1,
				"markdown": {"content": "This content appears in the Entity Panel."}
			}
		}
	}`

	var kind NodeKind
	if err := json.Unmarshal([]byte(documented), &kind); err != nil {
		t.Fatalf("our types cannot parse the documented section: %v", err)
	}
	section := kind.Info["overview"]
	if section.Title != "Overview" || section.Position != 1 || section.Markdown.Content != "This content appears in the Entity Panel." {
		t.Errorf("section = %+v, want the documented one", section)
	}

	reserialized, err := json.Marshal(kind)
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	if err := json.Unmarshal([]byte(documented), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reserialized, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("the documented section does not survive a round trip\n want: %v\n  got: %v", want, got)
	}

	plain, err := json.Marshal(RelationshipKind{Name: "TST_CanWrite"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), `"info"`) {
		t.Errorf("a kind with no sections carries an info field: %s", plain)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// reportFieldDrift walks both documents and names the first differing paths,
// so a failure points at the missing field instead of dumping two blobs.
func reportFieldDrift(t *testing.T, want, got map[string]any, prefix string) {
	t.Helper()
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("missing from our types: %s%s", prefix, k)
			continue
		}
		wm, wIsMap := wv.(map[string]any)
		gm, gIsMap := gv.(map[string]any)
		if wIsMap && gIsMap {
			reportFieldDrift(t, wm, gm, prefix+k+".")
			continue
		}
		wl, wIsList := wv.([]any)
		gl, gIsList := gv.([]any)
		if wIsList && gIsList && len(wl) > 0 && len(gl) > 0 {
			if wem, ok := wl[0].(map[string]any); ok {
				if gem, ok := gl[0].(map[string]any); ok {
					reportFieldDrift(t, wem, gem, prefix+k+"[].")
					continue
				}
			}
		}
		if !reflect.DeepEqual(wv, gv) {
			t.Errorf("value differs at %s%s: want %v, got %v", prefix, k, wv, gv)
		}
	}
}
