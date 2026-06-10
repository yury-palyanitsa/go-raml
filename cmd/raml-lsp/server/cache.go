package server

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	raml "github.com/acronis/go-raml/v3"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ParseResult holds everything derived from a single successful or failed parse of a
// root RAML document plus all its transitive library dependencies.
type ParseResult struct {
	RAML           *raml.RAML
	Locs           map[string]struct{}     // all reachable fragment file paths
	PosMaps        map[string]*PositionMap // file path → PositionMap
	RefIndex       *ReferenceIndex
	ContextIndexes map[string]*ContextIndex         // file path → ContextIndex
	Diagnostics    map[string][]protocol.Diagnostic // file URI → diagnostics
	Symbols        []protocol.DocumentSymbol        // pre-built document symbols for the root file
	Lines          docLines                         // pre-parsed lines of the root document text
}

// FileContext is a per-file view of a ParseResult. It bundles the three
// per-file indexes (PositionMap, ContextIndex) together with the shared model
// root (*raml.RAML) and the file path, so handlers and analysis functions do
// not have to perform map lookups or carry (r, ci, filePath) as separate args.
//
// A *FileContext is always non-nil; individual fields (PM, CI, RAML) may be
// nil when the model has not yet been parsed successfully for that file.
type FileContext struct {
	FilePath string
	PM       *PositionMap
	CI       *ContextIndex
	RAML     *raml.RAML
}

// FileContext returns the per-file view for filePath. Safe to call on a nil
// *ParseResult receiver — the returned context will have nil model fields.
// filePath may be an OS path or a file:// URI; it is normalised to a URI for
// the internal map lookups so callers do not have to convert themselves.
func (r *ParseResult) FileContext(filePath string) *FileContext {
	uri := raml.PathToFileURI(filePath) // normalise: OS path → URI (idempotent if already URI)
	fc := &FileContext{FilePath: uri}
	if r == nil {
		return fc
	}
	fc.RAML = r.RAML
	if r.PosMaps != nil {
		fc.PM = r.PosMaps[uri]
	}
	if r.ContextIndexes != nil {
		fc.CI = r.ContextIndexes[uri]
	}
	return fc
}

// ParseCache manages per-document parse results and schedules re-parses with a
// configurable debounce delay. Each call to Enqueue cancels any pending re-parse
// for the same URI and schedules a fresh one after the debounce period.
type ParseCache struct {
	mu      sync.RWMutex
	results map[string]*ParseResult // rootURI → latest ParseResult

	vfs    *VirtualFS
	depg   *DependencyGraph
	notify func(method string, params any) // glsp push notification func

	timersMu sync.Mutex
	timers   map[string]*time.Timer

	inFlightMu sync.Mutex
	inFlight   map[string]chan struct{} // closed when a goroutine-based parse completes

	debounce time.Duration

	// allowRemote enables HTTP/HTTPS resource loading for !include and uses:
	// directives that reference remote URIs. Set via SetAllowRemote.
	allowRemote bool

	// workspaceFolders holds the absolute OS paths of all workspace folders
	// reported by the LSP client in InitializeParams.WorkspaceFolders. Each
	// parse picks the longest-prefix folder that contains the file being
	// parsed and restricts I/O to that folder via OptWithWorkspaceRoot.
	// An empty slice means no restriction is applied.
	workspaceFolders []string

	// pinnedRoots maps workspace-folder absolute path → pinned root file URI.
	// When the LSP client pins a root for a workspace, every change to any
	// file inside that workspace folder triggers a re-parse of the pinned
	// root rather than (or in addition to) parsing the changed file as a
	// standalone fragment. Feature requests (hover, definition, …) made on
	// any file in the workspace consult the pinned root's parse result so
	// the open trait / RT library sees the diagnostics produced by the
	// consuming API document. A workspace folder absent from this map
	// behaves as before — the changed file is its own root.
	pinnedRoots map[string]string
}

func NewParseCache(
	vfs *VirtualFS,
	depg *DependencyGraph,
	debounce time.Duration,
) *ParseCache {
	return &ParseCache{
		results:     make(map[string]*ParseResult),
		vfs:         vfs,
		depg:        depg,
		timers:      make(map[string]*time.Timer),
		inFlight:    make(map[string]chan struct{}),
		debounce:    debounce,
		pinnedRoots: make(map[string]string),
	}
}

