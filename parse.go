package raml

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"

	"github.com/acronis/go-stacktrace"
)

// resolveUses resolves library uses by parsing each referenced library
// and linking it to the corresponding LibraryLink. It returns a stacktrace
// containing all accumulated errors, or nil if no errors occurred.
func (r *RAML) resolveUses(
	uses *orderedmap.OrderedMap[string, *LibraryLink],
	location string,
) *stacktrace.StackTrace {
	// fragment.Location is always a file:// URI set by parseFragment; PathToFileURI
	// is idempotent and harmless when the value is already normalised.
	location = PathToFileURI(location)
	var acc stacktrace.Accumulator
	for pair := uses.Oldest(); pair != nil; pair = pair.Next() {
		include := pair.Value
		libURI, err := resolveURIRef(location, include.Value)
		if err != nil {
			acc.Add(StacktraceNewWrapped("resolve uses URI", err, location,
				stacktrace.WithType(StacktraceTypeParsing), stacktrace.WithPosition(&include.KeyPos)))
			continue
		}
		sublib, err := r.parseLibrary(libURI)
		if err != nil {
			acc.Add(StacktraceNewWrapped("parse uses library", err, location,
				stacktrace.WithType(StacktraceTypeParsing), stacktrace.WithPosition(&include.KeyPos)))
		}
		include.Link = sublib
	}
	return acc.Result()
}

// ReadHead reads the first line from r and returns it (trimmed) together with
// a new reader that replays the consumed bytes followed by the rest of r.
// The returned reader must be used instead of r by subsequent callers.
func ReadHead(r io.Reader) (head string, remainder io.Reader, err error) {
	br := bufio.NewReader(r)
	line, readErr := br.ReadString('\n')
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return "", nil, fmt.Errorf("read fragment head: %w", readErr)
	}
	head = strings.TrimRight(line, "\r\n ")
	// Reconstruct a reader that includes the bytes already consumed by bufio.
	remainder = io.MultiReader(strings.NewReader(line), br)
	return head, remainder, nil
}

// IdentifyFragment returns the kind of the fragment by its head.
func IdentifyFragment(head string) (FragmentKind, error) {
	switch head {
	case "#%RAML 1.0":
		return FragmentAPI, nil
	case "#%RAML 1.0 DocumentationItem":
		return FragmentDocumentationItem, nil
	case "#%RAML 1.0 ResourceType":
		return FragmentResourceType, nil
	case "#%RAML 1.0 Trait":
		return FragmentTrait, nil
	case "#%RAML 1.0 AnnotationTypeDeclaration":
		return FragmentAnnotationTypeDeclaration, nil
	case "#%RAML 1.0 SecurityScheme":
		return FragmentSecurityScheme, nil
	// case "#%RAML 1.0 Overlay":
	// 	return FragmentOverlay, nil
	// case "#%RAML 1.0 Extension":
	// 	return FragmentExtension, nil
	case "#%RAML 1.0 Library":
		return FragmentLibrary, nil
	case "#%RAML 1.0 DataType":
		return FragmentDataType, nil
	case "#%RAML 1.0 NamedExample":
		return FragmentNamedExample, nil
	default:
		return FragmentUnknown, fmt.Errorf("unknown fragment kind: head: %s", head)
	}
}

// ReadRawFile reads a resource by URI or OS path, using the configured ResourceLoader.
func (r *RAML) ReadRawFile(path string) (io.ReadCloser, error) {
	uri := PathToFileURI(path)
	f, err := r.LoadResource(uri)
	if err != nil {
		return nil, StacktraceNewWrapped("load resource", err, path,
			stacktrace.WithType(StacktraceTypeReading))
	}
	return f, nil
}

// decodeYAMLNode decodes the first YAML document from r and returns the root
// node with the DocumentNode wrapper stripped. An empty document (io.EOF) is
// treated as an empty mapping node rather than an error.
func decodeYAMLNode(r io.Reader) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.NewDecoder(r).Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return &yaml.Node{Kind: yaml.MappingNode}, nil
		}
		return nil, err
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0], nil
	}
	return &doc, nil
}

