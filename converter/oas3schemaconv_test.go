package converter

import (
	"encoding/json"
	"math/big"
	"testing"

	raml "github.com/acronis/go-raml/v3"
	"github.com/stretchr/testify/require"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// ---------------------------------------------------------------------------
// OAS3SchemaConverter.Convert — entry point (positive + negative)
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_Convert_Positive(t *testing.T) {
	s := &raml.StringShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeString, Name: "test"},
	}
	s.Base().SetUnwrapped()

	schema, err := NewOAS3SchemaConverter().Convert(s)
	require.NoError(t, err)
	require.Equal(t, "string", schema.Type)
}

func TestOAS3SchemaConverter_Convert_Negative_NotUnwrapped(t *testing.T) {
	s := &raml.StringShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeString, Name: "test"},
	}
	// Not unwrapped — should fail.
	_, err := NewOAS3SchemaConverter().Convert(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be unwrapped")
}

// ---------------------------------------------------------------------------
// visitNumber
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitNumber(t *testing.T) {
	s := &raml.NumberShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeNumber},
		NumberFacets: raml.NumberFacets{
			Minimum:    scalarFacetOf(ratOf("1.5")),
			Maximum:    scalarFacetOf(ratOf("99.9")),
			MultipleOf: scalarFacetOf(ratOf("3")),
		},
		FormatFacets: raml.FormatFacets{
			Format: scalarFacetOf("float"),
		},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "number", schema.Type)
	require.Equal(t, "1.5", string(schema.Minimum))
	require.Equal(t, "99.9", string(schema.Maximum))
	require.Equal(t, "3", string(schema.MultipleOf))
	require.Equal(t, "float", schema.Format)
}

// ---------------------------------------------------------------------------
// visitBoolean
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitBoolean(t *testing.T) {
	s := &raml.BooleanShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeBoolean},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "boolean", schema.Type)
}

// ---------------------------------------------------------------------------
// visitFile → type:string, format:binary
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitFile(t *testing.T) {
	minLen := uint64(0)
	maxLen := uint64(1024)
	s := &raml.FileShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeFile},
		LengthFacets: raml.LengthFacets{
			MinLength: scalarFacetOf(minLen),
			MaxLength: scalarFacetOf(maxLen),
		},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "string", schema.Type)
	require.Equal(t, "binary", schema.Format)
	require.Equal(t, minLen, *schema.MinLength)
	require.Equal(t, maxLen, *schema.MaxLength)
}

// ---------------------------------------------------------------------------
// visitDateTime — rfc3339 → format:date-time
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitDateTime_RFC3339(t *testing.T) {
	s := &raml.DateTimeShape{
		BaseShape:    &raml.BaseShape{Type: raml.TypeDatetime},
		FormatFacets: raml.FormatFacets{Format: scalarFacetOf(raml.DateTimeFormatRFC3339)},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "string", schema.Type)
	require.Equal(t, "date-time", schema.Format)
}

func TestOAS3SchemaConverter_visitDateTime_RFC2616(t *testing.T) {
	s := &raml.DateTimeShape{
		BaseShape:    &raml.BaseShape{Type: raml.TypeDatetime},
		FormatFacets: raml.FormatFacets{Format: scalarFacetOf(raml.DateTimeFormatRFC2616)},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "string", schema.Type)
	require.Empty(t, schema.Format)
	require.Contains(t, schema.Pattern, "Mon|Tue|Wed")
}

func TestOAS3SchemaConverter_visitDateTime_Default(t *testing.T) {
	s := &raml.DateTimeShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeDatetime},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "date-time", schema.Format)
}

// ---------------------------------------------------------------------------
// visitDateTimeOnly
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitDateTimeOnly(t *testing.T) {
	s := &raml.DateTimeOnlyShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeDatetimeOnly},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "string", schema.Type)
	require.Contains(t, schema.Pattern, "T")
}

// ---------------------------------------------------------------------------
// visitDateOnly
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitDateOnly(t *testing.T) {
	s := &raml.DateOnlyShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeDateOnly},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "string", schema.Type)
	require.Equal(t, "date", schema.Format)
}

// ---------------------------------------------------------------------------
// visitTimeOnly
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitTimeOnly(t *testing.T) {
	s := &raml.TimeOnlyShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeTimeOnly},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "string", schema.Type)
	require.Equal(t, "time", schema.Format)
}

// ---------------------------------------------------------------------------
// visitNil — OAS3 has no type:null, uses nullable:true
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitNil(t *testing.T) {
	s := &raml.NilShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeNil},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.True(t, schema.Nullable)
	require.Empty(t, schema.Type)
}

