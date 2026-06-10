package raml

import (
	"fmt"

	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

type FragmentKind int

const (
	FragmentUnknown FragmentKind = iota - 1
	FragmentLibrary
	FragmentDataType
	FragmentNamedExample
	FragmentAPI
	FragmentDocumentationItem
	FragmentResourceType
	FragmentTrait
	FragmentAnnotationTypeDeclaration
	FragmentOverlay
	FragmentExtension
	FragmentSecurityScheme
)

// String returns the name of the FragmentKind for debugging/logging.
func (k FragmentKind) String() string {
	switch k {
	case FragmentUnknown:
		return "Unknown"
	case FragmentLibrary:
		return "Library"
	case FragmentDataType:
		return "DataType"
	case FragmentNamedExample:
		return "NamedExample"
	case FragmentAPI:
		return "API"
	case FragmentDocumentationItem:
		return "DocumentationItem"
	case FragmentResourceType:
		return "ResourceType"
	case FragmentTrait:
		return "Trait"
	case FragmentAnnotationTypeDeclaration:
		return "AnnotationTypeDeclaration"
	case FragmentOverlay:
		return "Overlay"
	case FragmentExtension:
		return "Extension"
	case FragmentSecurityScheme:
		return "SecurityScheme"
	default:
		return fmt.Sprintf("FragmentKind(%d)", k)
	}
}

// CutReferenceName cuts a reference name into two parts: before and after the dot.
func CutReferenceName(refName string) (string, string, bool) {
	// External ref - <fragment>.<identifier>
	// Local ref - <identifier>
	return CutLast(refName, '.')
}

type LocationGetter interface {
	GetLocation() string
}

// Fragment is the base interface for all RAML fragments.
// It provides only location information. Capabilities are checked via type assertions.
type Fragment interface {
	LocationGetter
}

// ReferenceResolver resolves type, annotation type, resource type, and trait references
// from within any typed fragment that declares a "uses:" map.
// Per the RAML 1.0 spec every typed fragment may carry a "uses:" node that imports a Library
// and may then reference any of the four declaration kinds exported by that library.
// Implemented by all typed fragments that support "uses:".
type ReferenceResolver interface {
	GetLocation() string
	GetReferenceType(refName string) (*BaseShape, error)
	GetReferenceAnnotationType(refName string) (*BaseShape, error)
	GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error)
	GetTraitDefinition(refName string) (*TraitDefinition, error)
}

// SecuritySchemeResolver can resolve security scheme definitions.
// Implemented by: Library, APIFragment.
type SecuritySchemeResolver interface {
	GetSecuritySchemeDefinition(refName string) (*SecuritySchemeDefinition, error)
}

// filterFragmentUses scans a mapping node for the top-level "uses:" key, unmarshals it,
// and returns a shallow copy of the node with that key removed. It is used by fragment
// UnmarshalYAML implementations that need to strip fragment-level library declarations
// before forwarding the node to a definition decoder.
func (r *RAML) filterFragmentUses(node *yaml.Node, location string) (*yaml.Node, *orderedmap.OrderedMap[string, *LibraryLink], error) {
	filteredContent := make([]*yaml.Node, 0, len(node.Content))
	uses := orderedmap.New[string, *LibraryLink](0)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		if keyNode.Value == FacetUses {
			u, err := r.unmarshalUses(valueNode, location)
			if err != nil {
				return nil, nil, StacktraceNewWrapped("parse uses", err, location, WithNodePosition(valueNode))
			}
			uses = u
		} else {
			filteredContent = append(filteredContent, keyNode, valueNode)
		}
	}
	filtered := *node
	filtered.Content = filteredContent
	return &filtered, uses, nil
}

func (r *RAML) unmarshalUses(valueNode *yaml.Node, location string) (*orderedmap.OrderedMap[string, *LibraryLink], error) {
	if valueNode.Tag == TagNull {
		return orderedmap.New[string, *LibraryLink](), nil
	} else if valueNode.Kind != yaml.MappingNode {
		return nil, StacktraceNew("uses must be a map", location, WithNodePosition(valueNode))
	}

	uses := orderedmap.New[string, *LibraryLink](len(valueNode.Content) / 2)
	for j := 0; j != len(valueNode.Content); j += 2 {
		keyNode := valueNode.Content[j]
		name := keyNode.Value
		// Check for duplicate library name.
		if _, exists := uses.Get(name); exists {
			return nil, StacktraceNew("duplicate library name", location, WithNodePosition(keyNode), stacktrace.WithInfo("library", name))
		}
		path := valueNode.Content[j+1]
		uses.Set(name, &LibraryLink{
			ID:       r.generateSequenceID(),
			Value:    path.Value,
			Location: location,
			KeyPos:   NewNodePosition(keyNode),
			ValuePos: NewNodePosition(path),
		})
	}
	return uses, nil
}