// decodeYAMLFragment decodes the first YAML document from r into v and
// returns the raw *yaml.Node root mapping so callers can optionally retain
// it for tooling purposes (see OptWithRawSource).
//
// An empty document (io.EOF after the RAML header line) is treated as an
// empty mapping and forwarded to v.UnmarshalYAML, letting the fragment's
// own decoder decide whether that is valid.
func decodeYAMLFragment(r io.Reader, v yaml.Unmarshaler) (*yaml.Node, error) {
	root, err := decodeYAMLNode(r)
	if err != nil {
		return nil, err
	}
	return root, v.UnmarshalYAML(root)
}

// storeSourceNode records node in r.sourceNodes[path] when retainSourceNodes
// is enabled. It is a no-op otherwise, keeping zero overhead for non-tooling
// callers.
func (r *RAML) storeSourceNode(path string, node *yaml.Node) {
	if !r.retainSourceNodes || node == nil {
		return
	}
	if r.sourceNodes == nil {
		r.sourceNodes = make(map[string]*yaml.Node)
	}
	r.sourceNodes[path] = node
}

// decodeDataType decodes a data type (*DataTypeFragment) from a file.
func (r *RAML) decodeDataType(f io.Reader, path string) (*DataTypeFragment, error) {
	// TODO: This is a temporary workaround for JSON data types.
	if strings.HasSuffix(path, ".json") {
		data, err := io.ReadAll(f)
		if err != nil {
			return nil, StacktraceNewWrapped("read file", err, path,
				stacktrace.WithType(StacktraceTypeReading))
		}
		dt, err := r.MakeJSONDataType(data, path)
		if err != nil {
			return nil, StacktraceNewWrapped("make json data type", err, path,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.PutFragment(path, dt)
		return dt, nil
	}

	dt := r.MakeDataTypeFragment(path)
	r.pushParseCtx(ParseCtx{AnchorFrag: dt})
	defer r.popParseCtx()
	node, err := decodeYAMLFragment(f, dt)
	r.storeSourceNode(path, node)
	if err != nil {
		return nil, StacktraceNewWrapped("decode data type", err, path,
			stacktrace.WithType(StacktraceTypeParsing))
	}

	r.PutFragment(path, dt)

	if st := r.resolveUses(dt.Uses, dt.Location); st != nil {
		return nil, st
	}

	return dt, nil
}

// CheckFragmentKind reads the first line of rc to verify it matches kind.
// It returns a replacement io.ReadCloser that replays the consumed bytes so
// the caller can still decode the full content. The original rc is used as
// the Closer; callers must close the returned reader.
func CheckFragmentKind(path string, rc io.ReadCloser, kind FragmentKind) (io.ReadCloser, error) {
	// Allow JSON data types without reading the head.
	if kind == FragmentDataType && strings.HasSuffix(path, ".json") {
		return rc, nil
	}
	head, remainder, err := ReadHead(rc)
	if err != nil {
		return rc, fmt.Errorf("read head: %w", err)
	}
	frag, err := IdentifyFragment(head)
	if err != nil {
		return rc, fmt.Errorf("identify fragment: %w", err)
	}
	if frag != kind {
		// AnnotationTypeDeclaration fragments are structurally identical to
		// DataType fragments (they share the same decoding path), so accept
		// either when the caller asked for DataType.
		if kind == FragmentDataType && frag == FragmentAnnotationTypeDeclaration {
			return &multiReadCloser{Reader: remainder, Closer: rc}, nil
		}
		return rc, fmt.Errorf("unexpected fragment frag != kind: %v != %v", frag, kind)
	}
	return &multiReadCloser{Reader: remainder, Closer: rc}, nil
}

// multiReadCloser pairs a replacement io.Reader with the original io.Closer.
type multiReadCloser struct {
	io.Reader
	io.Closer
}

func (r *RAML) parseDataType(path string) (*DataTypeFragment, error) {
	// IMPORTANT: May generate recursive structure.
	// Consumers (resolvers, validators, external clients) must implement recursion detection when traversing links.

	cached, f, err := r.parseFragmentCached(path, FragmentDataType)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return cached.(*DataTypeFragment), nil //nolint:errcheck // type is guaranteed by parseFragmentCached+PutFragment contract
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("close file error", "error", closeErr)
		}
	}()

	dt, err := r.decodeDataType(f, path)
	if err != nil {
		return nil, err
	}
	return dt, nil
}

