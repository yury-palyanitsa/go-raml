package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	raml "github.com/acronis/go-raml/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// triggerCompletion opens content in a temp server and requests completions at
// the given 0-based LSP cursor position.  It waits for the parse to complete
// (via FlushPending inside the completion handler).
func triggerCompletion(t *testing.T, content string, lspLine, lspChar uint32) []protocol.CompletionItem {
	t.Helper()
	dir := t.TempDir()
	s := newTestServer()
	nc := &notifyCapture{}
	uri := openDoc(t, s, nc, dir, "api.raml", content)
	waitParse(t, s.cache, uri)

	ctx := fakeCtx(nc)
	raw, err := s.completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: lspLine, Character: lspChar},
		},
	})
	require.NoError(t, err)
	if raw == nil {
		return nil
	}
	return raw.([]protocol.CompletionItem)
}

// ---- analyzePosition ----

// positionAt is a test helper that runs analyzePosition for a single cursor
// line. It builds a minimal document (empty lines before lspLine, then
// lineText at lspLine) so that upward structural scans find nothing above.
func positionAt(lineText string, lspLine int, ci *ContextIndex) CursorPosition {
	rawLines := make([]string, lspLine+1)
	rawLines[lspLine] = lineText
	return analyzePosition(lineText, lspLine, parseLines(rawLines), &FileContext{CI: ci})
}

func TestAnalyzePosition(t *testing.T) {
	// Text-only fallback cases: ci is nil (model not yet parsed).
	textFallbackCases := []struct {
		input    string
		wantRole CursorRole
		wantLib  string
	}{
		// No colon → key context.
		{"", RoleKey, ""},
		{"  ", RoleKey, ""},
		{"  desc", RoleKey, ""},
		// Known type-expression keys.
		{"  type: ", RoleTypeExpr, ""},
		{"  type: Str", RoleTypeExpr, ""},
		{"schema: ", RoleTypeExpr, ""},
		{"items: MyT", RoleTypeExpr, ""},
		// Union type expressions — value side, any content after the colon.
		{"  type: string | ", RoleTypeExpr, ""},
		{"  type: string | int", RoleTypeExpr, ""},
		{"  myProp: string | ", RoleTypeExpr, ""},
		// Library-qualified type (caught before colon check).
		{"  type: lib.", RoleLibType, "lib"},
		{"  type: myLib.Type", RoleLibType, "myLib"},
		// Annotation.
		{"  (ann", RoleAnnotationName, ""},
		{"(lib.ann", RoleLibType, "lib"},
		// Inline property shorthand.
		{"  myProp: Part", RoleTypeExpr, ""},
		{"  myProp: lib.", RoleLibType, "lib"},
		// Endpoint paths and HTTP method keywords → suppressed.
		{"/resources: ", RoleSuppressed, ""},
		{"/api/v1: ", RoleSuppressed, ""},
		{"get: ", RoleSuppressed, ""},
		{"  get: ", RoleSuppressed, ""},
		{"post: ", RoleSuppressed, ""},
		{"  put: Part", RoleSuppressed, ""},
		{"delete: ", RoleSuppressed, ""},
		{"patch: ", RoleSuppressed, ""},
		{"  head: ", RoleSuppressed, ""},
		{"options: ", RoleSuppressed, ""},
	}
	for _, tc := range textFallbackCases {
		got := positionAt(tc.input, 0, nil)
		assert.Equal(t, tc.wantRole, got.Role, "text-fallback input=%q", tc.input)
		assert.Equal(t, tc.wantLib, got.LibAlias, "text-fallback input=%q", tc.input)
	}

	// Model-graph cases: a ContextIndex with explicit entries overrides the text
	// heuristics. ramlLine is 1-based; positionAt converts to 0-based internally.
	makeCI := func(entries ...ContextEntry) *ContextIndex {
		return &ContextIndex{entries: entries}
	}

	type modelCase struct {
		desc     string
		input    string
		ci       *ContextIndex
		ramlLine int
		wantRole CursorRole
	}

	modelCases := []modelCase{
		// On the key line of an endpoint or operation.
		{
			"on endpoint key line",
			"/test: ",
			makeCI(ContextEntry{Kind: CKEndpoint, StartLine: 5, EndLine: 20}),
			5, RoleSuppressed,
		},
		{
			"on operation key line",
			"  get: ",
			makeCI(ContextEntry{Kind: CKOperation, StartLine: 7, EndLine: 15}),
			7, RoleSuppressed,
		},
		// Inside an endpoint/operation/trait body.
		{
			"inside endpoint body",
			"  someKey: Part",
			makeCI(ContextEntry{Kind: CKEndpoint, StartLine: 5, EndLine: 20}),
			10, RoleSuppressed,
		},
		{
			"inside operation body",
			"  someKey: Part",
			makeCI(ContextEntry{Kind: CKOperation, StartLine: 7, EndLine: 15}),
			10, RoleSuppressed,
		},
		{
			"inside trait body",
			"  someKey: Part",
			makeCI(ContextEntry{Kind: CKTrait, StartLine: 3, EndLine: 30}),
			15, RoleSuppressed,
		},
		// On a type key line: inline type shorthand (model confirms CKType).
		// Even if the key happens to look like an HTTP method, model wins.
		{
			"type named 'get' at-line CKType",
			"  get: Str",
			makeCI(ContextEntry{Kind: CKType, StartLine: 12, EndLine: 18}),
			12, RoleTypeExpr,
		},
		// On a property key line: inline property shorthand.
		{
			"on property key line",
			"  name: str",
			makeCI(ContextEntry{Kind: CKProperty, StartLine: 14, EndLine: 16}),
			14, RoleTypeExpr,
		},
		{
			"prop named 'get' at-line CKProperty",
			"  get: integr",
			makeCI(ContextEntry{Kind: CKProperty, StartLine: 14, EndLine: 16}),
			14, RoleTypeExpr,
		},
		// Union on a type key line.
		{
			"union on type key line",
			"  Test: string | ",
			makeCI(ContextEntry{Kind: CKType, StartLine: 12, EndLine: 18}),
			12, RoleTypeExpr,
		},
		// Inside a type/property body on the value side: scalar facet → suppressed.
		{
			"scalar facet inside type body",
			"  minLength: ",
			makeCI(ContextEntry{Kind: CKType, StartLine: 12, EndLine: 18}),
			15, RoleSuppressed,
		},
		{
			"description inside type body",
			"  description: ",
			makeCI(ContextEntry{Kind: CKType, StartLine: 12, EndLine: 18}),
			15, RoleSuppressed,
		},
		{
			"facet inside property body",
			"  description: ",
			makeCI(ContextEntry{Kind: CKProperty, StartLine: 14, EndLine: 16}),
			15, RoleSuppressed,
		},
	}
	for _, tc := range modelCases {
		got := positionAt(tc.input, tc.ramlLine-1, tc.ci)
		assert.Equal(t, tc.wantRole, got.Role, "model-aware: %s (input=%q)", tc.desc, tc.input)
	}
}

// ---- nearestSeqKey ----

