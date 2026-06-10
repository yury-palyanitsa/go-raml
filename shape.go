package raml

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/santhosh-tekuri/jsonschema/v6"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"

	"github.com/acronis/go-stacktrace"
)

type ShapeVisitor[T any] interface {
	VisitObjectShape(s *ObjectShape) T
	VisitArrayShape(s *ArrayShape) T
	VisitStringShape(s *StringShape) T
	VisitNumberShape(s *NumberShape) T
	VisitIntegerShape(s *IntegerShape) T
	VisitBooleanShape(s *BooleanShape) T
	VisitFileShape(s *FileShape) T
	VisitUnionShape(s *UnionShape) T
	VisitDateTimeShape(s *DateTimeShape) T
	VisitDateTimeOnlyShape(s *DateTimeOnlyShape) T
	VisitDateOnlyShape(s *DateOnlyShape) T
	VisitTimeOnlyShape(s *TimeOnlyShape) T
	VisitRecursiveShape(s *RecursiveShape) T
	VisitJSONShape(s *JSONShape) T
	VisitAnyShape(s *AnyShape) T
	VisitNilShape(s *NilShape) T
}

// type ShapeGetter interface {
// 	Shape() Shape
// }

type ShapeSetter interface {
	SetShape(Shape)
}

// TypeExprRef records a single named-type reference within a type expression,
// alongside the source position (1-based line and column) where that name
// appears in the document. Populated by the RDT visitor; used by LSP tooling
// to provide go-to-definition and hover at the exact reference site.
//
// Exactly one of Resolved, LibraryLink, or BuiltinType is set:
//   - Resolved is set for plain type-name references (e.g. "TestType").
//   - LibraryLink + LibraryAlias are set for the library-prefix part of a
//     qualified reference (e.g. "lib" in "lib.TestType"), allowing navigation
//     to the library file itself.
//   - BuiltinType is set for RAML built-in primitive type names (e.g. "string",
//     "integer"), enabling hover documentation without a definition target.
type TypeExprRef struct {
	Line   int
	Column int

	// Type-name reference (e.g. "TestType" or the "TestType" part of "lib.TestType").
	Resolved *BaseShape

	// Library-prefix reference (e.g. "lib" in "lib.TestType").
	LibraryLink  *LibraryLink
	LibraryAlias string // the prefix as written, e.g. "lib"

	// BuiltinType is the name of a RAML built-in type (e.g. "string", "integer").
	// Set by VisitPrimitive; Resolved and LibraryLink are nil when this is non-empty.
	BuiltinType string
}

type BaseShape struct {
	Shape
	ID          int64
	Name        string
	DisplayName *ScalarFacet[string]
	Description *ScalarFacet[string]
	// TODO: Move Type to underlying Shape
	Type     string
	TypeExpr *Node[string] // raw type expression as written; replaces typeExprLine/typeExprCol
	Example  *Example
	Examples *Examples
	Inherits []*BaseShape
	Alias    *BaseShape
	Default  *DataNode
	Required *ScalarFacet[bool]
	XML      *XMLSerialization
	Enum     Nodes

	// To support !include of DataType fragment
	Link *DataTypeFragment

	// CustomShapeFacets is a map of custom facets with values
	CustomShapeFacets *orderedmap.OrderedMap[string, *DataNode]
	// CustomShapeFacetDefinitions is an object properties share the same syntax with custom shape facets.
	CustomShapeFacetDefinitions *orderedmap.OrderedMap[string, Property]
	// CustomDomainProperties is a map of custom annotations
	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	// TypeExprRefs records each named type referenced in the expression alongside
	// the exact source position where that name was written.
	// Populated by the RDT visitor (VisitReference).
	TypeExprRefs []TypeExprRef

	// Controlled by UnwrapShape
	unwrapped bool
	// NOTE: Not thread safe and should be used only in one method simultaneously.
	ShapeVisited bool
	// IsAnnotationType marks shapes that were declared in an annotationTypes: block.
	// Set during parsing so the RDT resolver can route them through
	// GetReferencedAnnotationType instead of GetReferencedType.
	IsAnnotationType bool
	// anchorFrag is the nearest Library or APIFragment used for unqualified type
	// resolution.  Set from the ParseCtx stack in MakeBaseShape.
	anchorFrag ReferenceResolver

	raml *RAML

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
}

func (s *BaseShape) SetShape(shape Shape) {
	s.Shape = shape
}

// validateAt runs enum checking then delegates to the shape's internal
// validate. All internal callers that need path tracking use this method.
func (s *BaseShape) validateAt(v any, path string) error {
	if s.Enum != nil {
		return s.validateEnum(v)
	}
	return s.Shape.validate(v, path)
}

func (s *BaseShape) Validate(v any) error {
	return s.validateAt(v, "$")
}