// ---------------------------------------------------------------------------
// visitRecursive — emits $ref and registers in components
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitRecursive(t *testing.T) {
	head := &raml.BaseShape{
		Name: "TreeNode",
		Type: raml.TypeObject,
		Shape: &raml.ObjectShape{
			BaseShape: &raml.BaseShape{Type: raml.TypeObject, Name: "TreeNode"},
		},
	}
	head.Shape.(*raml.ObjectShape).BaseShape = head
	head.SetUnwrapped()

	s := &raml.RecursiveShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeRecursive, Name: "TreeNode"},
		Head:      head,
	}
	s.Base().SetUnwrapped()

	conv := NewOAS3SchemaConverter()
	schema := conv.Visit(s)
	require.Equal(t, "#/components/schemas/TreeNode", schema.Ref)

	// TreeNode should be registered in components.
	compSchema, ok := conv.Components().Get("TreeNode")
	require.True(t, ok)
	require.Equal(t, "object", compSchema.Type)
}

// ---------------------------------------------------------------------------
// applyOAS3IntegerFormat — int8/int16 → bounds, int32/int64 → format
// ---------------------------------------------------------------------------

func TestApplyOAS3IntegerFormat_Int8(t *testing.T) {
	schema := &OAS3Schema{}
	applyOAS3IntegerFormat(schema, "int8")
	require.Empty(t, schema.Format)
	require.Equal(t, "-128", string(schema.Minimum))
	require.Equal(t, "127", string(schema.Maximum))
}

func TestApplyOAS3IntegerFormat_Int16(t *testing.T) {
	schema := &OAS3Schema{}
	applyOAS3IntegerFormat(schema, "int16")
	require.Empty(t, schema.Format)
	require.Equal(t, "-32768", string(schema.Minimum))
	require.Equal(t, "32767", string(schema.Maximum))
}

func TestApplyOAS3IntegerFormat_Int32(t *testing.T) {
	schema := &OAS3Schema{}
	applyOAS3IntegerFormat(schema, "int32")
	require.Equal(t, "int32", schema.Format)
}

func TestApplyOAS3IntegerFormat_Int64(t *testing.T) {
	schema := &OAS3Schema{}
	applyOAS3IntegerFormat(schema, "int64")
	require.Equal(t, "int64", schema.Format)
}

func TestApplyOAS3IntegerFormat_Int_Alias(t *testing.T) {
	schema := &OAS3Schema{}
	applyOAS3IntegerFormat(schema, "int")
	require.Equal(t, "int32", schema.Format)
}

func TestApplyOAS3IntegerFormat_Long_Alias(t *testing.T) {
	schema := &OAS3Schema{}
	applyOAS3IntegerFormat(schema, "long")
	require.Equal(t, "int64", schema.Format)
}

func TestApplyOAS3IntegerFormat_RespectsExplicitBounds(t *testing.T) {
	// If explicit min/max are set, int8/int16 should NOT override them.
	schema := &OAS3Schema{Minimum: "0", Maximum: "100"}
	applyOAS3IntegerFormat(schema, "int8")
	// Explicit values should be preserved.
	require.Equal(t, "0", string(schema.Minimum))
	require.Equal(t, "100", string(schema.Maximum))
}

// ---------------------------------------------------------------------------
// visitInteger with format
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitInteger_Format(t *testing.T) {
	s := &raml.IntegerShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeInteger},
		IntegerFacets: raml.IntegerFacets{
			Minimum: scalarFacetOf(big.NewInt(10)),
			Maximum: scalarFacetOf(big.NewInt(50)),
		},
		FormatFacets: raml.FormatFacets{
			Format: scalarFacetOf("int32"),
		},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.Equal(t, "integer", schema.Type)
	require.Equal(t, "int32", schema.Format)
	require.Equal(t, "10", string(schema.Minimum))
	require.Equal(t, "50", string(schema.Maximum))
}

// ---------------------------------------------------------------------------
// oas3RatToNumber
// ---------------------------------------------------------------------------

func TestOas3RatToNumber_Integer(t *testing.T) {
	r := new(big.Rat).SetFrac64(6, 2) // 3
	require.Equal(t, "3", string(oas3RatToNumber(r)))
}

func TestOas3RatToNumber_Fraction(t *testing.T) {
	r := new(big.Rat).SetFrac64(1, 2) // 0.5
	require.Equal(t, "0.5", string(oas3RatToNumber(r)))
}

// ---------------------------------------------------------------------------
// oas3IntegerFormatBounds
// ---------------------------------------------------------------------------

func TestOas3IntegerFormatBounds_Int8(t *testing.T) {
	min, max, ok := oas3IntegerFormatBounds("int8")
	require.True(t, ok)
	require.Equal(t, "-128", string(min))
	require.Equal(t, "127", string(max))
}

func TestOas3IntegerFormatBounds_Int32(t *testing.T) {
	_, _, ok := oas3IntegerFormatBounds("int32")
	require.False(t, ok)
}

