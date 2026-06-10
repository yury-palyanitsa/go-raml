package raml

import "gopkg.in/yaml.v3"

// NodePair holds the key and value *yaml.Node for a single model entity as
// written in the source document. Both nodes are direct references into the
// parsed *yaml.Node tree; callers must not mutate them.
//
// Key is the YAML node for the entity's name token — the URI path for an
// EndPoint, the HTTP method for an Operation, the status-code scalar for a
// Response, the type name for a BaseShape, etc.
// For entities whose constructor does not receive a distinct key node (e.g.
// a parametrised operation produced during trait expansion) Key is nil.
//
// Value is the YAML mapping or scalar node that describes the entity's body.
type NodePair struct {
	Key   *yaml.Node
	Value *yaml.Node
}

// SourceInfo is an optional companion to a parsed RAML instance that maps
// every model entity's stable ID to the *yaml.Node pair from which it was
// decoded. It is populated during parsing when OptWithSourceInfo is passed,
// and is absent (nil) otherwise, so non-tooling consumers pay no overhead.
//
// All entity IDs are generated from a single monotonically increasing counter
// on the RAML instance, so they are globally unique within one parse and can
// be stored in a single flat map.
//
// Entities synthesised programmatically (MakeBaseShape, MakeNewShape) or
// produced by template expansion (makeParametrised*) have no entry in the
// map; callers should treat a missing entry the same as a nil NodePair.
type SourceInfo struct {
	nodes map[int64]*NodePair
}

// Get returns the NodePair for the entity with the given ID, or nil if the
// entity was not decoded from a source document (programmatic or synthetic).
// Safe to call on a nil *SourceInfo.
func (s *SourceInfo) Get(id int64) *NodePair {
	if s == nil {
		return nil
	}
	return s.nodes[id]
}

// store records the key/value node pair for entity id.
// No-op when s is nil.
func (s *SourceInfo) store(id int64, key, value *yaml.Node) {
	if s == nil {
		return
	}
	if s.nodes == nil {
		s.nodes = make(map[int64]*NodePair)
	}
	s.nodes[id] = &NodePair{Key: key, Value: value}
}
