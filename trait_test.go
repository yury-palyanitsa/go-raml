package raml

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func strNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: TagStr, Value: v}
}

func mapNode(kv ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: kv}
}

func seqNode(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Content: items}
}

// makeTraitDefinitionNode builds a representative trait body used by tests.
func makeTraitDefinitionNode(withUsage bool, withVars bool) *yaml.Node {
	content := make([]*yaml.Node, 0, 4)
	if withUsage {
		content = append(content, strNode(FacetUsage), strNode("Trait usage"))
	}

	qpName := "page"
	qpType := "integer"
	if withVars {
		qpName = "<<param>>"
		qpType = "<<type|!uppercase>>"
	}
	content = append(content,
		strNode(FacetQueryParameters),
		mapNode(
			strNode(qpName),
			mapNode(
				strNode(FacetType),
				strNode(qpType),
			),
		),
	)
	return mapNode(content...)
}

func TestRAML_makeTraitDefinition(t *testing.T) {
	r := makeTestRAML(t)
	location := "test.raml"

	t.Run("compiles when no vars", func(t *testing.T) {
		key := strNode("paged")
		def, err := r.makeTraitDefinition(key, makeTraitDefinitionNode(true, false), location)
		require.NoError(t, err)
		require.Equal(t, "paged", def.Name)
		require.NotNil(t, def.Usage)
		require.Equal(t, "Trait usage", def.Usage.Value)
		require.Empty(t, def.DeclaredVariables)
	})

	t.Run("collects vars when present", func(t *testing.T) {
		def, err := r.makeTraitDefinition(nil, makeTraitDefinitionNode(false, true), location)
		require.NoError(t, err)
		require.Contains(t, def.DeclaredVariables, "param")
		require.Contains(t, def.DeclaredVariables, "type")
	})

	t.Run("errors for invalid node kind", func(t *testing.T) {
		_, err := r.makeTraitDefinition(nil, seqNode(), location)
		require.Error(t, err)
	})
}

func TestTraitDefinition_decode(t *testing.T) {
	r := makeTestRAML(t)
	location := "test.raml"

	t.Run("null trait is allowed", func(t *testing.T) {
		def := &TraitDefinition{raml: r, Location: location}
		err := def.decode(&yaml.Node{Kind: yaml.ScalarNode, Tag: TagNull})
		require.NoError(t, err)
		require.Nil(t, def.Source)
	})

	t.Run("usage is extracted and removed from source", func(t *testing.T) {
		def := &TraitDefinition{raml: r, Location: location}
		err := def.decode(makeTraitDefinitionNode(true, false))
		require.NoError(t, err)
		require.NotNil(t, def.Usage)
		require.Equal(t, "Trait usage", def.Usage.Value)
		require.NotNil(t, def.Source)
		require.Len(t, def.Source.Content, 2)
		require.Equal(t, FacetQueryParameters, def.Source.Content[0].Value)
	})

	t.Run("errors for non mapping node", func(t *testing.T) {
		def := &TraitDefinition{raml: r, Location: location}
		err := def.decode(seqNode(strNode("x")))
		require.Error(t, err)
	})
}

func TestTraitDefinition_collectVariablesIndex(t *testing.T) {
	r := makeTestRAML(t)
	def, err := r.makeTraitDefinition(nil, makeTraitDefinitionNode(false, true), "test.raml")
	require.NoError(t, err)
	require.NotNil(t, def.Source)

	err = def.collectVariablesIndex(def.Source, 0)
	require.NoError(t, err)
	require.Contains(t, def.DeclaredVariables, "param")
	require.Contains(t, def.DeclaredVariables, "type")

	foundAction := false
	for _, vars := range def.NodeVariableIndex {
		for _, v := range vars {
			if v.Name == "type" {
				require.Equal(t, []string{ActionUppercase}, v.Actions)
				foundAction = true
			}
		}
	}
	require.True(t, foundAction)
}

