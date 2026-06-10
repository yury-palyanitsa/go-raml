package converter

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	raml "github.com/acronis/go-raml/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

func TestRAMLConverter_Convert_String(t *testing.T) {
	s := &raml.StringShape{
		BaseShape: &raml.BaseShape{
			Type: raml.TypeString,
		},
		StringFacets: raml.StringFacets{
			LengthFacets: raml.LengthFacets{
				MinLength: scalarFacetOf(uint64(1)),
				MaxLength: scalarFacetOf(uint64(100)),
			},
			Pattern: scalarFacetOf(regexp.MustCompile(`^[A-Z]`)),
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)

	assert.Equal(t, raml.TypeString, decl["type"])
	assert.Equal(t, uint64(1), decl["minLength"])
	assert.Equal(t, uint64(100), decl["maxLength"])
	assert.Equal(t, `^[A-Z]`, decl["pattern"])
}

func TestRAMLConverter_Convert_Integer(t *testing.T) {
	s := &raml.IntegerShape{
		BaseShape: &raml.BaseShape{
			Type: raml.TypeInteger,
		},
		IntegerFacets: raml.IntegerFacets{
			Minimum:    scalarFacetOf(big.NewInt(0)),
			Maximum:    scalarFacetOf(big.NewInt(255)),
			MultipleOf: scalarFacetOf(new(big.Rat).SetInt64(2)),
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)

	assert.Equal(t, raml.TypeInteger, decl["type"])
	assert.Equal(t, int64(0), decl["minimum"])
	assert.Equal(t, int64(255), decl["maximum"])
	assert.Equal(t, int64(2), decl["multipleOf"])
}

func TestRAMLConverter_Convert_Number(t *testing.T) {
	s := &raml.NumberShape{
		BaseShape: &raml.BaseShape{
			Type: raml.TypeNumber,
		},
		NumberFacets: raml.NumberFacets{
			Minimum: scalarFacetOf(ratOf("1.5")),
			Maximum: scalarFacetOf(ratOf("99.9")),
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)

	assert.Equal(t, raml.TypeNumber, decl["type"])
	assert.InDelta(t, 1.5, decl["minimum"], 1e-9)
	assert.InDelta(t, 99.9, decl["maximum"], 1e-9)
}

func TestRAMLConverter_Convert_Boolean(t *testing.T) {
	s := &raml.BooleanShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeBoolean},
	}
	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, raml.TypeBoolean, decl["type"])
}

func TestRAMLConverter_Convert_Nil(t *testing.T) {
	s := &raml.NilShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeNil},
	}
	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, raml.TypeNil, decl["type"])
}

func TestRAMLConverter_Convert_Any(t *testing.T) {
	s := &raml.AnyShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeAny},
	}
	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, raml.TypeAny, decl["type"])
}

func TestRAMLConverter_Convert_Object(t *testing.T) {
	props := orderedmap.New[string, raml.Property](2)
	props.Set("id", raml.Property{
		Name: "id",
		Base: &raml.BaseShape{
			Type:  raml.TypeString,
			Shape: &raml.StringShape{BaseShape: &raml.BaseShape{Type: raml.TypeString}},
		},
		Required: true,
	})
	props.Set("age", raml.Property{
		Name: "age",
		Base: &raml.BaseShape{
			Type:  raml.TypeInteger,
			Shape: &raml.IntegerShape{BaseShape: &raml.BaseShape{Type: raml.TypeInteger}},
		},
		Required: false,
	})

	s := &raml.ObjectShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeObject},
		ObjectFacets: raml.ObjectFacets{
			Properties:           props,
			AdditionalProperties: scalarFacetOf(false),
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)

	assert.Equal(t, raml.TypeObject, decl["type"])
	assert.Equal(t, false, decl["additionalProperties"])

	propsDecl, ok := decl["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, propsDecl, "id")
	assert.Contains(t, propsDecl, "age")

	// required property should not have "required: false"
	idDecl := propsDecl["id"].(map[string]any)
	assert.NotContains(t, idDecl, "required")

	// optional property should have "required: false"
	ageDecl := propsDecl["age"].(map[string]any)
	assert.Equal(t, false, ageDecl["required"])
}

func TestRAMLConverter_Convert_Array(t *testing.T) {
	s := &raml.ArrayShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeArray},
		ArrayFacets: raml.ArrayFacets{
			Items: &raml.BaseShape{
				Type:  raml.TypeString,
				Shape: &raml.StringShape{BaseShape: &raml.BaseShape{Type: raml.TypeString}},
			},
			MinItems:    scalarFacetOf(uint64(1)),
			MaxItems:    scalarFacetOf(uint64(10)),
			UniqueItems: scalarFacetOf(true),
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)

	assert.Equal(t, raml.TypeArray, decl["type"])
	assert.Equal(t, uint64(1), decl["minItems"])
	assert.Equal(t, uint64(10), decl["maxItems"])
	assert.Equal(t, true, decl["uniqueItems"])
	items := decl["items"].(map[string]any)
	assert.Equal(t, raml.TypeString, items["type"])
}

