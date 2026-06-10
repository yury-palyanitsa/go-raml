package raml

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseTemplateVariables(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		want    []VariableInfo
		wantErr bool
	}{
		{
			name: "no variables",
			s:    "plain text",
			want: nil,
		},
		{
			name: "single variable",
			s:    "<<name>>",
			want: []VariableInfo{{Name: "name", Substring: "<<name>>"}},
		},
		{
			name: "single variable with action",
			s:    "<<name|!uppercase>>",
			want: []VariableInfo{{Name: "name", Substring: "<<name|!uppercase>>", Actions: []string{ActionUppercase}}},
		},
		{
			name: "variable embedded in text",
			s:    "get/<<resourcePath>>",
			want: []VariableInfo{{Name: "resourcePath", Substring: "<<resourcePath>>"}},
		},
		{
			name: "multiple adjacent variables",
			s:    "<<a>> and <<b>>",
			want: []VariableInfo{
				{Name: "a", Substring: "<<a>>"},
				{Name: "b", Substring: "<<b>>"},
			},
		},
		{
			name: "variable with multiple actions",
			s:    "<<name|!uppercase|!singularize>>",
			want: []VariableInfo{{
				Name:      "name",
				Substring: "<<name|!uppercase|!singularize>>",
				Actions:   []string{ActionUppercase, ActionSingularize},
			}},
		},
		{
			name:    "unclosed variable",
			s:       "<<name",
			wantErr: true,
		},
		{
			name:    "invalid content: action without variable name",
			s:       "<<|!uppercase>>",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTemplateVariables("test.raml", tt.s)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseVariableContent(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantName    string
		wantActions []string
		wantErr     bool
	}{
		{
			name:     "simple name",
			content:  "name",
			wantName: "name",
		},
		{
			name:     "name with surrounding spaces",
			content:  " name ",
			wantName: "name",
		},
		{
			name:        "name with one action",
			content:     "name|!uppercase",
			wantName:    "name",
			wantActions: []string{ActionUppercase},
		},
		{
			name:        "name with multiple actions",
			content:     "name|!uppercase|!singularize",
			wantName:    "name",
			wantActions: []string{ActionUppercase, ActionSingularize},
		},
		{
			name:        "spaces around name and action",
			content:     " name | !uppercase ",
			wantName:    "name",
			wantActions: []string{ActionUppercase},
		},
		{
			name:        "empty pipe segment skipped",
			content:     "name||!uppercase",
			wantName:    "name",
			wantActions: []string{ActionUppercase},
		},
		{
			name:    "empty content: missing variable name",
			content: "",
			wantErr: true,
		},
		{
			name:    "only spaces: missing variable name",
			content: "   ",
			wantErr: true,
		},
		{
			name:    "action as first token: no variable name",
			content: "!uppercase",
			wantErr: true,
		},
		{
			name:    "action does not start with !",
			content: "name|bad",
			wantErr: true,
		},
		{
			name:    "unknown action",
			content: "name|!unknown",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotActions, err := parseVariableContent("test.raml", tt.content)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantName, gotName)
			require.Equal(t, tt.wantActions, gotActions)
		})
	}
}

func TestFindVariable(t *testing.T) {
	t.Run("non-TagStr scalar: skipped", func(t *testing.T) {
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := findVariable("test.raml", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "123"}, 0, idx, declared)
		require.NoError(t, err)
		require.Empty(t, idx)
		require.Empty(t, declared)
	})

	t.Run("TagStr scalar without template: not indexed", func(t *testing.T) {
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := findVariable("test.raml", strNode("plain text"), 0, idx, declared)
		require.NoError(t, err)
		require.Empty(t, idx)
		require.Empty(t, declared)
	})

	t.Run("TagStr scalar with variable: indexed and declared", func(t *testing.T) {
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := findVariable("test.raml", strNode("<<resourceName>>"), 7, idx, declared)
		require.NoError(t, err)
		require.Equal(t, map[int][]VariableInfo{
			7: {{Name: "resourceName", Substring: "<<resourceName>>"}},
		}, idx)
		require.Equal(t, map[string]struct{}{"resourceName": {}}, declared)
	})

	t.Run("TagStr scalar with action: action stored in index", func(t *testing.T) {
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := findVariable("test.raml", strNode("<<name|!uppercase>>"), 3, idx, declared)
		require.NoError(t, err)
		require.Equal(t, []string{ActionUppercase}, idx[3][0].Actions)
	})

	t.Run("non-scalar node returns error", func(t *testing.T) {
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := findVariable("test.raml", mapNode(), 0, idx, declared)
		require.Error(t, err)
	})

	t.Run("unclosed template returns error", func(t *testing.T) {
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := findVariable("test.raml", strNode("<<name"), 0, idx, declared)
		require.Error(t, err)
	})
}

