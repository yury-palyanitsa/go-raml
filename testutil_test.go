package raml

import (
	"context"
	"io"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/acronis/go-stacktrace"
)

// Compile-time check: mockReadSeeker must satisfy io.ReadSeeker.
var _ io.ReadSeeker = (*mockReadSeeker)(nil)

// nodeOf wraps a plain value in a *Node[T] for test construction
// (no *yaml.Node available, so KeyPos/ValuePos remain zero).
func nodeOf[T any](v T) *Node[T]               { return &Node[T]{Value: v} }
func scalarFacetOf[T any](v T) *ScalarFacet[T] { return &ScalarFacet[T]{Value: v} }

// ratOf parses a decimal string into a *big.Rat for use in NumberFacets test construction.
func ratOf(s string) *big.Rat {
	r, _ := new(big.Rat).SetString(s)
	return r
}

// linkBaseShape links a shape with its BaseShape.
// base.Shape is set to s, and s's embedded BaseShape field is set to base.
func linkBaseShape[T Shape](s T, base *BaseShape) T {
	base.Shape = s
	switch v := any(s).(type) {
	case *IntegerShape:
		v.BaseShape = base
	case *NumberShape:
		v.BaseShape = base
	case *StringShape:
		v.BaseShape = base
	case *BooleanShape:
		v.BaseShape = base
	case *FileShape:
		v.BaseShape = base
	case *DateTimeShape:
		v.BaseShape = base
	case *DateTimeOnlyShape:
		v.BaseShape = base
	case *DateOnlyShape:
		v.BaseShape = base
	case *TimeOnlyShape:
		v.BaseShape = base
	case *ArrayShape:
		v.BaseShape = base
	case *ObjectShape:
		v.BaseShape = base
	case *UnionShape:
		v.BaseShape = base
	case *AnyShape:
		v.BaseShape = base
	case *NilShape:
		v.BaseShape = base
	case *JSONShape:
		v.BaseShape = base
	case *UnknownShape:
		v.BaseShape = base
	case *RecursiveShape:
		v.BaseShape = base
	}
	return s
}

// NewTestShape creates a shape with a minimal BaseShape.
//
//	s := NewTestShape(&StringShape{}, 1)
//	s.BaseShape.Enum = Nodes{{Value: "a"}}
func NewTestShape[T Shape](s T, id int64) T {
	return linkBaseShape(s, &BaseShape{ID: id})
}

// NewTestShapeWithBase creates a shape with a custom BaseShape.
// Use this when you need additional metadata on BaseShape (e.g., Type, Enum, Description).
//
//	s := NewTestShapeWithBase(&StringShape{}, &BaseShape{
//		ID:     1,
//		Type:   "string",
//		Enum:   Nodes{{Value: "a"}, {Value: "b"}},
//	})
func NewTestShapeWithBase[T Shape](s T, base *BaseShape) T {
	return linkBaseShape(s, base)
}

// NewLinkedBase creates a BaseShape with a properly linked Shape.
// Use this when you need to create a Property or PatternProperty with a nested shape.
//
//	Base: NewLinkedBase(&StringShape{}, &BaseShape{ID: 1})
func NewLinkedBase[T Shape](s T, base *BaseShape) *BaseShape {
	linkBaseShape(s, base)
	return base
}

// makeTestRAML returns an initialized RAML instance for use in tests.
func makeTestRAML(t *testing.T) *RAML {
	t.Helper()
	return New(context.Background())
}

// makeTestBase creates a properly initialized BaseShape via RAML.
func makeTestBase(t *testing.T, r *RAML, name string) *BaseShape {
	t.Helper()
	return r.MakeBaseShape(name, "test.raml", stacktrace.Position{}, stacktrace.Position{})
}

// makeString creates a StringShape linked to a fresh BaseShape.
func makeString(t *testing.T, r *RAML) (*StringShape, *BaseShape) {
	t.Helper()
	base := makeTestBase(t, r, "s")
	s := &StringShape{BaseShape: base}
	base.SetShape(s)
	base.Type = TypeString
	return s, base
}

// makeUnionBase creates a UnionShape BaseShape with the given members.
func makeUnionBase(t *testing.T, r *RAML, members ...*BaseShape) *BaseShape {
	t.Helper()
	base := makeTestBase(t, r, "u")
	u := &UnionShape{BaseShape: base, UnionFacets: UnionFacets{AnyOf: members}}
	base.SetShape(u)
	base.Type = TypeUnion
	return base
}

// enumNode builds a single-value *DataNode for use in Enum fields.
func enumNode(v string) *DataNode {
	return &DataNode{Value: NewScalarNodeValue(v)}
}

// dataNodeOf creates a *DataNode holding an arbitrary scalar value.
// For string values prefer enumNode.
func dataNodeOf(v any) *DataNode {
	return &DataNode{Value: NewScalarNodeValue(v)}
}

// makeExample creates an *Example wrapping the given data node.
func makeExample(data *DataNode) *Example {
	return &Example{Data: data}
}

// testFS is a minimal in-memory ResourceLoader for tests.
// Keys are absolute OS paths, values are file contents.
type testFS map[string]string

func (m testFS) Load(uri string) (io.ReadCloser, error) {
	path := FileURIToPath(uri)
	if content, ok := m[path]; ok {
		return io.NopCloser(strings.NewReader(content)), nil
	}
	return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
}

// mockReadSeeker is an in-memory io.ReadSeeker for injecting bytes or errors
// into parse functions that accept an io.ReadSeeker / io.Reader.
type mockReadSeeker struct {
	P       []byte
	ReadErr error
	SeekErr error
}

func (m *mockReadSeeker) Read(p []byte) (n int, err error) {
	if m.ReadErr != nil {
		return 0, m.ReadErr
	}
	if len(m.P) == 0 {
		return 0, io.EOF
	}
	n = copy(p, m.P)
	return n, io.EOF
}

func (m *mockReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if m.SeekErr != nil {
		return 0, m.SeekErr
	}
	return 0, nil
}

// makeTestRAMLWithFS returns an initialized RAML instance configured to read
// files from the provided in-memory filesystem.
func makeTestRAMLWithFS(t *testing.T, fs testFS) *RAML {
	t.Helper()
	r := New(context.Background())
	if fs != nil {
		r.setLoader(fs)
	}
	return r
}
