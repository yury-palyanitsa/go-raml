package converter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"path/filepath"
	"strings"

	raml "github.com/acronis/go-raml/v3"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

// JSONSchemaConvertOpt is a functional option for ConvertJSONSchema.
type JSONSchemaConvertOpt func(*jsonSchemaConvertOptions)

type jsonSchemaConvertOptions struct {
	httpClient    *http.Client
	workspaceRoot string // OS path; empty -> no workspace restriction
}

// WithHTTPClient enables fetching remote $ref targets over HTTP/HTTPS during
// JSON Schema conversion. The provided client is forwarded to the RAML parser
// so that any http:// or https:// $ref URIs are resolved over the network.
func WithHTTPClient(c *http.Client) JSONSchemaConvertOpt {
	return func(o *jsonSchemaConvertOptions) {
		o.httpClient = c
	}
}

// WithWorkspaceRoot restricts file loading to the given OS directory (and its
// subdirectories). $ref targets that resolve to paths outside the directory
// will be rejected. When not provided, ConvertJSONSchema applies no workspace
// restriction so that cross-directory $ref chains resolve freely.
func WithWorkspaceRoot(dir string) JSONSchemaConvertOpt {
	return func(o *jsonSchemaConvertOptions) {
		o.workspaceRoot = dir
	}
}

// RAMLTypeDecl is a YAML-serializable map representing a RAML type declaration body.
// It is the value that would appear under a type name in a "types:" map.
type RAMLTypeDecl = map[string]any

// RAMLConverter converts RAML Shape objects to RAML 1.0 type-declaration YAML.
//
// The primary use-case is converting a JSON Schema that was loaded as a JSONShape:
// call JSONShape.AsShape() to obtain the native RAML view, then pass that view
// to Convert() or ConvertDataType().
type RAMLConverter struct{}

// NewRAMLConverter returns a ready-to-use RAMLConverter.
func NewRAMLConverter() *RAMLConverter { return &RAMLConverter{} }

// Convert returns a RAML type-declaration map for the given shape.
// The map can be marshalled directly with gopkg.in/yaml.v3.
func (c *RAMLConverter) Convert(s raml.Shape) (RAMLTypeDecl, error) {
	if s == nil {
		return nil, fmt.Errorf("nil shape")
	}
	return c.visitShape(s)
}

// ConvertDataType converts a single shape to a "#%RAML 1.0 DataType" YAML document.
func (c *RAMLConverter) ConvertDataType(s raml.Shape) ([]byte, error) {
	decl, err := c.Convert(s)
	if err != nil {
		return nil, err
	}
	body, err := yaml.Marshal(decl)
	if err != nil {
		return nil, err
	}
	return append([]byte("#%RAML 1.0 DataType\n"), body...), nil
}

// ConvertLibrary converts an ordered map of named BaseShapes to a "#%RAML 1.0 Library"
// YAML document containing a "types:" section.
func (c *RAMLConverter) ConvertLibrary(types *orderedmap.OrderedMap[string, *raml.BaseShape]) ([]byte, error) {
	typesMap := make(map[string]any, types.Len())
	for pair := types.Oldest(); pair != nil; pair = pair.Next() {
		base := pair.Value
		if base == nil || base.Shape == nil {
			continue
		}
		decl, err := c.Convert(base.Shape)
		if err != nil {
			return nil, fmt.Errorf("type %q: %w", pair.Key, err)
		}
		typesMap[pair.Key] = decl
	}
	body, err := yaml.Marshal(map[string]any{"types": typesMap})
	if err != nil {
		return nil, err
	}
	return append([]byte("#%RAML 1.0 Library\n"), body...), nil
}

