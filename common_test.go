package raml

import (
	"testing"
)

func TestCutLast(t *testing.T) {
	tests := []struct {
		name       string
		s          string
		sep        byte
		wantBefore string
		wantAfter  string
		wantFound  bool
	}{
		{
			name:       "separator present once",
			s:          "foo/bar",
			sep:        '/',
			wantBefore: "foo",
			wantAfter:  "bar",
			wantFound:  true,
		},
		{
			name:       "separator present multiple times — last is cut",
			s:          "foo/bar/baz",
			sep:        '/',
			wantBefore: "foo/bar",
			wantAfter:  "baz",
			wantFound:  true,
		},
		{
			name:       "separator absent",
			s:          "foobar",
			sep:        '/',
			wantBefore: "foobar",
			wantAfter:  "",
			wantFound:  false,
		},
		{
			name:       "empty string",
			s:          "",
			sep:        '/',
			wantBefore: "",
			wantAfter:  "",
			wantFound:  false,
		},
		{
			name:       "separator at start",
			s:          "/bar",
			sep:        '/',
			wantBefore: "",
			wantAfter:  "bar",
			wantFound:  true,
		},
		{
			name:       "separator at end",
			s:          "foo/",
			sep:        '/',
			wantBefore: "foo",
			wantAfter:  "",
			wantFound:  true,
		},
		{
			name:       "separator is the only character",
			s:          "/",
			sep:        '/',
			wantBefore: "",
			wantAfter:  "",
			wantFound:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after, found := CutLast(tt.s, tt.sep)
			if before != tt.wantBefore {
				t.Errorf("before = %q, want %q", before, tt.wantBefore)
			}
			if after != tt.wantAfter {
				t.Errorf("after = %q, want %q", after, tt.wantAfter)
			}
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
		})
	}
}

func Test_isValidProtocol(t *testing.T) {
	tests := []struct {
		name string
		p    string
		want bool
	}{
		{name: "lowercase http", p: "http", want: true},
		{name: "lowercase https", p: "https", want: true},
		{name: "uppercase HTTP", p: "HTTP", want: true},
		{name: "uppercase HTTPS", p: "HTTPS", want: true},
		{name: "mixed case Http", p: "Http", want: true},
		{name: "ftp is invalid", p: "ftp", want: false},
		{name: "empty string", p: "", want: false},
		{name: "ws is invalid", p: "ws", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidProtocol(tt.p); got != tt.want {
				t.Errorf("isValidProtocol(%q) = %v, want %v", tt.p, got, tt.want)
			}
		})
	}
}

func Test_isValidMediaType(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		want      bool
	}{
		{name: "simple application/json", mediaType: "application/json", want: true},
		{name: "text/plain", mediaType: "text/plain", want: true},
		{name: "multipart/form-data", mediaType: "multipart/form-data", want: true},
		{name: "application/vnd.api+json", mediaType: "application/vnd.api+json", want: true},
		{name: "application/x-www-form-urlencoded", mediaType: "application/x-www-form-urlencoded", want: true},
		{name: "no slash", mediaType: "applicationjson", want: false},
		{name: "empty string", mediaType: "", want: false},
		{name: "slash only", mediaType: "/", want: false},
		{name: "missing subtype", mediaType: "application/", want: false},
		{name: "missing type", mediaType: "/json", want: false},
		{name: "space in type", mediaType: "application /json", want: false},
		{name: "asterisk wildcard not allowed", mediaType: "application/*", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidMediaType(tt.mediaType); got != tt.want {
				t.Errorf("isValidMediaType(%q) = %v, want %v", tt.mediaType, got, tt.want)
			}
		})
	}
}

func Test_duplicateItems(t *testing.T) {
	tests := []struct {
		name      string
		arr       []any
		wantFound bool // whether a duplicate pair is expected
	}{
		{
			name:      "empty slice",
			arr:       []any{},
			wantFound: false,
		},
		{
			name:      "single element",
			arr:       []any{"a"},
			wantFound: false,
		},
		{
			name:      "all unique strings — small",
			arr:       []any{"a", "b", "c"},
			wantFound: false,
		},
		{
			name:      "duplicate strings — small",
			arr:       []any{"a", "b", "a"},
			wantFound: true,
		},
		{
			name:      "duplicate integers — small",
			arr:       []any{1, 2, 1},
			wantFound: true,
		},
		{
			name:      "duplicate bool — small",
			arr:       []any{true, false, true},
			wantFound: true,
		},
		{
			name:      "nil duplicate — small",
			arr:       []any{nil, nil},
			wantFound: true,
		},
		{
			name:      "duplicate maps — small",
			arr:       []any{map[string]any{"k": "v"}, map[string]any{"k": "v"}},
			wantFound: true,
		},
		{
			name:      "different maps are unique — small",
			arr:       []any{map[string]any{"k": "v"}, map[string]any{"k": "x"}},
			wantFound: false,
		},
		{
			name:      "duplicate slices — small",
			arr:       []any{[]any{"x", "y"}, []any{"x", "y"}},
			wantFound: true,
		},
		{
			name:      "int and float with same value are equal",
			arr:       []any{1, float64(1)},
			wantFound: true,
		},
	}

	// Build a large (>20) unique slice to exercise the hash path.
	largeUnique := make([]any, 25)
	for i := range largeUnique {
		largeUnique[i] = i
	}
	largeDuplicate := make([]any, 25)
	copy(largeDuplicate, largeUnique)
	largeDuplicate[24] = 0 // duplicate of index 0

	tests = append(tests,
		struct {
			name      string
			arr       []any
			wantFound bool
		}{name: "all unique — large (hash path)", arr: largeUnique, wantFound: false},
		struct {
			name      string
			arr       []any
			wantFound bool
		}{name: "duplicate present — large (hash path)", arr: largeDuplicate, wantFound: true},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i, j := duplicateItems(tt.arr)
			if tt.wantFound {
				if i < 0 || j < 0 {
					t.Errorf("duplicateItems() = (%d, %d), want a valid duplicate pair", i, j)
					return
				}
				if !semanticEqual(tt.arr[i], tt.arr[j]) {
					t.Errorf("duplicateItems() returned (%d, %d) but arr[%d]=%v and arr[%d]=%v are not equal",
						i, j, i, tt.arr[i], j, tt.arr[j])
				}
			} else {
				if i != -1 || j != -1 {
					t.Errorf("duplicateItems() = (%d, %d), want (-1, -1)", i, j)
				}
			}
		})
	}
}
