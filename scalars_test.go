package raml

import (
	"math/big"
	"reflect"
	"regexp"
	"testing"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

// makeSource links Base().Shape on src and returns src. Used in Inherit tests.
func makeSource(src Shape) Shape {
	src.Base().Shape = src
	return src
}

// baseWith returns a BaseShape initialized with CustomShapeFacets. If withRAML
// is true the internal raml pointer is set (required by facets that call makeDataNode).
func baseWith(t *testing.T, withRAML bool) *BaseShape {
	b := &BaseShape{CustomShapeFacets: orderedmap.New[string, *DataNode](0)}
	if withRAML {
		b.raml = makeTestRAML(t)
	}
	return b
}

// ── MakeEnum / isCompatibleEnum ──────────────────────────────────────────────

func TestRAML_MakeEnum(t *testing.T) {
	r := makeTestRAML(t)
	tests := []struct {
		name    string
		node    *yaml.Node
		wantLen int
		wantErr bool
	}{
		{
			name: "valid sequence",
			node: &yaml.Node{
				Kind: yaml.SequenceNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "v1"},
					{Kind: yaml.ScalarNode, Value: "v2"},
				},
			},
			wantLen: 2,
		},
		{
			name:    "non-sequence node",
			node:    &yaml.Node{Kind: yaml.MappingNode},
			wantErr: true,
		},
		{
			name: "invalid item in sequence",
			node: &yaml.Node{
				Kind:    yaml.SequenceNode,
				Content: []*yaml.Node{{Kind: yaml.SequenceNode, Value: "{"}},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.MakeEnum(tt.node, "test.raml")
			if (err != nil) != tt.wantErr {
				t.Fatalf("MakeEnum() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(got) != tt.wantLen {
				t.Errorf("MakeEnum() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func Test_isCompatibleEnum(t *testing.T) {
	n := func(vals ...string) Nodes {
		out := make(Nodes, len(vals))
		for i, v := range vals {
			out[i] = &DataNode{Value: NewScalarNodeValue(v)}
		}
		return out
	}
	tests := []struct {
		name   string
		source Nodes
		target Nodes
		want   bool
	}{
		{"target subset of source", n("a", "b", "c"), n("a", "b"), true},
		{"target not subset of source", n("a", "b"), n("a", "d"), false},
		{"empty target", n("a", "b"), Nodes{}, true},
		{"empty source non-empty target", Nodes{}, n("a"), false},
		{"both empty", Nodes{}, Nodes{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCompatibleEnum(tt.source, tt.target); got != tt.want {
				t.Errorf("isCompatibleEnum() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBaseShape_validateEnum(t *testing.T) {
	n := func(vals ...any) Nodes {
		out := make(Nodes, len(vals))
		for i, v := range vals {
			out[i] = &DataNode{Value: NewScalarNodeValue(v)}
		}
		return out
	}
	tests := []struct {
		name    string
		enum    Nodes
		value   any
		wantErr bool
	}{
		// string matching
		{"string match", n("a", "b", "c"), "b", false},
		{"string no match", n("a", "b", "c"), "d", true},
		// integer matching
		{"int match", n(1, 2, 3), 2, false},
		{"int no match", n(1, 2, 3), 4, true},
		// numeric cross-type: int enum value equals float64 input
		{"int enum float64 value equal", n(1, 2), float64(1), false},
		{"float64 enum int value equal", n(float64(1.0), float64(2.0)), 1, false},
		// bool matching
		{"bool true match", n(true, false), true, false},
		{"bool false match", n(true, false), false, false},
		{"bool no match", n(true), false, true},
		// nil matching
		{"nil match", Nodes{{Value: NewScalarNodeValue(nil)}}, nil, false},
		{"nil no match", n("a"), nil, true},
		// slice matching
		{
			"slice match",
			Nodes{{Value: anyToNodeValue([]any{"x", "y"})}},
			[]any{"x", "y"},
			false,
		},
		{
			"slice element mismatch",
			Nodes{{Value: anyToNodeValue([]any{"x", "y"})}},
			[]any{"x", "z"},
			true,
		},
		{
			"slice length mismatch",
			Nodes{{Value: anyToNodeValue([]any{"x"})}},
			[]any{"x", "y"},
			true,
		},
		// map matching
		{
			"map match",
			Nodes{{Value: anyToNodeValue(map[string]any{"a": 1})}},
			map[string]any{"a": 1},
			false,
		},
		{
			"map value mismatch",
			Nodes{{Value: anyToNodeValue(map[string]any{"a": 1})}},
			map[string]any{"a": 2},
			true,
		},
		{
			"map key mismatch",
			Nodes{{Value: anyToNodeValue(map[string]any{"a": 1})}},
			map[string]any{"b": 1},
			true,
		},
		{
			"map size mismatch",
			Nodes{{Value: anyToNodeValue(map[string]any{"a": 1})}},
			map[string]any{"a": 1, "b": 2},
			true,
		},
		// single-element enum
		{"single element match", n("only"), "only", false},
		{"single element no match", n("only"), "other", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &BaseShape{Enum: tt.enum}
			err := s.validateEnum(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEnum() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ── IntegerShape ─────────────────────────────────────────────────────────────

func TestIntegerShape_Validate(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		facets  IntegerFacets
		v       any
		wantErr bool
	}{
		{"valid int", &BaseShape{}, IntegerFacets{}, 42, false},
		{"valid float whole", &BaseShape{}, IntegerFacets{}, 123.0, false},
		{"valid uint", &BaseShape{}, IntegerFacets{}, uint(5), false},
		{"invalid string", &BaseShape{}, IntegerFacets{}, "123", true},
		{"invalid bool", &BaseShape{}, IntegerFacets{}, true, true},
		{"invalid nil", &BaseShape{}, IntegerFacets{}, nil, true},
		{
			"below minimum",
			&BaseShape{}, IntegerFacets{Minimum: scalarFacetOf(big.NewInt(100))},
			5, true,
		},
		{
			"above maximum",
			&BaseShape{}, IntegerFacets{Maximum: scalarFacetOf(big.NewInt(100))},
			150, true,
		},
		{
			"enum match",
			&BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(1)}, {Value: NewScalarNodeValue(uint64(2))}}},
			IntegerFacets{},
			1, false,
		},
		{
			"enum miss",
			&BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(1)}, {Value: NewScalarNodeValue(uint64(2))}}},
			IntegerFacets{},
			3, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewTestShapeWithBase(&IntegerShape{IntegerFacets: tt.facets}, tt.base)
			if err := s.BaseShape.Validate(tt.v); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIntegerShape_Inherit(t *testing.T) {
	tests := []struct {
		name    string
		shape   *IntegerShape
		source  Shape
		wantErr bool
	}{
		{
			"same type",
			NewTestShape(&IntegerShape{}, 1),
			makeSource(&IntegerShape{BaseShape: &BaseShape{}}),
			false,
		},
		{
			"incompatible type",
			NewTestShape(&IntegerShape{}, 2),
			makeSource(&StringShape{BaseShape: &BaseShape{}}),
			true,
		},
		{
			"minimum less restrictive in source",
			NewTestShapeWithBase(&IntegerShape{IntegerFacets: IntegerFacets{Minimum: scalarFacetOf(big.NewInt(100))}}, &BaseShape{}),
			makeSource(&IntegerShape{BaseShape: &BaseShape{}, IntegerFacets: IntegerFacets{Minimum: scalarFacetOf(big.NewInt(120))}}),
			true,
		},
		{
			"maximum less restrictive in source",
			NewTestShapeWithBase(&IntegerShape{IntegerFacets: IntegerFacets{Maximum: scalarFacetOf(big.NewInt(100))}}, &BaseShape{}),
			makeSource(&IntegerShape{BaseShape: &BaseShape{}, IntegerFacets: IntegerFacets{Maximum: scalarFacetOf(big.NewInt(80))}}),
			true,
		},
		{
			"enum in source not subset of base",
			NewTestShapeWithBase(&IntegerShape{}, &BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(1)}, {Value: NewScalarNodeValue(uint64(2))}}}),
			makeSource(&IntegerShape{BaseShape: &BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(3)}}}}),
			true,
		},
		{
			"format mismatch",
			NewTestShapeWithBase(&IntegerShape{FormatFacets: FormatFacets{Format: scalarFacetOf("int32")}}, &BaseShape{}),
			makeSource(&IntegerShape{BaseShape: &BaseShape{}, FormatFacets: FormatFacets{Format: scalarFacetOf("int64")}}),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.shape.BaseShape.Inherit(tt.source.Base()); (err != nil) != tt.wantErr {
				t.Errorf("Inherit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIntegerShape_Check(t *testing.T) {
	bigInt := func(s string) *big.Int { n, _ := new(big.Int).SetString(s, 10); return n }
	tests := []struct {
		name    string
		shape   *IntegerShape
		wantErr bool
	}{
		{
			"valid",
			&IntegerShape{BaseShape: &BaseShape{}},
			false,
		},
		{
			"minimum greater than maximum",
			&IntegerShape{BaseShape: &BaseShape{}, IntegerFacets: IntegerFacets{
				Minimum: scalarFacetOf(bigInt("100")),
				Maximum: scalarFacetOf(bigInt("50")),
			}},
			true,
		},
		{
			"valid format",
			&IntegerShape{BaseShape: &BaseShape{}, FormatFacets: FormatFacets{Format: scalarFacetOf("int32")}},
			false,
		},
		{
			"invalid format",
			&IntegerShape{BaseShape: &BaseShape{}, FormatFacets: FormatFacets{Format: scalarFacetOf("invalid")}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.shape.check(); (err != nil) != tt.wantErr {
				t.Errorf("check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIntegerShape_unmarshalYAMLNodes(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		nodes   []*yaml.Node
		wantErr bool
	}{
		{
			"empty nodes",
			baseWith(t, false),
			nil, false,
		},
		{
			"custom facet key-value",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "key1"}, {Kind: yaml.ScalarNode, Value: "value1"}},
			false,
		},
		{
			"invalid custom facet value",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.MappingNode, Value: "{"}, {Kind: yaml.MappingNode, Value: "{"}},
			true,
		},
		{
			"minimum valid",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "minimum"}, {Kind: yaml.ScalarNode, Value: "10", Tag: "!!int"}},
			false,
		},
		{
			"minimum invalid value",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "minimum"}, {Kind: yaml.ScalarNode, Value: "invalid", Tag: "!!int"}},
			true,
		},
		{
			"maximum valid",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "maximum"}, {Kind: yaml.ScalarNode, Value: "100", Tag: "!!int"}},
			false,
		},
		{
			"maximum invalid value",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "maximum"}, {Kind: yaml.ScalarNode, Value: "invalid", Tag: "!!int"}},
			true,
		},
		{
			"multipleOf valid",
			baseWith(t, true),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "multipleOf"}, {Kind: yaml.ScalarNode, Value: "5"}},
			false,
		},
		{
			"multipleOf invalid",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "multipleOf"}, {Kind: yaml.ScalarNode, Value: "invalid"}},
			true,
		},
		{
			"format valid",
			baseWith(t, true),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "format"}, {Kind: yaml.ScalarNode, Value: "int32"}},
			false,
		},
		{
			"format invalid",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "format"}, {Kind: yaml.ScalarNode, Value: "invalid"}},
			true,
		},
		{
			"enum valid sequence",
			baseWith(t, false),
			[]*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "enum"},
				{Kind: yaml.SequenceNode, Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "1"},
					{Kind: yaml.ScalarNode, Value: "2"},
				}},
			},
			false,
		},
		{
			// enum is handled at BaseShape.decodeValueNode; non-sequence falls through to custom facets
			"enum non-sequence falls through",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "enum"}, {Kind: yaml.ScalarNode, Value: "invalid"}},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &IntegerShape{BaseShape: tt.base}
			if err := s.unmarshalYAMLNodes(tt.nodes); (err != nil) != tt.wantErr {
				t.Errorf("unmarshalYAMLNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIntegerShape_Alias(t *testing.T) {
	tests := []struct {
		name    string
		source  Shape
		wantErr bool
	}{
		{
			"same type with facets",
			&IntegerShape{
				BaseShape:     &BaseShape{Type: "integer"},
				IntegerFacets: IntegerFacets{Minimum: scalarFacetOf(big.NewInt(1)), Maximum: scalarFacetOf(big.NewInt(10))},
			},
			false,
		},
		{"incompatible type", &StringShape{BaseShape: &BaseShape{Type: "string"}}, true},
	}
	s := &IntegerShape{
		BaseShape:     &BaseShape{Type: "integer"},
		IntegerFacets: IntegerFacets{Minimum: scalarFacetOf(big.NewInt(1)), Maximum: scalarFacetOf(big.NewInt(10))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.alias(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("alias() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.source) {
				t.Errorf("alias() = %v, want %v", got, tt.source)
			}
		})
	}
}

// ── NumberShape ──────────────────────────────────────────────────────────────

func TestNumberShape_Validate(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		facets  NumberFacets
		v       any
		wantErr bool
	}{
		{"valid float64", &BaseShape{}, NumberFacets{}, 123.45, false},
		{"valid int", &BaseShape{}, NumberFacets{}, 123, false},
		{"valid uint zero", &BaseShape{}, NumberFacets{}, uint(0), false},
		{"invalid string", &BaseShape{}, NumberFacets{}, "x", true},
		{"invalid bool", &BaseShape{}, NumberFacets{}, true, true},
		{"invalid nil", &BaseShape{}, NumberFacets{}, nil, true},
		{
			"below minimum",
			&BaseShape{}, NumberFacets{Minimum: scalarFacetOf(ratOf("100"))},
			5.0, true,
		},
		{
			"above maximum",
			&BaseShape{}, NumberFacets{Maximum: scalarFacetOf(ratOf("100"))},
			150.0, true,
		},
		{
			"enum match",
			&BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(1.0)}, {Value: NewScalarNodeValue(2.0)}}},
			NumberFacets{},
			1.0, false,
		},
		{
			"enum miss",
			&BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(1.0)}, {Value: NewScalarNodeValue(2.0)}}},
			NumberFacets{},
			3.0, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewTestShapeWithBase(&NumberShape{NumberFacets: tt.facets}, tt.base)
			if err := s.BaseShape.Validate(tt.v); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNumberShape_Inherit(t *testing.T) {
	tests := []struct {
		name    string
		shape   *NumberShape
		source  Shape
		wantErr bool
	}{
		{
			"same type",
			NewTestShape(&NumberShape{}, 1),
			makeSource(&NumberShape{BaseShape: &BaseShape{}}),
			false,
		},
		{
			"incompatible type",
			NewTestShape(&NumberShape{}, 2),
			makeSource(&IntegerShape{BaseShape: &BaseShape{}}),
			true,
		},
		{
			"minimum less restrictive in source",
			NewTestShapeWithBase(&NumberShape{NumberFacets: NumberFacets{Minimum: scalarFacetOf(ratOf("5"))}}, &BaseShape{}),
			makeSource(&NumberShape{BaseShape: &BaseShape{}, NumberFacets: NumberFacets{Minimum: scalarFacetOf(ratOf("10"))}}),
			true,
		},
		{
			"maximum less restrictive in source",
			NewTestShapeWithBase(&NumberShape{NumberFacets: NumberFacets{Maximum: scalarFacetOf(ratOf("150"))}}, &BaseShape{}),
			makeSource(&NumberShape{BaseShape: &BaseShape{}, NumberFacets: NumberFacets{Maximum: scalarFacetOf(ratOf("100"))}}),
			true,
		},
		{
			"enum in source not subset of base",
			NewTestShapeWithBase(&NumberShape{}, &BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(1.0)}, {Value: NewScalarNodeValue(2.0)}}}),
			makeSource(&NumberShape{BaseShape: &BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(3.0)}}}}),
			true,
		},
		{
			"format mismatch",
			NewTestShapeWithBase(&NumberShape{FormatFacets: FormatFacets{Format: scalarFacetOf("float")}}, &BaseShape{}),
			makeSource(&NumberShape{BaseShape: &BaseShape{}, FormatFacets: FormatFacets{Format: scalarFacetOf("double")}}),
			true,
		},
		{
			"multipleOf mismatch",
			NewTestShapeWithBase(&NumberShape{NumberFacets: NumberFacets{MultipleOf: scalarFacetOf(ratOf("2"))}}, &BaseShape{}),
			makeSource(&NumberShape{BaseShape: &BaseShape{}, NumberFacets: NumberFacets{MultipleOf: scalarFacetOf(ratOf("2"))}}),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.shape.BaseShape.Inherit(tt.source.Base()); (err != nil) != tt.wantErr {
				t.Errorf("Inherit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNumberShape_Check(t *testing.T) {
	tests := []struct {
		name    string
		shape   *NumberShape
		wantErr bool
	}{
		{
			"valid min<=max",
			&NumberShape{BaseShape: &BaseShape{}, NumberFacets: NumberFacets{
				Minimum: scalarFacetOf(ratOf("1")),
				Maximum: scalarFacetOf(ratOf("1")),
			}},
			false,
		},
		{
			"minimum greater than maximum",
			&NumberShape{BaseShape: &BaseShape{}, NumberFacets: NumberFacets{
				Minimum: scalarFacetOf(ratOf("2")),
				Maximum: scalarFacetOf(ratOf("1")),
			}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.shape.check(); (err != nil) != tt.wantErr {
				t.Errorf("check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNumberShape_unmarshalYAMLNodes(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		nodes   []*yaml.Node
		wantErr bool
	}{
		{
			"empty nodes",
			baseWith(t, false),
			nil, false,
		},
		{
			"custom facet",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "key1"}, {Kind: yaml.ScalarNode, Value: "value1"}},
			false,
		},
		{
			"invalid value node",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "key1"}, {Kind: yaml.MappingNode, Value: "{"}},
			true,
		},
		{
			"minimum valid",
			baseWith(t, true),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "minimum"}, {Kind: yaml.ScalarNode, Value: "1.0"}},
			false,
		},
		{
			"minimum invalid",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "minimum"}, {Kind: yaml.ScalarNode, Value: "invalid"}},
			true,
		},
		{
			"maximum valid",
			baseWith(t, true),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "maximum"}, {Kind: yaml.ScalarNode, Value: "10.0"}},
			false,
		},
		{
			"maximum invalid",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "maximum"}, {Kind: yaml.ScalarNode, Value: "invalid"}},
			true,
		},
		{
			"multipleOf valid",
			baseWith(t, true),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "multipleOf"}, {Kind: yaml.ScalarNode, Value: "2.0"}},
			false,
		},
		{
			"multipleOf invalid",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "multipleOf"}, {Kind: yaml.ScalarNode, Value: "invalid"}},
			true,
		},
		{
			"format valid",
			baseWith(t, true),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "format"}, {Kind: yaml.ScalarNode, Value: "double"}},
			false,
		},
		{
			"format invalid",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "format"}, {Kind: yaml.ScalarNode, Value: "invalid"}},
			true,
		},
		{
			// enum non-sequence falls through to custom facets — no error at this level
			"enum non-sequence falls through",
			baseWith(t, false),
			[]*yaml.Node{{Kind: yaml.ScalarNode, Value: "enum"}, {Kind: yaml.ScalarNode, Value: "invalid"}},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &NumberShape{BaseShape: tt.base}
			if err := s.unmarshalYAMLNodes(tt.nodes); (err != nil) != tt.wantErr {
				t.Errorf("unmarshalYAMLNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ── StringShape ──────────────────────────────────────────────────────────────

func TestStringShape_Validate(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		facets  StringFacets
		v       any
		wantErr bool
	}{
		{"valid string", &BaseShape{}, StringFacets{}, "hello", false},
		{"invalid non-string", &BaseShape{}, StringFacets{}, 123, true},
		{
			"enum match",
			&BaseShape{Enum: Nodes{{Value: NewScalarNodeValue("ok")}}},
			StringFacets{},
			"ok", false,
		},
		{
			"enum miss",
			&BaseShape{Enum: Nodes{{Value: NewScalarNodeValue("ok")}}},
			StringFacets{},
			"bad", true,
		},
		{
			"pattern match",
			&BaseShape{}, StringFacets{Pattern: scalarFacetOf(regexp.MustCompile(`^[a-z]+$`))},
			"abc", false,
		},
		{
			"pattern miss",
			&BaseShape{}, StringFacets{Pattern: scalarFacetOf(regexp.MustCompile(`^[a-z]+$`))},
			"ABC", true,
		},
		{
			"below minLength",
			&BaseShape{}, StringFacets{LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(6))}},
			"short", true,
		},
		{
			"above maxLength",
			&BaseShape{}, StringFacets{LengthFacets: LengthFacets{MaxLength: scalarFacetOf(uint64(4))}},
			"too long", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewTestShapeWithBase(&StringShape{StringFacets: tt.facets}, tt.base)
			if err := s.BaseShape.Validate(tt.v); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStringShape_Inherit(t *testing.T) {
	tests := []struct {
		name    string
		shape   *StringShape
		source  Shape
		wantErr bool
	}{
		{
			"same type",
			NewTestShapeWithBase(&StringShape{}, &BaseShape{Type: "string"}),
			makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}}),
			false,
		},
		{
			"incompatible type",
			NewTestShapeWithBase(&StringShape{}, &BaseShape{Type: "string"}),
			makeSource(&NumberShape{BaseShape: &BaseShape{Type: "number"}}),
			true,
		},
		{
			"minLength less restrictive in source",
			NewTestShapeWithBase(&StringShape{StringFacets: StringFacets{LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(2))}}}, &BaseShape{}),
			makeSource(&StringShape{BaseShape: &BaseShape{}, StringFacets: StringFacets{LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(4))}}}),
			true,
		},
		{
			"maxLength less restrictive in source",
			NewTestShapeWithBase(&StringShape{StringFacets: StringFacets{LengthFacets: LengthFacets{MaxLength: scalarFacetOf(uint64(4))}}}, &BaseShape{}),
			makeSource(&StringShape{BaseShape: &BaseShape{}, StringFacets: StringFacets{LengthFacets: LengthFacets{MaxLength: scalarFacetOf(uint64(2))}}}),
			true,
		},
		{
			"enum in source not subset of base",
			NewTestShapeWithBase(&StringShape{}, &BaseShape{Enum: Nodes{{Value: NewScalarNodeValue("ok")}}}),
			makeSource(&StringShape{BaseShape: &BaseShape{Type: "string", Enum: Nodes{{Value: NewScalarNodeValue("bad")}}}}),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.shape.BaseShape.Inherit(tt.source.Base()); (err != nil) != tt.wantErr {
				t.Errorf("Inherit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStringShape_Check(t *testing.T) {
	tests := []struct {
		name    string
		shape   *StringShape
		wantErr bool
	}{
		{
			"valid",
			&StringShape{BaseShape: &BaseShape{}},
			false,
		},
		{
			"minLength greater than maxLength",
			&StringShape{BaseShape: &BaseShape{}, StringFacets: StringFacets{LengthFacets: LengthFacets{
				MinLength: scalarFacetOf(uint64(5)),
				MaxLength: scalarFacetOf(uint64(4)),
			}}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.shape.check(); (err != nil) != tt.wantErr {
				t.Errorf("check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStringShape_unmarshalYAMLNodes(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		nodes   []*yaml.Node
		wantErr bool
	}{
		{
			"empty nodes",
			&BaseShape{},
			nil, false,
		},
		{
			"minLength valid",
			baseWith(t, true),
			[]*yaml.Node{{Value: "minLength"}, {Value: "1", Kind: yaml.ScalarNode, Tag: "!!float"}},
			false,
		},
		{
			"minLength invalid",
			baseWith(t, false),
			[]*yaml.Node{{Value: "minLength"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!float"}},
			true,
		},
		{
			"maxLength valid",
			baseWith(t, true),
			[]*yaml.Node{{Value: "maxLength"}, {Value: "1", Kind: yaml.ScalarNode, Tag: "!!float"}},
			false,
		},
		{
			"maxLength invalid",
			baseWith(t, false),
			[]*yaml.Node{{Value: "maxLength"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!float"}},
			true,
		},
		{
			"pattern valid",
			baseWith(t, false),
			[]*yaml.Node{{Value: "pattern"}, {Value: "[a-z]", Kind: yaml.ScalarNode, Tag: "!!str"}},
			false,
		},
		{
			"pattern invalid regex",
			baseWith(t, false),
			[]*yaml.Node{{Value: "pattern"}, {Value: "?$", Kind: yaml.ScalarNode, Tag: "!!str"}},
			true,
		},
		{
			"pattern invalid tag",
			baseWith(t, false),
			[]*yaml.Node{{Value: "pattern"}, {Value: "[a-z]", Kind: yaml.ScalarNode, Tag: "!!float"}},
			true,
		},
		{
			"invalid enum value node",
			baseWith(t, false),
			[]*yaml.Node{{Value: "enum"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!float"}},
			true,
		},
		{
			"unknown facet with invalid value",
			baseWith(t, false),
			[]*yaml.Node{{Value: "unknown"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!float"}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &StringShape{BaseShape: tt.base}
			if err := s.unmarshalYAMLNodes(tt.nodes); (err != nil) != tt.wantErr {
				t.Errorf("unmarshalYAMLNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStringShape_Alias(t *testing.T) {
	srcSame := &StringShape{
		BaseShape: &BaseShape{Type: "string"},
		StringFacets: StringFacets{
			LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(1)), MaxLength: scalarFacetOf(uint64(10))},
			Pattern:      scalarFacetOf(regexp.MustCompile(`^[a-zA-Z]+$`)),
		},
	}
	s := &StringShape{
		BaseShape: &BaseShape{Type: "string"},
		StringFacets: StringFacets{
			LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(1)), MaxLength: scalarFacetOf(uint64(10))},
			Pattern:      scalarFacetOf(regexp.MustCompile(`^[a-zA-Z]+$`)),
		},
	}
	t.Run("same type", func(t *testing.T) {
		got, err := s.alias(srcSame)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, srcSame) {
			t.Errorf("alias() = %v, want %v", got, srcSame)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.alias(&IntegerShape{BaseShape: &BaseShape{Type: "integer"}}); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

// ── FileShape ─────────────────────────────────────────────────────────────────

func TestFileShape_Validate(t *testing.T) {
	tests := []struct {
		name    string
		facets  LengthFacets
		v       any
		wantErr bool
	}{
		{"valid string", LengthFacets{}, "file_content", false},
		{"invalid non-string", LengthFacets{}, 123, true},
		{
			"above maxLength",
			LengthFacets{MaxLength: scalarFacetOf(uint64(5))},
			"valid_file", true,
		},
		{
			"below minLength",
			LengthFacets{MinLength: scalarFacetOf(uint64(5))},
			"v", true,
		},
		{
			"exact length match",
			LengthFacets{MinLength: scalarFacetOf(uint64(10)), MaxLength: scalarFacetOf(uint64(10))},
			"valid_file", false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &FileShape{BaseShape: &BaseShape{}, LengthFacets: tt.facets}
			if err := s.validate(tt.v, ""); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFileShape_Inherit(t *testing.T) {
	tests := []struct {
		name    string
		shape   *FileShape
		source  Shape
		wantErr bool
	}{
		{
			"same type",
			&FileShape{BaseShape: &BaseShape{Type: "file"}},
			makeSource(&FileShape{BaseShape: &BaseShape{Type: "file"}}),
			false,
		},
		{
			"incompatible type",
			&FileShape{BaseShape: &BaseShape{Type: "file"}},
			makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}}),
			true,
		},
		{
			"minLength less restrictive in source",
			&FileShape{BaseShape: &BaseShape{}, LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(5))}},
			makeSource(&FileShape{BaseShape: &BaseShape{}, LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(6))}}),
			true,
		},
		{
			"maxLength less restrictive in source",
			&FileShape{BaseShape: &BaseShape{}, LengthFacets: LengthFacets{MaxLength: scalarFacetOf(uint64(5))}},
			makeSource(&FileShape{BaseShape: &BaseShape{}, LengthFacets: LengthFacets{MaxLength: scalarFacetOf(uint64(3))}}),
			true,
		},
		{
			"incompatible fileTypes",
			&FileShape{BaseShape: &BaseShape{}, FileFacets: FileFacets{FileTypes: []*Node[string]{{Value: "image/png"}}}},
			makeSource(&FileShape{BaseShape: &BaseShape{}, FileFacets: FileFacets{FileTypes: []*Node[string]{{Value: "image/jpeg"}}}}),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.shape.BaseShape.Shape = tt.shape
			_, err := tt.shape.inherit(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("inherit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFileShape_Check(t *testing.T) {
	tests := []struct {
		name    string
		shape   *FileShape
		wantErr bool
	}{
		{
			"valid",
			&FileShape{
				BaseShape:    &BaseShape{},
				LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(5)), MaxLength: scalarFacetOf(uint64(6))},
				FileFacets:   FileFacets{FileTypes: []*Node[string]{{Value: "image/png"}}},
			},
			false,
		},
		{
			"minLength greater than maxLength",
			&FileShape{
				BaseShape:    &BaseShape{},
				LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(5)), MaxLength: scalarFacetOf(uint64(4))},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.shape.check(); (err != nil) != tt.wantErr {
				t.Errorf("check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFileShape_unmarshalYAMLNodes(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		nodes   []*yaml.Node
		wantErr bool
	}{
		{
			"all valid facets",
			baseWith(t, true),
			[]*yaml.Node{
				{Value: "minLength"}, {Value: "1", Kind: yaml.ScalarNode, Tag: "!!float"},
				{Value: "maxLength"}, {Value: "10", Kind: yaml.ScalarNode, Tag: "!!float"},
				{Value: "fileTypes"}, {Kind: yaml.SequenceNode, Content: []*yaml.Node{
					{Value: "image/png", Kind: yaml.ScalarNode, Tag: "!!str"},
				}},
				{Value: "custom", Kind: yaml.ScalarNode, Tag: "!!str"},
				{Kind: yaml.MappingNode, Content: []*yaml.Node{
					{Value: "key", Kind: yaml.ScalarNode, Tag: "!!str"},
					{Value: "value", Kind: yaml.ScalarNode, Tag: "!!str"},
				}},
			},
			false,
		},
		{
			"minLength invalid",
			&BaseShape{},
			[]*yaml.Node{{Value: "minLength"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!float"}},
			true,
		},
		{
			"maxLength invalid",
			&BaseShape{},
			[]*yaml.Node{{Value: "maxLength"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!float"}},
			true,
		},
		{
			"fileTypes must be sequence",
			baseWith(t, false),
			[]*yaml.Node{{Value: "fileTypes"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!str"}},
			true,
		},
		{
			"fileTypes item must be string tag",
			baseWith(t, false),
			[]*yaml.Node{{Value: "fileTypes"}, {Kind: yaml.SequenceNode, Content: []*yaml.Node{
				{Value: "image/png", Kind: yaml.ScalarNode, Tag: "!!float"},
			}}},
			true,
		},
		{
			"fileTypes item invalid node",
			baseWith(t, false),
			[]*yaml.Node{{Value: "fileTypes"}, {Kind: yaml.SequenceNode, Content: []*yaml.Node{
				{Value: "1.5", Kind: yaml.ScalarNode, Tag: "!!float"},
			}}},
			true,
		},
		{
			"unknown facet with invalid value",
			baseWith(t, false),
			[]*yaml.Node{{Value: "unknown"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!float"}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &FileShape{BaseShape: tt.base}
			if err := s.unmarshalYAMLNodes(tt.nodes); (err != nil) != tt.wantErr {
				t.Errorf("unmarshalYAMLNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFileShape_Alias(t *testing.T) {
	src := &FileShape{
		BaseShape:    &BaseShape{Type: "file"},
		FileFacets:   FileFacets{FileTypes: []*Node[string]{{Value: "image/png"}, {Value: "image/jpeg"}}},
		LengthFacets: LengthFacets{MinLength: scalarFacetOf(uint64(1)), MaxLength: scalarFacetOf(uint64(10))},
	}
	s := &FileShape{BaseShape: &BaseShape{Type: "file"}}
	t.Run("same type", func(t *testing.T) {
		got, err := s.alias(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, src) {
			t.Errorf("alias() = %v, want %v", got, src)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.alias(&StringShape{BaseShape: &BaseShape{Type: "string"}}); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

// ── BooleanShape ─────────────────────────────────────────────────────────────

func TestBooleanShape_Validate(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		v       any
		wantErr bool
	}{
		{
			"valid bool in enum",
			&BaseShape{Enum: Nodes{{Value: NewScalarNodeValue(true)}}},
			true, false,
		},
		{"invalid non-bool", &BaseShape{}, 123, true},
		{
			"enum miss",
			&BaseShape{Enum: Nodes{{Value: NewScalarNodeValue("true")}}},
			false, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewTestShapeWithBase(&BooleanShape{}, tt.base)
			if err := s.BaseShape.Validate(tt.v); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBooleanShape_Inherit(t *testing.T) {
	tests := []struct {
		name        string
		shape       *BooleanShape
		source      Shape
		wantErr     bool
		wantEnumLen int // 0 = don't check
	}{
		{
			"inherits enum from source when base has none",
			NewTestShapeWithBase(&BooleanShape{}, &BaseShape{Type: "boolean"}),
			makeSource(&BooleanShape{BaseShape: &BaseShape{Type: "boolean", Enum: Nodes{
				{Value: NewScalarNodeValue(true)}, {Value: NewScalarNodeValue(false)},
			}}}),
			false, 2,
		},
		{
			"base enum restricts source enum",
			NewTestShapeWithBase(&BooleanShape{}, &BaseShape{Type: "boolean", Enum: Nodes{
				{Value: NewScalarNodeValue(true)},
			}}),
			makeSource(&BooleanShape{BaseShape: &BaseShape{Type: "boolean", Enum: Nodes{
				{Value: NewScalarNodeValue(true)}, {Value: NewScalarNodeValue(false)},
			}}}),
			false, 1,
		},
		{
			"enum constraint violation",
			NewTestShapeWithBase(&BooleanShape{}, &BaseShape{Type: "boolean", Enum: Nodes{
				{Value: NewScalarNodeValue(true)},
			}}),
			makeSource(&BooleanShape{BaseShape: &BaseShape{Type: "boolean", Enum: Nodes{
				{Value: NewScalarNodeValue(false)},
			}}}),
			true, 0,
		},
		{
			"incompatible type",
			NewTestShape(&BooleanShape{}, 1),
			makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}}),
			true, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.shape.BaseShape.Inherit(tt.source.Base())
			if (err != nil) != tt.wantErr {
				t.Errorf("Inherit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantEnumLen > 0 && len(got.Enum) != tt.wantEnumLen {
				t.Errorf("Inherit() enum len = %d, want %d", len(got.Enum), tt.wantEnumLen)
			}
		})
	}
}

func TestBooleanShape_unmarshalYAMLNodes(t *testing.T) {
	t.Run("custom facet", func(t *testing.T) {
		s := &BooleanShape{BaseShape: baseWith(t, false)}
		nodes := []*yaml.Node{
			{Value: "custom", Kind: yaml.ScalarNode, Tag: "!!str"},
			{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Value: "key", Kind: yaml.ScalarNode, Tag: "!!str"},
				{Value: "val", Kind: yaml.ScalarNode, Tag: "!!str"},
			}},
		}
		if err := s.unmarshalYAMLNodes(nodes); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("alias node causes error", func(t *testing.T) {
		s := &BooleanShape{BaseShape: baseWith(t, false)}
		nodes := []*yaml.Node{{Value: "custom"}, {Kind: yaml.AliasNode}}
		if err := s.unmarshalYAMLNodes(nodes); err == nil {
			t.Error("expected error for alias node")
		}
	})
}

func TestBooleanShape_Alias(t *testing.T) {
	s := &BooleanShape{BaseShape: &BaseShape{Type: "boolean"}}
	t.Run("same type", func(t *testing.T) {
		src := &BooleanShape{BaseShape: &BaseShape{Type: "boolean"}}
		got, err := s.alias(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, src) {
			t.Errorf("alias() = %v, want %v", got, src)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.alias(&StringShape{BaseShape: &BaseShape{Type: "string"}}); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

// ── DateTimeShape ─────────────────────────────────────────────────────────────

func TestDateTimeShape_Validate(t *testing.T) {
	tests := []struct {
		name    string
		format  string // empty = no format facet
		v       any
		wantErr bool
	}{
		{"rfc3339 valid", "rfc3339", "2021-01-01T00:00:00Z", false},
		{"rfc3339 invalid", "rfc3339", "invalid", true},
		{"rfc2616 valid", "rfc2616", "Sun, 06 Nov 1994 08:49:37 GMT", false},
		{"rfc2616 invalid", "rfc2616", "invalid", true},
		{"no format defaults to rfc3339", "", "2021-01-01T00:00:00Z", false},
		{"no format invalid value", "", "invalid", true},
		{"invalid type", "", 123, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &DateTimeShape{BaseShape: &BaseShape{}}
			if tt.format != "" {
				s.FormatFacets = FormatFacets{Format: scalarFacetOf(tt.format)}
			}
			if err := s.validate(tt.v, ""); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDateTimeShape_Inherit(t *testing.T) {
	tests := []struct {
		name       string
		shape      *DateTimeShape
		source     Shape
		wantErr    bool
		wantFormat string
	}{
		{
			"inherits format from source",
			&DateTimeShape{BaseShape: &BaseShape{Type: "datetime"}},
			makeSource(&DateTimeShape{BaseShape: &BaseShape{Type: "datetime"}, FormatFacets: FormatFacets{Format: scalarFacetOf("rfc3339")}}),
			false, "rfc3339",
		},
		{
			"format conflict",
			&DateTimeShape{BaseShape: &BaseShape{Type: "datetime"}, FormatFacets: FormatFacets{Format: scalarFacetOf("rfc3339")}},
			makeSource(&DateTimeShape{BaseShape: &BaseShape{Type: "datetime"}, FormatFacets: FormatFacets{Format: scalarFacetOf("rfc2616")}}),
			true, "",
		},
		{
			"incompatible type",
			&DateTimeShape{BaseShape: &BaseShape{Type: "datetime"}},
			makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}}),
			true, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.shape.BaseShape.Shape = tt.shape
			got, err := tt.shape.inherit(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("inherit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantFormat != "" {
				gotDT, ok := got.(*DateTimeShape)
				if !ok || gotDT.FormatFacets.Format == nil || gotDT.FormatFacets.Format.Value != tt.wantFormat {
					t.Errorf("inherit() format = %v, want %q", got, tt.wantFormat)
				}
			}
		})
	}
}

func TestDateTimeShape_unmarshalYAMLNodes(t *testing.T) {
	tests := []struct {
		name    string
		base    *BaseShape
		nodes   []*yaml.Node
		wantErr bool
	}{
		{
			"format valid",
			baseWith(t, true),
			[]*yaml.Node{{Value: "format"}, {Value: "rfc3339", Kind: yaml.ScalarNode, Tag: "!!str"}},
			false,
		},
		{
			"format invalid value",
			baseWith(t, false),
			[]*yaml.Node{{Value: "format"}, {Value: "invalid", Kind: yaml.ScalarNode, Tag: "!!str"}},
			true,
		},
		{
			"format wrong tag",
			baseWith(t, false),
			[]*yaml.Node{{Value: "format"}, {Value: "rfc3339", Kind: yaml.ScalarNode, Tag: "!!int"}},
			true,
		},
		{
			"custom facet valid",
			baseWith(t, false),
			[]*yaml.Node{{Value: "custom"}, {Value: "value", Kind: yaml.ScalarNode, Tag: "!!str"}},
			false,
		},
		{
			"alias node causes error",
			baseWith(t, false),
			[]*yaml.Node{{Value: "custom"}, {Kind: yaml.AliasNode}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &DateTimeShape{BaseShape: tt.base}
			if err := s.unmarshalYAMLNodes(tt.nodes); (err != nil) != tt.wantErr {
				t.Errorf("unmarshalYAMLNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDateTimeShape_Alias(t *testing.T) {
	s := &DateTimeShape{BaseShape: &BaseShape{Type: "datetime"}, FormatFacets: FormatFacets{Format: scalarFacetOf("rfc3339")}}
	src := &DateTimeShape{BaseShape: &BaseShape{Type: "datetime"}, FormatFacets: FormatFacets{Format: scalarFacetOf("rfc3339")}}
	t.Run("same type", func(t *testing.T) {
		got, err := s.alias(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, src) {
			t.Errorf("alias() = %v, want %v", got, src)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.alias(&StringShape{BaseShape: &BaseShape{Type: "string"}}); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

// ── DateTimeOnlyShape ─────────────────────────────────────────────────────────

func TestDateTimeOnlyShape_Validate(t *testing.T) {
	s := &DateTimeOnlyShape{BaseShape: &BaseShape{}}
	for _, tc := range []struct {
		v       any
		wantErr bool
	}{
		{"2021-01-01T00:00:00", false},
		{"invalid", true},
		{123, true},
	} {
		if err := s.validate(tc.v, ""); (err != nil) != tc.wantErr {
			t.Errorf("validate(%v) error = %v, wantErr %v", tc.v, err, tc.wantErr)
		}
	}
}

func TestDateTimeOnlyShape_Inherit(t *testing.T) {
	s := &DateTimeOnlyShape{BaseShape: &BaseShape{Type: "datetime-only"}}
	s.BaseShape.Shape = s
	t.Run("same type", func(t *testing.T) {
		src := makeSource(&DateTimeOnlyShape{BaseShape: &BaseShape{Type: "datetime-only"}})
		if _, err := s.inherit(src); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		src := makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}})
		if _, err := s.inherit(src); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

func TestDateTimeOnlyShape_unmarshalYAMLNodes(t *testing.T) {
	t.Run("custom facet valid", func(t *testing.T) {
		s := &DateTimeOnlyShape{BaseShape: baseWith(t, false)}
		nodes := []*yaml.Node{
			{Value: "custom", Kind: yaml.ScalarNode, Tag: "!!str"},
			{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Value: "key", Kind: yaml.ScalarNode, Tag: "!!str"},
				{Value: "val", Kind: yaml.ScalarNode, Tag: "!!str"},
			}},
		}
		if err := s.unmarshalYAMLNodes(nodes); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("alias node causes error", func(t *testing.T) {
		s := &DateTimeOnlyShape{BaseShape: baseWith(t, false)}
		if err := s.unmarshalYAMLNodes([]*yaml.Node{{Value: "custom"}, {Kind: yaml.AliasNode}}); err == nil {
			t.Error("expected error")
		}
	})
}

func TestDateTimeOnlyShape_Alias(t *testing.T) {
	s := &DateTimeOnlyShape{BaseShape: &BaseShape{Type: "datetime-only"}}
	t.Run("same type", func(t *testing.T) {
		src := &DateTimeOnlyShape{BaseShape: &BaseShape{Type: "datetime-only"}}
		got, err := s.alias(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, src) {
			t.Errorf("alias() = %v, want %v", got, src)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.alias(&StringShape{BaseShape: &BaseShape{Type: "string"}}); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

// ── DateOnlyShape ─────────────────────────────────────────────────────────────

func TestDateOnlyShape_Validate(t *testing.T) {
	s := &DateOnlyShape{BaseShape: &BaseShape{}}
	for _, tc := range []struct {
		v       any
		wantErr bool
	}{
		{"2021-01-01", false},
		{"invalid", true},
		{123, true},
	} {
		if err := s.validate(tc.v, ""); (err != nil) != tc.wantErr {
			t.Errorf("validate(%v) error = %v, wantErr %v", tc.v, err, tc.wantErr)
		}
	}
}

func TestDateOnlyShape_Inherit(t *testing.T) {
	s := &DateOnlyShape{BaseShape: &BaseShape{Type: "date-only"}}
	s.BaseShape.Shape = s
	t.Run("same type", func(t *testing.T) {
		if _, err := s.inherit(makeSource(&DateOnlyShape{BaseShape: &BaseShape{Type: "date-only"}})); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.inherit(makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}})); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

func TestDateOnlyShape_unmarshalYAMLNodes(t *testing.T) {
	t.Run("custom facet valid", func(t *testing.T) {
		s := &DateOnlyShape{BaseShape: baseWith(t, false)}
		nodes := []*yaml.Node{
			{Value: "custom", Kind: yaml.ScalarNode, Tag: "!!str"},
			{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Value: "key", Kind: yaml.ScalarNode, Tag: "!!str"},
				{Value: "val", Kind: yaml.ScalarNode, Tag: "!!str"},
			}},
		}
		if err := s.unmarshalYAMLNodes(nodes); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("alias node causes error", func(t *testing.T) {
		s := &DateOnlyShape{BaseShape: baseWith(t, false)}
		if err := s.unmarshalYAMLNodes([]*yaml.Node{{Value: "custom"}, {Kind: yaml.AliasNode}}); err == nil {
			t.Error("expected error")
		}
	})
}

// ── TimeOnlyShape ─────────────────────────────────────────────────────────────

func TestTimeOnlyShape_Validate(t *testing.T) {
	s := &TimeOnlyShape{BaseShape: &BaseShape{}}
	for _, tc := range []struct {
		v       any
		wantErr bool
	}{
		{"00:00:00", false},
		{"invalid", true},
		{123, true},
	} {
		if err := s.validate(tc.v, ""); (err != nil) != tc.wantErr {
			t.Errorf("validate(%v) error = %v, wantErr %v", tc.v, err, tc.wantErr)
		}
	}
}

func TestTimeOnlyShape_Inherit(t *testing.T) {
	s := &TimeOnlyShape{BaseShape: &BaseShape{Type: "time-only"}}
	s.BaseShape.Shape = s
	t.Run("same type", func(t *testing.T) {
		if _, err := s.inherit(makeSource(&TimeOnlyShape{BaseShape: &BaseShape{Type: "time-only"}})); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.inherit(makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}})); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

func TestTimeOnlyShape_unmarshalYAMLNodes(t *testing.T) {
	t.Run("custom facet valid", func(t *testing.T) {
		s := &TimeOnlyShape{BaseShape: baseWith(t, false)}
		nodes := []*yaml.Node{
			{Value: "custom", Kind: yaml.ScalarNode, Tag: "!!str"},
			{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Value: "key", Kind: yaml.ScalarNode, Tag: "!!str"},
				{Value: "val", Kind: yaml.ScalarNode, Tag: "!!str"},
			}},
		}
		if err := s.unmarshalYAMLNodes(nodes); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("alias node causes error", func(t *testing.T) {
		s := &TimeOnlyShape{BaseShape: baseWith(t, false)}
		if err := s.unmarshalYAMLNodes([]*yaml.Node{{Value: "custom"}, {Kind: yaml.AliasNode}}); err == nil {
			t.Error("expected error")
		}
	})
}

func TestTimeOnlyShape_Alias(t *testing.T) {
	s := &TimeOnlyShape{BaseShape: &BaseShape{Type: "time-only"}}
	t.Run("same type", func(t *testing.T) {
		src := &TimeOnlyShape{BaseShape: &BaseShape{Type: "time-only"}}
		got, err := s.alias(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, src) {
			t.Errorf("alias() = %v, want %v", got, src)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.alias(&StringShape{BaseShape: &BaseShape{Type: "string"}}); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

// ── AnyShape ──────────────────────────────────────────────────────────────────

func TestAnyShape_Validate(t *testing.T) {
	s := &AnyShape{BaseShape: &BaseShape{}}
	if err := s.validate("anything", ""); err != nil {
		t.Errorf("AnyShape.validate() unexpected error: %v", err)
	}
}

func TestAnyShape_Inherit(t *testing.T) {
	s := &AnyShape{BaseShape: &BaseShape{Type: "any"}}
	s.BaseShape.Shape = s
	t.Run("same type", func(t *testing.T) {
		if _, err := s.inherit(makeSource(&AnyShape{BaseShape: &BaseShape{Type: "any"}})); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.inherit(makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}})); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

func TestAnyShape_unmarshalYAMLNodes(t *testing.T) {
	t.Run("custom facet valid", func(t *testing.T) {
		s := &AnyShape{BaseShape: baseWith(t, false)}
		nodes := []*yaml.Node{
			{Value: "custom", Kind: yaml.ScalarNode, Tag: "!!str"},
			{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Value: "key", Kind: yaml.ScalarNode, Tag: "!!str"},
				{Value: "val", Kind: yaml.ScalarNode, Tag: "!!str"},
			}},
		}
		if err := s.unmarshalYAMLNodes(nodes); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("alias node causes error", func(t *testing.T) {
		s := &AnyShape{BaseShape: baseWith(t, false)}
		if err := s.unmarshalYAMLNodes([]*yaml.Node{{Value: "custom"}, {Kind: yaml.AliasNode}}); err == nil {
			t.Error("expected error")
		}
	})
}

func TestAnyShape_Alias(t *testing.T) {
	s := &AnyShape{BaseShape: &BaseShape{Type: "any"}}
	t.Run("same type", func(t *testing.T) {
		src := &AnyShape{BaseShape: &BaseShape{Type: "any"}}
		got, err := s.alias(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, src) {
			t.Errorf("alias() = %v, want %v", got, src)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.alias(&StringShape{BaseShape: &BaseShape{Type: "string"}}); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

// ── NilShape ──────────────────────────────────────────────────────────────────

func TestNilShape_Validate(t *testing.T) {
	s := &NilShape{BaseShape: &BaseShape{}}
	t.Run("nil value valid", func(t *testing.T) {
		if err := s.validate(nil, ""); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("non-nil value invalid", func(t *testing.T) {
		if err := s.validate("value", ""); err == nil {
			t.Error("expected error for non-nil value")
		}
	})
}

func TestNilShape_Inherit(t *testing.T) {
	s := &NilShape{BaseShape: &BaseShape{Type: "nil"}}
	s.BaseShape.Shape = s
	t.Run("same type", func(t *testing.T) {
		if _, err := s.inherit(makeSource(&NilShape{BaseShape: &BaseShape{Type: "nil"}})); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.inherit(makeSource(&StringShape{BaseShape: &BaseShape{Type: "string"}})); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}

func TestNilShape_unmarshalYAMLNodes(t *testing.T) {
	t.Run("custom facet valid", func(t *testing.T) {
		s := &NilShape{BaseShape: baseWith(t, false)}
		nodes := []*yaml.Node{
			{Value: "custom", Kind: yaml.ScalarNode, Tag: "!!str"},
			{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Value: "key", Kind: yaml.ScalarNode, Tag: "!!str"},
				{Value: "val", Kind: yaml.ScalarNode, Tag: "!!str"},
			}},
		}
		if err := s.unmarshalYAMLNodes(nodes); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("alias node causes error", func(t *testing.T) {
		s := &NilShape{BaseShape: baseWith(t, false)}
		if err := s.unmarshalYAMLNodes([]*yaml.Node{{Value: "custom"}, {Kind: yaml.AliasNode}}); err == nil {
			t.Error("expected error")
		}
	})
}

func TestNilShape_Alias(t *testing.T) {
	s := &NilShape{BaseShape: &BaseShape{Type: "nil"}}
	t.Run("same type", func(t *testing.T) {
		src := &NilShape{BaseShape: &BaseShape{Type: "nil"}}
		got, err := s.alias(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, src) {
			t.Errorf("alias() = %v, want %v", got, src)
		}
	})
	t.Run("incompatible type", func(t *testing.T) {
		if _, err := s.alias(&StringShape{BaseShape: &BaseShape{Type: "string"}}); err == nil {
			t.Error("expected error for incompatible type")
		}
	})
}