func (r *RAML) unmarshalTypes(valueNode *yaml.Node, location string, isAnnotationType bool) (*orderedmap.OrderedMap[string, *BaseShape], error) {
	if valueNode.Tag == TagNull {
		return orderedmap.New[string, *BaseShape](), nil
	} else if valueNode.Kind != yaml.MappingNode {
		return nil, StacktraceNew("types must be a map", location, WithNodePosition(valueNode))
	}

	types := orderedmap.New[string, *BaseShape](len(valueNode.Content) / 2)
	for j := 0; j != len(valueNode.Content); j += 2 {
		keyNode := valueNode.Content[j]
		name := keyNode.Value
		// Check for duplicate type name.
		if _, exists := types.Get(name); exists {
			return nil, StacktraceNew("duplicate type name", location, WithNodePosition(keyNode), stacktrace.WithInfo("type", name))
		}
		valueNode := valueNode.Content[j+1]
		// Check if the type name is a built-in type
		if _, ok := SetOfBuiltInTypes[name]; ok {
			return nil, StacktraceNew("cannot redefine built-in type", location, WithNodePosition(keyNode), stacktrace.WithInfo("type", name))
		}
		shape, err := r.makeNewShapeYAML(keyNode, valueNode, location)
		if err != nil {
			return nil, StacktraceNewWrapped("unmarshal types: make shape", err, location, WithNodePosition(keyNode))
		}
		types.Set(name, shape)
		if isAnnotationType {
			shape.IsAnnotationType = true
			r.PutAnnotationTypeIntoFragment(name, location, shape)
		} else {
			r.PutTypeIntoFragment(name, location, shape)
		}
		r.PutTypeDefinitionIntoFragment(location, shape)
	}
	return types, nil
}

// Library is the RAML 1.0 Library
type Library struct {
	ID    int64
	Usage *ScalarFacet[string]

	AnnotationTypes *orderedmap.OrderedMap[string, *BaseShape]
	ResourceTypes   *orderedmap.OrderedMap[string, *ResourceTypeDefinition]
	Types           *orderedmap.OrderedMap[string, *BaseShape]
	Uses            *orderedmap.OrderedMap[string, *LibraryLink]
	Traits          *orderedmap.OrderedMap[string, *TraitDefinition]
	SecuritySchemes *orderedmap.OrderedMap[string, *SecuritySchemeDefinition]

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	raml     *RAML
}

// GetReferenceType returns a reference type by name, implementing the ReferenceResolver interface.
func (l *Library) GetReferenceType(refName string) (*BaseShape, error) {
	return resolveReference(l.Types, l.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

func (l *Library) GetSecuritySchemeDefinition(refName string) (*SecuritySchemeDefinition, error) {
	return resolveReference(l.SecuritySchemes, l.Uses, refName, func(lib *Library, suffix string) (*SecuritySchemeDefinition, bool) {
		return lib.SecuritySchemes.Get(suffix)
	})
}

// GetReferenceAnnotationType returns a reference annotation type by name,
// implementing the ReferenceResolver interface.
// Falls back to regular Types when the name is not found in AnnotationTypes,
// because annotation types share type-declaration syntax with regular types
// (RAML 1.0 §"Annotation Types").
func (l *Library) GetReferenceAnnotationType(refName string) (*BaseShape, error) {
	if ref, err := resolveReference(l.AnnotationTypes, l.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.AnnotationTypes.Get(suffix)
	}); err == nil {
		return ref, nil
	}
	// Annotation types may reference regular types from the same namespace.
	return resolveReference(l.Types, l.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

func (l *Library) GetTraitDefinition(refName string) (*TraitDefinition, error) {
	return resolveReference(l.Traits, l.Uses, refName, func(lib *Library, suffix string) (*TraitDefinition, bool) {
		return lib.Traits.Get(suffix)
	})
}

func (l *Library) GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error) {
	return resolveReference(l.ResourceTypes, l.Uses, refName, func(lib *Library, suffix string) (*ResourceTypeDefinition, bool) {
		return lib.ResourceTypes.Get(suffix)
	})
}

func (l *Library) GetLocation() string {
	return l.Location
}

type LibraryLink struct {
	ID    int64
	Value string

	Link *Library

	Location string
	KeyPos   stacktrace.Position // position of the key node (the alias)
	ValuePos stacktrace.Position // position of the value node (the path string)
}

// UnmarshalYAML unmarshals a Library from a yaml.Node, implementing the yaml.Unmarshaler interface
func (l *Library) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return StacktraceNew("must be map", l.Location, WithNodePosition(value))
	}

	hasTypes := false
	hasSchemas := false
	for i := 0; i != len(value.Content); i += 2 {
		node := value.Content[i]
		valueNode := value.Content[i+1]
		switch node.Value {
		case FacetUses:
			uses, err := l.raml.unmarshalUses(valueNode, l.Location)
			if err != nil {
				return StacktraceNewWrapped("parse uses", err, l.Location, WithNodePosition(valueNode))
			}
			l.Uses = uses
		case FacetTypes:
			if hasSchemas {
				return StacktraceNew("types and schemas are mutually exclusive", l.Location, WithNodePosition(valueNode))
			}
			hasTypes = true
			types, err := l.raml.unmarshalTypes(valueNode, l.Location, false)
			if err != nil {
				return StacktraceNewWrapped("parse types", err, l.Location, WithNodePosition(valueNode))
			}
			l.Types = types
		case FacetAnnotationTypes:
			types, err := l.raml.unmarshalTypes(valueNode, l.Location, true)
			if err != nil {
				return StacktraceNewWrapped("parse annotation types", err, l.Location, WithNodePosition(valueNode))
			}
			l.AnnotationTypes = types
		case FacetSecuritySchemes:
			securitySchemeDefs, err := l.raml.unmarshalSecuritySchemes(valueNode, l.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal security scheme definitions", err, l.Location, WithNodePosition(valueNode))
			}
			l.SecuritySchemes = securitySchemeDefs
		case FacetResourceTypes:
			rtDefs, err := l.raml.unmarshalResourceTypeDefinitions(valueNode, l.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal resource type definitions", err, l.Location, WithNodePosition(valueNode))
			}
			if rtDefs != nil {
				l.ResourceTypes = rtDefs
			}
		case FacetTraits:
			traitDefs, err := l.raml.unmarshalTraitDefinitions(valueNode, l.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal trait definitions", err, l.Location, WithNodePosition(valueNode))
			}
			l.Traits = traitDefs
		case FacetSchemas:
			// "schemas" is a deprecated alias for "types" (RAML 1.0 backward compat)
			if hasTypes {
				return StacktraceNew("schemas and types are mutually exclusive", l.Location, WithNodePosition(valueNode))
			}
			hasSchemas = true
			types, err := l.raml.unmarshalTypes(valueNode, l.Location, false)
			if err != nil {
				return StacktraceNewWrapped("parse schemas (types)", err, l.Location, WithNodePosition(valueNode))
			}
			l.Types = types
		case FacetUsage:
			sn, err := MakeScalarFacetYAML[string](l.raml, node, valueNode, l.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, l.Location, WithNodePosition(valueNode))
			}
			l.Usage = sn
		default:
			if IsCustomDomainExtensionNode(node.Value) {
				de, err := l.raml.unmarshalCustomDomainExtension(l.Location, node, valueNode)
				if err != nil {
					return StacktraceNewWrapped("unmarshal custom domain extension", err, l.Location,
						WithNodePosition(valueNode))
				}
				l.CustomDomainProperties.Set(de.Name, de)
			} else {
				return StacktraceNew("unknown field", l.Location, WithNodePosition(node), stacktrace.WithInfo("field", node.Value))
			}
		}
	}

	return nil
}

