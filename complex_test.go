package raml

import (
	"regexp"
	"testing"

	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

// propEntry is a key+Property pair used by newPropsMap.
type propEntry struct {
	key  string
	prop Property
}

// newPropsMap builds an ordered Property map from key/Property pairs.
func newPropsMap(entries ...propEntry) *orderedmap.OrderedMap[string, Property] {
	m := orderedmap.New[string, Property](len(entries))
	for _, e := range entries {
		m.Set(e.key, e.prop)
	}
	return m
}

// patternPropEntry is a key+PatternProperty pair used by newPatternPropsMap.
type patternPropEntry struct {
	key  string
	prop PatternProperty
}

// newPatternPropsMap builds an ordered PatternProperty map.
func newPatternPropsMap(entries ...patternPropEntry) *orderedmap.OrderedMap[string, PatternProperty] {
	m := orderedmap.New[string, PatternProperty](len(entries))
	for _, e := range entries {
		m.Set(e.key, e.prop)
	}
	return m
}

// stringProp is a convenience factory for a required string Property.
func stringProp(name string, id int64) Property {
	return Property{
		Name:     name,
		Base:     NewLinkedBase(&StringShape{}, &BaseShape{ID: id}),
		Required: true,
	}
}

// patternProp is a convenience factory for an optional pattern-based property.
func patternProp(rawPattern string, id int64) patternPropEntry {
	re := regexp.MustCompile(rawPattern)
	return patternPropEntry{
		key: "/" + rawPattern + "/",
		prop: PatternProperty{
			Pattern: re,
			Base:    NewLinkedBase(&StringShape{}, &BaseShape{ID: id}),
		},
	}
}

// TestArrayShape_clone verifies that clone produces an independent *ArrayShape.
func TestArrayShape_clone(t *testing.T) {
	itemBase := NewLinkedBase(&StringShape{}, &BaseShape{ID: 2})

	tests := []struct {
		name      string
		src       *ArrayShape
		cloneBase *BaseShape
		wantItems bool
	}{
		{
			name:      "no items",
			src:       NewTestShape(&ArrayShape{}, 1),
			cloneBase: &BaseShape{ID: 10},
		},
		{
			name: "already in clonedMap uses cached base",
			src: func() *ArrayShape {
				s := NewTestShape(&ArrayShape{}, 1)
				// The base is already in the map => clone reuses it
				return s
			}(),
			cloneBase: &BaseShape{
				ID: 1,
				Shape: &ArrayShape{
					BaseShape:   &BaseShape{ID: 1},
					ArrayFacets: ArrayFacets{},
				},
			},
		},
		{
			name: "with items clones items recursively",
			src: &ArrayShape{
				BaseShape:   &BaseShape{ID: 1},
				ArrayFacets: ArrayFacets{Items: itemBase},
			},
			cloneBase: &BaseShape{ID: 10},
			wantItems: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.src.clone(tt.cloneBase, make(map[int64]*BaseShape))
			arr, ok := got.(*ArrayShape)
			if !ok {
				t.Fatalf("clone() type = %T, want *ArrayShape", got)
			}
			if tt.wantItems && arr.Items == nil {
				t.Error("clone() Items is nil, want non-nil")
			}
			if !tt.wantItems && arr.Items != nil {
				t.Errorf("clone() Items = %v, want nil", arr.Items)
			}
		})
	}
}

