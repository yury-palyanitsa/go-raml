package raml

import (
	"testing"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.SetShape
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_SetShape(t *testing.T) {
	b := &BaseShape{}
	s := &StringShape{BaseShape: b}
	b.SetShape(s)
	if b.Shape != s {
		t.Fatal("SetShape: Shape not set")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.IsUnwrapped / SetUnwrapped
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_Unwrapped(t *testing.T) {
	b := &BaseShape{}
	if b.IsUnwrapped() {
		t.Fatal("expected false before SetUnwrapped")
	}
	b.SetUnwrapped()
	if !b.IsUnwrapped() {
		t.Fatal("expected true after SetUnwrapped")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.String
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_String(t *testing.T) {
	tests := []struct {
		name string
		base *BaseShape
		want string
	}{
		{
			name: "no inherits",
			base: &BaseShape{Name: "Foo", Type: "string"},
			want: "Type: string: Name: Foo",
		},
		{
			name: "with one inherit",
			base: &BaseShape{
				Name:     "Child",
				Type:     "object",
				Inherits: []*BaseShape{{Name: "Parent"}},
			},
			want: "Type: object: Name: Child: Inherits: Parent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.base.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.Check
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_Check(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("valid string shape", func(t *testing.T) {
		_, base := makeString(t, r)
		if err := base.Check(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("minLength > maxLength returns error", func(t *testing.T) {
		s, base := makeString(t, r)
		s.MinLength = scalarFacetOf(uint64(10))
		s.MaxLength = scalarFacetOf(uint64(5))
		if err := base.Check(); err == nil {
			t.Fatal("expected error")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.Validate
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_Validate(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("string validates string value", func(t *testing.T) {
		_, base := makeString(t, r)
		if err := base.Validate("hello"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("string rejects integer", func(t *testing.T) {
		_, base := makeString(t, r)
		if err := base.Validate(42); err == nil {
			t.Fatal("expected error for integer value")
		}
	})

	t.Run("enum allows listed value", func(t *testing.T) {
		_, base := makeString(t, r)
		base.Enum = Nodes{enumNode("a"), enumNode("b")}
		if err := base.Validate("a"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("enum rejects unlisted value", func(t *testing.T) {
		_, base := makeString(t, r)
		base.Enum = Nodes{enumNode("a")}
		if err := base.Validate("z"); err == nil {
			t.Fatal("expected error: value not in enum")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.Inherit
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_Inherit(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("visited source returns source immediately", func(t *testing.T) {
		_, target := makeString(t, r)
		_, source := makeString(t, r)
		source.ShapeVisited = true

		got, err := target.Inherit(source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != source {
			t.Fatal("expected visited source to be returned")
		}
	})

	t.Run("source AnyShape returns target unchanged", func(t *testing.T) {
		_, target := makeString(t, r)
		anyBase := NewLinkedBase(&AnyShape{}, makeTestBase(t, r, "any"))

		got, err := target.Inherit(anyBase)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != target {
			t.Fatal("expected target returned unchanged for any-source")
		}
	})

	t.Run("homogenous string inherits facets", func(t *testing.T) {
		s, target := makeString(t, r)
		s.MinLength = nil

		ss, source := makeString(t, r)
		ss.MinLength = scalarFacetOf(uint64(3))

		got, err := target.Inherit(source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		inherited := got.Shape.(*StringShape)
		if inherited.MinLength == nil || inherited.MinLength.Value != 3 {
			t.Fatalf("expected MinLength 3, got %v", inherited.MinLength)
		}
	})

	t.Run("description inherited from source when target has none", func(t *testing.T) {
		_, target := makeString(t, r)
		_, source := makeString(t, r)
		source.Description = scalarFacetOf("sourced")

		got, err := target.Inherit(source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Description == nil || got.Description.Value != "sourced" {
			t.Fatalf("expected description 'sourced', got %v", got.Description)
		}
	})

	t.Run("target description not overwritten by source", func(t *testing.T) {
		_, target := makeString(t, r)
		target.Description = scalarFacetOf("mine")
		_, source := makeString(t, r)
		source.Description = scalarFacetOf("theirs")

		got, err := target.Inherit(source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Description.Value != "mine" {
			t.Fatalf("expected description 'mine', got %q", got.Description.Value)
		}
	})

	t.Run("custom facets from source merged into target", func(t *testing.T) {
		_, target := makeString(t, r)
		_, source := makeString(t, r)
		source.CustomShapeFacets.Set("x-ext", &DataNode{})

		if _, err := target.Inherit(source); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := target.CustomShapeFacets.Get("x-ext"); !ok {
			t.Fatal("expected x-ext to be inherited into target")
		}
	})

	t.Run("enum inherited from source when target has none", func(t *testing.T) {
		_, target := makeString(t, r)
		target.Enum = nil
		_, source := makeString(t, r)
		source.Enum = Nodes{enumNode("a"), enumNode("b")}

		got, err := target.Inherit(source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Enum == nil {
			t.Fatal("expected Enum to be inherited from source")
		}
		if len(got.Enum) != 2 {
			t.Fatalf("expected 2 enum values, got %d", len(got.Enum))
		}
	})

	t.Run("incompatible enum returns error", func(t *testing.T) {
		_, target := makeString(t, r)
		target.Enum = Nodes{enumNode("a")}
		_, source := makeString(t, r)
		source.Enum = Nodes{enumNode("z")}

		if _, err := target.Inherit(source); err == nil {
			t.Fatal("expected enum constraint violation error")
		}
	})

	t.Run("ShapeVisited reset to false after successful inherit", func(t *testing.T) {
		_, target := makeString(t, r)
		_, source := makeString(t, r)

		if _, err := target.Inherit(source); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if source.ShapeVisited {
			t.Fatal("ShapeVisited should be reset to false after successful inherit")
		}
	})

	t.Run("source union with matching member", func(t *testing.T) {
		_, target := makeString(t, r)
		target.Type = TypeString
		_, member := makeString(t, r)
		member.Type = TypeString
		sourceBase := makeUnionBase(t, r, member)

		got, err := target.Inherit(sourceBase)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("got is nil")
		}
	})

	t.Run("target union inherits source into members", func(t *testing.T) {
		_, m1 := makeString(t, r)
		m1.Type = TypeString
		targetBase := makeUnionBase(t, r, m1)
		_, source := makeString(t, r)
		source.Type = TypeString

		got, err := targetBase.Inherit(source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("got is nil")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.inheritUnionSource
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_inheritUnionSource(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("any member short-circuits to target", func(t *testing.T) {
		_, target := makeString(t, r)
		target.Type = TypeString
		anyBase := makeTestBase(t, r, "any")
		anyBase.SetShape(&AnyShape{BaseShape: anyBase})
		u := &UnionShape{UnionFacets: UnionFacets{AnyOf: []*BaseShape{anyBase}}}

		got, err := target.inheritUnionSource(u)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != target {
			t.Fatal("expected target returned for any-membership")
		}
	})

	t.Run("single matching member simplifies to that member", func(t *testing.T) {
		_, target := makeString(t, r)
		target.Type = TypeString
		_, member := makeString(t, r)
		member.Type = TypeString
		u := &UnionShape{UnionFacets: UnionFacets{AnyOf: []*BaseShape{member}}}

		got, err := target.inheritUnionSource(u)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := got.Shape.(*StringShape); !ok {
			t.Fatalf("expected StringShape, got %T", got.Shape)
		}
	})

	t.Run("multiple matching members produce union", func(t *testing.T) {
		_, target := makeString(t, r)
		target.Type = TypeString
		_, m1 := makeString(t, r)
		m1.Type = TypeString
		_, m2 := makeString(t, r)
		m2.Type = TypeString
		u := &UnionShape{UnionFacets: UnionFacets{AnyOf: []*BaseShape{m1, m2}}}

		got, err := target.inheritUnionSource(u)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := got.Shape.(*UnionShape); !ok {
			t.Fatalf("expected UnionShape, got %T", got.Shape)
		}
	})

	t.Run("no matching member returns error", func(t *testing.T) {
		_, target := makeString(t, r)
		target.Type = TypeString
		intBase := NewLinkedBase(&IntegerShape{}, makeTestBase(t, r, "i"))
		intBase.Type = TypeInteger
		u := &UnionShape{UnionFacets: UnionFacets{AnyOf: []*BaseShape{intBase}}}

		if _, err := target.inheritUnionSource(u); err == nil {
			t.Fatal("expected error: no compatible union member")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.inheritUnionTarget
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_inheritUnionTarget(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("all members compatible", func(t *testing.T) {
		_, m1 := makeString(t, r)
		m1.Type = TypeString
		targetBase := makeUnionBase(t, r, m1)
		_, source := makeString(t, r)
		source.Type = TypeString

		// Inherit routes to inheritUnionTarget when target is union and source is not.
		got, err := targetBase.Inherit(source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("got is nil")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.AliasTo
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_AliasTo(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("copies all metadata fields from source", func(t *testing.T) {
		_, target := makeString(t, r)
		_, source := makeString(t, r)
		source.Description = scalarFacetOf("desc")
		source.DisplayName = scalarFacetOf("display")
		source.Example = &Example{}
		source.Examples = &Examples{}
		source.Inherits = []*BaseShape{makeTestBase(t, r, "parent")}
		source.Alias = makeTestBase(t, r, "alias")
		source.Default = &DataNode{}
		source.Required = scalarFacetOf(true)
		source.Enum = Nodes{enumNode("x")}
		source.CustomShapeFacets.Set("ext", &DataNode{})
		source.XML = &XMLSerialization{}

		got, err := target.AliasTo(source)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Description != source.Description {
			t.Error("Description not aliased")
		}
		if got.DisplayName != source.DisplayName {
			t.Error("DisplayName not aliased")
		}
		if got.Example != source.Example {
			t.Error("Example not aliased")
		}
		if got.Examples != source.Examples {
			t.Error("Examples not aliased")
		}
		if got.Inherits == nil {
			t.Error("Inherits not aliased")
		}
		if got.Alias == source {
			t.Error("Alias lost")
		}
		if got.Default != source.Default {
			t.Error("Default not aliased")
		}
		if got.Required != source.Required {
			t.Error("Required not aliased")
		}
		if got.Enum == nil {
			t.Error("Enum not aliased")
		}
		if got.CustomShapeFacets != source.CustomShapeFacets {
			t.Error("CustomShapeFacets not aliased")
		}
		if got.XML != source.XML {
			t.Error("XML not aliased")
		}
	})

	t.Run("mismatched types return error", func(t *testing.T) {
		_, target := makeString(t, r)
		intBase := NewLinkedBase(&IntegerShape{}, makeTestBase(t, r, "i"))

		if _, err := target.AliasTo(intBase); err == nil {
			t.Fatal("expected error for type mismatch")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.CloneShallow
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_CloneShallow(t *testing.T) {
	r := makeTestRAML(t)
	_, base := makeString(t, r)
	base.Name = "original"
	base.CustomShapeFacets.Set("key", enumNode("k"))

	clone := base.CloneShallow()

	if clone == base {
		t.Fatal("CloneShallow returned same pointer")
	}
	if clone.Name != "original" {
		t.Errorf("Name = %q, want %q", clone.Name, "original")
	}
	// Maps must be independent
	clone.CustomShapeFacets.Set("extra", &DataNode{})
	if base.CustomShapeFacets.Len() != 1 {
		t.Error("mutating clone's map affected original")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.Clone / CloneDetached
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_Clone(t *testing.T) {
	r := makeTestRAML(t)
	_, base := makeString(t, r)
	base.Name = "src"

	clonedMap := make(map[int64]*BaseShape)
	clone := base.Clone(clonedMap)

	if clone == base {
		t.Fatal("Clone returned same pointer")
	}
	if clone.Name != "src" {
		t.Errorf("Name = %q, want %q", clone.Name, "src")
	}
	if clone.Shape == base.Shape {
		t.Error("Shape pointer identical after Clone")
	}
}

func TestBaseShape_Clone_cacheHit(t *testing.T) {
	r := makeTestRAML(t)
	_, base := makeString(t, r)
	clonedMap := make(map[int64]*BaseShape)
	c1 := base.Clone(clonedMap)
	c2 := base.Clone(clonedMap)
	if c1 != c2 {
		t.Error("second Clone should return cached instance")
	}
}

func TestBaseShape_CloneDetached(t *testing.T) {
	r := makeTestRAML(t)
	_, parent := makeString(t, r)
	parent.Name = "parent"
	_, child := makeString(t, r)
	child.Name = "child"
	child.Inherits = []*BaseShape{parent}

	clone := child.CloneDetached()

	if clone == child {
		t.Fatal("CloneDetached returned same pointer")
	}
	if len(clone.Inherits) > 0 && clone.Inherits[0] == parent {
		t.Error("CloneDetached: Inherits[0] is original pointer, not a clone")
	}
}

func TestBaseShape_CloneDetached_withLink(t *testing.T) {
	r := makeTestRAML(t)
	_, base := makeString(t, r)
	_, linkShape := makeString(t, r)
	base.Link = &DataTypeFragment{Shape: linkShape}

	clone := base.CloneDetached()

	if clone.Link == base.Link {
		t.Error("CloneDetached: Link is same pointer, not cloned")
	}
	if clone.Link == nil {
		t.Fatal("CloneDetached: Link is nil after clone")
	}
}

func TestBaseShape_CloneDetached_withAlias(t *testing.T) {
	r := makeTestRAML(t)
	_, base := makeString(t, r)
	_, aliasShape := makeString(t, r)
	base.Alias = aliasShape

	clone := base.CloneDetached()

	if clone.Alias == aliasShape {
		t.Error("CloneDetached: Alias is same pointer, not cloned")
	}
	if clone.Alias == nil {
		t.Fatal("CloneDetached: Alias is nil after clone")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// BaseShape.Inherit – custom facet merge edge cases
// ──────────────────────────────────────────────────────────────────────────────

func TestBaseShape_Inherit_customFacetMerge(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("nil target map gets source map", func(t *testing.T) {
		_, target := makeString(t, r)
		target.CustomShapeFacets = nil
		_, source := makeString(t, r)
		source.CustomShapeFacets = orderedmap.New[string, *DataNode]()
		source.CustomShapeFacets.Set("ext", &DataNode{})

		if _, err := target.Inherit(source); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := target.CustomShapeFacets.Get("ext"); !ok {
			t.Error("expected ext key after inherit")
		}
	})

	t.Run("existing key not overwritten", func(t *testing.T) {
		existing := &DataNode{}
		_, target := makeString(t, r)
		target.CustomShapeFacets.Set("x", existing)
		_, source := makeString(t, r)
		source.CustomShapeFacets.Set("x", &DataNode{})

		if _, err := target.Inherit(source); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := target.CustomShapeFacets.Get("x")
		if got != existing {
			t.Error("existing custom facet was overwritten by inherit")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// identifyShapeType
// ──────────────────────────────────────────────────────────────────────────────

func TestIdentifyShapeType(t *testing.T) {
	node := func(v string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: v}
	}
	tests := []struct {
		name        string
		facets      []*yaml.Node
		defaultType string
		want        string
		wantErr     bool
	}{
		{"no facets uses default", nil, TypeString, TypeString, false},
		{"minLength → string", []*yaml.Node{node(FacetMinLength), {}}, TypeAny, TypeString, false},
		{"maxLength → string", []*yaml.Node{node(FacetMaxLength), {}}, TypeAny, TypeString, false},
		{"pattern → string (stringOnly)", []*yaml.Node{node(FacetPattern), {}}, TypeAny, TypeString, false},
		{"minimum → number", []*yaml.Node{node(FacetMinimum), {}}, TypeAny, TypeNumber, false},
		{"maximum → number", []*yaml.Node{node(FacetMaximum), {}}, TypeAny, TypeNumber, false},
		{"multipleOf → number", []*yaml.Node{node(FacetMultipleOf), {}}, TypeAny, TypeNumber, false},
		{"minItems → array", []*yaml.Node{node(FacetMinItems), {}}, TypeAny, TypeArray, false},
		{"maxItems → array", []*yaml.Node{node(FacetMaxItems), {}}, TypeAny, TypeArray, false},
		{"uniqueItems → array", []*yaml.Node{node(FacetUniqueItems), {}}, TypeAny, TypeArray, false},
		{"items → array", []*yaml.Node{node(FacetItems), {}}, TypeAny, TypeArray, false},
		{"minProperties → object", []*yaml.Node{node(FacetMinProperties), {}}, TypeAny, TypeObject, false},
		{"maxProperties → object", []*yaml.Node{node(FacetMaxProperties), {}}, TypeAny, TypeObject, false},
		{"properties → object", []*yaml.Node{node(FacetProperties), {}}, TypeAny, TypeObject, false},
		{"additionalProperties → object", []*yaml.Node{node(FacetAdditionalProperties), {}}, TypeAny, TypeObject, false},
		{"discriminator → object", []*yaml.Node{node(FacetDiscriminator), {}}, TypeAny, TypeObject, false},
		{"fileTypes → file", []*yaml.Node{node(FacetFileTypes), {}}, TypeAny, TypeFile, false},
		{
			"minLength then fileTypes (no pattern) → file",
			[]*yaml.Node{node(FacetMinLength), {}, node(FacetFileTypes), {}},
			TypeAny, TypeFile, false,
		},
		{
			"fileTypes then minLength (no pattern) → file",
			[]*yaml.Node{node(FacetFileTypes), {}, node(FacetMinLength), {}},
			TypeAny, TypeFile, false,
		},
		{
			"conflicting facets array+string → error",
			[]*yaml.Node{node(FacetMinItems), {}, node(FacetMinLength), {}},
			TypeString, "", true,
		},
		{
			"pattern then fileTypes → error (stringOnly blocks file)",
			[]*yaml.Node{node(FacetPattern), {}, node(FacetFileTypes), {}},
			TypeAny, "", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := identifyShapeType(tt.facets, tt.defaultType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RAML.MakeBaseShape
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_MakeBaseShape(t *testing.T) {
	r := makeTestRAML(t)
	b := makeTestBase(t, r, "myShape")

	if b.Name != "myShape" {
		t.Errorf("Name = %q, want %q", b.Name, "myShape")
	}
	if b.Location != "test.raml" {
		t.Errorf("Location = %q, want %q", b.Location, "test.raml")
	}
	if b.raml != r {
		t.Error("raml field not set")
	}
	if b.CustomShapeFacets == nil {
		t.Error("CustomShapeFacets is nil")
	}
	if b.CustomShapeFacetDefinitions == nil {
		t.Error("CustomShapeFacetDefinitions is nil")
	}
	if b.CustomDomainProperties == nil {
		t.Error("CustomDomainProperties is nil")
	}
	if b.ID == 0 {
		t.Error("ID was not generated")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RAML.generateSequenceID
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_generateSequenceID(t *testing.T) {
	r := makeTestRAML(t)
	a := r.generateSequenceID()
	b := r.generateSequenceID()
	if b != a+1 {
		t.Errorf("expected sequential IDs %d+1=%d, got %d", a, a+1, b)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RAML.MakeRecursiveShape
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_MakeRecursiveShape(t *testing.T) {
	r := makeTestRAML(t)
	_, head := makeString(t, r)
	head.Name = "Recurse"
	head.Description = &ScalarFacet[string]{Value: "recursive desc"}

	rec := r.MakeRecursiveShape(head)

	if rec == nil {
		t.Fatal("MakeRecursiveShape returned nil")
	}
	if rec.Type != TypeRecursive {
		t.Errorf("Type = %q, want %q", rec.Type, TypeRecursive)
	}
	if rec.Name != "Recurse" {
		t.Errorf("Name = %q, want %q", rec.Name, "Recurse")
	}
	if rec.Description != head.Description {
		t.Error("Description not copied from head")
	}
	rs, ok := rec.Shape.(*RecursiveShape)
	if !ok {
		t.Fatalf("Shape is %T, want *RecursiveShape", rec.Shape)
	}
	if rs.Head != head {
		t.Error("RecursiveShape.Head does not point to head")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RAML.MakeJSONShape
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_MakeJSONShape(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("valid empty schema", func(t *testing.T) {
		base := makeTestBase(t, r, "j")
		got, err := r.MakeJSONShape(base, `{}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Raw != `{}` {
			t.Errorf("Raw = %q, want %q", got.Raw, `{}`)
		}
		if got.Validator == nil {
			t.Error("Validator is nil")
		}
	})

	t.Run("typed string schema", func(t *testing.T) {
		base := makeTestBase(t, r, "j2")
		got, err := r.MakeJSONShape(base, `{"type":"string"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Raw != `{"type":"string"}` {
			t.Errorf("Raw = %q", got.Raw)
		}
	})

	t.Run("same location registered twice (ResourceExistsError suppressed)", func(t *testing.T) {
		// Both shapes share the same Location so the second AddResource call
		// triggers a ResourceExistsError, which must be silently ignored.
		base1 := makeTestBase(t, r, "j5a")
		base2 := makeTestBase(t, r, "j5b")
		if _, err := r.MakeJSONShape(base1, `{}`); err != nil {
			t.Fatalf("first call: unexpected error: %v", err)
		}
		if _, err := r.MakeJSONShape(base2, `{}`); err != nil {
			t.Fatalf("second call with same location: unexpected error: %v", err)
		}
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		base := makeTestBase(t, r, "j3")
		if _, err := r.MakeJSONShape(base, `{`); err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("invalid schema returns error", func(t *testing.T) {
		base := makeTestBase(t, r, "j4")
		if _, err := r.MakeJSONShape(base, `{"$schema":"bad"}`); err == nil {
			t.Fatal("expected error for invalid $schema")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// RAML.MakeConcreteShapeYAML
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_MakeConcreteShapeYAML(t *testing.T) {
	r := makeTestRAML(t)

	shapeTypes := []struct {
		typ     string
		check   func(Shape) bool
		typName string
	}{
		{TypeAny, func(s Shape) bool { _, ok := s.(*AnyShape); return ok }, "*AnyShape"},
		{TypeNil, func(s Shape) bool { _, ok := s.(*NilShape); return ok }, "*NilShape"},
		{TypeObject, func(s Shape) bool { _, ok := s.(*ObjectShape); return ok }, "*ObjectShape"},
		{TypeArray, func(s Shape) bool { _, ok := s.(*ArrayShape); return ok }, "*ArrayShape"},
		{TypeString, func(s Shape) bool { _, ok := s.(*StringShape); return ok }, "*StringShape"},
		{TypeInteger, func(s Shape) bool { _, ok := s.(*IntegerShape); return ok }, "*IntegerShape"},
		{TypeNumber, func(s Shape) bool { _, ok := s.(*NumberShape); return ok }, "*NumberShape"},
		{TypeDatetime, func(s Shape) bool { _, ok := s.(*DateTimeShape); return ok }, "*DateTimeShape"},
		{TypeDatetimeOnly, func(s Shape) bool { _, ok := s.(*DateTimeOnlyShape); return ok }, "*DateTimeOnlyShape"},
		{TypeDateOnly, func(s Shape) bool { _, ok := s.(*DateOnlyShape); return ok }, "*DateOnlyShape"},
		{TypeTimeOnly, func(s Shape) bool { _, ok := s.(*TimeOnlyShape); return ok }, "*TimeOnlyShape"},
		{TypeFile, func(s Shape) bool { _, ok := s.(*FileShape); return ok }, "*FileShape"},
		{TypeBoolean, func(s Shape) bool { _, ok := s.(*BooleanShape); return ok }, "*BooleanShape"},
		{TypeUnion, func(s Shape) bool { _, ok := s.(*UnionShape); return ok }, "*UnionShape"},
		{"unknown-type", func(s Shape) bool { _, ok := s.(*UnknownShape); return ok }, "*UnknownShape"},
	}

	for _, c := range shapeTypes {
		t.Run(c.typ, func(t *testing.T) {
			base := makeTestBase(t, r, c.typ)
			got, err := r.MakeConcreteShapeYAML(base, c.typ, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !c.check(got) {
				t.Errorf("want %s, got %T", c.typName, got)
			}
		})
	}

	t.Run("malformed facets return error", func(t *testing.T) {
		base := makeTestBase(t, r, "bad")
		// additionalProperties must be a boolean; a string value triggers a decode error.
		badFacets := []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: FacetAdditionalProperties},
			{Kind: yaml.ScalarNode, Value: "not-a-bool", Tag: "!!str"},
		}
		if _, err := r.MakeConcreteShapeYAML(base, TypeObject, badFacets); err == nil {
			t.Fatal("expected error for malformed facets")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// isCompatibleEnum
// ──────────────────────────────────────────────────────────────────────────────

func TestIsCompatibleEnum(t *testing.T) {
	mk := func(vals ...string) Nodes {
		n := make(Nodes, len(vals))
		for i, v := range vals {
			n[i] = enumNode(v)
		}
		return n
	}

	tests := []struct {
		name   string
		source Nodes
		target Nodes
		want   bool
	}{
		{"source superset of target", mk("a", "b", "c"), mk("a", "b"), true},
		{"same single value", mk("x"), mk("x"), true},
		{"disjoint sets", mk("a"), mk("b"), false},
		{"target has extra value", mk("a"), mk("a", "b"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCompatibleEnum(tt.source, tt.target); got != tt.want {
				t.Errorf("isCompatibleEnum = %v, want %v", got, tt.want)
			}
		})
	}
}
