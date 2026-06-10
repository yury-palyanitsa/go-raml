package raml

import (
	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

// ResourceTypeDefinition is a RAML 1.0 resource type definition template.
// It mirrors TraitDefinition but compiles to an EndPoint instead of an Operation.
type ResourceTypeDefinition struct {
	ID int64

	// Name is the declared resource type name (the key in the resourceTypes: map, e.g. "collection").
	Name string

	Usage       *ScalarFacet[string]
	Description *ScalarFacet[string]

	// Source holds the filtered YAML mapping node (without "usage"/"description" keys).
	Source            *yaml.Node
	DeclaredVariables map[string]struct{}
	NodeVariableIndex map[int][]VariableInfo
	// OptionalMethods tracks methods declared with the trailing "?" (e.g. "get?").
	OptionalMethods map[string]struct{}

	Link *ResourceTypeFragment

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	// anchorFrag is the fragment scope at this resource type's declaration site:
	// it resolves unqualified type names and governs trait-name resolution.
	anchorFrag ReferenceResolver
	raml       *RAML
}

// ResourceType is a reference to a resource type, stored on an EndPoint.
type ResourceType struct {
	ID int64

	Name   string
	Params map[string]string

	// Definition is set during resource type application and points to the resolved ResourceTypeDefinition.
	Definition *ResourceTypeDefinition

	Location string
	ValuePos stacktrace.Position
	// anchorFrag is the fragment scope at this resource type reference's site: the
	// document in which the type: entry is physically written. It governs lexical
	// resolution of the resource type name (and dotted lib.rtName forms via that
	// document's uses:). For an endpoint type: written in the root API this is the
	// API; for a parent type: written inside a ResourceType fragment body it is that
	// fragment, keeping RT→RT inheritance self-contained.
	anchorFrag ReferenceResolver
	raml       *RAML
}

// IsHTTPMethod returns true if s is a valid HTTP method name (lowercase).
func IsHTTPMethod(s string) bool {
	switch s {
	case MethodGet, MethodPost, MethodPut, MethodDelete, MethodPatch, MethodOptions, MethodHead, MethodTrace, MethodConnect:
		return true
	}
	return false
}

func (r *RAML) makeResourceTypeDefinition(keyNode, valueNode *yaml.Node, location string) (*ResourceTypeDefinition, error) {
	keyPos := NewNodePosition(valueNode)
	if keyNode != nil {
		keyPos = NewNodePosition(keyNode)
	}
	rtDef := &ResourceTypeDefinition{
		ID:                r.generateSequenceID(),
		DeclaredVariables: make(map[string]struct{}),
		NodeVariableIndex: make(map[int][]VariableInfo),
		OptionalMethods:   make(map[string]struct{}),
		raml:              r,
		KeyPos:            keyPos,
		ValuePos:          NewNodePosition(valueNode),
		Location:          location,
		anchorFrag:        r.currentParseCtx().AnchorFrag,
	}
	if keyNode != nil {
		rtDef.Name = keyNode.Value
	}

	r.storeEntityNode(rtDef.ID, keyNode, valueNode)

	if err := rtDef.decode(valueNode); err != nil {
		return nil, StacktraceNewWrapped("decode resource type definition", err, location, WithNodePosition(valueNode))
	}

	if rtDef.Source != nil {
		if err := rtDef.collectVariablesIndex(rtDef.Source, 0); err != nil {
			return nil, StacktraceNewWrapped("collect variables index", err, location, WithNodePosition(rtDef.Source))
		}
	}

	// Precompile is skipped: compiling requires knowing which optional methods the
	// target endpoint defines (to filter them), which varies per application site.

	return rtDef, nil
}

func (rt *ResourceTypeDefinition) decode(node *yaml.Node) error {
	if node.Tag == TagNull {
		return nil
	} else if node.Tag == TagInclude {
		rtFrag, err := rt.raml.parseResourceTypeFragment(rt.raml.noteIncludeRef(node, rt.Location))
		if err != nil {
			return StacktraceNewWrapped("parse resource type fragment", err, rt.Location, WithNodePosition(node))
		}
		rt.Link = rtFrag
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("resource type definition must be a mapping node", rt.Location, WithNodePosition(node))
	}

	content := make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		switch keyNode.Value {
		case FacetUsage:
			sn, err := MakeScalarFacetYAML[string](rt.raml, keyNode, valueNode, rt.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, rt.Location, WithNodePosition(valueNode))
			}
			rt.Usage = sn
		case FacetDisplayName, FacetDescription, FacetUriParameters, FacetType, FacetIs, FacetSecuredBy:
			content = append(content, keyNode, valueNode)
		default:
			// Annotation references are also valid at resource-type level and flow through
			// to the compiled endpoint via Source, matching traits behaviour.
			if IsCustomDomainExtensionNode(keyNode.Value) {
				content = append(content, keyNode, valueNode)
				continue
			}
			// Only HTTP methods (with or without optional "?" suffix) are valid here.
			methodName, wasOptional := chompImplicitOptional(keyNode.Value)
			if !IsHTTPMethod(methodName) {
				return StacktraceNew("resource type method must be an HTTP method", rt.Location,
					WithNodePosition(keyNode), stacktrace.WithInfo("key", keyNode.Value))
			}
			if wasOptional {
				rt.OptionalMethods[methodName] = struct{}{}
				// Normalize: store the plain key (no "?") so compile needs no renaming.
				normalizedKey := *keyNode
				normalizedKey.Value = methodName
				content = append(content, &normalizedKey, valueNode)
			} else {
				content = append(content, keyNode, valueNode)
			}
		}
	}
	rt.Source = &yaml.Node{
		Kind:    node.Kind,
		Tag:     node.Tag,
		Content: content,
		Line:    node.Line,
		Column:  node.Column,
	}
	return nil
}