// ---------------------------------------------------------------------------
// oas3IntegerFormat
// ---------------------------------------------------------------------------

func TestOas3IntegerFormat_Int_Alias(t *testing.T) {
	require.Equal(t, "int32", oas3IntegerFormat("int"))
}

func TestOas3IntegerFormat_Long_Alias(t *testing.T) {
	require.Equal(t, "int64", oas3IntegerFormat("long"))
}

func TestOas3IntegerFormat_Unknown(t *testing.T) {
	require.Empty(t, oas3IntegerFormat("unknown"))
}

// ---------------------------------------------------------------------------
// oas3XMLFrom
// ---------------------------------------------------------------------------

func TestOas3XMLFrom(t *testing.T) {
	xml := oas3XMLFrom(&raml.XMLSerialization{
		Name:      nodeOf("myName"),
		Namespace: nodeOf("http://example.com"),
		Prefix:    nodeOf("ex"),
		Attribute: nodeOf(true),
		Wrapped:   nodeOf(true),
	})
	require.Equal(t, "myName", xml.Name)
	require.Equal(t, "http://example.com", xml.Namespace)
	require.Equal(t, "ex", xml.Prefix)
	require.True(t, xml.Attribute)
	require.True(t, xml.Wrapped)
}

func TestOas3XMLFrom_Nil(t *testing.T) {
	xml := oas3XMLFrom(nil)
	require.Nil(t, xml)
}

// ---------------------------------------------------------------------------
// visitUnion — pure null (all nil members)
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitUnion_PureNull(t *testing.T) {
	s := &raml.UnionShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeUnion},
		UnionFacets: raml.UnionFacets{
			AnyOf: []*raml.BaseShape{
				{Shape: &raml.NilShape{BaseShape: &raml.BaseShape{Type: raml.TypeNil}}},
			},
		},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.True(t, schema.Nullable)
	require.Empty(t, schema.Type)
	require.Nil(t, schema.AnyOf)
}

// ---------------------------------------------------------------------------
// visitUnion — multiple non-nil members (anyOf)
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitUnion_MultipleNonNil(t *testing.T) {
	s := &raml.UnionShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeUnion},
		UnionFacets: raml.UnionFacets{
			AnyOf: []*raml.BaseShape{
				{Type: raml.TypeString, Shape: &raml.StringShape{BaseShape: &raml.BaseShape{Type: raml.TypeString}}},
				{Type: raml.TypeInteger, Shape: &raml.IntegerShape{BaseShape: &raml.BaseShape{Type: raml.TypeInteger}}},
				{Type: raml.TypeBoolean, Shape: &raml.BooleanShape{BaseShape: &raml.BaseShape{Type: raml.TypeBoolean}}},
			},
		},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.False(t, schema.Nullable)
	require.Len(t, schema.AnyOf, 3)
	require.Equal(t, "string", schema.AnyOf[0].Type)
	require.Equal(t, "integer", schema.AnyOf[1].Type)
	require.Equal(t, "boolean", schema.AnyOf[2].Type)
}

// ---------------------------------------------------------------------------
// visitObject — discriminator
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitObject_Discriminator(t *testing.T) {
	s := &raml.ObjectShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeObject},
		ObjectFacets: raml.ObjectFacets{
			Discriminator: scalarFacetOf("petType"),
		},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	require.NotNil(t, schema.Discriminator)
	require.Equal(t, "petType", schema.Discriminator.PropertyName)
}

// ---------------------------------------------------------------------------
// namedTypeRef — $ref to components/schemas
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_namedTypeRef_Positive(t *testing.T) {
	// Register a named type.
	parent := &raml.BaseShape{
		Name:  "Address",
		Type:  raml.TypeObject,
		Shape: &raml.ObjectShape{BaseShape: &raml.BaseShape{Type: raml.TypeObject, Name: "Address"}},
	}
	parent.Shape.(*raml.ObjectShape).BaseShape = parent
	parent.SetUnwrapped()

	child := &raml.BaseShape{
		Name:     "HomeAddress",
		Type:     raml.TypeObject,
		Shape:    &raml.ObjectShape{BaseShape: &raml.BaseShape{}},
		Inherits: []*raml.BaseShape{parent},
	}
	child.SetUnwrapped()

	conv := NewOAS3SchemaConverter()
	conv.namedTypes["Address"] = struct{}{}

	schema := conv.namedTypeRef(child)
	require.NotNil(t, schema)
	require.Equal(t, "#/components/schemas/Address", schema.Ref)
}