func TestRAMLConverter_Convert_Union(t *testing.T) {
	s := &raml.UnionShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeUnion},
		UnionFacets: raml.UnionFacets{
			AnyOf: []*raml.BaseShape{
				{Type: raml.TypeString},
				{Type: raml.TypeInteger},
			},
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, "string | integer", decl["type"])
}

func TestRAMLConverter_Convert_Union_Named(t *testing.T) {
	s := &raml.UnionShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeUnion},
		UnionFacets: raml.UnionFacets{
			AnyOf: []*raml.BaseShape{
				{Name: "Cat", Type: raml.TypeObject},
				{Name: "Dog", Type: raml.TypeObject},
			},
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, "Cat | Dog", decl["type"])
}

func TestRAMLConverter_Convert_DateTime(t *testing.T) {
	s := &raml.DateTimeShape{
		BaseShape:    &raml.BaseShape{Type: raml.TypeDatetime},
		FormatFacets: raml.FormatFacets{Format: scalarFacetOf(raml.DateTimeFormatRFC3339)},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, raml.TypeDatetime, decl["type"])
	assert.Equal(t, raml.DateTimeFormatRFC3339, decl["format"])
}

func TestRAMLConverter_Convert_BaseAnnotations(t *testing.T) {
	s := &raml.StringShape{
		BaseShape: &raml.BaseShape{
			Type:        raml.TypeString,
			DisplayName: scalarFacetOf("My Field"),
			Description: scalarFacetOf("A description"),
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, "My Field", decl["displayName"])
	assert.Equal(t, "A description", decl["description"])
}

func TestRAMLConverter_ConvertDataType_YAML(t *testing.T) {
	s := &raml.IntegerShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeInteger},
		IntegerFacets: raml.IntegerFacets{
			Minimum: scalarFacetOf(big.NewInt(0)),
		},
	}

	c := NewRAMLConverter()
	out, err := c.ConvertDataType(s)
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "#%RAML 1.0 DataType")
	assert.Contains(t, text, "type: integer")
	assert.Contains(t, text, "minimum: 0")
}

func TestRAMLConverter_ConvertLibrary_YAML(t *testing.T) {
	strBase := &raml.BaseShape{
		Name:  "MyStr",
		Type:  raml.TypeString,
		Shape: &raml.StringShape{BaseShape: &raml.BaseShape{Type: raml.TypeString}},
	}
	strBase.Shape.(*raml.StringShape).BaseShape = strBase

	types := orderedmap.New[string, *raml.BaseShape](1)
	types.Set("MyStr", strBase)

	c := NewRAMLConverter()
	out, err := c.ConvertLibrary(types)
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "#%RAML 1.0 Library")
	assert.Contains(t, text, "types:")
	assert.Contains(t, text, "MyStr:")
	assert.Contains(t, text, "type: string")
}

func TestRAMLConverter_Convert_Nil_Shape(t *testing.T) {
	c := NewRAMLConverter()
	_, err := c.Convert(nil)
	assert.Error(t, err)
}

func TestRAMLConverter_ConvertJSONSchema_DataType(t *testing.T) {
	schema := []byte(`{"type":"string","minLength":1,"maxLength":100}`)
	fakeFile := filepath.Join(os.TempDir(), "test_schema.json")

	c := NewRAMLConverter()
	out, err := c.ConvertJSONSchema(schema, fakeFile)
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "#%RAML 1.0 DataType")
	assert.Contains(t, text, "type: string")
	assert.Contains(t, text, "minLength:")
	assert.Contains(t, text, "maxLength:")
}

func TestRAMLConverter_ConvertJSONSchema_Library_definitions(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"street": {"type": "string"},
			"city": {"type": "string"}
		},
		"description": "A mailing address"
	}`)
	fakeFile := filepath.Join(os.TempDir(), "address.json")

	c := NewRAMLConverter()
	out, err := c.ConvertJSONSchema(schema, fakeFile)
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "#%RAML 1.0 DataType")
	assert.Contains(t, text, "type: object")
	assert.Contains(t, text, "properties:")
	assert.Contains(t, text, "street:")
	assert.Contains(t, text, "city:")
}

func TestRAMLConverter_ConvertJSONSchema_Library_defs(t *testing.T) {
	schema := []byte(`{
		"type": "integer",
		"minimum": 0
	}`)
	fakeFile := filepath.Join(os.TempDir(), "foo_schema.json")

	c := NewRAMLConverter()
	out, err := c.ConvertJSONSchema(schema, fakeFile)
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "#%RAML 1.0 DataType")
	assert.Contains(t, text, "type: integer")
	assert.Contains(t, text, "minimum:")
}

func TestRAMLConverter_ConvertJSONSchema_RootIncludedWhenHasContent(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"id": {"type": "integer"},
			"name": {"type": "string"}
		}
	}`)
	fakeFile := filepath.Join(os.TempDir(), "root_model.json")

	c := NewRAMLConverter()
	out, err := c.ConvertJSONSchema(schema, fakeFile)
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "#%RAML 1.0 DataType")
	assert.Contains(t, text, "type: object")
	assert.Contains(t, text, "properties:")
	assert.Contains(t, text, "id:")
	assert.Contains(t, text, "name:")
}