// TestArrayShape_Validate verifies the validate method on array values.
func TestArrayShape_Validate(t *testing.T) {
	itemBase := NewLinkedBase(&StringShape{}, &BaseShape{ID: 2})

	tests := []struct {
		name    string
		facets  ArrayFacets
		v       any
		wantErr bool
	}{
		{
			name: "valid array with unique string items",
			facets: ArrayFacets{
				Items:       itemBase,
				UniqueItems: scalarFacetOf(true),
			},
			v: []any{"a"},
		},
		{
			name:    "non-array value rejected",
			v:       "not-an-array",
			wantErr: true,
		},
		{
			name: "fewer items than minItems",
			facets: ArrayFacets{
				MinItems: scalarFacetOf(uint64(2)),
			},
			v:       []any{"only-one"},
			wantErr: true,
		},
		{
			name: "more items than maxItems",
			facets: ArrayFacets{
				MaxItems: scalarFacetOf(uint64(2)),
			},
			v:       []any{"a", "b", "c"},
			wantErr: true,
		},
		{
			name: "item fails type constraint",
			facets: ArrayFacets{
				Items: itemBase,
			},
			v:       []any{42},
			wantErr: true,
		},
		{
			name: "duplicate items when uniqueItems=true",
			facets: ArrayFacets{
				Items:       itemBase,
				UniqueItems: scalarFacetOf(true),
			},
			v:       []any{"dup", "dup"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ArrayShape{
				BaseShape:   &BaseShape{},
				ArrayFacets: tt.facets,
			}
			if err := s.validate(tt.v, ""); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestArrayShape_Inherit checks that array shape constraints are merged correctly.
func TestArrayShape_Inherit(t *testing.T) {
	stringItemBase := NewLinkedBase(&StringShape{}, &BaseShape{ID: 1})
	numberItemBase := NewLinkedBase(&NumberShape{}, &BaseShape{ID: 2})

	tests := []struct {
		name    string
		target  ArrayFacets
		source  ArrayFacets
		wantErr bool
		check   func(*ArrayShape)
	}{
		{
			name:   "all facets inherited from source when target is empty",
			target: ArrayFacets{},
			source: ArrayFacets{
				Items:       stringItemBase,
				MinItems:    scalarFacetOf(uint64(2)),
				MaxItems:    scalarFacetOf(uint64(4)),
				UniqueItems: scalarFacetOf(true),
			},
			check: func(s *ArrayShape) {
				if s.MinItems == nil || s.MinItems.Value != 2 {
					t.Error("MinItems not inherited")
				}
				if s.MaxItems == nil || s.MaxItems.Value != 4 {
					t.Error("MaxItems not inherited")
				}
				if s.UniqueItems == nil || !s.UniqueItems.Value {
					t.Error("UniqueItems not inherited")
				}
				if s.Items == nil {
					t.Error("Items not inherited")
				}
			},
		},
		{
			name: "target facets take precedence over source",
			target: ArrayFacets{
				Items:       stringItemBase,
				MinItems:    scalarFacetOf(uint64(2)),
				MaxItems:    scalarFacetOf(uint64(4)),
				UniqueItems: scalarFacetOf(true),
			},
			source: ArrayFacets{
				Items:       NewLinkedBase(&StringShape{}, &BaseShape{ID: 3}),
				MinItems:    scalarFacetOf(uint64(1)),
				MaxItems:    scalarFacetOf(uint64(6)),
				UniqueItems: scalarFacetOf(false),
			},
			check: func(s *ArrayShape) {
				if s.MinItems == nil || s.MinItems.Value != 2 {
					t.Error("MinItems should keep target value 2")
				}
				if s.MaxItems == nil || s.MaxItems.Value != 4 {
					t.Error("MaxItems should keep target value 4")
				}
				if s.UniqueItems == nil || !s.UniqueItems.Value {
					t.Error("UniqueItems should keep target value true")
				}
			},
		},
		{
			name:    "type mismatch returns error",
			target:  ArrayFacets{},
			wantErr: true,
		},
		{
			name: "incompatible items types returns error",
			target: ArrayFacets{
				Items: stringItemBase,
			},
			source: ArrayFacets{
				Items: numberItemBase,
			},
			wantErr: true,
		},
		{
			name: "target minItems looser than source — constraint violation",
			target: ArrayFacets{
				MinItems: scalarFacetOf(uint64(1)),
			},
			source: ArrayFacets{
				MinItems: scalarFacetOf(uint64(2)),
			},
			wantErr: true,
		},
		{
			name: "target maxItems tighter than source — constraint violation",
			target: ArrayFacets{
				MaxItems: scalarFacetOf(uint64(2)),
			},
			source: ArrayFacets{
				MaxItems: scalarFacetOf(uint64(1)),
			},
			wantErr: true,
		},
		{
			name: "target allows duplicates but source requires uniqueness — violation",
			target: ArrayFacets{
				UniqueItems: scalarFacetOf(false),
			},
			source: ArrayFacets{
				UniqueItems: scalarFacetOf(true),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ArrayShape{
				BaseShape:   &BaseShape{},
				ArrayFacets: tt.target,
			}
			var src Shape
			if tt.wantErr && tt.source == (ArrayFacets{}) && tt.name == "type mismatch returns error" {
				src = &StringShape{BaseShape: &BaseShape{ID: 99}}
			} else {
				src = &ArrayShape{
					BaseShape:   &BaseShape{ID: 99},
					ArrayFacets: tt.source,
				}
			}
			got, err := s.inherit(src)
			if (err != nil) != tt.wantErr {
				t.Errorf("inherit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				arr, ok := got.(*ArrayShape)
				if !ok {
					t.Fatalf("inherit() type = %T, want *ArrayShape", got)
				}
				tt.check(arr)
			}
		})
	}
}

// TestArrayShape_Check verifies structural self-consistency of ArrayFacets.
func TestArrayShape_Check(t *testing.T) {
	tests := []struct {
		name    string
		facets  ArrayFacets
		wantErr bool
	}{
		{
			name: "valid array with all facets set",
			facets: ArrayFacets{
				Items:       NewLinkedBase(&StringShape{}, &BaseShape{ID: 1}),
				MinItems:    scalarFacetOf(uint64(2)),
				MaxItems:    scalarFacetOf(uint64(4)),
				UniqueItems: scalarFacetOf(true),
			},
		},
		{
			name: "minItems greater than maxItems",
			facets: ArrayFacets{
				MinItems: scalarFacetOf(uint64(4)),
				MaxItems: scalarFacetOf(uint64(2)),
			},
			wantErr: true,
		},
		{
			name: "invalid nested items shape",
			facets: ArrayFacets{
				Items: &BaseShape{
					ID: 1,
					Shape: &StringShape{
						StringFacets: StringFacets{LengthFacets: LengthFacets{
							MinLength: scalarFacetOf(uint64(4)),
							MaxLength: scalarFacetOf(uint64(2)),
						}},
						BaseShape: &BaseShape{},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ArrayShape{
				BaseShape:   &BaseShape{},
				ArrayFacets: tt.facets,
			}
			if err := s.check(); (err != nil) != tt.wantErr {
				t.Errorf("check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestArrayShape_unmarshalYAMLNodes verifies YAML node parsing for ArrayShape facets.
// Each sub-test uses a fresh RAML/BaseShape to avoid cross-test contamination.
func TestArrayShape_unmarshalYAMLNodes(t *testing.T) {
	mappingNode := func(kv ...*yaml.Node) *yaml.Node {
		return &yaml.Node{Kind: yaml.MappingNode, Content: kv}
	}
	scalar := func(val, tag string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val, Tag: tag}
	}

	tests := []struct {
		name    string
		nodes   []*yaml.Node
		wantErr bool
	}{
		{
			name: "all facets: minItems, maxItems, uniqueItems, items, custom",
			nodes: []*yaml.Node{
				{Value: "minItems"}, scalar("2", "!!int"),
				{Value: "maxItems"}, scalar("4", "!!int"),
				{Value: "uniqueItems"}, scalar("true", "!!bool"),
				{Value: "items"}, mappingNode(scalar("type", "!!str"), scalar("string", "!!str")),
				{Value: "custom"}, scalar("value", "!!str"),
			},
		},
		{
			name: "invalid minItems type",
			nodes: []*yaml.Node{
				{Value: "minItems"}, scalar("string", "!!str"),
			},
			wantErr: true,
		},
		{
			name: "invalid maxItems type",
			nodes: []*yaml.Node{
				{Value: "maxItems"}, scalar("string", "!!str"),
			},
			wantErr: true,
		},
		{
			name: "invalid uniqueItems type",
			nodes: []*yaml.Node{
				{Value: "uniqueItems"}, scalar("string", "!!str"),
			},
			wantErr: true,
		},
		{
			name: "invalid items node",
			nodes: []*yaml.Node{
				{Value: "items"}, mappingNode(
					scalar("type", "!!int"),
					scalar("string", "!!int"),
				),
			},
			wantErr: true,
		},
		{
			name: "invalid custom facet tag",
			nodes: []*yaml.Node{
				{Value: "custom"}, scalar("value", "!!int"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			base := makeTestBase(t, r, "s")
			s := &ArrayShape{BaseShape: base}
			if err := s.unmarshalYAMLNodes(tt.nodes); (err != nil) != tt.wantErr {
				t.Errorf("unmarshalYAMLNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestObjectShape_unmarshalPatternProperties verifies parsing pattern property YAML nodes.
func TestObjectShape_unmarshalPatternProperties(t *testing.T) {
	scalar := func(val, tag string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: val, Tag: tag} }

	tests := []struct {
		name         string
		nodeName     string
		propertyName string
		data         *yaml.Node
		wantErr      bool
	}{
		{
			name:         "valid pattern property with type",
			nodeName:     "patternProperties",
			propertyName: "/^name.*/",
			data: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("type", "!!str"), scalar("string", "!!str"),
			}},
		},
		{
			name:         "required facet not allowed on pattern property",
			nodeName:     "patternProperties",
			propertyName: "/^name.*/",
			data: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("required", "!!str"), scalar("true", "!!bool"),
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			base := makeTestBase(t, r, "o")
			s := &ObjectShape{BaseShape: base}
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: tt.nodeName}
			err := s.unmarshalPatternProperties(tt.propertyName, keyNode, tt.data, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshalPatternProperties() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestObjectShape_unmarshalProperty verifies property-name parsing including pattern detection.
func TestObjectShape_unmarshalProperty(t *testing.T) {
	scalar := func(val, tag string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: val, Tag: tag} }
	typedMapping := func(typeName string) *yaml.Node {
		return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			scalar("type", "!!str"), scalar(typeName, "!!str"),
		}}
	}

	tests := []struct {
		name     string
		nodeName string
		data     *yaml.Node
		wantErr  bool
	}{
		{
			name:     "plain property name",
			nodeName: "myProp",
			data:     typedMapping("string"),
		},
		{
			name:     "slash-delimited name treated as pattern property",
			nodeName: "//",
			data:     typedMapping("string"),
		},
		{
			name:     "property with invalid 'required' decode fails",
			nodeName: "myProp",
			data: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("required", "!!bool"), scalar("true", "!!int"),
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			base := makeTestBase(t, r, "o")
			s := &ObjectShape{BaseShape: base}
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: tt.nodeName}
			if err := s.unmarshalProperty(keyNode, tt.data); (err != nil) != tt.wantErr {
				t.Errorf("unmarshalProperty() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestObjectShape_unmarshalYAMLNodes verifies full YAML node parsing for ObjectShape facets.
func TestObjectShape_unmarshalYAMLNodes(t *testing.T) {
	scalar := func(val, tag string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: val, Tag: tag} }
	strTypeMapping := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalar("type", "!!str"), scalar("string", "!!str"),
	}}

	tests := []struct {
		name    string
		nodes   []*yaml.Node
		wantErr bool
	}{
		{
			name: "all object facets: minProperties, maxProperties, additionalProperties, discriminator, discriminatorValue, properties, custom",
			nodes: []*yaml.Node{
				{Value: "minProperties"}, scalar("2", "!!int"),
				{Value: "maxProperties"}, scalar("4", "!!int"),
				{Value: "additionalProperties"}, scalar("false", "!!bool"),
				{Value: "discriminator"}, scalar("kind", "!!str"),
				{Value: "discriminatorValue"}, scalar("cat", "!!str"),
				{Value: "properties"}, {Kind: yaml.MappingNode, Content: []*yaml.Node{
					scalar("name", "!!str"),
					strTypeMapping,
				}},
				{Value: "custom"}, scalar("value", "!!str"),
			},
		},
		{
			name:    "invalid minProperties type",
			nodes:   []*yaml.Node{{Value: "minProperties"}, scalar("string", "!!str")},
			wantErr: true,
		},
		{
			name:    "invalid maxProperties type",
			nodes:   []*yaml.Node{{Value: "maxProperties"}, scalar("string", "!!str")},
			wantErr: true,
		},
		{
			name:    "invalid additionalProperties type",
			nodes:   []*yaml.Node{{Value: "additionalProperties"}, scalar("string", "!!str")},
			wantErr: true,
		},
		{
			name: "invalid discriminator type",
			nodes: []*yaml.Node{
				{Value: "discriminator"},
				{Kind: yaml.MappingNode, Value: "{", Tag: "!!unknown"},
			},
			wantErr: true,
		},
		{
			name: "invalid discriminatorValue kind",
			nodes: []*yaml.Node{
				{Value: "discriminatorValue"},
				{Kind: 100500, Value: "{", Tag: "!!int"},
			},
			wantErr: true,
		},
		{
			name: "invalid pattern property inside properties",
			nodes: []*yaml.Node{
				{Value: "properties"},
				{Kind: yaml.MappingNode, Content: []*yaml.Node{
					scalar("//", "!!str"),
					{Kind: yaml.MappingNode, Content: []*yaml.Node{
						scalar("required", "!!str"), scalar("true", "!!bool"),
					}},
				}},
			},
			wantErr: true,
		},
		{
			name:    "invalid custom facet tag",
			nodes:   []*yaml.Node{{Value: "custom"}, scalar("value", "!!int")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			base := makeTestBase(t, r, "o")
			s := &ObjectShape{BaseShape: base}
			if err := s.unmarshalYAMLNodes(tt.nodes); (err != nil) != tt.wantErr {
				t.Errorf("unmarshalYAMLNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestObjectShape_clone verifies that clone produces a deep-copied ObjectShape.
func TestObjectShape_clone(t *testing.T) {
	tests := []struct {
		name  string
		src   *ObjectShape
		check func(*ObjectShape)
	}{
		{
			name: "properties and patternProperties are deep-copied",
			src: &ObjectShape{
				BaseShape: &BaseShape{ID: 1},
				ObjectFacets: ObjectFacets{
					Properties: newPropsMap(
						propEntry{"name", stringProp("name", 10)},
					),
					PatternProperties: newPatternPropsMap(
						patternProp("^id.*", 20),
					),
				},
			},
			check: func(s *ObjectShape) {
				if s.ID == 1 {
					t.Error("clone() ID unchanged, expected a different base")
				}
				if s.Properties == nil {
					t.Error("Properties should be present after clone")
				}
				if s.PatternProperties == nil {
					t.Error("PatternProperties should be present after clone")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.src.clone(&BaseShape{ID: 99}, make(map[int64]*BaseShape))
			obj, ok := got.(*ObjectShape)
			if !ok {
				t.Fatalf("clone() type = %T, want *ObjectShape", got)
			}
			tt.check(obj)
		})
	}
}

// TestObjectShape_validateProperties verifies property-level validation logic.
func TestObjectShape_validateProperties(t *testing.T) {
	tests := []struct {
		name    string
		facets  ObjectFacets
		props   map[string]any
		wantErr bool
	}{
		{
			name: "all required properties present, additionalProperties forbidden",
			facets: ObjectFacets{
				Properties:           newPropsMap(propEntry{"name", stringProp("name", 1)}),
				AdditionalProperties: scalarFacetOf(false),
			},
			props: map[string]any{"name": "Alice"},
		},
		{
			name: "extra property allowed when additionalProperties=true",
			facets: ObjectFacets{
				AdditionalProperties: scalarFacetOf(true),
			},
			props: map[string]any{"unknown": "value"},
		},
		{
			name: "property value fails type validation",
			facets: ObjectFacets{
				Properties: newPropsMap(propEntry{"age", Property{
					Base: &BaseShape{Shape: &StringShape{}},
				}}),
			},
			props:   map[string]any{"age": 42},
			wantErr: true,
		},
		{
			name: "pattern property value fails type validation",
			facets: ObjectFacets{
				PatternProperties: newPatternPropsMap(patternPropEntry{
					key: "/^tag.*/",
					prop: PatternProperty{
						Pattern: regexp.MustCompile("^tag.*"),
						Base:    &BaseShape{Shape: &StringShape{}},
					},
				}),
			},
			props:   map[string]any{"tagX": 42},
			wantErr: true,
		},
		{
			name: "required property absent, additionalProperties forbidden",
			facets: ObjectFacets{
				Properties: newPropsMap(propEntry{"name", Property{
					Base: &BaseShape{Shape: &StringShape{}},
				}}),
				AdditionalProperties: scalarFacetOf(false),
			},
			props:   map[string]any{"other": "val"},
			wantErr: true,
		},
		{
			name: "extra property present but additionalProperties=false",
			facets: ObjectFacets{
				AdditionalProperties: scalarFacetOf(false),
			},
			props:   map[string]any{"extra": "value"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{
				BaseShape:    &BaseShape{ID: 1},
				ObjectFacets: tt.facets,
			}
			if err := s.validateProperties("$", tt.props); (err != nil) != tt.wantErr {
				t.Errorf("validateProperties() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestObjectShape_validate verifies top-level object validation including property counts.
func TestObjectShape_validate(t *testing.T) {
	tests := []struct {
		name    string
		facets  ObjectFacets
		v       any
		wantErr bool
	}{
		{
			name: "object within min/max property bounds",
			facets: ObjectFacets{
				MinProperties:        scalarFacetOf(uint64(2)),
				MaxProperties:        scalarFacetOf(uint64(4)),
				AdditionalProperties: scalarFacetOf(true),
			},
			v: map[string]any{
				"a": 1, "b": 2, "c": 3,
			},
		},
		{
			name:    "non-map value rejected",
			facets:  ObjectFacets{},
			v:       42,
			wantErr: true,
		},
		{
			name: "too few properties",
			facets: ObjectFacets{
				MinProperties: scalarFacetOf(uint64(2)),
			},
			v:       map[string]any{"a": 1},
			wantErr: true,
		},
		{
			name: "too many properties",
			facets: ObjectFacets{
				MaxProperties: scalarFacetOf(uint64(2)),
			},
			v:       map[string]any{"a": 1, "b": 2, "c": 3},
			wantErr: true,
		},
		{
			name: "extra property rejected because additionalProperties=false",
			facets: ObjectFacets{
				AdditionalProperties: scalarFacetOf(false),
			},
			v:       map[string]any{"unknown": "val"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{
				BaseShape:    &BaseShape{ID: 1},
				ObjectFacets: tt.facets,
			}
			if err := s.validate(tt.v, ""); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestObjectShape_inheritMinProperties(t *testing.T) {
	tests := []struct {
		name         string
		targetMin    *ScalarFacet[uint64]
		sourceMin    *ScalarFacet[uint64]
		wantErr      bool
		wantMinValue uint64
	}{
		{
			name:         "target present, larger than source — keep target",
			targetMin:    scalarFacetOf(uint64(4)),
			sourceMin:    scalarFacetOf(uint64(2)),
			wantMinValue: 4,
		},
		{
			name:         "target absent — inherit from source",
			sourceMin:    scalarFacetOf(uint64(4)),
			wantMinValue: 4,
		},
		{
			name:      "target smaller than source — constraint violation",
			targetMin: scalarFacetOf(uint64(2)),
			sourceMin: scalarFacetOf(uint64(4)),
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{
				BaseShape:    &BaseShape{ID: 1},
				ObjectFacets: ObjectFacets{MinProperties: tt.targetMin},
			}
			src := &ObjectShape{
				BaseShape:    &BaseShape{ID: 2},
				ObjectFacets: ObjectFacets{MinProperties: tt.sourceMin},
			}
			err := s.inheritMinProperties(src)
			if (err != nil) != tt.wantErr {
				t.Errorf("inheritMinProperties() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if s.MinProperties == nil || s.MinProperties.Value != tt.wantMinValue {
					t.Errorf("MinProperties.Value = %v, want %v", s.MinProperties, tt.wantMinValue)
				}
			}
		})
	}
}

func TestObjectShape_inheritMaxProperties(t *testing.T) {
	tests := []struct {
		name         string
		targetMax    *ScalarFacet[uint64]
		sourceMax    *ScalarFacet[uint64]
		wantErr      bool
		wantMaxValue uint64
	}{
		{
			name:         "target present, smaller than source — keep target",
			targetMax:    scalarFacetOf(uint64(2)),
			sourceMax:    scalarFacetOf(uint64(4)),
			wantMaxValue: 2,
		},
		{
			name:         "target absent — inherit from source",
			sourceMax:    scalarFacetOf(uint64(4)),
			wantMaxValue: 4,
		},
		{
			name:      "target larger than source — constraint violation",
			targetMax: scalarFacetOf(uint64(4)),
			sourceMax: scalarFacetOf(uint64(2)),
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{
				BaseShape:    &BaseShape{ID: 1},
				ObjectFacets: ObjectFacets{MaxProperties: tt.targetMax},
			}
			src := &ObjectShape{
				BaseShape:    &BaseShape{ID: 2},
				ObjectFacets: ObjectFacets{MaxProperties: tt.sourceMax},
			}
			err := s.inheritMaxProperties(src)
			if (err != nil) != tt.wantErr {
				t.Errorf("inheritMaxProperties() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if s.MaxProperties == nil || s.MaxProperties.Value != tt.wantMaxValue {
					t.Errorf("MaxProperties.Value = %v, want %v", s.MaxProperties, tt.wantMaxValue)
				}
			}
		})
	}
}

func TestObjectShape_inheritProperties(t *testing.T) {
	tests := []struct {
		name        string
		targetProps *orderedmap.OrderedMap[string, Property]
		sourceProps *orderedmap.OrderedMap[string, Property]
		wantErr     bool
		check       func(*ObjectShape)
	}{
		{
			name:        "source properties merged into target",
			targetProps: newPropsMap(propEntry{"existing", stringProp("existing", 1)}),
			sourceProps: newPropsMap(
				propEntry{"existing", stringProp("existing", 2)},
				propEntry{"new", stringProp("new", 3)},
			),
			check: func(s *ObjectShape) {
				if s.Properties.Len() != 2 {
					t.Errorf("Properties.Len() = %d, want 2", s.Properties.Len())
				}
				if _, ok := s.Properties.Get("new"); !ok {
					t.Error("'new' property not inherited from source")
				}
			},
		},
		{
			name:        "target has no properties — all inherited from source",
			targetProps: nil,
			sourceProps: newPropsMap(propEntry{"fromSource", stringProp("fromSource", 1)}),
			check: func(s *ObjectShape) {
				if s.Properties == nil || s.Properties.Len() != 1 {
					t.Errorf("Properties.Len() = %d, want 1", s.Properties.Len())
				}
			},
		},
		{
			name:        "source has no properties — target unchanged",
			targetProps: newPropsMap(propEntry{"existing", stringProp("existing", 1)}),
			sourceProps: nil,
			check: func(s *ObjectShape) {
				if s.Properties.Len() != 1 {
					t.Errorf("Properties.Len() = %d, want 1", s.Properties.Len())
				}
			},
		},
		{
			name: "making required source property optional in target is rejected",
			targetProps: newPropsMap(propEntry{"name", Property{
				Base:     &BaseShape{Shape: &StringShape{}},
				Required: false,
			}}),
			sourceProps: newPropsMap(propEntry{"name", Property{
				Base:     &BaseShape{Shape: &StringShape{}},
				Required: true,
			}}),
			wantErr: true,
		},
		{
			name: "incompatible property types are rejected",
			targetProps: newPropsMap(propEntry{"kind", Property{
				Base: &BaseShape{Shape: &StringShape{BaseShape: &BaseShape{KeyPos: stacktrace.Position{}}}},
			}}),
			sourceProps: newPropsMap(propEntry{"kind", Property{
				Base: &BaseShape{Shape: &NumberShape{BaseShape: &BaseShape{Type: "number"}}},
			}}),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{
				BaseShape:    &BaseShape{ID: 1},
				ObjectFacets: ObjectFacets{Properties: tt.targetProps},
			}
			src := &ObjectShape{
				BaseShape:    &BaseShape{ID: 2},
				ObjectFacets: ObjectFacets{Properties: tt.sourceProps},
			}
			err := s.inheritProperties(src)
			if (err != nil) != tt.wantErr {
				t.Errorf("inheritProperties() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(s)
			}
		})
	}
}

func TestObjectShape_inheritPatternProperties(t *testing.T) {
	pp1 := patternPropEntry{key: "/^prefix.*/", prop: PatternProperty{
		Pattern: regexp.MustCompile("^prefix.*"),
		Base:    &BaseShape{Shape: &StringShape{}},
	}}
	pp2 := patternPropEntry{key: "/^extra.*/", prop: PatternProperty{
		Pattern: regexp.MustCompile("^extra.*"),
		Base:    &BaseShape{Shape: &StringShape{}},
	}}
	pp1Incompatible := patternPropEntry{key: "/^prefix.*/", prop: PatternProperty{
		Pattern: regexp.MustCompile("^prefix.*"),
		Base:    &BaseShape{Shape: &NumberShape{BaseShape: &BaseShape{Type: "number"}}},
	}}

	tests := []struct {
		name         string
		targetPPs    *orderedmap.OrderedMap[string, PatternProperty]
		sourcePPs    *orderedmap.OrderedMap[string, PatternProperty]
		wantErr      bool
		wantPPsCount int
	}{
		{
			name:         "source pattern properties merged into target",
			targetPPs:    newPatternPropsMap(pp1),
			sourcePPs:    newPatternPropsMap(pp1, pp2),
			wantPPsCount: 2,
		},
		{
			name:         "target has none — all inherited from source",
			sourcePPs:    newPatternPropsMap(pp1),
			wantPPsCount: 1,
		},
		{
			name: "incompatible pattern property types rejected",
			targetPPs: newPatternPropsMap(patternPropEntry{
				key: "/^prefix.*/",
				prop: PatternProperty{
					Pattern: regexp.MustCompile("^prefix.*"),
					Base:    &BaseShape{Shape: &StringShape{BaseShape: &BaseShape{KeyPos: stacktrace.Position{}}}},
				},
			}),
			sourcePPs: newPatternPropsMap(pp1Incompatible),
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{
				BaseShape:    &BaseShape{ID: 1},
				ObjectFacets: ObjectFacets{PatternProperties: tt.targetPPs},
			}
			src := &ObjectShape{
				BaseShape:    &BaseShape{ID: 2},
				ObjectFacets: ObjectFacets{PatternProperties: tt.sourcePPs},
			}
			err := s.inheritPatternProperties(src)
			if (err != nil) != tt.wantErr {
				t.Errorf("inheritPatternProperties() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && s.PatternProperties.Len() != tt.wantPPsCount {
				t.Errorf("PatternProperties.Len() = %d, want %d", s.PatternProperties.Len(), tt.wantPPsCount)
			}
		})
	}
}

// TestObjectShape_inherit verifies the full Object inherit pipeline.
func TestObjectShape_inherit(t *testing.T) {
	tests := []struct {
		name    string
		target  ObjectFacets
		source  Shape
		wantErr bool
		check   func(Shape)
	}{
		{
			name: "all facets inherited or kept from source",
			target: ObjectFacets{
				MinProperties: scalarFacetOf(uint64(2)),
				MaxProperties: scalarFacetOf(uint64(4)),
				Properties:    newPropsMap(propEntry{"a", stringProp("a", 1)}),
				PatternProperties: newPatternPropsMap(patternPropEntry{
					key:  "/^p.*/",
					prop: PatternProperty{Pattern: regexp.MustCompile("^p.*"), Base: &BaseShape{Shape: &StringShape{}}},
				}),
			},
			source: &ObjectShape{
				BaseShape: &BaseShape{ID: 99},
				ObjectFacets: ObjectFacets{
					MinProperties: scalarFacetOf(uint64(1)),
					MaxProperties: scalarFacetOf(uint64(5)),
					Properties: newPropsMap(
						propEntry{"a", stringProp("a", 2)},
						propEntry{"b", stringProp("b", 3)},
					),
					PatternProperties: newPatternPropsMap(
						patternPropEntry{
							key:  "/^p.*/",
							prop: PatternProperty{Pattern: regexp.MustCompile("^p.*"), Base: &BaseShape{Shape: &StringShape{}}},
						},
						patternPropEntry{
							key:  "/^q.*/",
							prop: PatternProperty{Pattern: regexp.MustCompile("^q.*"), Base: &BaseShape{Shape: &StringShape{}}},
						},
					),
					AdditionalProperties: scalarFacetOf(false),
					Discriminator:        scalarFacetOf("kind"),
					DiscriminatorValue:   &DataNode{Value: NewScalarNodeValue("cat")},
				},
			},
			check: func(got Shape) {
				obj := got.(*ObjectShape)
				if obj.MinProperties == nil || obj.MinProperties.Value != 2 {
					t.Error("MinProperties mismatch")
				}
				if obj.MaxProperties == nil || obj.MaxProperties.Value != 4 {
					t.Error("MaxProperties mismatch")
				}
				if obj.Properties.Len() != 2 {
					t.Errorf("Properties.Len() = %d, want 2", obj.Properties.Len())
				}
				if obj.PatternProperties.Len() != 2 {
					t.Errorf("PatternProperties.Len() = %d, want 2", obj.PatternProperties.Len())
				}
				if obj.AdditionalProperties == nil || obj.AdditionalProperties.Value != false {
					t.Error("AdditionalProperties not inherited")
				}
				if obj.Discriminator == nil || obj.Discriminator.Value != "kind" {
					t.Error("Discriminator not inherited")
				}
			},
		},
		{
			name:   "recursive source shape is unwrapped",
			target: ObjectFacets{},
			source: &RecursiveShape{
				BaseShape: &BaseShape{ID: 2},
				Head: &BaseShape{
					Shape: &ObjectShape{
						BaseShape: &BaseShape{ID: 3},
						ObjectFacets: ObjectFacets{
							Properties: newPropsMap(propEntry{"x", stringProp("x", 4)}),
						},
					},
				},
			},
			check: func(got Shape) {
				obj := got.(*ObjectShape)
				if _, ok := obj.Properties.Get("x"); !ok {
					t.Error("property 'x' not inherited from recursive source")
				}
			},
		},
		{
			name:    "incompatible source type rejected",
			target:  ObjectFacets{},
			source:  &StringShape{BaseShape: &BaseShape{ID: 2}},
			wantErr: true,
		},
		{
			name:    "minProperties constraint violation",
			target:  ObjectFacets{MinProperties: scalarFacetOf(uint64(2))},
			source:  &ObjectShape{BaseShape: &BaseShape{ID: 2}, ObjectFacets: ObjectFacets{MinProperties: scalarFacetOf(uint64(4))}},
			wantErr: true,
		},
		{
			name:    "maxProperties constraint violation",
			target:  ObjectFacets{MaxProperties: scalarFacetOf(uint64(4))},
			source:  &ObjectShape{BaseShape: &BaseShape{ID: 2}, ObjectFacets: ObjectFacets{MaxProperties: scalarFacetOf(uint64(2))}},
			wantErr: true,
		},
		{
			name: "incompatible property type rejected",
			target: ObjectFacets{
				Properties: newPropsMap(propEntry{"kind", Property{
					Base: &BaseShape{Shape: &StringShape{BaseShape: &BaseShape{KeyPos: stacktrace.Position{}}}},
				}}),
			},
			source: &ObjectShape{
				BaseShape: &BaseShape{ID: 2},
				ObjectFacets: ObjectFacets{
					Properties: newPropsMap(propEntry{"kind", Property{
						Base: &BaseShape{Shape: &NumberShape{BaseShape: &BaseShape{Type: "number"}}},
					}}),
				},
			},
			wantErr: true,
		},
		{
			name: "incompatible pattern property type rejected",
			target: ObjectFacets{
				PatternProperties: newPatternPropsMap(patternPropEntry{
					key: "/^p.*/",
					prop: PatternProperty{
						Pattern: regexp.MustCompile("^p.*"),
						Base:    &BaseShape{Shape: &StringShape{BaseShape: &BaseShape{KeyPos: stacktrace.Position{}}}},
					},
				}),
			},
			source: &ObjectShape{
				BaseShape: &BaseShape{ID: 2},
				ObjectFacets: ObjectFacets{
					PatternProperties: newPatternPropsMap(patternPropEntry{
						key: "/^p.*/",
						prop: PatternProperty{
							Pattern: regexp.MustCompile("^p.*"),
							Base:    &BaseShape{Shape: &NumberShape{BaseShape: &BaseShape{Type: "number"}}},
						},
					}),
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{
				BaseShape:    &BaseShape{ID: 1},
				ObjectFacets: tt.target,
			}
			got, err := s.inherit(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("inherit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

func TestObjectShape_checkPatternProperties(t *testing.T) {
	tests := []struct {
		name    string
		facets  ObjectFacets
		wantErr bool
	}{
		{
			name: "valid pattern property",
			facets: ObjectFacets{
				PatternProperties: newPatternPropsMap(patternProp("^tag.*", 1)),
			},
		},
		{
			name:   "nil pattern properties",
			facets: ObjectFacets{},
		},
		{
			name: "pattern properties conflict with additionalProperties=false",
			facets: ObjectFacets{
				PatternProperties: newPatternPropsMap(patternPropEntry{
					key:  "/^tag.*/",
					prop: PatternProperty{Pattern: regexp.MustCompile("^tag.*"), Base: &BaseShape{Shape: &StringShape{}}},
				}),
				AdditionalProperties: scalarFacetOf(false),
			},
			wantErr: true,
		},
		{
			name: "invalid pattern property shape",
			facets: ObjectFacets{
				PatternProperties: newPatternPropsMap(patternPropEntry{
					key: "/^p.*/",
					prop: PatternProperty{
						Pattern: regexp.MustCompile("^p.*"),
						Base: &BaseShape{
							Shape: &StringShape{
								StringFacets: StringFacets{LengthFacets: LengthFacets{
									MinLength: scalarFacetOf(uint64(10)),
									MaxLength: scalarFacetOf(uint64(5)),
								}},
								BaseShape: &BaseShape{KeyPos: stacktrace.Position{}},
							},
						},
					},
				}),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{BaseShape: &BaseShape{ID: 1}, ObjectFacets: tt.facets}
			if err := s.checkPatternProperties(); (err != nil) != tt.wantErr {
				t.Errorf("checkPatternProperties() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestObjectShape_checkProperties(t *testing.T) {
	tests := []struct {
		name    string
		facets  ObjectFacets
		wantErr bool
	}{
		{
			name: "valid properties with discriminator matching a defined property",
			facets: ObjectFacets{
				Properties:    newPropsMap(propEntry{"kind", stringProp("kind", 1)}),
				Discriminator: scalarFacetOf("kind"),
			},
		},
		{
			name:   "nil properties",
			facets: ObjectFacets{},
		},
		{
			name: "invalid nested property shape",
			facets: ObjectFacets{
				Properties: newPropsMap(propEntry{"x", Property{
					Base: &BaseShape{
						Shape: &StringShape{
							StringFacets: StringFacets{LengthFacets: LengthFacets{
								MinLength: scalarFacetOf(uint64(10)),
								MaxLength: scalarFacetOf(uint64(5)),
							}},
							BaseShape: &BaseShape{KeyPos: stacktrace.Position{}},
						},
					},
				}}),
			},
			wantErr: true,
		},
		{
			name: "discriminator references missing property",
			facets: ObjectFacets{
				Properties:    newPropsMap(),
				Discriminator: scalarFacetOf("missing"),
			},
			wantErr: true,
		},
		{
			name: "discriminator property is not a scalar shape",
			facets: ObjectFacets{
				Properties: newPropsMap(propEntry{"kind", Property{
					Base: &BaseShape{Shape: &ObjectShape{BaseShape: &BaseShape{}}},
				}}),
				Discriminator: scalarFacetOf("kind"),
			},
			wantErr: true,
		},
		{
			name: "discriminatorValue has wrong type",
			facets: ObjectFacets{
				Properties:         newPropsMap(propEntry{"kind", Property{Base: &BaseShape{Shape: &StringShape{}}}}),
				Discriminator:      scalarFacetOf("kind"),
				DiscriminatorValue: &DataNode{Value: NewScalarNodeValue(1)},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{BaseShape: &BaseShape{ID: 1}, ObjectFacets: tt.facets}
			if err := s.checkProperties(); (err != nil) != tt.wantErr {
				t.Errorf("checkProperties() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestObjectShape_check(t *testing.T) {
	tests := []struct {
		name    string
		facets  ObjectFacets
		wantErr bool
	}{
		{
			name:   "empty facets pass check",
			facets: ObjectFacets{},
		},
		{
			name: "minProperties greater than maxProperties",
			facets: ObjectFacets{
				MinProperties: scalarFacetOf(uint64(4)),
				MaxProperties: scalarFacetOf(uint64(2)),
			},
			wantErr: true,
		},
		{
			name: "invalid nested property shape",
			facets: ObjectFacets{
				Properties: newPropsMap(propEntry{"x", Property{
					Base: &BaseShape{
						Shape: &StringShape{
							StringFacets: StringFacets{LengthFacets: LengthFacets{
								MinLength: scalarFacetOf(uint64(10)),
								MaxLength: scalarFacetOf(uint64(5)),
							}},
							BaseShape: &BaseShape{KeyPos: stacktrace.Position{}},
						},
					},
				}}),
			},
			wantErr: true,
		},
		{
			name: "invalid pattern property shape",
			facets: ObjectFacets{
				PatternProperties: newPatternPropsMap(patternPropEntry{
					key: "/^p.*/",
					prop: PatternProperty{
						Pattern: regexp.MustCompile("^p.*"),
						Base: &BaseShape{
							Shape: &StringShape{
								StringFacets: StringFacets{LengthFacets: LengthFacets{
									MinLength: scalarFacetOf(uint64(10)),
									MaxLength: scalarFacetOf(uint64(5)),
								}},
								BaseShape: &BaseShape{KeyPos: stacktrace.Position{}},
							},
						},
					},
				}),
			},
			wantErr: true,
		},
		{
			name: "discriminator without any properties",
			facets: ObjectFacets{
				Discriminator: scalarFacetOf("kind"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{BaseShape: &BaseShape{ID: 1}, ObjectFacets: tt.facets}
			if err := s.check(); (err != nil) != tt.wantErr {
				t.Errorf("check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRAML_makePatternProperty verifies pattern-property creation from YAML.
func TestRAML_makePatternProperty(t *testing.T) {
	tests := []struct {
		name                string
		propertyName        string
		keyNodeValue        string
		valueNode           *yaml.Node
		hasImplicitOptional bool
		wantErr             bool
		check               func(PatternProperty)
	}{
		{
			name:         "valid pattern property string type",
			propertyName: "/^prefix.*/",
			keyNodeValue: "name",
			valueNode:    &yaml.Node{Kind: yaml.ScalarNode, Value: "string", Tag: "!!str"},
			check: func(pp PatternProperty) {
				if !pp.Pattern.MatchString("prefixABC") {
					t.Error("Pattern does not match expected string")
				}
				if _, ok := pp.Base.Shape.(*StringShape); !ok {
					t.Errorf("Shape type = %T, want *StringShape", pp.Base.Shape)
				}
			},
		},
		{
			name:         "required facet on pattern property is rejected",
			propertyName: "/^prefix.*/",
			keyNodeValue: "name",
			valueNode:    &yaml.Node{Kind: yaml.ScalarNode, Value: "string", Tag: "!!int"},
			wantErr:      true,
		},
		{
			name:                "implicit-optional flag on pattern property is rejected",
			propertyName:        "/^prefix.*/",
			keyNodeValue:        "name",
			valueNode:           &yaml.Node{Kind: yaml.ScalarNode, Value: "string", Tag: "!!str"},
			hasImplicitOptional: true,
			wantErr:             true,
		},
		{
			name:         "invalid regex pattern returns error",
			propertyName: "/[unclosed/",
			keyNodeValue: "name",
			valueNode:    &yaml.Node{Kind: yaml.ScalarNode, Value: "string", Tag: "!!str"},
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: tt.keyNodeValue}
			got, err := r.makePatternProperty(tt.propertyName, keyNode, tt.valueNode, "test.raml", tt.hasImplicitOptional)
			if (err != nil) != tt.wantErr {
				t.Errorf("makePatternProperty() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

// TestRAML_chompImplicitOptional verifies optional-marker detection on property names.
func TestRAML_chompImplicitOptional(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantName     string
		wantOptional bool
	}{
		{
			name:         "name ending with '?' is optional",
			input:        "property?",
			wantName:     "property",
			wantOptional: true,
		},
		{
			name:         "name without '?' is not optional",
			input:        "property",
			wantName:     "property",
			wantOptional: false,
		},
		{
			name:         "empty string stays empty, not optional",
			input:        "",
			wantName:     "",
			wantOptional: false,
		},
		{
			name:         "only '?' is consumed and marked optional",
			input:        "?",
			wantName:     "",
			wantOptional: true,
		},
		{
			name:         "double '??' — only trailing '?' stripped",
			input:        "name??",
			wantName:     "name?",
			wantOptional: true,
		},
		{
			name:         "'?' in the middle — not stripped",
			input:        "na?me",
			wantName:     "na?me",
			wantOptional: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotOptional := chompImplicitOptional(tt.input)
			if gotName != tt.wantName {
				t.Errorf("chompImplicitOptional() name = %q, want %q", gotName, tt.wantName)
			}
			if gotOptional != tt.wantOptional {
				t.Errorf("chompImplicitOptional() optional = %v, want %v", gotOptional, tt.wantOptional)
			}
		})
	}
}

// TestRAML_makeProperty verifies property creation including name/required resolution.
func TestRAML_makeProperty(t *testing.T) {
	strTypeNode := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "type", Tag: "!!str"},
		{Kind: yaml.ScalarNode, Value: "string", Tag: "!!str"},
	}}
	withRequired := func(required string) *yaml.Node {
		return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "type", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "string", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "required", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: required, Tag: "!!bool"},
		}}
	}

	tests := []struct {
		name                string
		keyNodeValue        string
		propertyName        string
		valueNode           *yaml.Node
		hasImplicitOptional bool
		wantErr             bool
		check               func(Property)
	}{
		{
			name:         "explicit name, no '?', defaults to required",
			keyNodeValue: "address",
			propertyName: "address",
			valueNode:    strTypeNode,
			check: func(p Property) {
				if p.Name != "address" {
					t.Errorf("Name = %q, want %q", p.Name, "address")
				}
				if !p.Required {
					t.Error("Required = false, want true")
				}
			},
		},
		{
			name:                "name with '?' and hasImplicitOptional=true — chomped, not required",
			keyNodeValue:        "nick?",
			propertyName:        "nick",
			valueNode:           strTypeNode,
			hasImplicitOptional: true,
			check: func(p Property) {
				if p.Name != "nick" {
					t.Errorf("Name = %q, want %q", p.Name, "nick")
				}
				if p.Required {
					t.Error("Required = true, want false")
				}
			},
		},
		{
			name:                "explicit required=false overrides implicit optional — name kept raw",
			keyNodeValue:        "tag?",
			propertyName:        "tag",
			valueNode:           withRequired("false"),
			hasImplicitOptional: true,
			check: func(p Property) {
				// When hasImplicitOptional AND shape explicitly sets required,
				// node name (with '?') is kept as finalName.
				if p.Required {
					t.Error("Required = true, want false")
				}
			},
		},
		{
			name:         "required=true in shape",
			keyNodeValue: "id",
			propertyName: "id",
			valueNode:    withRequired("true"),
			check: func(p Property) {
				if !p.Required {
					t.Error("Required = false, want true")
				}
			},
		},
		{
			name:         "invalid YAML shape returns error",
			keyNodeValue: "bad",
			propertyName: "bad",
			valueNode:    &yaml.Node{Kind: yaml.ScalarNode, Value: "string", Tag: "!!int"},
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: tt.keyNodeValue}
			got, err := r.makeProperty(keyNode, tt.valueNode, tt.propertyName, "test.raml", tt.hasImplicitOptional)
			if (err != nil) != tt.wantErr {
				t.Errorf("makeProperty() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

// TestUnionShape_clone verifies that clone copies AnyOf members into a new UnionShape.
func TestUnionShape_clone(t *testing.T) {
	members := []*BaseShape{
		NewLinkedBase(&StringShape{}, &BaseShape{ID: 2}),
		NewLinkedBase(&NumberShape{}, &BaseShape{ID: 3}),
	}
	s := &UnionShape{
		BaseShape:   &BaseShape{ID: 1},
		UnionFacets: UnionFacets{AnyOf: members},
	}
	got := s.clone(&BaseShape{ID: 10}, make(map[int64]*BaseShape))
	u, ok := got.(*UnionShape)
	if !ok {
		t.Fatalf("clone() type = %T, want *UnionShape", got)
	}
	if len(u.AnyOf) != 2 {
		t.Errorf("AnyOf len = %d, want 2", len(u.AnyOf))
	}
}

// TestUnionShape_validate verifies that at least one AnyOf variant must match.
func TestUnionShape_validate(t *testing.T) {
	tests := []struct {
		name    string
		anyOf   []*BaseShape
		v       any
		wantErr bool
	}{
		{
			name: "value matches second union member",
			anyOf: []*BaseShape{
				NewLinkedBase(&NumberShape{}, &BaseShape{ID: 2}),
				NewLinkedBase(&StringShape{}, &BaseShape{ID: 3}),
			},
			v: "hello",
		},
		{
			name: "value matches no union member",
			anyOf: []*BaseShape{
				{Shape: &NumberShape{}},
			},
			v:       "not-a-number",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &UnionShape{
				BaseShape:   &BaseShape{ID: 1},
				UnionFacets: UnionFacets{AnyOf: tt.anyOf},
			}
			if err := s.validate(tt.v, ""); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestUnionShape_inherit verifies union member covariance during inheritance.
func TestUnionShape_inherit(t *testing.T) {
	tests := []struct {
		name        string
		targetAnyOf []*BaseShape
		source      Shape
		wantErr     bool
		check       func(Shape)
	}{
		{
			name: "compatible members — intersection kept",
			targetAnyOf: []*BaseShape{
				{ID: 2, Shape: &StringShape{}},
			},
			source: &UnionShape{
				BaseShape: &BaseShape{ID: 3},
				UnionFacets: UnionFacets{
					AnyOf: []*BaseShape{{ID: 4, Shape: &StringShape{}}},
				},
			},
			check: func(got Shape) {
				u := got.(*UnionShape)
				if len(u.AnyOf) != 1 {
					t.Errorf("AnyOf len = %d, want 1", len(u.AnyOf))
				}
			},
		},
		{
			name:        "empty target AnyOf — source takes over",
			targetAnyOf: []*BaseShape{},
			source:      &UnionShape{BaseShape: &BaseShape{}},
		},
		{
			name:    "incompatible source type returns error",
			source:  &StringShape{BaseShape: &BaseShape{ID: 2}},
			wantErr: true,
		},
		{
			name: "no compatible member found",
			targetAnyOf: []*BaseShape{
				{ID: 2, Shape: &StringShape{BaseShape: &BaseShape{}}},
			},
			source: &UnionShape{
				BaseShape: &BaseShape{ID: 3},
				UnionFacets: UnionFacets{
					AnyOf: []*BaseShape{{ID: 4, Shape: &NumberShape{BaseShape: &BaseShape{}}}},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			base := makeTestBase(t, r, "u")
			s := &UnionShape{
				BaseShape:   base,
				UnionFacets: UnionFacets{AnyOf: tt.targetAnyOf},
			}
			got, err := s.inherit(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("inherit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

// TestUnionShape_check verifies structural self-consistency of UnionFacets.
func TestUnionShape_check(t *testing.T) {
	tests := []struct {
		name    string
		anyOf   []*BaseShape
		wantErr bool
	}{
		{
			name:  "all members valid",
			anyOf: []*BaseShape{NewLinkedBase(&StringShape{}, &BaseShape{ID: 2})},
		},
		{
			name: "invalid member shape fails check",
			anyOf: []*BaseShape{
				{
					Shape: &StringShape{
						BaseShape: &BaseShape{KeyPos: stacktrace.Position{}},
						StringFacets: StringFacets{LengthFacets: LengthFacets{
							MinLength: scalarFacetOf(uint64(10)),
							MaxLength: scalarFacetOf(uint64(5)),
						}},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &UnionShape{
				BaseShape:   &BaseShape{ID: 1},
				UnionFacets: UnionFacets{AnyOf: tt.anyOf},
			}
			if err := s.check(); (err != nil) != tt.wantErr {
				t.Errorf("check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestJSONShape_inherit verifies JSON schema merging rules.
func TestJSONShape_inherit(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		source  Shape
		wantErr bool
		check   func(Shape)
	}{
		{
			name:   "identical JSON schemas pass",
			raw:    "{}",
			source: &JSONShape{BaseShape: &BaseShape{ID: 2}, Raw: "{}"},
			check: func(got Shape) {
				if got.(*JSONShape).Raw != "{}" {
					t.Errorf("Raw = %q, want {}", got.(*JSONShape).Raw)
				}
			},
		},
		{
			name:    "source is wrong type",
			raw:     "{}",
			source:  &StringShape{BaseShape: &BaseShape{ID: 2}},
			wantErr: true,
		},
		{
			name:    "differing JSON schemas rejected",
			raw:     "{}",
			source:  &JSONShape{BaseShape: &BaseShape{ID: 2}, Raw: "[]"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &JSONShape{BaseShape: &BaseShape{ID: 1}, Raw: tt.raw}
			got, err := s.inherit(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("inherit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

// TestArrayShape_alias verifies that alias replaces ArrayFacets from source.
func TestArrayShape_alias(t *testing.T) {
	src := &ArrayShape{
		BaseShape: &BaseShape{ID: 2},
		ArrayFacets: ArrayFacets{
			Items:       &BaseShape{ID: 99},
			MinItems:    scalarFacetOf(uint64(2)),
			MaxItems:    scalarFacetOf(uint64(20)),
			UniqueItems: scalarFacetOf(false),
		},
	}

	tests := []struct {
		name    string
		target  *ArrayShape
		source  Shape
		wantErr bool
		check   func(Shape)
	}{
		{
			name:   "all facets aliased from source",
			target: NewTestShape(&ArrayShape{}, 1),
			source: src,
			check: func(got Shape) {
				s := got.(*ArrayShape)
				if s.Items.ID != 99 {
					t.Errorf("Items.ID = %d, want 99", s.Items.ID)
				}
				if s.MinItems.Value != 2 {
					t.Errorf("MinItems.Value = %d, want 2", s.MinItems.Value)
				}
				if s.MaxItems.Value != 20 {
					t.Errorf("MaxItems.Value = %d, want 20", s.MaxItems.Value)
				}
				if s.UniqueItems.Value {
					t.Error("UniqueItems.Value = true, want false")
				}
			},
		},
		{
			name:    "incompatible source type rejected",
			target:  NewTestShape(&ArrayShape{}, 1),
			source:  &ObjectShape{BaseShape: &BaseShape{ID: 2}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.target.alias(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("alias() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

// TestObjectShape_alias verifies that alias replaces all ObjectFacets from source.
func TestObjectShape_alias(t *testing.T) {
	prop := func(id int64) Property {
		return Property{Name: "p", Base: &BaseShape{ID: id}}
	}
	pp := func(id int64) PatternProperty {
		return PatternProperty{Pattern: regexp.MustCompile("^p.*"), Base: &BaseShape{ID: id}}
	}
	src := &ObjectShape{
		BaseShape: &BaseShape{ID: 99},
		ObjectFacets: ObjectFacets{
			Properties:           newPropsMap(propEntry{"p", prop(10)}),
			PatternProperties:    newPatternPropsMap(patternPropEntry{"/^p.*/", pp(11)}),
			MinProperties:        scalarFacetOf(uint64(2)),
			MaxProperties:        scalarFacetOf(uint64(20)),
			AdditionalProperties: scalarFacetOf(false),
			Discriminator:        scalarFacetOf("kind"),
			DiscriminatorValue:   &DataNode{Value: NewScalarNodeValue("cat")},
		},
	}

	tests := []struct {
		name    string
		target  *ObjectShape
		source  Shape
		wantErr bool
		check   func(Shape)
	}{
		{
			name:   "all facets aliased from source",
			target: &ObjectShape{BaseShape: &BaseShape{ID: 1}, ObjectFacets: ObjectFacets{Properties: newPropsMap(propEntry{"p", prop(1)})}},
			source: src,
			check: func(got Shape) {
				s := got.(*ObjectShape)
				if s.MinProperties.Value != 2 {
					t.Errorf("MinProperties = %d, want 2", s.MinProperties.Value)
				}
				if s.MaxProperties.Value != 20 {
					t.Errorf("MaxProperties = %d, want 20", s.MaxProperties.Value)
				}
				if s.AdditionalProperties.Value {
					t.Error("AdditionalProperties = true, want false")
				}
				if s.Discriminator.Value != "kind" {
					t.Errorf("Discriminator = %q, want kind", s.Discriminator.Value)
				}
				if s.DiscriminatorValue.Value.Raw != "cat" {
					t.Errorf("DiscriminatorValue = %v, want cat", s.DiscriminatorValue.Value.Raw)
				}
			},
		},
		{
			name:    "incompatible source type rejected",
			target:  &ObjectShape{BaseShape: &BaseShape{ID: 1}},
			source:  &ArrayShape{BaseShape: &BaseShape{ID: 2}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.target.alias(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("alias() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

// TestUnionShape_alias verifies that alias replaces AnyOf from source.
func TestUnionShape_alias(t *testing.T) {
	tests := []struct {
		name    string
		target  *UnionShape
		source  Shape
		wantErr bool
		check   func(Shape)
	}{
		{
			name: "AnyOf replaced from source",
			target: &UnionShape{
				BaseShape:   &BaseShape{ID: 1},
				UnionFacets: UnionFacets{AnyOf: []*BaseShape{{ID: 2, Shape: &StringShape{}}}},
			},
			source: &UnionShape{
				BaseShape:   &BaseShape{ID: 3},
				UnionFacets: UnionFacets{AnyOf: []*BaseShape{{ID: 4, Shape: &NumberShape{}}}},
			},
			check: func(got Shape) {
				u := got.(*UnionShape)
				if len(u.AnyOf) != 1 || u.AnyOf[0].ID != 4 {
					t.Errorf("AnyOf[0].ID = %d, want 4", u.AnyOf[0].ID)
				}
			},
		},
		{
			name: "incompatible source type rejected",
			target: &UnionShape{
				BaseShape: &BaseShape{ID: 1},
			},
			source:  &StringShape{BaseShape: &BaseShape{ID: 2}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.target.alias(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("alias() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

// TestJSONShape_alias verifies that alias replaces the Raw JSON content from source.
func TestJSONShape_alias(t *testing.T) {
	tests := []struct {
		name    string
		target  *JSONShape
		source  Shape
		wantErr bool
		check   func(Shape)
	}{
		{
			name:   "Raw replaced from source",
			target: &JSONShape{BaseShape: &BaseShape{ID: 1}, Raw: "{}"},
			source: &JSONShape{BaseShape: &BaseShape{ID: 2}, Raw: `{"type":"string"}`},
			check: func(got Shape) {
				if got.(*JSONShape).Raw != `{"type":"string"}` {
					t.Errorf("Raw = %q, want {\"type\":\"string\"}", got.(*JSONShape).Raw)
				}
			},
		},
		{
			name:    "incompatible source type rejected",
			target:  &JSONShape{BaseShape: &BaseShape{ID: 1}, Raw: "{}"},
			source:  &StringShape{BaseShape: &BaseShape{ID: 2}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.target.alias(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("alias() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}

// TestUnknownShape_alias verifies that UnknownShape always errors on alias.
func TestUnknownShape_alias(t *testing.T) {
	s := &UnknownShape{BaseShape: &BaseShape{ID: 1}, facets: []*yaml.Node{}}
	src := &UnknownShape{BaseShape: &BaseShape{ID: 2}, facets: []*yaml.Node{}}
	if _, err := s.alias(src); err == nil {
		t.Error("alias() error = nil, want non-nil")
	}
}

// TestRecursiveShape_alias verifies that alias replaces the Head pointer.
func TestRecursiveShape_alias(t *testing.T) {
	tests := []struct {
		name    string
		target  *RecursiveShape
		source  Shape
		wantErr bool
		check   func(Shape)
	}{
		{
			name:   "Head replaced from source",
			target: &RecursiveShape{BaseShape: &BaseShape{ID: 1}, Head: &BaseShape{ID: 2}},
			source: &RecursiveShape{BaseShape: &BaseShape{ID: 3}, Head: &BaseShape{ID: 4}},
			check: func(got Shape) {
				if got.(*RecursiveShape).Head.ID != 4 {
					t.Errorf("Head.ID = %d, want 4", got.(*RecursiveShape).Head.ID)
				}
			},
		},
		{
			name:    "incompatible source type rejected",
			target:  &RecursiveShape{BaseShape: &BaseShape{ID: 1}, Head: &BaseShape{ID: 2}},
			source:  &StringShape{BaseShape: &BaseShape{ID: 3}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.target.alias(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("alias() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}