func TestNearestSeqKey(t *testing.T) {
	// Items more indented than their key (classic block sequence).
	//   0: "  type:"       indent 2
	//   1: "    - TypeA"   indent 4
	//   2: "    - "        indent 4 ← query line
	moreIndented := parseLines([]string{"  type:", "    - TypeA", "    - "})
	seqKey, seqLine := nearestSeqKey(moreIndented, 2)
	assert.Equal(t, "type", seqKey, "more-indented: key name")
	assert.Equal(t, 0, seqLine, "more-indented: key line")

	// Items at the same indent as their key (valid YAML).
	//   0: "  type:"      indent 2
	//   1: "  - TypeA"    indent 2
	//   2: "  - "         indent 2 ← query line
	sameIndented := parseLines([]string{"  type:", "  - TypeA", "  - "})
	seqKey, seqLine = nearestSeqKey(sameIndented, 2)
	assert.Equal(t, "type", seqKey, "same-indent: key name")
	assert.Equal(t, 0, seqLine, "same-indent: key line")

	// No parent key above (sequence at the top of the document).
	topLevel := parseLines([]string{"- TypeA", "- "})
	seqKey, seqLine = nearestSeqKey(topLevel, 1)
	assert.Equal(t, "", seqKey, "top-level: no key")
	assert.Equal(t, -1, seqLine, "top-level: no line")
}

// ---- existingKeysAt ----

// ramlWithUsesBlock is a document where a blank cursor line immediately follows
// an indented block (the uses: value), making the above-nearest-line heuristic
// produce the wrong targetIndent when scanning only upward.
//
// Line numbers (0-based):
//
//	0: #%RAML 1.0
//	1: title: test
//	2: (blank)
//	3: uses:
//	4:     lib: lib.raml          ← indent 4
//	5: (blank)                    ← CURSOR_BUG1: first non-blank above is indent 4
//	6: annotationTypes:
//	7:   myAnnotation:
//	8:     type: string
//	9: (blank)
//	10: types:
//	11:   MyType:
//	12:     type: string
const ramlWithUsesBlock = "#%RAML 1.0\ntitle: test\n\nuses:\n    lib: lib.raml\n\nannotationTypes:\n  myAnnotation:\n    type: string\n\ntypes:\n  MyType:\n    type: string\n"

// TestExistingKeysAt_AfterIndentedBlock ensures that a blank line immediately
// following an indented block (e.g. the body of `uses:`) uses the indent of the
// NEXT sibling key (0) rather than the indent of the last line of the block (4).
func TestExistingKeysAt_AfterIndentedBlock(t *testing.T) {
	lines := parseDoc(ramlWithUsesBlock)
	// LSP line 5 = blank after `    lib: lib.raml`.
	got := existingKeysAt(lines, 5)

	// Root-level keys that are present in the document must all be found so that
	// keyCompletions can exclude them from the suggestion list.
	assert.Contains(t, got, "title", "title should be detected as an existing root key")
	assert.Contains(t, got, "uses", "uses should be detected as an existing root key")
	assert.Contains(t, got, "annotationTypes", "annotationTypes should be detected as an existing root key")
	assert.Contains(t, got, "types", "types should be detected as an existing root key")
}

// TestExistingKeysAt_AfterAnnotationUsage verifies a position where the nearest
// non-blank line above is already at indent 0, so no special treatment is needed.
// This is the "works" case in the original bug report.
//
// Line layout: 0-based lines of ramlWithUsesBlock, cursor at LSP line 9 (blank
// between last annotation-type body line and `types:`).
func TestExistingKeysAt_AfterAnnotationUsage(t *testing.T) {
	lines := parseDoc(ramlWithUsesBlock)
	// LSP line 9 = blank between annotation body and `types:`.
	// Above: `    type: string` (indent 4).  Below: `types:` (indent 0).
	got := existingKeysAt(lines, 9)

	assert.Contains(t, got, "annotationTypes")
	assert.Contains(t, got, "types")
	assert.Contains(t, got, "title")
	assert.Contains(t, got, "uses")
}

// TestExistingKeysAt_InsideTypeBody verifies that sibling facets within a type
// definition are detected independently of the root-level keys.
//
// 0-based layout:
//
//	0: #%RAML 1.0
//	1: title: t
//	2: types:
//	3:   MyType:
//	4:     type: string
//	5: (blank)          ← CURSOR inside type body
//	6:     description: x
const ramlTypeBody = "#%RAML 1.0\ntitle: t\ntypes:\n  MyType:\n    type: string\n\n    description: x\n"

func TestExistingKeysAt_InsideTypeBody(t *testing.T) {
	lines := parseDoc(ramlTypeBody)
	// LSP line 5 = blank inside MyType body; above indent=4, below indent=4.
	got := existingKeysAt(lines, 5)

	assert.Contains(t, got, "type", "type facet should be found as an existing sibling key")
	assert.Contains(t, got, "description", "description facet should be found as an existing sibling key")
	// Root-level keys must NOT appear — they are at a different indent level.
	assert.NotContains(t, got, "title", "root key title must not pollute type-body scan")
	assert.NotContains(t, got, "types", "root key types must not pollute type-body scan")
}

// ---- ContextIndex.Query ----

func TestContextIndex_Query(t *testing.T) {
	ci := &ContextIndex{
		entries: []ContextEntry{
			// Outer type: lines 5–12.
			{StartLine: 5, EndLine: 12, Kind: CKType, ShapeType: raml.TypeObject},
			// Nested property: lines 8–10.
			{StartLine: 8, EndLine: 10, Kind: CKProperty, ShapeType: raml.TypeString},
		},
	}

	// Lines before the outer type → nil (root context).
	assert.Nil(t, ci.Query(4), "line before any entry should return nil")

	// Strictly on the start line: StartLine < line requires 5 < 5 = false → nil.
	assert.Nil(t, ci.Query(5), "cursor on the key definition line should not be inside the block")

	// Line 6 is strictly inside outer type but outside nested property.
	e := ci.Query(6)
	require.NotNil(t, e)
	assert.Equal(t, CKType, e.Kind)
	assert.Equal(t, raml.TypeObject, e.ShapeType)

	// Line 9 is inside both; innermost (higher StartLine) wins.
	e = ci.Query(9)
	require.NotNil(t, e)
	assert.Equal(t, CKProperty, e.Kind)
	assert.Equal(t, raml.TypeString, e.ShapeType)

	// Line 11 is inside outer but past inner EndLine.
	e = ci.Query(11)
	require.NotNil(t, e)
	assert.Equal(t, CKType, e.Kind)

	// Line 13 is past all entries → nil.
	assert.Nil(t, ci.Query(13))
}

func TestContextIndex_Query_NilReceiver(t *testing.T) {
	var ci *ContextIndex
	assert.Nil(t, ci.Query(5), "nil ContextIndex should return nil without panicking")
}

// TestContextIndex_LastBefore verifies that LastBefore returns the entry with
// the highest StartLine that is strictly less than the query line, regardless
// of EndLine (which may not cover trailing blank lines).
func TestContextIndex_LastBefore(t *testing.T) {
	ci := &ContextIndex{
		entries: []ContextEntry{
			{StartLine: 5, EndLine: 6, Kind: CKType, ShapeType: raml.TypeString},
			{StartLine: 10, EndLine: 12, Kind: CKType, ShapeType: raml.TypeObject},
		},
	}
	assert.Nil(t, ci.LastBefore(4), "before any entry")

	e := ci.LastBefore(7) // just past first entry's EndLine
	require.NotNil(t, e)
	assert.Equal(t, raml.TypeString, e.ShapeType)

	e = ci.LastBefore(9) // gap between the two entries
	require.NotNil(t, e)
	assert.Equal(t, raml.TypeString, e.ShapeType)

	e = ci.LastBefore(14) // past second entry's EndLine
	require.NotNil(t, e)
	assert.Equal(t, raml.TypeObject, e.ShapeType)
}

func TestContextIndex_LastBefore_NilReceiver(t *testing.T) {
	var ci *ContextIndex
	assert.Nil(t, ci.LastBefore(10))
}

