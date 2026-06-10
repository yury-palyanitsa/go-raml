package converter

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"

	raml "github.com/acronis/go-raml/v3"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

const (
	oas3FmtInt8  = "int8"
	oas3FmtInt16 = "int16"
	oas3FmtInt   = "int"
	oas3FmtInt32 = "int32"
	oas3FmtInt64 = "int64"
	oas3FmtLong  = "long"
)

// OAS3SchemaConverter converts unwrapped RAML shapes to OAS 3.0 Schema Objects.
//
// Usage:
//
//	conv := NewOAS3SchemaConverter()
//	schema, err := conv.Convert(shape)
//	schemas := conv.Components() // feed into components/schemas
//
// The input shape MUST be unwrapped (OptWithUnwrap must have run).
type OAS3SchemaConverter struct {
	// components collects schemas placed in components/schemas because they are
	// referenced by $ref (named types and recursive types).
	// Key is the shape name, value is the converted schema.
	components *orderedmap.OrderedMap[string, *OAS3Schema]
	// namedTypes is the set of top-level named type names registered by the
	// document converter via convertNamedTypes. Shapes whose single parent
	// (Inherits[0]) is a named type are emitted as $ref instead of inlined.
	namedTypes map[string]struct{}
}

// NewOAS3SchemaConverter creates a ready-to-use converter.
func NewOAS3SchemaConverter() *OAS3SchemaConverter {
	return &OAS3SchemaConverter{
		components: orderedmap.New[string, *OAS3Schema](),
		namedTypes: make(map[string]struct{}),
	}
}

// Components returns all schemas accumulated in components/schemas during
// conversion. The caller (document converter) is responsible for merging
// named API types into this map before building the final document.
func (c *OAS3SchemaConverter) Components() *orderedmap.OrderedMap[string, *OAS3Schema] {
	return c.components
}

// Convert converts a single unwrapped shape to an OAS3Schema.
// Returns an error if the shape has not been unwrapped.
func (c *OAS3SchemaConverter) Convert(s raml.Shape) (*OAS3Schema, error) {
	if !s.Base().IsUnwrapped() {
		return nil, fmt.Errorf("shape %q must be unwrapped before OAS3 conversion", s.Base().Name)
	}
	return c.Visit(s), nil
}

// Visit dispatches to the concrete shape visitor. It never returns nil for
// known shape types; unknown types yield an empty schema.
//
// If the shape directly inherits from a single top-level named type (registered
// via convertNamedTypes), it is emitted as a $ref to components/schemas and the
// named type is lazily registered there on first use.
func (c *OAS3SchemaConverter) Visit(s raml.Shape) *OAS3Schema {
	if ref := c.namedTypeRef(s.Base()); ref != nil {
		return ref
	}
	return c.visitDirect(s)
}

// visitDirect dispatches to the concrete shape visitor without the named-type
// $ref check. Used when building a named type's own component schema so that
// the type is expanded inline rather than collapsing to a self-$ref.
func (c *OAS3SchemaConverter) visitDirect(s raml.Shape) *OAS3Schema {
	switch t := s.(type) {
	case *raml.ObjectShape:
		return c.visitObject(t)
	case *raml.ArrayShape:
		return c.visitArray(t)
	case *raml.UnionShape:
		return c.visitUnion(t)
	case *raml.StringShape:
		return c.visitString(t)
	case *raml.IntegerShape:
		return c.visitInteger(t)
	case *raml.NumberShape:
		return c.visitNumber(t)
	case *raml.BooleanShape:
		return c.visitBoolean(t)
	case *raml.FileShape:
		return c.visitFile(t)
	case *raml.DateTimeShape:
		return c.visitDateTime(t)
	case *raml.DateTimeOnlyShape:
		return c.visitDateTimeOnly(t)
	case *raml.DateOnlyShape:
		return c.visitDateOnly(t)
	case *raml.TimeOnlyShape:
		return c.visitTimeOnly(t)
	case *raml.NilShape:
		return c.visitNil(t)
	case *raml.AnyShape:
		return c.visitAny(t)
	case *raml.JSONShape:
		return c.visitJSON(t)
	case *raml.RecursiveShape:
		return c.visitRecursive(t)
	default:
		return &OAS3Schema{}
	}
}

