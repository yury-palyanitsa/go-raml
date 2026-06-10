package raml

import (
	"testing"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

func TestFragmentKind_String(t *testing.T) {
	tests := []struct {
		kind FragmentKind
		want string
	}{
		{FragmentUnknown, "Unknown"},
		{FragmentLibrary, "Library"},
		{FragmentDataType, "DataType"},
		{FragmentNamedExample, "NamedExample"},
		{FragmentAPI, "API"},
		{FragmentDocumentationItem, "DocumentationItem"},
		{FragmentResourceType, "ResourceType"},
		{FragmentTrait, "Trait"},
		{FragmentAnnotationTypeDeclaration, "AnnotationTypeDeclaration"},
		{FragmentOverlay, "Overlay"},
		{FragmentExtension, "Extension"},
		{FragmentSecurityScheme, "SecurityScheme"},
		{FragmentKind(99), "FragmentKind(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCutReferenceName(t *testing.T) {
	tests := []struct {
		name    string
		refName string
		before  string
		after   string
		found   bool
	}{
		{"dotted reference", "fragment.identifier", "fragment", "identifier", true},
		{"no dot", "identifier", "identifier", "", false},
		{"multiple dots uses last", "a.b.c", "a.b", "c", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, a, f := CutReferenceName(tt.refName)
			if b != tt.before || a != tt.after || f != tt.found {
				t.Errorf("CutReferenceName(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.refName, b, a, f, tt.before, tt.after, tt.found)
			}
		})
	}
}

// ─── Library ─────────────────────────────────────────────────────────────────

func TestLibrary_GetLocation(t *testing.T) {
	l := &Library{Location: "my/lib.raml"}
	if got := l.GetLocation(); got != "my/lib.raml" {
		t.Errorf("GetLocation() = %q, want %q", got, "my/lib.raml")
	}
}

func TestLibrary_GetReferenceType(t *testing.T) {
	shape := &BaseShape{ID: 1}
	localTypes := orderedmap.New[string, *BaseShape](0)
	localTypes.Set("MyType", shape)

	linkedTypes := orderedmap.New[string, *BaseShape](0)
	linkedTypes.Set("RemoteType", &BaseShape{ID: 2})

	uses := orderedmap.New[string, *LibraryLink](0)
	uses.Set("lib", &LibraryLink{
		Link: &Library{
			Types:           linkedTypes,
			AnnotationTypes: orderedmap.New[string, *BaseShape](0),
		},
	})

	tests := []struct {
		name    string
		lib     *Library
		refName string
		wantNil bool
		wantErr bool
	}{
		{
			name:    "local reference found",
			lib:     &Library{Types: localTypes, Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "MyType",
		},
		{
			name:    "library reference found",
			lib:     &Library{Types: orderedmap.New[string, *BaseShape](0), Uses: uses},
			refName: "lib.RemoteType",
		},
		{
			name:    "local reference not found",
			lib:     &Library{Types: orderedmap.New[string, *BaseShape](0), Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "Missing",
			wantErr: true,
		},
		{
			name:    "library not in uses",
			lib:     &Library{Types: orderedmap.New[string, *BaseShape](0), Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "missing.Type",
			wantErr: true,
		},
		{
			name: "type not in library",
			lib: &Library{
				Types: orderedmap.New[string, *BaseShape](0),
				Uses: func() *orderedmap.OrderedMap[string, *LibraryLink] {
					u := orderedmap.New[string, *LibraryLink](0)
					u.Set("lib", &LibraryLink{
						Link: &Library{
							Types:           orderedmap.New[string, *BaseShape](0),
							AnnotationTypes: orderedmap.New[string, *BaseShape](0),
						},
					})
					return u
				}(),
			},
			refName: "lib.NoSuchType",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.lib.GetReferenceType(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetReferenceType() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got == nil {
				t.Errorf("GetReferenceType() = nil, want non-nil")
			}
		})
	}
}

func TestLibrary_GetReferenceAnnotationType(t *testing.T) {
	annotType := &BaseShape{ID: 10}
	annotTypes := orderedmap.New[string, *BaseShape](0)
	annotTypes.Set("MyAnnot", annotType)

	libAnnotTypes := orderedmap.New[string, *BaseShape](0)
	libAnnotTypes.Set("RemoteAnnot", &BaseShape{ID: 11})
	uses := orderedmap.New[string, *LibraryLink](0)
	uses.Set("lib", &LibraryLink{
		Link: &Library{
			AnnotationTypes: libAnnotTypes,
			Types:           orderedmap.New[string, *BaseShape](0),
		},
	})

	tests := []struct {
		name    string
		lib     *Library
		refName string
		wantErr bool
	}{
		{
			name: "local annotation type found",
			lib: &Library{
				AnnotationTypes: annotTypes,
				Types:           orderedmap.New[string, *BaseShape](0),
				Uses:            orderedmap.New[string, *LibraryLink](0),
			},
			refName: "MyAnnot",
		},
		{
			name: "library annotation type found",
			lib: &Library{
				AnnotationTypes: orderedmap.New[string, *BaseShape](0),
				Types:           orderedmap.New[string, *BaseShape](0),
				Uses:            uses,
			},
			refName: "lib.RemoteAnnot",
		},
		{
			name: "falls back to Types for local ref",
			lib: &Library{
				AnnotationTypes: orderedmap.New[string, *BaseShape](0),
				Types:           annotTypes,
				Uses:            orderedmap.New[string, *LibraryLink](0),
			},
			refName: "MyAnnot",
		},
		{
			name: "not found anywhere",
			lib: &Library{
				AnnotationTypes: orderedmap.New[string, *BaseShape](0),
				Types:           orderedmap.New[string, *BaseShape](0),
				Uses:            orderedmap.New[string, *LibraryLink](0),
			},
			refName: "Missing",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.lib.GetReferenceAnnotationType(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetReferenceAnnotationType() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got == nil {
				t.Errorf("GetReferenceAnnotationType() = nil, want non-nil")
			}
		})
	}
}

func TestLibrary_GetTraitDefinition(t *testing.T) {
	traitDef := &TraitDefinition{}
	traits := orderedmap.New[string, *TraitDefinition](0)
	traits.Set("MyTrait", traitDef)

	tests := []struct {
		name    string
		lib     *Library
		refName string
		wantErr bool
	}{
		{
			name:    "local trait found",
			lib:     &Library{Traits: traits, Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "MyTrait",
		},
		{
			name:    "trait not found",
			lib:     &Library{Traits: orderedmap.New[string, *TraitDefinition](0), Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "Missing",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.lib.GetTraitDefinition(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTraitDefinition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLibrary_GetResourceTypeDefinition(t *testing.T) {
	rtDef := &ResourceTypeDefinition{}
	rts := orderedmap.New[string, *ResourceTypeDefinition](0)
	rts.Set("MyRT", rtDef)

	tests := []struct {
		name    string
		lib     *Library
		refName string
		wantErr bool
	}{
		{
			name:    "local resource type found",
			lib:     &Library{ResourceTypes: rts, Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "MyRT",
		},
		{
			name:    "resource type not found",
			lib:     &Library{ResourceTypes: orderedmap.New[string, *ResourceTypeDefinition](0), Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "Missing",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.lib.GetResourceTypeDefinition(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetResourceTypeDefinition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLibrary_GetSecuritySchemeDefinition(t *testing.T) {
	ssDef := &SecuritySchemeDefinition{}
	ss := orderedmap.New[string, *SecuritySchemeDefinition](0)
	ss.Set("MyScheme", ssDef)

	tests := []struct {
		name    string
		lib     *Library
		refName string
		wantErr bool
	}{
		{
			name:    "local security scheme found",
			lib:     &Library{SecuritySchemes: ss, Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "MyScheme",
		},
		{
			name:    "security scheme not found",
			lib:     &Library{SecuritySchemes: orderedmap.New[string, *SecuritySchemeDefinition](0), Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "Missing",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.lib.GetSecuritySchemeDefinition(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetSecuritySchemeDefinition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRAML_MakeLibrary(t *testing.T) {
	r := makeTestRAML(t)
	l := r.MakeLibrary("/path/to/lib.raml")
	if l.Location != "/path/to/lib.raml" {
		t.Errorf("Location = %q, want %q", l.Location, "/path/to/lib.raml")
	}
	if l.Uses == nil {
		t.Error("Uses is nil")
	}
	if l.Types == nil {
		t.Error("Types is nil")
	}
	if l.AnnotationTypes == nil {
		t.Error("AnnotationTypes is nil")
	}
	if l.CustomDomainProperties == nil {
		t.Error("CustomDomainProperties is nil")
	}
}

func TestLibrary_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		node    *yaml.Node
		want    func(t *testing.T, l *Library)
		wantErr bool
	}{
		{
			name:    "not a mapping node",
			node:    &yaml.Node{Kind: yaml.ScalarNode, Value: "value"},
			wantErr: true,
		},
		{
			name: "unknown field",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "unknown_field"},
					{Kind: yaml.ScalarNode, Value: "val"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid uses: not a map",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "uses"},
					{Kind: yaml.SequenceNode},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid types: not a map",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "types"},
					{Kind: yaml.SequenceNode},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid annotationTypes: not a map",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "annotationTypes"},
					{Kind: yaml.SequenceNode},
				},
			},
			wantErr: true,
		},
		{
			name: "types and schemas mutually exclusive",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "types"},
					{Kind: yaml.MappingNode, Tag: TagNull},
					{Kind: yaml.ScalarNode, Value: "schemas"},
					{Kind: yaml.MappingNode, Tag: TagNull},
				},
			},
			wantErr: true,
		},
		{
			name: "schemas and types mutually exclusive",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "schemas"},
					{Kind: yaml.MappingNode, Tag: TagNull},
					{Kind: yaml.ScalarNode, Value: "types"},
					{Kind: yaml.MappingNode, Tag: TagNull},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid usage: mapping instead of scalar",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "usage"},
					{Kind: yaml.MappingNode, Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Value: "nested"},
						{Kind: yaml.ScalarNode, Value: "value"},
					}},
				},
			},
			wantErr: true,
		},
		{
			name: "all null fields parsed without error",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "uses"},
					{Kind: yaml.MappingNode, Tag: TagNull},
					{Kind: yaml.ScalarNode, Value: "types"},
					{Kind: yaml.MappingNode, Tag: TagNull},
					{Kind: yaml.ScalarNode, Value: "annotationTypes"},
					{Kind: yaml.MappingNode, Tag: TagNull},
					{Kind: yaml.ScalarNode, Value: "usage"},
					{Kind: yaml.ScalarNode, Value: "some usage", Tag: TagStr},
				},
			},
			want: func(t *testing.T, l *Library) {
				if l.Usage == nil || l.Usage.Value != "some usage" {
					t.Errorf("Usage = %v, want \"some usage\"", l.Usage)
				}
			},
		},
		{
			name: "schemas alias for types",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "schemas"},
					{Kind: yaml.MappingNode, Tag: TagNull},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			l := r.MakeLibrary("test.raml")
			err := l.UnmarshalYAML(tt.node)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil {
				tt.want(t, l)
			}
		})
	}
}

// ─── RAML.unmarshalUses ───────────────────────────────────────────────────────

func TestRAML_unmarshalUses(t *testing.T) {
	tests := []struct {
		name    string
		node    *yaml.Node
		want    func(t *testing.T, uses *orderedmap.OrderedMap[string, *LibraryLink])
		wantErr bool
	}{
		{
			name: "null tag returns empty map",
			node: &yaml.Node{Kind: yaml.MappingNode, Tag: TagNull},
			want: func(t *testing.T, uses *orderedmap.OrderedMap[string, *LibraryLink]) {
				if uses.Len() != 0 {
					t.Errorf("len = %d, want 0", uses.Len())
				}
			},
		},
		{
			name:    "not a mapping node",
			node:    &yaml.Node{Kind: yaml.SequenceNode},
			wantErr: true,
		},
		{
			name: "valid mapping",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "myLib"},
					{Kind: yaml.ScalarNode, Value: "path/to/lib.raml"},
				},
			},
			want: func(t *testing.T, uses *orderedmap.OrderedMap[string, *LibraryLink]) {
				link, ok := uses.Get("myLib")
				if !ok {
					t.Fatal("key \"myLib\" not found")
				}
				if link.Value != "path/to/lib.raml" {
					t.Errorf("link.Value = %q, want %q", link.Value, "path/to/lib.raml")
				}
			},
		},
		{
			name: "duplicate library name",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "myLib"},
					{Kind: yaml.ScalarNode, Value: "path/to/lib.raml"},
					{Kind: yaml.ScalarNode, Value: "myLib"},
					{Kind: yaml.ScalarNode, Value: "path/to/other.raml"},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			uses, err := r.unmarshalUses(tt.node, "test.raml")
			if (err != nil) != tt.wantErr {
				t.Fatalf("unmarshalUses() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil {
				tt.want(t, uses)
			}
		})
	}
}

// ─── RAML.unmarshalTypes ─────────────────────────────────────────────────────

func TestRAML_unmarshalTypes(t *testing.T) {
	tests := []struct {
		name             string
		node             *yaml.Node
		isAnnotationType bool
		want             func(t *testing.T, types *orderedmap.OrderedMap[string, *BaseShape])
		wantErr          bool
	}{
		{
			name: "null tag returns empty map",
			node: &yaml.Node{Kind: yaml.MappingNode, Tag: TagNull},
			want: func(t *testing.T, types *orderedmap.OrderedMap[string, *BaseShape]) {
				if types.Len() != 0 {
					t.Errorf("len = %d, want 0", types.Len())
				}
			},
		},
		{
			name:    "not a mapping node",
			node:    &yaml.Node{Kind: yaml.SequenceNode},
			wantErr: true,
		},
		{
			name: "valid type declaration",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "MyString", Tag: TagStr},
					{Kind: yaml.ScalarNode, Value: "string", Tag: TagStr},
				},
			},
			want: func(t *testing.T, types *orderedmap.OrderedMap[string, *BaseShape]) {
				shape, ok := types.Get("MyString")
				if !ok {
					t.Fatal("\"MyString\" not found")
				}
				if shape.Type != TypeString {
					t.Errorf("shape.Type = %q, want %q", shape.Type, TypeString)
				}
			},
		},
		{
			name: "duplicate type name",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "MyType", Tag: TagStr},
					{Kind: yaml.ScalarNode, Value: "string", Tag: TagStr},
					{Kind: yaml.ScalarNode, Value: "MyType", Tag: TagStr},
					{Kind: yaml.ScalarNode, Value: "integer", Tag: TagStr},
				},
			},
			wantErr: true,
		},
		{
			name: "cannot redefine built-in type",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "string", Tag: TagStr},
					{Kind: yaml.ScalarNode, Value: "string", Tag: TagStr},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			types, err := r.unmarshalTypes(tt.node, "test.raml", tt.isAnnotationType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unmarshalTypes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil {
				tt.want(t, types)
			}
		})
	}
}