func (s *BaseShape) Inherit(sourceBase *BaseShape) (*BaseShape, error) {
	// Avoid recursion caused by inheritance chain
	if sourceBase.ShapeVisited {
		// NOTE: We do not mark any recursions here. External code must handle this case.
		return sourceBase, nil
	}
	sourceBase.ShapeVisited = true
	defer func() { sourceBase.ShapeVisited = false }()

	source := sourceBase.Shape
	target := s.Shape

	if s.Description == nil {
		s.Description = sourceBase.Description
	}

	// Inherit custom shape facets
	if s.CustomShapeFacets == nil {
		s.CustomShapeFacets = sourceBase.CustomShapeFacets
	} else if sourceBase.CustomShapeFacets != nil {
		for pair := sourceBase.CustomShapeFacets.Oldest(); pair != nil; pair = pair.Next() {
			k, sourceNode := pair.Key, pair.Value
			if _, present := s.CustomShapeFacets.Get(k); !present {
				s.CustomShapeFacets.Set(k, sourceNode)
			}
		}
	}

	// Inherit enum
	if s.Enum == nil {
		s.Enum = sourceBase.Enum
	} else if sourceBase.Enum != nil && !isCompatibleEnum(sourceBase.Enum, s.Enum) {
		return nil, StacktraceNew("enum constraint violation", s.Location,
			stacktrace.WithPosition(&s.KeyPos),
			stacktrace.WithInfo("source", sourceBase.Enum.String()),
			stacktrace.WithInfo("target", s.Enum.String()))
	}

	// If source type is any, return target as is
	if _, ok := source.(*AnyShape); ok {
		return s, nil
	}

	sourceUnion, isSourceUnion := source.(*UnionShape)
	targetUnion, isTargetUnion := target.(*UnionShape)

	switch {
	case isSourceUnion && !isTargetUnion:
		return s.inheritUnionSource(sourceUnion)

	case isTargetUnion && !isSourceUnion:
		return s.inheritUnionTarget(targetUnion)
	}
	// Homogenous types produce same type
	_, err := target.inherit(source)
	if err != nil {
		return nil, StacktraceNewWrapped("merge shapes", err, target.Base().Location,
			stacktrace.WithPosition(&target.Base().KeyPos))
	}
	return s, nil
}

func (s *BaseShape) inheritUnionSource(sourceUnion *UnionShape) (*BaseShape, error) {
	var filtered []*BaseShape
	var acc stacktrace.Accumulator
	for _, source := range sourceUnion.AnyOf {
		// If at least one union member has any type, the whole union is considered as any type.
		if _, ok := source.Shape.(*AnyShape); ok {
			return s, nil
		}
		if source.Type == s.Type {
			// Deep copy with ID change is required since we create new union members from source members
			tc := s.CloneDetached()
			tc.ID = s.raml.generateSequenceID()
			is, err := tc.Inherit(source)
			if err != nil {
				acc.Add(StacktraceNewWrapped("merge shapes", err, s.Location,
					stacktrace.WithPosition(&s.KeyPos)))
				// Skip shapes that didn't pass inheritance check
				continue
			}
			filtered = append(filtered, is)
		}
	}
	if len(filtered) == 0 {
		se := StacktraceNew("failed to find compatible union member", s.Location,
			stacktrace.WithPosition(&s.KeyPos))
		if details := acc.Result(); details != nil {
			se = se.Append(details)
		}
		return nil, se
	}
	// If only one union member remains - simplify to target type
	if len(filtered) == 1 {
		return filtered[0], nil
	}
	// Convert target to union
	s.Type = TypeUnion
	s.SetShape(&UnionShape{
		BaseShape: s,
		UnionFacets: UnionFacets{
			AnyOf: filtered,
		},
	})
	return s, nil
}

func (s *BaseShape) inheritUnionTarget(targetUnion *UnionShape) (*BaseShape, error) {
	var acc stacktrace.Accumulator
	for _, item := range targetUnion.AnyOf {
		// Merge will raise an error in case any of union members has incompatible type
		_, err := item.Inherit(s)
		if err != nil {
			acc.Add(StacktraceNewWrapped("merge shapes", err, targetUnion.Base().Location,
				stacktrace.WithPosition(&targetUnion.Base().KeyPos)))
			continue
		}
	}
	if result := acc.Result(); result != nil {
		return nil, result
	}
	return targetUnion.Base(), nil
}

// checkInheritType performs the type assertion check common to all inherit() methods.
func checkInheritType[T Shape](target Shape, source Shape) (T, error) {
	ss, ok := source.(T)
	if !ok {
		var zero T
		return zero, StacktraceNew("cannot inherit from different type", target.Base().Location,
			stacktrace.WithPosition(&target.Base().KeyPos),
			stacktrace.WithInfo("source", source.Base().Type),
			stacktrace.WithInfo("target", target.Base().Type))
	}
	return ss, nil
}

// checkAliasType performs the type assertion check common to all alias() methods.
func checkAliasType[T Shape](target Shape, source Shape) (T, error) {
	ss, ok := source.(T)
	if !ok {
		var zero T
		return zero, StacktraceNew("cannot make alias from different type", target.Base().Location,
			stacktrace.WithPosition(&target.Base().KeyPos),
			stacktrace.WithInfo("source", source.Base().Type),
			stacktrace.WithInfo("target", target.Base().Type))
	}
	return ss, nil
}

func (s *BaseShape) AliasTo(source *BaseShape) (*BaseShape, error) {
	_, err := s.Shape.alias(source.Shape)
	if err != nil {
		return nil, StacktraceNewWrapped("alias shape", err, s.Location,
			stacktrace.WithPosition(&s.KeyPos))
	}
	s.DisplayName = source.DisplayName
	s.Description = source.Description
	s.Example = source.Example
	s.Examples = source.Examples
	s.Inherits = source.Inherits
	// Alias must point to the original shape
	s.Default = source.Default
	s.Required = source.Required
	s.Enum = source.Enum
	s.CustomShapeFacets = source.CustomShapeFacets
	s.CustomShapeFacetDefinitions = source.CustomShapeFacetDefinitions
	s.CustomDomainProperties = source.CustomDomainProperties
	s.XML = source.XML
	return s, nil
}

