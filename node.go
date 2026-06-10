package raml

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/acronis/go-stacktrace"
	"github.com/santhosh-tekuri/jsonschema/v6"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

type Nodes []*DataNode

func (n Nodes) String() string {
	vals := make([]string, len(n))
	for i, node := range n {
		vals[i] = node.String()
	}
	return strings.Join(vals, ", ")
}

// IncludeInfo records the !include directive that provided a value.
type IncludeInfo struct {
	// Path is the literal include argument as written (e.g. "schemas/order.json").
	Path string
	// AbsURI is the resolved absolute URI of the included resource (file:// or https://).
	AbsURI string
}

// IncludeRef records a single !include directive encountered during parsing.
type IncludeRef struct {
	// SourcePath is the absolute URI of the file that contains the !include directive.
	SourcePath string
	// Path is the literal argument as written (e.g. "schemas/order.json").
	Path string
	// AbsPath is the resolved absolute URI of the included resource.
	AbsPath string
	// Pos is the 1-based source position of the !include token.
	Pos stacktrace.Position
}

// Node[T] is a parsed document value with its source positions and optional
// !include provenance. Use DataNode (= Node[any]) for untyped structured values.
type Node[T any] struct {
	Value T

	// Include is non-nil when this node was set via a !include directive.
	Include *IncludeInfo

	Location string

	// KeyPos is the 1-based position of the YAML key node that introduced this
	// value (e.g. the "minimum:" key). Zero for sequence items where there is no key.
	KeyPos stacktrace.Position
	// ValuePos is the 1-based position of the YAML value token.
	// EndLine is set to the last line of the scalar/mapping/sequence block.
	ValuePos stacktrace.Position
}

// ScalarNode[T] is a parsed document value with its source positions and optional
// !include provenance. Use AnyScalarNode (= ScalarNode[any]) for untyped structured values.
type ScalarFacet[T any] struct {
	Value T

	// Include is non-nil when this node was set via a !include directive.
	Include *IncludeInfo

	Location string

	// KeyPos is the 1-based position of the YAML key node that introduced this
	// value (e.g. the "minimum:" key). Zero for sequence items where there is no key.
	KeyPos stacktrace.Position
	// ValuePos is the 1-based position of the YAML value token.
	// EndLine is set to the last line of the scalar/mapping/sequence block.
	ValuePos stacktrace.Position

	// CustomDomainExtensions holds annotations that were present in the RAML 1.0
	// annotated-scalar form alongside the scalar value, e.g.:
	//   description:
	//     value: "some text"
	//     (myAnnotation): annotationValue
	CustomDomainExtensions *orderedmap.OrderedMap[string, *DomainExtension]
}

// DataNode holds an arbitrary structured RAML value (custom facet value, annotation
// value, example data, default, discriminator value, enum member) together with its
// source positions at every level of nesting.
//
// Unlike the generic Node[T], DataNode stores a *ValueNode rather than a plain any,
// so that mapping / sequence structures preserve per-key / per-item positions that
// enable LSP hover and go-to-definition at any depth.
type DataNode struct {
	Value *ValueNode

	// Include is non-nil when this node was set via a !include directive.
	Include *IncludeInfo

	Location string

	// KeyPos is the 1-based position of the YAML key node that introduced this value.
	KeyPos stacktrace.Position
	// ValuePos is the 1-based position of the YAML value token.
	ValuePos stacktrace.Position
}

// String returns a human-readable representation of the node's value.
func (n *DataNode) String() string {
	if n == nil || n.Value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", n.Value.Raw)
}

// ValueNode is the structured representation of an arbitrary RAML value.
// Exactly one of Scalar, Mapping, or Sequence is non-nil.
// Raw holds the plain Go representation (computed once during construction)
// for use with validation and JSON serialization.
type ValueNode struct {
	Scalar   *ScalarValue
	Mapping  *MappingValue
	Sequence *SequenceValue
	Raw      any
}

// ScalarValue holds a single decoded scalar coming from YAML or JSON.
type ScalarValue struct {
	Value any
}

// MappingValue holds an ordered list of key/value entries from a YAML mapping,
// preserving the source position of each key for LSP hover/go-to-definition.
type MappingValue struct {
	Entries []MappingEntry
}

// MappingEntry is one key/value pair within a MappingValue.
type MappingEntry struct {
	Key      string
	KeyPos   stacktrace.Position
	Value    *ValueNode
	ValuePos stacktrace.Position
}

// SequenceValue holds an ordered list of items from a YAML sequence.
type SequenceValue struct {
	Items []SequenceItem
}