func TestOAS3SchemaConverter_namedTypeRef_Negative_NotNamed(t *testing.T) {
	child := &raml.BaseShape{
		Name:  "Anonymous",
		Type:  raml.TypeString,
		Shape: &raml.StringShape{BaseShape: &raml.BaseShape{Type: raml.TypeString}},
	}
	child.SetUnwrapped()

	conv := NewOAS3SchemaConverter()
	schema := conv.namedTypeRef(child)
	require.Nil(t, schema)
}

// ---------------------------------------------------------------------------
// visitJSON — fallback path (no compiled validator)
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_visitJSON_Fallback(t *testing.T) {
	s := &raml.JSONShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeJSON},
	}
	s.Base().SetUnwrapped()

	schema := NewOAS3SchemaConverter().Visit(s)
	// When AsShape() fails (no validator), the fallback emits x-json extension.
	require.Contains(t, schema.Extensions, "x-json")
}

// ---------------------------------------------------------------------------
// baseSchema — with metadata (displayName, description, default, enum, xml)
// ---------------------------------------------------------------------------

func TestOAS3SchemaConverter_baseSchema_Metadata(t *testing.T) {
	base := &raml.BaseShape{
		Type:        raml.TypeString,
		DisplayName: scalarFacetOf("My Field"),
		Description: scalarFacetOf("A description"),
		Default:     &raml.DataNode{Value: raml.NewScalarNodeValue("default-val")},
		Enum: raml.Nodes{
			{Value: raml.NewScalarNodeValue("a")},
			{Value: raml.NewScalarNodeValue("b")},
		},
	}
	base.SetUnwrapped()

	schema := NewOAS3SchemaConverter().baseSchema(base)
	require.Equal(t, "My Field", schema.Title)
	require.Equal(t, "A description", schema.Description)
	require.Equal(t, "default-val", schema.Default)
	require.Len(t, schema.Enum, 2)
}

// ---------------------------------------------------------------------------
// mergeOAS3BaseInto — copies type-level fields from src into dst
// ---------------------------------------------------------------------------

func TestMergeOAS3BaseInto(t *testing.T) {
	dst := &OAS3Schema{Title: "Dst Title", Description: "Dst Desc"}
	src := &OAS3Schema{
		Type:        "string",
		Format:      "email",
		Description: "Src Desc",
	}
	mergeOAS3BaseInto(dst, src)

	// Metadata preserved from dst.
	require.Equal(t, "Dst Title", dst.Title)
	require.Equal(t, "Dst Desc", dst.Description)
	// Type-level fields taken from src.
	require.Equal(t, "string", dst.Type)
	require.Equal(t, "email", dst.Format)
}

// ---------------------------------------------------------------------------
// OAS3Schema.MarshalJSON — with extensions
// ---------------------------------------------------------------------------

func TestOAS3Schema_MarshalJSON_Extensions(t *testing.T) {
	schema := &OAS3Schema{
		Type:       "string",
		Format:     "email",
		Extensions: map[string]any{"x-custom": "value"},
	}
	data, err := json.Marshal(schema)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, "string", out["type"])
	require.Equal(t, "email", out["format"])
	require.Equal(t, "value", out["x-custom"])
}

func TestOAS3Schema_MarshalJSON_Nil(t *testing.T) {
	var schema *OAS3Schema
	data, err := json.Marshal(schema)
	require.NoError(t, err)
	require.Equal(t, "null", string(data))
}

// ---------------------------------------------------------------------------
// OAS3SecurityScheme.MarshalJSON — with extensions
// ---------------------------------------------------------------------------

func TestOAS3SecurityScheme_MarshalJSON_Extensions(t *testing.T) {
	ss := &OAS3SecurityScheme{
		Type:     "http",
		Scheme:   "bearer",
		Extensions: map[string]any{"x-raml-type": "Pass Through"},
	}
	data, err := json.Marshal(ss)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, "http", out["type"])
	require.Equal(t, "Pass Through", out["x-raml-type"])
}

// ---------------------------------------------------------------------------
// OAS3Document.MarshalJSON — with extensions
// ---------------------------------------------------------------------------

func TestOAS3Document_MarshalJSON_Extensions(t *testing.T) {
	doc := &OAS3Document{
		OpenAPI: "3.0.3",
		Info:    OAS3Info{Title: "Test", Version: "v1"},
		Paths:   orderedmap.New[string, *OAS3PathItem](),
		Extensions: map[string]any{"x-custom-doc": true},
	}
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, true, out["x-custom-doc"])
}

// ---------------------------------------------------------------------------
// OAS3Operation.MarshalJSON — with extensions
// ---------------------------------------------------------------------------

func TestOAS3Operation_MarshalJSON_Extensions(t *testing.T) {
	op := &OAS3Operation{
		Summary:   "Test op",
		Responses: orderedmap.New[string, *OAS3Response](),
		Extensions: map[string]any{"x-internal": "yes"},
	}
	data, err := json.Marshal(op)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, "yes", out["x-internal"])
}