// TestContextIndex_EntryAt verifies that EntryAt returns the entry whose
// StartLine exactly matches the query line and nil for all other lines.
func TestContextIndex_EntryAt(t *testing.T) {
	ci := &ContextIndex{
		entries: []ContextEntry{
			{StartLine: 5, EndLine: 10, Kind: CKEndpoint},
			{StartLine: 7, EndLine: 9, Kind: CKOperation},
			{StartLine: 12, EndLine: 15, Kind: CKType, ShapeType: raml.TypeString},
		},
	}

	// Exact StartLine matches.
	e := ci.EntryAt(5)
	require.NotNil(t, e)
	assert.Equal(t, CKEndpoint, e.Kind)

	e = ci.EntryAt(7)
	require.NotNil(t, e)
	assert.Equal(t, CKOperation, e.Kind)

	e = ci.EntryAt(12)
	require.NotNil(t, e)
	assert.Equal(t, CKType, e.Kind)

	// Lines that are inside a range but not at StartLine → nil.
	assert.Nil(t, ci.EntryAt(6), "interior line of endpoint should not match")
	assert.Nil(t, ci.EntryAt(11), "gap line should not match")
	assert.Nil(t, ci.EntryAt(20), "past all entries should not match")
}

func TestContextIndex_EntryAt_NilReceiver(t *testing.T) {
	var ci *ContextIndex
	assert.Nil(t, ci.EntryAt(5))
}

// ---- keyCompletions pure-logic tests ----

// docWithAnnotationTypes is a document whose annotationTypes body immediately
// follows `title:`, so the blank between the body and `types:` sits after an
// indented block.
//
// 0-based lines:
//
//	0: #%RAML 1.0
//	1: title: test
//	2: (blank)
//	3: annotationTypes:
//	4:   myAnnotation:
//	5:     type: string
//	6:     description: an annotation
//	7: (blank)    ← CURSOR_ROOT: first non-blank above is indent 4
//	8: types:
//	9:   MyType:
//	10:     type: string
const docWithAnnotationTypes = "#%RAML 1.0\ntitle: test\n\nannotationTypes:\n  myAnnotation:\n    type: string\n    description: an annotation\n\ntypes:\n  MyType:\n    type: string\n"

// TestKeyCompletions_RootDeduplicates checks that root key completions do not
// include keys that are already present in the document.
func TestKeyCompletions_RootDeduplicates(t *testing.T) {
	// ContextIndex has no entries covering line 8 (1-based), so entry is nil (root context).
	items := keyCompletions(nil, raml.FragmentAPI, 7, parseDoc(docWithAnnotationTypes)) // LSP line 7 = blank before types:
	labels := itemLabels(items)

	// Already-defined root keys must be absent.
	assert.NotContains(t, labels, "title", "title is already defined")
	assert.NotContains(t, labels, "annotationTypes", "annotationTypes is already defined")
	assert.NotContains(t, labels, "types", "types is already defined")

	// Undefined root keys must be present.
	assert.Contains(t, labels, "version")
	assert.Contains(t, labels, "baseUri")
	assert.Contains(t, labels, "description")
}

// TestKeyCompletions_StringTypeFacets checks that completions inside a string
// type body contain string-specific facets but not object-only facets.
func TestKeyCompletions_StringTypeFacets(t *testing.T) {
	entry := &ContextEntry{StartLine: 10, EndLine: 11, Kind: CKType, ShapeType: raml.TypeString}
	// LSP line 10 is inside body (StartLine=10, EndLine=11).
	items := keyCompletions(entry, raml.FragmentAPI, 10, parseDoc(docWithAnnotationTypes))
	labels := itemLabels(items)

	// String-specific facets.
	assert.Contains(t, labels, raml.FacetPattern)
	assert.Contains(t, labels, raml.FacetMinLength)
	assert.Contains(t, labels, raml.FacetMaxLength)

	// Object-only facets must be absent for string type.
	assert.NotContains(t, labels, raml.FacetProperties)
	assert.NotContains(t, labels, raml.FacetAdditionalProperties)

	// Array-only facets must be absent.
	assert.NotContains(t, labels, raml.FacetItems)
}

// TestAnalyzePosition_NilWhenIndentedAtRoot verifies that an indented blank line
// with no matching ContextIndex entry returns RoleSuppressed (cursor is inside a
// container block like `types:` at the name level, where we have no known entry).
func TestAnalyzePosition_NilWhenIndentedAtRoot(t *testing.T) {
	// Document: types section with an indented blank (inside the types: block at
	// the name level, before any named type).
	const indentedDoc = "#%RAML 1.0\ntitle: t\ntypes:\n  \n"
	ci := &ContextIndex{} // no entries
	cp := positionAt("  ", 3, ci)
	assert.Equal(t, RoleSuppressed, cp.Role, "indented blank with no context entry should be suppressed")
}

// TestLSP_Completion_LibraryFile confirms that a Library fragment offers the
// library root key set (not the API root keys) at blank lines in the root.
func TestLSP_Completion_LibraryFile(t *testing.T) {
	// Blank at LSP line 4 is after the types block.
	const libDoc = "#%RAML 1.0 Library\ntypes:\n  Base:\n    type: object\n\n"
	s := newTestServer()
	nc := &notifyCapture{}
	uri := openDoc(t, s, nc, t.TempDir(), "lib.raml", libDoc)
	waitParse(t, s.cache, uri)

	ctx := fakeCtx(nc)
	raw, err := s.completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: 4, Character: 0},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, raw)
	labels := itemLabels(raw.([]protocol.CompletionItem))

	// Library root key 'annotationTypes' should be present (types is already defined).
	assert.NotContains(t, labels, "types", "types already defined")
	assert.Contains(t, labels, "annotationTypes")
	// API-only keys must be absent from a Library fragment.
	assert.NotContains(t, labels, "title", "title is not a library root key")
	assert.NotContains(t, labels, "baseUri", "baseUri is not a library root key")
}

// TestLSP_Completion_SingleLineType ensures that an inline-shorthand type
// (`Test: string`) does not panic when the cursor is on an indented blank below
// it.  The result may be nil or some completion list; the requirement is no panic.
func TestLSP_Completion_SingleLineType(t *testing.T) {
	const doc = "#%RAML 1.0\ntitle: test\ntypes:\n  Test: string\n    \n"
	assert.NotPanics(t, func() {
		_ = triggerCompletion(t, doc, 4, 0)
	})
}

// ---- value-side completions (booleans, format, protocols, securedBy, is, type) ----

// TestValueFieldCompletions_Static checks that static enum completions
// (booleans, format, protocols, mediaType) are returned correctly without a
// parse result.
func TestValueFieldCompletions_Static(t *testing.T) {
	cases := []struct {
		vk       ValueKind
		contains []string
	}{
		{ValueKindBool, []string{"true", "false"}},
		{ValueKindFormat, []string{"int", "int32", "int64", "float", "double", "rfc3339"}},
		{ValueKindProtocols, []string{"HTTP", "HTTPS"}},
		{ValueKindMediaType, []string{"application/json", "application/xml", "text/plain"}},
	}
	for _, tc := range cases {
		items := scalarValueCompletions(CursorPosition{ValueKind: tc.vk}, nil, "", nil)
		labels := itemLabels(items)
		for _, want := range tc.contains {
			assert.Contains(t, labels, want, "ValueKind=%v should contain %q", tc.vk, want)
		}
	}
}

// TestValueFieldCompletions_UnknownKey verifies that ValueKindNone (zero value)
// returns nil — the defensive case when no ValueKind is set.
func TestValueFieldCompletions_UnknownKey(t *testing.T) {
	// ValueKindNone is the zero value; scalarValueCompletions must return nil.
	assert.Nil(t, scalarValueCompletions(CursorPosition{}, nil, "", nil), "ValueKindNone should return nil")
}

