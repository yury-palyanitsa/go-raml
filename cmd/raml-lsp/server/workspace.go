package server

import (
	"encoding/json"
	"fmt"

	"github.com/tliron/glsp"
)

// Custom LSP request names for managing the workspace's pinned root API
// file. The pinned root is the document the LSP treats as the parse root
// when re-parsing on changes to any file inside the workspace folder that
// contains the root. See ParseCache.SetPinnedRoot for the semantics.
const (
	setRootMethod   = "ramlLsp/setRoot"
	getRootMethod   = "ramlLsp/getRoot"
	clearRootMethod = "ramlLsp/clearRoot"
)

// rootPayload is the shared request shape for set/clear: the workspace and
// the root document URI. For getRoot, only Workspace is consulted.
type rootPayload struct {
	// Workspace is the file:// URI of the workspace folder whose pin is
	// being read or written. May be empty when the client cannot determine
	// the folder; the server then keys the pin off the file's parent
	// directory (single-folder workspace fallback).
	Workspace string `json:"workspace,omitempty"`
	// URI is the file:// URI of the RAML document to pin as root. Empty
	// when clearing.
	URI string `json:"uri,omitempty"`
}

// rootResponse echoes back the workspace and the currently pinned URI for
// it (empty when no pin is set).
type rootResponse struct {
	Workspace string `json:"workspace,omitempty"`
	URI       string `json:"uri,omitempty"`
}

// handleSetRoot pins payload.URI as the parse root for the workspace folder
// containing it, kicks off an immediate parse of the new root, and clears
// the cached standalone result of the previously pinned root (if any) so
// stale diagnostics are not retained.
func (s *Server) handleSetRoot(_ *glsp.Context, raw json.RawMessage) (any, error) {
	var payload rootPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("setRoot: invalid payload: %w", err)
	}
	if payload.URI == "" {
		return nil, fmt.Errorf("setRoot: uri is required")
	}
	uri := NormalizeURI(payload.URI)
	prev := s.cache.SetPinnedRoot(uri)
	if prev != "" && prev != uri {
		// Drop the old root's cached result so its diagnostics are
		// republished as empty and feature lookups stop returning stale data.
		s.cache.Invalidate(prev)
	}
	// Eagerly parse the new root so diagnostics appear immediately without
	// waiting for the next edit. EnqueueNow falls back to disk when the
	// root is not open in the editor (see ParseCache.readContent).
	s.cache.EnqueueNow(uri)
	return rootResponse{Workspace: payload.Workspace, URI: uri}, nil
}

// handleGetRoot returns the currently pinned root URI for the workspace
// folder containing payload.Workspace (or payload.URI as a fallback).
func (s *Server) handleGetRoot(_ *glsp.Context, raw json.RawMessage) (any, error) {
	var payload rootPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("getRoot: invalid payload: %w", err)
	}
	probe := payload.Workspace
	if probe == "" {
		probe = payload.URI
	}
	if probe == "" {
		return rootResponse{}, nil
	}
	return rootResponse{Workspace: payload.Workspace, URI: s.cache.PinnedRoot(probe)}, nil
}

// handleClearRoot removes the pin for the workspace folder containing
// payload.Workspace (or payload.URI). The previously pinned root's cached
// result is dropped so stale diagnostics do not linger.
func (s *Server) handleClearRoot(_ *glsp.Context, raw json.RawMessage) (any, error) {
	var payload rootPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("clearRoot: invalid payload: %w", err)
	}
	probe := payload.Workspace
	if probe == "" {
		probe = payload.URI
	}
	if probe == "" {
		return rootResponse{}, nil
	}
	prev := s.cache.ClearPinnedRoot(probe)
	if prev != "" {
		s.cache.Invalidate(prev)
	}
	return rootResponse{Workspace: payload.Workspace}, nil
}
