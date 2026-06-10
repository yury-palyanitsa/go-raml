package raml

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/acronis/go-stacktrace"
	"gopkg.in/yaml.v3"
)

func TestNodes_String(t *testing.T) {
	tests := []struct {
		name string
		n    Nodes
		want string
	}{
		{
			name: "full positive case",
			n: Nodes{
				{
					Value:    NewScalarNodeValue("value1"),
					Location: "location1",
					ValuePos: stacktrace.Position{Line: 1, Column: 1},
				},
				{
					Value:    NewScalarNodeValue("value2"),
					Location: "location2",
					ValuePos: stacktrace.Position{Line: 2, Column: 2},
				},
			},
			want: "value1, value2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNode_String(t *testing.T) {
	tests := []struct {
		name string
		node *DataNode
		want string
	}{
		{
			name: "full positive case",
			node: &DataNode{
				Value:    NewScalarNodeValue("value1"),
				Location: "location1",
				ValuePos: stacktrace.Position{Line: 1, Column: 1},
			},
			want: "value1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.node.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRAML_makeIncludedNode(t *testing.T) {
	base := t.TempDir()
	loc := filepath.Join(base, "location.raml")

	type args struct {
		node     *yaml.Node
		location string
	}
	tests := []struct {
		name    string
		fs      testFS
		args    args
		want    func(tt *testing.T, n *DataNode)
		wantErr bool
	}{
		{
			name: "full positive case: yaml node",
			fs:   testFS{filepath.Join(base, "filename.yaml"): "key: value"},
			args: args{
				node: &yaml.Node{
					Tag:   TagInclude,
					Value: "filename.yaml",
					Line:  1,
				},
				location: loc,
			},
		},
		{
			name: "full positive case: json node",
			fs:   testFS{filepath.Join(base, "filename.json"): `{"key": "value"}`},
			args: args{
				node: &yaml.Node{
					Tag:   TagInclude,
					Value: "filename.json",
					Line:  1,
				},
				location: loc,
			},
		},
		{
			name: "full positive case: unknown extension",
			fs:   testFS{filepath.Join(base, "filename.unknown"): "key: value"},
			args: args{
				node: &yaml.Node{
					Tag:   TagInclude,
					Value: "filename.unknown",
					Line:  1,
				},
				location: loc,
			},
		},
		{
			name: "negative case: file not found",
			args: args{
				node: &yaml.Node{
					Tag:   TagInclude,
					Value: "notfound.yaml",
					Line:  1,
				},
				location: loc,
			},
			wantErr: true,
		},
		{
			name: "negative case: json decode error",
			fs:   testFS{filepath.Join(base, "err.json"): `{"key": "value`},
			args: args{
				node: &yaml.Node{
					Tag:   TagInclude,
					Value: "err.json",
					Line:  1,
				},
				location: loc,
			},
			wantErr: true,
		},
		{
			name: "negative case: yaml decode error",
			fs:   testFS{filepath.Join(base, "err.yaml"): "key: value\nbad"},
			args: args{
				node: &yaml.Node{
					Tag:   TagInclude,
					Value: "err.yaml",
					Line:  1,
				},
				location: loc,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			got, errInclude := r.makeIncludedNode(nil, tt.args.node, tt.args.location)
			if (errInclude != nil) != tt.wantErr {
				t.Errorf("makeIncludedNode() error = %v, wantErr %v", errInclude, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestRAML_makeRootNode(t *testing.T) {
	type args struct {
		node     *yaml.Node
		location string
	}
	tests := []struct {
		name    string
		args    args
		want    func(*testing.T, *DataNode)
		wantErr bool
	}{
		{
			name: "full positive case: json unmarshal",
			args: args{
				node: &yaml.Node{
					Value: `{"key": "value"}`,
				},
				location: "location.raml",
			},
			want: func(t *testing.T, n *DataNode) {
				if !reflect.DeepEqual(n.Value.Raw, map[string]any{"key": "value"}) {
					t.Errorf("makeRootNode() = %v, want %v", n.Value.Raw, map[string]any{"key": "value"})
				}
			},
		},
		{
			name: "negative case: tag include",
			args: args{
				node: &yaml.Node{
					Tag: "!include",
				},
			},
			wantErr: true,
		},
		{
			name: "negative case: json unmarshal error",
			args: args{
				node: &yaml.Node{
					Value: `{"key": "value`,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			got, err := r.makeRootNode(nil, tt.args.node, tt.args.location)
			if (err != nil) != tt.wantErr {
				t.Errorf("makeRootNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestRAML_makeYamlNode(t *testing.T) {
	type args struct {
		node     *yaml.Node
		location string
	}
	tests := []struct {
		name    string
		args    args
		want    func(*testing.T, *DataNode)
		wantErr bool
	}{
		{
			name: "full positive case",
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "value",
				},
			},
			want: func(t *testing.T, n *DataNode) {
				if n.Value.Raw != "value" {
					t.Errorf("makeYamlNode() = %v, want %v", n.Value.Raw, "value")
				}
			},
		},
		{
			name: "negative case: yaml node to data node error: unexpected kind",
			args: args{
				node: &yaml.Node{
					Kind: 123,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAML(t)
			got, err := r.makeYamlNode(nil, tt.args.node, tt.args.location)
			if (err != nil) != tt.wantErr {
				t.Errorf("makeYamlNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func Test_scalarNodeToDataNode(t *testing.T) {
	base := t.TempDir()
	loc := filepath.Join(base, "location.raml")
	type args struct {
		node     *yaml.Node
		location string
	}
	tests := []struct {
		name    string
		fs      testFS
		args    args
		want    any
		wantErr bool
	}{
		{
			name: "positive case: decode default node: int",
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "123",
					Tag:   "!!int",
				},
			},
			want: 123,
		},
		{
			name: "negative case: decode default node: int",
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "value",
					Tag:   "!!int",
				},
			},
			wantErr: true,
		},
		{
			name: "positive case: decode str node",
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "value",
					Tag:   "!!str",
				},
			},
			want: "value",
		},
		{
			name: "positive case: decode timestamp node",
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "2021-09-01T00:00:00Z",
					Tag:   "!!timestamp",
				},
			},
			want: "2021-09-01T00:00:00Z",
		},
		{
			name: "positive case: decode include node: yaml",
			fs:   testFS{filepath.Join(base, "filename.yaml"): "key: value"},
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "filename.yaml",
					Tag:   "!include",
				},
				location: loc,
			},
			want: map[string]any{"key": "value"},
		},
		{
			name: "positive case: decode include node: any",
			fs:   testFS{filepath.Join(base, "filename.txt"): "Hello world!"},
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "filename.txt",
					Tag:   "!include",
				},
				location: loc,
			},
			want: "Hello world!",
		},
		{
			name: "negative case: decode include node: file not found",
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "notfound.yaml",
					Tag:   "!include",
				},
				location: loc,
			},
			wantErr: true,
		},
		{
			name: "negative case: circular include",
			fs:   testFS{filepath.Join(base, "cycle.yaml"): "!include cycle.yaml"},
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "cycle.yaml",
					Tag:   "!include",
				},
				location: loc,
			},
			wantErr: true,
		},
		{
			name: "negative case: decode included yaml node: bad indent",
			fs:   testFS{filepath.Join(base, "err.yaml"): "\tkey: value\n bad: bad\n\t"},
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "err.yaml",
					Tag:   "!include",
				},
				location: loc,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRAMLWithFS(t, tt.fs)
			got, err := r.scalarNodeToNodeValue(tt.args.node, tt.args.location, nil)
			if tt.wantErr {
				if err == nil {
					t.Errorf("scalarNodeToNodeValue() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("scalarNodeToNodeValue() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got.Raw, tt.want) {
				t.Errorf("scalarNodeToNodeValue() got = %v, want %v", got.Raw, tt.want)
			}
		})
	}
}

func Test_yamlNodeToDataNode(t *testing.T) {
	r := makeTestRAML(t)
	type args struct {
		node     *yaml.Node
		location string
	}
	tests := []struct {
		name    string
		args    args
		want    any
		wantErr bool
	}{
		{
			name: "negative case: unexpected kind",
			args: args{
				node: &yaml.Node{
					Kind: 123,
				},
			},
			wantErr: true,
		},
		{
			name: "negative case: alias nodes are not supported",
			args: args{
				node: &yaml.Node{
					Kind: yaml.AliasNode,
				},
			},
			wantErr: true,
		},
		{
			name: "positive case: document node",
			args: args{
				node: &yaml.Node{
					Kind: yaml.DocumentNode,
					Content: []*yaml.Node{
						{
							Kind:  yaml.ScalarNode,
							Value: "value",
						},
					},
				},
			},
			want: "value",
		},
		{
			name: "positive case: scalar node",
			args: args{
				node: &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "value",
				},
			},
			want: "value",
		},
		{
			name: "positive case: mapping node",
			args: args{
				node: &yaml.Node{
					Kind: yaml.MappingNode,
					Content: []*yaml.Node{
						{
							Value: "key",
						},
						{
							Kind:  yaml.ScalarNode,
							Value: "value",
						},
					},
				},
			},
			want: map[string]any{"key": "value"},
		},
		{
			name: "positive case: sequence node",
			args: args{
				node: &yaml.Node{
					Kind: yaml.SequenceNode,
					Content: []*yaml.Node{
						{
							Kind:  yaml.ScalarNode,
							Value: "value1",
						},
						{
							Kind:  yaml.ScalarNode,
							Value: "value2",
						},
					},
				},
			},
			want: []any{"value1", "value2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.yamlNodeToNodeValue(tt.args.node, tt.args.location, nil)
			if tt.wantErr {
				if err == nil {
					t.Errorf("yamlNodeToNodeValue() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("yamlNodeToNodeValue() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got.Raw, tt.want) {
				t.Errorf("yamlNodeToNodeValue() = %v, want %v", got.Raw, tt.want)
			}
		})
	}
}