// namedTypeRef returns a $ref schema if base directly inherits from exactly one
// top-level named type, lazily registering that type in components/schemas.
// Returns nil if the shape is not a simple named-type reference.
func (c *OAS3SchemaConverter) namedTypeRef(base *raml.BaseShape) *OAS3Schema {
	if len(base.Inherits) != 1 {
		return nil
	}
	parent := base.Inherits[0]
	if parent == nil || parent.Name == "" || parent.Shape == nil {
		return nil
	}
	if _, isNamed := c.namedTypes[parent.Name]; !isNamed {
		return nil
	}
	name := parent.Name
	if _, exists := c.components.Get(name); !exists {
		// Set placeholder before recursing to prevent infinite loops on
		// self-referential types that are not wrapped in RecursiveShape.
		c.components.Set(name, &OAS3Schema{})
		c.components.Set(name, c.visitDirect(parent.Shape))
	}
	return &OAS3Schema{Ref: "#/components/schemas/" + name}
}

// ---- concrete visitors -------------------------------------------------------

func (c *OAS3SchemaConverter) visitObject(s *raml.ObjectShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeObject
	schema.MinProperties = raml.ScalarFacetValPtr(s.MinProperties)
	schema.MaxProperties = raml.ScalarFacetValPtr(s.MaxProperties)
	schema.AdditionalProperties = raml.ScalarFacetValPtr(s.AdditionalProperties)

	if s.Discriminator != nil {
		schema.Discriminator = &OAS3Discriminator{PropertyName: s.Discriminator.Value}
	}

	if l := s.Properties.Len(); l > 0 {
		schema.Properties = orderedmap.New[string, *OAS3Schema](l)
		for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
			k := pair.Key
			v := &pair.Value
			schema.Properties.Set(k, c.Visit(v.Base.Shape))
			if v.Required {
				schema.Required = append(schema.Required, k)
			}
		}
	}
	if l := s.PatternProperties.Len(); l > 0 {
		schema.PatternProperties = orderedmap.New[string, *OAS3Schema](l)
		for pair := s.PatternProperties.Oldest(); pair != nil; pair = pair.Next() {
			k := pair.Key
			// RAML stores pattern properties with surrounding slashes (/pattern/);
			// strip them to get the bare regex.
			k = k[1 : len(k)-1]
			schema.PatternProperties.Set(k, c.Visit(pair.Value.Base.Shape))
		}
	}
	return schema
}

func (c *OAS3SchemaConverter) visitArray(s *raml.ArrayShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeArray
	schema.MinItems = raml.ScalarFacetValPtr(s.MinItems)
	schema.MaxItems = raml.ScalarFacetValPtr(s.MaxItems)
	schema.UniqueItems = raml.ScalarFacetValPtr(s.UniqueItems)
	if s.Items != nil {
		schema.Items = c.Visit(s.Items.Shape)
	}
	return schema
}

// visitUnion handles RAML union shapes, including the special nullable pattern.
//
// If any member of the union is raml.NilShape the result uses OAS 3.0's
// `nullable: true` instead of wrapping in anyOf:
//   - one non-nil member  → emit that member's schema with nullable: true
//   - multiple non-nil    → emit anyOf of the non-nil members with nullable: true
//   - all nil             → emit empty schema with nullable: true (pure null type)
func (c *OAS3SchemaConverter) visitUnion(s *raml.UnionShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())

	var nonNil []*raml.BaseShape
	hasNil := false
	for _, member := range s.AnyOf {
		if _, ok := member.Shape.(*raml.NilShape); ok {
			hasNil = true
		} else {
			nonNil = append(nonNil, member)
		}
	}

	if hasNil {
		schema.Nullable = true
	}

	switch len(nonNil) {
	case 0:
		// Pure null type (unlikely but valid in RAML).
		// OAS3 has no type:null; represent as nullable with no type constraint.
		return schema
	case 1:
		// Flatten: merge the single non-nil member into schema to avoid a
		// superfluous anyOf wrapper.
		inner := c.Visit(nonNil[0].Shape)
		mergeOAS3BaseInto(schema, inner)
		return schema
	default:
		schema.AnyOf = make([]*OAS3Schema, len(nonNil))
		for i, m := range nonNil {
			schema.AnyOf[i] = c.Visit(m.Shape)
		}
		return schema
	}
}

