package raml

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplySecuritySchemes_NullOverridesInherited verifies the RAML 1.0
// specification requirement that an operation-level `securedBy: [null]`
// overrides — and is not appended to — any inherited security schemes from the
// endpoint level or the global API-level securedBy.
func TestApplySecuritySchemes_NullOverridesInherited(t *testing.T) {
	const doc = `#%RAML 1.0
title: Test API
securedBy: [oauth2]
securitySchemes:
  oauth2:
    type: OAuth 2.0
    settings:
      authorizationUri: https://example.com/oauth/authorize
      accessTokenUri: https://example.com/oauth/token
      authorizationGrants: [authorization_code]
/public:
  get:
    securedBy: [null]
    responses:
      200:
        description: OK
/protected:
  get:
    responses:
      200:
        description: OK
`
	r := makeTestRAML(t)
	require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

	// /public GET has explicit securedBy: [null] — must not be extended with
	// the global oauth2 scheme.
	pubEP, ok := r.endPoints["/public"]
	require.True(t, ok, "/public endpoint must be present")
	pubGet, ok := pubEP.Operations.Get("get")
	require.True(t, ok, "/public GET operation must be present")

	require.Len(t, pubGet.SecuredBy, 1, "/public GET must have exactly one security scheme (null)")
	assert.Equal(t, "null", pubGet.SecuredBy[0].Name,
		"/public GET securedBy[0] must be the null scheme")

	// /protected GET has no explicit securedBy — must inherit the global oauth2.
	protEP, ok := r.endPoints["/protected"]
	require.True(t, ok, "/protected endpoint must be present")
	protGet, ok := protEP.Operations.Get("get")
	require.True(t, ok, "/protected GET operation must be present")

	require.Len(t, protGet.SecuredBy, 1, "/protected GET must have exactly one security scheme (oauth2)")
	assert.Equal(t, "oauth2", protGet.SecuredBy[0].Name,
		"/protected GET securedBy[0] must be the inherited oauth2 scheme")
}

// TestApplySecuritySchemes_EndpointExplicitDoesNotAffectOperationExplicit
// verifies that when both an endpoint and one of its operations carry explicit
// securedBy declarations, the operation's own declaration wins unchanged.
func TestApplySecuritySchemes_EndpointExplicitDoesNotAffectOperationExplicit(t *testing.T) {
	const doc = `#%RAML 1.0
title: Test API
securedBy: [oauth2]
securitySchemes:
  oauth2:
    type: OAuth 2.0
    settings:
      authorizationUri: https://example.com/oauth/authorize
      accessTokenUri: https://example.com/oauth/token
      authorizationGrants: [authorization_code]
  session:
    type: Pass Through
/resources:
  securedBy: [session]
  get:
    securedBy: [null]
    responses:
      200:
        description: OK
  post:
    responses:
      200:
        description: OK
`
	r := makeTestRAML(t)
	require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

	ep, ok := r.endPoints["/resources"]
	require.True(t, ok, "/resources endpoint must be present")

	// GET has explicit securedBy: [null] — must not be overwritten by endpoint's [session].
	getOp, ok := ep.Operations.Get("get")
	require.True(t, ok, "/resources GET must be present")
	require.Len(t, getOp.SecuredBy, 1, "/resources GET must have exactly one scheme")
	assert.Equal(t, "null", getOp.SecuredBy[0].Name,
		"/resources GET must keep its own explicit null, not endpoint's session")

	// POST has no explicit securedBy — must take endpoint's explicit [session].
	postOp, ok := ep.Operations.Get("post")
	require.True(t, ok, "/resources POST must be present")
	require.Len(t, postOp.SecuredBy, 1, "/resources POST must have exactly one scheme")
	assert.Equal(t, "session", postOp.SecuredBy[0].Name,
		"/resources POST must inherit the endpoint's explicit session scheme")
}

