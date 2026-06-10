package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	raml "github.com/acronis/go-raml/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// pinAndExpect calls ramlLsp/setRoot via the custom handler and asserts the
// echoed URI matches the input. Helper to keep the smoke tests legible.
func pinAndExpect(t *testing.T, s *Server, workspaceURI, rootURI string) {
	t.Helper()
	payload, err := json.Marshal(rootPayload{Workspace: workspaceURI, URI: rootURI})
	require.NoError(t, err)
	resp, err := s.handleSetRoot(nil, payload)
	require.NoError(t, err)
	assert.Equal(t, rootURI, resp.(rootResponse).URI)
}

// TestLSP_PinnedRoot_ReparseFromFragmentEdit verifies the core feature: when
// a root is pinned, editing an unrelated fragment (here, an open library)
// triggers a re-parse of the pinned root rather than (only) the fragment.
// The library file gets diagnostics emitted from the root's parse context,
// not from its own standalone fragment parse.
func TestLSP_PinnedRoot_ReparseFromFragmentEdit(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	// Workspace folder = test dir. The pinned root will live inside it.
	workspaceURI := raml.PathToFileURI(dir)
	s.cache.SetWorkspaceFolders([]string{dir})

	// Library defines a single type. We write it to disk so the pinned root
	// can include it via uses: when re-parsed.
	libPath := filepath.Join(dir, "lib.raml")
	libContent := `#%RAML 1.0 Library
types:
  Base:
    type: object
    properties:
      id: integer
`
	require.NoError(t, os.WriteFile(libPath, []byte(libContent), 0o644))
	libURI := raml.PathToFileURI(libPath)

	// Root API references the library. Initially valid.
	rootContent := `#%RAML 1.0
title: Pinned Root
uses:
  lib: lib.raml
types:
  Entity:
    type: lib.Base
`
	rootURI := openDoc(t, s, nc, dir, "api.raml", rootContent)
	// Wait for the standalone parse to complete so we know didOpen finished.
	waitParse(t, s.cache, rootURI)

	// Pin the root. The handler enqueues an immediate reparse via EnqueueNow.
	pinAndExpect(t, s, workspaceURI, rootURI)
	waitParse(t, s.cache, rootURI)

	// Now open the library file. With the pin, didOpen routes the parse to
	// the pinned root URI — the library file's own URI must NOT receive its
	// own ParseResult.
	nc.reset()
	libOpenURI := openDoc(t, s, nc, dir, "lib.raml", libContent)
	require.Equal(t, libURI, libOpenURI)
	waitParse(t, s.cache, rootURI)
	assert.Nil(t, s.cache.Get(libURI), "library must not have a standalone result while pin is active")

	// Edit the library to break the reference target (rename Base → Renamed).
	// With the pin, didChange enqueues the ROOT, not the library — and the
	// root reparse should produce a diagnostic that lands in lib.raml at
	// the point where the type definition is broken, AND/OR in api.raml at
	// the uses: site for the now-missing referent.
	brokenLib := `#%RAML 1.0 Library
types:
  Renamed:
    type: object
    properties:
      id: integer
`
	nc.reset()
	require.NoError(t, s.didChange(fakeCtx(nc), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: protocol.DocumentUri(libURI)},
		},
		ContentChanges: []any{protocol.TextDocumentContentChangeEventWhole{Text: brokenLib}},
	}))

	// The root reparse should land in c.results[rootURI]. The library's own
	// cache entry should remain absent.
	waitParse(t, s.cache, rootURI)
	assert.Nil(t, s.cache.Get(libURI), "library still must not have a standalone result")

	// A diagnostic must have been published for api.raml (the failing
	// reference). The library file may or may not receive an empty-clear,
	// depending on whether the previous parse mentioned it.
	rootDiags := nc.waitDiags(t, rootURI)
	assert.NotEmpty(t, rootDiags, "expected a diagnostic on api.raml after breaking the library")
}

// TestLSP_PinnedRoot_RootForLookup verifies the RootFor / PinnedRoot helpers
// behave as expected under various conditions.
func TestLSP_PinnedRoot_RootForLookup(t *testing.T) {
	s := newTestServer()
	dir := t.TempDir()
	s.cache.SetWorkspaceFolders([]string{dir})

	rootURI := raml.PathToFileURI(filepath.Join(dir, "api.raml"))
	libURI := raml.PathToFileURI(filepath.Join(dir, "sub", "lib.raml"))

	// No pin → RootFor returns the input unchanged.
	assert.Equal(t, libURI, s.cache.RootFor(libURI))
	assert.Empty(t, s.cache.PinnedRoot(libURI))

	// Pin the root, then any file inside the workspace folder resolves to it.
	s.cache.SetPinnedRoot(rootURI)
	assert.Equal(t, rootURI, s.cache.RootFor(libURI))
	assert.Equal(t, rootURI, s.cache.PinnedRoot(libURI))

	// Clear restores baseline behaviour.
	s.cache.ClearPinnedRoot(rootURI)
	assert.Equal(t, libURI, s.cache.RootFor(libURI))
	assert.Empty(t, s.cache.PinnedRoot(libURI))
}