func (c *OAS3SchemaConverter) visitString(s *raml.StringShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeString
	schema.MinLength = raml.ScalarFacetValPtr(s.MinLength)
	schema.MaxLength = raml.ScalarFacetValPtr(s.MaxLength)
	if s.Pattern != nil {
		schema.Pattern = s.Pattern.Value.String()
	}
	return schema
}

func (c *OAS3SchemaConverter) visitInteger(s *raml.IntegerShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeInteger
	if s.Minimum != nil {
		schema.Minimum = json.Number(s.Minimum.Value.String())
	}
	if s.Maximum != nil {
		schema.Maximum = json.Number(s.Maximum.Value.String())
	}
	if s.MultipleOf != nil {
		schema.MultipleOf = oas3RatToNumber(s.MultipleOf.Value)
	}
	if s.Format != nil {
		applyOAS3IntegerFormat(schema, s.Format.Value)
	}
	return schema
}

// applyOAS3IntegerFormat sets format or min/max bounds on schema based on the
// RAML integer format string. int8/int16 expand to explicit bounds; other
// formats map directly to an OAS3 format keyword.
func applyOAS3IntegerFormat(schema *OAS3Schema, format string) {
	if fmtMin, fmtMax, ok := oas3IntegerFormatBounds(format); ok {
		// int8/int16: no OAS3 format keyword — substitute with min/max bounds
		// only when the shape carries no explicit constraint of its own.
		if schema.Minimum == "" {
			schema.Minimum = fmtMin
		}
		if schema.Maximum == "" {
			schema.Maximum = fmtMax
		}
		return
	}
	schema.Format = oas3IntegerFormat(format)
}

func (c *OAS3SchemaConverter) visitNumber(s *raml.NumberShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeNumber
	if s.Minimum != nil {
		schema.Minimum = oas3RatToNumber(s.Minimum.Value)
	}
	if s.Maximum != nil {
		schema.Maximum = oas3RatToNumber(s.Maximum.Value)
	}
	if s.MultipleOf != nil {
		schema.MultipleOf = oas3RatToNumber(s.MultipleOf.Value)
	}
	if s.Format != nil {
		schema.Format = s.Format.Value
	}
	return schema
}

func (c *OAS3SchemaConverter) visitBoolean(s *raml.BooleanShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeBoolean
	return schema
}

// visitFile maps RAML file to OAS3 type:string + format:binary.
// The RAML-specific fileTypes facet is dropped because OAS 3.0 has no
// equivalent per-type constraint; contentEncoding/contentMediaType are
// JSON Schema Draft-7 keywords not present in OAS 3.0.
func (c *OAS3SchemaConverter) visitFile(s *raml.FileShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeString
	schema.Format = "binary"
	schema.MinLength = raml.ScalarFacetValPtr(s.MinLength)
	schema.MaxLength = raml.ScalarFacetValPtr(s.MaxLength)
	return schema
}

func (c *OAS3SchemaConverter) visitDateTime(s *raml.DateTimeShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeString
	if s.Format != nil {
		switch s.Format.Value {
		case raml.DateTimeFormatRFC3339:
			schema.Format = raml.FormatDateTime
		case raml.DateTimeFormatRFC2616:
			schema.Pattern = "^(Mon|Tue|Wed|Thu|Fri|Sat|Sun), ([0-3][0-9]) " +
				"(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) ([0-9]{4})" +
				" ([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9] GMT$"
		}
	} else {
		schema.Format = raml.FormatDateTime
	}
	return schema
}

func (c *OAS3SchemaConverter) visitDateTimeOnly(s *raml.DateTimeOnlyShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeString
	schema.Pattern = "^[0-9]{4}-(?:0[0-9]|1[0-2])-(?:[0-2][0-9]|3[01])T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]$"
	return schema
}

func (c *OAS3SchemaConverter) visitDateOnly(s *raml.DateOnlyShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeString
	schema.Format = raml.FormatDate
	return schema
}