// SetPinnedRoot pins a root file for the workspace folder that contains
// rootURI, so that subsequent edits to any file in that folder trigger a
// re-parse of rootURI and feature requests return the pinned root's parse
// result. Returns the previously pinned URI for the same workspace (empty
// when none was set) so the caller can re-publish empty diagnostics for the
// old root if needed. If no workspace folder contains rootURI the URI is
// used as its own key; multiple-folder workspaces remain supported.
func (c *ParseCache) SetPinnedRoot(rootURI string) string {
	rootURI = NormalizeURI(rootURI)
	rootPath := raml.FileURIToPath(rootURI)
	c.mu.Lock()
	folder := workspaceFolderFor(rootPath, c.workspaceFolders)
	if folder == "" {
		folder = filepath.Dir(rootPath)
	}
	prev := c.pinnedRoots[folder]
	c.pinnedRoots[folder] = rootURI
	c.mu.Unlock()
	return prev
}

// ClearPinnedRoot removes the pin for the workspace folder that contains
// uri. Returns the previously pinned URI (empty when none).
func (c *ParseCache) ClearPinnedRoot(uri string) string {
	uri = NormalizeURI(uri)
	path := raml.FileURIToPath(uri)
	c.mu.Lock()
	folder := workspaceFolderFor(path, c.workspaceFolders)
	if folder == "" {
		folder = filepath.Dir(path)
	}
	prev := c.pinnedRoots[folder]
	delete(c.pinnedRoots, folder)
	c.mu.Unlock()
	return prev
}

// PinnedRoot returns the pinned root URI for the workspace folder containing
// uri, or "" when no pin is set. uri must be a file:// URI.
func (c *ParseCache) PinnedRoot(uri string) string {
	path := raml.FileURIToPath(NormalizeURI(uri))
	c.mu.RLock()
	defer c.mu.RUnlock()
	folder := workspaceFolderFor(path, c.workspaceFolders)
	if folder == "" {
		folder = filepath.Dir(path)
	}
	return c.pinnedRoots[folder]
}

// RootFor returns the parse-root URI for a given file URI. If a pinned root
// exists for the workspace folder containing uri, that URI is returned;
// otherwise uri is returned unchanged. Callers route reparses and feature
// lookups through this helper so the same code path serves both modes.
func (c *ParseCache) RootFor(uri string) string {
	if pinned := c.PinnedRoot(uri); pinned != "" {
		return pinned
	}
	return NormalizeURI(uri)
}

// SetNotify stores the function used to push notifications to the LSP client.
// It must be called once the LSP connection is established (e.g. in the initialized handler).
func (c *ParseCache) SetNotify(fn func(method string, params any)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notify = fn
}

// SetAllowRemote enables or disables HTTP/HTTPS resource loading for every
// subsequent parse. When enabled, !include and uses: directives that reference
// http:// or https:// URIs are resolved using http.DefaultClient.
func (c *ParseCache) SetAllowRemote(allow bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allowRemote = allow
}

// AllowRemote reports whether HTTP/HTTPS resource loading is currently enabled.
func (c *ParseCache) AllowRemote() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.allowRemote
}

// SetWorkspaceFolders stores the workspace folder paths (absolute OS paths)
// reported by the LSP client. Called once from the initialize handler. Each
// subsequent parse will restrict file I/O to whichever folder contains the
// file being parsed (longest-prefix match).
func (c *ParseCache) SetWorkspaceFolders(dirs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workspaceFolders = dirs
}

// WorkspaceFolders returns the current workspace folder paths.
func (c *ParseCache) WorkspaceFolders() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.workspaceFolders
}

// workspaceFolderFor returns the workspace folder that contains filePath
// (longest-prefix wins for nested workspaces), or "" if none match.
func workspaceFolderFor(filePath string, folders []string) string {
	best := ""
	for _, folder := range folders {
		rel, err := filepath.Rel(folder, filePath)
		if err != nil {
			continue
		}
		// filepath.Rel succeeds even for paths outside the subtree, but
		// those always start with "..". A clean relative path that does
		// not start with ".." means filePath is inside folder.
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if len(folder) > len(best) {
				best = folder
			}
		}
	}
	return best
}

