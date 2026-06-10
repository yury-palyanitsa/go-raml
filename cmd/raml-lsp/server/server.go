package server

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	raml "github.com/acronis/go-raml/v3"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

const serverName = "raml-lsp"
const serverVersion = "0.1.0"

// Options configures the RAML language server.
type Options struct {
	// AllowRemote enables HTTP/HTTPS resource loading for !include and uses:
	// directives that reference remote URIs. When false (default) only
	// file:// URIs are resolved.
	AllowRemote bool
}

// Server is the RAML language server. It wires the glsp protocol handler to the
// parse cache, document store, virtual FS, and dependency graph.
type Server struct {
	mu    sync.RWMutex
	store *DocumentStore
	depg  *DependencyGraph
	cache *ParseCache

	glspServer *glspserver.Server
}

// New creates a Server with a 300 ms debounce on re-parses.
func New(opts Options) *Server {
	store := NewDocumentStore()
	vfs := NewVirtualFS(store)
	depg := NewDependencyGraph()
	cache := NewParseCache(vfs, depg, 300*time.Millisecond)
	if opts.AllowRemote {
		cache.SetAllowRemote(true)
	}

	s := &Server{
		store: store,
		depg:  depg,
		cache: cache,
	}

	handler := protocol.Handler{
		Initialize:  s.initialize,
		Initialized: s.initialized,
		Shutdown:    s.shutdown,

		TextDocumentDidOpen:   s.didOpen,
		TextDocumentDidChange: s.didChange,
		TextDocumentDidClose:  s.didClose,

		TextDocumentHover:          s.hover,
		TextDocumentDefinition:     s.definition,
		TextDocumentReferences:     s.references,
		TextDocumentDocumentSymbol: s.documentSymbol,
		TextDocumentCompletion:     s.completion,
		TextDocumentDocumentLink:   s.documentLink,

		CustomRequest: s.customRequestHandlers(),
	}

	s.glspServer = glspserver.NewServer(&handler, serverName, false)
	return s
}

// RunStdio starts the language server on stdin/stdout.
func (s *Server) RunStdio() error {
	return s.glspServer.RunStdio()
}

// ---- Lifecycle ----

func (s *Server) initialize(ctx *glsp.Context, params *protocol.InitializeParams) (any, error) {
	syncKind := protocol.TextDocumentSyncKindFull

	// Convert each workspace folder URI to an absolute OS path and register
	// them with the parse cache. The cache will restrict each parse to the
	// specific folder that contains the file being parsed (longest-prefix
	// match), preventing cross-workspace file leaks without any fallback
	// heuristics.
	if len(params.WorkspaceFolders) > 0 {
		dirs := make([]string, 0, len(params.WorkspaceFolders))
		for _, wf := range params.WorkspaceFolders {
			dirs = append(dirs, raml.FileURIToPath(string(wf.URI)))
		}
		s.cache.SetWorkspaceFolders(dirs)
	}

	return protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				OpenClose: ptrTo(true),
				Change:    &syncKind,
			},
			HoverProvider:          ptrTo(true),
			DefinitionProvider:     &protocol.DefinitionOptions{},
			ReferencesProvider:     &protocol.ReferenceOptions{},
			DocumentSymbolProvider: ptrTo(true),
			DocumentLinkProvider:   &protocol.DocumentLinkOptions{},
			CompletionProvider: &protocol.CompletionOptions{
				// Trigger on `.` for library-qualified completions (`lib.`),
				// `(` for annotation types, and `/` for !include paths.
				TriggerCharacters: []string{".", "(", "/"},
			},
		},
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    serverName,
			Version: ptrTo(serverVersion),
		},
	}, nil
}

func (s *Server) initialized(ctx *glsp.Context, _ *protocol.InitializedParams) error {
	// Capture the notify function so the parse cache can push diagnostics asynchronously.
	s.cache.SetNotify(ctx.Notify)
	return nil
}

func (s *Server) shutdown(ctx *glsp.Context) error {
	return nil
}

// ---- Document Synchronization ----

func (s *Server) didOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	uri := NormalizeURI(string(params.TextDocument.URI))
	s.store.Set(uri, params.TextDocument.Text)
	// When a root is pinned for this workspace, parse the pinned root so the
	// open file inherits the root's context (the consuming API document) and
	// gets diagnostics produced by that context instead of the standalone
	// fragment parse it would receive on its own.
	s.cache.EnqueueNow(s.cache.RootFor(uri))
	return nil
}

func (s *Server) didChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) == 0 {
		return nil
	}
	// We advertised Full sync, so each change carries the complete new text.
	last := params.ContentChanges[len(params.ContentChanges)-1]
	var text string
	switch v := last.(type) {
	case protocol.TextDocumentContentChangeEventWhole:
		text = v.Text
	case protocol.TextDocumentContentChangeEvent:
		text = v.Text
	}
	uri := NormalizeURI(string(params.TextDocument.URI))
	s.store.Set(uri, text)
	s.cache.Enqueue(s.cache.RootFor(uri))

	// Also re-parse documents that depend on this file (e.g. it may be a
	// library used by another root). When the pinned root is the dependent,
	// this is a no-op because Enqueue dedupes by URI through the timer table.
	for _, dep := range s.depg.Dependents(uri) {
		s.cache.Enqueue(dep)
	}
	return nil
}

