package server

import (
	"encoding/json"
	"fmt"
	"os"

	raml "github.com/acronis/go-raml/v3"
	"github.com/acronis/go-raml/v3/converter"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// serializationMethod is the custom LSP request name for AMF graph serialization.
const serializationMethod = "serialization"

// serializationPayload is the request payload sent by the client.
type serializationPayload struct {
	DocumentIdentifier struct {
		URI string `json:"uri"`
	} `json:"documentIdentifier"`
}

// serializationResponse is returned to the client.
type serializationResponse struct {
	URI   string `json:"uri"`
	Model any    `json:"model"`
}

// handleSerialization handles the custom "serialization" LSP request.
// It flushes any pending parse for the requested document, serialises the
// result to AMF-compatible JSON-LD, and returns the parsed object (not a
// string) so the client can pass it directly to the API Console.
func (s *Server) handleSerialization(ctx *glsp.Context, raw json.RawMessage) (any, error) {
	var payload serializationPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("serialization: invalid payload: %w", err)
	}

	uri := NormalizeURI(payload.DocumentIdentifier.URI)
	result := s.cache.FlushPending(s.cache.RootFor(uri))
	if result == nil || result.RAML == nil {
		return nil, fmt.Errorf("serialization: document not found or not parsed: %s", uri)
	}

	// UnwrapShapes is already called during parsing (cache.go step 3); calling
	// it again here is a no-op but would also be harmless.
	graph, err := converter.NewJSONLDConverter().Convert(result.RAML)
	if err != nil {
		return nil, fmt.Errorf("serialization: failed to build AMF graph: %w", err)
	}

	return serializationResponse{
		URI:   payload.DocumentIdentifier.URI,
		Model: graph,
	}, nil
}

// convertMethod is the custom LSP request name for whole-document format conversion.
const convertMethod = "convert"

// convertFormat identifies the target output format for a whole-document conversion.
type convertFormat string

const (
	convertFormatOAS3       convertFormat = "oas3"
	convertFormatJSONSchema convertFormat = "jsonschema"
	convertFormatOAS3Schema convertFormat = "oas3schema" // single named type → OAS3 components/schemas snippet
	convertFormatRAML       convertFormat = "raml"       // JSON Schema → RAML 1.0 (DataType or Library)
)

// convertPayload is the request payload for whole-document conversion.
type convertPayload struct {
	DocumentIdentifier struct {
		URI string `json:"uri"`
	} `json:"documentIdentifier"`
	Format   convertFormat `json:"format"`
	TypeName string        `json:"typeName,omitempty"` // required for oas3schema; optional for jsonschema (empty = all types)
}

// convertResponse is returned to the client for whole-document conversions.
type convertResponse struct {
	URI      string `json:"uri"`
	Document any    `json:"document"`
	TypeName string `json:"typeName,omitempty"` // echoed back when a single named type was converted
}