// ─── DataTypeFragment ─────────────────────────────────────────────────────────

func TestDataTypeFragment_GetLocation(t *testing.T) {
	dt := &DataTypeFragment{Location: "data.raml"}
	if got := dt.GetLocation(); got != "data.raml" {
		t.Errorf("GetLocation() = %q, want %q", got, "data.raml")
	}
}

func TestDataTypeFragment_GetReferenceType(t *testing.T) {
	linkedTypes := orderedmap.New[string, *BaseShape](0)
	linkedTypes.Set("MyType", &BaseShape{ID: 1})
	uses := orderedmap.New[string, *LibraryLink](0)
	uses.Set("lib", &LibraryLink{
		Link: &Library{
			Types:           linkedTypes,
			AnnotationTypes: orderedmap.New[string, *BaseShape](0),
		},
	})

	tests := []struct {
		name    string
		dt      *DataTypeFragment
		refName string
		wantErr bool
	}{
		{
			name:    "dotted reference found",
			dt:      &DataTypeFragment{Uses: uses},
			refName: "lib.MyType",
		},
		{
			name:    "local (non-dotted) reference rejected",
			dt:      &DataTypeFragment{Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "MyType",
			wantErr: true,
		},
		{
			name:    "library not in uses",
			dt:      &DataTypeFragment{Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "missing.Type",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.dt.GetReferenceType(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReferenceType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDataTypeFragment_GetReferenceAnnotationType(t *testing.T) {
	linkedAnnot := orderedmap.New[string, *BaseShape](0)
	linkedAnnot.Set("MyAnnot", &BaseShape{ID: 10})
	uses := orderedmap.New[string, *LibraryLink](0)
	uses.Set("lib", &LibraryLink{
		Link: &Library{
			AnnotationTypes: linkedAnnot,
			Types:           orderedmap.New[string, *BaseShape](0),
		},
	})

	tests := []struct {
		name    string
		dt      *DataTypeFragment
		refName string
		wantErr bool
	}{
		{
			name:    "library annotation type found",
			dt:      &DataTypeFragment{Uses: uses},
			refName: "lib.MyAnnot",
		},
		{
			name:    "library not found",
			dt:      &DataTypeFragment{Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "missing.Annot",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.dt.GetReferenceAnnotationType(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReferenceAnnotationType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRAML_MakeDataTypeFragment(t *testing.T) {
	r := makeTestRAML(t)
	dt := r.MakeDataTypeFragment("/path/dt.raml")
	if dt.Location != "/path/dt.raml" {
		t.Errorf("Location = %q, want %q", dt.Location, "/path/dt.raml")
	}
	if dt.Uses == nil {
		t.Error("Uses is nil")
	}
}

func TestDataTypeFragment_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		node    *yaml.Node
		want    func(t *testing.T, dt *DataTypeFragment)
		wantErr bool
	}{
		{
			name:    "not a mapping node",
			node:    &yaml.Node{Kind: yaml.ScalarNode, Value: "value"},
			wantErr: true,
		},
		{
			name: "valid string type",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "type", Tag: TagStr},
					{Kind: yaml.ScalarNode, Value: "string", Tag: TagStr},
				},
			},
			want: func(t *testing.T, dt *DataTypeFragment) {
				if dt.Shape == nil {
					t.Error("Shape is nil")
				}
				if dt.Shape.Type != TypeString {
					t.Errorf("Shape.Type = %q, want %q", dt.Shape.Type, TypeString)
				}
			},
		},
		{
			name: "valid uses with null map",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "uses", Tag: TagStr},
					{Kind: yaml.MappingNode, Tag: TagNull},
					{Kind: yaml.ScalarNode, Value: "type", Tag: TagStr},
					{Kind: yaml.ScalarNode, Value: "string", Tag: TagStr},
				},
			},
			want: func(t *testing.T, dt *DataTypeFragment) {
				if dt.Shape == nil {
					t.Error("Shape is nil")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			dt := r.MakeDataTypeFragment("dt.raml")
			err := dt.UnmarshalYAML(tt.node)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil {
				tt.want(t, dt)
			}
		})
	}
}

func TestRAML_MakeJSONDataType(t *testing.T) {
	tests := []struct {
		name    string
		value   []byte
		path    string
		want    func(t *testing.T, dt *DataTypeFragment)
		wantErr bool
	}{
		{
			name:  "valid JSON schema",
			value: []byte(`{"type": "string"}`),
			path:  "schema.json",
			want: func(t *testing.T, dt *DataTypeFragment) {
				if dt.Shape == nil {
					t.Error("Shape is nil")
				}
				if dt.Location != "schema.json" {
					t.Errorf("Location = %q, want %q", dt.Location, "schema.json")
				}
			},
		},
		{
			name:    "invalid JSON decode error",
			value:   []byte(`{`),
			path:    "bad.json",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			dt, err := r.MakeJSONDataType(tt.value, tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MakeJSONDataType() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil {
				tt.want(t, dt)
			}
		})
	}
}

// ─── NamedExample ─────────────────────────────────────────────────────────────

func TestNamedExample_GetLocation(t *testing.T) {
	ne := &NamedExample{Location: "named.raml"}
	if got := ne.GetLocation(); got != "named.raml" {
		t.Errorf("GetLocation() = %q, want %q", got, "named.raml")
	}
}

func TestNamedExample_GetReferenceType(t *testing.T) {
	linkedTypes := orderedmap.New[string, *BaseShape](0)
	linkedTypes.Set("MyType", &BaseShape{ID: 1})
	uses := orderedmap.New[string, *LibraryLink](0)
	uses.Set("lib", &LibraryLink{
		Link: &Library{
			Types:           linkedTypes,
			AnnotationTypes: orderedmap.New[string, *BaseShape](0),
		},
	})

	tests := []struct {
		name    string
		ne      *NamedExample
		refName string
		wantErr bool
	}{
		{
			name:    "library reference found",
			ne:      &NamedExample{Uses: uses},
			refName: "lib.MyType",
		},
		{
			name:    "local reference rejected",
			ne:      &NamedExample{Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "MyType",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.ne.GetReferenceType(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReferenceType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNamedExample_GetReferenceAnnotationType(t *testing.T) {
	linkedAnnot := orderedmap.New[string, *BaseShape](0)
	linkedAnnot.Set("MyAnnot", &BaseShape{ID: 10})
	uses := orderedmap.New[string, *LibraryLink](0)
	uses.Set("lib", &LibraryLink{
		Link: &Library{
			AnnotationTypes: linkedAnnot,
			Types:           orderedmap.New[string, *BaseShape](0),
		},
	})

	tests := []struct {
		name    string
		ne      *NamedExample
		refName string
		wantErr bool
	}{
		{
			name:    "library annotation type found",
			ne:      &NamedExample{Uses: uses},
			refName: "lib.MyAnnot",
		},
		{
			name:    "not found",
			ne:      &NamedExample{Uses: orderedmap.New[string, *LibraryLink](0)},
			refName: "missing.Annot",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.ne.GetReferenceAnnotationType(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReferenceAnnotationType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRAML_MakeNamedExample(t *testing.T) {
	r := makeTestRAML(t)
	ne := r.MakeNamedExample("/path/example.raml")
	if ne.Location != "/path/example.raml" {
		t.Errorf("Location = %q, want %q", ne.Location, "/path/example.raml")
	}
	if ne.Map != nil {
		t.Error("Map should be nil before UnmarshalYAML")
	}
}

func TestNamedExample_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		node    *yaml.Node
		want    func(t *testing.T, ne *NamedExample)
		wantErr bool
	}{
		{
			name:    "not a mapping node",
			node:    &yaml.Node{Kind: yaml.ScalarNode, Value: "value"},
			wantErr: true,
		},
		{
			name: "valid examples map",
			node: &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "exampleA"},
					{Kind: yaml.ScalarNode, Value: "hello", Tag: TagStr},
				},
			},
			want: func(t *testing.T, ne *NamedExample) {
				if ne.Map == nil {
					t.Fatal("Map is nil")
				}
				ex, ok := ne.Map.Get("exampleA")
				if !ok {
					t.Fatal("\"exampleA\" not found in Map")
				}
				if ex.Data.Value.Raw != "hello" {
					t.Errorf("example value = %v, want %q", ex.Data.Value.Raw, "hello")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			ne := r.MakeNamedExample("example.raml")
			err := ne.UnmarshalYAML(tt.node)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil {
				tt.want(t, ne)
			}
		})
	}
}
