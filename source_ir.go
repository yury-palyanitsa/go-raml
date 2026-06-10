package raml

import (
	"strings"

	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

// This file implements stage 1 of the two-stage build of endpoints and
// operations: the structural decode.
//
// Stage 1 resolves only the directive names that drive the structural merge
// defined by the RAML spec (§ "Algorithm of Merging Traits and Methods"):
// type:, is: and securedBy:. Everything that is type-bearing (headers, query
// parameters, bodies, responses, uriParameters, ...) is NOT decoded into shapes
// yet — it is retained as its original YAML subtree so that the structural merge
// can operate on declaration trees, exactly as the spec describes, before any
// type resolution happens.
//
// The lexical scope a subtree was physically written in is captured as a
// ParseCtx (AnchorFrag — the fragment scope used for unqualified type references
// and trait names). For a freshly decoded document every subtree shares the
// document's own
// scope; the provenance overlay only records the boundary roots that acquire a
// different scope after substitution/merge (populated in a later stage).

// provenanceOverlay maps the root node of a grafted or substituted subtree to
// the lexical scope that subtree must be resolved in. It is sparse: only nodes
// whose scope differs from the enclosing SourceOperation/SourceEndPoint default
// scope appear here. Nodes absent from the overlay inherit their parent's scope.
type provenanceOverlay map[*yaml.Node]ParseCtx

// SourceOperation is the stage-1 product for a single HTTP method. Its
// directives are resolved structurally; its type-bearing body is retained as a
// YAML subtree (body) plus a provenance overlay, to be materialized into a real
// Operation in stage 2.
type SourceOperation struct {
	ID int64

	Method string

	// Directives — resolved structurally in stage 1 (names only, no shapes).
	Traits []*Trait
	// RTTraits holds traits contributed by an applied resource type (method-level),
	// kept separate from Traits to preserve the spec application order
	// (method → resource → RT-method → RT-resource).
	RTTraits          []*Trait
	SecuredBy         []*SecurityScheme
	explicitSecuredBy bool

	// body is the operation body with the directive keys (is:, securedBy:)
	// removed — the type-bearing remainder to be materialized in stage 2. It is
	// nil for an empty operation.
	body *yaml.Node
	// scope is the default lexical scope of body (the document it was decoded
	// from). Subtrees with a different scope are recorded in provenance.
	scope ParseCtx
	// provenance records boundary roots whose scope differs from scope. Empty
	// immediately after stage-1 decode; populated during the structural merge.
	provenance provenanceOverlay

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}

// SourceEndPoint is the stage-1 product for a resource. Its directives (type:,
// is:, securedBy:) are resolved structurally; its child operations and nested
// endpoints are themselves stage-1 IR; its type-bearing endpoint facets
// (uriParameters, displayName, description, custom domain extensions) are
// retained as a YAML subtree for stage-2 materialization.
type SourceEndPoint struct {
	ID int64

	URI     string
	FullURI string

	// Directives — resolved structurally in stage 1.
	ResourceType *ResourceType
	Traits       []*Trait
	// RTTraits holds traits contributed by an applied resource type (endpoint-level),
	// kept separate from Traits to preserve the spec application order.
	RTTraits          []*Trait
	SecuredBy         []*SecurityScheme
	explicitSecuredBy bool

	Operations *orderedmap.OrderedMap[string, *SourceOperation]
	EndPoints  *orderedmap.OrderedMap[string, *SourceEndPoint]

	// body is the endpoint-level type-bearing remainder (uriParameters,
	// displayName, description, custom domain extensions). It is nil when the
	// endpoint declares none of these.
	body       *yaml.Node
	scope      ParseCtx
	provenance provenanceOverlay

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}

// compileSourceProvenance performs template parameter substitution on a
// trait/resource-type body while recording the lexical scope of every
// substituted subtree into overlay.
//
// Per decision D2 (static -> declaration site, dynamic -> application site):
//   - Static body nodes (no substitution applied) keep the declaration scope.
//     They are left UNMARKED in overlay; stage-2 decode treats an unmarked node
//     as carrying its enclosing SourceOperation/SourceEndPoint default scope,
//     which is the declaration fragment's scope.
//   - Dynamic nodes produced by substitution (a complex parameter spliced in, or
//     a scalar whose text received a parameter value) resolve against the caller.
//     Their boundary root is recorded as callerScope so stage-2 decode pushes the
//     caller's namespace while resolving inside that subtree.
//
// idx follows the same positional walk used by collectVariablesIndex and the
// original compileSource: a node has index idx and its i-th child has idx+i.
// nodeVariableIndex is the precomputed variable index of the template body.
//
// The returned tree shares unchanged node pointers with the input; only nodes on
// a substitution path are freshly allocated, so node identity remains a valid key
// for overlay lookups during the structural merge and stage-2 decode.
func compileSourceProvenance(
	node *yaml.Node,
	params map[string]*yaml.Node,
	idx int,
	nodeVariableIndex map[int][]VariableInfo,
	callerScope ParseCtx,
	overlay provenanceOverlay,
) *yaml.Node {
	if node == nil {
		return node
	}

	if node.Kind == yaml.ScalarNode {
		variables, exists := nodeVariableIndex[idx]
		if !exists {
			return node // static scalar: declaration scope (unmarked).
		}
		// A complex (non-scalar) parameter replaces the entire node. The grafted
		// subtree comes from the caller, so it resolves in caller scope.
		for _, variable := range variables {
			param, ok := params[variable.Name]
			if ok && param.Kind != yaml.ScalarNode {
				overlay[param] = callerScope
				return param
			}
		}
		// Scalar string replacement. Mark the node caller-scoped only when at
		// least one parameter was actually applied; an unsubstituted scalar stays
		// static (declaration scope).
		newNode := *node
		substituted := false
		for _, variable := range variables {
			param, ok := params[variable.Name]
			if !ok {
				continue
			}
			value := param.Value
			for _, action := range variable.Actions {
				value = applyTemplateAction(value, action)
			}
			newNode.Value = strings.Replace(newNode.Value, variable.Substring, value, 1)
			substituted = true
		}
		if !substituted {
			return node
		}
		overlay[&newNode] = callerScope
		return &newNode
	}

	// Container node: recurse. The container itself is structural and keeps the
	// enclosing scope; only substituted descendants are marked.
	modified := false
	content := make([]*yaml.Node, len(node.Content))
	for i := 0; i < len(node.Content); i++ {
		child := compileSourceProvenance(node.Content[i], params, idx+i, nodeVariableIndex, callerScope, overlay)
		content[i] = child
		if child != node.Content[i] {
			modified = true
		}
	}
	if modified {
		newNode := *node
		newNode.Content = content
		return &newNode
	}
	return node
}

// remainderMapping builds a mapping node from the collected key/value content,
// preserving the position metadata of the original node. It returns nil when no
// content was retained so callers can represent an empty body as nil.
func remainderMapping(orig *yaml.Node, content []*yaml.Node) *yaml.Node {
	if len(content) == 0 {
		return nil
	}
	return &yaml.Node{
		Kind:    yaml.MappingNode,
		Tag:     orig.Tag,
		Content: content,
		Line:    orig.Line,
		Column:  orig.Column,
	}
}

// makeSourceOperation performs the stage-1 structural decode of an operation.
// keyNode is the method name; valueNode is the operation body. The current parse
// context is captured as the body's default scope.
func (r *RAML) makeSourceOperation(location string, keyNode, valueNode *yaml.Node) (*SourceOperation, error) {
	op := &SourceOperation{
		ID:         r.generateSequenceID(),
		Method:     keyNode.Value,
		scope:      r.currentParseCtx(),
		provenance: make(provenanceOverlay),
		raml:       r,
		KeyPos:     stacktrace.Position{Line: keyNode.Line, Column: keyNode.Column, EndLine: keyNode.Line},
		ValuePos:   NewNodePosition(valueNode),
		Location:   location,
	}

	if valueNode.Tag == TagNull {
		return op, nil
	} else if valueNode.Kind != yaml.MappingNode {
		return nil, StacktraceNew("operation must be a mapping node", location, WithNodePosition(valueNode))
	}

	content := make([]*yaml.Node, 0, len(valueNode.Content))
	for i := 0; i < len(valueNode.Content); i += 2 {
		k := valueNode.Content[i]
		v := valueNode.Content[i+1]
		switch k.Value {
		case FacetIs:
			traits, err := r.makeTraits(v, location)
			if err != nil {
				return nil, StacktraceNewWrapped("make traits", err, location, WithNodePosition(v))
			}
			op.Traits = traits
		case FacetSecuredBy:
			securitySchemes, err := r.makeSecuritySchemes(v, location)
			if err != nil {
				return nil, StacktraceNewWrapped("make security schemes", err, location, WithNodePosition(v))
			}
			op.SecuredBy = securitySchemes
			op.explicitSecuredBy = true
		default:
			content = append(content, k, v)
		}
	}
	op.body = remainderMapping(valueNode, content)
	return op, nil
}

// makeSourceEndPoint performs the stage-1 structural decode of a resource and,
// recursively, its child operations and nested endpoints. It does not register
// the endpoint into the RAML endpoint index — stage 1 produces a detached IR
// tree that the structural merge consumes before materialization.
func (r *RAML) makeSourceEndPoint(keyNode, valueNode *yaml.Node, location, parent string) (*SourceEndPoint, error) {
	ep := &SourceEndPoint{
		ID:         r.generateSequenceID(),
		URI:        keyNode.Value,
		FullURI:    parent + keyNode.Value,
		Operations: orderedmap.New[string, *SourceOperation](0),
		EndPoints:  orderedmap.New[string, *SourceEndPoint](0),
		scope:      r.currentParseCtx(),
		provenance: make(provenanceOverlay),
		raml:       r,
		KeyPos:     stacktrace.Position{Line: keyNode.Line, Column: keyNode.Column, EndLine: keyNode.Line},
		ValuePos:   NewNodePosition(valueNode),
		Location:   location,
	}

	if valueNode.Tag == TagNull {
		return ep, nil
	} else if valueNode.Kind != yaml.MappingNode {
		return nil, StacktraceNew("endpoint must be a mapping node", location, WithNodePosition(valueNode))
	}

	content := make([]*yaml.Node, 0, len(valueNode.Content))
	for i := 0; i < len(valueNode.Content); i += 2 {
		k := valueNode.Content[i]
		v := valueNode.Content[i+1]
		switch k.Value {
		case FacetType:
			rt, err := r.makeResourceType(v, location)
			if err != nil {
				return nil, StacktraceNewWrapped("make resource type", err, location, WithNodePosition(v))
			}
			ep.ResourceType = rt
		case FacetIs:
			traits, err := r.makeTraits(v, location)
			if err != nil {
				return nil, StacktraceNewWrapped("make traits", err, location, WithNodePosition(v))
			}
			ep.Traits = traits
		case FacetSecuredBy:
			securitySchemes, err := r.makeSecuritySchemes(v, location)
			if err != nil {
				return nil, StacktraceNewWrapped("make security schemes", err, location, WithNodePosition(v))
			}
			ep.SecuredBy = securitySchemes
			ep.explicitSecuredBy = true
		case MethodGet, MethodPost, MethodPut, MethodDelete, MethodPatch,
			MethodOptions, MethodHead, MethodTrace, MethodConnect:
			operation, err := r.makeSourceOperation(location, k, v)
			if err != nil {
				return nil, StacktraceNewWrapped("make source operation", err, location, WithNodePosition(v))
			}
			ep.Operations.Set(k.Value, operation)
		default:
			switch {
			case IsEndPoint(k.Value):
				child, err := r.makeSourceEndPoint(k, v, location, ep.FullURI)
				if err != nil {
					return nil, StacktraceNewWrapped("make source endpoint", err, location, WithNodePosition(v))
				}
				ep.EndPoints.Set(k.Value, child)
			default:
				// displayName, description, uriParameters and custom domain
				// extensions are type-bearing or scalar facets retained for
				// stage-2 materialization.
				content = append(content, k, v)
			}
		}
	}
	ep.body = remainderMapping(valueNode, content)
	return ep, nil
}