func (s *Server) didClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := NormalizeURI(string(params.TextDocument.URI))
	s.store.Delete(uri)
	// Closing a file that is the pinned root must not drop its cached result:
	// other open files in the workspace still resolve through the pin and
	// rely on its parse output. The disk-backed readContent fallback keeps
	// future re-parses working.
	if s.cache.PinnedRoot(uri) == uri {
		return nil
	}
	s.cache.Invalidate(uri)
	return nil
}

// ---- Language Features ----

func (s *Server) definition(ctx *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	uri := NormalizeURI(string(params.TextDocument.URI))
	result := s.cache.FlushPending(s.cache.RootFor(uri))
	if result == nil {
		return nil, nil
	}
	fc := result.FileContext(uri)
	if fc.PM == nil {
		return nil, nil
	}
	entry, ok := fc.PM.HitTest(params.Position.Line, params.Position.Character)
	if !ok {
		return nil, nil
	}
	link := DefinitionLocationLink(entry)
	if link == nil {
		return nil, nil
	}
	return []protocol.LocationLink{*link}, nil
}

func (s *Server) references(ctx *glsp.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	uri := NormalizeURI(string(params.TextDocument.URI))
	result := s.cache.FlushPending(s.cache.RootFor(uri))
	if result == nil || result.RefIndex == nil {
		return nil, nil
	}
	fc := result.FileContext(uri)
	if fc.PM == nil {
		return nil, nil
	}
	entry, ok := fc.PM.HitTest(params.Position.Line, params.Position.Character)
	if !ok {
		return nil, nil
	}
	id, ok := entry.EntityID()
	if !ok {
		return nil, nil
	}
	locs := result.RefIndex.Find(id)
	if len(locs) == 0 {
		return nil, nil
	}
	return locs, nil
}

func (s *Server) documentSymbol(ctx *glsp.Context, params *protocol.DocumentSymbolParams) (any, error) {
	uri := NormalizeURI(string(params.TextDocument.URI))
	// Symbols are root-scoped: a pinned root supplies symbols only for the
	// root file. A non-root document being queried under a pin therefore
	// returns nil (its own symbols would require its own standalone parse,
	// which we deliberately skip while a pin is active).
	root := s.cache.RootFor(uri)
	if root != uri {
		return nil, nil
	}
	result := s.cache.FlushPending(root)
	if result == nil || len(result.Symbols) == 0 {
		return nil, nil
	}
	return result.Symbols, nil
}

func (s *Server) completion(ctx *glsp.Context, params *protocol.CompletionParams) (any, error) {
	uri := NormalizeURI(string(params.TextDocument.URI))

	text, ok := s.store.Get(uri)
	if !ok {
		return nil, nil
	}

	// Flush the parse cache so the context index is available for
	// model-aware context detection below.
	result := s.cache.FlushPending(s.cache.RootFor(uri))
	var lines docLines
	if result != nil {
		lines = result.Lines
	} else {
		lines = parseDoc(text)
	}
	lineText := getLineUpToCursor(lines, params.Position)

	// Suppress completions inside YAML comments.
	if strings.HasPrefix(strings.TrimLeft(lineText, " \t"), "#") {
		return nil, nil
	}

	lspLine := params.Position.Line
	fc := result.FileContext(uri)

	cp := analyzePosition(lineText, int(lspLine), lines, fc)

	switch cp.Role {
	case RoleUsesPath:
		return includePathCompletions(filepath.Dir(raml.FileURIToPath(fc.FilePath)), cp.PartialPath, params.Position, cp.PathStart, ".raml"), nil
	case RoleIncludePath:
		return includePathCompletions(filepath.Dir(raml.FileURIToPath(fc.FilePath)), cp.PartialPath, params.Position, cp.PathStart), nil
	case RoleAnnotationName:
		if fc.RAML == nil {
			return nil, nil
		}
		return annotationKeyCompletions(fc.RAML, fc.FilePath), nil
	case RoleLibType:
		if fc.RAML == nil {
			return nil, nil
		}
		return libTypeCompletions(fc.RAML, fc.FilePath, cp.LibAlias), nil
	case RoleTypeExpr:
		if fc.RAML == nil {
			return nil, nil
		}
		return typeExprCompletions(fc.RAML, fc.FilePath), nil
	case RoleScalarValue:
		return scalarValueCompletions(cp, fc.RAML, fc.FilePath, lines), nil
	case RoleStatusCode:
		return statusCodeCompletions(), nil
	case RoleTypedObjectKey:
		if cp.TypedShape == nil {
			return nil, nil
		}
		return typedObjectKeyCompletions(cp.TypedShape, existingKeysAt(lines, int(lspLine))), nil
	case RoleKey:
		existing := existingKeysAt(lines, int(lspLine))
		items := keyCompletions(cp.Entry, cp.FragKind, lspLine, lines)
		if fc.RAML != nil {
			items = append(items, customFacetKeysForEntry(cp.Entry, fc.RAML, fc.FilePath, existing)...)
			items = append(items, annotationKeyCompletions(fc.RAML, fc.FilePath)...)
		}
		return items, nil
	}
	return nil, nil
}

func (s *Server) documentLink(_ *glsp.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	uri := NormalizeURI(string(params.TextDocument.URI))
	result := s.cache.FlushPending(s.cache.RootFor(uri))
	if result == nil || result.RAML == nil {
		return nil, nil
	}
	links := BuildDocumentLinks(result.RAML, uri)
	if len(links) == 0 {
		return nil, nil
	}
	return links, nil
}