// CloneShallow creates a shallow copy of the shape.
func (s *BaseShape) CloneShallow() *BaseShape {
	c := *s
	ptr := &c

	c.CustomDomainProperties = orderedmap.New[string, *DomainExtension](s.CustomDomainProperties.Len())
	for pair := s.CustomDomainProperties.Oldest(); pair != nil; pair = pair.Next() {
		c.CustomDomainProperties.Set(pair.Key, pair.Value)
	}

	c.CustomShapeFacets = orderedmap.New[string, *DataNode](s.CustomShapeFacets.Len())
	for pair := s.CustomShapeFacets.Oldest(); pair != nil; pair = pair.Next() {
		c.CustomShapeFacets.Set(pair.Key, pair.Value)
	}

	c.CustomShapeFacetDefinitions = orderedmap.New[string, Property](s.CustomShapeFacetDefinitions.Len())
	for pair := s.CustomShapeFacetDefinitions.Oldest(); pair != nil; pair = pair.Next() {
		prop := pair.Value
		c.CustomShapeFacetDefinitions.Set(pair.Key, prop)
	}

	c.Shape = s.Shape.cloneShallow(ptr)
	return ptr
}

// Clone creates a deep copy of the shape.
//
// Use this method to make a deep copy of the shape while preserving the relationships between shapes.
// Passed cloned map will be populated with cloned shape IDs that can be reused in subsequent Clone calls.
//
// NOTE: If you need a completely independent copy of a shape, use CloneDetached method.
func (s *BaseShape) Clone(clonedMap map[int64]*BaseShape) *BaseShape {
	return s.clone(clonedMap)
}

// CloneDetached creates a detached deep copy of the shape.
//
// Detached copy makes a deep copy of the shape, including parents, links and aliases.
// This makes the copied shape and its references completely independent from the original tree.
//
// NOTE: To avoid excessive memory copies and allocation, this method must be used only
// when an independent shape copy is required. Otherwise, use Clone method.
func (s *BaseShape) CloneDetached() *BaseShape {
	return s.clone(make(map[int64]*BaseShape))
}

func (s *BaseShape) clone(clonedMap map[int64]*BaseShape) *BaseShape {
	if shape, ok := clonedMap[s.ID]; ok {
		return shape
	}

	c := *s
	clonedMap[s.ID] = &c

	// TODO: Node is not deep copied yet, but it's not mutated anyway
	c.CustomShapeFacets = orderedmap.New[string, *DataNode](s.CustomShapeFacets.Len())
	for pair := s.CustomShapeFacets.Oldest(); pair != nil; pair = pair.Next() {
		c.CustomShapeFacets.Set(pair.Key, pair.Value)
	}

	c.CustomShapeFacetDefinitions = orderedmap.New[string, Property](s.CustomShapeFacetDefinitions.Len())
	for pair := s.CustomShapeFacetDefinitions.Oldest(); pair != nil; pair = pair.Next() {
		prop := pair.Value
		prop.Base = prop.Base.clone(clonedMap)
		c.CustomShapeFacetDefinitions.Set(pair.Key, prop)
	}

	// TODO: DomainExtension is not deep copied yet, but it's not mutated anyway
	c.CustomDomainProperties = orderedmap.New[string, *DomainExtension](s.CustomDomainProperties.Len())
	for pair := s.CustomDomainProperties.Oldest(); pair != nil; pair = pair.Next() {
		c.CustomDomainProperties.Set(pair.Key, pair.Value)
	}

	if s.Alias != nil {
		c.Alias = s.Alias.clone(clonedMap)
	}
	if s.Inherits != nil {
		c.Inherits = make([]*BaseShape, len(s.Inherits))
		for i, v := range s.Inherits {
			c.Inherits[i] = v.clone(clonedMap)
		}
	}
	if s.Link != nil {
		l := *s.Link
		c.Link = &l
		c.Link.Shape = s.Link.Shape.clone(clonedMap)
	}
	c.Shape = s.Shape.clone(&c, clonedMap)

	return &c
}

// Check returns an error if type shape is invalid.
func (s *BaseShape) Check() error {
	if err := s.Shape.check(); err != nil {
		return err
	}
	for _, e := range s.Enum {
		if err := s.validate(e.Value.Raw, "$"); err != nil {
			return StacktraceNewWrapped("enum value is invalid", err, s.Location,
				stacktrace.WithPosition(&e.ValuePos))
		}
	}
	return nil
}

// IsUnwrapped returns true if the shape is unwrapped.
func (s *BaseShape) IsUnwrapped() bool {
	return s.unwrapped
}

func (s *BaseShape) SetUnwrapped() {
	s.unwrapped = true
}

// String implements fmt.Stringer.
func (s *BaseShape) String() string {
	r := fmt.Sprintf("Type: %s: Name: %s",
		s.Type,
		s.Name,
	)
	if len(s.Inherits) > 0 {
		r = fmt.Sprintf("%s: Inherits", r)
		for _, i := range s.Inherits {
			r = fmt.Sprintf("%s: %s", r, i.Name)
		}
	}
	return r
}

// Examples represents a collection of examples.
type Examples struct {
	ID  string
	Map *orderedmap.OrderedMap[string, *Example]

	// To support !include of NamedExample fragment
	Link *NamedExample

	Location string
	stacktrace.Position
}

