package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	orderedmap "github.com/wk8/go-ordered-map/v2"

	"github.com/acronis/go-raml/v3"
	"github.com/acronis/go-raml/v3/converter"
)

type ConvertOptions struct {
	Format           string
	Output           string
	TypeName         string // optional: convert only this named type (jsonschema format)
	AllowRemote      bool
	WorkspaceRoot    string
	NoWorkspaceGuard bool
}

type ConvertCommand struct {
	Opts ConvertOptions
	Args []string
}

func NewConvertCmd(opts ConvertOptions, args []string) *ConvertCommand {
	return &ConvertCommand{
		Opts: opts,
		Args: args,
	}
}

func (c *ConvertCommand) Execute(ctx context.Context) error {
	if len(c.Args) == 0 {
		return fmt.Errorf("at least one RAML file path is required")
	}

	switch c.Opts.Format {
	case "oas3", "jsonschema", "raml":
	default:
		return fmt.Errorf("unsupported format %q: supported formats are \"oas3\", \"jsonschema\", \"raml\"", c.Opts.Format)
	}

	for _, path := range c.Args {
		slog.Info("Converting...", slog.String("path", path), slog.String("format", c.Opts.Format))

		// "raml" converts a JSON Schema file to RAML; no RAML parsing needed.
		if c.Opts.Format == "raml" {
			results, err := c.convertToRAML(path)
			if err != nil {
				return err
			}
			for _, out := range results {
				if err := c.writeOutput(out.name, out.data, path); err != nil {
					return err
				}
			}
			continue
		}

		parseOpts := []raml.ParseOpt{raml.OptWithUnwrap(), raml.OptWithValidate()}
		switch {
		case c.Opts.NoWorkspaceGuard:
			parseOpts = append(parseOpts, raml.OptWithFileLoader(raml.OSFileLoader{}))
		case c.Opts.WorkspaceRoot != "":
			parseOpts = append(parseOpts, raml.OptWithWorkspaceRoot(c.Opts.WorkspaceRoot))
		}
		if c.Opts.AllowRemote {
			parseOpts = append(parseOpts, raml.OptWithHTTPClient(raml.NewHTTPClient()))
		}
		r, err := raml.ParseFromPathCtx(ctx, path, parseOpts...)
		if err != nil {
			return fmt.Errorf("parse %q: %w", path, err)
		}

		var results []namedOutput
		switch c.Opts.Format {
		case "oas3":
			results, err = c.convertOAS3(r, path)
		case "jsonschema":
			results, err = c.convertJSONSchema(r, path, c.Opts.TypeName)
		}
		if err != nil {
			return err
		}

		for _, out := range results {
			if err := c.writeOutput(out.name, out.data, path); err != nil {
				return err
			}
		}
	}

	return nil
}

// namedOutput holds marshalled JSON and a logical name used to derive the
// output filename when writing to a directory.
type namedOutput struct {
	name string
	data []byte
}

func (c *ConvertCommand) convertOAS3(r *raml.RAML, path string) ([]namedOutput, error) {
	api, ok := r.EntryPoint().(*raml.APIFragment)
	if !ok {
		return nil, fmt.Errorf("%q is not an API fragment (got %T)", path, r.EntryPoint())
	}

	conv := converter.NewOAS3Converter()
	doc, err := conv.Convert(api)
	if err != nil {
		return nil, fmt.Errorf("convert %q: %w", path, err)
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}

	return []namedOutput{{name: baseWithoutExt(path) + ".openapi", data: data}}, nil
}

