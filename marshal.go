package bhgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// MarshalJSON renders the graph in the shape BloodHound's file-upload endpoint
// expects, which is the same for generic and structured graphs:
//
//	{"graph": {"nodes": [...], "edges": [...]}}
//
// A Ref with an empty MatchBy is emitted as MatchByID.
func (g Graph) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if _, err := g.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteTo streams the graph to w without holding the whole serialized payload
// in memory. Nodes and edges are encoded one at a time, so the peak allocation
// is one element rather than the entire graph. The document is assembled by
// hand for that reason: encoding it as a single value would defeat the point.
//
// It reports the number of bytes written, satisfying io.WriterTo.
func (g Graph) WriteTo(w io.Writer) (int64, error) {
	out := newGraphWriter(w)

	out.write(`{"graph":{"nodes":[`)
	for i, n := range g.Nodes {
		if i > 0 {
			out.write(",")
		}
		if !out.encode(n) {
			return out.n, fmt.Errorf("encoding node %q: %w", n.ID, out.err)
		}
	}

	out.write(`],"edges":[`)
	for i, e := range g.Edges {
		if i > 0 {
			out.write(",")
		}
		if !out.encode(normalizeEdge(e)) {
			return out.n, fmt.Errorf("encoding edge %d (%s): %w", i, e.Kind, out.err)
		}
	}

	out.write(`]}}`)
	return out.n, out.err
}

// normalizeEdge spells out the default match strategy, so that a Ref built from
// a bare Value says on the wire what it means.
func normalizeEdge(e Edge) Edge {
	if e.Start.matchesByID() {
		e.Start.MatchBy = MatchByID
	}
	if e.End.matchesByID() {
		e.End.MatchBy = MatchByID
	}
	return e
}

// graphWriter counts what it writes and keeps the first write error, so that
// WriteTo reads as a list of writes rather than as the same error check after
// every one of them. It is the pattern from the Go blog, "Errors are values".
//
// The buffer and the encoder are built once and reused for every element. A
// fresh pair per node would put the allocation back in proportion to the graph,
// which is the one thing WriteTo exists to avoid.
type graphWriter struct {
	w   io.Writer
	buf bytes.Buffer
	enc *json.Encoder
	n   int64
	err error
}

func newGraphWriter(w io.Writer) *graphWriter {
	gw := &graphWriter{w: w}
	gw.enc = json.NewEncoder(&gw.buf)
	// Property values are data, not markup. Leaving <, > and & unescaped keeps
	// the payload readable; both forms decode to the same string.
	gw.enc.SetEscapeHTML(false)
	return gw
}

func (gw *graphWriter) write(s string) {
	if gw.err != nil {
		return
	}
	n, err := io.WriteString(gw.w, s)
	gw.n += int64(n)
	gw.err = err
}

// encode appends one element as JSON and reports whether it can carry on. A
// false leaves the reason in gw.err, and the caller names the element that was
// being written, which is the part gw cannot know.
func (gw *graphWriter) encode(v any) bool {
	if gw.err != nil {
		return false
	}
	gw.buf.Reset()
	if err := gw.enc.Encode(v); err != nil {
		gw.err = err
		return false
	}
	// Encode appends a newline. Trimming it keeps the output a single document,
	// and keeps the bytes identical to the payload a real server accepted.
	n, err := gw.w.Write(bytes.TrimRight(gw.buf.Bytes(), "\n"))
	gw.n += int64(n)
	gw.err = err
	return err == nil
}
