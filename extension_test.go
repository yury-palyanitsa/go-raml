package raml

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestRAML_unmarshalCustomDomainExtension verifies annotation parsing from YAML key/value nodes.
func TestRAML_unmarshalCustomDomainExtension(t *testing.T) {
	scalar := func(val, tag string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val, Tag: tag}
	}

	tests := []struct {
		name      string
		keyNode   *yaml.Node
		valueNode *yaml.Node
		wantErr   bool
		check     func(*DomainExtension)
	}{
		{
			name:      "valid annotation extracts name and value",
			keyNode:   &yaml.Node{Kind: yaml.ScalarNode, Value: "(name)"},
			valueNode: scalar("value", ""),
			check: func(de *DomainExtension) {
				if de.Name != "name" {
					t.Errorf("Name = %q, want %q", de.Name, "name")
				}
				if de.Extension.Value.Raw != "value" {
					t.Errorf("Extension.Value.Raw = %q, want value", de.Extension.Value.Raw)
				}
			},
		},
		{
			name:      "empty annotation name is rejected",
			keyNode:   &yaml.Node{Kind: yaml.ScalarNode, Value: "()"},
			valueNode: scalar("value", ""),
			wantErr:   true,
		},
		{
			name:      "invalid value node tag causes make-node error",
			keyNode:   &yaml.Node{Kind: yaml.ScalarNode, Value: "(name)"},
			valueNode: scalar("value", "!!int"),
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			de, err := r.unmarshalCustomDomainExtension("test.raml", tt.keyNode, tt.valueNode)
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshalCustomDomainExtension() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(de)
			}
		})
	}
}