// handleConvert handles the custom "convert" LSP request.
// Currently supports "oas3" (full API document); the document must be a RAML API fragment.
func (s *Server) handleConvert(ctx *glsp.Context, raw json.RawMessage) (any, error) {
	var payload convertPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("convert: invalid payload: %w", err)
	}

	uri := NormalizeURI(payload.DocumentIdentifier.URI)

	// "raml" converts a JSON Schema document to RAML; it does not require a
	// parsed RAML cache entry, so handle it before the FlushPending gate.
	if payload.Format == convertFormatRAML {
		filePath := raml.FileURIToPath(uri)
		var content []byte
		if text, ok := s.store.Get(uri); ok {
			content = []byte(text)
		} else {
			var err error
			content, err = os.ReadFile(filePath) //nolint:gosec
			if err != nil {
				return nil, fmt.Errorf("convert: read %q: %w", filePath, err)
			}
		}
		var ramlConvOpts []converter.JSONSchemaConvertOpt
		if s.cache.AllowRemote() {
			ramlConvOpts = append(ramlConvOpts, converter.WithHTTPClient(raml.NewHTTPClient()))
		}
		if root := workspaceFolderFor(filePath, s.cache.WorkspaceFolders()); root != "" {
			ramlConvOpts = append(ramlConvOpts, converter.WithWorkspaceRoot(root))
		}
		data, err := converter.NewRAMLConverter().ConvertJSONSchema(content, filePath, ramlConvOpts...)
		if err != nil {
			return nil, fmt.Errorf("convert: JSON Schema → RAML: %w", err)
		}
		return convertResponse{URI: payload.DocumentIdentifier.URI, Document: string(data)}, nil
	}

	result := s.cache.FlushPending(s.cache.RootFor(uri))
	if result == nil || result.RAML == nil {
		return nil, fmt.Errorf("convert: document not found or not parsed: %s", uri)
	}

	switch payload.Format {
	case convertFormatOAS3:
		api, ok := result.RAML.EntryPoint().(*raml.APIFragment)
		if !ok {
			return nil, fmt.Errorf("convert: document is not a RAML API fragment")
		}
		doc, err := converter.NewOAS3Converter().Convert(api)
		if err != nil {
			return nil, fmt.Errorf("convert: OAS3 conversion failed: %w", err)
		}
		return convertResponse{URI: payload.DocumentIdentifier.URI, Document: doc}, nil
	case convertFormatJSONSchema:
		types, err := typesFromFragmentLSP(result.RAML.EntryPoint())
		if err != nil {
			return nil, fmt.Errorf("convert: %w", err)
		}
		conv, err := converter.NewJSONSchemaConverter(
			converter.WithWrapper(converter.JSONSchemaWrapper),
		)
		if err != nil {
			return nil, fmt.Errorf("convert: failed to create JSON Schema converter: %w", err)
		}
		// Single-type export when the client supplied a name.
		if payload.TypeName != "" {
			base, ok := types.Get(payload.TypeName)
			if !ok {
				return nil, fmt.Errorf("convert: type %q not found in document", payload.TypeName)
			}
			if base.Shape == nil {
				return nil, fmt.Errorf("convert: type %q has not been resolved", payload.TypeName)
			}
			schema, err := conv.Convert(base.Shape)
			if err != nil {
				return nil, fmt.Errorf("convert: type %q: %w", payload.TypeName, err)
			}
			return convertResponse{URI: payload.DocumentIdentifier.URI, Document: schema, TypeName: payload.TypeName}, nil
		}
		// No name supplied — convert every type in the document.
		schemas := orderedmap.New[string, *converter.JSONSchemaRAML](types.Len())
		for pair := types.Oldest(); pair != nil; pair = pair.Next() {
			base := pair.Value
			if base.Shape == nil {
				continue
			}
			schema, err := conv.Convert(base.Shape)
			if err != nil {
				return nil, fmt.Errorf("convert: type %q: %w", pair.Key, err)
			}
			schemas.Set(pair.Key, schema)
		}
		return convertResponse{URI: payload.DocumentIdentifier.URI, Document: schemas}, nil
	case convertFormatOAS3Schema:
		types, err := typesFromFragmentLSP(result.RAML.EntryPoint())
		if err != nil {
			return nil, fmt.Errorf("convert: %w", err)
		}

		convertOne := func(name string, base *raml.BaseShape) (any, error) {
			if base.Shape == nil {
				return nil, fmt.Errorf("convert: type %q has not been resolved", name)
			}
			oas3conv := converter.NewOAS3SchemaConverter()
			schema, err := oas3conv.Convert(base.Shape)
			if err != nil {
				return nil, fmt.Errorf("convert: OAS3 schema for %q failed: %w", name, err)
			}
			schemas := orderedmap.New[string, *converter.OAS3Schema]()
			schemas.Set(name, schema)
			for pair := oas3conv.Components().Oldest(); pair != nil; pair = pair.Next() {
				if _, exists := schemas.Get(pair.Key); !exists {
					schemas.Set(pair.Key, pair.Value)
				}
			}
			doc := struct {
				Components struct {
					Schemas *orderedmap.OrderedMap[string, *converter.OAS3Schema] `json:"schemas"`
				} `json:"components"`
			}{}
			doc.Components.Schemas = schemas
			return doc, nil
		}

		if payload.TypeName != "" {
			base, ok := types.Get(payload.TypeName)
			if !ok {
				return nil, fmt.Errorf("convert: type %q not found in document", payload.TypeName)
			}
			doc, err := convertOne(payload.TypeName, base)
			if err != nil {
				return nil, err
			}
			return convertResponse{URI: payload.DocumentIdentifier.URI, Document: doc, TypeName: payload.TypeName}, nil
		}
		// No name — merge all types into one components/schemas document.
		merged := orderedmap.New[string, *converter.OAS3Schema](types.Len())
		for pair := types.Oldest(); pair != nil; pair = pair.Next() {
			if pair.Value.Shape == nil {
				continue
			}
			oas3conv := converter.NewOAS3SchemaConverter()
			schema, err := oas3conv.Convert(pair.Value.Shape)
			if err != nil {
				return nil, fmt.Errorf("convert: OAS3 schema for %q failed: %w", pair.Key, err)
			}
			merged.Set(pair.Key, schema)
			for cp := oas3conv.Components().Oldest(); cp != nil; cp = cp.Next() {
				if _, exists := merged.Get(cp.Key); !exists {
					merged.Set(cp.Key, cp.Value)
				}
			}
		}
		doc := struct {
			Components struct {
				Schemas *orderedmap.OrderedMap[string, *converter.OAS3Schema] `json:"schemas"`
			} `json:"components"`
		}{}
		doc.Components.Schemas = merged
		return convertResponse{URI: payload.DocumentIdentifier.URI, Document: doc}, nil
	default:
		return nil, fmt.Errorf("convert: unsupported format %q", payload.Format)
	}
}