// baseDecl fills m with the common BaseShape metadata fields that apply to every
// RAML type declaration (displayName, description, default, example/examples, enum).
func (c *RAMLConverter) baseDecl(base *raml.BaseShape, m map[string]any) {
	if base.DisplayName != nil {
		m["displayName"] = base.DisplayName.Value
	}
	if base.Description != nil {
		m["description"] = base.Description.Value
	}
	if base.Default != nil && base.Default.Value != nil {
		m["default"] = base.Default.Value.Raw
	}
	if base.Example != nil && base.Example.Data != nil && base.Example.Data.Value != nil {
		m["example"] = base.Example.Data.Value.Raw
	}
	if base.Examples != nil && base.Examples.Map != nil && base.Examples.Map.Len() > 0 {
		exs := make(map[string]any, base.Examples.Map.Len())
		for p := base.Examples.Map.Oldest(); p != nil; p = p.Next() {
			if p.Value.Data != nil && p.Value.Data.Value != nil {
				exs[p.Key] = p.Value.Data.Value.Raw
			}
		}
		if len(exs) > 0 {
			m["examples"] = exs
		}
	}
	if len(base.Enum) > 0 {
		enums := make([]any, 0, len(base.Enum))
		for _, v := range base.Enum {
			if v != nil && v.Value != nil {
				enums = append(enums, v.Value.Raw)
			}
		}
		if len(enums) > 0 {
			m["enum"] = enums
		}
	}
}

// typeExpr computes the RAML "type" value for a BaseShape.
// Returns a string for zero or single inheritance, or []string for multiple
// inheritance (RAML 1.0 encodes multiple parents as a YAML sequence).
func typeExpr(base *raml.BaseShape) any {
	switch len(base.Inherits) {
	case 0:
		return base.Type
	case 1:
		return base.Inherits[0].Name
	default:
		parts := make([]string, len(base.Inherits))
		for i, p := range base.Inherits {
			parts[i] = p.Name
		}
		return parts
	}
}

func (c *RAMLConverter) visitShape(s raml.Shape) (map[string]any, error) {
	switch v := s.(type) {
	case *raml.ObjectShape:
		return c.visitObject(v)
	case *raml.ArrayShape:
		return c.visitArray(v)
	case *raml.StringShape:
		return c.visitString(v)
	case *raml.IntegerShape:
		return c.visitInteger(v)
	case *raml.NumberShape:
		return c.visitNumber(v)
	case *raml.BooleanShape:
		return c.visitBoolean(v)
	case *raml.NilShape:
		return c.visitNil(v)
	case *raml.AnyShape:
		return c.visitAny(v)
	case *raml.UnionShape:
		return c.visitUnion(v)
	case *raml.FileShape:
		return c.visitFile(v)
	case *raml.DateTimeShape:
		return c.visitDateTime(v)
	case *raml.DateTimeOnlyShape:
		return c.visitDateTimeOnly(v)
	case *raml.DateOnlyShape:
		return c.visitDateOnly(v)
	case *raml.TimeOnlyShape:
		return c.visitTimeOnly(v)
	case *raml.JSONShape:
		return c.visitJSONShape(v)
	case *raml.RecursiveShape:
		return c.visitRecursive(v)
	default:
		return map[string]any{"type": raml.TypeAny}, nil
	}
}

