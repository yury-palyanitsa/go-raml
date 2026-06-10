package server

import (
	"path/filepath"
	"testing"

	raml "github.com/acronis/go-raml/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ---- Helpers used only by this file's integration tests -------------------

// findEntry returns the PosEntry for a shape with the given name from a PositionMap.
func findEntry(pm *PositionMap, name string) (PosEntry, bool) {
	for _, entries := range pm.lineEntries {
		for _, e := range entries {
			// Only match definition-site entries: the entry's position must match
			// the shape's own key position. TypeExprRef entries sit at the reference
			// site (e.Line != e.Shape.Line) and must be ignored here.
			if e.Shape != nil && e.Shape.Name == name && e.Line == e.Shape.KeyPos.Line {
				return e, true
			}
		}
	}
	return PosEntry{}, false
}

// symbolNames returns the Name field of each DocumentSymbol (top level only).
// symbolNames returns a flat list of every symbol name reachable from syms,
// including all descendants, by a depth-first traversal.
func symbolNames(syms []protocol.DocumentSymbol) []string {
	var names []string
	var collect func([]protocol.DocumentSymbol)
	collect = func(ss []protocol.DocumentSymbol) {
		for _, s := range ss {
			names = append(names, s.Name)
			collect(s.Children)
		}
	}
	collect(syms)
	return names
}

// findSymbol returns the first symbol (at any depth) whose name matches.
func findSymbol(syms []protocol.DocumentSymbol, name string) (protocol.DocumentSymbol, bool) {
	for _, s := range syms {
		if s.Name == name {
			return s, true
		}
		if found, ok := findSymbol(s.Children, name); ok {
			return found, ok
		}
	}
	return protocol.DocumentSymbol{}, false
}

// lspPos converts a 1-based RAML PosEntry position to a 0-based LSP Position.
func lspPos(e PosEntry) protocol.Position {
	return protocol.Position{
		Line:      uint32(e.Line - 1),
		Character: uint32(e.Col - 1),
	}
}

// dumpPosMap prints every entry in pm to t.Log. Only visible with -v.
func dumpPosMap(t *testing.T, pm *PositionMap) {
	t.Helper()
	if !testing.Verbose() {
		return
	}
	for line, entries := range pm.lineEntries {
		for _, e := range entries {
			switch {
			case e.BuiltinType != "":
				t.Logf("  posmap line=%d col=%d builtin=%q", line, e.Col, e.BuiltinType)
			case e.Shape != nil:
				t.Logf("  posmap line=%d col=%d shape=%q dest=%s:%d", line, e.Col, e.Shape.Name, e.DestPath, e.DestLine)
			case e.LibLink != nil:
				t.Logf("  posmap line=%d col=%d lib=%q", line, e.Col, e.LibAlias)
			}
		}
	}
}

// ---- Integration tests ----------------------------------------------------

// TestLSP_DocumentSymbols verifies that types in a valid RAML file are returned
// as DocumentSymbols with the correct kinds.
func TestLSP_DocumentSymbols(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	uri := openDoc(t, s, nc, dir, "api.raml", ramlValid)
	waitParse(t, s.cache, uri)

	ctx := fakeCtx(nc)
	raw, err := s.documentSymbol(ctx, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
	})
	require.NoError(t, err)
	require.NotNil(t, raw, "documentSymbol should return at least one symbol")

	syms := raw.([]protocol.DocumentSymbol)
	names := symbolNames(syms)
	assert.Contains(t, names, "Person")
	assert.Contains(t, names, "Employee")

	for _, name := range []string{"Person", "Employee"} {
		sym, ok := findSymbol(syms, name)
		require.True(t, ok, "%s symbol should be found in the tree", name)
		assert.Equal(t, protocol.SymbolKindClass, sym.Kind,
			"%s should be SymbolKindClass (ObjectShape)", name)
	}
}

