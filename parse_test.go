package raml

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// In-memory content constants replace fixture files on disk.
const (
	// Valid single-fragment bodies.
	contentDType   = "#%RAML 1.0 DataType\ntype: string\n"
	contentLibrary = "#%RAML 1.0 Library\ntypes:\n  A:\n    type: string\n"
	contentNamedEx = "#%RAML 1.0 NamedExample\nexample: {\"name\": \"John\"}\n"

	// Error-triggering bodies.
	contentUnknownKind    = "#%RAML 1.0 UnknownKind\ntype: string\n"
	contentBrokenDType    = "#%RAML 1.0 DataType\ntype: [\n"
	contentBrokenLibrary  = "#%RAML 1.0 Library\ntypes: [\n"
	contentBrokenNamedEx  = "#%RAML 1.0 NamedExample\nexample: [\n"
	contentBrokenDTypeLib = "#%RAML 1.0 DataType\ninvalid"

	// Library bodies that trigger specific parse-pipeline phases.
	// Used by parseFragment error-propagation tests.
	contentLibBadInheritance = "#%RAML 1.0 Library\ntypes:\n  C:\n    type: NonExistent\n"
	contentLibCircular       = "#%RAML 1.0 Library\ntypes:\n  B:\n    type: B\n"
	contentLibBadValidation  = "#%RAML 1.0 Library\ntypes:\n  A:\n    type: string\n    minLength: 10\n    maxLength: 5\n"

	// Fixtures for TestAnchorResolutionForbidden and TestSecuritySchemeUsesLib.
	//
	// The antipattern: a SecuritySchemeFragment references an unqualified type
	// (ErrorBody) from the including API's namespace without a uses: declaration.
	contentAnchorAPIAntipattern = "#%RAML 1.0\ntitle: Anchor Resolution Test\nmediaType: application/json\ntypes:\n  ErrorBody:\n    type: object\n    properties:\n      message: string\nsecuritySchemes:\n  oauth2: !include security/oauth2.raml\n"
	contentAnchorSecAntipattern = "#%RAML 1.0 SecurityScheme\ntype: OAuth 2.0\nsettings:\n  authorizationUri: https://example.com/oauth/authorize\n  accessTokenUri: https://example.com/oauth/token\n  authorizationGrants:\n    - authorization_code\ndescribedBy:\n  responses:\n    401:\n      body:\n        application/json:\n          type: ErrorBody\n"

	// The correct pattern: the SecuritySchemeFragment declares ErrorBody via uses:.
	contentAnchorAPICorrect = "#%RAML 1.0\ntitle: Anchor Resolution Test\nmediaType: application/json\nsecuritySchemes:\n  oauth2: !include security/oauth2.raml\n"
	contentAnchorSecCorrect = "#%RAML 1.0 SecurityScheme\nuses:\n  errors: errors.raml\ntype: OAuth 2.0\nsettings:\n  authorizationUri: https://example.com/oauth/authorize\n  accessTokenUri: https://example.com/oauth/token\n  authorizationGrants:\n    - authorization_code\ndescribedBy:\n  responses:\n    401:\n      body:\n        application/json:\n          type: errors.ErrorBody\n"
	contentAnchorErrorLib   = "#%RAML 1.0 Library\ntypes:\n  ErrorBody:\n    type: object\n    properties:\n      message: string\n"
)

func TestReadHead(t *testing.T) {
	tests := []struct {
		name    string
		f       io.Reader
		want    string
		wantErr bool
	}{
		{
			name: "CRLF line ending stripped",
			f:    &mockReadSeeker{P: []byte("#%RAML 1.0 Library\r\n")},
			want: "#%RAML 1.0 Library",
		},
		{
			name: "LF line ending stripped",
			f:    &mockReadSeeker{P: []byte("#%RAML 1.0 Library\n")},
			want: "#%RAML 1.0 Library",
		},
		{
			name:    "read error propagated",
			f:       &mockReadSeeker{ReadErr: errors.New("read error")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, remainder, err := ReadHead(tt.f)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadHead() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ReadHead() = %q, want %q", got, tt.want)
			}
			if err == nil {
				// remainder must replay the complete original content (header line included).
				original := tt.f.(*mockReadSeeker).P
				replayed, readErr := io.ReadAll(remainder)
				if readErr != nil {
					t.Fatalf("ReadHead() remainder read error: %v", readErr)
				}
				if !bytes.Equal(replayed, original) {
					t.Errorf("ReadHead() remainder = %q, want %q", replayed, original)
				}
			}
		})
	}
}