// SequenceItem is one element within a SequenceValue.
type SequenceItem struct {
	Value    *ValueNode
	ValuePos stacktrace.Position
}

// NewScalarNodeValue creates a *ValueNode holding a single scalar.
// Useful for constructing DataNode values programmatically and in tests.
func NewScalarNodeValue(v any) *ValueNode {
	return &ValueNode{Scalar: &ScalarValue{Value: v}, Raw: v}
}

// anyToNodeValue wraps a plain Go value (such as one decoded from inline JSON)
// into a *ValueNode. Position information is unavailable for inline JSON values.
func anyToNodeValue(v any) *ValueNode {
	switch val := v.(type) {
	case map[string]any:
		entries := make([]MappingEntry, 0, len(val))
		for k, child := range val {
			entries = append(entries, MappingEntry{Key: k, Value: anyToNodeValue(child)})
		}
		return &ValueNode{Mapping: &MappingValue{Entries: entries}, Raw: val}
	case []any:
		items := make([]SequenceItem, len(val))
		for i, child := range val {
			items[i] = SequenceItem{Value: anyToNodeValue(child)}
		}
		return &ValueNode{Sequence: &SequenceValue{Items: items}, Raw: val}
	default:
		return &ValueNode{Scalar: &ScalarValue{Value: v}, Raw: val}
	}
}

func ScalarFacetValPtr[T any](n *ScalarFacet[T]) *T {
	if n == nil {
		return nil
	}
	v := n.Value
	return &v
}

// nodeStringValues extracts the string values from a slice of *Node[string].
// Used when a []string is needed from parsed node data (e.g. globalProtocols).
func nodeStringValues(nodes []*Node[string]) []string {
	vals := make([]string, len(nodes))
	for i, n := range nodes {
		vals[i] = n.Value
	}
	return vals
}

func (n *ScalarFacet[T]) String() string {
	return fmt.Sprintf("%v", n.Value)
}

func (n *Node[T]) String() string {
	return fmt.Sprintf("%v", n.Value)
}

// resolveAnnotatedScalar unwraps the RAML 1.0 annotated scalar form and
// processes any annotation keys found alongside the value.
//
// When node is a MappingNode that contains a "value" key alongside annotation
// keys (parenthesised names), this method:
//  1. Returns the inner "value" node so that normal scalar decoding works unchanged.
//  2. Parses each (annotation) key via unmarshalCustomDomainExtension, registering
//     the extensions in r.domainExtensions and returning them for the caller to
//     attach to the Node[T] being constructed.
//
// Only call this for nodes that are declared as scalar-valued facets in the
// RAML 1.0 spec (e.g. displayName, description, type, minLength, baseUri…).
// Non-mapping nodes and mapping nodes without a "value" key are invalid at
// those positions and return an error.
func (r *RAML) resolveAnnotatedScalar(node *yaml.Node, location string) (*yaml.Node, *orderedmap.OrderedMap[string, *DomainExtension], error) {
	switch node.Kind {
	case yaml.ScalarNode:
		return node, nil, nil
	case yaml.MappingNode:
		var valueNode *yaml.Node
		var exts *orderedmap.OrderedMap[string, *DomainExtension]
		for i := 0; i != len(node.Content); i += 2 {
			k := node.Content[i]
			v := node.Content[i+1]
			if k.Value == FacetValue {
				valueNode = v
			} else if IsCustomDomainExtensionNode(k.Value) {
				de, err := r.unmarshalCustomDomainExtension(location, k, v)
				if err != nil {
					return nil, nil, StacktraceNewWrapped("annotated scalar extension", err, location,
						WithNodePosition(k))
				}
				if exts == nil {
					exts = orderedmap.New[string, *DomainExtension](0)
				}
				exts.Set(de.Name, de)
			} else {
				return nil, nil, StacktraceNew("unknown field in annotated scalar", location, WithNodePosition(k), stacktrace.WithInfo("field", k.Value))
			}
		}
		if valueNode == nil {
			return nil, nil, StacktraceNew("missing value key in annotated scalar", location, WithNodePosition(node))
		}
		return valueNode, exts, nil
	default:
		return nil, nil, StacktraceNew("expected scalar or mapping node", location,
			stacktrace.WithInfo("node.kind", stacktrace.Stringer(node.Kind)), WithNodePosition(node))
	}
}

