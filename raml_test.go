package raml

import (
	"context"
	"testing"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

func TestRAML_EntryPoint(t *testing.T) {
	lib := &Library{}
	r := &RAML{entryPoint: lib}
	if got := r.EntryPoint(); got != lib {
		t.Errorf("EntryPoint() = %v, want %v", got, lib)
	}
}

func TestRAML_SetEntryPoint(t *testing.T) {
	r := makeTestRAML(t)
	lib := &Library{}
	got := r.SetEntryPoint(lib)
	if got.EntryPoint() != lib {
		t.Errorf("SetEntryPoint() entry point = %v, want %v", got.EntryPoint(), lib)
	}
}

func TestRAML_GetLocation(t *testing.T) {
	tests := []struct {
		name       string
		entryPoint Fragment
		want       string
	}{
		{"with location", &Library{Location: "/tmp/lib.raml"}, "/tmp/lib.raml"},
		{"nil entry point", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RAML{entryPoint: tt.entryPoint}
			if got := r.GetLocation(); got != tt.want {
				t.Errorf("GetLocation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	r := New(context.Background())
	if r.fragmentTypes == nil {
		t.Error("fragmentTypes is nil")
	}
	if r.fragmentAnnotationTypes == nil {
		t.Error("fragmentAnnotationTypes is nil")
	}
	if r.fragmentTypeDefinitions == nil {
		t.Error("fragmentTypeDefinitions is nil")
	}
	if r.fragmentsCache == nil {
		t.Error("fragmentsCache is nil")
	}
	if r.domainExtensions == nil {
		t.Error("domainExtensions is nil")
	}
	if r.endPoints == nil {
		t.Error("endPoints is nil")
	}
	if r.ctx == nil {
		t.Error("ctx is nil")
	}
	if r.maxIncludeSize != DefaultMaxIncludeSize {
		t.Errorf("maxIncludeSize = %d, want %d", r.maxIncludeSize, DefaultMaxIncludeSize)
	}
}

func TestRAML_GetAllAnnotationsPtr(t *testing.T) {
	de := &DomainExtension{Location: "/tmp/a.raml"}
	r := &RAML{domainExtensions: []*DomainExtension{de}}
	got := r.GetAllAnnotationsPtr()
	if len(got) != 1 || got[0] != de {
		t.Errorf("GetAllAnnotationsPtr() = %v, want [%v]", got, de)
	}
}

func TestRAML_GetAllAnnotations(t *testing.T) {
	r := &RAML{domainExtensions: []*DomainExtension{
		{Location: "/tmp/a.raml"},
		{Location: "/tmp/b.raml"},
	}}
	got := r.GetAllAnnotations()
	if len(got) != 2 {
		t.Fatalf("GetAllAnnotations() len = %d, want 2", len(got))
	}
	if got[0].Location != "/tmp/a.raml" || got[1].Location != "/tmp/b.raml" {
		t.Errorf("GetAllAnnotations() = %v", got)
	}
}

// --- Shapes ------------------------------------------------------------------

func TestRAML_GetShapes(t *testing.T) {
	shape := &BaseShape{Location: "/tmp/s.raml"}
	r := &RAML{shapes: []*BaseShape{shape}}
	got := r.GetShapes()
	if len(got) != 1 || got[0] != shape {
		t.Errorf("GetShapes() = %v, want [%v]", got, shape)
	}
}

func TestRAML_PutShape(t *testing.T) {
	r := makeTestRAML(t)
	shape := &BaseShape{Location: "/tmp/s.raml"}
	r.PutShape(shape)
	got := r.GetShapes()
	if len(got) != 1 || got[0] != shape {
		t.Errorf("after PutShape, GetShapes() = %v, want [%v]", got, shape)
	}
}

// --- Fragment type maps -------------------------------------------------------

func TestRAML_GetFragmentTypePtrs(t *testing.T) {
	shape := &BaseShape{}
	r := &RAML{fragmentTypes: map[string]map[string]*BaseShape{
		"file:///loc": {"MyType": shape},
	}}
	if got := r.GetFragmentTypePtrs("/loc"); got["MyType"] != shape {
		t.Errorf("GetFragmentTypePtrs(\"/loc\") missing MyType")
	}
	if got := r.GetFragmentTypePtrs("missing"); got != nil {
		t.Errorf("GetFragmentTypePtrs(\"missing\") = %v, want nil", got)
	}
}

func TestRAML_GetTypeFromFragmentPtr(t *testing.T) {
	shape := &BaseShape{}
	r := &RAML{fragmentTypes: map[string]map[string]*BaseShape{
		"file:///loc": {"MyType": shape},
	}}
	tests := []struct {
		name     string
		location string
		typeName string
		wantNil  bool
		wantErr  bool
	}{
		{"found", "/loc", "MyType", false, false},
		{"type not in fragment", "/loc", "Other", true, false},
		{"fragment not found", "missing", "MyType", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.GetTypeFromFragmentPtr(tt.location, tt.typeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if (got == nil) != tt.wantNil {
				t.Errorf("got = %v, wantNil %v", got, tt.wantNil)
			}
		})
	}
}

func TestRAML_PutTypeIntoFragment(t *testing.T) {
	r := makeTestRAML(t)
	shape := &BaseShape{}
	r.PutTypeIntoFragment("MyType", "loc.raml", shape)
	got, err := r.GetTypeFromFragmentPtr("loc.raml", "MyType")
	if err != nil {
		t.Fatalf("GetTypeFromFragmentPtr() error = %v", err)
	}
	if got != shape {
		t.Errorf("got = %v, want %v", got, shape)
	}
}

// --- Fragment annotation type maps -------------------------------------------

func TestRAML_GetAnnotationTypeFromFragmentPtr(t *testing.T) {
	shape := &BaseShape{}
	r := &RAML{fragmentAnnotationTypes: map[string]map[string]*BaseShape{
		"file:///loc": {"MyAnnot": shape},
	}}
	tests := []struct {
		name     string
		location string
		typeName string
		wantNil  bool
		wantErr  bool
	}{
		{"found", "/loc", "MyAnnot", false, false},
		{"annotation not in fragment", "/loc", "Other", true, false},
		{"fragment not found", "missing", "MyAnnot", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.GetAnnotationTypeFromFragmentPtr(tt.location, tt.typeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if (got == nil) != tt.wantNil {
				t.Errorf("got = %v, wantNil %v", got, tt.wantNil)
			}
		})
	}
}

func TestRAML_PutAnnotationTypeIntoFragment(t *testing.T) {
	r := makeTestRAML(t)
	shape := &BaseShape{}
	r.PutAnnotationTypeIntoFragment("MyAnnot", "loc.raml", shape)
	got, err := r.GetAnnotationTypeFromFragmentPtr("loc.raml", "MyAnnot")
	if err != nil {
		t.Fatalf("GetAnnotationTypeFromFragmentPtr() error = %v", err)
	}
	if got != shape {
		t.Errorf("got = %v, want %v", got, shape)
	}
}

// --- Fragment cache -----------------------------------------------------------

func TestRAML_GetFragment(t *testing.T) {
	lib := &Library{}
	r := &RAML{fragmentsCache: map[string]Fragment{"file:///loc": lib}}
	if got := r.GetFragment("/loc"); got != lib {
		t.Errorf("GetFragment(\"/loc\") = %v, want %v", got, lib)
	}
	if got := r.GetFragment("missing"); got != nil {
		t.Errorf("GetFragment(\"missing\") = %v, want nil", got)
	}
}

func TestRAML_PutFragment(t *testing.T) {
	r := makeTestRAML(t)
	lib := &Library{}
	r.PutFragment("loc", lib)
	if got := r.GetFragment("loc"); got != lib {
		t.Errorf("after PutFragment, GetFragment() = %v, want %v", got, lib)
	}
}

// --- Cross-fragment reference resolution -------------------------------------

func TestRAML_GetReferencedType(t *testing.T) {
	shape := &BaseShape{}
	types := orderedmap.New[string, *BaseShape](0)
	types.Set("MyType", shape)
	r := &RAML{fragmentsCache: map[string]Fragment{
		"file:///loc": &Library{
			Types: types,
			Uses:  orderedmap.New[string, *LibraryLink](0),
		},
	}}
	tests := []struct {
		name     string
		refName  string
		location string
		wantNil  bool
		wantErr  bool
	}{
		{"found", "MyType", "/loc", false, false},
		{"type not found", "Missing", "/loc", true, true},
		{"fragment not found", "MyType", "missing", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.GetReferencedType(tt.refName, tt.location)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if (got == nil) != tt.wantNil {
				t.Errorf("got = %v, wantNil %v", got, tt.wantNil)
			}
		})
	}
}

func TestRAML_GetReferencedAnnotationType(t *testing.T) {
	shape := &BaseShape{}
	annotTypes := orderedmap.New[string, *BaseShape](0)
	annotTypes.Set("MyAnnot", shape)
	r := &RAML{fragmentsCache: map[string]Fragment{
		"file:///loc": &Library{
			AnnotationTypes: annotTypes,
			Types:           orderedmap.New[string, *BaseShape](0),
			Uses:            orderedmap.New[string, *LibraryLink](0),
		},
	}}
	tests := []struct {
		name     string
		refName  string
		location string
		wantNil  bool
		wantErr  bool
	}{
		{"found", "MyAnnot", "/loc", false, false},
		{"annotation not found", "Missing", "/loc", true, true},
		{"fragment not found", "MyAnnot", "missing", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.GetReferencedAnnotationType(tt.refName, tt.location)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if (got == nil) != tt.wantNil {
				t.Errorf("got = %v, wantNil %v", got, tt.wantNil)
			}
		})
	}
}