// ShapeBaser is the interface that represents a retriever of a base shape.
type ShapeBaser interface {
	Base() *BaseShape
}

// ShapeValidator is the interface that represents a validator of a RAML shape.
type ShapeValidator interface {
	validate(v any, ctxPath string) error
}

// ShapeInheritor is the interface that represents an inheritor of a RAML shape.
type ShapeInheritor interface {
	inherit(source Shape) (Shape, error)
}

// ShapeCloner is the interface that provide clone implementation for a RAML shape.
type ShapeCloner interface {
	clone(base *BaseShape, clonedMap map[int64]*BaseShape) Shape
	cloneShallow(base *BaseShape) Shape
}

// ShapeAliaser is the interface that provides alias implementation for a RAML shape.
type ShapeAliaser interface {
	alias(source Shape) (Shape, error)
}

type ShapeChecker interface {
	check() error
}

// yamlNodesUnmarshaller is the interface that represents an unmarshaller of a RAML shape from YAML nodes.
type yamlNodesUnmarshaller interface {
	unmarshalYAMLNodes(v []*yaml.Node) error
}

// Shape is the interface that represents a RAML shape.
type Shape interface {
	// Inherit is a ShapeInheritor
	ShapeInheritor
	ShapeBaser
	ShapeChecker
	// ShapeCloner Clones the shape and its children and points to specified base shape.
	ShapeCloner
	ShapeAliaser
	ShapeValidator

	yamlNodesUnmarshaller
	fmt.Stringer
	IsScalar() bool
}

// identifyShapeType identifies the type of the shape by facets.
func identifyShapeType(shapeFacets []*yaml.Node, defaultType string) (string, error) {
	var t = ""
	var stringOnly bool
	for i := 0; i != len(shapeFacets); i += 2 {
		node := shapeFacets[i]
		var ft string
		var ok bool
		switch node.Value {
		case FacetMinLength, FacetMaxLength:
			ok = true
			ft = TypeString
		case FacetPattern:
			ok = true
			ft = TypeString
			stringOnly = true
		case FacetMinimum, FacetMaximum, FacetMultipleOf:
			ok = true
			ft = TypeNumber
		case FacetMinItems, FacetMaxItems, FacetUniqueItems, FacetItems:
			ok = true
			ft = TypeArray
		case FacetMinProperties, FacetMaxProperties, FacetAdditionalProperties, FacetProperties, FacetDiscriminator:
			ok = true
			ft = TypeObject
		case FacetFileTypes:
			ok = true
			ft = TypeFile
		}
		if ok {
			switch t {
			case TypeString:
				if ft == TypeFile && !stringOnly {
					t = TypeFile
					ft = TypeFile
				}
			case TypeFile:
				if ft == TypeString && !stringOnly {
					ft = TypeFile
					t = TypeFile
				}
			}
			if t != "" && ft != t {
				return "", fmt.Errorf("detected types by facets are not equal: %s and %s", t, ft)
			}
			t = ft
		}
	}
	// Return default type if no type can be determined
	if t == "" {
		t = defaultType
	}
	return t, nil
}

func (r *RAML) MakeRecursiveShape(headBase *BaseShape) *BaseShape {
	recursiveBase := r.MakeBaseShape(headBase.Name, headBase.Location, headBase.KeyPos, headBase.ValuePos)
	recursiveBase.anchorFrag = headBase.anchorFrag
	recursiveBase.Name = headBase.Name
	recursiveBase.Type = TypeRecursive
	recursiveBase.Description = headBase.Description
	recursiveBase.CustomDomainProperties = headBase.CustomDomainProperties
	recursiveBase.CustomShapeFacets = headBase.CustomShapeFacets
	// Recursive shapes must not provide facet definitions,
	// they are provided by the head shape.
	s := &RecursiveShape{BaseShape: recursiveBase, Head: headBase}
	recursiveBase.SetShape(s)
	return recursiveBase
}

func (r *RAML) MakeJSONShape(base *BaseShape, rawSchema string) (*JSONShape, error) {
	base.Type = "json"

	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(rawSchema))
	if err != nil {
		return nil, StacktraceNewWrapped("unmarshal json schema doc", err, base.Location,
			stacktrace.WithPosition(&base.KeyPos))
	}

	if err = compiledMetaSchemaDraft07.Validate(doc); err != nil {
		return nil, StacktraceNewWrapped("validate json schema", err, base.Location,
			stacktrace.WithPosition(&base.KeyPos))
	}

	// Use the file URI as the schema identity so that relative $ref values are
	// resolved against the RAML file that contains the inline JSON Schema.
	// pathToFileURI is idempotent: if base.Location is already a file:// URI
	// (from ParseFromPath / ParseFromString) it is returned unchanged; if it
	// is a raw OS path (from direct MakeJSONShape calls in tests/constructors)
	// it is converted to the canonical file:// form.
	p := PathToFileURI(base.Location)

	if err = r.jsonSchemaCompiler.AddResource(p, doc); err != nil {
		// ResourceExistsError means this URL was already registered by an earlier
		// schema in the same RAML parse session — the cached entry is identical,
		// so it is safe to continue.
		var exists *jsonschema.ResourceExistsError
		if !errors.As(err, &exists) {
			return nil, StacktraceNewWrapped("add schema resource", err, base.Location,
				stacktrace.WithPosition(&base.KeyPos))
		}
	}
	validator, err := r.jsonSchemaCompiler.Compile(p)
	if err != nil {
		return nil, StacktraceNewWrapped("compile schema", err, base.Location,
			stacktrace.WithPosition(&base.KeyPos))
	}

	return &JSONShape{BaseShape: base, Raw: rawSchema, Validator: validator}, nil
}