func (rt *ResourceTypeDefinition) collectVariablesIndex(node *yaml.Node, idx int) error {
	return collectVariablesIndex(rt.Location, node, idx, rt.NodeVariableIndex, rt.DeclaredVariables)
}

// collectRequiredVariables collects all variable names from the normalized source YAML.
// This is used to determine which parameters are actually required after optional methods are removed.
func (rt *ResourceTypeDefinition) collectRequiredVariables(node *yaml.Node, idx int) map[string]struct{} {
	return collectRequiredVariables(node, idx, rt.NodeVariableIndex)
}

// reservedParam returns true if name is a RAML-reserved template parameter.
func reservedParam(name string) bool {
	switch name {
	case ParamResourcePath, ParamResourcePathName, ParamMethodName:
		return true
	}
	return false
}

// normalizeOptionalMethods returns a copy of the YAML mapping node with optional methods that are
// absent from existingOps removed. Keys are already normalized (no "?" suffix) by decode.
func (rt *ResourceTypeDefinition) normalizeOptionalMethods(node *yaml.Node, existingOps map[string]struct{}) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode || existingOps == nil || len(rt.OptionalMethods) == 0 {
		return node
	}
	content := make([]*yaml.Node, 0, len(node.Content))
	modified := false
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if _, isOptional := rt.OptionalMethods[keyNode.Value]; isOptional {
			if _, onEndpoint := existingOps[keyNode.Value]; !onEndpoint {
				modified = true
				continue
			}
		}
		content = append(content, keyNode, valNode)
	}
	if !modified {
		return node
	}
	newNode := *node
	newNode.Content = content
	return &newNode
}

func (r *RAML) makeResourceType(valueNode *yaml.Node, location string) (*ResourceType, error) {
	if valueNode.Tag == TagNull {
		return nil, nil
	}
	rt := &ResourceType{
		ID:       r.generateSequenceID(),
		Location: location,
		raml:     r,
		ValuePos: NewNodePosition(valueNode),

		anchorFrag: r.currentParseCtx().AnchorFrag,
	}
	if err := rt.decode(valueNode); err != nil {
		return nil, StacktraceNewWrapped("decode resource type", err, location, WithNodePosition(valueNode))
	}
	return rt, nil
}

func (rt *ResourceType) decode(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		rt.Name = node.Value
	case yaml.MappingNode:
		if len(node.Content) < 2 {
			return StacktraceNew("resource type reference must have a name", rt.Location, WithNodePosition(node))
		}
		rt.Name = node.Content[0].Value
		if node.Content[1].Tag != TagNull {
			if err := node.Content[1].Decode(&rt.Params); err != nil {
				return StacktraceNewWrapped("decode resource type parameters", err, rt.Location,
					WithNodePosition(node.Content[1]))
			}
		}
	default:
		return StacktraceNew("resource type must be either scalar or mapping node", rt.Location, WithNodePosition(node))
	}
	return nil
}

func (r *RAML) unmarshalResourceTypeDefinitions(
	node *yaml.Node,
	location string,
) (*orderedmap.OrderedMap[string, *ResourceTypeDefinition], error) {
	if node.Tag == TagNull {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, StacktraceNew("resourceTypes must be a mapping node", location, WithNodePosition(node))
	}
	rtDefs := orderedmap.New[string, *ResourceTypeDefinition](len(node.Content) / 2)
	for j := 0; j < len(node.Content); j += 2 {
		keyNode := node.Content[j]
		data := node.Content[j+1]
		rtDef, err := r.makeResourceTypeDefinition(keyNode, data, location)
		if err != nil {
			return nil, StacktraceNewWrapped("make resource type definition", err, location, WithNodePosition(data))
		}
		rtDefs.Set(keyNode.Value, rtDef)
	}
	return rtDefs, nil
}
