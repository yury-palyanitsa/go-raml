package raml

import (
	"context"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// RAML is a store for all fragments and shapes.
// WARNING: Not thread-safe
type RAML struct {
	// loader is the ResourceLoader used to fetch all resources (RAML fragments,
	// !include targets, JSON Schema $refs). Defaults to OSFileLoader for file://
	// URIs.
	loader ResourceLoader

	// workspaceRootURI is the file:// URI of the workspace root directory.
	// RAML "absolute" includes (paths starting with "/") are resolved relative
	// to this root rather than the filesystem root, matching RAML specification
	// semantics.  Set by ParseFromPathCtx and ParseFromString during setup.
	workspaceRootURI string
	fragmentsCache   map[string]Fragment // Library, NamedExample, DataType

	// FragmentAnnotationTypes is a map of fragment location to a map of types.
	fragmentTypes map[string]map[string]*BaseShape

	// FragmentAnnotationTypes is a map of fragment location to a map of annotation types.
	fragmentAnnotationTypes map[string]map[string]*BaseShape

	// FragmentTypeDefinitions is a map of fragment location to a list of top-level shapes.
	// This includes types, annotationTypes, request/response bodies, headers, query parameters and query strings.
	fragmentTypeDefinitions map[string][]*BaseShape

	// entryPoint is a Library, NamedExample or DataType fragment that is used as an entry point for the resolution.
	entryPoint Fragment
	// basePath   string

	// May be reused for both validation and resolution.
	domainExtensions []*DomainExtension

	endPoints map[string]*EndPoint
	shapes    []*BaseShape
	// Temporary storages for unresolved entities.
	unresolvedShapes []*BaseShape

	// TODO: Maybe it makes sense to make a separate context for WebAPI?
	globalProtocols []string
	globalMediaType []string
	globalSecuredBy []*SecurityScheme

	// idCounter is a counter for generating unique IDs per raml
	idCounter int64
	// ctx is a context of the RAML, for future use.
	ctx context.Context

	// jsonShapeRegistry caches the Shape produced from each compiled *jsonschema.Schema
	// so that multiple $ref references to the same definition share a single view.
	jsonShapeRegistry map[*jsonschema.Schema]Shape

	// includeRefs tracks every !include directive resolved during parsing,
	// keyed by the absolute path of the file containing the directive.
	// Populated by (r *RAML).resolveInclude.
	includeRefs map[string][]IncludeRef

	// includeNodeCache caches the parsed *yaml.Node for each !include target
	// path to avoid repeated IO when the same file is included more than once.
	includeNodeCache map[string]*yaml.Node

	// maxIncludeSize is the maximum number of bytes permitted for a single
	// !include file. Zero means no limit.
	maxIncludeSize int64

	// unwrapped is set to true when UnwrapShapes has been called (i.e. the
	// parser was invoked with OptWithUnwrap).  It is used by MarshalJSONLD to
	// set doc:transformed correctly on the processing-data node.
	unwrapped bool

	// parseCtxStack is pushed/popped on entry/exit of each fragment decode so
	// that shapes, traits, and domain extensions created during parsing can
	// capture their resolution context directly rather than storing a string
	// path and performing a fragmentsCache lookup at resolution time.
	parseCtxStack []ParseCtx

	// activeOverlay is the provenance overlay of the SourceOperation/SourceEndPoint
	// body currently being materialized in stage-2 decode. It is consulted by
	// makeNewShapeYAMLWithDefault so that a type-bearing node produced by
	// parameter substitution (marked with the caller's scope) is decoded under
	// that scope even when it sits below facet-value granularity. nil outside the
	// two-stage decode path, where it has no effect.
	activeOverlay provenanceOverlay

	// jsonSchemaCompiler is a shared compiler for all JSON Schema fragments
	// parsed within this RAML instance. Reusing a single compiler lets the
	// jsonschema library cache compiled *Schema objects and loaded documents
	// across schemas, so common $ref targets are only resolved once.
	jsonSchemaCompiler *jsonschema.Compiler

	// retainSourceNodes is set by OptWithRawSource. When true, the raw
	// *yaml.Node root mapping for each decoded fragment is stored in
	// sourceNodes, keyed by the fragment's absolute path. It is inherited
	// by all recursive sub-parses (libraries, resource types, traits, …)
	// because they share the same RAML instance.
	retainSourceNodes bool

	// sourceNodes maps each fragment path to its raw *yaml.Node root.
	// Only populated when retainSourceNodes is true.
	sourceNodes map[string]*yaml.Node

	// sourceInfo is set by OptWithRawSource. When non-nil, every make*
	// constructor records the key+value *yaml.Node pair for the entity it
	// creates so that tooling (e.g. an LSP server) can navigate from a model
	// entity back to its exact source location without re-walking the YAML AST.
	sourceInfo *SourceInfo
}

// ParseCtx carries the fragment reference needed during RAML decoding.
// It is managed as a stack on the RAML struct so all shape-creation helpers
// can read the current context without threading extra parameters.
type ParseCtx struct {
	// AnchorFrag is the fragment scope in effect while decoding — the nearest
	// Library or APIFragment used to resolve unqualified type names and the
	// fragment whose uses: map governs trait-name resolution. Self-referential
	// for Library/APIFragment.
	AnchorFrag ReferenceResolver
}

// currentParseCtx returns the top of the parse context stack, or a zero
// ParseCtx if the stack is empty (e.g. during the unwrap/compile phase).
func (r *RAML) currentParseCtx() ParseCtx {
	if len(r.parseCtxStack) == 0 {
		return ParseCtx{}
	}
	return r.parseCtxStack[len(r.parseCtxStack)-1]
}

// pushParseCtx pushes ctx onto the parse context stack.
func (r *RAML) pushParseCtx(ctx ParseCtx) {
	r.parseCtxStack = append(r.parseCtxStack, ctx)
}

// popParseCtx pops the top entry from the parse context stack.
func (r *RAML) popParseCtx() {
	if len(r.parseCtxStack) > 0 {
		r.parseCtxStack = r.parseCtxStack[:len(r.parseCtxStack)-1]
	}
}

// EntryPoint returns the entry point of the RAML.
func (r *RAML) EntryPoint() Fragment {
	return r.entryPoint
}

// SetEntryPoint sets the entry point of the RAML.
func (r *RAML) SetEntryPoint(entryPoint Fragment) *RAML {
	r.entryPoint = entryPoint
	return r
}

// GetLocation returns the location of the RAML.
func (r *RAML) GetLocation() string {
	if r.entryPoint == nil {
		return ""
	}
	return r.entryPoint.GetLocation()
}

// GetAllAnnotationsPtr returns all annotations as pointers.
func (r *RAML) GetAllAnnotationsPtr() []*DomainExtension {
	var annotations []*DomainExtension
	return append(annotations, r.domainExtensions...)
}

// GetAllAnnotations returns all annotations.
func (r *RAML) GetAllAnnotations() []DomainExtension {
	var annotations []DomainExtension
	for _, de := range r.domainExtensions {
		annotations = append(annotations, *de)
	}
	return annotations
}

// New creates a new RAML.
func New(ctx context.Context) *RAML {
	return &RAML{
		fragmentTypes:           make(map[string]map[string]*BaseShape),
		fragmentAnnotationTypes: make(map[string]map[string]*BaseShape),
		fragmentTypeDefinitions: make(map[string][]*BaseShape),
		fragmentsCache:          make(map[string]Fragment),
		endPoints:               make(map[string]*EndPoint),
		domainExtensions:        make([]*DomainExtension, 0),
		includeRefs:             make(map[string][]IncludeRef),
		includeNodeCache:        make(map[string]*yaml.Node),
		maxIncludeSize:          DefaultMaxIncludeSize,
		ctx:                     ctx,
		jsonSchemaCompiler:      newOfflineCompiler(),
		jsonShapeRegistry:       make(map[*jsonschema.Schema]Shape),
	}
}

// Shapes returns all shapes.
func (r *RAML) GetShapes() []*BaseShape {
	return r.shapes
}

// GetSourceNode returns the raw *yaml.Node root mapping that was decoded for
// the fragment at path, or nil if OptWithRawSource was not set or the path
// was not parsed by this RAML instance.
// path may be a file:// URI or an OS path; both are normalised to a URI for
// the internal lookup so that callers do not need to convert manually.
func (r *RAML) GetSourceNode(path string) *yaml.Node {
	return r.sourceNodes[PathToFileURI(path)]
}

// SourceInfo returns the per-entity source node index built during parsing,
// or nil if OptWithRawSource was not passed. Use SourceInfo.Get(entity.ID)
// to retrieve the key+value *yaml.Node pair for any model entity.
func (r *RAML) SourceInfo() *SourceInfo {
	return r.sourceInfo
}

// storeEntityNode records the key+value node pair for a model entity in
// r.sourceInfo. No-op when sourceInfo is nil (OptWithSourceInfo not set).
func (r *RAML) storeEntityNode(id int64, key, value *yaml.Node) {
	r.sourceInfo.store(id, key, value)
}

// GetIncludeRefs returns all !include directives recorded for the given source
// file path during parsing. The slice is in parse order.
func (r *RAML) GetIncludeRefs(sourcePath string) []IncludeRef {
	return r.includeRefs[PathToFileURI(sourcePath)]
}

// IsUnwrapped reports whether UnwrapShapes has been called on this RAML instance.
func (r *RAML) IsUnwrapped() bool {
	return r.unwrapped
}

// GetFragments returns the fragments cache (location → Fragment) built during parsing.
func (r *RAML) GetFragments() map[string]Fragment {
	return r.fragmentsCache
}

// GetAllIncludeRefs returns the full include-refs map (source path → []IncludeRef).
func (r *RAML) GetAllIncludeRefs() map[string][]IncludeRef {
	return r.includeRefs
}

func (r *RAML) PutShape(shape *BaseShape) {
	r.shapes = append(r.shapes, shape)
}

// GetFragmentTypePtrs returns fragment shapes as pointers.
func (r *RAML) GetFragmentTypePtrs(location string) map[string]*BaseShape {
	return r.fragmentTypes[PathToFileURI(location)]
}

// GetTypeFromFragmentPtr returns a shape from a fragment as a pointer.
func (r *RAML) GetTypeFromFragmentPtr(location string, typeName string) (*BaseShape, error) {
	loc, ok := r.fragmentTypes[PathToFileURI(location)]
	if !ok {
		return nil, fmt.Errorf("location %s not found", location)
	}
	return loc[typeName], nil
}

// PutTypeIntoFragment puts a shape into a fragment.
func (r *RAML) PutTypeIntoFragment(name string, location string, shape *BaseShape) {
	location = PathToFileURI(location)
	loc, ok := r.fragmentTypes[location]
	if !ok {
		loc = make(map[string]*BaseShape)
		r.fragmentTypes[location] = loc
	}
	loc[name] = shape
}

// PutTypeDefinitionIntoFragment puts a shape into a fragment's type definitions list.
func (r *RAML) PutTypeDefinitionIntoFragment(location string, shape *BaseShape) {
	location = PathToFileURI(location)
	if r.fragmentTypeDefinitions == nil {
		r.fragmentTypeDefinitions = make(map[string][]*BaseShape)
	}
	r.fragmentTypeDefinitions[location] = append(r.fragmentTypeDefinitions[location], shape)
}

func (r *RAML) GetTypeDefinitionsFromFragment(location string) []*BaseShape {
	return r.fragmentTypeDefinitions[PathToFileURI(location)]
}

// GetAnnotationTypeDefinitionsFromFragment returns all annotation type shapes
// defined in the fragment at location. The returned map is keyed by type name.
func (r *RAML) GetAnnotationTypeDefinitionsFromFragment(location string) map[string]*BaseShape {
	return r.fragmentAnnotationTypes[PathToFileURI(location)]
}

// GetTypeFromFragmentPtr returns a shape from a fragment.
func (r *RAML) GetAnnotationTypeFromFragmentPtr(location string, typeName string) (*BaseShape, error) {
	loc, ok := r.fragmentAnnotationTypes[PathToFileURI(location)]
	if !ok {
		return nil, fmt.Errorf("location %s not found", location)
	}
	return loc[typeName], nil
}

// PutTypeIntoFragment puts a shape into a fragment.
func (r *RAML) PutAnnotationTypeIntoFragment(name string, location string, shape *BaseShape) {
	location = PathToFileURI(location)
	loc, ok := r.fragmentAnnotationTypes[location]
	if !ok {
		loc = make(map[string]*BaseShape)
		r.fragmentAnnotationTypes[location] = loc
	}
	loc[name] = shape
}

// GetFragment returns a fragment.
func (r *RAML) GetFragment(location string) Fragment {
	return r.fragmentsCache[PathToFileURI(location)]
}

// PutFragment puts a fragment.
func (r *RAML) PutFragment(location string, fragment Fragment) {
	location = PathToFileURI(location)
	if _, ok := r.fragmentsCache[location]; !ok {
		r.fragmentsCache[location] = fragment
	}
}

func (r *RAML) GetReferencedType(refName string, location string) (*BaseShape, error) {
	frag := r.GetFragment(location)
	if frag == nil {
		return nil, fmt.Errorf("fragment not found")
	}
	resolver, ok := frag.(ReferenceResolver)
	if !ok {
		return nil, fmt.Errorf("fragment at %s does not support type resolution", location)
	}
	ref, err := resolver.GetReferenceType(refName)
	if err != nil {
		return nil, fmt.Errorf("get reference type: %s: %w", refName, err)
	}
	return ref, nil
}

// GetLibraryLinkByPrefix returns the LibraryLink for libraryPrefix as it appears
// in the uses map of the fragment at location. Returns nil when the fragment or
// prefix cannot be found.
func (r *RAML) GetLibraryLinkByPrefix(prefix string, location string) *LibraryLink {
	frag := r.GetFragment(location)
	if frag == nil {
		return nil
	}
	switch f := frag.(type) {
	case *Library:
		if f.Uses != nil {
			if link, ok := f.Uses.Get(prefix); ok {
				return link
			}
		}
	case *APIFragment:
		if f.Uses != nil {
			if link, ok := f.Uses.Get(prefix); ok {
				return link
			}
		}
	}
	return nil
}

func (r *RAML) GetReferencedAnnotationType(refName string, location string) (*BaseShape, error) {
	frag := r.GetFragment(location)
	if frag == nil {
		return nil, fmt.Errorf("fragment not found")
	}
	resolver, ok := frag.(ReferenceResolver)
	if !ok {
		return nil, fmt.Errorf("fragment at %s does not support annotation type resolution", location)
	}
	ref, err := resolver.GetReferenceAnnotationType(refName)
	if err != nil {
		return nil, fmt.Errorf("get reference annotation type: %s: %w", refName, err)
	}
	return ref, nil
}