// MakeConcreteShapeYAML creates a new concrete shape and assigns it to the base shape.
func (r *RAML) MakeConcreteShapeYAML(base *BaseShape, shapeType string, shapeFacets []*yaml.Node) (Shape, error) {
	base.Type = shapeType

	// NOTE: Shape resolution is performed in a separate stage.
	var shape Shape
	switch shapeType {
	default:
		// NOTE: UnknownShape is a special type of shape that will be resolved later.
		shape = &UnknownShape{BaseShape: base}
	case TypeAny:
		shape = &AnyShape{BaseShape: base}
	case TypeNil:
		shape = &NilShape{BaseShape: base}
	case TypeObject:
		shape = &ObjectShape{BaseShape: base}
	case TypeArray:
		shape = &ArrayShape{BaseShape: base}
	case TypeString:
		shape = &StringShape{BaseShape: base}
	case TypeInteger:
		shape = &IntegerShape{BaseShape: base}
	case TypeNumber:
		shape = &NumberShape{BaseShape: base}
	case TypeDatetime:
		shape = &DateTimeShape{BaseShape: base}
	case TypeDatetimeOnly:
		shape = &DateTimeOnlyShape{BaseShape: base}
	case TypeDateOnly:
		shape = &DateOnlyShape{BaseShape: base}
	case TypeTimeOnly:
		shape = &TimeOnlyShape{BaseShape: base}
	case TypeFile:
		shape = &FileShape{BaseShape: base}
	case TypeBoolean:
		shape = &BooleanShape{BaseShape: base}
	case TypeUnion:
		shape = &UnionShape{BaseShape: base}
	case TypeJSON:
		shape = &JSONShape{BaseShape: base}
	}
	base.SetShape(shape)

	if err := shape.unmarshalYAMLNodes(shapeFacets); err != nil {
		return nil, StacktraceNewWrapped("unmarshal yaml nodes", err, base.Location,
			stacktrace.WithPosition(&base.KeyPos), stacktrace.WithInfo("shape type", shapeType))
	}

	return shape, nil
}

// MakeBaseShape creates a new base shape which is the base for all shapes.
// The anchor fragment (used for unqualified type resolution) is taken from the
// current ParseCtx stack entry.
func (r *RAML) MakeBaseShape(name string, location string, KeyPos stacktrace.Position, ValuePos stacktrace.Position) *BaseShape {
	// If position is not set, use default position.
	if KeyPos.Line == 0 && KeyPos.Column == 0 {
		KeyPos.Line = 1
	}
	if ValuePos.Line == 0 && ValuePos.Column == 0 {
		ValuePos.Line = 1
	}
	b := &BaseShape{
		ID:         r.generateSequenceID(),
		Name:       name,
		Location:   location,
		anchorFrag: r.currentParseCtx().AnchorFrag,
		KeyPos:     KeyPos,
		ValuePos:   ValuePos,

		raml:                        r,
		CustomDomainProperties:      orderedmap.New[string, *DomainExtension](0),
		CustomShapeFacets:           orderedmap.New[string, *DataNode](0),
		CustomShapeFacetDefinitions: orderedmap.New[string, Property](0),
	}
	r.PutShape(b)
	return b
}

func (r *RAML) generateSequenceID() int64 {
	return atomic.AddInt64(&r.idCounter, 1)
}

func (r *RAML) makeShapeType(
	typeKeyNode *yaml.Node,
	shapeTypeNode *yaml.Node,
	shapeFacets []*yaml.Node,
	location string,
	base *BaseShape,
	defaultType string,
) (string, Shape, error) {
	var shapeType string
	switch shapeTypeNode.Kind {
	default:
		return "", nil, StacktraceNew("type must be string or array", location,
			WithNodePosition(shapeTypeNode))
	case yaml.DocumentNode:
		return "", nil, StacktraceNew("document node is not allowed", location,
			WithNodePosition(shapeTypeNode))
	case yaml.MappingNode:
		return "", nil, StacktraceNew("mapping node is not allowed", location,
			WithNodePosition(shapeTypeNode))
	case yaml.AliasNode:
		return "", nil, StacktraceNew("alias node is not allowed", location,
			WithNodePosition(shapeTypeNode))
	case yaml.ScalarNode:
		// Record where this type expression was written for LSP reference tracking.
		includedFrom := r.noteIncludeRef(shapeTypeNode, location)
		if typeKeyNode != nil {
			base.TypeExpr = MakeNode(shapeTypeNode.Value, typeKeyNode, shapeTypeNode, location, includedFrom)
		} else {
			base.TypeExpr = MakeSeqNode(shapeTypeNode.Value, shapeTypeNode, location, includedFrom)
		}
		switch shapeTypeNode.Tag {
		case TagStr:
			shapeType = shapeTypeNode.Value
			if shapeType == "" {
				shapeTypeI, err := identifyShapeType(shapeFacets, defaultType)
				if err != nil {
					return "", nil, StacktraceNewWrapped("identify shape type", err, location,
						WithNodePosition(shapeTypeNode))
				}
				shapeType = shapeTypeI
			} else if shapeType[0] == '{' {
				s, errMake := r.MakeJSONShape(base, shapeType)
				if errMake != nil {
					return "", nil, StacktraceNewWrapped("make json shape", errMake, location,
						WithNodePosition(shapeTypeNode))
				}
				shapeType = TypeJSON
				return shapeType, s, nil
			}
		case TagInclude:
			dt, errParse := r.parseDataType(includedFrom)
			if errParse != nil {
				return "", nil, StacktraceNewWrapped("parse data", errParse, location,
					WithNodePosition(shapeTypeNode))
			}
			base.Link = dt
		case TagNull:
			shapeType = TypeString
		default:
			return "", nil, StacktraceNew("type must be string", location,
				WithNodePosition(shapeTypeNode))
		}
	case yaml.SequenceNode:
		var inherits = make([]*BaseShape, len(shapeTypeNode.Content))
		for i, node := range shapeTypeNode.Content {
			if node.Kind != yaml.ScalarNode {
				return "", nil, StacktraceNew("node kind must be scalar", location,
					WithNodePosition(node))
			} else if node.Tag == TagInclude {
				return "", nil, StacktraceNew("!include is not allowed in multiple inheritance",
					location, WithNodePosition(node))
			}
			s, errMake := r.makeNewShapeYAML(node, node, location)
			if errMake != nil {
				return "", nil, StacktraceNewWrapped("make shape", errMake, location,
					WithNodePosition(node))
			}
			inherits[i] = s
		}
		base.Inherits = inherits
		shapeType = TypeComposite
	}
	return shapeType, nil, nil
}