// TestLSP_Definition verifies go-to-definition using TypeExprRefs:
//   - The key-position entry for a named type (PersonRef) has no navigation dest.
//   - A TypeExprRef entry at the value position (where "Person" was written) navigates
//     to Person's definition.
//   - Employee's key position also has no dest (it is the definition site, not a ref).
func TestLSP_Definition(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	uri := openDoc(t, s, nc, dir, "api.raml", ramlValid)
	result := waitParse(t, s.cache, uri)
	require.NotNil(t, result.PosMaps)

	pm := result.PosMaps[uri]
	require.NotNil(t, pm)

	// The key-position entry for PersonRef is the definition site — no navigation.
	personRefEntry, ok := findEntry(pm, "PersonRef")
	require.True(t, ok, "PersonRef should have a position map entry")
	assert.Empty(t, personRefEntry.DestPath,
		"PersonRef's key position is its own definition site, not a type reference")

	// The TypeExprRef entry is on the same line but at the column of the "Person"
	// text: col = personRefEntry.Col + len("PersonRef") + len(": ") = +11.
	refLSPLine := uint32(personRefEntry.Line - 1)
	refLSPChar := uint32(personRefEntry.Col - 1 + len("PersonRef") + len(": "))
	refEntry, found := pm.HitTest(refLSPLine, refLSPChar)
	require.True(t, found, "there should be a TypeExprRef entry for 'Person' reference")
	require.NotEmpty(t, refEntry.DestPath, "the 'Person' type reference should have a definition destination")

	ctx := fakeCtx(nc)
	raw, err := s.definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: refLSPLine, Character: refLSPChar},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, raw, "definition should return a location")

	links := raw.([]protocol.LocationLink)
	require.Len(t, links, 1)
	link := links[0]
	assert.Equal(t, protocol.DocumentUri(uri), link.TargetURI, "definition should point back to the same file")

	personEntry, ok := findEntry(pm, "Person")
	require.True(t, ok)
	assert.Equal(t, uint32(personEntry.Line-1), link.TargetSelectionRange.Start.Line,
		"definition target line should be Person's declaration line")

	// OriginSelectionRange must cover the "Person" token at the reference site.
	require.NotNil(t, link.OriginSelectionRange, "OriginSelectionRange should be set")
	assert.Equal(t, uint32(refLSPLine), link.OriginSelectionRange.Start.Line,
		"origin range should be on the reference line")
	originLen := link.OriginSelectionRange.End.Character - link.OriginSelectionRange.Start.Character
	assert.Equal(t, uint32(len("Person")), originLen, "origin range should cover exactly the type name")

	// TargetSelectionRange must also cover the full "Person" name at the definition site.
	targetLen := link.TargetSelectionRange.End.Character - link.TargetSelectionRange.Start.Character
	assert.Equal(t, uint32(len("Person")), targetLen, "target selection range should cover the full type name")

	// Employee's key position is its own definition site — no navigation.
	employeeEntry, ok := findEntry(pm, "Employee")
	require.True(t, ok, "Employee should have a position map entry")
	assert.Empty(t, employeeEntry.DestPath,
		"Employee's key position is its own definition site, not a type reference")
}

// TestLSP_ArrayDefinition verifies that go-to-definition and OriginSelectionRange
// work correctly for array shorthand notation (type: Agent[]).
// The "Agent" reference inside the expression must:
//   - navigate to Agent's definition
//   - produce an OriginSelectionRange covering exactly "Agent" (not the full "Agent[]")
func TestLSP_ArrayDefinition(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	uri := openDoc(t, s, nc, dir, "api.raml", ramlArrayNotation)
	result := waitParse(t, s.cache, uri)
	require.NotNil(t, result.PosMaps)

	pm := result.PosMaps[uri]
	require.NotNil(t, pm)
	dumpPosMap(t, pm)

	// Locate the TestList definition entry → its key line gives us the type expr line.
	testListEntry, ok := findEntry(pm, "TestList")
	require.True(t, ok, "TestList should have a posmap entry")
	// The type expr "Test[]" is on the line after TestList's key line.
	// Its value starts at column len("    type: ")+1 = 11 (1-based).
	refRAMLLine := testListEntry.Line + 1
	refRAMLCol := 11 // 1-based: 4 spaces + "type: " = 10 chars, then "T" at col 11
	refLSPLine := uint32(refRAMLLine - 1)
	refLSPChar := uint32(refRAMLCol - 1)

	refEntry, found := pm.HitTest(refLSPLine, refLSPChar)
	require.True(t, found, "posmap must have an entry at the 'Test' reference inside 'Test[]'")
	require.NotEmpty(t, refEntry.DestPath, "'Test' inside 'Test[]' must have a definition destination")

	ctx := fakeCtx(nc)
	raw, err := s.definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     protocol.Position{Line: refLSPLine, Character: refLSPChar},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, raw, "definition should return a location for 'Test' reference")

	links := raw.([]protocol.LocationLink)
	require.Len(t, links, 1)
	link := links[0]
	assert.Equal(t, protocol.DocumentUri(uri), link.TargetURI, "definition should point back to the same file")

	testEntry, ok := findEntry(pm, "Test")
	require.True(t, ok)
	assert.Equal(t, uint32(testEntry.Line-1), link.TargetSelectionRange.Start.Line,
		"definition target should be Test's declaration line")

	// OriginSelectionRange must cover exactly "Test" (4 chars), not "Test[]" (6 chars).
	require.NotNil(t, link.OriginSelectionRange, "OriginSelectionRange should be set")
	assert.Equal(t, refLSPLine, link.OriginSelectionRange.Start.Line,
		"origin range should be on the reference line")
	originLen := link.OriginSelectionRange.End.Character - link.OriginSelectionRange.Start.Character
	assert.Equal(t, uint32(len("Test")), originLen,
		"origin range should cover exactly 'Test', not the full 'Test[]' expression")

	// TargetSelectionRange must also cover the full "Test" name at the definition site.
	targetLen := link.TargetSelectionRange.End.Character - link.TargetSelectionRange.Start.Character
	assert.Equal(t, uint32(len("Test")), targetLen,
		"target selection range should cover 'Test' at the definition site")
}

