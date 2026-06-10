package server

import "sync"

// DocumentStore is a thread-safe in-memory store for currently open document contents.
// A document is "open" while the editor has it open (didOpen → didClose lifecycle).
type DocumentStore struct {
	mu   sync.RWMutex
	docs map[string]string // URI → full text content
}

func NewDocumentStore() *DocumentStore {
	return &DocumentStore{docs: make(map[string]string)}
}

func (d *DocumentStore) Set(uri, content string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.docs[uri] = content
}

func (d *DocumentStore) Get(uri string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.docs[uri]
	return v, ok
}

func (d *DocumentStore) Delete(uri string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.docs, uri)
}