// TestAnalyzePosition_EndpointTypeReturnsScalarValue checks that analyzePosition
// returns RoleScalarValue for `type:` value side when inside a CKEndpoint entry,
// so resource-type completions can be offered via scalarValueCompletions.
func TestAnalyzePosition_EndpointTypeReturnsScalarValue(t *testing.T) {
	sm := newSourceMap()
	sm.records[8] = &LineRecord{ // ramlLine = lspLine(7) + 1
		KeyName:     "type",
		KeyEndCol:   6, // "  type" — 2 spaces + 4 chars
		ValInline:   true,
		ValNodeKind: 8, // yaml.ScalarNode
		Semantic:    ValSemanticEnum,
		ValKind:     ValueKindResourceType,
	}
	ci := &ContextIndex{
		entries: []ContextEntry{
			{StartLine: 5, EndLine: 20, Kind: CKEndpoint},
		},
		sm: sm,
	}
	// Cursor on value side of `type:` at line 7 (inside the endpoint, not at StartLine).
	cp := positionAt("  type: ", 7, ci)
	assert.Equal(t, RoleScalarValue, cp.Role,
		"type: inside an endpoint block must return RoleScalarValue for resource-type completions")
	assert.Equal(t, ValueKindResourceType, cp.ValueKind, "ValueKind must be ValueKindResourceType")
}

// TestAnalyzePosition_TypeInTypeBodyReturnsTypeExpr ensures a `type:` key
// whose value is an inline scalar type expression returns RoleTypeExpr.
// The SourceMap records the line with ValSemanticTypeExpr (Phase 2 SourceInfo annotation).
func TestAnalyzePosition_TypeInTypeBodyReturnsTypeExpr(t *testing.T) {
	sm := newSourceMap()
	sm.records[7] = &LineRecord{
		KeyName:     "type",
		KeyEndCol:   6, // "    type" — 4 spaces + 4 chars
		ValInline:   true,
		ValNodeKind: 8, // yaml.ScalarNode
		Semantic:    ValSemanticTypeExpr,
	}
	ci := &ContextIndex{sm: sm}
	cp := positionAt("    type: ", 7, ci)
	assert.Equal(t, RoleTypeExpr, cp.Role,
		"type: line with ValSemanticTypeExpr must yield RoleTypeExpr")
}

// ---- !include path completions ----

func TestDetectIncludeArg(t *testing.T) {
	cases := []struct {
		line          string
		wantPath      string
		wantStartChar uint32
		wantOK        bool
	}{
		// Key-value: empty partial path (cursor right after space).
		{"  type: !include ", "", 17, true},
		// Key-value: partial path present.
		{"  type: !include schemas/", "schemas/", 17, true},
		{"  type: !include schemas/my-typ", "schemas/my-typ", 17, true},
		// Sequence item value.
		{"  - !include ", "", 13, true},
		{"  - !include lib/", "lib/", 13, true},
		// No space after !include → not yet an arg context.
		{"  type: !include", "", 0, false},
		// No !include on line.
		{"  type: string", "", 0, false},
		// !include on key side (before colon) → not value side.
		{"!include: value", "", 0, false},
	}
	for _, tc := range cases {
		partial, startChar, ok := detectIncludeArg(tc.line)
		assert.Equal(t, tc.wantOK, ok, "line=%q", tc.line)
		if ok {
			assert.Equal(t, tc.wantPath, partial, "line=%q partial", tc.line)
			assert.Equal(t, tc.wantStartChar, startChar, "line=%q startChar", tc.line)
		}
	}
}

func TestLSP_Completion_Include_RootDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.raml"), []byte("#%RAML 1.0 DataType\ntype: string\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "example.json"), []byte(`"hello"`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "schemas"), 0o700))

	s := newTestServer()
	nc := &notifyCapture{}
	content := "#%RAML 1.0\ntitle: T\ntypes:\n  Foo:\n    type: !include "
	uri := openDoc(t, s, nc, dir, "api.raml", content)
	waitParse(t, s.cache, uri)

	raw, err := s.completion(fakeCtx(nc), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: 4, Character: uint32(len("    type: !include "))},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, raw)
	items := raw.([]protocol.CompletionItem)
	labels := itemLabels(items)

	assert.Contains(t, labels, "schema.raml")
	assert.Contains(t, labels, "example.json")
	assert.Contains(t, labels, "schemas/")
	// Type names must not appear in include completions.
	assert.NotContains(t, labels, "string")
}

func TestLSP_Completion_Include_Subdir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "schemas"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schemas", "order.raml"), []byte("#%RAML 1.0 DataType\ntype: object\n"), 0o600))

	s := newTestServer()
	nc := &notifyCapture{}
	content := "#%RAML 1.0\ntitle: T\ntypes:\n  Order:\n    type: !include schemas/"
	uri := openDoc(t, s, nc, dir, "api.raml", content)
	waitParse(t, s.cache, uri)

	raw, err := s.completion(fakeCtx(nc), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: 4, Character: uint32(len("    type: !include schemas/"))},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, raw)
	items := raw.([]protocol.CompletionItem)
	labels := itemLabels(items)

	assert.Contains(t, labels, "schemas/order.raml")
	// Files from the root must not appear — only schemas/ contents.
	assert.NotContains(t, labels, "order.raml")
}

func TestLSP_Completion_Include_TextEditRange(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.raml"), []byte("#%RAML 1.0 DataType\ntype: string\n"), 0o600))

	s := newTestServer()
	nc := &notifyCapture{}
	content := "#%RAML 1.0\ntitle: T\ntypes:\n  Foo:\n    type: !include sche"
	uri := openDoc(t, s, nc, dir, "api.raml", content)
	waitParse(t, s.cache, uri)

	lineLen := uint32(len("    type: !include sche"))
	pathStart := uint32(len("    type: !include "))
	raw, err := s.completion(fakeCtx(nc), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: 4, Character: lineLen},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, raw)
	items := raw.([]protocol.CompletionItem)

	var found *protocol.CompletionItem
	for i := range items {
		if items[i].Label == "schema.raml" {
			found = &items[i]
			break
		}
	}
	require.NotNil(t, found, "expected schema.raml in completions")

	te, ok := found.TextEdit.(protocol.TextEdit)
	require.True(t, ok, "TextEdit must be a protocol.TextEdit")
	assert.Equal(t, uint32(4), te.Range.Start.Line)
	assert.Equal(t, pathStart, te.Range.Start.Character, "edit must start at path start column")
	assert.Equal(t, lineLen, te.Range.End.Character, "edit must end at cursor")
	assert.Equal(t, "schema.raml", te.NewText)
}

// ---- Document link tests ----

func TestBuildDocumentLinks_BasicInclude(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "schema.json")

	// schema.json does not need to exist: appendIncludeRef records the ref
	// before the parser tries to open the file, so BuildDocumentLinks can
	// return the correct link even from a partial/errored parse.
	content := "#%RAML 1.0\ntitle: Test\ntypes:\n  Foo:\n    type: !include schema.json\n"
	apiPath := filepath.Join(dir, "api.raml")

	r := parseRAMLForTest(t, dir, "api.raml", content)
	links := BuildDocumentLinks(r, apiPath)
	require.Len(t, links, 1)

	link := links[0]
	assert.Equal(t, uint32(4), link.Range.Start.Line) // 0-based line 4 = the type: line
	require.NotNil(t, link.Target)
	assert.Equal(t, raml.PathToFileURI(target), string(*link.Target))
	// Range must cover "schema.json" exactly (not the !include keyword).
	assert.Equal(t, uint32(len("schema.json")), link.Range.End.Character-link.Range.Start.Character)
}