// LoadResource fetches a resource using the configured ResourceLoader
// (or OSFileLoader for file:// if none is set).
//
// The argument may be a URI (file://, https://, etc.) or a bare OS path; in
// the latter case LoadResource canonicalizes it to a file:// URI via
// PathToFileURI so the loader always sees a URI. Library users who call this
// directly therefore need not pre-convert paths.
func (r *RAML) LoadResource(uri string) (io.ReadCloser, error) {
	// "://" cannot occur in a legitimate OS path (drive paths use ":\",
	// POSIX paths have no colon-slash sequence), so its absence flags a
	// bare path that must be converted to a file URI.
	if !strings.Contains(uri, "://") {
		uri = PathToFileURI(uri)
	}
	if r.loader != nil {
		return r.loader.Load(uri)
	}
	return OSFileLoader{}.Load(uri)
}

// parseFragmentCached checks the fragment cache and returns the cached fragment
// if it exists, otherwise it loads the resource, checks the kind, and returns
// a reader positioned at the start. The caller is responsible for closing it.
func (r *RAML) parseFragmentCached(
	path string,
	kind FragmentKind,
) (cached Fragment, f io.ReadCloser, err error) {
	if frag := r.GetFragment(path); frag != nil {
		return frag, nil, nil
	}

	f, err = r.LoadResource(path)
	if err != nil {
		return nil, nil, StacktraceNewWrapped("load resource", err, path,
			stacktrace.WithType(StacktraceTypeLoading))
	}

	f, err = CheckFragmentKind(path, f, kind)
	if err != nil {
		_ = f.Close()
		return nil, nil, StacktraceNewWrapped("check fragment kind", err, path,
			stacktrace.WithType(StacktraceTypeReading))
	}

	return nil, f, nil
}

func (r *RAML) decodeApi(f io.Reader, path string) (*APIFragment, error) {
	api := r.MakeAPIFragment(path)
	r.pushParseCtx(ParseCtx{AnchorFrag: api})
	defer r.popParseCtx()
	node, err := decodeYAMLFragment(f, api)
	// Store the YAML AST before checking the error: even a partially-decoded
	// document (e.g. one with semantic errors like an unknown security-scheme
	// type) has a valid yaml.Node that the LSP SourceMap builder can use.
	r.storeSourceNode(path, node)
	if err != nil {
		return nil, StacktraceNewWrapped("decode api", err, path,
			stacktrace.WithType(StacktraceTypeParsing))
	}

	r.PutFragment(path, api)

	// Resolve included libraries in a separate stage.
	if st := r.resolveUses(api.Uses, api.Location); st != nil {
		return nil, st
	}
	return api, nil
}

func (r *RAML) decodeResourceTypeFragment(f io.Reader, path string) (*ResourceTypeFragment, error) {
	rtFrag := r.MakeResourceTypeFragment(path)
	// Typed fragments are self-contained: all external type dependencies must be
	// declared via uses:. Inheriting the caller's anchorFrag would allow unqualified
	// references to resolve against the including document's namespace — an antipattern
	// that makes fragments semantically non-standalone and breaks the fragment cache.
	r.pushParseCtx(ParseCtx{AnchorFrag: rtFrag})
	defer r.popParseCtx()
	node, err := decodeYAMLFragment(f, rtFrag)
	r.storeSourceNode(path, node)
	if err != nil {
		return nil, StacktraceNewWrapped("decode resource type fragment", err, path,
			stacktrace.WithType(StacktraceTypeParsing))
	}

	r.PutFragment(path, rtFrag)

	if st := r.resolveUses(rtFrag.Uses, rtFrag.Location); st != nil {
		return nil, st
	}
	return rtFrag, nil
}

