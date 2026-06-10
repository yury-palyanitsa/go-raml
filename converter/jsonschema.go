package converter

import (
	"encoding/json"
	"errors"
	"math/big"
	"strconv"

	raml "github.com/acronis/go-raml/v3"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

type WrapperFunc[T jsonSchemaWrapper[T]] func(conv *JSONSchemaConverter[T], core *JSONSchemaGeneric[T], src *raml.BaseShape) T

type JSONSchemaConverterOptions[T jsonSchemaWrapper[T]] struct {
	// omitRefs bool
	wrap WrapperFunc[T]
}

type JSONSchemaConverterOpt[T jsonSchemaWrapper[T]] interface {
	apply(*JSONSchemaConverterOptions[T])
}

type optWrapper[T jsonSchemaWrapper[T]] struct{ f WrapperFunc[T] }

//nolint:unused // Actually used in JSONSchemaConverter constructor.
func (o optWrapper[T]) apply(c *JSONSchemaConverterOptions[T]) { c.wrap = o.f }

// WithWrapper lets the caller provide a dialect wrapper.
func WithWrapper[T jsonSchemaWrapper[T]](f WrapperFunc[T]) JSONSchemaConverterOpt[T] {
	return optWrapper[T]{f}
}

// type optOmitRefs[T jsonSchemaWrapper[T]] struct{ omitRefs bool }

// func (o optOmitRefs[T]) apply(c *JSONSchemaConverterOptions[T]) { c.omitRefs = o.omitRefs }
// func WithOmitRefs[T jsonSchemaWrapper[T]](b bool) JSONSchemaConverterOpt[T] {
// 	return optOmitRefs[T]{b}
// }

type JSONSchemaConverter[T jsonSchemaWrapper[T]] struct {
	raml.ShapeVisitor[T]

	definitions map[string]T
	err         error

	opts JSONSchemaConverterOptions[T]
}

func NewJSONSchemaConverter[T jsonSchemaWrapper[T]](opt ...JSONSchemaConverterOpt[T]) (*JSONSchemaConverter[T], error) {
	c := &JSONSchemaConverter[T]{definitions: make(map[string]T)}
	for _, o := range opt {
		o.apply(&c.opts)
	}
	if c.opts.wrap == nil {
		if _, ok := any((*JSONSchema)(nil)).(T); !ok {
			return nil, errors.New("NewJSONSchemaConverter requires WithWrapper for customized schemas")
		}
	}
	return c, nil
}

func (c *JSONSchemaConverter[T]) Convert(s raml.Shape) (T, error) {
	// TODO: Need to pass *raml.BaseShape
	var zero T
	if !s.Base().IsUnwrapped() {
		return zero, errors.New("entrypoint shape must be unwrapped")
	}

	entrypointName := s.Base().Name
	c.definitions = make(map[string]T)
	c.err = nil
	// NOTE: Assign empty schema before traversing to definitions to occupy the name.
	c.definitions[entrypointName] = zero
	c.definitions[entrypointName] = c.Visit(s)

	if c.err != nil {
		return zero, c.err
	}

	core := &JSONSchemaGeneric[T]{
		Version:     JSONSchemaVersion,
		Ref:         "#/definitions/" + entrypointName,
		Definitions: c.definitions,
	}

	if c.opts.wrap != nil {
		return c.opts.wrap(c, core, nil), nil
	}

	return any(core).(T), nil
}

func (c *JSONSchemaConverter[T]) Visit(s raml.Shape) T {
	switch shapeType := s.(type) {
	case *raml.ObjectShape:
		return c.VisitObjectShape(shapeType)
	case *raml.ArrayShape:
		return c.VisitArrayShape(shapeType)
	case *raml.StringShape:
		return c.VisitStringShape(shapeType)
	case *raml.NumberShape:
		return c.VisitNumberShape(shapeType)
	case *raml.IntegerShape:
		return c.VisitIntegerShape(shapeType)
	case *raml.BooleanShape:
		return c.VisitBooleanShape(shapeType)
	case *raml.FileShape:
		return c.VisitFileShape(shapeType)
	case *raml.UnionShape:
		return c.VisitUnionShape(shapeType)
	case *raml.NilShape:
		return c.VisitNilShape(shapeType)
	case *raml.AnyShape:
		return c.VisitAnyShape(shapeType)
	case *raml.DateTimeShape:
		return c.VisitDateTimeShape(shapeType)
	case *raml.DateTimeOnlyShape:
		return c.VisitDateTimeOnlyShape(shapeType)
	case *raml.DateOnlyShape:
		return c.VisitDateOnlyShape(shapeType)
	case *raml.TimeOnlyShape:
		return c.VisitTimeOnlyShape(shapeType)
	case *raml.JSONShape:
		return c.VisitJSONShape(shapeType)
	case *raml.RecursiveShape:
		return c.VisitRecursiveShape(shapeType)
	default:
		var zero T
		return zero
	}
}

func (c *JSONSchemaConverter[T]) VisitObjectShape(s *raml.ObjectShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()

	schema.Type = raml.TypeObject
	schema.MinProperties = raml.ScalarFacetValPtr(s.MinProperties)
	schema.MaxProperties = raml.ScalarFacetValPtr(s.MaxProperties)
	schema.AdditionalProperties = raml.ScalarFacetValPtr(s.AdditionalProperties)

	if l := s.Properties.Len(); l > 0 {
		schema.Properties = orderedmap.New[string, T](l)
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
		schema.PatternProperties = orderedmap.New[string, T](l)
		for pair := s.PatternProperties.Oldest(); pair != nil; pair = pair.Next() {
			k := pair.Key
			k = k[1 : len(k)-1]
			schema.PatternProperties.Set(k, c.Visit(pair.Value.Base.Shape))
		}
	}
	return node
}

func (c *JSONSchemaConverter[T]) VisitArrayShape(s *raml.ArrayShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()

	schema.Type = raml.TypeArray
	schema.MinItems = raml.ScalarFacetValPtr(s.MinItems)
	schema.MaxItems = raml.ScalarFacetValPtr(s.MaxItems)
	schema.UniqueItems = raml.ScalarFacetValPtr(s.UniqueItems)

	if s.Items != nil {
		schema.Items = c.Visit(s.Items.Shape)
	}
	return node
}

func (c *JSONSchemaConverter[T]) VisitUnionShape(s *raml.UnionShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()

	schema.AnyOf = make([]T, len(s.AnyOf))
	for i, item := range s.AnyOf {
		schema.AnyOf[i] = c.Visit(item.Shape)
	}
	return node
}

func (c *JSONSchemaConverter[T]) VisitStringShape(s *raml.StringShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeString
	schema.MinLength = raml.ScalarFacetValPtr(s.MinLength)
	schema.MaxLength = raml.ScalarFacetValPtr(s.MaxLength)
	if s.Pattern != nil {
		schema.Pattern = s.Pattern.Value.String()
	}
	return node
}

func (c *JSONSchemaConverter[T]) VisitIntegerShape(s *raml.IntegerShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeInteger
	if s.Minimum != nil {
		schema.Minimum = json.Number(s.Minimum.Value.String())
	}
	if s.Maximum != nil {
		schema.Maximum = json.Number(s.Maximum.Value.String())
	}
	if s.MultipleOf != nil {
		schema.MultipleOf = ratToJSONNumber(s.MultipleOf.Value)
	}
	// TODO: JSON Schema does not have a format for numbers
	return node
}

// ratToJSONNumber formats a *big.Rat as a JSON number string.
// Integer-valued rationals (denominator == 1) are emitted as plain integers;
// fractions fall back to the shortest float64 decimal representation.
func ratToJSONNumber(r *big.Rat) json.Number {
	if r.IsInt() {
		return json.Number(r.Num().String())
	}
	f, _ := r.Float64()
	return json.Number(strconv.FormatFloat(f, 'f', -1, 64))
}

func (c *JSONSchemaConverter[T]) VisitNumberShape(s *raml.NumberShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeNumber
	if s.Minimum != nil {
		schema.Minimum = ratToJSONNumber(s.Minimum.Value)
	}
	if s.Maximum != nil {
		schema.Maximum = ratToJSONNumber(s.Maximum.Value)
	}
	if s.MultipleOf != nil {
		schema.MultipleOf = ratToJSONNumber(s.MultipleOf.Value)
	}
	// TODO: JSON Schema does not have a format for numbers
	return node
}

func (c *JSONSchemaConverter[T]) VisitFileShape(s *raml.FileShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeString
	schema.MinLength = raml.ScalarFacetValPtr(s.MinLength)
	schema.MaxLength = raml.ScalarFacetValPtr(s.MaxLength)
	schema.ContentEncoding = "base64"

	// TODO: JSON Schema allows for only one content media type
	if s.FileTypes != nil {
		schema.ContentMediaType = s.FileTypes[0].Value
	}
	return node
}

func (c *JSONSchemaConverter[T]) VisitBooleanShape(s *raml.BooleanShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeBoolean
	return node
}

func (c *JSONSchemaConverter[T]) VisitDateTimeShape(s *raml.DateTimeShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
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
	return node
}

func (c *JSONSchemaConverter[T]) VisitDateTimeOnlyShape(s *raml.DateTimeOnlyShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeString
	schema.Pattern = "^[0-9]{4}-(?:0[0-9]|1[0-2])-(?:[0-2][0-9]|3[01])T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]$"
	return node
}

func (c *JSONSchemaConverter[T]) VisitDateOnlyShape(s *raml.DateOnlyShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeString
	schema.Format = raml.FormatDate
	return node
}

func (c *JSONSchemaConverter[T]) VisitTimeOnlyShape(s *raml.TimeOnlyShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeString
	schema.Format = raml.FormatTime
	return node
}

func (c *JSONSchemaConverter[T]) VisitAnyShape(s *raml.AnyShape) T {
	return c.makeSchemaFromBaseShape(s.Base())
}

func (c *JSONSchemaConverter[T]) VisitNilShape(s *raml.NilShape) T {
	node := c.makeSchemaFromBaseShape(s.Base())
	schema := node.Generic()
	schema.Type = raml.TypeNull
	return node
}

func (c *JSONSchemaConverter[T]) VisitRecursiveShape(s *raml.RecursiveShape) T {
	// NOTE: Recursive schema will always produce ref.
	// Ref ignores all other keywords defined within the schema per JSON Schema spec.

	// NOTE: We create empty schema because all base RAML types are allowed to have
	// custom facets which can be recursive.
	// The use of `makeSchemaFromBaseShape` will lead to infinite recursion.
	node := c.makeEmptySchema()
	schema := node.Generic()

	head := s.Head.Shape
	baseHead := head.Base()
	// TODO: Type name is not unique, need pretty naming to avoid collisions.
	definition := baseHead.Name
	if _, ok := c.definitions[definition]; !ok {
		// NOTE: Assign empty schema to definitions to occupy the name before traversing.
		var placeholder T
		c.definitions[definition] = placeholder
		c.definitions[definition] = c.Visit(head)
	}
	schema.Ref = "#/definitions/" + definition

	return node
}

func (c *JSONSchemaConverter[T]) VisitJSONShape(s *raml.JSONShape) T {
	view, err := s.AsShape()
	if err != nil {
		c.err = err
		var zero T
		return zero
	}
	if view == nil {
		// No compiled validator present; emit a bare schema derived from the RAML base.
		return c.makeSchemaFromBaseShape(s.Base())
	}
	// Validator-based path: the structured view carries the type constraints.
	// RAML-level metadata on the outer shape takes priority over JSON Schema annotations.
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
	viewBase.CustomShapeFacetDefinitions = ramlBase.CustomShapeFacetDefinitions
	viewBase.CustomShapeFacets = ramlBase.CustomShapeFacets
	return c.Visit(view)
}

func (c *JSONSchemaConverter[T]) recast(src *JSONSchema) T {
	var zero T
	if src == nil {
		return zero
	}

	core := &JSONSchemaGeneric[T]{
		Version: src.Version,
		ID:      src.ID,
		Ref:     src.Ref,
		Comment: src.Comment,

		If:   c.recast(src.If),
		Then: c.recast(src.Then),
		Else: c.recast(src.Else),
		Not:  c.recast(src.Not),

		Items: c.recast(src.Items),

		AdditionalProperties: src.AdditionalProperties,
		PropertyNames:        c.recast(src.PropertyNames),
		Type:                 src.Type,
		Enum:                 src.Enum,
		Const:                src.Const,
		MultipleOf:           src.MultipleOf,
		Maximum:              src.Maximum,
		Minimum:              src.Minimum,
		MaxLength:            src.MaxLength,
		MinLength:            src.MinLength,
		Pattern:              src.Pattern,
		MaxItems:             src.MaxItems,
		MinItems:             src.MinItems,
		UniqueItems:          src.UniqueItems,
		MaxContains:          src.MaxContains,
		MinContains:          src.MinContains,
		MaxProperties:        src.MaxProperties,
		MinProperties:        src.MinProperties,
		Required:             src.Required,
		ContentEncoding:      src.ContentEncoding,
		ContentMediaType:     src.ContentMediaType,
		Format:               src.Format,

		Title:       src.Title,
		Description: src.Description,
		Default:     src.Default,
		Examples:    src.Examples,
	}

	if len(src.AnyOf) > 0 {
		core.AnyOf = make([]T, len(src.AnyOf))
		for i, it := range src.AnyOf {
			core.AnyOf[i] = c.recast(it)
		}
	}
	if len(src.AllOf) > 0 {
		core.AllOf = make([]T, len(src.AllOf))
		for i, it := range src.AllOf {
			core.AllOf[i] = c.recast(it)
		}
	}
	if len(src.OneOf) > 0 {
		core.OneOf = make([]T, len(src.OneOf))
		for i, it := range src.OneOf {
			core.OneOf[i] = c.recast(it)
		}
	}
	if l := src.Properties.Len(); l > 0 {
		core.Properties = orderedmap.New[string, T](l)
		for p := src.Properties.Oldest(); p != nil; p = p.Next() {
			core.Properties.Set(p.Key, c.recast(p.Value))
		}
	}
	if l := src.PatternProperties.Len(); l > 0 {
		core.PatternProperties = orderedmap.New[string, T](l)
		for p := src.PatternProperties.Oldest(); p != nil; p = p.Next() {
			core.PatternProperties.Set(p.Key, c.recast(p.Value))
		}
	}
	if l := len(src.Definitions); l > 0 {
		core.Definitions = make(map[string]T, l)
		for k, v := range src.Definitions {
			core.Definitions[k] = c.recast(v)
		}
	}
	if c.opts.wrap != nil {
		return c.opts.wrap(c, core, nil)
	}
	return any(core).(T)
}

func (c *JSONSchemaConverter[T]) makeEmptySchema() T {
	core := &JSONSchemaGeneric[T]{}
	if c.opts.wrap != nil {
		return c.opts.wrap(c, core, nil)
	}
	return any(core).(T)
}

func (c *JSONSchemaConverter[T]) makeSchemaFromBaseShape(base *raml.BaseShape) T {
	core := &JSONSchemaGeneric[T]{}
	if base.DisplayName != nil {
		core.Title = base.DisplayName.Value
	}
	if base.Description != nil {
		core.Description = base.Description.Value
	}
	if base.Default != nil {
		core.Default = base.Default.Value.Raw
	}
	if base.Examples != nil {
		for pair := base.Examples.Map.Oldest(); pair != nil; pair = pair.Next() {
			ex := pair.Value
			core.Examples = append(core.Examples, ex.Data.Value.Raw)
		}
	}
	if base.Example != nil {
		core.Examples = []any{base.Example.Data.Value.Raw}
	}
	if base.Enum != nil {
		core.Enum = make([]any, len(base.Enum))
		for i, v := range base.Enum {
			core.Enum[i] = v.Value.Raw
		}
	}
	if c.opts.wrap != nil {
		return c.opts.wrap(c, core, base)
	}
	return any(core).(T)
}
