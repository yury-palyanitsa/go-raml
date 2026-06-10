package server

// testhelpers_test.go — shared test infrastructure for all LSP server tests.
//
// All helpers and RAML fixtures used by more than one _test.go file live here.
// File-specific helpers (posmap lookups, document-symbol helpers, etc.) stay in
// their own test files.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	raml "github.com/acronis/go-raml/v3"
	"github.com/stretchr/testify/require"
	glsp "github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ---- RAML fixtures --------------------------------------------------------
//
// These constants are intentionally package-level so every _test.go file in
// this package can reference them without re-declaring them.

const ramlValid = `#%RAML 1.0
title: Test API
types:
  Person:
    type: object
    description: A person entity
    properties:
      name: string
      age: integer
  Employee:
    type: Person
    properties:
      company: string
  PersonRef: Person
`

const ramlInvalidType = `#%RAML 1.0
title: Error API
types:
  BadType:
    type: DoesNotExist
`

const ramlArrayNotation = `#%RAML 1.0
title: Array Notation Test
types:
  Test:
    type: object
    properties:
      id: string
  TestList:
    type: Test[]
`

const ramlLibrary = `#%RAML 1.0 Library
types:
  Base:
    type: object
    properties:
      id: integer
`

const ramlUsesLibrary = `#%RAML 1.0
title: Library Test
uses:
  lib: library.raml
types:
  Entity:
    type: lib.Base
    properties:
      name: string
`

const ramlCustomFacets = `#%RAML 1.0
title: Custom Facets Test
types:
  Extensible:
    facets:
      myFacet:
        description: A custom constraint
        type: string
  MyShape:
    type: Extensible
    myFacet: "some value"
`

const ramlCDPValueKeys = `#%RAML 1.0
title: CDP Value Keys Test
annotationTypes:
  deprecated:
    properties:
      since:
        description: Version since which it is deprecated
        type: string
      reason:
        description: Reason for deprecation
        type: string
types:
  MyType:
    type: object
    (deprecated):
      since: "2.0"
      reason: "old design"
`

const ramlObjectFacetValueKeys = `#%RAML 1.0
title: Object Facets Test
types:
  Extensible:
    facets:
      metadata:
        type: object
        properties:
          author:
            description: Author of the metadata
            type: string
  MyShape:
    type: Extensible
    metadata:
      author: "John"
`

const ramlCDPArrayValueKeys = `#%RAML 1.0
title: CDP Array Value Keys Test
annotationTypes:
  roles:
    type: array
    items:
      type: object
      properties:
        name:
          description: Role name
          type: string
        id:
          description: Role identifier
          type: string
types:
  MyType:
    type: object
    (roles):
    - name: public
      id: "1"
`

const ramlArrayFacetValueKeys = `#%RAML 1.0
title: Array Facets Test
types:
  Extensible:
    facets:
      tags:
        type: array
        items:
          type: object
          properties:
            name:
              description: Tag name
              type: string
  MyShape:
    type: Extensible
    tags:
    - name: "alpha"
`

// ---- Server / store helpers -----------------------------------------------

// newTestServer creates a Server with zero debounce for immediate parsing in tests.
// The glspServer field is intentionally left nil — it is only needed for RunStdio.
func newTestServer() *Server {
	store := NewDocumentStore()
	vfs := NewVirtualFS(store)
	depg := NewDependencyGraph()
	cache := NewParseCache(vfs, depg, 0)
	return &Server{
		store: store,
		depg:  depg,
		cache: cache,
	}
}

// notifyCapture records publishDiagnostics notifications for later assertions.
type notifyCapture struct {
	mu       sync.Mutex
	diagsFor map[string][]protocol.Diagnostic // uri → latest diagnostics
	received map[string]struct{}              // uri → ever received
}

func (n *notifyCapture) fn(method string, params any) {
	if method != string(protocol.ServerTextDocumentPublishDiagnostics) {
		return
	}
	p := params.(*protocol.PublishDiagnosticsParams)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.diagsFor == nil {
		n.diagsFor = make(map[string][]protocol.Diagnostic)
		n.received = make(map[string]struct{})
	}
	n.diagsFor[p.URI] = p.Diagnostics
	n.received[p.URI] = struct{}{}
}

// waitDiags blocks until a publishDiagnostics notification has been received for uri.
func (n *notifyCapture) waitDiags(t *testing.T, uri string) []protocol.Diagnostic {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		_, recv := n.received[uri]
		ok := n.received != nil && recv
		diags := n.diagsFor[uri]
		n.mu.Unlock()
		if ok {
			return diags
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for diagnostics notification for %s", uri)
	return nil
}

// reset clears all captured notifications (use between test phases).
func (n *notifyCapture) reset() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.diagsFor = nil
	n.received = nil
}

// fakeCtx builds a glsp.Context that routes notifications to nc.
func fakeCtx(nc *notifyCapture) *glsp.Context {
	return &glsp.Context{
		Context: context.Background(),
		Notify:  nc.fn,
	}
}

// openDoc registers content in the server's DocumentStore and calls didOpen.
// No file is written to disk: the parser reads the content via VirtualFS which
// checks the DocumentStore before falling back to the real filesystem.
// dir is used only to derive a stable base URI; it should be t.TempDir().
//
// initialized is called before didOpen to mirror the real LSP lifecycle. This
// ensures SetNotify is wired up so that diagnostic push notifications are
// delivered before the caller calls waitDiags.
func openDoc(t *testing.T, s *Server, nc *notifyCapture, dir, filename, content string) string {
	t.Helper()
	uri := raml.PathToFileURI(filepath.Join(dir, filename))
	ctx := fakeCtx(nc)
	_ = s.initialized(ctx, &protocol.InitializedParams{})
	require.NoError(t, s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        protocol.DocumentUri(uri),
			LanguageID: "raml",
			Text:       content,
		},
	}))
	return uri
}

// waitParse blocks until ParseCache has a non-nil ParseResult for uri.
func waitParse(t *testing.T, cache *ParseCache, uri string) *ParseResult {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r := cache.Get(uri); r != nil {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for parse of %s", uri)
	return nil
}

// ---- Completion helpers ---------------------------------------------------

// itemLabels returns the Label field of each CompletionItem.
func itemLabels(items []protocol.CompletionItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

// ---- In-memory filesystem -------------------------------------------------

// testFS is a minimal in-memory raml.ResourceLoader for tests.
// Keys are absolute OS paths; values are file contents.
// Pass it to parseRAMLForTest (or raml.OptWithFileLoader directly) to supply secondary
// files — includes, library fragments — without writing anything to disk.
type testFS map[string]string

func (m testFS) Load(uri string) (io.ReadCloser, error) {
	path := raml.FileURIToPath(uri)
	if content, ok := m[path]; ok {
		return io.NopCloser(strings.NewReader(content)), nil
	}
	return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
}

// parseRAMLForTest parses RAML content in memory and returns the RAML instance.
// Pass an optional testFS to supply extra files (includes, library fragments)
// without writing them to disk.  Parse errors are ignored so callers get a
// partial model even for documents with semantic errors.
func parseRAMLForTest(t *testing.T, dir, filename, content string, extra ...testFS) *raml.RAML {
	t.Helper()
	r := raml.New(context.Background())
	var opts []raml.ParseOpt
	if len(extra) > 0 {
		opts = append(opts, raml.OptWithFileLoader(extra[0]))
	}
	err := r.ParseFromString(content, filename, dir, opts...)
	_ = err
	return r
}