func (c *RAMLConverter) visitObject(s *raml.ObjectShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())

	if s.MinProperties != nil {
		m["minProperties"] = s.MinProperties.Value
	}
	if s.MaxProperties != nil {
		m["maxProperties"] = s.MaxProperties.Value
	}
	if s.AdditionalProperties != nil {
		m["additionalProperties"] = s.AdditionalProperties.Value
	}
	if s.Discriminator != nil {
		m["discriminator"] = s.Discriminator.Value
	}
	if s.DiscriminatorValue != nil && s.DiscriminatorValue.Value != nil {
		m["discriminatorValue"] = s.DiscriminatorValue.Value.Raw
	}

	hasProps := (s.Properties != nil && s.Properties.Len() > 0) ||
		(s.PatternProperties != nil && s.PatternProperties.Len() > 0)
	if hasProps {
		props := make(map[string]any)
		if s.Properties != nil {
			for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
				prop := pair.Value
				if prop.Base == nil || prop.Base.Shape == nil {
					continue
				}
				propDecl, err := c.visitShape(prop.Base.Shape)
				if err != nil {
					return nil, fmt.Errorf("property %q: %w", pair.Key, err)
				}
				if !prop.Required {
					propDecl["required"] = false
				}
				props[pair.Key] = propDecl
			}
		}
		if s.PatternProperties != nil {
			for pair := s.PatternProperties.Oldest(); pair != nil; pair = pair.Next() {
				pp := pair.Value
				if pp.Base == nil || pp.Base.Shape == nil {
					continue
				}
				propDecl, err := c.visitShape(pp.Base.Shape)
				if err != nil {
					return nil, fmt.Errorf("pattern property %q: %w", pair.Key, err)
				}
				// RAML pattern properties use /regex/ syntax as the property key.
				patternKey := pair.Key
				if len(patternKey) == 0 || patternKey[0] != '/' {
					patternKey = "/" + patternKey + "/"
				}
				props[patternKey] = propDecl
			}
		}
		if len(props) > 0 {
			m["properties"] = props
		}
	}

	return m, nil
}

func (c *RAMLConverter) visitArray(s *raml.ArrayShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())

	if s.Items != nil && s.Items.Shape != nil {
		itemDecl, err := c.visitShape(s.Items.Shape)
		if err != nil {
			return nil, fmt.Errorf("items: %w", err)
		}
		m["items"] = itemDecl
	}
	if s.MinItems != nil {
		m["minItems"] = s.MinItems.Value
	}
	if s.MaxItems != nil {
		m["maxItems"] = s.MaxItems.Value
	}
	if s.UniqueItems != nil {
		m["uniqueItems"] = s.UniqueItems.Value
	}
	return m, nil
}

func (c *RAMLConverter) visitString(s *raml.StringShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	if s.MinLength != nil {
		m["minLength"] = s.MinLength.Value
	}
	if s.MaxLength != nil {
		m["maxLength"] = s.MaxLength.Value
	}
	if s.Pattern != nil {
		m["pattern"] = s.Pattern.Value.String()
	}
	return m, nil
}

func (c *RAMLConverter) visitInteger(s *raml.IntegerShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	if s.Minimum != nil {
		m["minimum"] = bigIntToAny(s.Minimum.Value)
	}
	if s.Maximum != nil {
		m["maximum"] = bigIntToAny(s.Maximum.Value)
	}
	if s.MultipleOf != nil {
		m["multipleOf"] = bigRatToAny(s.MultipleOf.Value)
	}
	if s.Format != nil && s.Format.Value != "" {
		m["format"] = s.Format.Value
	}
	return m, nil
}

func (c *RAMLConverter) visitNumber(s *raml.NumberShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	if s.Minimum != nil {
		m["minimum"] = bigRatToAny(s.Minimum.Value)
	}
	if s.Maximum != nil {
		m["maximum"] = bigRatToAny(s.Maximum.Value)
	}
	if s.MultipleOf != nil {
		m["multipleOf"] = bigRatToAny(s.MultipleOf.Value)
	}
	if s.Format != nil && s.Format.Value != "" {
		m["format"] = s.Format.Value
	}
	return m, nil
}

func (c *RAMLConverter) visitBoolean(s *raml.BooleanShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	return m, nil
}

func (c *RAMLConverter) visitNil(s *raml.NilShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	return m, nil
}

func (c *RAMLConverter) visitAny(s *raml.AnyShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	return m, nil
}

func (c *RAMLConverter) visitUnion(s *raml.UnionShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)

	// Build RAML union type expression: "TypeA | TypeB | …"
	// Named members use their declared name; anonymous members use their type string.
	parts := make([]string, 0, len(s.AnyOf))
	for _, member := range s.AnyOf {
		if member == nil {
			continue
		}
		if member.Name != "" {
			parts = append(parts, member.Name)
		} else {
			parts = append(parts, member.Type)
		}
	}
	m["type"] = strings.Join(parts, " | ")
	return m, nil
}