func MakeScalarFacetYAML[T any](r *RAML, keyNode, valueNode *yaml.Node, location string) (*ScalarFacet[T], error) {
	if r == nil {
		return nil, StacktraceNew("RAML instance is nil", location, WithNodePosition(valueNode))
	}
	fragmentPath, rn, err := r.resolveInclude(valueNode, location)
	if err != nil {
		return nil, StacktraceNewWrapped("resolve include", err, location, WithNodePosition(valueNode))
	}
	var exts *orderedmap.OrderedMap[string, *DomainExtension]
	rn, exts, err = r.resolveAnnotatedScalar(rn, location)
	if err != nil {
		return nil, StacktraceNewWrapped("resolve value node", err, location, WithNodePosition(valueNode))
	}
	var v T
	if err := rn.Decode(&v); err != nil {
		return nil, StacktraceNewWrapped("decode", err, location,
			WithNodePosition(rn),
			stacktrace.WithInfo("facet", keyNode.Value))
	}
	return MakeScalarFacet(v, keyNode, valueNode, location, fragmentPath, exts), nil
}

// MakeNode constructs a Node from a key/value YAML pair (the common case for struct-field facets).
// includedFrom is the resolved absolute URI returned by resolveInclude, or "" when not an include.
func MakeNode[T any](v T, keyNode, valueNode *yaml.Node, location, includedFrom string) *Node[T] {
	n := &Node[T]{
		Value:    v,
		Location: location,
		KeyPos:   NewNodePosition(keyNode),
		ValuePos: NewNodePosition(valueNode),
	}
	if includedFrom != "" {
		n.Include = &IncludeInfo{Path: valueNode.Value, AbsURI: includedFrom}
	}
	return n
}

// MakeScalarFacet constructs a ScalarFacet from a key/value YAML pair.
// includedFrom is the resolved absolute URI returned by resolveInclude, or "" when not an include.
// Optional CustomDomainExtensions (collected from RAML 1.0 annotated-scalar form) may be attached.
func MakeScalarFacet[T any](v T, keyNode, valueNode *yaml.Node, location, includedFrom string, exts *orderedmap.OrderedMap[string, *DomainExtension]) *ScalarFacet[T] {
	n := &ScalarFacet[T]{
		Value:                  v,
		Location:               location,
		KeyPos:                 NewNodePosition(keyNode),
		ValuePos:               NewNodePosition(valueNode),
		CustomDomainExtensions: exts,
	}
	if includedFrom != "" {
		n.Include = &IncludeInfo{Path: valueNode.Value, AbsURI: includedFrom}
	}
	return n
}

// MakeSeqNode constructs a Node from a sequence-item YAML node (no key). KeyPos is zero.
// includedFrom is the resolved absolute URI returned by resolveInclude, or "" when not an include.
func MakeSeqNode[T any](v T, valueNode *yaml.Node, location, includedFrom string) *Node[T] {
	n := &Node[T]{
		Value:    v,
		Location: location,
		ValuePos: NewNodePosition(valueNode),
	}
	if includedFrom != "" {
		n.Include = &IncludeInfo{Path: valueNode.Value, AbsURI: includedFrom}
	}
	return n
}

// resolveIncludeURI returns the absolute URI referenced by an !include node,
// resolving node.Value relative to the location URI.
// It is the single authoritative URI computation for all include-dispatch sites.
// location may be an OS path or a file:// URI; it is normalised to a URI here.
//
// RAML "absolute" includes (node.Value starting with "/") are resolved
// relative to the workspace root rather than the filesystem root, matching
// RAML specification semantics.  When r.workspaceRootURI is not set the
// standard RFC 3986 resolution is used as a fallback.
func (r *RAML) resolveIncludeURI(node *yaml.Node, location string) (string, error) {
	ref := node.Value
	if strings.HasPrefix(ref, "/") && r.workspaceRootURI != "" {
		// RAML absolute path: resolve against the workspace root.
		base := r.workspaceRootURI
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		// Strip the leading "/" so that resolveURIRef treats it as a
		// relative path segment from the workspace root.
		return resolveURIRef(base, ref[1:])
	}
	return resolveURIRef(PathToFileURI(location), ref)
}

// appendIncludeRef is the single place that builds and records one IncludeRef.
// Callers must verify node.Tag == TagInclude before calling.
// Returns the resolved absolute URI so callers can reuse it without recomputing.
func (r *RAML) appendIncludeRef(node *yaml.Node, location string) (string, error) {
	absURI, err := r.resolveIncludeURI(node, location)
	if err != nil {
		return "", err
	}
	if r.includeRefs == nil {
		r.includeRefs = make(map[string][]IncludeRef)
	}
	r.includeRefs[location] = append(r.includeRefs[location], IncludeRef{
		SourcePath: location,
		Path:       node.Value,
		AbsPath:    absURI,
		Pos:        NewNodePosition(node),
	})
	return absURI, nil
}