func (r *RAML) MakeNewShape(
	name string,
	shapeType string,
	location string,
	KeyPos stacktrace.Position,
	ValuePos stacktrace.Position,
) (*BaseShape, Shape, error) {
	base := r.MakeBaseShape(name, location, KeyPos, ValuePos)
	s, err := r.MakeConcreteShapeYAML(base, shapeType, nil)
	if err != nil {
		return nil, nil, StacktraceNewWrapped("make concrete shape", err, location,
			stacktrace.WithPosition(&base.KeyPos))
	}
	return base, s, nil
}

// makeNewShapeYAML creates a new shape from the given YAML node.
// Default type is string when no type can be inferred.
// Anchor context is read from r.currentParseCtx().
func (r *RAML) makeNewShapeYAML(k, v *yaml.Node, location string) (*BaseShape, error) {
	return r.makeNewShapeYAMLWithDefault(k, v, location, TypeString)
}

// makeNewBodyShapeYAML creates a new shape for body context from the given YAML node.
// Default type is any when no type can be inferred (per RAML spec).
func (r *RAML) makeNewBodyShapeYAML(k, v *yaml.Node, location string) (*BaseShape, error) {
	return r.makeNewShapeYAMLWithDefault(k, v, location, TypeAny)
}

// makeNewShapeYAMLWithDefault creates a new shape from the given YAML node with a specified default type.
func (r *RAML) makeNewShapeYAMLWithDefault(k, v *yaml.Node, location string, defaultType string) (*BaseShape, error) {
	// When materializing a two-stage build body, a type-bearing node produced by
	// parameter substitution or by a grafted trait/RT body carries an overlay
	// scope. Push it so unqualified type references resolve in the right
	// namespace. The shape's Location is also derived from the node so that
	// stage-2 errors and the shape's own diagnostic position attribute to the
	// file the node was authored in, not the (often different) file of the
	// caller's threaded default — see RAML.locationOf for the rule.
	if sc, ok := r.provenanceScopeFor(v); ok {
		r.pushParseCtx(sc)
		defer r.popParseCtx()
	}
	location = r.locationOf(v, location)
	name := k.Value
	base := r.MakeBaseShape(
		name,
		location,
		NewNodePosition(k),
		NewNodePosition(v),
	)
	r.storeEntityNode(base.ID, k, v)

	typeKeyNode, shapeTypeNode, shapeFacets, err := base.decode(v)
	if err != nil {
		return nil, StacktraceNewWrapped("decode", err, location, WithNodePosition(v))
	}

	var shapeType string
	if shapeTypeNode == nil {
		shapeType, err = identifyShapeType(shapeFacets, defaultType)
		if err != nil {
			return nil, StacktraceNewWrapped("identify shape type", err, location,
				WithNodePosition(v))
		}
	} else {
		var shape Shape
		shapeType, shape, err = r.makeShapeType(typeKeyNode, shapeTypeNode, shapeFacets, location, base, defaultType)
		if err != nil {
			return nil, StacktraceNewWrapped("make shape type", err, base.Location, stacktrace.WithPosition(&base.KeyPos))
		}
		if shape != nil {
			base.SetShape(shape)
			return base, nil
		}
	}

	s, err := r.MakeConcreteShapeYAML(base, shapeType, shapeFacets)
	if err != nil {
		return nil, StacktraceNewWrapped("make concrete shape", err, base.Location,
			stacktrace.WithPosition(&base.KeyPos))
	}
	if _, ok := s.(*UnknownShape); ok {
		r.unresolvedShapes = append(r.unresolvedShapes, base)
	}
	return base, nil
}

