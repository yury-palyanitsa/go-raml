package raml

import (
	"testing"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

// makeTestExample builds an Example backed by a fresh RAML instance.
// CustomDomainProperties is always initialised so domain-extension tests work out of the box.
func makeTestExample(t *testing.T, r *RAML) *Example {
	t.Helper()
	return &Example{
		raml:                   r,
		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
	}
}

// TestExample_decode verifies that individual key/value pairs are decoded into Example fields.
func TestExample_decode(t *testing.T) {
	scalar := func(val string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: val} }
	mapping := func(kv ...*yaml.Node) *yaml.Node {
		return &yaml.Node{Kind: yaml.MappingNode, Content: kv}
	}

	tests := []struct {
		name      string
		key       string
		valueNode *yaml.Node
		wantErr   bool
	}{
		{
			name:      "strict=true is accepted",
			key:       "strict",
			valueNode: scalar("true"),
		},
		{
			name:      "displayName scalar is accepted",
			key:       "displayName",
			valueNode: scalar("My Display Name"),
		},
		{
			name:      "description scalar is accepted",
			key:       "description",
			valueNode: scalar("A description"),
		},
		{
			name:      "custom domain extension is accepted",
			key:       "(custom)",
			valueNode: mapping(scalar("key"), scalar("value")),
		},
		{
			name:      "strict with mapping node is rejected",
			key:       "strict",
			valueNode: &yaml.Node{Kind: yaml.MappingNode, Value: "invalid"},
			wantErr:   true,
		},
		{
			name:      "displayName with mapping node is rejected",
			key:       "displayName",
			valueNode: &yaml.Node{Kind: yaml.MappingNode, Value: "invalid"},
			wantErr:   true,
		},
		{
			name:      "description with mapping node is rejected",
			key:       "description",
			valueNode: &yaml.Node{Kind: yaml.MappingNode, Value: "invalid"},
			wantErr:   true,
		},
		{
			name: "custom domain extension with invalid content is rejected",
			key:  "()",
			valueNode: mapping(
				&yaml.Node{Kind: yaml.ScalarNode, Value: "key", Tag: "!!int"},
				&yaml.Node{Kind: yaml.MappingNode, Value: "invalid", Tag: "!!int"},
			),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			ex := makeTestExample(t, r)
			keyNode := &yaml.Node{Value: tt.key}
			if err := ex.decode(keyNode, tt.valueNode, "test.raml"); (err != nil) != tt.wantErr {
				t.Errorf("decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestExample_fill verifies that a mapping node is parsed into an Example via the "value" key.
func TestExample_fill(t *testing.T) {
	scalar := func(val string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: val} }

	tests := []struct {
		name    string
		value   *yaml.Node
		wantErr bool
	}{
		{
			name: "mapping with value key is accepted",
			value: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("value"), scalar("example"),
			}},
		},
		{
			name: "mapping without value key returns ErrValueKeyNotFound",
			value: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("other"), scalar("data"),
			}},
			wantErr: true,
		},
		{
			name: "value key present but subsequent decode fails",
			value: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("value"), scalar("data"),
				scalar("strict"), scalar("123"),
			}},
			wantErr: true,
		},
		{
			name: "value is present but makeRootNode fails on invalid tag",
			value: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("value"),
				{Kind: yaml.MappingNode, Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "key", Tag: "!!int"},
					{Kind: yaml.ScalarNode, Value: "val", Tag: "!!int"},
				}},
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			ex := makeTestExample(t, r)
			if err := ex.fill("test.raml", tt.value); (err != nil) != tt.wantErr {
				t.Errorf("fill() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRAML_makeExample verifies the full example construction pipeline.
func TestRAML_makeExample(t *testing.T) {
	scalar := func(val string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: val} }

	tests := []struct {
		name    string
		value   *yaml.Node
		wantErr bool
		check   func(*Example)
	}{
		{
			name: "mapping with value key — fill path sets Data",
			value: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("value"), scalar("example"),
			}},
			check: func(ex *Example) {
				if ex.Data == nil {
					t.Error("Data is nil, want non-nil")
				}
			},
		},
		{
			name:  "scalar value — direct node path sets Data",
			value: scalar("hello"),
			check: func(ex *Example) {
				if ex.Data == nil {
					t.Error("Data is nil, want non-nil")
				}
			},
		},
		{
			name: "mapping without value key — treated as object value, Data set",
			value: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("key"), scalar("val"),
			}},
			check: func(ex *Example) {
				if ex.Data == nil {
					t.Error("Data is nil, want non-nil")
				}
			},
		},
		{
			name: "mapping fill error propagated",
			value: &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("value"), scalar("data"),
				scalar("strict"), scalar("not-a-bool"),
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			got, err := r.makeExample(tt.value, "ex", "test.raml")
			if (err != nil) != tt.wantErr {
				t.Errorf("makeExample() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(got)
			}
		})
	}
}