func (r *RAML) MakeLibrary(path string) *Library {
	return &Library{
		ID:                     r.generateSequenceID(),
		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
		Uses:                   orderedmap.New[string, *LibraryLink](0),
		Types:                  orderedmap.New[string, *BaseShape](0),
		AnnotationTypes:        orderedmap.New[string, *BaseShape](0),
		Traits:                 orderedmap.New[string, *TraitDefinition](0),
		SecuritySchemes:        orderedmap.New[string, *SecuritySchemeDefinition](0),
		ResourceTypes:          orderedmap.New[string, *ResourceTypeDefinition](0),

		Location: path,
		raml:     r,
	}
}

// DataTypeFragment is the RAML 1.0 DataType
type DataTypeFragment struct {
	ID int64

	Uses *orderedmap.OrderedMap[string, *LibraryLink]

	Shape *BaseShape

	Location string
	raml     *RAML
}

// GetReferenceType returns a reference type by name, implementing the ReferenceResolver interface.
func (dt *DataTypeFragment) GetReferenceType(refName string) (*BaseShape, error) {
	return resolveLibraryReference(dt.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetReferenceAnnotationType returns a reference annotation type by name,
// implementing the ReferenceResolver interface. Falls back to the library's
// regular Types when the name is not found in AnnotationTypes.
func (dt *DataTypeFragment) GetReferenceAnnotationType(refName string) (*BaseShape, error) {
	if ref, err := resolveLibraryReference(dt.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.AnnotationTypes.Get(suffix)
	}); err == nil {
		return ref, nil
	}
	return resolveLibraryReference(dt.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetResourceTypeDefinition returns a resource type definition by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (dt *DataTypeFragment) GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error) {
	return resolveLibraryReference(dt.Uses, refName, func(lib *Library, suffix string) (*ResourceTypeDefinition, bool) {
		return lib.ResourceTypes.Get(suffix)
	})
}

// GetTraitDefinition returns a trait definition by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (dt *DataTypeFragment) GetTraitDefinition(refName string) (*TraitDefinition, error) {
	return resolveLibraryReference(dt.Uses, refName, func(lib *Library, suffix string) (*TraitDefinition, bool) {
		return lib.Traits.Get(suffix)
	})
}

func (dt *DataTypeFragment) GetLocation() string {
	return dt.Location
}

func (dt *DataTypeFragment) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return StacktraceNew("must be map", dt.Location, WithNodePosition(value))
	}
	filtered, uses, err := dt.raml.filterFragmentUses(value, dt.Location)
	if err != nil {
		return StacktraceNewWrapped("filter fragment uses", err, dt.Location)
	}
	dt.Uses = uses
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: uriBase(dt.Location), Tag: "!!str"}
	shape, err := dt.raml.makeNewShapeYAML(keyNode, filtered, dt.Location)
	if err != nil {
		return StacktraceNewWrapped("parse types: make shape", err, dt.Location, WithNodePosition(keyNode))
	}
	dt.Shape = shape
	dt.raml.PutTypeDefinitionIntoFragment(dt.Location, shape)
	return nil
}

func (r *RAML) MakeDataTypeFragment(path string) *DataTypeFragment {
	return &DataTypeFragment{
		ID:       r.generateSequenceID(),
		Uses:     orderedmap.New[string, *LibraryLink](0),
		Location: path,
		raml:     r,
	}
}

func (r *RAML) MakeJSONDataType(value []byte, path string) (*DataTypeFragment, error) {
	dt := r.MakeDataTypeFragment(path)
	// Convert to yaml node to reuse the same data node creation interface
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{
				Kind:  yaml.ScalarNode,
				Value: "type",
				Tag:   "!!str",
			},
			{
				Kind:  yaml.ScalarNode,
				Value: string(value),
				Tag:   "!!str",
			},
		},
	}
	if err := node.Decode(&dt); err != nil {
		return nil, StacktraceNewWrapped("decode fragment", err, path)
	}
	return dt, nil
}