func TestBuildDocumentLinks_UsesEntry(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "types.raml")
	libContent := "#%RAML 1.0 Library\ntypes:\n  T:\n    type: string\n"

	content := "#%RAML 1.0\nuses:\n  lib: types.raml\ntitle: API\n"
	apiPath := filepath.Join(dir, "api.raml")

	r := parseRAMLForTest(t, dir, "api.raml", content, testFS{lib: libContent})
	links := BuildDocumentLinks(r, apiPath)
	require.Len(t, links, 1)

	link := links[0]
	require.NotNil(t, link.Target)
	assert.Equal(t, raml.PathToFileURI(lib), string(*link.Target))
	// Range must cover "types.raml" exactly.
	assert.Equal(t, uint32(len("types.raml")), link.Range.End.Character-link.Range.Start.Character)
}

// parseRAMLForTest, testFS, itemLabels and other shared helpers are defined in testhelpers_test.go.

// probe is one completion audit entry: a document, a cursor position, and
// expected label sets.
//
// The audit runner:
//   - opens the document once and collects parse diagnostics,
//   - sweeps EVERY line at both key-column (leading indent) and value-column
//     (end of line when a ": " is present), printing all results so that false
//     positives at wrong positions are immediately visible,
//   - then enforces mustHave / mustNotHave only at the designated (line, col).
type probe struct {
	name string
	doc  string
	line uint32
	col  uint32
	// mustHave: labels that MUST appear at (line, col).
	mustHave []string
	// mustNotHave: labels that MUST NOT appear at (line, col).
	mustNotHave []string
	// wantNil: when true, no completions are expected at all at (line, col).
	wantNil bool
	// wantNoDiagnostics: when true the sub-test fails if any parse
	// diagnostic is emitted (enforces that the test document is valid).
	wantNoDiagnostics bool
}