// TestUnwrapParents_SingleInheritance verifies that single-parent inheritance
// correctly propagates constraints and does not mutate the parent's shapes.
func TestUnwrapParents_SingleInheritance(t *testing.T) {
	t.Run("object: child gets all parent properties", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: object
    properties:
      a: string
      b: integer
  B:
    type: A
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		b, ok := frag.Types.Get("B")
		require.True(t, ok)

		obj, ok := b.Shape.(*ObjectShape)
		require.True(t, ok)
		_, hasA := obj.Properties.Get("a")
		_, hasB := obj.Properties.Get("b")
		assert.True(t, hasA, "B must inherit property 'a'")
		assert.True(t, hasB, "B must inherit property 'b'")
	})

	t.Run("object: parent properties map is not aliased into child (adding to child does not affect parent)", func(t *testing.T) {
		// B has no own properties, so inheritProperties will alias B.Properties = A.Properties.
		// A second child C that also extends A and adds property 'c' should not
		// cause A's properties map to gain 'c'.
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: object
    properties:
      a: string
  B:
    type: A
  C:
    type: A
    properties:
      c: boolean
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		a, ok := frag.Types.Get("A")
		require.True(t, ok)
		aObj, ok := a.Shape.(*ObjectShape)
		require.True(t, ok)

		_, aHasC := aObj.Properties.Get("c")
		assert.False(t, aHasC, "A must not gain 'c' from child C")
	})

	t.Run("object: parent is not mutated when child adds own property", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: object
    properties:
      a: string
  B:
    type: A
    properties:
      b: integer
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		a, ok := frag.Types.Get("A")
		require.True(t, ok)
		aObj, ok := a.Shape.(*ObjectShape)
		require.True(t, ok)

		_, aHasB := aObj.Properties.Get("b")
		assert.False(t, aHasB, "A must not gain child B's property 'b'")
	})

	t.Run("array: child inherits parent constraints", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: array
    minItems: 2
    maxItems: 10
  B:
    type: A
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		b, ok := frag.Types.Get("B")
		require.True(t, ok)

		arr, ok := b.Shape.(*ArrayShape)
		require.True(t, ok)
		require.NotNil(t, arr.MinItems)
		require.NotNil(t, arr.MaxItems)
		assert.Equal(t, uint64(2), arr.MinItems.Value)
		assert.Equal(t, uint64(10), arr.MaxItems.Value)
	})

	t.Run("array: object items from parent are not mutated when child adds own items properties", func(t *testing.T) {
		// A has items: object { x: string }.
		// B extends A and narrows items to also require y: integer.
		// A's items object must not gain 'y'.
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: array
    items:
      type: object
      properties:
        x: string
  B:
    type: A
    items:
      type: object
      properties:
        y: integer
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		// B's items must have both x (inherited) and y (own).
		b, ok := frag.Types.Get("B")
		require.True(t, ok)
		bArr, ok := b.Shape.(*ArrayShape)
		require.True(t, ok)
		require.NotNil(t, bArr.Items)
		bItems, ok := bArr.Items.Shape.(*ObjectShape)
		require.True(t, ok)
		_, hasX := bItems.Properties.Get("x")
		_, hasY := bItems.Properties.Get("y")
		assert.True(t, hasX, "B items must inherit 'x' from A items")
		assert.True(t, hasY, "B items must keep own 'y'")

		// A's items must still only have x.
		a, ok := frag.Types.Get("A")
		require.True(t, ok)
		aArr, ok := a.Shape.(*ArrayShape)
		require.True(t, ok)
		require.NotNil(t, aArr.Items)
		aItems, ok := aArr.Items.Shape.(*ObjectShape)
		require.True(t, ok)
		_, aHasY := aItems.Properties.Get("y")
		assert.False(t, aHasY, "A items must not gain 'y' from child B")
	})

	t.Run("number: child inherits and can narrow constraints", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  Base:
    type: number
    minimum: 0
    maximum: 100
  Child:
    type: Base
    maximum: 50
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		child, ok := frag.Types.Get("Child")
		require.True(t, ok)

		num, ok := child.Shape.(*NumberShape)
		require.True(t, ok)
		require.NotNil(t, num.Minimum)
		require.NotNil(t, num.Maximum)
		assert.Equal(t, "0", num.Minimum.Value.RatString(), "Child must inherit minimum from Base")
		assert.Equal(t, "50", num.Maximum.Value.RatString(), "Child's own maximum must take precedence")

		// Base must not be modified.
		base, ok := frag.Types.Get("Base")
		require.True(t, ok)
		baseNum, ok := base.Shape.(*NumberShape)
		require.True(t, ok)
		require.NotNil(t, baseNum.Maximum)
		assert.Equal(t, "100", baseNum.Maximum.Value.RatString(), "Base maximum must remain 100")
	})
}

