package raml

import (
	"testing"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// ──────────────────────────────────────────────────────────────────────────────
// unwrapShape
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_unwrapShape(t *testing.T) {
	r := makeTestRAML(t)

	t.Run("already unwrapped returns shape as-is without caching", func(t *testing.T) {
		_, base := makeString(t, r)
		base.unwrapped = true
		cache := map[int64]*BaseShape{}

		got, se := r.unwrapShape(base, cache)
		if se != nil {
			t.Fatalf("unexpected error: %v", se)
		}
		if got != base {
			t.Fatal("expected same pointer for already-unwrapped shape")
		}
		if len(cache) != 0 {
			t.Error("cache must not be populated for an already-unwrapped shape")
		}
	})

	t.Run("not yet unwrapped simple shape succeeds and caches result", func(t *testing.T) {
		_, base := makeString(t, r)
		// base.unwrapped is false by default
		cache := map[int64]*BaseShape{}

		got, se := r.unwrapShape(base, cache)
		if se != nil {
			t.Fatalf("unexpected error: %v", se)
		}
		if got == nil {
			t.Fatal("expected non-nil unwrapped result")
		}
		// CloneDetached preserves original ID, so cache must be keyed by base.ID.
		if _, ok := cache[base.ID]; !ok {
			t.Error("unwrapped clone must be stored in cache under the original shape ID")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// validateTypes
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_validateTypes(t *testing.T) {
	t.Run("empty fragment definitions returns nil", func(t *testing.T) {
		r := makeTestRAML(t)
		if got := r.validateTypes(map[int64]*BaseShape{}); got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
	})

	t.Run("valid string shape passes all checks", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		r.PutTypeDefinitionIntoFragment("test.raml", base)

		if got := r.validateTypes(map[int64]*BaseShape{}); got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
	})

	t.Run("shape with Check error accumulates error", func(t *testing.T) {
		r := makeTestRAML(t)
		s, base := makeString(t, r)
		// minLength > maxLength triggers Check error
		s.MinLength = scalarFacetOf(uint64(10))
		s.MaxLength = scalarFacetOf(uint64(5))
		base.unwrapped = true // skip unwrap phase, focus on Check
		r.PutTypeDefinitionIntoFragment("test.raml", base)

		if got := r.validateTypes(map[int64]*BaseShape{}); got == nil {
			t.Fatal("expected error for shape with minLength > maxLength")
		}
	})

	t.Run("shape with invalid example accumulates error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.unwrapped = true
		base.Example = makeExample(dataNodeOf(42))
		r.PutTypeDefinitionIntoFragment("test.raml", base)

		if got := r.validateTypes(map[int64]*BaseShape{}); got == nil {
			t.Fatal("expected error: integer value is not a valid string example")
		}
	})

	t.Run("multiple invalid shapes accumulate independent errors", func(t *testing.T) {
		r := makeTestRAML(t)
		s1, base1 := makeString(t, r)
		s1.MinLength = scalarFacetOf(uint64(10))
		s1.MaxLength = scalarFacetOf(uint64(1))
		base1.unwrapped = true

		s2, base2 := makeString(t, r)
		s2.MinLength = scalarFacetOf(uint64(10))
		s2.MaxLength = scalarFacetOf(uint64(1))
		base2.unwrapped = true

		r.PutTypeDefinitionIntoFragment("test.raml", base1)
		r.PutTypeDefinitionIntoFragment("test.raml", base2)

		got := r.validateTypes(map[int64]*BaseShape{})
		if got == nil {
			t.Fatal("expected accumulated errors for two invalid shapes")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// validateDomainExtensions
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_validateDomainExtensions(t *testing.T) {
	t.Run("no domain extensions returns nil", func(t *testing.T) {
		r := makeTestRAML(t)
		if got := r.validateDomainExtensions(map[int64]*BaseShape{}); got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
	})

	t.Run("nil DefinedBy is skipped", func(t *testing.T) {
		r := makeTestRAML(t)
		r.domainExtensions = []*DomainExtension{
			{Extension: enumNode("x"), DefinedBy: nil},
		}
		if got := r.validateDomainExtensions(map[int64]*BaseShape{}); got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
	})

	t.Run("already-unwrapped DefinedBy with valid value passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, strBase := makeString(t, r)
		strBase.unwrapped = true
		r.domainExtensions = []*DomainExtension{
			{DefinedBy: strBase, Extension: enumNode("hello")},
		}
		if got := r.validateDomainExtensions(map[int64]*BaseShape{}); got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
	})

	t.Run("not-unwrapped DefinedBy found in cache passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, definedBy := makeString(t, r)
		definedBy.unwrapped = false

		_, cached := makeString(t, r)
		cached.unwrapped = true
		cache := map[int64]*BaseShape{definedBy.ID: cached}

		r.domainExtensions = []*DomainExtension{
			{DefinedBy: definedBy, Extension: enumNode("world")},
		}
		if got := r.validateDomainExtensions(cache); got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
	})

	t.Run("not-unwrapped DefinedBy not in cache returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, definedBy := makeString(t, r)
		definedBy.unwrapped = false
		r.domainExtensions = []*DomainExtension{
			{DefinedBy: definedBy, Extension: enumNode("x")},
		}
		if got := r.validateDomainExtensions(map[int64]*BaseShape{}); got == nil {
			t.Fatal("expected error: unwrapped shape not found in cache")
		}
	})

	t.Run("DefinedBy type mismatch with extension value returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, strBase := makeString(t, r)
		strBase.unwrapped = true
		// StringShape expects a string, but we pass an integer.
		r.domainExtensions = []*DomainExtension{
			{DefinedBy: strBase, Extension: dataNodeOf(42)},
		}
		if got := r.validateDomainExtensions(map[int64]*BaseShape{}); got == nil {
			t.Fatal("expected error: integer value does not satisfy string constraint")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// ValidateShapes (public)
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_ValidateShapes(t *testing.T) {
	t.Run("empty RAML returns nil", func(t *testing.T) {
		r := makeTestRAML(t)
		if err := r.ValidateShapes(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid string shape in fragment definitions passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		r.PutTypeDefinitionIntoFragment("test.raml", base)
		if err := r.ValidateShapes(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid shape returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		s, base := makeString(t, r)
		s.MinLength = scalarFacetOf(uint64(100))
		s.MaxLength = scalarFacetOf(uint64(1))
		base.unwrapped = true
		r.PutTypeDefinitionIntoFragment("test.raml", base)
		if err := r.ValidateShapes(); err == nil {
			t.Fatal("expected error for shape with minLength > maxLength")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// validateObjectShape
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_validateObjectShape(t *testing.T) {
	t.Run("nil properties and pattern properties returns nil", func(t *testing.T) {
		r := makeTestRAML(t)
		if err := r.validateObjectShape(&ObjectShape{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid properties pass validateShapeCommons", func(t *testing.T) {
		r := makeTestRAML(t)
		_, propBase := makeString(t, r)

		props := orderedmap.New[string, Property]()
		props.Set("name", Property{Name: "name", Base: propBase})
		obj := &ObjectShape{ObjectFacets: ObjectFacets{Properties: props}}

		if err := r.validateObjectShape(obj); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid pattern properties pass validateShapeCommons", func(t *testing.T) {
		r := makeTestRAML(t)
		_, propBase := makeString(t, r)

		patProps := orderedmap.New[string, PatternProperty]()
		patProps.Set("[a-z]+", PatternProperty{Base: propBase})
		obj := &ObjectShape{ObjectFacets: ObjectFacets{PatternProperties: patProps}}

		if err := r.validateObjectShape(obj); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("property with invalid example returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, propBase := makeString(t, r)
		// Integer value fails StringShape validation.
		propBase.Example = makeExample(dataNodeOf(99))

		props := orderedmap.New[string, Property]()
		props.Set("field", Property{Name: "field", Base: propBase})
		obj := &ObjectShape{ObjectFacets: ObjectFacets{Properties: props}}

		if err := r.validateObjectShape(obj); err == nil {
			t.Fatal("expected error for property with invalid example")
		}
	})

	t.Run("pattern property with invalid example returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, propBase := makeString(t, r)
		propBase.Example = makeExample(dataNodeOf(true))

		patProps := orderedmap.New[string, PatternProperty]()
		patProps.Set("[a-z]+", PatternProperty{Base: propBase})
		obj := &ObjectShape{ObjectFacets: ObjectFacets{PatternProperties: patProps}}

		if err := r.validateObjectShape(obj); err == nil {
			t.Fatal("expected error for pattern property with invalid example")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// validateShapeCommons
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_validateShapeCommons(t *testing.T) {
	t.Run("AnyShape with no constraints passes", func(t *testing.T) {
		r := makeTestRAML(t)
		anyBase := NewLinkedBase(&AnyShape{}, makeTestBase(t, r, "a"))
		if err := r.validateShapeCommons(anyBase); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ObjectShape with no properties passes", func(t *testing.T) {
		r := makeTestRAML(t)
		base := makeTestBase(t, r, "obj")
		base.SetShape(&ObjectShape{BaseShape: base})
		if err := r.validateShapeCommons(base); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ArrayShape with string items passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, itemBase := makeString(t, r)
		arrBase := makeTestBase(t, r, "arr")
		arr := &ArrayShape{BaseShape: arrBase, ArrayFacets: ArrayFacets{Items: itemBase}}
		arrBase.SetShape(arr)
		if err := r.validateShapeCommons(arrBase); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("UnionShape with string member passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, m1 := makeString(t, r)
		unionBase := makeUnionBase(t, r, m1)
		if err := r.validateShapeCommons(unionBase); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("shape with invalid Example returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.Example = makeExample(dataNodeOf(777))
		if err := r.validateShapeCommons(base); err == nil {
			t.Fatal("expected error: integer is not a valid string example")
		}
	})

	t.Run("shape with invalid item in Examples map returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.Examples = &Examples{Map: orderedmap.New[string, *Example]()}
		base.Examples.Map.Set("bad", makeExample(dataNodeOf(false)))
		if err := r.validateShapeCommons(base); err == nil {
			t.Fatal("expected error: bool is not a valid string example")
		}
	})

	t.Run("shape with unknown custom facet returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		// No parent defines "x-custom", so it is an unknown facet.
		base.CustomShapeFacets.Set("x-custom", enumNode("value"))
		if err := r.validateShapeCommons(base); err == nil {
			t.Fatal("expected error: unknown custom facet")
		}
	})

	t.Run("ArrayShape with invalid item example propagates error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, itemBase := makeString(t, r)
		itemBase.Example = makeExample(dataNodeOf(3.14))
		arrBase := makeTestBase(t, r, "arr2")
		arr := &ArrayShape{BaseShape: arrBase, ArrayFacets: ArrayFacets{Items: itemBase}}
		arrBase.SetShape(arr)
		if err := r.validateShapeCommons(arrBase); err == nil {
			t.Fatal("expected error from invalid array item example")
		}
	})

	t.Run("UnionShape with invalid member example propagates error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, m1 := makeString(t, r)
		m1.Example = makeExample(dataNodeOf(0))
		unionBase := makeUnionBase(t, r, m1)
		if err := r.validateShapeCommons(unionBase); err == nil {
			t.Fatal("expected error from invalid union member example")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// validateExamples
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_validateExamples(t *testing.T) {
	t.Run("no example or default returns nil", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		if err := r.validateExamples(base); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid Example passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.Example = makeExample(enumNode("hello"))
		if err := r.validateExamples(base); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid Example returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.Example = makeExample(dataNodeOf(42))
		if err := r.validateExamples(base); err == nil {
			t.Fatal("expected error: integer is not a valid string example")
		}
	})

	t.Run("valid Examples map entry passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.Examples = &Examples{Map: orderedmap.New[string, *Example]()}
		base.Examples.Map.Set("ex1", makeExample(enumNode("world")))
		if err := r.validateExamples(base); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid Examples map entry returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.Examples = &Examples{Map: orderedmap.New[string, *Example]()}
		base.Examples.Map.Set("ex1", makeExample(dataNodeOf(3.14)))
		if err := r.validateExamples(base); err == nil {
			t.Fatal("expected error: float is not a valid string example")
		}
	})

	t.Run("valid Default passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.Default = enumNode("default-value")
		if err := r.validateExamples(base); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid Default returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		base.Default = dataNodeOf(false)
		if err := r.validateExamples(base); err == nil {
			t.Fatal("expected error: bool is not a valid string default")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// validateShapeFacets
// ──────────────────────────────────────────────────────────────────────────────

func TestRAML_validateShapeFacets(t *testing.T) {
	t.Run("no inherits and no custom facets returns nil", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		if err := r.validateShapeFacets(base); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown custom facet on child returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, base := makeString(t, r)
		// No parent defines "x-meta", so it is unknown.
		base.CustomShapeFacets.Set("x-meta", enumNode("data"))
		if err := r.validateShapeFacets(base); err == nil {
			t.Fatal("expected error: unknown custom facet")
		}
	})

	t.Run("inherited required facet present with valid value passes", func(t *testing.T) {
		r := makeTestRAML(t)
		// Build parent that defines a required "color" facet backed by a string shape.
		_, facetBase := makeString(t, r)
		parent := makeTestBase(t, r, "parent")
		parent.CustomShapeFacetDefinitions.Set("color", Property{
			Name:     "color",
			Required: true,
			Base:     facetBase,
		})

		// Build child that inherits from parent and supplies the facet value.
		_, child := makeString(t, r)
		child.Inherits = []*BaseShape{parent}
		child.CustomShapeFacets.Set("color", enumNode("red"))

		if err := r.validateShapeFacets(child); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("inherited required facet missing from child returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, facetBase := makeString(t, r)
		parent := makeTestBase(t, r, "parent")
		parent.CustomShapeFacetDefinitions.Set("size", Property{
			Name:     "size",
			Required: true,
			Base:     facetBase,
		})

		_, child := makeString(t, r)
		child.Inherits = []*BaseShape{parent}
		// "size" not added to child.CustomShapeFacets.

		if err := r.validateShapeFacets(child); err == nil {
			t.Fatal("expected error: required facet 'size' is missing")
		}
	})

	t.Run("duplicate facet in child own definitions and parent returns error", func(t *testing.T) {
		r := makeTestRAML(t)
		_, facetBase := makeString(t, r)
		parent := makeTestBase(t, r, "parent")
		parent.CustomShapeFacetDefinitions.Set("tag", Property{
			Name: "tag",
			Base: facetBase,
		})

		_, child := makeString(t, r)
		child.Inherits = []*BaseShape{parent}
		// Child also declares its own "tag" definition → duplicate.
		child.CustomShapeFacetDefinitions.Set("tag", Property{Name: "tag", Base: facetBase})

		if err := r.validateShapeFacets(child); err == nil {
			t.Fatal("expected error: duplicate custom facet definition")
		}
	})

	t.Run("optional inherited facet absent from child passes", func(t *testing.T) {
		r := makeTestRAML(t)
		_, facetBase := makeString(t, r)
		parent := makeTestBase(t, r, "parent")
		parent.CustomShapeFacetDefinitions.Set("label", Property{
			Name:     "label",
			Required: false, // optional
			Base:     facetBase,
		})

		_, child := makeString(t, r)
		child.Inherits = []*BaseShape{parent}
		// Not providing the optional facet is valid.

		if err := r.validateShapeFacets(child); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