// noteIncludeRef records an !include directive in r.includeRefs for LSP
// document-link purposes without reading or caching the referenced file.
// Use this when the file will be loaded through a separate mechanism (e.g.
// parseDataType / parseNamedExample) that maintains its own cache, and
// calling resolveInclude would cause a redundant file read.
// Returns the resolved absolute URI. No-op (returns "") for non-include nodes.
func (r *RAML) noteIncludeRef(node *yaml.Node, location string) string {
	if node.Tag != TagInclude {
		return ""
	}
	// URI resolution errors (malformed location) are non-fatal here; they will
	// surface later when the referenced file is actually opened.
	absURI, _ := r.appendIncludeRef(node, location)
	return absURI
}

// resolveInclude resolves an !include node, recording the directive in
// r.includeRefs for LSP document links, caching the parsed *yaml.Node to
// avoid repeated IO, and enforcing r.maxIncludeSize when set.
// Non-include nodes are returned unchanged.
//
// For YAML/RAML/JSON files the content is decoded into a yaml.Node tree.
// For all other files the content is returned as a !!str scalar (spec §3.2).
//
// The original node MUST be kept at call sites: pass it to MakeNode so that
// ValuePos and Include provenance reference the !include directive in the
// parent document. Use the returned node only for decoding the actual value.
//
// For non-include nodes fragmentPath is empty and the original node is returned.
func (r *RAML) resolveInclude(node *yaml.Node, location string) (fragmentPath string, resolved *yaml.Node, err error) {
	if node.Tag != TagInclude {
		return "", node, nil
	}
	fragmentPath, err = r.appendIncludeRef(node, location)
	if err != nil {
		return "", nil, StacktraceNewWrapped("include: resolve URI", err, location, WithNodePosition(node))
	}

	// Return cached parsed node to avoid repeated IO.
	if r.includeNodeCache == nil {
		r.includeNodeCache = make(map[string]*yaml.Node)
	}
	if cached, ok := r.includeNodeCache[fragmentPath]; ok {
		return fragmentPath, cached, nil
	}

	// Load via the configured ResourceLoader (OS filesystem by default).
	f, err := r.LoadResource(fragmentPath)
	if err != nil {
		return "", nil, StacktraceNewWrapped("include", err, location,
			WithNodePosition(node), stacktrace.WithInfo("path", fragmentPath))
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Error("close file error", "error", cerr)
		}
	}()

	var reader io.Reader = f
	if r.maxIncludeSize > 0 {
		reader = io.LimitReader(f, r.maxIncludeSize+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", nil, StacktraceNewWrapped("include: read file", err, fragmentPath, WithNodePosition(node))
	}
	if r.maxIncludeSize > 0 && int64(len(data)) > r.maxIncludeSize {
		return "", nil, StacktraceNew("include file exceeds size limit", location,
			WithNodePosition(node),
			stacktrace.WithInfo("path", fragmentPath),
			stacktrace.WithInfo("limit", r.maxIncludeSize))
	}

	var result *yaml.Node
	ext := strings.ToLower(filepath.Ext(node.Value))
	switch ext {
	case ".yaml", ".yml", ".raml", ".json":
		// yaml.v3 (YAML 1.2) is a strict superset of JSON; .json files parse correctly.
		result, err = decodeYAMLNode(bytes.NewReader(data))
		if err != nil {
			return "", nil, StacktraceNewWrapped("include: yaml decode", err, fragmentPath, WithNodePosition(node))
		}
	default:
		// Spec: include as a scalar string for non-YAML/RAML files.
		result = &yaml.Node{Kind: yaml.ScalarNode, Tag: TagStr, Value: string(data)}
	}

	r.includeNodeCache[fragmentPath] = result
	return fragmentPath, result, nil
}

func (r *RAML) makeIncludedNode(keyNode, node *yaml.Node, location string) (*DataNode, error) {
	fragmentPath, rn, err := r.resolveInclude(node, location)
	if err != nil {
		return nil, err
	}
	nv, err := r.yamlNodeToNodeValue(rn, fragmentPath, nil)
	if err != nil {
		return nil, StacktraceNewWrapped("include: yaml node to node value", err, fragmentPath,
			WithNodePosition(node))
	}
	n := &DataNode{
		Value:    nv,
		Location: fragmentPath,
		Include:  &IncludeInfo{Path: node.Value, AbsURI: fragmentPath},
		ValuePos: NewNodePosition(node),
	}
	if keyNode != nil {
		n.KeyPos = NewNodePosition(keyNode)
	}
	return n, nil
}

