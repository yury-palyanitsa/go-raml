package server

import (
	"fmt"
	"strings"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// hoverProbe is one hover test case: a document, a cursor position, and
// expected content rules for the returned MarkupContent.
//
// The probe runner:
//   - opens the document, waits for parse,
//   - calls hover at (line, col),
//   - logs a structured debug block (document with target line marked, hover
//     result markdown) so failures are immediately readable without -v,
//   - then enforces mustContain / mustNotContain.
type hoverProbe struct {
	name string
	doc  string
	line uint32
	col  uint32
	// mustContain: substrings that MUST appear in the hover markdown.
	mustContain []string
	// mustNotContain: substrings that MUST NOT appear in the hover markdown.
	mustNotContain []string
	// wantNil: when true the hover result must be nil (position has no target).
	wantNil bool
}

const ramlBuiltinTypeDoc = "#%RAML 1.0\ntitle: Test\ntypes:\n  OtherType: date-only | string\n  MyType:\n    type: date-only\n    enum: [\"2015-05-23\"]\n"

// hoverProbes is the authoritative list of hover test cases.
// Positions are 0-based LSP (line, character).
var hoverProbes = []hoverProbe{
	// ── Named type definition ─────────────────────────────────────────────────
	// ramlValid L3 "  Person:" — key at col 2.
	{
		name: "shape / named type definition",
		doc:  ramlValid,
		line: 3, col: 2,
		mustContain: []string{"Person", "object", "A person entity"},
	},

	// ── Custom facet value key ────────────────────────────────────────────────
	// ramlCustomFacets L10 "    myFacet: \"some value\"" — key at col 4.
	{
		name: "custom facet value key",
		doc:  ramlCustomFacets,
		line: 10, col: 4,
		mustContain: []string{"A custom constraint", "string"},
	},

	// ── Annotation (CDP) value property key ───────────────────────────────────
	// ramlCDPValueKeys L15 "      since: \"2.0\"" — key at col 6.
	{
		name: "CDP annotation value property",
		doc:  ramlCDPValueKeys,
		line: 15, col: 6,
		mustContain: []string{"Version since which it is deprecated", "string"},
	},

	// ── Array-typed annotation value property key ─────────────────────────────
	// ramlCDPArrayValueKeys L18 "    - name: public" — key at col 6.
	{
		name: "CDP array annotation value property",
		doc:  ramlCDPArrayValueKeys,
		line: 18, col: 6,
		mustContain: []string{"Role name", "string"},
	},

	// ── Object-typed custom facet value property key ──────────────────────────
	// ramlObjectFacetValueKeys L14 "      author: \"John\"" — key at col 6.
	{
		name: "object facet value property",
		doc:  ramlObjectFacetValueKeys,
		line: 14, col: 6,
		mustContain: []string{"Author of the metadata", "string"},
	},

	// ── Array-typed custom facet value property key ───────────────────────────
	// ramlArrayFacetValueKeys L16 "    - name: \"alpha\"" — key at col 6.
	{
		name: "array facet value property",
		doc:  ramlArrayFacetValueKeys,
		line: 16, col: 6,
		mustContain: []string{"Tag name", "string"},
	},

	// ── Built-in type: inline union expression ────────────────────────────────
	// ramlBuiltinTypeDoc L3 "  OtherType: date-only | string" — "date-only" at col 13.
	{
		name: "builtin type / union expression",
		doc:  ramlBuiltinTypeDoc,
		line: 3, col: 13,
		mustContain: []string{"date-only"},
	},

	// ── Built-in type: explicit type facet ───────────────────────────────────
	// ramlBuiltinTypeDoc L5 "    type: date-only" — "date-only" at col 10.
	{
		name: "builtin type / explicit facet",
		doc:  ramlBuiltinTypeDoc,
		line: 5, col: 10,
		mustContain: []string{"date-only"},
	},

	// ── HTTP status code response key ─────────────────────────────────────────
	// L5 "      404:" — "404" at col 6.
	{
		name: "HTTP status code key",
		doc:  "#%RAML 1.0\ntitle: test\n/foo:\n  get:\n    responses:\n      404:\n        description: Not here\n",
		line: 5, col: 6,
		mustContain: []string{"404", "Not Found"},
	},
}

// TestLSP_HoverProbes runs every hoverProbe as an independent sub-test.
//
// Each sub-test logs a structured debug block containing the annotated document
// and the full hover markdown, so failures can be diagnosed without -v.
//
// Run with: go test -v -run TestLSP_HoverProbes ./...
func TestLSP_HoverProbes(t *testing.T) {
	for _, p := range hoverProbes {
		p := p
		t.Run(p.name, func(t *testing.T) {
			runHoverProbe(t, p)
		})
	}
}

// runHoverProbe is the per-probe driver described in TestLSP_HoverProbes.
func runHoverProbe(t *testing.T, p hoverProbe) {
	t.Helper()

	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()
	uri := openDoc(t, s, nc, dir, "api.raml", p.doc)
	waitParse(t, s.cache, uri)

	ctx := fakeCtx(nc)
	resp, err := s.hover(ctx, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: p.line, Character: p.col},
		},
	})

	// ── Build debug block ──────────────────────────────────────────────────
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n=== HOVER PROBE: %s ===\n", p.name)
	fmt.Fprintf(&sb, "Target: L%d C%d\n", p.line, p.col)

	// Annotated document: target line is marked with "=>".
	fmt.Fprintf(&sb, "\nDocument:\n")
	for i, line := range strings.Split(p.doc, "\n") {
		mark := "  "
		if uint32(i) == p.line {
			mark = "=>"
		}
		fmt.Fprintf(&sb, "  %s L%-2d  %s\n", mark, i, line)
	}

	// Hover result.
	fmt.Fprintf(&sb, "\nHover result: ")
	if err != nil {
		fmt.Fprintf(&sb, "ERROR: %v\n", err)
	} else if resp == nil {
		fmt.Fprintf(&sb, "nil\n")
	} else if mc, ok := resp.Contents.(protocol.MarkupContent); ok {
		// Indent each line of the markdown for readability.
		indented := strings.ReplaceAll(strings.TrimRight(mc.Value, "\n"), "\n", "\n    ")
		fmt.Fprintf(&sb, "MarkupContent (%s)\n    %s\n", mc.Kind, indented)
	} else {
		fmt.Fprintf(&sb, "%T: %v\n", resp.Contents, resp.Contents)
	}

	// ── Evaluate assertions ────────────────────────────────────────────────
	var failures []string

	if err != nil {
		failures = append(failures, fmt.Sprintf("hover returned error: %v", err))
	} else if p.wantNil {
		if resp != nil {
			failures = append(failures, "expected nil hover result")
		}
	} else {
		if resp == nil {
			failures = append(failures, "hover returned nil")
		} else {
			mc, ok := resp.Contents.(protocol.MarkupContent)
			if !ok {
				failures = append(failures, fmt.Sprintf("contents is %T, want MarkupContent", resp.Contents))
			} else {
				for _, want := range p.mustContain {
					if !strings.Contains(mc.Value, want) {
						failures = append(failures, fmt.Sprintf("MISSING %q in hover markdown", want))
					}
				}
				for _, bad := range p.mustNotContain {
					if strings.Contains(mc.Value, bad) {
						failures = append(failures, fmt.Sprintf("UNEXPECTED %q in hover markdown", bad))
					}
				}
			}
		}
	}

	if len(failures) > 0 {
		fmt.Fprintf(&sb, "\nFAILURES:\n")
		for _, f := range failures {
			fmt.Fprintf(&sb, "  - %s\n", f)
		}
	} else {
		fmt.Fprintf(&sb, "\nRESULT: OK\n")
	}

	t.Log(sb.String())

	if len(failures) > 0 {
		t.Fail()
	}
}

// triggerHover opens content in a temp server and requests hover at the given
// 0-based LSP cursor position. Kept as a low-level helper for one-off tests
// that do not fit the probe table.
func triggerHover(t *testing.T, content string, lspLine, lspChar uint32) *protocol.Hover {
	t.Helper()
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()
	uri := openDoc(t, s, nc, dir, "api.raml", content)
	waitParse(t, s.cache, uri)
	ctx := fakeCtx(nc)
	result, err := s.hover(ctx, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: lspLine, Character: lspChar},
		},
	})
	if err != nil {
		t.Fatalf("hover error: %v", err)
	}
	return result
}