func (c *RAMLConverter) visitFile(s *raml.FileShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	if s.MinLength != nil {
		m["minLength"] = s.MinLength.Value
	}
	if s.MaxLength != nil {
		m["maxLength"] = s.MaxLength.Value
	}
	if len(s.FileTypes) > 0 {
		fileTypes := make([]string, len(s.FileTypes))
		for i, ft := range s.FileTypes {
			fileTypes[i] = ft.Value
		}
		m["fileTypes"] = fileTypes
	}
	return m, nil
}

func (c *RAMLConverter) visitDateTime(s *raml.DateTimeShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	if s.Format != nil && s.Format.Value != "" {
		m["format"] = s.Format.Value
	}
	return m, nil
}

func (c *RAMLConverter) visitDateTimeOnly(s *raml.DateTimeOnlyShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	return m, nil
}

func (c *RAMLConverter) visitDateOnly(s *raml.DateOnlyShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	return m, nil
}

func (c *RAMLConverter) visitTimeOnly(s *raml.TimeOnlyShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = typeExpr(s.Base())
	return m, nil
}

// visitJSONShape attempts to produce a native RAML declaration via AsShape().
// If the compiled validator is available, AsShape() is called and the resulting
// native RAML shape is converted recursively.  When AsShape() is unavailable or
// fails, the raw JSON Schema string is emitted as the "type" value — which is
// valid inline JSON Schema in a RAML DataType context.
func (c *RAMLConverter) visitJSONShape(s *raml.JSONShape) (map[string]any, error) {
	view, err := s.AsShape()
	if err == nil && view != nil {
		return c.visitShape(view)
	}
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	m["type"] = s.Raw
	return m, nil
}

func (c *RAMLConverter) visitRecursive(s *raml.RecursiveShape) (map[string]any, error) {
	m := make(map[string]any)
	c.baseDecl(s.Base(), m)
	if s.Head != nil && s.Head.Name != "" {
		m["type"] = s.Head.Name
	} else {
		m["type"] = raml.TypeAny
	}
	return m, nil
}

