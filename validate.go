package bhgraph

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template/parse"
)

// ValidationError collects everything wrong with a graph or a schema, rather
// than stopping at the first problem.
//
// A failed ingest job reports errors that are poor and asynchronous: the upload
// is accepted, and what went wrong surfaces later, if at all. Catching problems
// before sending is the reason this library exists rather than writing the JSON
// by hand.
type ValidationError struct {
	Problems []string
}

func (v *ValidationError) Error() string {
	if len(v.Problems) == 1 {
		return "bhgraph: " + v.Problems[0]
	}
	return fmt.Sprintf("bhgraph: %d validation problems:\n  - %s",
		len(v.Problems), strings.Join(v.Problems, "\n  - "))
}

func (v *ValidationError) add(format string, args ...any) {
	v.Problems = append(v.Problems, fmt.Sprintf(format, args...))
}

func (v *ValidationError) orNil() error {
	if len(v.Problems) == 0 {
		return nil
	}
	return v
}

// Validate reports structural problems in the graph itself, without reference
// to any schema.
//
// It checks that every node has a non-empty ID and at least one kind, that IDs
// are unique, that every edge has a kind, and that edges matching by ID point
// at nodes present in the payload. That last check is the one that catches the
// common mistake: an edge to a node the collector forgot to emit is accepted by
// BloodHound and silently produces nothing.
func (g Graph) Validate() error {
	v := &ValidationError{}
	g.validate(v)
	return v.orNil()
}

// validate appends the structural problems to v, so that ValidateAgainst can
// report them next to the schema ones instead of unwrapping an error it has
// just built itself.
func (g Graph) validate(v *ValidationError) {
	ids := make(map[string]bool, len(g.Nodes))
	for i, n := range g.Nodes {
		switch {
		case n.ID == "":
			v.add("node %d: empty id", i)
		case ids[n.ID]:
			v.add("node %d: duplicate id %q", i, n.ID)
		default:
			ids[n.ID] = true
		}
		if len(n.Kinds) == 0 {
			v.add("node %q: no kinds", n.ID)
		}
		for _, k := range n.Kinds {
			if k == "" {
				v.add("node %q: empty kind", n.ID)
			}
		}
	}

	// Only id matching can be checked locally: name and property matchers are
	// resolved by BloodHound against data we cannot see.
	checkEndpoint := func(edge int, kind, side string, r Ref) {
		switch {
		case r.Value == "":
			v.add("edge %d (%s): empty %s value", edge, kind, side)
		case r.matchesByID() && !ids[r.Value]:
			v.add("edge %d (%s): %s %q is not a node in this payload", edge, kind, side, r.Value)
		}
	}

	for i, e := range g.Edges {
		if e.Kind == "" {
			v.add("edge %d: empty kind", i)
		}
		checkEndpoint(i, e.Kind, "start", e.Start)
		checkEndpoint(i, e.Kind, "end", e.End)
	}
}

// Validate reports problems in the extension schema itself.
//
// Every kind name, the environment kind included, must start with the declared
// namespace followed by an underscore, and something must follow that prefix.
// BloodHound appends the underscore itself and refuses the schema if a single
// kind does not match (cmd/api/src/model/graphschema.go:508 at v9.7.0). A
// namespace declared as MSSQL_ therefore fails on install even though every
// kind name visibly starts with it: the server looks for MSSQL__Database.
func (e Extension) Validate() error {
	v := &ValidationError{}

	if e.Schema.Name == "" {
		v.add("schema: empty name")
	}
	if e.Schema.Version == "" {
		v.add("schema: empty version")
	}
	ns := e.Schema.Namespace
	if ns == "" {
		v.add("schema: empty namespace")
	}

	if len(e.NodeKinds) == 0 {
		v.add("schema: no node kinds declared")
	}

	checkNamespace := func(what, name string) {
		if ns == "" {
			return
		}
		rest, found := strings.CutPrefix(name, ns+"_")
		switch {
		case !found:
			v.add("%s %q: does not carry the declared namespace %q followed by an underscore", what, name, ns)
		case strings.TrimSpace(rest) == "":
			v.add("%s %q: nothing follows the namespace prefix", what, name)
		}
	}

	// Node kinds and relationship kinds share one set of names on purpose: a
	// name that meant two things would be ambiguous in a saved Cypher query.
	seen := map[string]bool{}
	checkName := func(what, name string) {
		if name == "" {
			v.add("%s: empty name", what)
			return
		}
		if seen[name] {
			v.add("%s: duplicate kind name %q", what, name)
		}
		seen[name] = true
		checkNamespace(what, name)
	}

	displayKinds := 0
	for _, nk := range e.NodeKinds {
		checkName("node kind", nk.Name)
		validateInfo(v, fmt.Sprintf("node kind %q", nk.Name), nk.Info)
		if nk.IsDisplayKind {
			displayKinds++
		}
	}
	if len(e.NodeKinds) > 0 && displayKinds == 0 {
		v.add("schema: no node kind is marked is_display_kind")
	}

	for _, rk := range e.RelationshipKinds {
		checkName("relationship kind", rk.Name)
		validateInfo(v, fmt.Sprintf("relationship kind %q", rk.Name), rk.Info)
	}

	declared := e.declaredNodeKinds()
	for i, env := range e.Environments {
		if env.EnvironmentKind == "" {
			v.add("environment %d: empty environment_kind", i)
		} else {
			checkNamespace(fmt.Sprintf("environment %d: environment_kind", i), env.EnvironmentKind)
		}
		if len(env.PrincipalKinds) == 0 {
			v.add("environment %d: no principal kinds", i)
		}
		for _, pk := range env.PrincipalKinds {
			if !declared[pk] {
				v.add("environment %d: principal kind %q is not a declared node kind", i, pk)
			}
		}
	}

	return v.orNil()
}

