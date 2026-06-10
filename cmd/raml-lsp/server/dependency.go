package server

import "sync"

// DependencyGraph tracks which fragment files (libraries, includes) each root document
// depends on. When a dependency changes, all dependents must be re-parsed.
type DependencyGraph struct {
	mu sync.RWMutex
	// deps[depURI] = set of root document URIs that depend on depURI
	deps map[string]map[string]struct{}
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{deps: make(map[string]map[string]struct{})}
}

// SetDeps records that rootURI depends on exactly the given set of library URIs.
// Previous entries for rootURI are replaced atomically.
func (g *DependencyGraph) SetDeps(rootURI string, libraryURIs []string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Remove rootURI from every existing dependency set.
	for _, set := range g.deps {
		delete(set, rootURI)
	}
	// Add rootURI to each new library's dependency set.
	for _, lib := range libraryURIs {
		if g.deps[lib] == nil {
			g.deps[lib] = make(map[string]struct{})
		}
		g.deps[lib][rootURI] = struct{}{}
	}
}

// Dependents returns all root document URIs that directly depend on fileURI.
// Transitively-dependent documents are handled incrementally: when a library
// is re-parsed and its dependents map updated, their own dependents are
// re-queued on subsequent change events.
func (g *DependencyGraph) Dependents(fileURI string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	set := g.deps[fileURI]
	result := make([]string, 0, len(set))
	for uri := range set {
		result = append(result, uri)
	}
	return result
}