// convertTypeMethod is the custom LSP request name for single-type conversion.
// The cursor position selects which type to convert.
const convertTypeMethod = "convertType"

// convertTypeFormat identifies the target schema format for a type conversion.
type convertTypeFormat string

const (
	convertTypeFormatJSONSchema convertTypeFormat = "jsonschema"
	convertTypeFormatOAS3Schema convertTypeFormat = "oas3schema"
)

// convertTypePayload is the request payload for single-type conversion.
// Position must point to a type definition or reference in the document.
type convertTypePayload struct {
	DocumentIdentifier struct {
		URI string `json:"uri"`
	} `json:"documentIdentifier"`
	Format   convertTypeFormat `json:"format"`
	Position protocol.Position `json:"position"`
}

// convertTypeResponse is returned to the client.
// TypeName is set to the resolved type name so the client can suggest a default filename.
type convertTypeResponse struct {
	URI      string `json:"uri"`
	Document any    `json:"document"`
	TypeName string `json:"typeName,omitempty"`
}

// handleConvertType handles the custom "convertType" LSP request.
// It resolves the type at Position and converts it to the requested schema format.
func (s *Server) handleConvertType(ctx *glsp.Context, raw json.RawMessage) (any, error) {
	var payload convertTypePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("convertType: invalid payload: %w", err)
	}

	uri := NormalizeURI(payload.DocumentIdentifier.URI)
	result := s.cache.FlushPending(s.cache.RootFor(uri))
	if result == nil || result.RAML == nil {
		return nil, fmt.Errorf("convertType: document not found or not parsed: %s", uri)
	}

	fc := result.FileContext(uri)
	if fc.PM == nil {
		return nil, fmt.Errorf("convertType: no position index available for %s", uri)
	}
	entry, ok := fc.PM.HitTest(payload.Position.Line, payload.Position.Character)
	if !ok || entry.Shape == nil {
		return nil, fmt.Errorf("convertType: no type found at the given position")
	}
	shape := entry.Shape
	if shape.Shape == nil {
		return nil, fmt.Errorf("convertType: type %q has not been resolved", shape.Name)
	}

	switch payload.Format {
	case convertTypeFormatJSONSchema:
		conv, err := converter.NewJSONSchemaConverter(
			converter.WithWrapper(converter.JSONSchemaWrapper),
		)
		if err != nil {
			return nil, fmt.Errorf("convertType: failed to create JSON Schema converter: %w", err)
		}
		schema, err := conv.Convert(shape.Shape)
		if err != nil {
			return nil, fmt.Errorf("convertType: JSON Schema conversion failed: %w", err)
		}
		return convertTypeResponse{
			URI:      payload.DocumentIdentifier.URI,
			Document: schema,
			TypeName: shape.Name,
		}, nil

	case convertTypeFormatOAS3Schema:
		conv := converter.NewOAS3SchemaConverter()
		schema, err := conv.Convert(shape.Shape)
		if err != nil {
			return nil, fmt.Errorf("convertType: OAS3 schema conversion failed: %w", err)
		}
		// Root type first, then any referenced dependencies.
		schemas := orderedmap.New[string, *converter.OAS3Schema]()
		schemas.Set(shape.Name, schema)
		for pair := conv.Components().Oldest(); pair != nil; pair = pair.Next() {
			if _, exists := schemas.Get(pair.Key); !exists {
				schemas.Set(pair.Key, pair.Value)
			}
		}
		doc := struct {
			Components struct {
				Schemas *orderedmap.OrderedMap[string, *converter.OAS3Schema] `json:"schemas"`
			} `json:"components"`
		}{}
		doc.Components.Schemas = schemas
		return convertTypeResponse{
			URI:      payload.DocumentIdentifier.URI,
			Document: doc,
			TypeName: shape.Name,
		}, nil

	default:
		return nil, fmt.Errorf("convertType: unsupported format %q", payload.Format)
	}
}