func (s *BaseShape) decodeExamples(valueNode *yaml.Node) error {
	if s.Example != nil {
		return StacktraceNew("example and examples cannot be defined together", s.Location,
			WithNodePosition(valueNode))
	}
	if valueNode.Kind == yaml.ScalarNode && valueNode.Tag == TagInclude {
		n, err := s.raml.parseNamedExample(s.raml.noteIncludeRef(valueNode, s.Location))
		if err != nil {
			return StacktraceNewWrapped("parse named example", err, s.Location,
				WithNodePosition(valueNode))
		}
		s.Examples = &Examples{Link: n, Location: s.Location}
		return nil
	} else if valueNode.Kind != yaml.MappingNode {
		return StacktraceNew("examples must be map", s.Location,
			WithNodePosition(valueNode))
	}
	examples := orderedmap.New[string, *Example](len(valueNode.Content) / 2)
	for j := 0; j != len(valueNode.Content); j += 2 {
		name := valueNode.Content[j].Value
		data := valueNode.Content[j+1]
		example, err := s.raml.makeExample(data, name, s.Location)
		if err != nil {
			return StacktraceNewWrapped(fmt.Sprintf("make examples: [%d]", j),
				err, s.Location, WithNodePosition(data))
		}
		examples.Set(name, example)
	}
	s.Examples = &Examples{Map: examples, Location: s.Location}
	return nil
}

// isBuiltInFacetForType checks if a facet name is a built-in facet for a specific shape type.
func isBuiltInFacetForType(name, shapeType string) bool {
	// Check common facets
	if _, ok := commonFacets[name]; ok {
		return true
	}
	// Check type-specific facets
	if typeFacets, ok := typeSpecificFacets[shapeType]; ok {
		if _, ok := typeFacets[name]; ok {
			return true
		}
	}
	return false
}

// decodeFacets decodes the facet: "facets" from the YAML node.
func (s *BaseShape) decodeFacets(valueNode *yaml.Node) error {
	s.CustomShapeFacetDefinitions = orderedmap.New[string, Property](len(valueNode.Content) / 2)
	for j := 0; j != len(valueNode.Content); j += 2 {
		keyNode := valueNode.Content[j]
		valueNode := valueNode.Content[j+1]

		nodeName := keyNode.Value
		// Facet names MUST NOT begin with open parenthesis to disambiguate from annotations
		if len(nodeName) > 0 && nodeName[0] == '(' {
			return StacktraceNew("facet name must not begin with '('", s.Location,
				WithNodePosition(keyNode), stacktrace.WithInfo("facet", nodeName))
		}

		propertyName, hasImplicitOptional := chompImplicitOptional(nodeName)
		// Check if the facet name conflicts with built-in facets for this shape type
		if isBuiltInFacetForType(propertyName, s.Type) {
			return StacktraceNew("cannot redefine built-in facet", s.Location,
				WithNodePosition(keyNode), stacktrace.WithInfo("facet", nodeName))
		}

		property, err := s.raml.makeProperty(keyNode, valueNode, propertyName, s.Location, hasImplicitOptional)
		if err != nil {
			return StacktraceNewWrapped("make property", err, s.Location,
				WithNodePosition(keyNode))
		}
		s.CustomShapeFacetDefinitions.Set(property.Name, property)
	}
	return nil
}

// XMLSerialization represents the xml facet for XML serialization configuration.
type XMLSerialization struct {
	Attribute *Node[bool]
	Wrapped   *Node[bool]
	Name      *Node[string]
	Namespace *Node[string]
	Prefix    *Node[string]

	Location string
	stacktrace.Position
}

// Decode decodes the XMLSerialization from a YAML node.
func (x *XMLSerialization) Decode(valueNode *yaml.Node, location string, r *RAML) error {
	x.Location = location
	x.Position = NewNodePosition(valueNode)
	if valueNode.Kind != yaml.MappingNode {
		return StacktraceNew("xml must be a mapping", location, WithNodePosition(valueNode))
	}
	for i := 0; i != len(valueNode.Content); i += 2 {
		key := valueNode.Content[i]
		val := valueNode.Content[i+1]
		fragmentPath, rv, err := r.resolveInclude(val, location)
		if err != nil {
			return StacktraceNewWrapped("resolve include", err, location, WithNodePosition(val))
		}
		switch key.Value {
		case XmlAttribute:
			var attr bool
			if err := rv.Decode(&attr); err != nil {
				return StacktraceNewWrapped("decode attribute", err, location, WithNodePosition(val))
			}
			x.Attribute = MakeNode(attr, key, val, location, fragmentPath)
		case XmlWrapped:
			var wrapped bool
			if err := rv.Decode(&wrapped); err != nil {
				return StacktraceNewWrapped("decode wrapped", err, location, WithNodePosition(val))
			}
			x.Wrapped = MakeNode(wrapped, key, val, location, fragmentPath)
		case XmlName:
			var name string
			if err := rv.Decode(&name); err != nil {
				return StacktraceNewWrapped("decode name", err, location, WithNodePosition(val))
			}
			x.Name = MakeNode(name, key, val, location, fragmentPath)
		case XmlNamespace:
			var namespace string
			if err := rv.Decode(&namespace); err != nil {
				return StacktraceNewWrapped("decode namespace", err, location, WithNodePosition(val))
			}
			x.Namespace = MakeNode(namespace, key, val, location, fragmentPath)
		case XmlPrefix:
			var prefix string
			if err := rv.Decode(&prefix); err != nil {
				return StacktraceNewWrapped("decode prefix", err, location, WithNodePosition(val))
			}
			x.Prefix = MakeNode(prefix, key, val, location, fragmentPath)
		default:
			return StacktraceNew("unknown xml property", location, WithNodePosition(key), stacktrace.WithInfo("property", key.Value))
		}
	}
	return nil
}