func (c *ConvertCommand) convertJSONSchema(r *raml.RAML, path string, typeName string) ([]namedOutput, error) {
	types, err := typesFromFragment(r.EntryPoint(), path)
	if err != nil {
		return nil, err
	}

	conv, err := converter.NewJSONSchemaConverter(
		converter.WithWrapper(converter.JSONSchemaWrapper),
	)
	if err != nil {
		return nil, fmt.Errorf("create JSON Schema converter: %w", err)
	}

	// If a specific type name was requested, narrow the map to just that entry.
	if typeName != "" {
		base, ok := types.Get(typeName)
		if !ok {
			return nil, fmt.Errorf("type %q not found in %q", typeName, path)
		}
		narrow := orderedmap.New[string, *raml.BaseShape](1)
		narrow.Set(typeName, base)
		types = narrow
	}

	var results []namedOutput
	for pair := types.Oldest(); pair != nil; pair = pair.Next() {
		name := pair.Key
		base := pair.Value
		if base.Shape == nil {
			continue
		}
		schema, err := conv.Convert(base.Shape)
		if err != nil {
			return nil, fmt.Errorf("convert type %q: %w", name, err)
		}
		data, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal type %q: %w", name, err)
		}
		results = append(results, namedOutput{name: name, data: data})
	}

	return results, nil
}

// typesFromFragment extracts the named-types map from any supported fragment.
func typesFromFragment(
	frag raml.Fragment,
	path string,
) (*orderedmap.OrderedMap[string, *raml.BaseShape], error) {
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
		if f.Shape == nil {
			return orderedmap.New[string, *raml.BaseShape](), nil
		}
		m := orderedmap.New[string, *raml.BaseShape](1)
		m.Set(f.Shape.Name, f.Shape)
		return m, nil
	default:
		return nil, fmt.Errorf(
			"%q is not a supported fragment for JSON Schema conversion (got %T); "+
				"supported: API, Library, DataType",
			path, frag,
		)
	}
}

// writeOutput writes data either to stdout, a specific file, or a file inside
// a directory (when multiple inputs are being processed).
func (c *ConvertCommand) writeOutput(logicalName string, data []byte, srcPath string) error {
	if c.Opts.Output == "" || c.Opts.Output == "-" {
		fmt.Println(string(data))
		return nil
	}

	outPath := c.Opts.Output

	// When multiple inputs are given the output path is treated as a directory.
	if len(c.Args) > 1 {
		if err := os.MkdirAll(outPath, 0o750); err != nil {
			return fmt.Errorf("create output directory %q: %w", outPath, err)
		}
		ext := outputExt(c.Opts.Format)
		outPath = outPath + "/" + logicalName + ext
	}

	//nolint:gosec // 0o644 is appropriate for generated specs
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", outPath, err)
	}
	slog.Info("Wrote output", slog.String("output", outPath), slog.String("src", srcPath))
	return nil
}

func (c *ConvertCommand) convertToRAML(path string) ([]namedOutput, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path %q: %w", path, err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", absPath, err)
	}
	var ramlConvOpts []converter.JSONSchemaConvertOpt
	// --no-workspace-guard suppresses the workspace restriction even if a root
	// is otherwise configured; the converter's default (no WithWorkspaceRoot)
	// already allows cross-directory $refs to resolve freely.
	if c.Opts.WorkspaceRoot != "" && !c.Opts.NoWorkspaceGuard {
		ramlConvOpts = append(ramlConvOpts, converter.WithWorkspaceRoot(c.Opts.WorkspaceRoot))
	}
	if c.Opts.AllowRemote {
		ramlConvOpts = append(ramlConvOpts, converter.WithHTTPClient(raml.NewHTTPClient()))
	}
	data, err := converter.NewRAMLConverter().ConvertJSONSchema(content, absPath, ramlConvOpts...)
	if err != nil {
		return nil, fmt.Errorf("convert %q: %w", path, err)
	}
	return []namedOutput{{name: baseWithoutExt(path), data: data}}, nil
}

func outputExt(format string) string {
	switch format {
	case "jsonschema":
		return ".schema.json"
	case "raml":
		return ".raml"
	default: // oas3
		return ".openapi.json"
	}
}

// baseWithoutExt returns the filename component of path with its extension
// removed (e.g. "path/to/api.raml" → "api").
func baseWithoutExt(path string) string {
	base := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			base = path[i+1:]
			break
		}
	}
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '.' {
			return base[:i]
		}
	}
	return base
}