// TestLSP_References asserts that requesting references for the Person type
// returns a location for Employee (which inherits from Person).
func TestLSP_References(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	uri := openDoc(t, s, nc, dir, "api.raml", ramlValid)
	result := waitParse(t, s.cache, uri)
	require.NotNil(t, result.PosMaps)
	require.NotNil(t, result.RefIndex)

	pm := result.PosMaps[uri]
	require.NotNil(t, pm)

	personEntry, ok := findEntry(pm, "Person")
	require.True(t, ok)

	ctx := fakeCtx(nc)
	locs, err := s.references(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(uri)},
			Position:     lspPos(personEntry),
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, locs, "Person should have at least one reference (Employee inherits it)")

	refURIs := make([]string, 0, len(locs))
	for _, l := range locs {
		refURIs = append(refURIs, l.URI)
	}
	assert.Contains(t, refURIs, uri,
		"Employee is in the same file, so its URI should appear in references")
}

// TestLSP_LibraryDependency verifies that after parsing an API that uses a library,
// the dependency graph records library.raml → api file.
func TestLSP_LibraryDependency(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	// Register the library in the server's in-memory store so VirtualFS can
	// serve it without a disk file.  The library is not opened as an LSP
	// document (no didOpen call), but VirtualFS will find it in the store.
	libPath := filepath.Join(dir, "library.raml")
	libURI := raml.PathToFileURI(libPath)
	s.store.Set(libURI, ramlLibrary)

	// Open the API that uses the library.
	apiURI := openDoc(t, s, nc, dir, "api-with-lib.raml", ramlUsesLibrary)
	waitParse(t, s.cache, apiURI)

	apiResult := s.cache.Get(apiURI)
	require.NotNil(t, apiResult.RAML)
	assert.Empty(t, apiResult.Diagnostics, "API that uses library should parse without errors")

	// Entity should appear in document symbols.
	ctx := fakeCtx(nc)
	raw, err := s.documentSymbol(ctx, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(apiURI)},
	})
	require.NoError(t, err)
	require.NotNil(t, raw)
	names := symbolNames(raw.([]protocol.DocumentSymbol))
	assert.Contains(t, names, "Entity")

	// The dependency graph should record library → api.
	deps := s.depg.Dependents(libURI)
	assert.Contains(t, deps, apiURI,
		"dependency graph should record that api-with-lib.raml depends on library.raml")
}

// TestLSP_LibraryReferences verifies that Find All References on a library alias
// returns the locations where the alias is used as a type prefix (lib.Base).
//
// The fixture is:
//
//	uses:
//	  lib: library.raml   ← declaration at key position
//	types:
//	  Entity:
//	    type: lib.Base    ← usage of lib as prefix
//
// Hovering / requesting references at the "lib" alias key position must
// return [at least] the location of "lib" in "lib.Base".
func TestLSP_LibraryReferences(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	libPath := filepath.Join(dir, "library.raml")
	s.store.Set(raml.PathToFileURI(libPath), ramlLibrary)

	apiURI := openDoc(t, s, nc, dir, "api-with-lib.raml", ramlUsesLibrary)
	result := waitParse(t, s.cache, apiURI)
	require.NotNil(t, result.PosMaps)
	require.NotNil(t, result.RefIndex, "RefIndex must be built for a successfully parsed file")

	pm := result.PosMaps[apiURI]
	require.NotNil(t, pm)
	dumpPosMap(t, pm)

	// Find the posmap entry at the "lib" alias key position.
	// ramlUsesLibrary line 3 (1-based): "  lib: library.raml"
	// lspLine = 3 (0-based), char = 2 (column of 'l' in 'lib').
	// We shift to the key by hitting the HitTest at that position.
	keyEntry, found := pm.HitTest(3, 2)
	require.True(t, found, "posmap must have an entry at the 'lib' alias key position")
	require.NotNil(t, keyEntry.LibLink, "entry at uses: key must reference a LibraryLink")
	assert.Equal(t, "lib", keyEntry.LibAlias, "LibAlias must be 'lib'")
	assert.NotZero(t, keyEntry.LibLink.ID, "LibraryLink.ID must be non-zero (populated by go-raml)")

	// Find All References via the server handler.
	ctx := fakeCtx(nc)
	locs, err := s.references(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(apiURI)},
			Position:     protocol.Position{Line: 3, Character: 2},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, locs, "Find All References on 'lib' must return at least one location (lib.Base usage)")

	// All returned locations must be in the same file.
	for _, l := range locs {
		assert.Equal(t, protocol.DocumentUri(apiURI), l.URI)
	}

	// The usage location (lib.Base) must be among the results.
	// ramlUsesLibrary line 7 (1-based RAML) = line 6 (0-based LSP): "    type: lib.Base"
	// The 'lib' prefix starts at column 11 (1-based RAML) = column 10 (0-based LSP).
	usageFound := false
	for _, l := range locs {
		if l.Range.Start.Line == 6 {
			usageFound = true
		}
	}
	assert.True(t, usageFound, "a reference at the 'lib.Base' usage line must appear in results")
}