func (r *RAML) parseResourceTypeFragment(path string) (*ResourceTypeFragment, error) {
	cached, f, err := r.parseFragmentCached(path, FragmentResourceType)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return cached.(*ResourceTypeFragment), nil //nolint:errcheck // type is guaranteed by parseFragmentCached+PutFragment contract
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("close file error", "error", closeErr)
		}
	}()
	return r.decodeResourceTypeFragment(f, path)
}

func (r *RAML) decodeTraitFragment(f io.Reader, path string) (*TraitFragment, error) {
	traitFrag := r.MakeTraitFragment(path)
	// Typed fragments are self-contained: all external type dependencies must be
	// declared via uses:. Inheriting the caller's anchorFrag would allow unqualified
	// references to resolve against the including document's namespace — an antipattern
	// that makes fragments semantically non-standalone and breaks the fragment cache.
	r.pushParseCtx(ParseCtx{AnchorFrag: traitFrag})
	defer r.popParseCtx()
	node, err := decodeYAMLFragment(f, traitFrag)
	r.storeSourceNode(path, node)
	if err != nil {
		return nil, StacktraceNewWrapped("decode trait fragment", err, path,
			stacktrace.WithType(StacktraceTypeParsing))
	}

	r.PutFragment(path, traitFrag)

	// Resolve included libraries in a separate stage.
	if st := r.resolveUses(traitFrag.Uses, traitFrag.Location); st != nil {
		return nil, st
	}
	return traitFrag, nil
}

func (r *RAML) parseTraitFragment(path string) (*TraitFragment, error) {
	cached, f, err := r.parseFragmentCached(path, FragmentTrait)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return cached.(*TraitFragment), nil //nolint:errcheck // type is guaranteed by parseFragmentCached+PutFragment contract
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("close file error", "error", closeErr)
		}
	}()
	return r.decodeTraitFragment(f, path)
}

func (r *RAML) decodeSecuritySchemeFragment(f io.Reader, path string) (*SecuritySchemeFragment, error) {
	securitySchemeFrag := r.MakeSecuritySchemeFragment(path)
	// Typed fragments are self-contained: all external type dependencies must be
	// declared via uses:. Inheriting the caller's anchorFrag would allow unqualified
	// references to resolve against the including document's namespace — an antipattern
	// that makes fragments semantically non-standalone and breaks the fragment cache.
	r.pushParseCtx(ParseCtx{AnchorFrag: securitySchemeFrag})
	defer r.popParseCtx()

	node, err := decodeYAMLFragment(f, securitySchemeFrag)
	r.storeSourceNode(path, node)
	if err != nil {
		return nil, StacktraceNewWrapped("decode security scheme fragment", err, path,
			stacktrace.WithType(StacktraceTypeParsing))
	}

	r.PutFragment(path, securitySchemeFrag)

	// Resolve included libraries in a separate stage.
	if st := r.resolveUses(securitySchemeFrag.Uses, securitySchemeFrag.Location); st != nil {
		return nil, st
	}

	// Shape resolution is deferred to the top-level resolveShapes pass — same as
	// every other typed fragment decoder (DataType, Library, Trait, RT). A
	// SecuritySchemeFragment is not standalone: its describedBy bodies/headers
	// are embedded into operations via securedBy, so its shapes belong to the
	// same global resolution batch. Resolving them eagerly here would race the
	// parent's resolveUses when this fragment is loaded via !include from inside
	// the parent's UnmarshalYAML, draining parent-owned shapes whose uses: links
	// are still nil.
	return securitySchemeFrag, nil
}