func (r *RAML) makeRootNode(keyNode, node *yaml.Node, location string) (*DataNode, error) {
	if node.Tag == TagInclude {
		return r.makeIncludedNode(keyNode, node, location)
	}

	if node.Value != "" && (node.Value[0] == '{' || node.Value[0] == '[') {
		value, err := jsonschema.UnmarshalJSON(strings.NewReader(node.Value))
		if err != nil {
			return nil, StacktraceNewWrapped("json unmarshal", err, location, WithNodePosition(node))
		}
		n := &DataNode{
			Value:    anyToNodeValue(value),
			Location: location,
			ValuePos: NewNodePosition(node),
		}
		if keyNode != nil {
			n.KeyPos = NewNodePosition(keyNode)
		}
		return n, nil
	}

	return r.makeYamlNode(keyNode, node, location)
}

func (r *RAML) makeYamlNode(keyNode, node *yaml.Node, location string) (*DataNode, error) {
	nv, err := r.yamlNodeToNodeValue(node, location, nil)
	if err != nil {
		return nil, StacktraceNewWrapped("yaml node to node value", err, location, WithNodePosition(node))
	}
	n := &DataNode{
		Value:    nv,
		Location: location,
		ValuePos: NewNodePosition(node),
	}
	if keyNode != nil {
		n.KeyPos = NewNodePosition(keyNode)
	}
	return n, nil
}

func (r *RAML) scalarNodeToNodeValue(node *yaml.Node, location string, visited map[string]struct{}) (*ValueNode, error) {
	switch node.Tag {
	case TagStr, TagTimestamp:
		return &ValueNode{Scalar: &ScalarValue{Value: node.Value}, Raw: node.Value}, nil
	case TagInclude:
		// TODO: For includes that must produce a plain string, a dedicated !includestr
		// tag would be cleaner; for now we resolve and recurse.
		fragmentPath, rn, err := r.resolveInclude(node, location)
		if err != nil {
			return nil, err
		}
		if visited == nil {
			visited = make(map[string]struct{})
		}
		if _, cycle := visited[fragmentPath]; cycle {
			return nil, StacktraceNew("circular include detected", location,
				WithNodePosition(node), stacktrace.WithInfo("path", fragmentPath))
		}
		visited[fragmentPath] = struct{}{}
		defer delete(visited, fragmentPath)
		return r.yamlNodeToNodeValue(rn, fragmentPath, visited)
	default:
		var val any
		if err := node.Decode(&val); err != nil {
			return nil, StacktraceNewWrapped("decode scalar node", err, location)
		}
		return &ValueNode{Scalar: &ScalarValue{Value: val}, Raw: val}, nil
	}
}

func (r *RAML) yamlNodeToNodeValue(node *yaml.Node, location string, visited map[string]struct{}) (*ValueNode, error) {
	switch node.Kind {
	default:
		return nil, StacktraceNew("unexpected kind", location,
			stacktrace.WithInfo("node.kind", stacktrace.Stringer(node.Kind)), WithNodePosition(node))
	case yaml.AliasNode:
		return nil, StacktraceNew("alias nodes are not supported", location, WithNodePosition(node))
	case yaml.DocumentNode:
		return r.yamlNodeToNodeValue(node.Content[0], location, visited)
	case yaml.ScalarNode:
		return r.scalarNodeToNodeValue(node, location, visited)
	case yaml.MappingNode:
		entries := make([]MappingEntry, 0, len(node.Content)/2)
		rawMap := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			kNode := node.Content[i]
			vNode := node.Content[i+1]
			child, err := r.yamlNodeToNodeValue(vNode, location, visited)
			if err != nil {
				return nil, StacktraceNewWrapped("yaml node to node value", err, location,
					WithNodePosition(vNode))
			}
			entries = append(entries, MappingEntry{
				Key:      kNode.Value,
				KeyPos:   NewNodePosition(kNode),
				Value:    child,
				ValuePos: NewNodePosition(vNode),
			})
			rawMap[kNode.Value] = child.Raw
		}
		return &ValueNode{Mapping: &MappingValue{Entries: entries}, Raw: rawMap}, nil
	case yaml.SequenceNode:
		items := make([]SequenceItem, len(node.Content))
		rawSlice := make([]any, len(node.Content))
		for i, item := range node.Content {
			child, err := r.yamlNodeToNodeValue(item, location, visited)
			if err != nil {
				return nil, StacktraceNewWrapped("yaml node to node value", err, location, WithNodePosition(item))
			}
			items[i] = SequenceItem{Value: child, ValuePos: NewNodePosition(item)}
			rawSlice[i] = child.Raw
		}
		return &ValueNode{Sequence: &SequenceValue{Items: items}, Raw: rawSlice}, nil
	}
}