var auditProbes = []probe{
	// ── Root blank ────────────────────────────────────────────────────────────
	// L0: #%RAML 1.0  L1: title: t  L2: ← blank
	{
		name: "root blank",
		doc:  "#%RAML 1.0\ntitle: t\n\n",
		line: 2, col: 0,
		mustHave:    []string{"types", "baseUri", "protocols", "mediaType", "uses", "description", "annotationTypes"},
		mustNotHave: []string{"title", "string", "integer", "object", "get", "post", "properties"},
	},

	// ── Annotation type body (string) ─────────────────────────────────────────
	// L4: type: string  L5: ← blank
	{
		name: "annotation-type body / string",
		doc:  "#%RAML 1.0\ntitle: t\nannotationTypes:\n  audited:\n    type: string\n    \n",
		line: 5, col: 4,
		mustHave:    []string{"description", "pattern", "minLength", "maxLength"},
		mustNotHave: []string{"type", "title", "baseUri", "properties", "items", "additionalProperties"},
	},

	// ── Object type body ──────────────────────────────────────────────────────
	// L4: type: object  L5: ← blank
	{
		name: "type body / object",
		doc:  "#%RAML 1.0\ntitle: t\ntypes:\n  MyObj:\n    type: object\n    \n",
		line: 5, col: 4,
		mustHave:    []string{"properties", "additionalProperties", "description", "displayName"},
		mustNotHave: []string{"type", "pattern", "minLength", "items", "title", "get"},
	},

	// ── Custom facets inherited from parent ───────────────────────────────────
	// Extensible declares two custom facets; MyShape inherits from it.
	// Only myFacet is written; cursor on L11 (blank) — anotherFacet is still
	// available but myFacet must not be offered again (dedup).
	{
		name: "custom facet / inherited from parent",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Extensible:\n    type: object\n    facets:\n      myFacet: string\n      anotherFacet: integer\n  MyShape:\n    type: Extensible\n    myFacet: hello\n    \n",
		line: 11, col: 4,
		mustHave:          []string{"anotherFacet"},
		mustNotHave:       []string{"myFacet", "type", "title", "get", "post"},
		wantNoDiagnostics: false, // anotherFacet not yet assigned — parse error expected
	},

	// ── Custom facet absent for unrelated type ────────────────────────────────
	// L9: ← blank inside PlainType (no inheritance)
	{
		name: "custom facet / absent for unrelated type",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Extensible:\n    type: object\n    facets:\n      myFacet: string\n  PlainType:\n    type: object\n    \n",
		line: 9, col: 4,
		mustNotHave: []string{"myFacet"},
	},

	// ── Trait body ────────────────────────────────────────────────────────────
	// L4: description: Requires auth  L5: ← blank
	{
		name: "trait body",
		doc:  "#%RAML 1.0\ntitle: test\ntraits:\n  logged:\n    description: Requires auth\n    \n",
		line: 5, col: 4,
		mustHave:    []string{"headers", "queryParameters", "body", "responses", "securedBy", "is"},
		mustNotHave: []string{"description", "type", "uriParameters", "get", "post", "title", "properties"},
	},

	// ── Security scheme: type: value ─────────────────────────────────────────
	// L4: type: ← cursor after "    type: "
	{
		name: "security scheme / type: value",
		doc:  "#%RAML 1.0\ntitle: test\nsecuritySchemes:\n  myScheme:\n    type: \n",
		line: 4, col: uint32(len("    type: ")),
		mustHave:    []string{"OAuth 2.0", "Basic Authentication", "Digest Authentication", "Pass Through"},
		mustNotHave: []string{"string", "integer", "object", "array"},
	},

	// ── Security scheme body ──────────────────────────────────────────────────
	// L4: type: Basic Authentication  L5: ← blank
	{
		name: "security scheme body",
		doc:  "#%RAML 1.0\ntitle: test\nsecuritySchemes:\n  basic:\n    type: Basic Authentication\n    \n",
		line: 5, col: 4,
		mustHave:    []string{"description", "displayName", "describedBy"},
		mustNotHave: []string{"type", "string", "properties", "title", "queryParameters", "get"},
	},

	// ── Endpoint body ─────────────────────────────────────────────────────────
	// L3: description: A resource  L4: ← blank
	{
		name: "endpoint body",
		doc:  "#%RAML 1.0\ntitle: test\n/foo:\n  description: A resource\n  \n",
		line: 4, col: 2,
		mustHave:    []string{"displayName", "is", "type", "securedBy", "get", "post", "put", "delete", "uriParameters"},
		mustNotHave: []string{"description", "queryParameters", "queryString", "body", "responses", "string", "title"},
	},

	// ── Operation body ────────────────────────────────────────────────────────
	// L4: description: Gets stuff  L5: ← blank
	{
		name: "operation body",
		doc:  "#%RAML 1.0\ntitle: test\n/foo:\n  get:\n    description: Gets stuff\n    \n",
		line: 5, col: 4,
		mustHave:    []string{"queryParameters", "headers", "body", "responses", "is", "securedBy"},
		mustNotHave: []string{"description", "type", "uriParameters", "get", "post", "string", "title"},
	},

	// ── is: value ─────────────────────────────────────────────────────────────
	// L9: is: ← cursor after "    is: "
	{
		name: "is: value / trait names",
		doc:  "#%RAML 1.0\ntitle: test\ntraits:\n  secured:\n    description: Requires auth\n  paged:\n    description: Returns paged results\n/r:\n  get:\n    is: \n",
		line: 9, col: uint32(len("    is: ")),
		mustHave:    []string{"secured", "paged"},
		mustNotHave: []string{"string", "integer", "object", "OAuth 2.0"},
	},

	// ── securedBy: value ──────────────────────────────────────────────────────
	// L9: securedBy: ← cursor after "    securedBy: "
	{
		name: "securedBy: value / scheme names",
		doc:  "#%RAML 1.0\ntitle: test\nsecuritySchemes:\n  basic:\n    type: Basic Authentication\n  digest:\n    type: Digest Authentication\n/r:\n  get:\n    securedBy: \n",
		line: 9, col: uint32(len("    securedBy: ")),
		mustHave:    []string{"basic", "digest"},
		mustNotHave: []string{"string", "integer", "object", "paged"},
	},

	// ── Response body ─────────────────────────────────────────────────────────
	// L5: 200:  L6: ← blank inside 200 body
	{
		name: "response body",
		doc:  "#%RAML 1.0\ntitle: test\n/test:\n  get:\n    responses:\n      200:\n        \n",
		line: 6, col: 8,
		mustHave:    []string{"description", "headers", "body"},
		mustNotHave: []string{"queryParameters", "protocols", "is", "get", "type", "uriParameters"},
	},

	// ── protocols: inline value ───────────────────────────────────────────────
	// L2: protocols: ← cursor after "protocols: "
	{
		name: "protocols: value",
		doc:  "#%RAML 1.0\ntitle: test\nprotocols: \n",
		line: 2, col: uint32(len("protocols: ")),
		mustHave:    []string{"HTTP", "HTTPS"},
		mustNotHave: []string{"string", "integer", "object", "OAuth 2.0"},
	},

	// ── mediaType: inline value ───────────────────────────────────────────────
	// L2: mediaType: ← cursor after "mediaType: "
	{
		name: "mediaType: value",
		doc:  "#%RAML 1.0\ntitle: test\nmediaType: \n",
		line: 2, col: uint32(len("mediaType: ")),
		mustHave:    []string{"application/json", "application/xml"},
		mustNotHave: []string{"string", "integer", "get", "OAuth 2.0"},
	},

	// ── is: block sequence item ────────────────────────────────────────────────
	// Cursor is at col 6 on the `      - ` line (keyCol = indent = 6).
	// The sequence detection path should offer trait names, not key completions.
	{
		name: "is: block sequence item",
		doc:  "#%RAML 1.0\ntitle: test\ntraits:\n  secured:\n    description: Requires auth\n/r:\n  get:\n    is:\n      - secured\n      - \n",
		line: 9, col: 6,
		mustHave:    []string{"secured"},
		mustNotHave: []string{"queryParameters", "headers", "get", "post", "string"},
	},

	// ── securedBy: block sequence item ────────────────────────────────────────
	// Cursor is at col 6 on the `      - ` line (keyCol = indent = 6).
	{
		name: "securedBy: block sequence item",
		doc:  "#%RAML 1.0\ntitle: test\nsecuritySchemes:\n  basic:\n    type: Basic Authentication\n/r:\n  get:\n    securedBy:\n      - basic\n      - \n",
		line: 9, col: 6,
		mustHave:    []string{"basic"},
		mustNotHave: []string{"string", "integer", "get", "OAuth 2.0"},
	},

	// ── Root key dedup after annotationTypes + types ──────────────────────────
	// Blank at L7 falls between an annotationTypes body and a types block;
	// title / annotationTypes / types already defined — must not reappear.
	{
		name: "root keys / dedup after annotationTypes block",
		doc:  "#%RAML 1.0\ntitle: test\n\nannotationTypes:\n  myAnnotation:\n    type: string\n    description: an annotation\n\ntypes:\n  MyType:\n    type: string\n",
		line: 7, col: 0,
		mustHave:    []string{"version"},
		mustNotHave: []string{"title", "annotationTypes", "types"},
	},

	// ── String type body: blank line between existing facets ─────────────────
	// L6 is an empty blank inside MyType (string) body; displayName on L5 and
	// description on L7 are already declared — string facets must appear.
	{
		name: "string type body / blank with context",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  MyType:\n    type: string\n    displayName: A string type\n\n    description: desc\n",
		line: 6, col: 0,
		mustHave:    []string{"pattern", "minLength", "maxLength"},
		mustNotHave: []string{"properties", "items"},
	},

	// ── Object type body: completely empty blank inside body ─────────────────
	{
		name: "object type body / empty blank line",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  MyObj:\n    type: object\n\n    description: an object\n",
		line: 5, col: 0,
		mustHave:    []string{"properties", "additionalProperties"},
		mustNotHave: []string{"pattern", "items"},
	},

	// ── Short type body: trailing indented blank ─────────────────────────────
	// Single-facet body (`type: string`); new blank must still get string facets.
	{
		name: "short type body / trailing indented blank",
		doc:  "#%RAML 1.0\ntitle: test\n\ntypes:\n  Test:\n    type: string\n    \n",
		line: 6, col: 0,
		mustHave:    []string{"pattern", "minLength", "maxLength"},
		mustNotHave: []string{"properties", "type"},
	},

	// ── Inferred type body: trailing blank ───────────────────────────────────
	// No explicit `type:` key; description already written — must not repeat.
	{
		name: "inferred type body / trailing blank",
		doc:  "#%RAML 1.0\ntitle: test\n\ntypes:\n  Test:\n    description: a type\n    \n",
		line: 6, col: 0,
		mustHave:    []string{"type", "displayName"},
		mustNotHave: []string{"description"},
	},

	// ── Type-name-level blanks: no completions expected ──────────────────────
	{
		name: "type name level / single type",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  TypeOne:\n    type: string\n  \n",
		line: 5, col: 0,
		wantNil: true,
	},
	{
		name: "type name level / between two types",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  TypeOne:\n    type: string\n  \n  TypeTwo:\n    type: object\n",
		line: 5, col: 0,
		wantNil: true,
	},

	// ── Endpoint / method key lines: no type names ───────────────────────────
	{
		name: "endpoint key line / no type names",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  MyType: object\n\n/test:\n  get:\n    responses:\n      200:\n",
		line: 5, col: 7,
		mustNotHave: []string{"string", "object", "array", "integer", "number", "MyType"},
	},
	{
		name: "method key line / no type names",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  MyType: object\n\n/test:\n  get:\n    responses:\n      200:\n",
		line: 6, col: 6,
		mustNotHave: []string{"string", "object", "array", "MyType"},
	},

	// ── Scalar facet value: no type names ────────────────────────────────────
	{
		name: "scalar facet value / no type names",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Test:\n    type: string\n    minLength: \n",
		line: 5, col: uint32(len("    minLength: ")),
		mustNotHave: []string{"string", "integer", "object", "array", "number", "boolean"},
	},

	// ── Union type: second-member completions ────────────────────────────────
	{
		name: "union type / inline second member",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Test: string | \n  Explicit:\n    type: string | \n",
		line: 3, col: uint32(len("  Test: string | ")),
		mustHave: []string{"integer", "boolean", "number", "object", "array"},
	},
	{
		name: "union type / explicit type key second member",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Test: string | \n  Explicit:\n    type: string | \n",
		line: 5, col: uint32(len("    type: string | ")),
		mustHave: []string{"integer", "boolean", "number"},
	},

	// ── Boolean facet value ───────────────────────────────────────────────────
	{
		name: "boolean facet value",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Prop:\n    type: string\n    required: \n",
		line: 5, col: uint32(len("    required: ")),
		mustHave:    []string{"true", "false"},
		mustNotHave: []string{"string", "object"},
	},

	// ── Format facet value ────────────────────────────────────────────────────
	{
		name: "format facet value",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Age:\n    type: integer\n    format: \n",
		line: 5, col: uint32(len("    format: ")),
		mustHave:    []string{"int", "int32", "int64", "long", "float"},
		mustNotHave: []string{"string", "boolean"},
	},

	// ── Freeform map keys: RAML structural keys suppressed ───────────────────
	{
		name: "headers map key / RAML keys suppressed",
		doc:  "#%RAML 1.0\ntitle: test\n/foo:\n  get:\n    description: test\n    headers:\n      \n",
		line: 6, col: uint32(len("      ")),
		mustNotHave: []string{"body", "responses", "queryParameters", "displayName", "type", "uriParameters"},
	},
	{
		name: "queryParameters map key / RAML keys suppressed",
		doc:  "#%RAML 1.0\ntitle: test\n/foo:\n  get:\n    description: test\n    queryParameters:\n      \n",
		line: 6, col: uint32(len("      ")),
		mustNotHave: []string{"body", "responses", "headers", "type", "uriParameters", "get"},
	},

	// ── Named-parameter body ─────────────────────────────────────────────────
	{
		name: "named param body",
		doc:  "#%RAML 1.0\ntitle: test\n/foo:\n  get:\n    queryParameters:\n      page:\n        type: integer\n        \n",
		line: 7, col: uint32(len("        ")),
		mustHave:    []string{"description", "required", "default", "example"},
		mustNotHave: []string{"body", "responses", "queryParameters", "headers", "uriParameters"},
	},

	// ── HTTP status codes under responses: ───────────────────────────────────
	{
		name: "status codes under responses:",
		doc:  "#%RAML 1.0\ntitle: test\n/foo:\n  get:\n    responses:\n      \n",
		line: 5, col: 6,
		mustHave:    []string{"200", "201", "204", "400", "401", "403", "404", "422", "500", "503"},
		mustNotHave: []string{"description", "headers", "body", "type", "get", "post"},
	},

	// ── Resource type names on endpoint type: ────────────────────────────────
	{
		name: "endpoint type: / resource type names",
		doc:  "#%RAML 1.0\ntitle: test\nresourceTypes:\n  collection:\n    description: A collection resource\n  single:\n    description: A single item resource\n/r:\n  type: \n",
		line: 8, col: uint32(len("  type: ")),
		mustHave:    []string{"collection", "single"},
		mustNotHave: []string{"string", "integer", "object", "array", "number"},
	},

	// ── type: inside type body must offer shape types ─────────────────────────
	{
		name: "type: in type body / shape types",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  MyType:\n    type: \n",
		line: 4, col: uint32(len("    type: ")),
		mustHave: []string{"string", "object", "integer"},
	},

	// ── Block-sequence type completions ──────────────────────────────────────
	{
		name: "block seq type / more-indented item",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  TypeA:\n    type: string\n  TypeB:\n    type: string\n  MyType:\n    type:\n      - TypeA\n      - \n",
		line: 10, col: 6,
		mustHave: []string{"TypeA", "TypeB", "string"},
	},
	{
		name: "block seq type / same-indent item",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  TypeA:\n    type: string\n  TypeB:\n    type: string\n  MyType:\n    type:\n    - TypeA\n    - \n",
		line: 10, col: 4,
		mustHave: []string{"TypeA", "TypeB", "string"},
	},
	{
		name: "block seq type / union continuation",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  TypeA:\n    type: string\n  TypeB:\n    type: string\n  MyType:\n    type:\n      - TypeA\n      - TypeB | \n",
		line: 10, col: uint32(len("      - TypeB | ")),
		mustHave: []string{"TypeA", "TypeB", "string"},
	},

	// ── Custom facet keys ─────────────────────────────────────────────────────
	// Both facets offered when neither has been written yet.
	{
		name: "custom facet keys / both offered",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Extensible:\n    type: object\n    facets:\n      myFacet: string\n      anotherFacet: integer\n  MyShape:\n    type: Extensible\n    \n",
		line: 10, col: 0,
		mustHave:    []string{"myFacet", "anotherFacet"},
		mustNotHave: []string{"type", "title", "get"},
	},

	// ── Annotation key completions ────────────────────────────────────────────
	{
		name: "annotation key completions",
		doc:  "#%RAML 1.0\ntitle: test\nannotationTypes:\n  myAnnotation:\n    type: string\ntypes:\n  MyType:\n    type: string\n    \n",
		line: 8, col: 0,
		mustHave: []string{"(myAnnotation)"},
	},

	// ── Typed CDP value completions (RoleTypedObjectKey) ─────────────────────

	// Cursor on a blank line directly inside a structured (object-typed)
	// annotation value body — should offer the annotation's property names.
	{
		name: "CDP value / direct object annotation",
		doc:  "#%RAML 1.0\ntitle: test\nannotationTypes:\n  deprecated:\n    properties:\n      since:\n        type: string\n        required: true\n      reason:\n        type: string\n        required: false\ntypes:\n  MyType:\n    type: object\n    (deprecated):\n      \n",
		line: 15, col: 6,
		mustHave:    []string{"since", "reason"},
		mustNotHave: []string{"type", "title", "properties", "get", "post"},
	},

	// Same as above but `since` is already written — dedup must exclude it.
	{
		name: "CDP value / dedup already-written property",
		doc:  "#%RAML 1.0\ntitle: test\nannotationTypes:\n  deprecated:\n    properties:\n      since:\n        type: string\n        required: true\n      reason:\n        type: string\n        required: false\ntypes:\n  MyType:\n    type: object\n    (deprecated):\n      since: \"2.0\"\n      \n",
		line: 16, col: 6,
		mustHave:    []string{"reason"},
		mustNotHave: []string{"since"},
	},

	// Cursor after `- ` inside an array-of-objects annotation value where
	// the `- ` is MORE INDENTED than the annotation key (standard form).
	{
		name: "CDP value / array-of-objects annotation",
		doc:  "#%RAML 1.0\ntitle: test\nannotationTypes:\n  roles:\n    type: array\n    items:\n      type: object\n      properties:\n        id:\n          type: string\n        name:\n          type: string\ntypes:\n  MyType:\n    type: object\n    (roles):\n      - \n",
		line: 16, col: 8,
		mustHave:    []string{"id", "name"},
		mustNotHave: []string{"type", "title", "properties", "get"},
	},

	// Same array annotation but the `- ` is at the SAME indent as `(roles):`.
	// This is valid YAML (same-indent block sequence) and was the bug that
	// exposed the text-scanning approach as fragile.
	{
		name: "CDP value / array-of-objects annotation same-indent sequence",
		doc:  "#%RAML 1.0\ntitle: test\nannotationTypes:\n  roles:\n    type: array\n    items:\n      type: object\n      properties:\n        id:\n          type: string\n        name:\n          type: string\ntypes:\n  MyType:\n    type: object\n    (roles):\n    - \n",
		line: 16, col: 6,
		mustHave:    []string{"id", "name"},
		mustNotHave: []string{"type", "title", "properties", "get"},
	},

	// Cursor on a blank line inside a custom-facet (non-annotation) value body
	// where the facet is typed as an object — should offer the facet's property
	// names, not general RAML keys.
	{
		name: "custom facet value / object type",
		doc:  "#%RAML 1.0\ntitle: test\ntypes:\n  Extensible:\n    facets:\n      meta:\n        type: object\n        properties:\n          author:\n            type: string\n            required: false\n          version:\n            type: integer\n            required: false\n  MyShape:\n    type: Extensible\n    meta:\n      \n",
		line: 17, col: 6,
		mustHave:    []string{"author", "version"},
		mustNotHave: []string{"type", "title", "facets", "properties", "get"},
	},
	// Cursor on a blank line inside a nested object property within an
	// annotation value body — must offer the sub-property names, not the
	// top-level annotation properties.
	{
		name: "CDP value / nested object property",
		doc:  "#%RAML 1.0\ntitle: test\nannotationTypes:\n  meta:\n    properties:\n      author:\n        type: object\n        properties:\n          name:\n            type: string\n            required: false\n          email:\n            type: string\n            required: false\ntypes:\n  MyType:\n    type: object\n    (meta):\n      author:\n        \n",
		line: 19, col: 8,
		mustHave:    []string{"name", "email"},
		mustNotHave: []string{"author", "type", "title"},
	},

	// Cursor after `- ` inside a nested array property within an annotation
	// value body — must offer the array item's object properties.
	{
		name: "CDP value / nested array property",
		doc:  "#%RAML 1.0\ntitle: test\nannotationTypes:\n  meta:\n    properties:\n      tags:\n        type: array\n        items:\n          type: object\n          properties:\n            id:\n              type: string\n              required: false\n            label:\n              type: string\n              required: false\ntypes:\n  MyType:\n    type: object\n    (meta):\n      tags:\n        - \n",
		line: 21, col: 8,
		mustHave:    []string{"id", "label"},
		mustNotHave: []string{"tags", "type", "title", "properties"},
	},

	// Cursor on a blank line at the SAME indent as the already-written keys
	// inside a sequence-item mapping, where the last written key has a
	// block-mapping value.  The cursor is a sibling (same column as `id:` and
	// `meta:`), NOT inside `meta:`'s sub-block.  Must offer the remaining
	// item-level properties (`name`), not `meta:`'s sub-properties.
	//
	// This is a regression test: the former line-only span check in
	// mappingKeyContaining incorrectly descended into `meta:`'s value block
	// because it had no next sibling, causing sub-properties to be offered.
	{
		name: "CDP value / array item sibling after last sub-object",
		// L0-L16: annotation type definition for `roles` (array of objects
		//   with id/meta/name), L17-L24: type using (roles), L25: blank cursor.
		doc:  "#%RAML 1.0\ntitle: t\nannotationTypes:\n  roles:\n    type: array\n    items:\n      type: object\n      properties:\n        id:\n          type: string\n        meta:\n          type: object\n          properties:\n            key: string\n            value: string\n        name:\n          type: string\ntypes:\n  MyType:\n    type: object\n    (roles):\n    - id: foo\n      meta:\n        key: k\n        value: v\n      \n",
		line: 25, col: 6,
		mustHave:    []string{"name"},
		mustNotHave: []string{"key", "value", "id", "meta"},
	},
}