func TestIdentifyFragment(t *testing.T) {
	tests := []struct {
		head    string
		want    FragmentKind
		wantErr bool
	}{
		{"#%RAML 1.0", FragmentAPI, false},
		{"#%RAML 1.0 Library", FragmentLibrary, false},
		{"#%RAML 1.0 DataType", FragmentDataType, false},
		{"#%RAML 1.0 NamedExample", FragmentNamedExample, false},
		{"#%RAML 1.0 ResourceType", FragmentResourceType, false},
		{"#%RAML 1.0 Trait", FragmentTrait, false},
		{"#%RAML 1.0 AnnotationTypeDeclaration", FragmentAnnotationTypeDeclaration, false},
		{"#%RAML 1.0 SecurityScheme", FragmentSecurityScheme, false},
		{"#%RAML 1.0 DocumentationItem", FragmentDocumentationItem, false},
		{"#%RAML 1.0 UnknownKind", FragmentUnknown, true},
		{"", FragmentUnknown, true},
	}
	for _, tt := range tests {
		t.Run(tt.head, func(t *testing.T) {
			got, err := IdentifyFragment(tt.head)
			if (err != nil) != tt.wantErr {
				t.Errorf("IdentifyFragment(%q) error = %v, wantErr %v", tt.head, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("IdentifyFragment(%q) = %v, want %v", tt.head, got, tt.want)
			}
		})
	}
}

func TestReadRawFile(t *testing.T) {
	base := t.TempDir()
	libPath := filepath.Join(base, "library.raml")

	tests := []struct {
		name    string
		fs      testFS
		path    string
		wantErr bool
	}{
		{
			name: "existing file returns readable closer",
			fs:   testFS{libPath: contentLibrary},
			path: libPath,
		},
		{
			name:    "missing file returns error",
			fs:      testFS{},
			path:    filepath.Join(base, "not-found.raml"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			got, err := r.ReadRawFile(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadRawFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				require.NotNil(t, got)
				require.NoError(t, got.Close())
			}
		})
	}
}

func TestRAML_decodeDataType(t *testing.T) {
	base := t.TempDir()
	testRaml := filepath.Join(base, "test.raml")
	testJSON := filepath.Join(base, "test.json")
	commonRaml := filepath.Join(base, "common.raml")

	tests := []struct {
		name    string
		fs      testFS
		f       io.Reader
		path    string
		want    func(*testing.T, *DataTypeFragment)
		wantErr bool
	}{
		{
			name: "RAML DataType: string shape decoded",
			f:    &mockReadSeeker{P: []byte(contentDType)},
			path: testRaml,
			want: func(tt *testing.T, got *DataTypeFragment) {
				require.NotNil(tt, got)
				require.Equal(tt, "string", got.Shape.Type)
			},
		},
		{
			name: "JSON schema: type set to json",
			f:    &mockReadSeeker{P: []byte(`{"type": "string"}`)},
			path: testJSON,
			want: func(tt *testing.T, got *DataTypeFragment) {
				require.NotNil(tt, got)
				require.Equal(tt, "json", got.Shape.Type)
			},
		},
		{
			name: "RAML DataType with uses: Uses field populated",
			fs:   testFS{commonRaml: contentLibrary},
			f:    &mockReadSeeker{P: []byte("#%RAML 1.0 DataType\nuses:\n  common: common.raml\ntype: common.A")},
			path: testRaml,
			want: func(tt *testing.T, got *DataTypeFragment) {
				require.NotNil(tt, got)
				require.NotNil(tt, got.Uses)
			},
		},
		{
			name:    "JSON file read error propagated",
			f:       &mockReadSeeker{ReadErr: errors.New("read error")},
			path:    testJSON,
			wantErr: true,
		},
		{
			name:    "invalid JSON returns parse error",
			f:       &mockReadSeeker{P: []byte("{invalid json")},
			path:    testJSON,
			wantErr: true,
		},
		{
			name:    "invalid YAML returns parse error",
			f:       &mockReadSeeker{P: []byte(contentBrokenDTypeLib)},
			path:    testRaml,
			wantErr: true,
		},
		{
			name:    "invalid uses path returns error",
			f:       &mockReadSeeker{P: []byte("#%RAML 1.0 DataType\nuses:\n  common: invalid_decode.raml")},
			path:    testRaml,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			got, err := r.decodeDataType(tt.f, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeDataType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestCheckFragmentKind(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		f           io.ReadCloser
		kind        FragmentKind
		wantErr     bool
		wantContent []byte // non-nil: verify the returned reader produces this content
	}{
		{
			name:        "library content matches library kind",
			path:        "/test/library.raml",
			f:           io.NopCloser(&mockReadSeeker{P: []byte(contentLibrary)}),
			kind:        FragmentLibrary,
			wantContent: []byte(contentLibrary),
		},
		{
			name:        "datatype content matches datatype kind",
			path:        "/test/dtype.raml",
			f:           io.NopCloser(&mockReadSeeker{P: []byte(contentDType)}),
			kind:        FragmentDataType,
			wantContent: []byte(contentDType),
		},
		{
			name: "JSON path accepted without reading head",
			path: "/test/dtype.json",
			f:    nil,
			kind: FragmentDataType,
		},
		{
			name:    "library content mismatched against datatype kind",
			path:    "/test/library.raml",
			f:       io.NopCloser(&mockReadSeeker{P: []byte(contentLibrary)}),
			kind:    FragmentDataType,
			wantErr: true,
		},
		{
			name:    "read error causes failure",
			path:    "/test/library.raml",
			f:       io.NopCloser(&mockReadSeeker{ReadErr: errors.New("read error")}),
			kind:    FragmentLibrary,
			wantErr: true,
		},
		{
			name:    "unrecognised fragment kind in content",
			path:    "/test/unknown.raml",
			f:       io.NopCloser(&mockReadSeeker{P: []byte(contentUnknownKind)}),
			kind:    FragmentLibrary,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc, err := CheckFragmentKind(tt.path, tt.f, tt.kind)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckFragmentKind() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantContent != nil {
				// The returned reader must replay the full original content
				// (including the header line) so the YAML decoder sees it.
				got, readErr := io.ReadAll(rc)
				if readErr != nil {
					t.Fatalf("CheckFragmentKind() returned reader error: %v", readErr)
				}
				if !bytes.Equal(got, tt.wantContent) {
					t.Errorf("CheckFragmentKind() reader content = %q, want %q", got, tt.wantContent)
				}
			}
		})
	}
}

func TestRAML_parseDataType(t *testing.T) {
	base := t.TempDir()
	dtype := filepath.Join(base, "dtype.raml")

	tests := []struct {
		name    string
		fs      testFS
		setup   func(*RAML)
		path    string
		want    func(*testing.T, *DataTypeFragment)
		wantErr bool
	}{
		{
			name: "valid DataType string shape decoded",
			fs:   testFS{dtype: contentDType},
			path: dtype,
			want: func(tt *testing.T, got *DataTypeFragment) {
				require.NotNil(tt, got)
				require.Equal(tt, "string", got.Shape.Type)
			},
		},
		{
			name: "cached fragment returned without re-parsing",
			setup: func(r *RAML) {
				r.fragmentsCache["file:///cache-key"] = &DataTypeFragment{Shape: &BaseShape{Type: "string"}}
			},
			path: "file:///cache-key",
			want: func(tt *testing.T, got *DataTypeFragment) {
				require.NotNil(tt, got)
				require.Equal(tt, "string", got.Shape.Type)
			},
		},
		{
			name:    "invalid header kind returns error",
			fs:      testFS{dtype: contentUnknownKind},
			path:    dtype,
			wantErr: true,
		},
		{
			name:    "missing file returns error",
			fs:      testFS{},
			path:    filepath.Join(base, "not-found.raml"),
			wantErr: true,
		},
		{
			name:    "invalid DataType YAML returns error",
			fs:      testFS{dtype: contentBrokenDType},
			path:    dtype,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			if tt.setup != nil {
				tt.setup(r)
			}
			got, err := r.parseDataType(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDataType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestRAML_parseLibrary(t *testing.T) {
	base := t.TempDir()
	lib := filepath.Join(base, "library.raml")

	tests := []struct {
		name    string
		fs      testFS
		path    string
		want    func(*testing.T, *Library)
		wantErr bool
	}{
		{
			name: "valid Library decoded",
			fs:   testFS{lib: contentLibrary},
			path: lib,
			want: func(tt *testing.T, got *Library) {
				require.NotNil(tt, got)
			},
		},
		{
			name:    "missing file returns error",
			fs:      testFS{},
			path:    filepath.Join(base, "not-found.raml"),
			wantErr: true,
		},
		{
			name:    "DataType content fails library kind check",
			fs:      testFS{lib: contentDType},
			path:    lib,
			wantErr: true,
		},
		{
			name:    "broken Library YAML returns error",
			fs:      testFS{lib: contentBrokenLibrary},
			path:    lib,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			got, err := r.parseLibrary(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLibrary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestRAML_decodeNamedExample(t *testing.T) {
	tests := []struct {
		name    string
		f       io.Reader
		path    string
		want    func(*testing.T, *NamedExample)
		wantErr bool
	}{
		{
			name: "valid named example decoded",
			f:    &mockReadSeeker{P: []byte(contentNamedEx)},
			path: "/test/named_example.raml",
			want: func(tt *testing.T, got *NamedExample) {
				require.NotNil(tt, got)
				require.NotEmpty(tt, got.Map)
			},
		},
		{
			name:    "broken YAML returns error",
			f:       &mockReadSeeker{P: []byte(contentBrokenNamedEx)},
			path:    "/test/named_example.raml",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			got, err := r.decodeNamedExample(tt.f, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeNamedExample() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestRAML_parseNamedExample(t *testing.T) {
	base := t.TempDir()
	ne := filepath.Join(base, "named_example.raml")

	tests := []struct {
		name    string
		fs      testFS
		setup   func(*RAML)
		path    string
		want    func(*testing.T, *NamedExample)
		wantErr bool
	}{
		{
			name: "valid NamedExample decoded",
			fs:   testFS{ne: contentNamedEx},
			path: ne,
			want: func(tt *testing.T, got *NamedExample) {
				require.NotNil(tt, got)
				require.NotEmpty(tt, got.Map)
			},
		},
		{
			name: "cached fragment returned without re-parsing",
			setup: func(r *RAML) {
				r.fragmentsCache["file:///cache-key"] = &NamedExample{ID: 1}
			},
			path: "file:///cache-key",
			want: func(tt *testing.T, got *NamedExample) {
				require.NotNil(tt, got)
				require.Equal(tt, int64(1), got.ID)
			},
		},
		{
			name:    "missing file returns error",
			fs:      testFS{},
			path:    filepath.Join(base, "not-found.raml"),
			wantErr: true,
		},
		{
			name:    "DataType content fails named-example kind check",
			fs:      testFS{ne: contentDType},
			path:    ne,
			wantErr: true,
		},
		{
			name:    "broken NamedExample YAML returns error",
			fs:      testFS{ne: contentBrokenNamedEx},
			path:    ne,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			if tt.setup != nil {
				tt.setup(r)
			}
			got, err := r.parseNamedExample(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseNamedExample() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestRAML_ParseFromPath(t *testing.T) {
	base := t.TempDir()
	ne := filepath.Join(base, "named_example.raml")

	tests := []struct {
		name    string
		fs      testFS
		path    string
		opts    []ParseOpt
		wantErr bool
	}{
		{
			name: "named example parses with unwrap and validate",
			fs:   testFS{ne: contentNamedEx},
			path: ne,
			opts: []ParseOpt{OptWithUnwrap(), OptWithValidate()},
		},
		{
			name:    "missing file returns error",
			fs:      testFS{},
			path:    filepath.Join(base, "not-found.raml"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			if err := r.ParseFromPath(tt.path, tt.opts...); (err != nil) != tt.wantErr {
				t.Errorf("ParseFromPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRAML_ParseFromString(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		fileName string
		opts     []ParseOpt
		wantErr  bool
	}{
		{
			name:     "named example inline string parses",
			content:  contentNamedEx,
			fileName: "test.raml",
			opts:     []ParseOpt{OptWithUnwrap(), OptWithValidate()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			if err := r.ParseFromString(tt.content, tt.fileName, t.TempDir(), tt.opts...); (err != nil) != tt.wantErr {
				t.Errorf("ParseFromString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestOptWithRawSource verifies that OptWithRawSource causes the raw
// *yaml.Node root to be retained for each decoded fragment and that it is
// absent when the option is not used.
func TestOptWithRawSource(t *testing.T) {
	const apiDoc = "#%RAML 1.0\ntitle: Test API\ntypes:\n  MyType:\n    type: string\n"
	const libDoc = "#%RAML 1.0 Library\ntypes:\n  Base:\n    type: object\n"

	baseDir := t.TempDir()

	t.Run("node retained for API fragment", func(t *testing.T) {
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(apiDoc, "api.raml", baseDir, OptWithRawSource()))

		node := r.GetSourceNode(filepath.Join(baseDir, "api.raml"))
		require.NotNil(t, node, "GetSourceNode must return non-nil when OptWithRawSource is set")
		require.Equal(t, yaml.MappingNode, node.Kind,
			"root node must be a MappingNode (the YAML document root)")
		require.Greater(t, len(node.Content), 0,
			"root mapping must have at least one key/value pair")
	})

	t.Run("node absent without option", func(t *testing.T) {
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(apiDoc, "api.raml", baseDir))

		node := r.GetSourceNode(filepath.Join(baseDir, "api.raml"))
		require.Nil(t, node, "GetSourceNode must return nil when OptWithRawSource is not set")
	})

	t.Run("retainSourceNodes propagates to library sub-parse", func(t *testing.T) {
		dir := t.TempDir()
		libPath := filepath.Join(dir, "lib.raml")

		const apiWithUses = "#%RAML 1.0\ntitle: T\nuses:\n  lib: lib.raml\n"
		r := makeTestRAMLWithFS(t, testFS{libPath: libDoc})
		require.NoError(t, r.ParseFromString(apiWithUses, "api.raml", dir, OptWithRawSource()))

		libNode := r.GetSourceNode(libPath)
		require.NotNil(t, libNode,
			"GetSourceNode must return the library node: flag propagates to recursive parses")
		require.Equal(t, yaml.MappingNode, libNode.Kind)
	})

	t.Run("line/column positions on root keys", func(t *testing.T) {
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(apiDoc, "api.raml", baseDir, OptWithRawSource()))

		node := r.GetSourceNode(filepath.Join(baseDir, "api.raml"))
		require.NotNil(t, node)
		// node.Content is [key0, val0, key1, val1, …]; first key is "title".
		require.GreaterOrEqual(t, len(node.Content), 2)
		titleKey := node.Content[0]
		require.Equal(t, "title", titleKey.Value)
		require.Greater(t, titleKey.Line, 0, "key node must have a positive Line position")
		require.Greater(t, titleKey.Column, 0, "key node must have a positive Column position")
	})
}

func TestRAML_parseFragment(t *testing.T) {
	base := t.TempDir()
	dtype := filepath.Join(base, "dtype.raml")

	tests := []struct {
		name         string
		fs           testFS
		f            io.ReadSeeker
		fragmentPath string
		pOpts        *parserOptions
		wantErr      bool
	}{
		{
			name:         "NamedExample fragment parsed",
			f:            &mockReadSeeker{P: []byte(contentNamedEx)},
			fragmentPath: filepath.Join(base, "named_example.raml"),
			pOpts:        &parserOptions{withUnwrapOpt: true, withValidateOpt: true},
		},
		{
			name:         "DataType fragment parsed",
			f:            &mockReadSeeker{P: []byte(contentDType)},
			fragmentPath: dtype,
			pOpts:        &parserOptions{withUnwrapOpt: true, withValidateOpt: true},
		},
		{
			name:         "Library fragment parsed",
			f:            &mockReadSeeker{P: []byte(contentLibrary)},
			fragmentPath: filepath.Join(base, "library.raml"),
			pOpts:        &parserOptions{withUnwrapOpt: true, withValidateOpt: true},
		},
		{
			name:    "read error propagated",
			f:       &mockReadSeeker{ReadErr: errors.New("read error")},
			wantErr: true,
		},
		{
			name:    "unrecognised fragment kind returns error",
			f:       &mockReadSeeker{P: []byte(contentUnknownKind)},
			wantErr: true,
		},
		{
			name:    "invalid Library YAML returns error",
			f:       &mockReadSeeker{P: []byte(contentBrokenLibrary)},
			wantErr: true,
		},
		{
			name:    "invalid DataType YAML returns error",
			f:       &mockReadSeeker{P: []byte(contentBrokenDType)},
			wantErr: true,
		},
		{
			name:    "invalid NamedExample YAML returns error",
			f:       &mockReadSeeker{P: []byte(contentBrokenNamedEx)},
			wantErr: true,
		},
		{
			name: "resolve type error propagated",
			fs:   testFS{filepath.Join(base, "lib_bad_inheritance.raml"): contentLibBadInheritance},
			f: &mockReadSeeker{P: []byte(
				"#%RAML 1.0 DataType\nuses:\n  lib: lib_bad_inheritance.raml\ntype: lib.C",
			)},
			fragmentPath: dtype,
			pOpts:        &parserOptions{},
			wantErr:      true,
		},
		{
			name:         "resolve annotation type error propagated",
			f:            &mockReadSeeker{P: []byte("#%RAML 1.0 Library\n(A): B")},
			fragmentPath: dtype,
			pOpts:        &parserOptions{},
			wantErr:      true,
		},
		{
			name: "unwrap error propagated",
			fs:   testFS{filepath.Join(base, "lib_circular.raml"): contentLibCircular},
			f: &mockReadSeeker{P: []byte(
				"#%RAML 1.0 DataType\nuses:\n  lib: lib_circular.raml\ntype: lib.B",
			)},
			fragmentPath: dtype,
			pOpts:        &parserOptions{withUnwrapOpt: true},
			wantErr:      true,
		},
		{
			name: "validate error propagated",
			fs:   testFS{filepath.Join(base, "lib_bad_validation.raml"): contentLibBadValidation},
			f: &mockReadSeeker{P: []byte(
				"#%RAML 1.0 DataType\nuses:\n  lib: lib_bad_validation.raml\ntype: lib.A",
			)},
			fragmentPath: dtype,
			pOpts:        &parserOptions{withUnwrapOpt: true, withValidateOpt: true},
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			if err := r.parseFragment(tt.f, tt.fragmentPath, tt.pOpts); (err != nil) != tt.wantErr {
				t.Errorf("parseFragment() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseFromPathCtx(t *testing.T) {
	// Nil-context error is the unique behaviour of this wrapper.
	// The valid-path happy path is covered by TestRAML_ParseFromPath.
	t.Run("nil context returns error", func(t *testing.T) {
		_, err := ParseFromPathCtx(nil, "")
		require.Error(t, err)
	})
}

func TestParseFromStringCtx(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		content  string
		fileName string
		opts     []ParseOpt
		want     func(*testing.T, *RAML)
		wantErr  bool
	}{
		{
			name:     "named example parses with background context",
			ctx:      context.Background(),
			content:  contentNamedEx,
			fileName: "test.raml",
			opts:     []ParseOpt{OptWithUnwrap(), OptWithValidate()},
			want:     func(tt *testing.T, got *RAML) { require.NotNil(tt, got) },
		},
		{
			name:    "nil context returns error",
			ctx:     nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFromStringCtx(tt.ctx, tt.content, tt.fileName, t.TempDir(), tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFromStringCtx() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestParseFromString(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		fileName string
		baseDir  string
		opts     []ParseOpt
		want     func(*testing.T, *RAML)
		wantErr  bool
	}{
		{
			name:     "named example parses with absolute base dir",
			content:  contentNamedEx,
			fileName: "test.raml",
			baseDir:  t.TempDir(),
			opts:     []ParseOpt{OptWithUnwrap(), OptWithValidate()},
			want:     func(tt *testing.T, got *RAML) { require.NotNil(tt, got) },
		},
		{
			name:    "relative base dir returns error",
			content: contentNamedEx,
			baseDir: "fixtures",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFromString(tt.content, tt.fileName, tt.baseDir, tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

// TestAnchorResolutionForbidden verifies that a typed fragment (SecuritySchemeFragment)
// referencing an unqualified type from the including API's namespace without a uses:
// declaration is rejected. Typed fragments must be self-contained: all external type
// dependencies must be declared via uses:.
func TestAnchorResolutionForbidden(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	secPath := filepath.Join(base, "security", "oauth2.raml")

	r := makeTestRAMLWithFS(t, testFS{
		apiPath: contentAnchorAPIAntipattern,
		secPath: contentAnchorSecAntipattern,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	if err == nil {
		t.Fatal("expected error for unqualified type reference in SecuritySchemeFragment, got nil")
	}
}

// TestSecuritySchemeUsesLib verifies the correct pattern: a SecuritySchemeFragment
// declares its external type dependencies via uses: and references them with dot notation.
func TestSecuritySchemeUsesLib(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	secPath := filepath.Join(base, "security", "oauth2.raml")
	libPath := filepath.Join(base, "security", "errors.raml")

	r := makeTestRAMLWithFS(t, testFS{
		apiPath: contentAnchorAPICorrect,
		secPath: contentAnchorSecCorrect,
		libPath: contentAnchorErrorLib,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	if err != nil {
		t.Fatalf("ParseFromPath: %v", err)
	}

	api, ok := r.EntryPoint().(*APIFragment)
	if !ok {
		t.Fatal("entry point is not *APIFragment")
	}
	schemeDef, ok := api.SecuritySchemes.Get("oauth2")
	if !ok {
		t.Fatal("expected security scheme 'oauth2'")
	}
	if schemeDef.DescribedBy == nil {
		t.Fatal("expected DescribedBy to be non-nil")
	}
	resp, ok := schemeDef.DescribedBy.Responses.Get(401)
	if !ok {
		t.Fatal("expected response 401 in oauth2 describedBy")
	}
	body, ok := resp.Bodies.Get("application/json")
	if !ok {
		t.Fatal("expected application/json body in 401 response")
	}
	if body.Shape == nil {
		t.Fatal("expected body shape to be non-nil")
	}
	if body.Shape.Type != TypeObject {
		t.Fatalf("expected body shape type %q, got %q", TypeObject, body.Shape.Type)
	}
}

// TestRTFragmentStaticBodyTypeForbidden verifies that a ResourceType fragment whose
// body hard-codes an unqualified type name is rejected. The fragment's anchorFrag is
// itself (self-contained), and resolveLibraryReference requires a dot-qualified name,
// so bare names that only exist in the including API's namespace cannot resolve.
//
// Unlike traits, RT fragments have no eager compilation, so both static types and
// unqualified param values are forbidden for the same reason.
func TestRTFragmentStaticBodyTypeForbidden(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	rtPath := filepath.Join(base, "resourceTypes", "collection.raml")

	api := `#%RAML 1.0
title: RT Static Body Antipattern
types:
  Item:
    type: object
    properties:
      id: integer
resourceTypes:
  collection: !include resourceTypes/collection.raml
/items:
  type: collection
`
	// The RT body hard-codes an unqualified type that only exists in the including API.
	// The fragment must declare this dependency via its own uses: — not inherit it.
	rt := `#%RAML 1.0 ResourceType
get:
  responses:
    200:
      body:
        application/json:
          type: Item
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath: api,
		rtPath:  rt,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.Error(t, err, "expected error: unqualified type in RT body must not resolve against the including API")
}

// TestRTFragmentParamQualifiedTypeApplicationScope verifies the negative case for
// resource-type parameter substitution: a parameter VALUE written at the application
// site must resolve in the application site's namespace, even when its qualified
// prefix happens to match an alias inside the RT fragment's own uses:.
//
// Symmetric scoping rule: a fragment is self-contained for its hardcoded
// (static) references — they resolve at the definition site — but values
// supplied by the caller are interpreted in the caller's namespace. The caller
// wrote "types.Item"; if the caller has no "types" alias the reference is
// dangling regardless of any matching alias inside the RT fragment.
func TestRTFragmentParamQualifiedTypeApplicationScope(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	rtPath := filepath.Join(base, "resourceTypes", "collection.raml")
	libPath := filepath.Join(base, "resourceTypes", "types.raml")

	// The API supplies the parameter value "types.Item" but does NOT declare a
	// "types" alias of its own. The matching alias inside the RT fragment must
	// not be borrowed for resolution.
	api := `#%RAML 1.0
title: RT Param Qualified Type Antipattern
resourceTypes:
  collection: !include resourceTypes/collection.raml
/items:
  type:
    collection:
      itemType: types.Item
`
	rt := `#%RAML 1.0 ResourceType
uses:
  types: types.raml
get:
  responses:
    200:
      body:
        application/json:
          type: <<itemType>>
`
	lib := `#%RAML 1.0 Library
types:
  Item:
    type: object
    properties:
      id: integer
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath: api,
		rtPath:  rt,
		libPath: lib,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.Error(t, err,
		"a parameter value written at the application site must resolve in the application site's namespace; the RT fragment's own uses: must not provide aliases to the caller")
}

// TestRTFragmentParamQualifiedTypeValid is the positive counterpart of
// TestRTFragmentParamQualifiedTypeApplicationScope: when the API itself declares
// the qualified alias used in the supplied parameter value, resolution
// succeeds because the value resolves in the application site's namespace.
func TestRTFragmentParamQualifiedTypeValid(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	rtPath := filepath.Join(base, "resourceTypes", "collection.raml")
	libPath := filepath.Join(base, "types.raml")

	// The API supplies "apiTypes.Item" and declares its own apiTypes alias.
	// The RT fragment is parameter-agnostic — it just substitutes <<itemType>>
	// and the resulting reference resolves at the call site.
	api := `#%RAML 1.0
title: RT Param Qualified Type Valid
uses:
  apiTypes: types.raml
resourceTypes:
  collection: !include resourceTypes/collection.raml
/items:
  type:
    collection:
      itemType: apiTypes.Item
`
	rt := `#%RAML 1.0 ResourceType
get:
  responses:
    200:
      body:
        application/json:
          type: <<itemType>>
`
	lib := `#%RAML 1.0 Library
types:
  Item:
    type: object
    properties:
      id: integer
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath: api,
		rtPath:  rt,
		libPath: lib,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.NoError(t, err)

	apiEntry := r.EntryPoint().(*APIFragment)
	ep, ok := apiEntry.EndPoints.Get("/items")
	require.True(t, ok, "expected /items endpoint")
	op, ok := ep.Operations.Get("get")
	require.True(t, ok, "expected GET operation from RT")
	resp, ok := op.Responses.Get(200)
	require.True(t, ok, "expected 200 response")
	body, ok := resp.Bodies.Get("application/json")
	require.True(t, ok, "expected application/json body")
	require.NotNil(t, body.Shape)
	require.Equal(t, TypeObject, body.Shape.Type,
		"param value apiTypes.Item must resolve to object type via the API's own uses:")
}

// TestRTFragmentWithTrait verifies the combination of a ResourceType fragment and a
// Trait under strict lexical scoping. A trait name written inside an RT fragment
// resolves against that fragment's own uses:/traits ONLY — it does not leak into the
// including API's trait namespace. The fragment therefore imports the trait library
// via its own uses: and references the trait with a qualified name.
func TestRTFragmentWithTrait(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	rtPath := filepath.Join(base, "resourceTypes", "collection.raml")
	traitsLibPath := filepath.Join(base, "resourceTypes", "traits.raml")

	api := `#%RAML 1.0
title: RT + Trait Combination
resourceTypes:
  collection: !include resourceTypes/collection.raml
/items:
  type: collection
`
	// The RT fragment imports the trait library via its own uses: and references the
	// trait by its qualified name. This keeps the fragment self-contained.
	rt := `#%RAML 1.0 ResourceType
uses:
  traitsLib: traits.raml
get:
  is: [traitsLib.paged]
  responses:
    200:
      body:
        application/json:
          type: object
`
	traitsLib := `#%RAML 1.0 Library
traits:
  paged:
    queryParameters:
      page:
        type: integer
        default: 1
      pageSize:
        type: integer
        default: 20
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath:       api,
		rtPath:        rt,
		traitsLibPath: traitsLib,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.NoError(t, err)

	apiEntry := r.EntryPoint().(*APIFragment)
	ep, ok := apiEntry.EndPoints.Get("/items")
	require.True(t, ok, "expected /items endpoint")
	op, ok := ep.Operations.Get("get")
	require.True(t, ok, "expected GET operation from RT")

	// The paged trait contributes query parameters via RTTraits → applyTraits.
	require.NotNil(t, op.QueryParameters, "expected query parameters from paged trait")
	_, hasPage := op.QueryParameters.Get("page")
	require.True(t, hasPage, "expected page query parameter from paged trait")
	_, hasPageSize := op.QueryParameters.Get("pageSize")
	require.True(t, hasPageSize, "expected pageSize query parameter from paged trait")
}

// TestRTFragmentUnqualifiedTraitForbidden verifies the converse of the lexical-scoping
// rule: an unqualified trait name written inside a ResourceType fragment (is: [paged])
// must NOT resolve against the including API's trait namespace. The fragment has no
// local traits: and the bare name carries no uses: alias, so resolution fails — even
// though the API declares a matching "paged" trait.
func TestRTFragmentUnqualifiedTraitForbidden(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	rtPath := filepath.Join(base, "resourceTypes", "collection.raml")

	api := `#%RAML 1.0
title: RT Unqualified Trait Antipattern
traits:
  paged:
    queryParameters:
      page:
        type: integer
        default: 1
resourceTypes:
  collection: !include resourceTypes/collection.raml
/items:
  type: collection
`
	// The RT fragment references "paged" by unqualified name. Under lexical scoping
	// this is a dangling reference: it only exists in the including API's namespace.
	rt := `#%RAML 1.0 ResourceType
get:
  is: [paged]
  responses:
    200:
      body:
        application/json:
          type: object
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath: api,
		rtPath:  rt,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.Error(t, err, "expected error: unqualified trait name in RT fragment must not resolve against the including API")
}

// TestRTFragmentParentTypeQualified verifies RT→RT inheritance under lexical scoping:
// a parent type: written inside a ResourceType fragment resolves against that
// fragment's own uses:/resourceTypes, NOT the including API. The fragment imports the
// base resource type via its own uses: and references it with a qualified name.
func TestRTFragmentParentTypeQualified(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	rtPath := filepath.Join(base, "resourceTypes", "collection.raml")
	baseRTPath := filepath.Join(base, "resourceTypes", "base.raml")

	api := `#%RAML 1.0
title: RT Parent Type Qualified
resourceTypes:
  collection: !include resourceTypes/collection.raml
/items:
  type: collection
`
	// The RT fragment inherits a base resource type imported via its own uses:.
	rt := `#%RAML 1.0 ResourceType
uses:
  rtLib: base.raml
type: rtLib.base
get:
  responses:
    200:
      body:
        application/json:
          type: object
`
	baseRT := `#%RAML 1.0 Library
resourceTypes:
  base:
    post:
      responses:
        201:
          body:
            application/json:
              type: object
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath:    api,
		rtPath:     rt,
		baseRTPath: baseRT,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.NoError(t, err)

	apiEntry := r.EntryPoint().(*APIFragment)
	ep, ok := apiEntry.EndPoints.Get("/items")
	require.True(t, ok, "expected /items endpoint")
	// The GET comes from the collection RT; the POST is inherited from the base RT.
	_, hasGet := ep.Operations.Get("get")
	require.True(t, hasGet, "expected GET operation from collection RT")
	_, hasPost := ep.Operations.Get("post")
	require.True(t, hasPost, "expected POST operation inherited from base RT via the fragment's own uses:")
}

// TestRTFragmentUnqualifiedParentTypeForbidden verifies the converse: an unqualified
// parent type: written inside a ResourceType fragment must NOT resolve against the
// including API's resourceTypes: namespace, even when the API declares a matching name.
func TestRTFragmentUnqualifiedParentTypeForbidden(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	rtPath := filepath.Join(base, "resourceTypes", "collection.raml")

	api := `#%RAML 1.0
title: RT Unqualified Parent Type Antipattern
resourceTypes:
  base:
    post:
      responses:
        201:
          body:
            application/json:
              type: object
  collection: !include resourceTypes/collection.raml
/items:
  type: collection
`
	// The RT fragment references "base" by unqualified name. Under lexical scoping this
	// is a dangling reference: "base" only exists in the including API's namespace.
	rt := `#%RAML 1.0 ResourceType
type: base
get:
  responses:
    200:
      body:
        application/json:
          type: object
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath: api,
		rtPath:  rt,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.Error(t, err, "expected error: unqualified parent type in RT fragment must not resolve against the including API")
}

// TestTraitFragmentStaticBodyTypeForbidden verifies that a typed Trait fragment whose
// body references an unqualified type without a uses: declaration is rejected.
// This is the antipattern: the type name is only meaningful in the including API's
// namespace, making the fragment semantically non-standalone.
//
//	#%RAML 1.0 Trait
//	responses:
//	  200:
//	    body:
//	      application/json:
//	        type: PagedResult    # ← dangling: PagedResult is not in this fragment's uses:
func TestTraitFragmentStaticBodyTypeForbidden(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	traitPath := filepath.Join(base, "traits", "paged.raml")

	api := `#%RAML 1.0
title: Static Ref Antipattern
types:
  PagedResult:
    type: object
    properties:
      items: any[]
traits:
  paged: !include traits/paged.raml
/items:
  get:
    is: [paged]
`
	// The trait body hard-codes an unqualified type name that only exists in the
	// including API. No <<parameter>> involved — this is pure ambient coupling.
	trait := `#%RAML 1.0 Trait
responses:
  200:
    body:
      application/json:
        type: PagedResult
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath:   api,
		traitPath: trait,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.Error(t, err, "expected error: unqualified type in trait body must not resolve against the including API")
}

// TestInlineTraitForwardRefType verifies that an inline parameter-free trait whose body
// references an unqualified type resolves correctly even when the types: section is
// declared AFTER traits: in the same document. This is the common real-world layout:
// traits are listed near the top, types at the bottom.
//
// Resolution is deferred to the unwrap phase, by which time all types are registered,
// so forward references within the same document always work.
//
// This mirrors the pattern:
//
//	traits:
//	  hasError:
//	    responses:
//	      401:
//	        body:
//	          application/json:
//	            type: ErrorBody   # ← declared below in types:
//	/items:
//	  get:
//	    is: [hasError]
//	types:
//	  ErrorBody:
//	    type: object
func TestInlineTraitForwardRefType(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")

	// types: comes AFTER traits: to exercise the forward-reference / deferred-resolution
	// path that governs inline trait eager precompilation.
	api := `#%RAML 1.0
title: Inline Trait Forward Ref
traits:
  hasError:
    responses:
      401:
        body:
          application/json:
            type: ErrorBody
/items:
  get:
    is: [hasError]
    responses:
      200:
        body:
          application/json:
            type: string
types:
  ErrorBody:
    type: object
    properties:
      message: string
`
	r := makeTestRAMLWithFS(t, testFS{apiPath: api})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.NoError(t, err)

	apiEntry := r.EntryPoint().(*APIFragment)
	ep, ok := apiEntry.EndPoints.Get("/items")
	require.True(t, ok)
	op, ok := ep.Operations.Get("get")
	require.True(t, ok)
	resp, ok := op.Responses.Get(401)
	require.True(t, ok, "expected 401 response contributed by hasError trait")
	body, ok := resp.Bodies.Get("application/json")
	require.True(t, ok)
	require.NotNil(t, body.Shape)
	require.Equal(t, TypeObject, body.Shape.Type,
		"inline trait body type must resolve against the API's own types: even when declared after traits:")
}

// TestTraitFragmentParamUnqualifiedType verifies that a <<parameter>> value supplied
// by the applying document CAN be an unqualified type from that document's namespace.
// This is the legitimate injection mechanism: the trait declares the variable explicitly,
// and the caller names a type from its own scope at the call site.
//
//	# API:  is: [paged: {responseType: PagedResult}]   ← caller names its own type
//	# Trait: type: <<responseType>>                     ← trait receives it as a parameter
func TestTraitFragmentParamUnqualifiedType(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	traitPath := filepath.Join(base, "traits", "paged.raml")

	api := `#%RAML 1.0
title: Param Injection Test
types:
  PagedResult:
    type: object
    properties:
      items: any[]
traits:
  paged: !include traits/paged.raml
/items:
  get:
    is:
      - paged:
          responseType: PagedResult
`
	// The trait body is fully standalone: it has no hard-coded external type names.
	// The type is injected by the caller via <<responseType>>.
	trait := `#%RAML 1.0 Trait
usage: Use to describe a pageable collection. Pass responseType for the 200 body.
responses:
  200:
    body:
      application/json:
        type: <<responseType>>
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath:   api,
		traitPath: trait,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.NoError(t, err)

	api200 := r.EntryPoint().(*APIFragment)
	ep, ok := api200.EndPoints.Get("/items")
	require.True(t, ok, "expected /items endpoint")
	op, ok := ep.Operations.Get("get")
	require.True(t, ok, "expected GET operation")
	body, ok := op.Responses.Get(200)
	require.True(t, ok, "expected 200 response from applied trait")
	appJSON, ok := body.Bodies.Get("application/json")
	require.True(t, ok, "expected application/json body")
	require.NotNil(t, appJSON.Shape)
	require.Equal(t, TypeObject, appJSON.Shape.Type,
		"parameter value PagedResult (an object) must resolve to object type")
}

// TestTraitLibQualifiedTypeFromOtherLib verifies provenance for the cross-library
// case: a trait library declares uses: typ: types.raml and the trait body
// statically references typ.Paging. The API applies the trait but has no typ:
// alias of its own. Provenance must keep the static trait body in the trait
// library's declaration scope so typ.Paging resolves against types.raml, not the
// API's namespace.
func TestTraitLibQualifiedTypeFromOtherLib(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	traitsLibPath := filepath.Join(base, "traits.raml")
	typesLibPath := filepath.Join(base, "types.raml")

	typesLib := `#%RAML 1.0 Library
types:
  Paging:
    type: object
    properties:
      cursor: string
`
	// traits.raml imports types.raml under its own "typ" alias; the trait body
	// statically references typ.Paging. No <<parameter>> is involved — the type
	// reference must resolve in the trait library's own namespace.
	traitsLib := `#%RAML 1.0 Library
uses:
  typ: types.raml
traits:
  ParametrizedTrait:
    queryParameters:
      cursor:
        type: typ.Paging
`
	// The API uses only traits.raml and applies the qualified trait. Crucially
	// the API itself has no "typ" alias.
	api := `#%RAML 1.0
title: Provenance Cross-Library
uses:
  traitsLib: traits.raml
/items:
  get:
    is: [traitsLib.ParametrizedTrait]
    responses:
      200:
        body:
          application/json:
            type: string
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath:       api,
		traitsLibPath: traitsLib,
		typesLibPath:  typesLib,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.NoError(t, err, "static trait body reference typ.Paging must resolve in the trait library's scope, not the API's")

	apiEntry := r.EntryPoint().(*APIFragment)
	ep, ok := apiEntry.EndPoints.Get("/items")
	require.True(t, ok, "expected /items endpoint")
	op, ok := ep.Operations.Get("get")
	require.True(t, ok, "expected GET operation")
	require.NotNil(t, op.QueryParameters, "expected query parameters from trait")
	cursor, ok := op.QueryParameters.Get("cursor")
	require.True(t, ok, "expected cursor query parameter from trait")
	require.NotNil(t, cursor.Base)
	require.Equal(t, TypeObject, cursor.Base.Type,
		"cursor type typ.Paging must resolve to the object type from types.raml via the trait library's uses:")
}

// TestTraitLibSameLibTypeRef verifies provenance for the single-library case:
// a library declares both the type and the trait that references it by a bare,
// unqualified name. The API applies the qualified trait but does not declare the
// bare type. Provenance must keep the static trait body in that library's scope
// so the unqualified Paging resolves against the library's own types, not the
// API's namespace.
func TestTraitLibSameLibTypeRef(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	libPath := filepath.Join(base, "lib.raml")

	// The library defines Paging and a trait that statically references it by
	// the bare name — only meaningful in the library's own namespace.
	lib := `#%RAML 1.0 Library
types:
  Paging:
    type: object
    properties:
      cursor: string
traits:
  ParametrizedTrait:
    queryParameters:
      cursor:
        type: Paging
`
	api := `#%RAML 1.0
title: Provenance Same-Library
uses:
  lib: lib.raml
/items:
  get:
    is: [lib.ParametrizedTrait]
    responses:
      200:
        body:
          application/json:
            type: string
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath: api,
		libPath: lib,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.NoError(t, err, "static trait body reference Paging must resolve in the library's own scope")

	apiEntry := r.EntryPoint().(*APIFragment)
	ep, ok := apiEntry.EndPoints.Get("/items")
	require.True(t, ok, "expected /items endpoint")
	op, ok := ep.Operations.Get("get")
	require.True(t, ok, "expected GET operation")
	require.NotNil(t, op.QueryParameters, "expected query parameters from trait")
	cursor, ok := op.QueryParameters.Get("cursor")
	require.True(t, ok, "expected cursor query parameter from trait")
	require.NotNil(t, cursor.Base)
	require.Equal(t, TypeObject, cursor.Base.Type,
		"cursor type Paging must resolve to the object type in the library's own namespace")
}

// TestRTLibSameLibTypeRef is the resource-type analogue of TestTraitLibSameLibTypeRef:
// a single library declares both a type and a resource type whose static body
// references that type by bare name. The API uses the library and applies the
// qualified resource type. Provenance must keep the static RT body in the
// library's declaration scope so the unqualified type name resolves against
// the library's own types, not the API's namespace (which has none).
func TestRTLibSameLibTypeRef(t *testing.T) {
	base := t.TempDir()
	apiPath := filepath.Join(base, "api.raml")
	libPath := filepath.Join(base, "lib.raml")

	// The library defines both LocalType and a resource type that statically
	// references LocalType by the bare name — meaningful only in the library's
	// own namespace.
	lib := `#%RAML 1.0 Library
types:
  LocalType:
    type: object
    properties:
      id: integer
resourceTypes:
  RTWithLocalType:
    get:
      responses:
        200:
          body:
            application/json:
              type: LocalType
`
	api := `#%RAML 1.0
title: Provenance Same-Library RT
uses:
  lib: lib.raml
/items:
  type: lib.RTWithLocalType
`
	r := makeTestRAMLWithFS(t, testFS{
		apiPath: api,
		libPath: lib,
	})
	err := r.ParseFromPath(apiPath, OptWithUnwrap())
	require.NoError(t, err, "static RT body reference LocalType must resolve in the library's own scope")

	apiEntry := r.EntryPoint().(*APIFragment)
	ep, ok := apiEntry.EndPoints.Get("/items")
	require.True(t, ok, "expected /items endpoint")
	op, ok := ep.Operations.Get("get")
	require.True(t, ok, "expected GET operation contributed by the resource type")
	resp, ok := op.Responses.Get(200)
	require.True(t, ok, "expected 200 response contributed by the resource type")
	body, ok := resp.Bodies.Get("application/json")
	require.True(t, ok, "expected application/json body")
	require.NotNil(t, body.Shape)
	require.Equal(t, TypeObject, body.Shape.Type,
		"static RT body type LocalType must resolve to the object type in the library's own namespace")
}

// TestOptWithWorkspaceRoot verifies that the default file:// loader
// (SafeOSFileLoader, installed when OptWithFileLoader is not supplied)
// restricts all file loading — fragment uses:, !include directives, and
// inline JSON Schema $ref targets — to paths inside the declared workspace
// root. Tests use real temp files so the actual safeopen-backed I/O path is
// exercised; custom in-memory loaders (testFS) are now solely responsible
// for their own bounds and are covered separately at the end of this test.
func TestOptWithWorkspaceRoot(t *testing.T) {
	workspaceDir := t.TempDir()
	outsideDir := t.TempDir()

	const libContent = "#%RAML 1.0 Library\ntypes:\n  A:\n    type: string\n"

	// writeFile creates a file on disk and returns its OS path.
	writeFile := func(t *testing.T, dir, name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}

	t.Run("uses inside workspace allowed", func(t *testing.T) {
		insideLibPath := writeFile(t, workspaceDir, "lib.raml", libContent)
		rel, relErr := filepath.Rel(workspaceDir, insideLibPath)
		require.NoError(t, relErr)
		apiPath := writeFile(t, workspaceDir, "api.raml",
			"#%RAML 1.0\ntitle: T\nuses:\n  lib: "+rel+"\n")
		r := New(context.Background())
		require.NoError(t, r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir)))
	})

	t.Run("uses outside workspace rejected", func(t *testing.T) {
		outsideLibPath := writeFile(t, outsideDir, "lib.raml", libContent)
		rel, relErr := filepath.Rel(workspaceDir, outsideLibPath)
		require.NoError(t, relErr)
		apiPath := writeFile(t, workspaceDir, "api_out.raml",
			"#%RAML 1.0\ntitle: T\nuses:\n  lib: "+rel+"\n")
		r := New(context.Background())
		err := r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir))
		require.Error(t, err)
		require.Contains(t, err.Error(), "outside workspace root")
	})

	t.Run("default workspace root restricts outside access", func(t *testing.T) {
		// Without OptWithWorkspaceRoot the default is filepath.Dir(apiPath),
		// which equals workspaceDir. Accessing a file in outsideDir must be rejected.
		outsideLibPath := writeFile(t, outsideDir, "lib_def.raml", libContent)
		rel, relErr := filepath.Rel(workspaceDir, outsideLibPath)
		require.NoError(t, relErr)
		apiPath := writeFile(t, workspaceDir, "api_def.raml",
			"#%RAML 1.0\ntitle: T\nuses:\n  lib: "+rel+"\n")
		r := New(context.Background())
		err := r.ParseFromPath(apiPath)
		require.Error(t, err)
		require.Contains(t, err.Error(), "outside workspace root")
	})

	t.Run("include inside workspace allowed", func(t *testing.T) {
		writeFile(t, workspaceDir, "type.raml", "#%RAML 1.0 DataType\ntype: string\n")
		apiPath := writeFile(t, workspaceDir, "api_inc.raml",
			"#%RAML 1.0\ntitle: T\ntypes:\n  MyType: !include type.raml\n")
		r := New(context.Background())
		require.NoError(t, r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir)))
	})

	t.Run("include outside workspace rejected", func(t *testing.T) {
		outsideTypePath := writeFile(t, outsideDir, "type_out.raml",
			"#%RAML 1.0 DataType\ntype: string\n")
		rel, relErr := filepath.Rel(workspaceDir, outsideTypePath)
		require.NoError(t, relErr)
		apiPath := writeFile(t, workspaceDir, "api_inc_out.raml",
			"#%RAML 1.0\ntitle: T\ntypes:\n  MyType: !include "+rel+"\n")
		r := New(context.Background())
		err := r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir))
		require.Error(t, err)
		require.Contains(t, err.Error(), "outside workspace root")
	})

	// JSON Schema $refs are resolved through the same ResourceLoader as RAML
	// !include directives (see metaschema.go: resourceLoaderAdapter), so the
	// default SafeOSFileLoader enforces the bound here too.

	t.Run("json schema datatype !include inside workspace allowed", func(t *testing.T) {
		writeFile(t, workspaceDir, "schema_in.json", `{}`)
		apiPath := writeFile(t, workspaceDir, "api_js_in.raml",
			"#%RAML 1.0\ntitle: T\ntypes:\n  Foo:\n    type: !include schema_in.json\n")
		r := New(context.Background())
		require.NoError(t, r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir)))
	})

	t.Run("json schema datatype !include outside workspace rejected", func(t *testing.T) {
		outsideSchemaPath := writeFile(t, outsideDir, "schema_out.json", `{}`)
		rel, relErr := filepath.Rel(workspaceDir, outsideSchemaPath)
		require.NoError(t, relErr)
		apiPath := writeFile(t, workspaceDir, "api_js_out.raml",
			"#%RAML 1.0\ntitle: T\ntypes:\n  Foo:\n    type: !include "+rel+"\n")
		r := New(context.Background())
		err := r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir))
		require.Error(t, err)
		require.Contains(t, err.Error(), "outside workspace root")
	})

	t.Run("inline json schema $ref inside workspace allowed", func(t *testing.T) {
		writeFile(t, workspaceDir, "person.json", `{"type": "object"}`)
		apiPath := writeFile(t, workspaceDir, "api_ref_in.raml",
			"#%RAML 1.0\ntitle: T\ntypes:\n  Foo:\n    type: '{\"$ref\": \"person.json\"}'\n")
		r := New(context.Background())
		require.NoError(t, r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir)))
	})

	t.Run("inline json schema $ref outside workspace rejected", func(t *testing.T) {
		outsideSchemaPath := writeFile(t, outsideDir, "person_out.json", `{"type": "object"}`)
		rel, relErr := filepath.Rel(workspaceDir, outsideSchemaPath)
		require.NoError(t, relErr)
		// $ref uses URI syntax (forward slashes).
		apiPath := writeFile(t, workspaceDir, "api_ref_out.raml",
			"#%RAML 1.0\ntitle: T\ntypes:\n  Foo:\n    type: '{\"$ref\": \""+filepath.ToSlash(rel)+"\"}'\n")
		r := New(context.Background())
		err := r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir))
		require.Error(t, err)
		require.Contains(t, err.Error(), "outside workspace root")
	})

	// With a custom file loader installed (via setLoader / OptWithFileLoader),
	// the loader takes full ownership of safety; the workspace root governs
	// path-resolution semantics only and does not constrain I/O. These
	// subtests verify that the custom loader is consulted directly.
	t.Run("custom FS: inside workspace served by custom loader", func(t *testing.T) {
		insideLibPath := filepath.Join(workspaceDir, "lib2.raml")
		rel, relErr := filepath.Rel(workspaceDir, insideLibPath)
		require.NoError(t, relErr)
		apiPath := filepath.Join(workspaceDir, "api_custom.raml")
		apiContent := "#%RAML 1.0\ntitle: T\nuses:\n  lib: " + rel + "\n"
		r := makeTestRAMLWithFS(t, testFS{
			apiPath:       apiContent,
			insideLibPath: libContent,
		})
		require.NoError(t, r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir)))
	})

	t.Run("custom FS: custom loader is consulted, no fallback to OS", func(t *testing.T) {
		// The custom FS holds the file; if a default OS loader were consulted
		// instead of the custom loader, the in-memory content would not be
		// found and parsing would fail with a file-not-found error.
		insideLibPath := filepath.Join(workspaceDir, "lib3.raml")
		rel, relErr := filepath.Rel(workspaceDir, insideLibPath)
		require.NoError(t, relErr)
		apiPath := filepath.Join(workspaceDir, "api_custom_only.raml")
		apiContent := "#%RAML 1.0\ntitle: T\nuses:\n  lib: " + rel + "\n"
		r := makeTestRAMLWithFS(t, testFS{
			apiPath:       apiContent,
			insideLibPath: libContent,
		})
		require.NoError(t, r.ParseFromPath(apiPath, OptWithWorkspaceRoot(workspaceDir)))
	})
}

// httpServer is a convenience wrapper around httptest.NewServer that registers
// a set of path→content pairs and shuts down when the test ends.
func httpServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, content := range files {
		body := content // capture
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestOptWithHTTPClient(t *testing.T) {
	const libContent = "#%RAML 1.0 Library\ntypes:\n  Foo:\n    type: string\n"
	const dtContent = "#%RAML 1.0 DataType\ntype: string\n"

	t.Run("uses: library loaded over HTTP", func(t *testing.T) {
		srv := httpServer(t, map[string]string{"/lib.raml": libContent})
		apiContent := "#%RAML 1.0\ntitle: T\nuses:\n  lib: " + srv.URL + "/lib.raml\n"

		base := t.TempDir()
		r := makeTestRAML(t)
		err := r.ParseFromString(apiContent, "api.raml", base, OptWithHTTPClient(srv.Client()))
		require.NoError(t, err)
	})

	t.Run("!include DataType loaded over HTTP", func(t *testing.T) {
		srv := httpServer(t, map[string]string{"/dtype.raml": dtContent})
		apiContent := "#%RAML 1.0 DataType\ntype: !include " + srv.URL + "/dtype.raml\n"

		base := t.TempDir()
		r := makeTestRAML(t)
		err := r.ParseFromString(apiContent, "dtype.raml", base, OptWithHTTPClient(srv.Client()))
		require.NoError(t, err)
	})

	t.Run("HTTP 404 propagates as error", func(t *testing.T) {
		srv := httpServer(t, map[string]string{}) // no routes — everything 404s
		apiContent := "#%RAML 1.0\ntitle: T\nuses:\n  lib: " + srv.URL + "/missing.raml\n"

		base := t.TempDir()
		r := makeTestRAML(t)
		err := r.ParseFromString(apiContent, "api.raml", base, OptWithHTTPClient(srv.Client()))
		require.Error(t, err)
		require.Contains(t, err.Error(), "status 404")
	})

	t.Run("HTTP URI rejected without OptWithHTTPClient", func(t *testing.T) {
		srv := httpServer(t, map[string]string{"/lib.raml": libContent})
		apiContent := "#%RAML 1.0\ntitle: T\nuses:\n  lib: " + srv.URL + "/lib.raml\n"

		base := t.TempDir()
		r := makeTestRAML(t)
		// No OptWithHTTPClient — SchemeLoader has no "http" entry.
		err := r.ParseFromString(apiContent, "api.raml", base)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no loader registered for URI scheme")
	})

	t.Run("custom http.Client is used (intercepts requests)", func(t *testing.T) {
		srv := httpServer(t, map[string]string{"/lib.raml": libContent})

		// Wrap the test server's client with a transport that records calls.
		called := false
		transport := &recordingTransport{
			delegate:  srv.Client().Transport,
			onRequest: func() { called = true },
		}
		customClient := &http.Client{Transport: transport}

		apiContent := "#%RAML 1.0\ntitle: T\nuses:\n  lib: " + srv.URL + "/lib.raml\n"
		base := t.TempDir()
		r := makeTestRAML(t)
		err := r.ParseFromString(apiContent, "api.raml", base, OptWithHTTPClient(customClient))
		require.NoError(t, err)
		require.True(t, called, "custom http.Client.Transport was not used")
	})
}

// recordingTransport wraps an http.RoundTripper and fires onRequest for each call.
type recordingTransport struct {
	delegate  http.RoundTripper
	onRequest func()
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.onRequest()
	return rt.delegate.RoundTrip(req)
}