// listTypesMethod is the custom LSP request name for listing type names in a document.
const listTypesMethod = "listTypes"

// listTypesPayload is the request payload for type listing.
type listTypesPayload struct {
	DocumentIdentifier struct {
		URI string `json:"uri"`
	} `json:"documentIdentifier"`
}

// listTypesResponse is returned to the client.
type listTypesResponse struct {
	URI   string   `json:"uri"`
	Types []string `json:"types"`
}

// handleListTypes handles the custom "listTypes" LSP request.
// It returns the ordered list of type names defined in the document.
func (s *Server) handleListTypes(ctx *glsp.Context, raw json.RawMessage) (any, error) {
	var payload listTypesPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("listTypes: invalid payload: %w", err)
	}

	uri := NormalizeURI(payload.DocumentIdentifier.URI)
	result := s.cache.FlushPending(s.cache.RootFor(uri))
	if result == nil || result.RAML == nil {
		return nil, fmt.Errorf("listTypes: document not found or not parsed: %s", uri)
	}

	types, err := typesFromFragmentLSP(result.RAML.EntryPoint())
	if err != nil {
		return nil, fmt.Errorf("listTypes: %w", err)
	}

	names := make([]string, 0, types.Len())
	for pair := types.Oldest(); pair != nil; pair = pair.Next() {
		names = append(names, pair.Key)
	}
	return listTypesResponse{URI: payload.DocumentIdentifier.URI, Types: names}, nil
}

// customRequestHandlers returns the map of custom LSP request handlers to be
// registered on the glsp Handler.
func (s *Server) customRequestHandlers() map[string]protocol.CustomRequestHandler {
	return map[string]protocol.CustomRequestHandler{
		serializationMethod: {
			Func: s.handleSerialization,
		},
		convertMethod: {
			Func: s.handleConvert,
		},
		convertTypeMethod: {
			Func: s.handleConvertType,
		},
		listTypesMethod: {
			Func: s.handleListTypes,
		},
		setRootMethod: {
			Func: s.handleSetRoot,
		},
		getRootMethod: {
			Func: s.handleGetRoot,
		},
		clearRootMethod: {
			Func: s.handleClearRoot,
		},
	}
}

// typesFromFragmentLSP extracts the named-types map from a parsed RAML fragment.
// Supported fragment types: APIFragment, Library, DataTypeFragment.
// For a DataTypeFragment the single shape is returned under its own name.
func typesFromFragmentLSP(frag raml.Fragment) (*orderedmap.OrderedMap[string, *raml.BaseShape], error) {
	switch f := frag.(type) {
	case *raml.APIFragment:
		if f.Types == nil {
			return orderedmap.New[string, *raml.BaseShape](), nil
		}
		return f.Types, nil
	case *raml.Library:
		if f.Types == nil {
			return orderedmap.New[string, *raml.BaseShape](), nil
		}
		return f.Types, nil
	case *raml.DataTypeFragment:
		m := orderedmap.New[string, *raml.BaseShape](1)
		if f.Shape != nil {
			m.Set(f.Shape.Name, f.Shape)
		}
		return m, nil
	default:
		return nil, fmt.Errorf(
			"document is not a supported fragment for JSON Schema conversion (got %T); "+
				"supported: API, Library, DataType", frag,
		)
	}
}
