package converter

import (
	"encoding/json"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// OAS3Schema represents an OpenAPI 3.0 Schema Object.
//
// The OAS 3.0 schema dialect is a superset/subset of JSON Schema Draft 4 with
// additional keywords (nullable, discriminator, xml, readOnly, writeOnly) and
// a smaller allowed keyword set (no $schema/$id/$comment/if/then/else).
//
// https://spec.openapis.org/oas/v3.0.3#schema-object
type OAS3Schema struct {
	Ref string `json:"$ref,omitempty" yaml:"$ref,omitempty"`

	// Combination
	AllOf []*OAS3Schema `json:"allOf,omitempty" yaml:"allOf,omitempty"`
	AnyOf []*OAS3Schema `json:"anyOf,omitempty" yaml:"anyOf,omitempty"`
	OneOf []*OAS3Schema `json:"oneOf,omitempty" yaml:"oneOf,omitempty"`
	Not   *OAS3Schema   `json:"not,omitempty"   yaml:"not,omitempty"`

	// Type
	Type     string `json:"type,omitempty"     yaml:"type,omitempty"`
	Nullable bool   `json:"nullable,omitempty" yaml:"nullable,omitempty"`
	Format   string `json:"format,omitempty"   yaml:"format,omitempty"`

	// Generic constraints
	Enum []any `json:"enum,omitempty" yaml:"enum,omitempty"`

	// Object
	Properties           *orderedmap.OrderedMap[string, *OAS3Schema] `json:"properties,omitempty"           yaml:"properties,omitempty"`
	AdditionalProperties *bool                                       `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
	Required             []string                                    `json:"required,omitempty"             yaml:"required,omitempty"`
	MinProperties        *uint64                                     `json:"minProperties,omitempty"        yaml:"minProperties,omitempty"`
	MaxProperties        *uint64                                     `json:"maxProperties,omitempty"        yaml:"maxProperties,omitempty"`
	Discriminator        *OAS3Discriminator                          `json:"discriminator,omitempty"        yaml:"discriminator,omitempty"`

	// patternProperties is not part of the OAS 3.0 spec but is widely accepted by
	// tooling. It is included here and emitted as-is; generators targeting strict
	// OAS 3.0 parsers should post-process or drop this field.
	PatternProperties *orderedmap.OrderedMap[string, *OAS3Schema] `json:"patternProperties,omitempty" yaml:"patternProperties,omitempty"`

	// Array
	Items       *OAS3Schema `json:"items,omitempty"       yaml:"items,omitempty"`
	MinItems    *uint64     `json:"minItems,omitempty"    yaml:"minItems,omitempty"`
	MaxItems    *uint64     `json:"maxItems,omitempty"    yaml:"maxItems,omitempty"`
	UniqueItems *bool       `json:"uniqueItems,omitempty" yaml:"uniqueItems,omitempty"`

	// Numeric
	Minimum    json.Number `json:"minimum,omitempty"    yaml:"minimum,omitempty"`
	Maximum    json.Number `json:"maximum,omitempty"    yaml:"maximum,omitempty"`
	MultipleOf json.Number `json:"multipleOf,omitempty" yaml:"multipleOf,omitempty"`

	// String
	MinLength *uint64 `json:"minLength,omitempty" yaml:"minLength,omitempty"`
	MaxLength *uint64 `json:"maxLength,omitempty" yaml:"maxLength,omitempty"`
	Pattern   string  `json:"pattern,omitempty"   yaml:"pattern,omitempty"`

	// Metadata
	Title       string `json:"title,omitempty"       yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Default     any    `json:"default,omitempty"     yaml:"default,omitempty"`
	// OAS 3.0 uses a singular example (not the JSON Schema array form).
	Example any `json:"example,omitempty" yaml:"example,omitempty"`

	// OAS3-specific
	ReadOnly  bool     `json:"readOnly,omitempty"  yaml:"readOnly,omitempty"`
	WriteOnly bool     `json:"writeOnly,omitempty" yaml:"writeOnly,omitempty"`
	XML       *OAS3XML `json:"xml,omitempty"       yaml:"xml,omitempty"`

	// Extensions holds x- fields sourced from RAML annotations and non-standard
	// features. They are marshalled inline next to the other schema fields.
	Extensions map[string]any `json:"-" yaml:"-"`
}

// MarshalJSON implements custom JSON marshalling so that Extensions are emitted
// inline alongside the struct fields rather than nested under an "extensions" key.
func (s *OAS3Schema) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("null"), nil
	}
	// Use an alias to avoid infinite recursion.
	type Alias OAS3Schema
	base, err := json.Marshal((*Alias)(s))
	if err != nil {
		return nil, err
	}
	return mergeJSONExtensions(base, s.Extensions)
}

// OAS3Discriminator represents the OAS 3.0 Discriminator Object.
//
// https://spec.openapis.org/oas/v3.0.3#discriminator-object
type OAS3Discriminator struct {
	PropertyName string            `json:"propertyName"      yaml:"propertyName"`
	Mapping      map[string]string `json:"mapping,omitempty" yaml:"mapping,omitempty"`
}

// OAS3XML represents the OAS 3.0 XML Object, mirroring the RAML xml facet.
//
// https://spec.openapis.org/oas/v3.0.3#xml-object
type OAS3XML struct {
	Name      string `json:"name,omitempty"      yaml:"name,omitempty"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Prefix    string `json:"prefix,omitempty"    yaml:"prefix,omitempty"`
	Attribute bool   `json:"attribute,omitempty" yaml:"attribute,omitempty"`
	Wrapped   bool   `json:"wrapped,omitempty"   yaml:"wrapped,omitempty"`
}