func TestRAML_makeTraitAndTraits(t *testing.T) {
	r := makeTestRAML(t)
	location := "test.raml"

	t.Run("makeTrait from scalar", func(t *testing.T) {
		trait, err := r.makeTrait(strNode("paged"), location)
		require.NoError(t, err)
		require.Equal(t, "paged", trait.Name)
		require.Nil(t, trait.Params)
	})

	t.Run("makeTrait from mapping with params", func(t *testing.T) {
		node := mapNode(
			strNode("paged"),
			mapNode(
				strNode("size"), strNode("10"),
				strNode("config"), mapNode(strNode("strict"), strNode("true")),
			),
		)
		trait, err := r.makeTrait(node, location)
		require.NoError(t, err)
		require.Equal(t, "paged", trait.Name)
		require.Contains(t, trait.Params, "size")
		require.Equal(t, "10", trait.Params["size"].Value)
		require.Equal(t, yaml.MappingNode, trait.Params["config"].Kind)
	})

	t.Run("makeTrait errors on invalid node", func(t *testing.T) {
		_, err := r.makeTrait(seqNode(), location)
		require.Error(t, err)
	})

	t.Run("makeTraits from scalar", func(t *testing.T) {
		traits, err := r.makeTraits(strNode("a"), location)
		require.NoError(t, err)
		require.Len(t, traits, 1)
		require.Equal(t, "a", traits[0].Name)
	})

	t.Run("makeTraits from sequence", func(t *testing.T) {
		traits, err := r.makeTraits(seqNode(strNode("a"), strNode("b")), location)
		require.NoError(t, err)
		require.Len(t, traits, 2)
		require.Equal(t, "a", traits[0].Name)
		require.Equal(t, "b", traits[1].Name)
	})

	t.Run("makeTraits null scalar returns nil", func(t *testing.T) {
		traits, err := r.makeTraits(&yaml.Node{Kind: yaml.ScalarNode, Tag: TagNull}, location)
		require.NoError(t, err)
		require.Nil(t, traits)
	})

	t.Run("makeTraits errors on invalid kind", func(t *testing.T) {
		_, err := r.makeTraits(mapNode(strNode("a"), strNode("b")), location)
		require.Error(t, err)
	})
}

func TestApplyTemplateAction(t *testing.T) {
	tests := []struct {
		name   string
		action string
		in     string
		want   string
	}{
		{name: "uppercase", action: ActionUppercase, in: "abc", want: "ABC"},
		{name: "lowercase", action: ActionLowercase, in: "AbC", want: "abc"},
		{name: "upper camel", action: ActionUpperCamelCase, in: "hello_world", want: "HelloWorld"},
		{name: "lower camel", action: ActionLowerCamelCase, in: "hello_world", want: "helloWorld"},
		{name: "upper underscore", action: ActionUpperUnderscore, in: "helloWorld", want: "HELLO_WORLD"},
		{name: "lower underscore", action: ActionLowerUnderscore, in: "helloWorld", want: "hello_world"},
		{name: "upper hyphen", action: ActionUpperHyphen, in: "helloWorld", want: "HELLO-WORLD"},
		{name: "lower hyphen", action: ActionLowerHyphen, in: "helloWorld", want: "hello-world"},
		{name: "singular", action: ActionSingularize, in: "people", want: "person"},
		{name: "plural", action: ActionPluralize, in: "company", want: "companies"},
		{name: "unknown returns input", action: "!unknown", in: "abc", want: "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, applyTemplateAction(tt.in, tt.action))
		})
	}
}

func TestSingularizeAndPluralize(t *testing.T) {
	t.Run("singularize irregular and regular words", func(t *testing.T) {
		require.Equal(t, "person", singularize("people"))
		require.Equal(t, "company", singularize("companies"))
		require.Equal(t, "box", singularize("boxes"))
	})

	t.Run("pluralize regular words", func(t *testing.T) {
		require.Equal(t, "companies", pluralize("company"))
		require.Equal(t, "buses", pluralize("bus"))
		require.Equal(t, "cars", pluralize("car"))
	})
}