func (r *RAML) parseSecuritySchemeFragment(path string) (*SecuritySchemeFragment, error) {
	cached, f, err := r.parseFragmentCached(path, FragmentSecurityScheme)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return cached.(*SecuritySchemeFragment), nil //nolint:errcheck // type is guaranteed by parseFragmentCached+PutFragment contract
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("close file error", "error", closeErr)
		}
	}()
	return r.decodeSecuritySchemeFragment(f, path)
}

func (r *RAML) decodeLibrary(f io.Reader, path string) (*Library, error) {
	lib := r.MakeLibrary(path)
	r.pushParseCtx(ParseCtx{AnchorFrag: lib})
	defer r.popParseCtx()
	node, err := decodeYAMLFragment(f, lib)
	r.storeSourceNode(path, node)
	if err != nil {
		return nil, StacktraceNewWrapped("decode library", err, path,
			stacktrace.WithType(StacktraceTypeParsing))
	}

	r.PutFragment(path, lib)

	// Resolve included libraries in a separate stage.
	if st := r.resolveUses(lib.Uses, lib.Location); st != nil {
		return nil, st
	}
	return lib, nil
}

func (r *RAML) parseLibrary(path string) (*Library, error) {
	// IMPORTANT: May generate recursive structure.
	// Consumers (resolvers, validators, external clients) must implement recursion detection when traversing links.

	cached, f, err := r.parseFragmentCached(path, FragmentLibrary)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return cached.(*Library), nil //nolint:errcheck // type is guaranteed by parseFragmentCached+PutFragment contract
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("close file error", "error", closeErr)
		}
	}()

	lib, err := r.decodeLibrary(f, path)
	if err != nil {
		return nil, err
	}
	return lib, nil
}

func (r *RAML) decodeNamedExample(f io.Reader, path string) (*NamedExample, error) {
	ne := r.MakeNamedExample(path)
	r.pushParseCtx(ParseCtx{AnchorFrag: ne})
	defer r.popParseCtx()
	node, err := decodeYAMLFragment(f, ne)
	r.storeSourceNode(path, node)
	if err != nil {
		return nil, StacktraceNewWrapped("decode named example", err, path,
			stacktrace.WithType(StacktraceTypeParsing))
	}

	r.PutFragment(path, ne)

	if st := r.resolveUses(ne.Uses, ne.Location); st != nil {
		return nil, st
	}
	return ne, nil
}

func (r *RAML) parseNamedExample(path string) (*NamedExample, error) {
	cached, f, err := r.parseFragmentCached(path, FragmentNamedExample)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return cached.(*NamedExample), nil //nolint:errcheck // type is guaranteed by parseFragmentCached+PutFragment contract
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("close file error", "error", closeErr)
		}
	}()

	ne, err := r.decodeNamedExample(f, path)
	if err != nil {
		return nil, err
	}
	return ne, nil
}

// applyParserOptions applies the loader, maxIncludeSize, workspace root, and
// source-node/info retention from pOpts to r.
// defaultRoot is used as the workspace root when pOpts.workspaceRoot is empty.
//
// The workspace root governs two distinct things:
//
//   - RAML path-resolution semantics: "absolute" !include paths such as
//     "/types/foo.raml" are resolved relative to it (see node.go).
//   - The default file:// loader's I/O sandbox: when no custom loader was set
//     (neither r.setLoader nor OptWithFileLoader), the default loader is a
//     SafeOSFileLoader bound to the workspace root, which uses OS-level
//     primitives to refuse symlink-based escape from the root.
//
// When a custom loader is provided, the caller owns its I/O safety; the
// workspace root then affects only the path-resolution semantics above.
func (r *RAML) applyParserOptions(pOpts *parserOptions, defaultRoot string) {
	if pOpts.maxIncludeSizeSet {
		r.maxIncludeSize = pOpts.maxIncludeSize
	}

	workspaceRoot := pOpts.workspaceRoot
	if workspaceRoot == "" {
		workspaceRoot = defaultRoot
	}
	r.workspaceRootURI = PathToFileURI(workspaceRoot)
	if r.loader == nil {
		r.setLoader(buildSchemeLoader(pOpts, workspaceRoot))
	}

	if pOpts.withSourceNodes {
		// Set on the RAML instance before any decoding begins so that
		// storeSourceNode works for both this fragment and all recursive
		// sub-parses (libraries, resource types, traits, …).
		r.retainSourceNodes = true
		if r.sourceNodes == nil {
			r.sourceNodes = make(map[string]*yaml.Node)
		}
	}
	if pOpts.withSourceInfo {
		// Allocate once; all make* constructors share the same RAML instance
		// so the map is populated across all recursive sub-parses automatically.
		if r.sourceInfo == nil {
			r.sourceInfo = &SourceInfo{}
		}
	}
}