// NamedExample is the RAML 1.0 NamedExample
type NamedExample struct {
	// FIXME: NamedExampleFragment should follow the same pattern as other generic fragments.
	ID int64

	Uses *orderedmap.OrderedMap[string, *LibraryLink]

	Map *orderedmap.OrderedMap[string, *Example]

	Location string
	raml     *RAML
}

func (ne *NamedExample) GetLocation() string {
	return ne.Location
}

// GetReferenceType returns a reference type by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (ne *NamedExample) GetReferenceType(refName string) (*BaseShape, error) {
	return resolveLibraryReference(ne.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetReferenceAnnotationType returns a reference annotation type by name via this fragment's uses,
// implementing the ReferenceResolver interface. Falls back to the library's regular Types.
func (ne *NamedExample) GetReferenceAnnotationType(refName string) (*BaseShape, error) {
	if ref, err := resolveLibraryReference(ne.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.AnnotationTypes.Get(suffix)
	}); err == nil {
		return ref, nil
	}
	return resolveLibraryReference(ne.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetTraitDefinition returns a trait definition by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (ne *NamedExample) GetTraitDefinition(refName string) (*TraitDefinition, error) {
	return resolveLibraryReference(ne.Uses, refName, func(lib *Library, suffix string) (*TraitDefinition, bool) {
		return lib.Traits.Get(suffix)
	})
}

// GetResourceTypeDefinition returns a resource type definition by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (ne *NamedExample) GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error) {
	return resolveLibraryReference(ne.Uses, refName, func(lib *Library, suffix string) (*ResourceTypeDefinition, bool) {
		return lib.ResourceTypes.Get(suffix)
	})
}

func (r *RAML) MakeNamedExample(path string) *NamedExample {
	return &NamedExample{
		ID:       r.generateSequenceID(),
		Location: path,
		raml:     r,
	}
}

func (ne *NamedExample) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return StacktraceNew("must be map", ne.Location, WithNodePosition(value))
	}
	filtered, uses, err := ne.raml.filterFragmentUses(value, ne.Location)
	if err != nil {
		return StacktraceNewWrapped("filter fragment uses", err, ne.Location)
	}
	ne.Uses = uses
	examples := orderedmap.New[string, *Example](len(filtered.Content) / 2)
	for i := 0; i != len(filtered.Content); i += 2 {
		node := filtered.Content[i]
		valueNode := filtered.Content[i+1]
		example, err := ne.raml.makeExample(valueNode, node.Value, ne.Location)
		if err != nil {
			return StacktraceNewWrapped("make example", err, ne.Location, WithNodePosition(valueNode))
		}
		examples.Set(node.Value, example)
	}
	ne.Map = examples

	return nil
}

///////////////////////////////////////////

type ResourceTypeFragment struct {
	ID int64

	Uses *orderedmap.OrderedMap[string, *LibraryLink]

	ResourceType *ResourceTypeDefinition

	Location string
	raml     *RAML
}

func (r *RAML) MakeResourceTypeFragment(path string) *ResourceTypeFragment {
	return &ResourceTypeFragment{
		ID:       r.generateSequenceID(),
		Uses:     orderedmap.New[string, *LibraryLink](0),
		Location: path,
		raml:     r,
	}
}

func (rt *ResourceTypeFragment) GetLocation() string {
	return rt.Location
}

// GetReferenceType returns a reference type by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (rt *ResourceTypeFragment) GetReferenceType(refName string) (*BaseShape, error) {
	return resolveLibraryReference(rt.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetReferenceAnnotationType returns a reference annotation type by name via this fragment's uses,
// implementing the ReferenceResolver interface. Falls back to the library's regular Types.
func (rt *ResourceTypeFragment) GetReferenceAnnotationType(refName string) (*BaseShape, error) {
	if ref, err := resolveLibraryReference(rt.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.AnnotationTypes.Get(suffix)
	}); err == nil {
		return ref, nil
	}
	return resolveLibraryReference(rt.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetTraitDefinition resolves a dotted trait reference via this fragment's uses: map,
// implementing the ReferenceResolver interface.
func (rt *ResourceTypeFragment) GetTraitDefinition(refName string) (*TraitDefinition, error) {
	return resolveLibraryReference(rt.Uses, refName, func(lib *Library, suffix string) (*TraitDefinition, bool) {
		return lib.Traits.Get(suffix)
	})
}

// GetResourceTypeDefinition returns a resource type definition by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (rt *ResourceTypeFragment) GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error) {
	return resolveLibraryReference(rt.Uses, refName, func(lib *Library, suffix string) (*ResourceTypeDefinition, bool) {
		return lib.ResourceTypes.Get(suffix)
	})
}

func (rt *ResourceTypeFragment) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return StacktraceNew("resource type fragment must be a map", rt.Location, WithNodePosition(node))
	}
	filtered, uses, err := rt.raml.filterFragmentUses(node, rt.Location)
	if err != nil {
		return StacktraceNewWrapped("filter fragment uses", err, rt.Location)
	}
	rt.Uses = uses
	rtDef, err := rt.raml.makeResourceTypeDefinition(nil, filtered, rt.Location)
	if err != nil {
		return StacktraceNewWrapped("make resource type definition", err, rt.Location, WithNodePosition(node))
	}
	rt.ResourceType = rtDef
	return nil
}