func TestRAMLConverter_ConvertJSONSchema_InvalidJSON(t *testing.T) {
	fakeFile := filepath.Join(os.TempDir(), "bad.json")
	c := NewRAMLConverter()
	_, err := c.ConvertJSONSchema([]byte(`{bad json`), fakeFile)
	assert.Error(t, err)
}

func TestRAMLConverter_ConvertJSONSchema_RemoteRef(t *testing.T) {
	// Serve a remote $ref schema over HTTP.
	addressSchema := []byte(`{"type":"object","properties":{"street":{"type":"string"},"city":{"type":"string"}}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(addressSchema)
	}))
	defer srv.Close()

	// Root schema references the remote address schema.
	rootSchema := []byte(`{
		"type": "object",
		"properties": {
			"address": { "$ref": "` + srv.URL + `/address.json" }
		}
	}`)
	fakeFile := filepath.Join(os.TempDir(), "root_remote.json")

	c := NewRAMLConverter()
	out, err := c.ConvertJSONSchema(rootSchema, fakeFile, WithHTTPClient(srv.Client()))
	require.NoError(t, err)

	text := string(out)
	assert.Contains(t, text, "RAML 1.0")
	assert.Contains(t, text, "type: object")
}

func TestRAMLConverter_ConvertJSONSchema_RemoteRef_WithoutHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"string"}`))
	}))
	defer srv.Close()

	rootSchema := []byte(`{"$ref": "` + srv.URL + `/remote.json"}`)
	fakeFile := filepath.Join(os.TempDir(), "no_client.json")

	c := NewRAMLConverter()
	_, err := c.ConvertJSONSchema(rootSchema, fakeFile) // no WithHTTPClient
	assert.Error(t, err, "expected error when HTTP client is not provided for remote $ref")
}

func TestRAMLConverter_Convert_File(t *testing.T) {
	minLen := uint64(0)
	maxLen := uint64(1024)
	s := &raml.FileShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeFile},
		LengthFacets: raml.LengthFacets{
			MinLength: scalarFacetOf(minLen),
			MaxLength: scalarFacetOf(maxLen),
		},
		FileFacets: raml.FileFacets{
			FileTypes: []*raml.Node[string]{
				{Value: "application/pdf"},
				{Value: "image/png"},
			},
		},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)

	assert.Equal(t, raml.TypeFile, decl["type"])
	assert.Equal(t, uint64(0), decl["minLength"])
	assert.Equal(t, uint64(1024), decl["maxLength"])
	fileTypes := decl["fileTypes"].([]string)
	assert.Len(t, fileTypes, 2)
	assert.Equal(t, "application/pdf", fileTypes[0])
	assert.Equal(t, "image/png", fileTypes[1])
}

func TestRAMLConverter_Convert_DateTimeOnly(t *testing.T) {
	s := &raml.DateTimeOnlyShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeDatetimeOnly},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, raml.TypeDatetimeOnly, decl["type"])
}

func TestRAMLConverter_Convert_DateOnly(t *testing.T) {
	s := &raml.DateOnlyShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeDateOnly},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, raml.TypeDateOnly, decl["type"])
}

func TestRAMLConverter_Convert_TimeOnly(t *testing.T) {
	s := &raml.TimeOnlyShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeTimeOnly},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, raml.TypeTimeOnly, decl["type"])
}

func TestRAMLConverter_Convert_Recursive(t *testing.T) {
	head := &raml.BaseShape{
		Name: "TreeNode",
		Type: raml.TypeObject,
		Shape: &raml.ObjectShape{
			BaseShape: &raml.BaseShape{Type: raml.TypeObject, Name: "TreeNode"},
		},
	}
	head.Shape.(*raml.ObjectShape).BaseShape = head

	s := &raml.RecursiveShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeRecursive, Name: "TreeNode"},
		Head:      head,
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, "TreeNode", decl["type"])
}

func TestRAMLConverter_Convert_Recursive_NoHead(t *testing.T) {
	s := &raml.RecursiveShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeRecursive, Name: "Anonymous"},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	assert.Equal(t, raml.TypeAny, decl["type"])
}

func TestRAMLConverter_Convert_JSONShape_Fallback(t *testing.T) {
	s := &raml.JSONShape{
		BaseShape: &raml.BaseShape{Type: raml.TypeJSON},
	}

	c := NewRAMLConverter()
	decl, err := c.Convert(s)
	require.NoError(t, err)
	// When AsShape() fails (no compiled validator), the raw JSON is emitted as the type.
	assert.NotNil(t, decl["type"])
}