func (s *BaseShape) decodeExample(valueNode *yaml.Node) error {
	if s.Examples != nil {
		return StacktraceNew("example and examples cannot be defined together", s.Location,
			WithNodePosition(valueNode))
	}
	example, err := s.raml.makeExample(valueNode, "", s.Location)
	if err != nil {
		return StacktraceNewWrapped("make example", err, s.Location,
			WithNodePosition(valueNode))
	}
	s.Example = example
	return nil
}

func (s *BaseShape) decodeValueNode(keyNode, valueNode *yaml.Node) (typeKeyNode, shapeTypeNode *yaml.Node, shapeFacets []*yaml.Node, err error) {
	shapeFacets = make([]*yaml.Node, 0)

	switch keyNode.Value {
	case FacetSchema:
		fallthrough
	case FacetType:
		typeKeyNode = keyNode
		shapeTypeNode = valueNode
	case FacetDisplayName:
		var sn *ScalarFacet[string]
		if sn, err = MakeScalarFacetYAML[string](s.raml, keyNode, valueNode, s.Location); err != nil {
			return
		}
		s.DisplayName = sn
	case FacetDescription:
		var sn *ScalarFacet[string]
		if sn, err = MakeScalarFacetYAML[string](s.raml, keyNode, valueNode, s.Location); err != nil {
			return
		}
		s.Description = sn
	case FacetRequired:
		var sn *ScalarFacet[bool]
		if sn, err = MakeScalarFacetYAML[bool](s.raml, keyNode, valueNode, s.Location); err != nil {
			err = StacktraceNewWrapped("decode required", err, s.Location, WithNodePosition(valueNode))
			return
		}
		s.Required = sn
	case FacetFacets:
		if err = s.decodeFacets(valueNode); err != nil {
			err = StacktraceNewWrapped("decode facets", err, s.Location,
				WithNodePosition(valueNode))
			return
		}
	case FacetExample:
		if err = s.decodeExample(valueNode); err != nil {
			err = StacktraceNewWrapped("decode example", err, s.Location,
				WithNodePosition(valueNode))
			return
		}
	case FacetExamples:
		if err = s.decodeExamples(valueNode); err != nil {
			err = StacktraceNewWrapped("decode example", err, s.Location,
				WithNodePosition(valueNode))
			return
		}
	case FacetDefault:
		var n *DataNode
		n, err = s.raml.makeRootNode(keyNode, valueNode, s.Location)
		if err != nil {
			err = StacktraceNewWrapped("make node default", err, s.Location,
				WithNodePosition(valueNode))
			return
		}
		s.Default = n
	case FacetEnum:
		enums, enumErr := s.raml.MakeEnum(valueNode, s.Location)
		if enumErr != nil {
			err = StacktraceNewWrapped("make enum", enumErr, s.Location, WithNodePosition(valueNode))
			return
		}
		s.Enum = enums
	case FacetXml:
		xml := &XMLSerialization{}
		if err = xml.Decode(valueNode, s.Location, s.raml); err != nil {
			err = StacktraceNewWrapped("decode xml", err, s.Location,
				WithNodePosition(valueNode))
			return
		}
		s.XML = xml
	case FacetAllowedTargets:
		// if err := valueNode.Decode(&s.AllowedTargets); err != nil {
		// 	return nil, nil, StacktraceNewWrapped("decode allowed targets", err, s.Location,
		// 		WithNodePosition(valueNode))
		// }
	default:
		if IsCustomDomainExtensionNode(keyNode.Value) {
			var de *DomainExtension
			de, err = s.raml.unmarshalCustomDomainExtension(s.Location, keyNode, valueNode)
			if err != nil {
				err = StacktraceNewWrapped("unmarshal custom domain extension", err, s.Location,
					WithNodePosition(valueNode))
				return
			}
			s.CustomDomainProperties.Set(de.Name, de)
		} else {
			shapeFacets = append(shapeFacets, keyNode, valueNode)
		}
	}
	return
}

// decode decodes the shape from the YAML node.
// It returns the type-key node, the shape type node, facets and an error if any.
func (s *BaseShape) decode(value *yaml.Node) (typeKeyNode, shapeTypeNode *yaml.Node, shapeFacets []*yaml.Node, err error) {
	// For inline type declaration
	if value.Kind == yaml.ScalarNode || value.Kind == yaml.SequenceNode {
		return nil, value, nil, nil
	}

	if value.Kind != yaml.MappingNode {
		return nil, nil, nil, StacktraceNew("value kind must be map", s.Location, WithNodePosition(value))
	}

	shapeFacets = make([]*yaml.Node, 0)

	for i := 0; i != len(value.Content); i += 2 {
		node := value.Content[i]
		valueNode := value.Content[i+1]
		tk, t, f, decodeErr := s.decodeValueNode(node, valueNode)
		if decodeErr != nil {
			return nil, nil, nil, StacktraceNewWrapped("decode value node", decodeErr, s.Location)
		}
		if t != nil {
			// RAML 1.0 forbids defining a body (or shape) with both `type:` and
			// `schema:` — they are mutually exclusive aliases.
			if typeKeyNode != nil {
				return nil, nil, nil, StacktraceNew(
					"`type` and `schema` are mutually exclusive",
					s.Location, WithNodePosition(node))
			}
			typeKeyNode = tk
			shapeTypeNode = t
		}
		if len(f) > 0 {
			shapeFacets = append(shapeFacets, f...)
		}
	}

	return
}