type TraitFragment struct {
	ID int64

	Uses *orderedmap.OrderedMap[string, *LibraryLink]

	Trait *TraitDefinition

	Location string
	raml     *RAML
}

func (t *TraitFragment) GetLocation() string {
	return t.Location
}

// GetReferenceType returns a reference type by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (t *TraitFragment) GetReferenceType(refName string) (*BaseShape, error) {
	return resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetReferenceAnnotationType returns a reference annotation type by name via this fragment's uses,
// implementing the ReferenceResolver interface. Falls back to the library's regular Types.
func (t *TraitFragment) GetReferenceAnnotationType(refName string) (*BaseShape, error) {
	if ref, err := resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.AnnotationTypes.Get(suffix)
	}); err == nil {
		return ref, nil
	}
	return resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetTraitDefinition resolves a dotted trait reference via this fragment's uses: map,
// implementing the ReferenceResolver interface.
func (t *TraitFragment) GetTraitDefinition(refName string) (*TraitDefinition, error) {
	return resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*TraitDefinition, bool) {
		return lib.Traits.Get(suffix)
	})
}

// GetResourceTypeDefinition returns a resource type definition by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (t *TraitFragment) GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error) {
	return resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*ResourceTypeDefinition, bool) {
		return lib.ResourceTypes.Get(suffix)
	})
}

func (t *TraitFragment) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return StacktraceNew("trait fragment must be a map", t.Location, WithNodePosition(node))
	}
	filtered, uses, err := t.raml.filterFragmentUses(node, t.Location)
	if err != nil {
		return StacktraceNewWrapped("filter fragment uses", err, t.Location)
	}
	t.Uses = uses
	traitDef, err := t.raml.makeTraitDefinition(nil, filtered, t.Location)
	if err != nil {
		return StacktraceNewWrapped("make trait definition", err, t.Location, WithNodePosition(node))
	}
	t.Trait = traitDef
	return nil
}

func (r *RAML) MakeTraitFragment(path string) *TraitFragment {
	return &TraitFragment{
		ID:       r.generateSequenceID(),
		Uses:     orderedmap.New[string, *LibraryLink](0),
		Location: path,
		raml:     r,
	}
}

type SecuritySchemeFragment struct {
	ID int64

	Uses *orderedmap.OrderedMap[string, *LibraryLink]

	SecurityScheme *SecuritySchemeDefinition

	Location string
	raml     *RAML
}

func (r *RAML) MakeSecuritySchemeFragment(path string) *SecuritySchemeFragment {
	return &SecuritySchemeFragment{
		ID:       r.generateSequenceID(),
		Uses:     orderedmap.New[string, *LibraryLink](0),
		Location: path,
		raml:     r,
	}
}

func (t *SecuritySchemeFragment) GetLocation() string {
	return t.Location
}

// GetReferenceType returns a reference type by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (t *SecuritySchemeFragment) GetReferenceType(refName string) (*BaseShape, error) {
	return resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetReferenceAnnotationType returns a reference annotation type by name via this fragment's uses,
// implementing the ReferenceResolver interface. Falls back to the library's regular Types.
func (t *SecuritySchemeFragment) GetReferenceAnnotationType(refName string) (*BaseShape, error) {
	if ref, err := resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.AnnotationTypes.Get(suffix)
	}); err == nil {
		return ref, nil
	}
	return resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetTraitDefinition returns a trait definition by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (t *SecuritySchemeFragment) GetTraitDefinition(refName string) (*TraitDefinition, error) {
	return resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*TraitDefinition, bool) {
		return lib.Traits.Get(suffix)
	})
}

// GetResourceTypeDefinition returns a resource type definition by name via this fragment's uses,
// implementing the ReferenceResolver interface.
func (t *SecuritySchemeFragment) GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error) {
	return resolveLibraryReference(t.Uses, refName, func(lib *Library, suffix string) (*ResourceTypeDefinition, bool) {
		return lib.ResourceTypes.Get(suffix)
	})
}

func (t *SecuritySchemeFragment) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return StacktraceNew("security scheme fragment must be a map", t.Location, WithNodePosition(node))
	}
	filtered, uses, err := t.raml.filterFragmentUses(node, t.Location)
	if err != nil {
		return StacktraceNewWrapped("filter fragment uses", err, t.Location)
	}
	t.Uses = uses
	securitySchemeDef, err := t.raml.makeSecuritySchemeDefinition(nil, filtered, t.Location)
	if err != nil {
		return StacktraceNewWrapped("make security scheme definition", err, t.Location, WithNodePosition(filtered))
	}
	t.SecurityScheme = securitySchemeDef
	return nil
}

type DocumentationItemFragment struct {
	ID int64

	Uses *orderedmap.OrderedMap[string, *LibraryLink]

	DocumentationItem *DocumentationItem

	Location string
	raml     *RAML
}

func (r *RAML) MakeDocumentationItemFragment(path string) *DocumentationItemFragment {
	return &DocumentationItemFragment{
		ID: r.generateSequenceID(),

		Uses: orderedmap.New[string, *LibraryLink](0),

		Location: path,
		raml:     r,
	}
}