func TestCollectVariablesIndex(t *testing.T) {
	t.Run("scalar with variable is indexed", func(t *testing.T) {
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := collectVariablesIndex("test.raml", strNode("<<name>>"), 0, idx, declared)
		require.NoError(t, err)
		require.Len(t, idx[0], 1)
		require.Equal(t, "name", idx[0][0].Name)
		require.Equal(t, map[string]struct{}{"name": {}}, declared)
	})

	t.Run("scalar without variable: no index entry", func(t *testing.T) {
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := collectVariablesIndex("test.raml", strNode("plain"), 0, idx, declared)
		require.NoError(t, err)
		require.Empty(t, idx)
		require.Empty(t, declared)
	})

	t.Run("mapping node: value scalar indexed at child offset", func(t *testing.T) {
		// Content[0]=key (idx+0=0), Content[1]=value (idx+1=1)
		node := mapNode(strNode("key"), strNode("<<value>>"))
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := collectVariablesIndex("test.raml", node, 0, idx, declared)
		require.NoError(t, err)
		require.Empty(t, idx[0], "key node must not be indexed")
		require.Len(t, idx[1], 1)
		require.Equal(t, "value", idx[1][0].Name)
		require.Contains(t, declared, "value")
	})

	t.Run("sequence node: each item indexed at child offset", func(t *testing.T) {
		node := seqNode(strNode("<<a>>"), strNode("<<b>>"))
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := collectVariablesIndex("test.raml", node, 0, idx, declared)
		require.NoError(t, err)
		require.Equal(t, "a", idx[0][0].Name)
		require.Equal(t, "b", idx[1][0].Name)
	})

	t.Run("error from nested bad template is propagated", func(t *testing.T) {
		node := mapNode(strNode("key"), strNode("<<unclosed"))
		idx := make(map[int][]VariableInfo)
		declared := make(map[string]struct{})
		err := collectVariablesIndex("test.raml", node, 0, idx, declared)
		require.Error(t, err)
	})
}

func TestCollectRequiredVariables(t *testing.T) {
	t.Run("nil node returns empty map", func(t *testing.T) {
		result := collectRequiredVariables(nil, 0, nil)
		require.Empty(t, result)
	})

	t.Run("scalar with matching index entry returns variable names", func(t *testing.T) {
		idx := map[int][]VariableInfo{
			5: {{Name: "pageSize"}, {Name: "offset"}},
		}
		result := collectRequiredVariables(strNode("<<pageSize>> and <<offset>>"), 5, idx)
		require.Equal(t, map[string]struct{}{"pageSize": {}, "offset": {}}, result)
	})

	t.Run("scalar without index entry returns empty", func(t *testing.T) {
		result := collectRequiredVariables(strNode("plain"), 0, map[int][]VariableInfo{})
		require.Empty(t, result)
	})

	t.Run("mapping node collects variables from child scalars", func(t *testing.T) {
		// Content[1] is at idx+1=1; index has an entry there.
		node := mapNode(strNode("key"), strNode("<<name>>"))
		idx := map[int][]VariableInfo{
			1: {{Name: "name"}},
		}
		result := collectRequiredVariables(node, 0, idx)
		require.Equal(t, map[string]struct{}{"name": {}}, result)
	})

	t.Run("sequence node collects variables from all items", func(t *testing.T) {
		node := seqNode(strNode("<<x>>"), strNode("<<y>>"))
		idx := map[int][]VariableInfo{
			0: {{Name: "x"}},
			1: {{Name: "y"}},
		}
		result := collectRequiredVariables(node, 0, idx)
		require.Equal(t, map[string]struct{}{"x": {}, "y": {}}, result)
	})
}
