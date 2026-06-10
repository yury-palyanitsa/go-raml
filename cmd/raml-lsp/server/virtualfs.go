package server

import (
	"io"
	"strings"

	raml "github.com/acronis/go-raml/v3"
)

// VirtualFS overlays in-memory document content (from the DocumentStore) on
// top of disk reads. When a file is open in the editor, its in-memory
// content shadows the on-disk version so that unsaved edits are visible to
// dependent RAML files during parsing.
//
// Its Load method uses a bare OS file loader for disk reads. For parses
// driven by a workspace root, construct a per-parse loader via
// LoaderForRoot, which falls back through SafeOSFileLoader and so refuses
// path-traversal or symlink-escape from the workspace.
type VirtualFS struct {
	store *DocumentStore
}

func NewVirtualFS(store *DocumentStore) *VirtualFS {
	return &VirtualFS{store: store}
}

// Load satisfies raml.ResourceLoader. uri is always an absolute file:// URI.
// In-memory content for open documents shadows the on-disk file; disk reads
// use a bare OSFileLoader. Prefer LoaderForRoot for parses that have a
// workspace root in scope.
func (v *VirtualFS) Load(uri string) (io.ReadCloser, error) {
	if content, ok := v.store.Get(uri); ok {
		return io.NopCloser(strings.NewReader(content)), nil
	}
	return raml.OSFileLoader{}.Load(uri)
}

// GetContent returns the in-memory text for a document URI and whether it is
// currently open in the editor. It does not fall back to disk — use Load
// for filesystem access.
func (v *VirtualFS) GetContent(uri string) (string, bool) {
	return v.store.Get(uri)
}

// LoaderForRoot returns a per-parse ResourceLoader that overlays the
// in-memory store on disk reads. When root is non-empty, disk reads go
// through SafeOSFileLoader bound to root so symlink-based escapes are
// refused; when empty, a bare OSFileLoader is used.
func (v *VirtualFS) LoaderForRoot(root string) raml.ResourceLoader {
	var disk raml.ResourceLoader
	if root != "" {
		disk = raml.SafeOSFileLoader{Root: root}
	} else {
		disk = raml.OSFileLoader{}
	}
	return &vfsLoader{store: v.store, disk: disk}
}

type vfsLoader struct {
	store *DocumentStore
	disk  raml.ResourceLoader
}

func (l *vfsLoader) Load(uri string) (io.ReadCloser, error) {
	if content, ok := l.store.Get(uri); ok {
		return io.NopCloser(strings.NewReader(content)), nil
	}
	return l.disk.Load(uri)
}