type APIFragment struct {
	ID int64

	Title       *ScalarFacet[string]
	Description *ScalarFacet[string]
	Version     *ScalarFacet[string]

	Documentation []*DocumentationItem

	// NOTE: We might want to keep forward compatibility with OpenAPI and define multiple servers
	BaseURI           *ScalarFacet[string]
	BaseURIParameters *orderedmap.OrderedMap[string, *BaseShape]
	Protocols         []*Node[string]

	MediaType []*Node[string]   // Global media types
	SecuredBy []*SecurityScheme // Global security schemes

	AnnotationTypes *orderedmap.OrderedMap[string, *BaseShape]
	ResourceTypes   *orderedmap.OrderedMap[string, *ResourceTypeDefinition]
	Types           *orderedmap.OrderedMap[string, *BaseShape]
	Uses            *orderedmap.OrderedMap[string, *LibraryLink]
	Traits          *orderedmap.OrderedMap[string, *TraitDefinition]
	SecuritySchemes *orderedmap.OrderedMap[string, *SecuritySchemeDefinition]

	EndPoints *orderedmap.OrderedMap[string, *EndPoint]

	// sourceEndPoints holds the stage-1 IR of the API's top-level endpoints,
	// produced during decode and consumed by buildEndPoints (resolve directives,
	// merge resource types/traits, materialize) before resolveShapes.
	sourceEndPoints []*SourceEndPoint

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	raml     *RAML
}

func (r *RAML) MakeAPIFragment(path string) *APIFragment {
	return &APIFragment{
		ID:                     r.generateSequenceID(),
		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
		Uses:                   orderedmap.New[string, *LibraryLink](0),
		Types:                  orderedmap.New[string, *BaseShape](0),
		ResourceTypes:          orderedmap.New[string, *ResourceTypeDefinition](0),
		Traits:                 orderedmap.New[string, *TraitDefinition](0),
		SecuritySchemes:        orderedmap.New[string, *SecuritySchemeDefinition](0),
		AnnotationTypes:        orderedmap.New[string, *BaseShape](0),
		EndPoints:              orderedmap.New[string, *EndPoint](0),
		Location:               path,
		raml:                   r,
	}
}

func (api *APIFragment) GetLocation() string {
	return api.Location
}