// infoKey is the form BloodHound holds an Entity Panel section key to, and
// maxInfo the most sections it takes for one kind
// (cmd/api/src/model/graphschema.go:85 and 164 at v9.7.0).
var infoKey = regexp.MustCompile(`^[a-z0-9_-]{1,128}$`)

const maxInfo = 100

// validateInfo reports what BloodHound would refuse in the Entity Panel
// sections of one kind (validateKindInfo, cmd/api/src/model/graphschema.go:163
// at v9.7.0): too many of them, a key it does not accept, a blank title, a
// negative position, two sections at the same position, and content that is
// not a template it can parse.
//
// The template is parsed without knowing its functions. BloodHound offers the
// hermetic set of the sprig library less a few, and reproducing that list here
// would take sprig as a dependency, so a template that calls a function the
// server does not offer passes here and is refused on upload. So is Markdown
// holding HTML the server does not allow, which it checks and this does not.
func validateInfo(v *ValidationError, kind string, info map[string]KindInfo) {
	if len(info) > maxInfo {
		v.add("%s: %d info sections, and BloodHound takes at most %d", kind, len(info), maxInfo)
	}

	keys := make([]string, 0, len(info))
	for key := range info {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	positions := make(map[int]string, len(info))
	for _, key := range keys {
		section := info[key]
		if !infoKey.MatchString(key) {
			v.add("%s: info key %q does not match %s", kind, key, infoKey)
		}
		if strings.TrimSpace(section.Title) == "" {
			v.add("%s: info %q has no title", kind, key)
		}
		switch other, taken := positions[section.Position]; {
		case section.Position < 0:
			v.add("%s: info %q at position %d, and positions start at 0", kind, key, section.Position)
		case taken:
			v.add("%s: info %q and %q share position %d", kind, other, key, section.Position)
		default:
			positions[section.Position] = key
		}
		if err := parseTemplate(section.Markdown.Content); err != nil {
			v.add("%s: info %q is not a template BloodHound can parse: %v", kind, key, err)
		}
	}
}

// parseTemplate parses the content of a section as text/template does, with
// the check that every function is defined left out.
func parseTemplate(content string) error {
	tree := parse.New("info")
	tree.Mode = parse.SkipFuncCheck
	_, err := tree.Parse(content, "", "", map[string]*parse.Tree{})
	return err
}

// ValidateAgainst checks the graph against a schema, on top of the structural
// checks Validate performs.
//
// Every kind used by a node or an edge must be declared. An undeclared kind is
// accepted by the ingest endpoint and then does not appear in the UI as
// expected, which is a slow and confusing way to find a typo.
func (g Graph) ValidateAgainst(ext Extension) error {
	v := &ValidationError{}
	g.validate(v)

	nodeKinds := ext.declaredNodeKinds()
	relKinds := ext.declaredRelationshipKinds()

	for _, n := range g.Nodes {
		for _, k := range n.Kinds {
			if k != "" && !nodeKinds[k] {
				v.add("node %q: kind %q is not declared in the schema", n.ID, k)
			}
		}
	}
	for i, e := range g.Edges {
		if e.Kind != "" && !relKinds[e.Kind] {
			v.add("edge %d: kind %q is not declared in the schema", i, e.Kind)
		}
	}

	return v.orNil()
}