func (c *OAS3SchemaConverter) visitTimeOnly(s *raml.TimeOnlyShape) *OAS3Schema {
	schema := c.baseSchema(s.Base())
	schema.Type = raml.TypeString
	schema.Format = raml.FormatTime
	return schema
}

func (c *OAS3SchemaConverter) visitNil(_ *raml.NilShape) *OAS3Schema {
	// OAS3 has no type:null. Represent as an empty schema with nullable:true.
	// In practice raml.NilShape only appears inside raml.UnionShape where visitUnion
	// handles it directly; this path fires only for a bare nil-typed shape.
	return &OAS3Schema{Nullable: true}
}

func (c *OAS3SchemaConverter) visitAny(s *raml.AnyShape) *OAS3Schema {
	return c.baseSchema(s.Base())
}

// visitJSON converts an inline JSON Schema body (type: {...}) to OAS3.
//
// The JSON Schema is compiled to a RAML shape view via AsShape(); that view is
// then run through the OAS3 converter. RAML-level metadata on the outer shape
// overrides any conflicting metadata on the inner view.
func (c *OAS3SchemaConverter) visitJSON(s *raml.JSONShape) *OAS3Schema {
	view, err := s.AsShape()
	if err != nil || view == nil {
		// Fallback: emit metadata only, no type constraint.
		schema := c.baseSchema(s.Base())
		if schema.Extensions == nil {
			schema.Extensions = make(map[string]any, 1)
		}
		schema.Extensions["x-json"] = s.Raw
		return schema
	}
	ramlBase := s.Base()
	viewBase := view.Base()
	if ramlBase.DisplayName != nil {
		viewBase.DisplayName = ramlBase.DisplayName
	}
	if ramlBase.Description != nil {
		viewBase.Description = ramlBase.Description
	}
	if ramlBase.Default != nil {
		viewBase.Default = ramlBase.Default
	}
	if ramlBase.Examples != nil || ramlBase.Example != nil {
		viewBase.Examples = ramlBase.Examples
		viewBase.Example = ramlBase.Example
	}
	viewBase.CustomDomainProperties = ramlBase.CustomDomainProperties
	return c.Visit(view)
}

// visitRecursive emits a $ref to components/schemas and registers the target
// schema in the components map if not already present.
func (c *OAS3SchemaConverter) visitRecursive(s *raml.RecursiveShape) *OAS3Schema {
	head := s.Head.Shape
	name := head.Base().Name

	if _, exists := c.components.Get(name); !exists {
		// Occupy the slot before recursing to prevent infinite loops.
		// Use visitDirect so the type is expanded inline rather than
		// collapsing to a self-$ref via namedTypeRef.
		c.components.Set(name, &OAS3Schema{})
		c.components.Set(name, c.visitDirect(head))
	}

	return &OAS3Schema{Ref: "#/components/schemas/" + name}
}

// ---- helpers -----------------------------------------------------------------

// baseSchema builds an OAS3Schema pre-populated with metadata from a
// raml.BaseShape: title, description, default, example, enum, xml, and any
// RAML annotations mapped to x- extensions.
func (c *OAS3SchemaConverter) baseSchema(base *raml.BaseShape) *OAS3Schema {
	schema := &OAS3Schema{}

	if base.DisplayName != nil {
		schema.Title = base.DisplayName.Value
	}
	if base.Description != nil {
		schema.Description = base.Description.Value
	}
	if base.Default != nil {
		schema.Default = base.Default.Value.Raw
	}
	// OAS 3.0 uses a singular example. Prefer the named Examples map (take the
	// first entry); fall back to the inline Example facet.
	if base.Examples != nil && base.Examples.Map.Len() > 0 {
		first := base.Examples.Map.Oldest()
		schema.Example = first.Value.Data.Value.Raw
	} else if base.Example != nil {
		schema.Example = base.Example.Data.Value.Raw
	}
	if base.Enum != nil {
		schema.Enum = make([]any, len(base.Enum))
		for i, v := range base.Enum {
			schema.Enum[i] = v.Value.Raw
		}
	}
	if base.XML != nil {
		schema.XML = oas3XMLFrom(base.XML)
	}
	if n := base.CustomDomainProperties.Len(); n > 0 {
		schema.Extensions = make(map[string]any, n)
		for pair := base.CustomDomainProperties.Oldest(); pair != nil; pair = pair.Next() {
			// RAML annotation names are wrapped in parentheses; strip them and
			// prefix with x- to produce a valid OAS3 extension key.
			key := "x-" + pair.Key
			schema.Extensions[key] = pair.Value.Extension.Value.Raw
		}
	}
	return schema
}