// GetReferenceType returns a reference type by name, implementing the ReferenceResolver interface.
func (api *APIFragment) GetReferenceType(refName string) (*BaseShape, error) {
	return resolveReference(api.Types, api.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

// GetReferenceAnnotationType returns a reference annotation type by name,
// implementing the ReferenceResolver interface. Falls back to regular Types.
func (api *APIFragment) GetReferenceAnnotationType(refName string) (*BaseShape, error) {
	if ref, err := resolveReference(api.AnnotationTypes, api.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.AnnotationTypes.Get(suffix)
	}); err == nil {
		return ref, nil
	}
	// Annotation types may reference regular types from the same namespace.
	return resolveReference(api.Types, api.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

func (api *APIFragment) GetTraitDefinition(refName string) (*TraitDefinition, error) {
	return resolveReference(api.Traits, api.Uses, refName, func(lib *Library, suffix string) (*TraitDefinition, bool) {
		return lib.Traits.Get(suffix)
	})
}

func (api *APIFragment) GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error) {
	return resolveReference(api.ResourceTypes, api.Uses, refName, func(lib *Library, suffix string) (*ResourceTypeDefinition, bool) {
		return lib.ResourceTypes.Get(suffix)
	})
}

func (api *APIFragment) GetSecuritySchemeDefinition(refName string) (*SecuritySchemeDefinition, error) {
	return resolveReference(api.SecuritySchemes, api.Uses, refName, func(lib *Library, suffix string) (*SecuritySchemeDefinition, bool) {
		return lib.SecuritySchemes.Get(suffix)
	})
}

func (api *APIFragment) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return StacktraceNew("must be map", api.Location, WithNodePosition(node))
	}

	filtered, err := api.preProcess(node)
	if err != nil {
		return StacktraceNewWrapped("preprocess", err, api.Location)
	}

	hasTitle := false
	hasTypes := false
	hasSchemas := false
	for i := 0; i != len(filtered); i += 2 {
		keyNode := filtered[i]
		valueNode := filtered[i+1]
		switch keyNode.Value {
		case FacetTitle:
			sn, err := MakeScalarFacetYAML[string](api.raml, keyNode, valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, api.Location, WithNodePosition(valueNode))
			}
			if sn.Value == "" {
				return StacktraceNew("title must not be empty", api.Location, WithNodePosition(keyNode))
			}
			hasTitle = true
			api.Title = sn
		case FacetDescription:
			sn, err := MakeScalarFacetYAML[string](api.raml, keyNode, valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, api.Location, WithNodePosition(valueNode))
			}
			api.Description = sn
		case FacetVersion:
			sn, err := MakeScalarFacetYAML[string](api.raml, keyNode, valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, api.Location, WithNodePosition(valueNode))
			}
			api.Version = sn
		case FacetBaseUri:
			sn, err := MakeScalarFacetYAML[string](api.raml, keyNode, valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, api.Location, WithNodePosition(valueNode))
			}
			api.BaseURI = sn
		case FacetBaseUriParameters:
			if err := api.unmarshalBaseURIParameters(valueNode); err != nil {
				return StacktraceNewWrapped("unmarshal base uri parameters", err, api.Location, WithNodePosition(valueNode))
			}
		case FacetDocumentation:
			documentationItems, err := api.raml.unmarshalDocumentationItems(keyNode, valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("parse documentation items", err, api.Location, WithNodePosition(keyNode))
			}
			api.Documentation = documentationItems
		case FacetTypes:
			if hasSchemas {
				return StacktraceNew("types and schemas are mutually exclusive", api.Location, WithNodePosition(valueNode))
			}
			hasTypes = true
			types, err := api.raml.unmarshalTypes(valueNode, api.Location, false)
			if err != nil {
				return StacktraceNewWrapped("parse types", err, api.Location, WithNodePosition(valueNode))
			}
			api.Types = types
		case FacetAnnotationTypes:
			types, err := api.raml.unmarshalTypes(valueNode, api.Location, true)
			if err != nil {
				return StacktraceNewWrapped("parse annotation types", err, api.Location, WithNodePosition(valueNode))
			}
			api.AnnotationTypes = types
		case FacetSecuritySchemes:
			securitySchemeDefs, err := api.raml.unmarshalSecuritySchemes(valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal security scheme definitions", err, api.Location, WithNodePosition(valueNode))
			}
			api.SecuritySchemes = securitySchemeDefs
		case FacetUses:
			uses, err := api.raml.unmarshalUses(valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("parse uses", err, api.Location, WithNodePosition(valueNode))
			}
			api.Uses = uses
		case FacetResourceTypes:
			rtDefs, err := api.raml.unmarshalResourceTypeDefinitions(valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal resource type definitions", err, api.Location, WithNodePosition(valueNode))
			}
			if rtDefs != nil {
				api.ResourceTypes = rtDefs
			}
		case FacetSchemas:
			// "schemas" is a deprecated alias for "types" (RAML 1.0 backward compat)
			if hasTypes {
				return StacktraceNew("schemas and types are mutually exclusive", api.Location, WithNodePosition(valueNode))
			}
			hasSchemas = true
			types, err := api.raml.unmarshalTypes(valueNode, api.Location, false)
			if err != nil {
				return StacktraceNewWrapped("parse schemas (types)", err, api.Location, WithNodePosition(valueNode))
			}
			api.Types = types
		case FacetTraits:
			traitDefs, err := api.raml.unmarshalTraitDefinitions(valueNode, api.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal trait definitions", err, api.Location, WithNodePosition(valueNode))
			}
			api.Traits = traitDefs
		default:
			switch {
			case IsCustomDomainExtensionNode(keyNode.Value):
				de, err := api.raml.unmarshalCustomDomainExtension(api.Location, keyNode, valueNode)
				if err != nil {
					return StacktraceNewWrapped("unmarshal custom domain extension", err, api.Location, WithNodePosition(valueNode))
				}
				api.CustomDomainProperties.Set(de.Name, de)
			case IsEndPoint(keyNode.Value):
				sep, err := api.raml.makeSourceEndPoint(keyNode, valueNode, api.Location, "")
				if err != nil {
					return StacktraceNewWrapped("make endpoint", err, api.Location, WithNodePosition(valueNode))
				}
				api.sourceEndPoints = append(api.sourceEndPoints, sep)
			default:
				return StacktraceNew("unknown field", api.Location, WithNodePosition(keyNode), stacktrace.WithInfo("field", keyNode.Value))
			}
		}
	}

	// Validate that title is present
	if !hasTitle {
		return StacktraceNew("title is required", api.Location, WithNodePosition(node))
	}

	// Synthesize and validate base URI template parameters after both baseUri
	// and baseUriParameters have been fully parsed.
	if err := api.validateBaseURIParameters(); err != nil {
		return err
	}

	return nil
}

// validateBaseURIParameters parses the baseUri template once, synthesizes a
// minimal string BaseShape for any template variable that lacks an explicit
// baseUriParameters declaration, then cross-checks all declared parameters.
func (api *APIFragment) validateBaseURIParameters() error {
	if api.BaseURI == nil {
		return nil
	}
	params, err := validateAndSynthesizeURIParameters(api.BaseURI.Value, api.BaseURIParameters, api.Location, api.BaseURI.ValuePos)
	if err != nil {
		return StacktraceNewWrapped("parse base uri", err, api.Location,
			stacktrace.WithPosition(&api.BaseURI.ValuePos))
	}
	api.BaseURIParameters = params
	return nil
}

func (api *APIFragment) preProcess(node *yaml.Node) ([]*yaml.Node, error) {
	// NOTE: Preprocessing step ensures that we have global metadata set for this API.
	// This metadata will be used when specifying media types, security and protocols for the requests and responses.
	// Filtered array stores YAML key-value pairs that are not global metadata.
	var filtered []*yaml.Node
	for i := 0; i != len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		switch keyNode.Value {
		case FacetProtocols:
			if err := api.unmarshalProtocols(valueNode); err != nil {
				return nil, StacktraceNewWrapped("unmarshal protocols", err, api.Location, WithNodePosition(valueNode))
			}
			api.raml.globalProtocols = nodeStringValues(api.Protocols)
		case FacetMediaType:
			if err := api.unmarshalMediaType(keyNode, valueNode); err != nil {
				return nil, StacktraceNewWrapped("unmarshal media type", err, api.Location, WithNodePosition(valueNode))
			}
			api.raml.globalMediaType = nodeStringValues(api.MediaType)
		case FacetSecuredBy:
			securitySchemes, err := api.raml.makeSecuritySchemes(valueNode, api.Location)
			if err != nil {
				return nil, StacktraceNewWrapped("make security schemes", err, api.Location, WithNodePosition(valueNode))
			}
			api.SecuredBy = securitySchemes
			api.raml.globalSecuredBy = securitySchemes
		default:
			filtered = append(filtered, keyNode, valueNode)
		}
	}
	return filtered, nil
}