// Enqueue schedules a re-parse of uri after the debounce period.
// If a parse is already pending for uri it is cancelled and replaced.
func (c *ParseCache) Enqueue(uri string) {
	c.timersMu.Lock()
	defer c.timersMu.Unlock()

	if t, ok := c.timers[uri]; ok {
		t.Stop()
	}
	c.timers[uri] = time.AfterFunc(c.debounce, func() {
		c.timersMu.Lock()
		delete(c.timers, uri)
		c.timersMu.Unlock()

		// Register an in-flight channel so FlushPending can wait for this
		// goroutine even when t.Stop() already returned false (timer had
		// already fired). Without this a feature request arriving just after
		// the timer fires would receive stale cached data.
		ch := make(chan struct{})
		c.inFlightMu.Lock()
		c.inFlight[uri] = ch
		c.inFlightMu.Unlock()

		c.parseAndPublish(uri)

		c.inFlightMu.Lock()
		if c.inFlight[uri] == ch {
			delete(c.inFlight, uri)
		}
		c.inFlightMu.Unlock()
		close(ch)
	})
}

// EnqueueNow parses uri immediately without debouncing (used for didOpen).
// It registers an in-flight channel so that FlushPending can wait for the
// goroutine to complete when a feature request (e.g. documentSymbol) arrives
// before the parse has finished.
func (c *ParseCache) EnqueueNow(uri string) {
	c.timersMu.Lock()
	if t, ok := c.timers[uri]; ok {
		t.Stop()
		delete(c.timers, uri)
	}
	c.timersMu.Unlock()

	ch := make(chan struct{})
	c.inFlightMu.Lock()
	c.inFlight[uri] = ch
	c.inFlightMu.Unlock()

	go func() {
		c.parseAndPublish(uri)
		c.inFlightMu.Lock()
		if c.inFlight[uri] == ch {
			delete(c.inFlight, uri)
		}
		c.inFlightMu.Unlock()
		close(ch)
	}()
}

// Get returns the latest ParseResult for a URI (may be nil).
func (c *ParseCache) Get(uri string) *ParseResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.results[uri]
}

// FlushPending cancels any pending debounce timer for uri and performs a
// synchronous parse before returning the result. This ensures that feature
// requests (hover, documentSymbol, etc.) always see the latest content rather
// than the result of the previous debounce cycle.
//
// If no timer is pending the cached result is returned immediately (fast path).
// If Stop() returns false the timer goroutine is already in flight; in that
// case we skip the redundant parse and return whatever is in the cache — the
// in-flight goroutine will update it momentarily.
func (c *ParseCache) FlushPending(uri string) *ParseResult {
	c.timersMu.Lock()
	t, pending := c.timers[uri]
	stopped := false
	if pending {
		stopped = t.Stop()
		if stopped {
			delete(c.timers, uri)
		}
	}
	c.timersMu.Unlock()

	if pending && stopped {
		c.parseAndPublish(uri)
		return c.Get(uri)
	}

	// Wait for any goroutine-based parse started by EnqueueNow.
	c.inFlightMu.Lock()
	ch := c.inFlight[uri]
	c.inFlightMu.Unlock()
	if ch != nil {
		<-ch
	}

	return c.Get(uri)
}

// Invalidate removes the cached result for uri and clears its diagnostics.
func (c *ParseCache) Invalidate(uri string) {
	c.mu.Lock()
	delete(c.results, uri)
	c.mu.Unlock()
	c.publishDiagnostics(uri, nil)
}