func (r *RAML) ParseFromPath(path string, opts ...ParseOpt) error {
	pOpts := &parserOptions{}
	for _, opt := range opts {
		opt.Apply(pOpts)
	}

	// Resolve to absolute path before computing the default workspace root.
	if !filepath.IsAbs(path) {
		workdir, err := os.Getwd()
		if err != nil {
			return StacktraceNewWrapped("get workdir", err, path,
				stacktrace.WithType(StacktraceTypeReading))
		}
		path = filepath.Join(workdir, path)
	}

	r.applyParserOptions(pOpts, filepath.Dir(path))

	// Convert to a file:// URI so that all fragment locations and cache keys
	// use URIs consistently throughout the parse (including recursive libs).
	path = PathToFileURI(path)

	f, err := r.LoadResource(path)
	if err != nil {
		return StacktraceNewWrapped("load resource", err, path,
			stacktrace.WithType(StacktraceTypeReading))
	}

	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("close file error", "error", closeErr)
		}
	}()

	return r.parseFragment(f, path, pOpts)
}

func (r *RAML) ParseFromString(content string, fileName string, baseDir string, opts ...ParseOpt) error {
	pOpts := &parserOptions{}
	for _, opt := range opts {
		opt.Apply(pOpts)
	}

	r.applyParserOptions(pOpts, baseDir)

	f := strings.NewReader(content)

	return r.parseFragment(f, PathToFileURI(filepath.Join(baseDir, fileName)), pOpts)
}