// TestLSP_CompletionAudit runs every probe as an independent sub-test.
//
// For each probe the sub-test:
//  1. Opens the document once and collects parse diagnostics via
//     notifyCapture.waitDiags — diagnostics indicate an unreliable model.
//  2. Sweeps EVERY document line at two columns:
//     • key-col  = leading-whitespace count (where a new key would be typed),
//     • val-col  = len(line) when the line ends with ": " or similar value context.
//     The output shows all completions returned, making false positives that
//     appear at structurally-wrong positions immediately visible.
//  3. Enforces mustHave / mustNotHave at the designated (probe.line, probe.col).
//  4. Fails if diagnostics are non-empty, mustHave items are absent, or
//     mustNotHave items are present.
//
// Run with: go test -v -run TestLSP_CompletionAudit ./...
func TestLSP_CompletionAudit(t *testing.T) {
	for _, p := range auditProbes {
		p := p
		t.Run(p.name, func(t *testing.T) {
			runProbeAudit(t, p)
		})
	}
}

// runProbeAudit is the per-probe driver described in TestLSP_CompletionAudit.
func runProbeAudit(t *testing.T, p probe) {
	t.Helper()

	// ── 1. Open document once, collect diagnostics ─────────────────────────
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()
	uri := openDoc(t, s, nc, dir, "api.raml", p.doc)
	waitParse(t, s.cache, uri)
	diags := nc.waitDiags(t, uri)

	docLines := strings.Split(p.doc, "\n")
	ctx := fakeCtx(nc)

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n=== PROBE: %s ===\n", p.name)

	// ── Diagnostics section ────────────────────────────────────────────────
	if len(diags) == 0 {
		fmt.Fprintf(&sb, "Diagnostics : NONE (clean parse)\n")
	} else {
		fmt.Fprintf(&sb, "Diagnostics : %d ERROR(s) — completion results may be unreliable!\n", len(diags))
		for _, d := range diags {
			fmt.Fprintf(&sb, "  L%-3d col%-3d : %s\n",
				d.Range.Start.Line, d.Range.Start.Character, d.Message)
		}
	}

	// ── 2. Line sweep ──────────────────────────────────────────────────────
	fmt.Fprintf(&sb, "\n%-2s  %-4s  %-4s  %-45s  %s\n", "mk", "LINE", "COL", "LINE TEXT", "COMPLETIONS")
	fmt.Fprintf(&sb, "%s\n", strings.Repeat("-", 110))

	complete := func(lineIdx uint32, col uint32) []string {
		raw, _ := s.completion(ctx, &protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
				Position:     protocol.Position{Line: lineIdx, Character: col},
			},
		})
		if raw == nil {
			return nil
		}
		return itemLabels(raw.([]protocol.CompletionItem))
	}

	for lineIdx, lineText := range docLines {
		// Key column: number of leading spaces (where a new YAML key begins).
		trimmed := strings.TrimLeft(lineText, " \t")
		keyCol := uint32(len(lineText) - len(trimmed))

		// Value column: end of line, relevant when a ": " exists on the line.
		hasValueContext := strings.Contains(lineText, ": ") || strings.HasSuffix(lineText, ": ")
		valCol := uint32(len(lineText))

		displayText := lineText
		if len(displayText) > 43 {
			displayText = displayText[:40] + "..."
		}

		// Key position.
		isKeyTarget := uint32(lineIdx) == p.line && keyCol == p.col
		keyMark := "  "
		if isKeyTarget {
			keyMark = "=>"
		}
		keyLabels := complete(uint32(lineIdx), keyCol)
		fmt.Fprintf(&sb, "%s  L%-2d  c%-3d  %-45s  %v\n",
			keyMark, lineIdx, keyCol, displayText, keyLabels)

		// Value position (only when the line contains ": " or ends with ": ").
		if hasValueContext && valCol != keyCol {
			isValTarget := uint32(lineIdx) == p.line && valCol == p.col
			valMark := "  "
			if isValTarget {
				valMark = "=>"
			}
			valLabels := complete(uint32(lineIdx), valCol)
			fmt.Fprintf(&sb, "%s  L%-2d  c%-3d  %-45s  %v\n",
				valMark, lineIdx, valCol, displayText+" [val]", valLabels)
		}
	}

	// ── 3. mustHave / mustNotHave at target ────────────────────────────────
	targetLabels := complete(p.line, p.col)
	gotSet := make(map[string]bool, len(targetLabels))
	for _, l := range targetLabels {
		gotSet[l] = true
	}

	var failures []string
	if p.wantNil {
		if len(targetLabels) > 0 {
			failures = append(failures, fmt.Sprintf("WANT NIL completions at L%d c%d, got %d item(s): %v",
				p.line, p.col, len(targetLabels), targetLabels))
		}
	} else {
		for _, want := range p.mustHave {
			if !gotSet[want] {
				failures = append(failures, fmt.Sprintf("MISSING %q (must-have absent at L%d c%d)", want, p.line, p.col))
			}
		}
		for _, bad := range p.mustNotHave {
			if gotSet[bad] {
				failures = append(failures, fmt.Sprintf("WRONG %q (must-not-have present at L%d c%d)", bad, p.line, p.col))
			}
		}
	}
	if p.wantNoDiagnostics {
		for _, d := range diags {
			failures = append(failures, fmt.Sprintf("DIAGNOSTIC L%d: %s", d.Range.Start.Line, d.Message))
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