// mergeOAS3BaseInto copies type-level fields from src into dst, preserving
// any metadata already set on dst (title, description, example, etc.).
// Used when flattening a single-member nullable union.
func mergeOAS3BaseInto(dst, src *OAS3Schema) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
	if dst.Default == nil {
		dst.Default = src.Default
	}
	if dst.Example == nil {
		dst.Example = src.Example
	}
	if dst.Enum == nil {
		dst.Enum = src.Enum
	}
	if dst.XML == nil {
		dst.XML = src.XML
	}
	if dst.Extensions == nil && len(src.Extensions) > 0 {
		dst.Extensions = src.Extensions
	}

	// Type-level fields — always taken from src (the concrete type).
	dst.Type = src.Type
	dst.Format = src.Format
	dst.Ref = src.Ref
	dst.AllOf = src.AllOf
	dst.AnyOf = src.AnyOf
	dst.OneOf = src.OneOf
	dst.Not = src.Not
	dst.Discriminator = src.Discriminator
	dst.Properties = src.Properties
	dst.PatternProperties = src.PatternProperties
	dst.AdditionalProperties = src.AdditionalProperties
	dst.Required = src.Required
	dst.MinProperties = src.MinProperties
	dst.MaxProperties = src.MaxProperties
	dst.Items = src.Items
	dst.MinItems = src.MinItems
	dst.MaxItems = src.MaxItems
	dst.UniqueItems = src.UniqueItems
	dst.Minimum = src.Minimum
	dst.Maximum = src.Maximum
	dst.MultipleOf = src.MultipleOf
	dst.MinLength = src.MinLength
	dst.MaxLength = src.MaxLength
	dst.Pattern = src.Pattern
}

// oas3XMLFrom converts an raml.XMLSerialization facet to an OAS3XML object.
func oas3XMLFrom(x *raml.XMLSerialization) *OAS3XML {
	if x == nil {
		return nil
	}
	xml := &OAS3XML{}
	if x.Name != nil {
		xml.Name = x.Name.Value
	}
	if x.Namespace != nil {
		xml.Namespace = x.Namespace.Value
	}
	if x.Prefix != nil {
		xml.Prefix = x.Prefix.Value
	}
	if x.Attribute != nil {
		xml.Attribute = x.Attribute.Value
	}
	if x.Wrapped != nil {
		xml.Wrapped = x.Wrapped.Value
	}
	return xml
}

// oas3RatToNumber formats a *big.Rat as a JSON Number.
// Integer-valued rationals are emitted without a decimal point.
func oas3RatToNumber(r *big.Rat) json.Number {
	if r.IsInt() {
		return json.Number(r.Num().String())
	}
	f, _ := r.Float64()
	return json.Number(strconv.FormatFloat(f, 'f', -1, 64))
}

// oas3IntegerFormatBounds returns the inclusive lo/hi bounds implied by RAML
// integer formats that have no OAS3 format keyword equivalent (int8, int16).
// The third return is false for formats that do have a direct OAS3 format keyword.
func oas3IntegerFormatBounds(f string) (json.Number, json.Number, bool) {
	switch f {
	case oas3FmtInt8:
		return "-128", "127", true
	case oas3FmtInt16:
		return "-32768", "32767", true
	default:
		return "", "", false
	}
}

// oas3IntegerFormat maps RAML integer format strings that have a direct OAS3
// format keyword. int8/int16 are handled separately by oas3IntegerFormatBounds.
// RAML "int" and "int32" both map to "int32"; "long" maps to "int64".
func oas3IntegerFormat(f string) string {
	switch f {
	case oas3FmtInt32, oas3FmtInt:
		return oas3FmtInt32
	case oas3FmtInt64, oas3FmtLong:
		return oas3FmtInt64
	default:
		return ""
	}
}