func (r *RAML) parseFragment(f io.Reader, fragmentPath string, pOpts *parserOptions) error {
	head, remainder, err := ReadHead(f)
	if err != nil {
		return StacktraceNewWrapped("read head", err, fragmentPath,
			stacktrace.WithType(StacktraceTypeParsing))
	}
	frag, err := IdentifyFragment(head)
	if err != nil {
		return StacktraceNewWrapped("identify fragment", err, fragmentPath,
			stacktrace.WithType(StacktraceTypeParsing))
	}
	f = remainder
	switch frag {
	case FragmentUnknown:
		return StacktraceNew("unknown fragment kind", fragmentPath,
			stacktrace.WithType(StacktraceTypeParsing))
	case FragmentLibrary:
		lib, errDecode := r.decodeLibrary(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse library", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(lib)
	case FragmentDataType:
		dt, errDecode := r.decodeDataType(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse data type", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(dt)
	case FragmentNamedExample:
		ne, errDecode := r.decodeNamedExample(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse named example", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(ne)
	case FragmentAPI:
		api, errDecode := r.decodeApi(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse api", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(api)
		// Build endpoints from the stage-1 IR: apply resource types and traits by
		// structural merge, then materialize. Runs before resolveShapes so that all
		// template-contributed shapes are included in the resolution pass.
		if st := r.buildEndPoints(api); st != nil {
			return StacktraceNewWrapped("build endpoints", st, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		if st := r.applySecuritySchemes(api); st != nil {
			return StacktraceNewWrapped("apply security schemes", st, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.propagateURIParameters(api)
	case FragmentTrait:
		traitFrag, errDecode := r.decodeTraitFragment(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse trait", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(traitFrag)
	case FragmentResourceType:
		rtFrag, errDecode := r.decodeResourceTypeFragment(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse resource type fragment", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(rtFrag)
	case FragmentAnnotationTypeDeclaration:
		// AnnotationTypeDeclaration is structurally identical to DataType.
		dt, errDecode := r.decodeDataType(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse annotation type fragment", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(dt)
	case FragmentDocumentationItem:
		diFrag, errDecode := r.decodeDocumentationItemFragment(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse documentation item fragment", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(diFrag)
	case FragmentSecurityScheme:
		securitySchemeFrag, errDecode := r.decodeSecuritySchemeFragment(f, fragmentPath)
		if errDecode != nil {
			return StacktraceNewWrapped("parse security scheme fragment", errDecode, fragmentPath,
				stacktrace.WithType(StacktraceTypeParsing))
		}
		r.SetEntryPoint(securitySchemeFrag)
	default:
		return StacktraceNew("unknown fragment kind", fragmentPath,
			stacktrace.WithInfo("head", head), stacktrace.WithType(StacktraceTypeParsing))
	}

	err = r.resolveShapes()
	if err != nil {
		return StacktraceNewWrapped("resolve shapes", err, fragmentPath,
			stacktrace.WithType(StacktraceTypeResolving))
	}
	err = r.resolveDomainExtensions()
	if err != nil {
		return StacktraceNewWrapped("resolve domain extensions", err, fragmentPath,
			stacktrace.WithType(StacktraceTypeResolving))
	}

	if pOpts.withUnwrapOpt {
		err = r.UnwrapShapes()
		if err != nil {
			return StacktraceNewWrapped("unwrap shapes", err, fragmentPath,
				stacktrace.WithType(StacktraceTypeUnwrapping))
		}
	}

	if pOpts.withValidateOpt {
		err = r.ValidateShapes()
		if err != nil {
			return StacktraceNewWrapped("validate shapes", err, fragmentPath,
				stacktrace.WithType(StacktraceTypeValidating))
		}
	}

	return nil
}

func ParseFromPathCtx(ctx context.Context, path string, opts ...ParseOpt) (*RAML, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	rml := New(ctx)
	err := rml.ParseFromPath(path, opts...)
	return rml, err
}

func ParseFromPath(path string, opts ...ParseOpt) (*RAML, error) {
	return ParseFromPathCtx(context.Background(), path, opts...)
}

func ParseFromStringCtx(
	ctx context.Context, content string, fileName string,
	baseDir string, opts ...ParseOpt,
) (*RAML, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	rml := New(ctx)
	err := rml.ParseFromString(content, fileName, baseDir, opts...)
	return rml, err
}

func ParseFromString(content string, fileName string, baseDir string, opts ...ParseOpt) (*RAML, error) {
	// TODO: Probably needs to be a bit more flexible. Maybe baseDir must be defined as parser option?
	if !filepath.IsAbs(baseDir) {
		return nil, fmt.Errorf("baseDir must be an absolute path")
	}
	return ParseFromStringCtx(context.Background(), content, fileName, baseDir, opts...)
}

type parserOptions struct {
	withUnwrapOpt     bool
	withValidateOpt   bool
	withSourceNodes   bool
	withSourceInfo    bool
	fileLoader        ResourceLoader
	httpClient        *http.Client
	maxIncludeSize    int64
	maxIncludeSizeSet bool
	workspaceRoot     string
}

type ParseOpt interface {
	Apply(*parserOptions)
}

type parseOptWithUnwrap struct{}

func (parseOptWithUnwrap) Apply(opt *parserOptions) {
	opt.withUnwrapOpt = true
}

func OptWithUnwrap() ParseOpt {
	return parseOptWithUnwrap{}
}

type parseOptWithValidate struct{}

func (parseOptWithValidate) Apply(opt *parserOptions) {
	opt.withValidateOpt = true
}

func OptWithValidate() ParseOpt {
	return parseOptWithValidate{}
}

type parseOptWithFileLoader struct{ l ResourceLoader }

func (o parseOptWithFileLoader) Apply(opt *parserOptions) {
	opt.fileLoader = o.l
}

// OptWithFileLoader sets a ResourceLoader used exclusively for file:// URIs,
// leaving HTTP/HTTPS handling controlled independently via OptWithHTTPClient.
// Useful for LSP servers that shadow open documents over the OS filesystem
// without affecting remote resource loading.
//
// SECURITY: when set, the workspace root's I/O sandbox is disabled for
// file:// URIs — l is solely responsible for path-traversal and symlink
// safety. The workspace root, if set via OptWithWorkspaceRoot, still governs
// RAML path-resolution semantics (e.g. "/types/foo.raml" rewriting), but
// does not constrain what l may open. Custom loaders that fall through to
// the real filesystem should compose SafeOSFileLoader rather than
// OSFileLoader to preserve symlink-escape protection.
func OptWithFileLoader(l ResourceLoader) ParseOpt {
	return parseOptWithFileLoader{l: l}
}

type parseOptWithHTTPClient struct{ c *http.Client }

func (o parseOptWithHTTPClient) Apply(opt *parserOptions) {
	opt.httpClient = o.c
}

// OptWithHTTPClient enables HTTP/HTTPS resource loading using c.
// When set, !include and uses: values with http:// or https:// URIs are
// fetched via c. Without this option remote URIs are rejected.
func OptWithHTTPClient(c *http.Client) ParseOpt {
	return parseOptWithHTTPClient{c: c}
}

type parseOptWithMaxIncludeSize struct{ n int64 }

func (o parseOptWithMaxIncludeSize) Apply(opt *parserOptions) {
	opt.maxIncludeSize = o.n
	opt.maxIncludeSizeSet = true
}

// OptWithMaxIncludeSize overrides the maximum byte size allowed for a single
// !include file. The default is DefaultMaxIncludeSize (1 MiB). Pass 0 to
// disable the limit entirely.
func OptWithMaxIncludeSize(n int64) ParseOpt {
	return parseOptWithMaxIncludeSize{n: n}
}

type parseOptWithRawSource struct{}

func (parseOptWithRawSource) Apply(opt *parserOptions) {
	opt.withSourceNodes = true
	opt.withSourceInfo = true
}

// OptWithRawSource instructs the parser to retain the full YAML AST for
// tooling consumers (LSP servers, formatters, round-trip transformers).
//
// It enables two complementary indices:
//
//   - Per-fragment node map: the raw *yaml.Node root mapping for each decoded
//     fragment is kept in memory. Retrieve it via RAML.GetSourceNode(path).
//     Useful for per-token position queries (e.g. determining which YAML key
//     a cursor position falls inside).
//
//   - Entity source-info index: every parsed model entity's ID is mapped to
//     the key+value *yaml.Node pair from which it was decoded. Retrieve the
//     index via RAML.SourceInfo() and look up individual entities with
//     SourceInfo.Get(entity.ID). Useful for navigating from a model element
//     directly to its source location without re-walking the document.
//
// Non-tooling consumers (validators, code generators) should omit this option
// to keep memory overhead minimal; the *yaml.Node trees are otherwise
// discarded once each fragment is decoded.
func OptWithRawSource() ParseOpt { return parseOptWithRawSource{} }

type parseOptWithWorkspaceRoot struct{ dir string }

func (o parseOptWithWorkspaceRoot) Apply(opt *parserOptions) {
	opt.workspaceRoot = o.dir
}

// OptWithWorkspaceRoot restricts all file loading — fragment uses:, !include
// directives, and JSON Schema $ref — to paths located inside dir.
// dir must be an absolute path; it is cleaned with filepath.Clean before use.
// It overrides the default workspace root, which is the directory of the file
// being parsed. Use this to widen the boundary when the API spans multiple
// directories within a workspace.
func OptWithWorkspaceRoot(dir string) ParseOpt {
	return parseOptWithWorkspaceRoot{dir: filepath.Clean(dir)}
}