// RAML 1.0 spec § "Multiple Inheritance".
func TestUnwrapParents_MultipleInheritance(t *testing.T) {
	t.Run("object: child gets all properties from all parents", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: object
    properties:
      a: string
  B:
    type: object
    properties:
      b: integer
  C:
    type: [A, B]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		c, ok := frag.Types.Get("C")
		require.True(t, ok, "type C must exist")

		obj, ok := c.Shape.(*ObjectShape)
		require.True(t, ok, "C must be an ObjectShape")

		_, hasA := obj.Properties.Get("a")
		_, hasB := obj.Properties.Get("b")
		assert.True(t, hasA, "C must have property 'a' from A")
		assert.True(t, hasB, "C must have property 'b' from B")
	})

	t.Run("object: parents are not mutated", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: object
    properties:
      a: string
  B:
    type: object
    properties:
      b: integer
  C:
    type: [A, B]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		a, ok := frag.Types.Get("A")
		require.True(t, ok, "type A must exist")
		b, ok := frag.Types.Get("B")
		require.True(t, ok, "type B must exist")

		aObj, ok := a.Shape.(*ObjectShape)
		require.True(t, ok)
		bObj, ok := b.Shape.(*ObjectShape)
		require.True(t, ok)

		_, aHasB := aObj.Properties.Get("b")
		_, bHasA := bObj.Properties.Get("a")
		assert.False(t, aHasB, "A must not gain property 'b' from sibling B")
		assert.False(t, bHasA, "B must not gain property 'a' from sibling A")
	})

	t.Run("object: three parents, child has all properties", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: object
    properties:
      a: string
  B:
    type: object
    properties:
      b: integer
  C:
    type: object
    properties:
      c: boolean
  D:
    type: [A, B, C]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		d, ok := frag.Types.Get("D")
		require.True(t, ok, "type D must exist")

		obj, ok := d.Shape.(*ObjectShape)
		require.True(t, ok)
		_, hasA := obj.Properties.Get("a")
		_, hasB := obj.Properties.Get("b")
		_, hasC := obj.Properties.Get("c")
		assert.True(t, hasA, "D must have 'a'")
		assert.True(t, hasB, "D must have 'b'")
		assert.True(t, hasC, "D must have 'c'")
	})

	t.Run("number: constraints are merged", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  N1:
    type: number
    minimum: 4
  N2:
    type: number
    maximum: 10
  N3:
    type: [N1, N2]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		n3, ok := frag.Types.Get("N3")
		require.True(t, ok, "type N3 must exist")

		num, ok := n3.Shape.(*NumberShape)
		require.True(t, ok, "N3 must be a NumberShape")
		require.NotNil(t, num.Minimum, "N3 must inherit minimum from N1")
		require.NotNil(t, num.Maximum, "N3 must inherit maximum from N2")
		assert.Equal(t, "4", num.Minimum.Value.RatString())
		assert.Equal(t, "10", num.Maximum.Value.RatString())
	})

	t.Run("number: incompatible constraints are rejected at validation", func(t *testing.T) {
		// minimum: 4 with maximum: 2 is invalid (max < min); constraint conflict
		// is caught by the validation pass, not the unwrap pass.
		const doc = `#%RAML 1.0
title: Test
types:
  N1:
    type: number
    minimum: 4
  N2:
    type: number
    maximum: 2
  N3:
    type: [N1, N2]
`
		r := makeTestRAML(t)
		err := r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap(), OptWithValidate())
		require.Error(t, err, "combining minimum: 4 with maximum: 2 must be rejected by validation")
	})

	t.Run("incompatible types are rejected", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  Bad:
    type: [string, integer]
`
		r := makeTestRAML(t)
		err := r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap())
		require.Error(t, err, "inheriting from different primitive types must be rejected")
	})

	t.Run("object: child own property takes precedence over parent property", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A:
    type: object
    properties:
      shared:
        type: string
        minLength: 1
  B:
    type: object
    properties:
      b: integer
  C:
    type: [A, B]
    properties:
      shared:
        type: string
        minLength: 5
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		c, ok := frag.Types.Get("C")
		require.True(t, ok, "type C must exist")

		obj, ok := c.Shape.(*ObjectShape)
		require.True(t, ok)
		prop, hasProp := obj.Properties.Get("shared")
		require.True(t, hasProp)

		ss, ok := prop.Base.Shape.(*StringShape)
		require.True(t, ok)
		require.NotNil(t, ss.MinLength)
		assert.Equal(t, uint64(5), ss.MinLength.Value, "C's own minLength:5 must win over A's minLength:1")
	})

	t.Run("array: constraints merged from two parents", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A1:
    type: array
    minItems: 2
  A2:
    type: array
    maxItems: 8
  A3:
    type: [A1, A2]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		a3, ok := frag.Types.Get("A3")
		require.True(t, ok)

		arr, ok := a3.Shape.(*ArrayShape)
		require.True(t, ok, "A3 must be an ArrayShape")
		require.NotNil(t, arr.MinItems, "A3 must inherit minItems from A1")
		require.NotNil(t, arr.MaxItems, "A3 must inherit maxItems from A2")
		assert.Equal(t, uint64(2), arr.MinItems.Value)
		assert.Equal(t, uint64(8), arr.MaxItems.Value)
	})

	t.Run("array: conflicting minItems fails at unwrap", func(t *testing.T) {
		// A1 has minItems: 2; A2 has minItems: 5.
		// After inheriting A1 (minItems: 2), the synthetic shape has minItems: 2.
		// Inheriting A2 (minItems: 5) violates the constraint (target < source).
		const doc = `#%RAML 1.0
title: Test
types:
  A1:
    type: array
    minItems: 2
  A2:
    type: array
    minItems: 5
  A3:
    type: [A1, A2]
`
		r := makeTestRAML(t)
		err := r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap())
		require.Error(t, err, "minItems: 2 followed by minItems: 5 must fail at unwrap time")
	})

	t.Run("array: items type is inherited", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  A1:
    type: array
    items: string
  A2:
    type: array
    minItems: 1
  A3:
    type: [A1, A2]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		a3, ok := frag.Types.Get("A3")
		require.True(t, ok)

		arr, ok := a3.Shape.(*ArrayShape)
		require.True(t, ok)
		require.NotNil(t, arr.Items, "A3 must inherit items from A1")
		assert.Equal(t, TypeString, arr.Items.Type)
	})

	t.Run("array: object items from multiple parents are merged without mutating parent items", func(t *testing.T) {
		// A1 has items: object { x: string }
		// A2 has items: object { y: integer }
		// A3 inherits [A1, A2]: its items must have both x and y.
		// A1's items must still only have x (not gain y).
		const doc = `#%RAML 1.0
title: Test
types:
  A1:
    type: array
    items:
      type: object
      properties:
        x: string
  A2:
    type: array
    items:
      type: object
      properties:
        y: integer
  A3:
    type: [A1, A2]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		// Child: A3's items must have both properties.
		a3, ok := frag.Types.Get("A3")
		require.True(t, ok)
		a3arr, ok := a3.Shape.(*ArrayShape)
		require.True(t, ok, "A3 must be an ArrayShape")
		require.NotNil(t, a3arr.Items)
		a3items, ok := a3arr.Items.Shape.(*ObjectShape)
		require.True(t, ok, "A3 items must be an ObjectShape")
		_, hasX := a3items.Properties.Get("x")
		_, hasY := a3items.Properties.Get("y")
		assert.True(t, hasX, "A3 items must have 'x' from A1 items")
		assert.True(t, hasY, "A3 items must have 'y' from A2 items")

		// Parents must not be mutated.
		a1, ok := frag.Types.Get("A1")
		require.True(t, ok)
		a1arr, ok := a1.Shape.(*ArrayShape)
		require.True(t, ok)
		require.NotNil(t, a1arr.Items)
		a1items, ok := a1arr.Items.Shape.(*ObjectShape)
		require.True(t, ok)
		_, a1HasY := a1items.Properties.Get("y")
		assert.False(t, a1HasY, "A1 items must not gain 'y' from sibling A2")

		a2, ok := frag.Types.Get("A2")
		require.True(t, ok)
		a2arr, ok := a2.Shape.(*ArrayShape)
		require.True(t, ok)
		require.NotNil(t, a2arr.Items)
		a2items, ok := a2arr.Items.Shape.(*ObjectShape)
		require.True(t, ok)
		_, a2HasX := a2items.Properties.Get("x")
		assert.False(t, a2HasX, "A2 items must not gain 'x' from sibling A1")
	})

	t.Run("union: multiple parents narrow members to common types", func(t *testing.T) {
		// U1 = string | integer | boolean
		// U2 = string | integer
		// [U1, U2] should narrow to string | integer (the intersection).
		const doc = `#%RAML 1.0
title: Test
types:
  U1:
    type: string | integer | boolean
  U2:
    type: string | integer
  U3:
    type: [U1, U2]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		u3, ok := frag.Types.Get("U3")
		require.True(t, ok)

		uni, ok := u3.Shape.(*UnionShape)
		require.True(t, ok, "U3 must be a UnionShape")
		assert.Len(t, uni.AnyOf, 2, "U3 must have 2 members (string and integer)")

		types := make(map[string]bool, len(uni.AnyOf))
		for _, m := range uni.AnyOf {
			types[m.Type] = true
		}
		assert.True(t, types[TypeString], "U3 must contain string")
		assert.True(t, types[TypeInteger], "U3 must contain integer")
		assert.False(t, types[TypeBoolean], "U3 must not contain boolean (narrowed out)")
	})

	t.Run("union: parents are not mutated", func(t *testing.T) {
		const doc = `#%RAML 1.0
title: Test
types:
  U1:
    type: string | integer | boolean
  U2:
    type: string | integer
  U3:
    type: [U1, U2]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		u1, ok := frag.Types.Get("U1")
		require.True(t, ok)

		uni, ok := u1.Shape.(*UnionShape)
		require.True(t, ok)
		assert.Len(t, uni.AnyOf, 3, "U1 must retain all 3 original members after child is unwrapped")
	})

	t.Run("union: no common types fails at unwrap", func(t *testing.T) {
		// U1 = string | integer; U2 = boolean | number — no overlap.
		const doc = `#%RAML 1.0
title: Test
types:
  U1:
    type: string | integer
  U2:
    type: boolean | number
  U3:
    type: [U1, U2]
`
		r := makeTestRAML(t)
		err := r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap())
		require.Error(t, err, "unions with no common member types must fail at unwrap time")
	})

	t.Run("array: chained (array-of-array) items not mutated across parents", func(t *testing.T) { // A1 has items: array { items: string }
		// A2 has items: array { maxItems: 5 }
		// A3 = [A1, A2]: items must be array { items: string, maxItems: 5 }
		// A1's items and A1's items.items must not be mutated.
		const doc = `#%RAML 1.0
title: Test
types:
  A1:
    type: array
    items:
      type: array
      items: string
  A2:
    type: array
    items:
      type: array
      maxItems: 5
  A3:
    type: [A1, A2]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		// A3: outer array -> inner array -> string items, maxItems:5
		a3, ok := frag.Types.Get("A3")
		require.True(t, ok)
		a3outer, ok := a3.Shape.(*ArrayShape)
		require.True(t, ok, "A3 must be ArrayShape")
		require.NotNil(t, a3outer.Items, "A3 must have items")
		a3inner, ok := a3outer.Items.Shape.(*ArrayShape)
		require.True(t, ok, "A3 items must be ArrayShape")
		require.NotNil(t, a3inner.Items, "A3 inner items must have items (string)")
		assert.Equal(t, TypeString, a3inner.Items.Type)
		require.NotNil(t, a3inner.MaxItems, "A3 inner items must inherit maxItems from A2")
		assert.Equal(t, uint64(5), a3inner.MaxItems.Value)

		// A1's inner array must not have gained maxItems.
		a1, ok := frag.Types.Get("A1")
		require.True(t, ok)
		a1outer, ok := a1.Shape.(*ArrayShape)
		require.True(t, ok)
		require.NotNil(t, a1outer.Items)
		a1inner, ok := a1outer.Items.Shape.(*ArrayShape)
		require.True(t, ok)
		assert.Nil(t, a1inner.MaxItems, "A1 inner array must not gain maxItems from A2")

		// A2's inner array must not have gained items.
		a2, ok := frag.Types.Get("A2")
		require.True(t, ok)
		a2outer, ok := a2.Shape.(*ArrayShape)
		require.True(t, ok)
		require.NotNil(t, a2outer.Items)
		a2inner, ok := a2outer.Items.Shape.(*ArrayShape)
		require.True(t, ok)
		assert.Nil(t, a2inner.Items, "A2 inner array must not gain items from A1")
	})

	t.Run("array: self-referential items does not cause infinite loop in makeMultipleInheritanceShape", func(t *testing.T) {
		// TreeList.items = TreeList (self-referential). After UnwrapShape, Items
		// points back to the same BaseShape. makeMultipleInheritanceShape must
		// not enter infinite recursion when scanning parents for Items.
		const doc = `#%RAML 1.0
title: Test
types:
  TreeList:
    type: array
    items: TreeList
  OtherList:
    type: array
    minItems: 1
  Combined:
    type: [TreeList, OtherList]
`
		r := makeTestRAML(t)
		require.NoError(t, r.ParseFromString(doc, "api.raml", t.TempDir(), OptWithUnwrap()))

		frag, ok := r.EntryPoint().(*APIFragment)
		require.True(t, ok)

		combined, ok := frag.Types.Get("Combined")
		require.True(t, ok)
		arr, ok := combined.Shape.(*ArrayShape)
		require.True(t, ok, "Combined must be an ArrayShape")
		require.NotNil(t, arr.MinItems, "Combined must inherit minItems from OtherList")
		assert.Equal(t, uint64(1), arr.MinItems.Value)
	})
}
