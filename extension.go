package bhgraph

// Extension is a BloodHound extension definition schema, introduced in
// BloodHound CE v9.0.0 and installed with PUT /api/v2/extensions.
//
// Declaring a schema is what makes a graph "structured" rather than "generic",
// and structured graphs are the only ones whose edges take part in the UI's
// pathfinding. The mechanism is a single flag: IsTraversable on each
// RelationshipKind.
type Extension struct {
	Schema               SchemaMeta         `json:"schema"`
	NodeKinds            []NodeKind         `json:"node_kinds"`
	RelationshipKinds    []RelationshipKind `json:"relationship_kinds"`
	Environments         []Environment      `json:"environments"`
	RelationshipFindings []any              `json:"relationship_findings"`
}

// SchemaMeta identifies the extension.
//
// Namespace is declared without the underscore that joins it to each kind name:
// MSSQL, for kinds such as MSSQL_Database and MSSQL_AddMember. BloodHound adds
// the underscore itself when it checks the kind names, so MSSQL_ would make it
// look for MSSQL__Database. Validate applies the same check.
type SchemaMeta struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
	Namespace   string `json:"namespace"`
}

// NodeKind declares one node type and how the UI should draw it.
//
// Icon takes a Font Awesome free solid icon name. Declaring Icon and Color here
// makes POST /api/v2/custom-nodes redundant.
type NodeKind struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description"`
	IsDisplayKind bool   `json:"is_display_kind"`
	Icon          string `json:"icon"`
	Color         string `json:"color"`

	// Info is what BloodHound shows in the Entity Panel for a node of this
	// kind, next to its properties, keyed by section. See KindInfo.
	Info map[string]KindInfo `json:"info,omitempty"`
}

// RelationshipKind declares one edge type.
//
// IsTraversable governs pathfinding and Attack Path detection. Marking an edge
// traversable when it does not represent a capability produces false paths in
// the UI, so it is a semantic decision rather than a cosmetic one.
type RelationshipKind struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	IsTraversable bool   `json:"is_traversable"`

	// Info is what BloodHound shows in the Entity Panel for a relationship of
	// this kind, keyed by section. See KindInfo.
	Info map[string]KindInfo `json:"info,omitempty"`
}

// KindInfo is one section of the Entity Panel, the panel BloodHound opens on
// the node or relationship selected in Explore. It is where an extension says
// what a kind means, how it is abused and how it is closed, in the place the
// analyst is already looking.
//
// The section is keyed in the Info map by an identifier made of lowercase
// letters, digits, hyphens and underscores, which a later version of the schema
// uses to update the same section, so it should not change. The panel starts
// with the properties at position 0, and the sections of an extension follow in
// the order of Position; BloodHound's documentation numbers them from 1.
//
// Markdown.Content is Markdown, and it is also a Go text/template evaluated for
// the selected entity: {{ .Properties.name }} for a node, and
// {{ .Source.Properties.name }} and {{ .Target.Properties.name }} for the two
// ends of a relationship. BloodHound reads Info from v9.5.0, and evaluates the
// content as a template from v9.7.0.
type KindInfo struct {
	Title    string           `json:"title"`
	Position int              `json:"position"`
	Markdown KindInfoMarkdown `json:"markdown"`
}

// KindInfoMarkdown holds the content of one Entity Panel section.
type KindInfoMarkdown struct {
	Content string `json:"content"`
}

// Environment scopes findings and risk metrics to a set of principal kinds.
//
// Findings and risk metrics are BloodHound Enterprise features. On Community
// Edition this field is filled in for schema correctness, and no automatic
// findings should be expected from it.
type Environment struct {
	EnvironmentKind string   `json:"environment_kind"`
	SourceKind      string   `json:"source_kind"`
	PrincipalKinds  []string `json:"principal_kinds"`
}

// TraversableKinds returns the names of the relationship kinds that take part
// in pathfinding. Useful in tests and in reviewing a schema before installing
// it, since this set is the entire difference between a graph the UI can walk
// and one it cannot.
func (e Extension) TraversableKinds() []string {
	out := make([]string, 0, len(e.RelationshipKinds))
	for _, rk := range e.RelationshipKinds {
		if rk.IsTraversable {
			out = append(out, rk.Name)
		}
	}
	return out
}

// declaredNodeKinds returns the set of node kind names declared by the schema.
func (e Extension) declaredNodeKinds() map[string]bool {
	set := make(map[string]bool, len(e.NodeKinds))
	for _, nk := range e.NodeKinds {
		set[nk.Name] = true
	}
	return set
}

// declaredRelationshipKinds returns the set of relationship kind names declared
// by the schema.
func (e Extension) declaredRelationshipKinds() map[string]bool {
	set := make(map[string]bool, len(e.RelationshipKinds))
	for _, rk := range e.RelationshipKinds {
		set[rk.Name] = true
	}
	return set
}