func (r *RAML) unmarshalTraitDefinitions(node *yaml.Node, location string) (*orderedmap.OrderedMap[string, *TraitDefinition], error) {
	if node.Tag == TagNull {
		return nil, nil
	} else if node.Kind != yaml.MappingNode {
		return nil, StacktraceNew("traits must be a mapping node", location, WithNodePosition(node))
	}

	traitDefs := orderedmap.New[string, *TraitDefinition](len(node.Content) / 2)
	for j := 0; j != len(node.Content); j += 2 {
		keyNode := node.Content[j]
		data := node.Content[j+1]

		traitDef, err := r.makeTraitDefinition(keyNode, data, location)
		if err != nil {
			return nil, StacktraceNewWrapped("make traits", err, location, WithNodePosition(data))
		}
		traitDefs.Set(keyNode.Value, traitDef)
	}
	return traitDefs, nil
}

func (r *RAML) unmarshalSecuritySchemes(node *yaml.Node, location string) (*orderedmap.OrderedMap[string, *SecuritySchemeDefinition], error) {
	if node.Tag == TagNull {
		return nil, nil
	} else if node.Kind != yaml.MappingNode {
		return nil, StacktraceNew("endpoint must be a mapping node", location, WithNodePosition(node))
	}

	securitySchemeDefs := orderedmap.New[string, *SecuritySchemeDefinition](len(node.Content) / 2)
	for j := 0; j != len(node.Content); j += 2 {
		keyNode := node.Content[j]
		data := node.Content[j+1]

		securityScheme, err := r.makeSecuritySchemeDefinition(keyNode, data, location)
		if err != nil {
			return nil, StacktraceNewWrapped("make security scheme definition", err, location, WithNodePosition(data))
		}
		securitySchemeDefs.Set(keyNode.Value, securityScheme)
	}
	return securitySchemeDefs, nil
}

func (api *APIFragment) unmarshalMediaType(keyNode, node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		fragmentPath, rn, err := api.raml.resolveInclude(node, api.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve include", err, api.Location, WithNodePosition(node))
		}
		if rn.Tag != TagStr {
			return StacktraceNew("media type must be a string", api.Location, WithNodePosition(keyNode))
		}
		m := MakeNode(rn.Value, keyNode, node, api.Location, fragmentPath)
		if !isValidMediaType(m.Value) {
			return StacktraceNew("media type: invalid media type "+m.Value, api.Location, WithNodePosition(node))
		}
		api.MediaType = []*Node[string]{m}
	} else if node.Kind == yaml.SequenceNode {
		mediaType := make([]*Node[string], len(node.Content))
		for i, v := range node.Content {
			fragmentPath, ri, err := api.raml.resolveInclude(v, api.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve include", err, api.Location, WithNodePosition(v))
			}
			var val string
			if err := ri.Decode(&val); err != nil {
				return StacktraceNewWrapped("parse media type item", err, api.Location, WithNodePosition(v))
			}
			m := MakeSeqNode(val, v, api.Location, fragmentPath)
			if !isValidMediaType(m.Value) {
				return StacktraceNew("media type: invalid media type "+m.Value, api.Location, WithNodePosition(v))
			}
			mediaType[i] = m
		}
		api.MediaType = mediaType
	} else {
		return StacktraceNew("media type must be a string or sequence", api.Location, WithNodePosition(node))
	}
	if len(api.MediaType) == 0 {
		return StacktraceNew("media type must not be empty", api.Location, WithNodePosition(node))
	}
	return nil
}

func (api *APIFragment) unmarshalProtocols(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return StacktraceNew("protocols must be an array", api.Location, WithNodePosition(node))
	}
	protocols := make([]*Node[string], len(node.Content))
	for i, item := range node.Content {
		fragmentPath, ri, err := api.raml.resolveInclude(item, api.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve include", err, api.Location, WithNodePosition(item))
		}
		var val string
		if err := ri.Decode(&val); err != nil {
			return StacktraceNewWrapped("parse protocol item", err, api.Location, WithNodePosition(item))
		}
		if !isValidProtocol(val) {
			return StacktraceNew("unknown protocol", api.Location, WithNodePosition(item))
		}
		protocols[i] = MakeSeqNode(val, item, api.Location, fragmentPath)
	}
	if len(protocols) == 0 {
		return StacktraceNew("protocols must not be empty", api.Location, WithNodePosition(node))
	}
	api.Protocols = protocols
	return nil
}

func (api *APIFragment) unmarshalBaseURIParameters(node *yaml.Node) error {
	if node.Tag == TagNull {
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("baseUriParameters must be a mapping node", api.Location, WithNodePosition(node))
	}

	api.BaseURIParameters = orderedmap.New[string, *BaseShape](len(node.Content) / 2)
	for j := 0; j != len(node.Content); j += 2 {
		keyNode := node.Content[j]
		valueNode := node.Content[j+1]

		shape, err := api.raml.makeNewShapeYAML(keyNode, valueNode, api.Location)
		if err != nil {
			return StacktraceNewWrapped("make new shape yaml", err, api.Location, WithNodePosition(keyNode))
		}
		api.BaseURIParameters.Set(keyNode.Value, shape)
		api.raml.PutTypeDefinitionIntoFragment(api.Location, shape)
	}
	return nil
}