// ConvertJSONSchema converts a raw JSON Schema document to a "#%RAML 1.0 DataType"
// YAML document containing native RAML types (ObjectShape, StringShape, etc.).
//
// The conversion feeds the JSON Schema through the RAML parser's own pipeline:
// a synthetic DataType fragment with "type: !include <baseName>" is parsed with
// an in-memory VirtualFS that serves the JSON Schema content, then the resulting
// JSONShape is converted to a native RAML shape via JSONShape.AsShape().
//
// filePath must be an absolute OS path to the JSON Schema file. It anchors the
// base directory so that relative $ref values in the schema resolve correctly
// against the real filesystem.
//
// Pass WithHTTPClient to enable resolution of remote $ref targets over HTTP/HTTPS.
func (c *RAMLConverter) ConvertJSONSchema(content []byte, filePath string, opts ...JSONSchemaConvertOpt) ([]byte, error) {
	var options jsonSchemaConvertOptions
	for _, opt := range opts {
		opt(&options)
	}
	if !filepath.IsAbs(filePath) {
		return nil, fmt.Errorf("filePath must be absolute, got %q", filePath)
	}
	baseDir := filepath.Dir(filePath)
	baseName := filepath.Base(filePath)

	// Synthetic RAML DataType fragment that !include-s the JSON Schema file.
	// The RAML parser reads the file through the VFS (serving in-memory content),
	// calls MakeJSONShape on the bytes, and stores the result as base.Link.
	// OptWithUnwrap then follows the link, leaving a *JSONShape on dt.Shape.Shape.
	ramlSrc := "#%RAML 1.0 DataType\ntype: !include " + baseName

	// When the caller restricts loading to a workspace root, fall back through
	// SafeOSFileLoader so $ref targets that escape the root via path traversal
	// or symlinks are refused at the filesystem layer. When no root is set,
	// $refs may resolve freely across directories via the bare OSFileLoader.
	var fallback raml.ResourceLoader
	if options.workspaceRoot != "" {
		fallback = raml.SafeOSFileLoader{Root: options.workspaceRoot}
	} else {
		fallback = raml.OSFileLoader{}
	}
	vfs := &singleFileFS{path: filePath, content: content, fallback: fallback}

	parseOpts := []raml.ParseOpt{
		raml.OptWithFileLoader(vfs),
		raml.OptWithUnwrap(),
	}
	if options.workspaceRoot != "" {
		parseOpts = append(parseOpts, raml.OptWithWorkspaceRoot(options.workspaceRoot))
	}
	if options.httpClient != nil {
		parseOpts = append(parseOpts, raml.OptWithHTTPClient(options.httpClient))
	}

	r, err := raml.ParseFromStringCtx(
		context.Background(),
		ramlSrc,
		"_schema.raml",
		baseDir,
		parseOpts...,
	)
	if err != nil {
		return nil, fmt.Errorf("parse JSON Schema: %w", err)
	}

	dt, ok := r.EntryPoint().(*raml.DataTypeFragment)
	if !ok || dt.Shape == nil {
		return nil, fmt.Errorf("unexpected fragment type %T", r.EntryPoint())
	}

	jsonShape, ok := dt.Shape.Shape.(*raml.JSONShape)
	if !ok {
		return nil, fmt.Errorf("expected JSONShape after unwrap, got %T", dt.Shape.Shape)
	}

	native, err := jsonShape.AsShape()
	if err != nil {
		return nil, fmt.Errorf("JSON Schema → RAML: %w", err)
	}
	if native == nil {
		return nil, fmt.Errorf("JSON Schema produced no convertible RAML shape")
	}

	defs := jsonShape.AsShapeDefs()
	if defs == nil || defs.Len() == 0 {
		return c.ConvertDataType(native)
	}

	// $ref targets were extracted as named types — produce a Library so that
	// union members and property types can reference them by name.
	rootName := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	typesMap := make(map[string]any, defs.Len()+1)

	for pair := defs.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil {
			continue // skip in-progress sentinel (shouldn't happen here)
		}
		decl, err := c.Convert(pair.Value)
		if err != nil {
			return nil, fmt.Errorf("named type %q: %w", pair.Key, err)
		}
		typesMap[pair.Key] = decl
	}

	rootDecl, err := c.Convert(native)
	if err != nil {
		return nil, err
	}
	typesMap[rootName] = rootDecl

	body, err := yaml.Marshal(map[string]any{"types": typesMap})
	if err != nil {
		return nil, err
	}
	return append([]byte("#%RAML 1.0 Library\n"), body...), nil
}

// singleFileFS is a raml.ResourceLoader that serves one in-memory file and
// delegates all other URIs to a fallback loader (typically a real-filesystem
// loader for $ref targets). The fallback's safety is its own responsibility.
type singleFileFS struct {
	path     string
	content  []byte
	fallback raml.ResourceLoader
}

func (f *singleFileFS) Load(uri string) (io.ReadCloser, error) {
	// uri is a file:// URI; convert to OS path for comparison.
	path := raml.FileURIToPath(uri)
	if path == f.path {
		return io.NopCloser(bytes.NewReader(f.content)), nil
	}
	return f.fallback.Load(uri)
}

// bigIntToAny converts a *big.Int to int64 when it fits, otherwise float64.
func bigIntToAny(v *big.Int) any {
	if v.IsInt64() {
		return v.Int64()
	}
	f, _ := new(big.Float).SetInt(v).Float64()
	return f
}

// bigRatToAny converts a *big.Rat to int64 when the value is an integer that
// fits, otherwise float64.
func bigRatToAny(v *big.Rat) any {
	if v.IsInt() {
		return bigIntToAny(v.Num())
	}
	f, _ := v.Float64()
	return f
}