// readContent returns the current text of uri, preferring the in-memory
// DocumentStore (so unsaved edits drive re-parses) and falling back to disk
// via the virtual FS so a pinned root that is not currently open in the
// editor can still be parsed when its dependencies change.
func (c *ParseCache) readContent(uri string) (string, bool) {
	if content, ok := c.vfs.GetContent(uri); ok {
		return content, true
	}
	rc, err := c.vfs.Load(uri)
	if err != nil {
		return "", false
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (c *ParseCache) parseAndPublish(uri string) {
	content, ok := c.readContent(uri)
	if !ok {
		// File neither in the document store nor on disk — clear diagnostics.
		c.Invalidate(uri)
		return
	}

	result := c.parse(uri, content)

	c.mu.Lock()
	prev := c.results[uri]
	c.results[uri] = result
	c.mu.Unlock()

	// Publish diagnostics for the root document and all its library
	// dependencies, tracking which file URIs were published this round so we
	// can clear any file that previously had diagnostics but doesn't anymore
	// (e.g. an error inside a referenced trait fragment that the user just
	// fixed — VS Code keeps stale diagnostics until the server explicitly
	// re-publishes an empty list for that file).
	published := make(map[string]struct{}, len(result.Diagnostics)+1)
	for fileURI, diags := range result.Diagnostics {
		if fileURI == "" {
			fileURI = uri
		}
		c.publishDiagnostics(fileURI, diags)
		published[fileURI] = struct{}{}
	}
	if prev != nil {
		for prevURI := range prev.Diagnostics {
			if prevURI == "" {
				prevURI = uri
			}
			if _, stillHas := published[prevURI]; stillHas {
				continue
			}
			c.publishDiagnostics(prevURI, nil)
			published[prevURI] = struct{}{}
		}
	}
	// Ensure the root URI always gets an entry (clears stale diagnostics on success).
	if _, seen := published[uri]; !seen {
		c.publishDiagnostics(uri, nil)
	}

	// Update dependency graph from the parse result.
	if result.RAML != nil {
		c.updateDeps(uri, result)
	}
}

func (c *ParseCache) parse(uri, content string) *ParseResult {
	filePath := raml.FileURIToPath(uri)
	fileName := filepath.Base(filePath)
	baseDir := filepath.Dir(filePath)

	r := raml.New(context.Background())

	c.mu.RLock()
	folders := c.workspaceFolders
	allowRemote := c.allowRemote
	c.mu.RUnlock()

	root := workspaceFolderFor(filePath, folders)
	parseOpts := []raml.ParseOpt{
		raml.OptWithFileLoader(c.vfs.LoaderForRoot(root)),
		raml.OptWithRawSource(),
	}
	if root != "" {
		parseOpts = append(parseOpts, raml.OptWithWorkspaceRoot(root))
	}
	if allowRemote {
		parseOpts = append(parseOpts, raml.OptWithHTTPClient(raml.NewHTTPClient()))
	}

	// Step 1: Parse without validation options to obtain the original unmodified tree.
	// OptWithRawSource retains the raw *yaml.Node for each fragment (for BuildAllIndexes)
	// and builds the entity-ID → YAML node companion map (for LSP handler navigation).
	parseErr := r.ParseFromString(content, fileName, baseDir, parseOpts...)

	result := &ParseResult{
		RAML:  r,
		Lines: parseDoc(content),
	}

	// Step 2: Build all LSP protocol models on the original unmodified tree.
	// For broken documents these will be sparse/empty but won't cause errors,
	// and partial results (e.g. valid types alongside one bad field) are still useful.
	// Call GetShapes() once and share the slice with the unified index builder.
	shapes := r.GetShapes()
	locs := allFragmentLocations(r.EntryPoint(), shapes)
	// Always include the root file so the YAML-based SourceMap is built even
	// when parseErr caused r.EntryPoint() to be nil (partial/error documents).
	// Use the URI form so locs keys are consistent with fragment locations.
	locs[uri] = struct{}{}
	result.Locs = locs
	result.PosMaps, result.RefIndex, result.ContextIndexes, result.Symbols = BuildAllIndexes(r, shapes, locs)

	// Step 3: Unwrap and validate. Running these after LSP model construction
	// ensures that the cloning and tree rewriting performed by the validation
	// stage does not interfere with position indexing.
	unwrapErr := r.UnwrapShapes()
	AugmentPosMapsPostUnwrap(r, result.PosMaps)
	validateErr := r.ValidateShapes()

	// Merge diagnostics from all three phases into a single map. The
	// include-site lookup lets walkST anchor cross-file orphan diagnostics
	// (e.g. an error inside a security-scheme fragment loaded via !include)
	// at the !include / uses: site in the root document.
	lookup := includeSiteLookupFor(r)
	diags := make(map[string][]protocol.Diagnostic)
	for _, e := range []error{parseErr, unwrapErr, validateErr} {
		for k, v := range ExtractDiagnostics(e, lookup) {
			diags[k] = append(diags[k], v...)
		}
	}
	result.Diagnostics = diags

	return result
}

// updateDeps derives library dependencies directly from the pre-computed
// Locs set in the ParseResult, avoiding a redundant fragment-graph walk.
func (c *ParseCache) updateDeps(rootURI string, result *ParseResult) {
	ep := result.RAML.EntryPoint()
	if ep == nil {
		return
	}
	// ep.GetLocation() returns a file:// URI.
	entryURI := ep.GetLocation()
	uris := make([]string, 0, len(result.Locs))
	for loc := range result.Locs {
		if loc != entryURI {
			uris = append(uris, loc)
		}
	}
	c.depg.SetDeps(rootURI, uris)
}

func (c *ParseCache) publishDiagnostics(uri string, diags []protocol.Diagnostic) {
	c.mu.RLock()
	fn := c.notify
	c.mu.RUnlock()
	if fn == nil {
		return
	}
	if diags == nil {
		diags = []protocol.Diagnostic{}
	}
	fn(string(protocol.ServerTextDocumentPublishDiagnostics), &protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}
