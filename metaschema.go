package raml

import (
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// newOfflineCompiler creates a Compiler restricted to file-based loading only.
// Remote schema fetching is disabled: only "file" URLs are allowed.
// Draft-07 is used as the default when no "$schema" keyword is present.
func newOfflineCompiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	c.UseLoader(jsonschema.SchemeURLLoader{"file": jsonschema.FileLoader{}})
	return c
}

// resourceLoaderAdapter is a jsonschema.URLLoader that delegates to the RAML
// ResourceLoader, making JSON Schema $ref resolution use the same loader as
// all other RAML resource loading (including in-memory LSP content and HTTP).
type resourceLoaderAdapter struct {
	l ResourceLoader
}

func (a resourceLoaderAdapter) Load(url string) (any, error) {
	rc, err := a.l.Load(url)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return jsonschema.UnmarshalJSON(rc)
}

// setLoader stores the ResourceLoader and rewires the shared JSON Schema
// compiler to use it, covering all file I/O through one consistent path.
// All three common schemes are wired to the same adapter: the underlying
// ResourceLoader is responsible for scheme dispatch and will return a clear
// error for any scheme it does not support (e.g. http when no HTTP client
// is configured).
func (r *RAML) setLoader(l ResourceLoader) {
	r.loader = l
	adapter := resourceLoaderAdapter{l}
	r.jsonSchemaCompiler.UseLoader(jsonschema.SchemeURLLoader{
		"file":  adapter,
		"http":  adapter,
		"https": adapter,
	})
}

// compiledMetaSchemaDraft07 is pre-compiled from the draft-07 meta-schema that is
// embedded inside the jsonschema library. No network access is required.
var compiledMetaSchemaDraft07 = func() *jsonschema.Schema {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	// Meta-schemas are resolved from the embedded FS inside the library, not from
	// the URLLoader, so no URLLoader restriction is needed here.
	s, err := c.Compile("http://json-schema.org/draft-07/schema#")
	if err != nil {
		panic(fmt.Errorf("compile draft-07 meta-schema: %w", err))
	}
	return s
}()
