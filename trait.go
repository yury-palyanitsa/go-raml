package raml

import (
	"strings"

	"github.com/acronis/go-stacktrace"
	gopluralize "github.com/gertd/go-pluralize"
	"gopkg.in/yaml.v3"
)

// pluralizeClient powers the RAML !singularize / !pluralize template actions.
// go-pluralize already covers the bulk of our historic irregular list, but it
// treats medium/media as uncountable and has no rules for memorandum/memoranda
// or vortex/vortices. We restore parity for those three pairs because the RAML
// 1.0 TCK fixtures (e.g. ResourceTypes/chaining-functions) and existing
// generated artefacts depend on them inflecting.
var pluralizeClient = func() *gopluralize.Client {
	c := gopluralize.NewClient()
	c.AddIrregularRule("medium", "media")
	c.AddIrregularRule("memorandum", "memoranda")
	c.AddIrregularRule("vortex", "vortices")
	return c
}()

type VariableInfo struct {
	Name      string
	Substring string
	Actions   []string
}

// Trait must be indexed YAML nodes with special unmarshalling logic since this trait is an Operation template.
type TraitDefinition struct {
	ID int64

	// Name is the declared trait name (the key in the traits: map, e.g. "pageable").
	Name string

	Usage *Node[string]

	Source            *yaml.Node
	DeclaredVariables map[string]struct{}
	NodeVariableIndex map[int][]VariableInfo

	Link *TraitFragment

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	// anchorFrag is the fragment scope at this trait's declaration site: it both
	// resolves unqualified type names in shapes produced by this trait's compiled
	// operations and governs trait-name resolution inside its is: entries.
	anchorFrag ReferenceResolver
	raml       *RAML
}

func (r *RAML) makeTraitDefinition(keyNode, valueNode *yaml.Node, location string) (*TraitDefinition, error) {
	keyPos := NewNodePosition(valueNode)
	if keyNode != nil {
		keyPos = NewNodePosition(keyNode)
	}
	pctx := r.currentParseCtx()
	traitDef := &TraitDefinition{
		ID:                r.generateSequenceID(),
		DeclaredVariables: make(map[string]struct{}),
		NodeVariableIndex: make(map[int][]VariableInfo),

		raml:       r,
		KeyPos:     keyPos,
		ValuePos:   NewNodePosition(valueNode),
		Location:   location,
		anchorFrag: pctx.AnchorFrag,
	}
	if keyNode != nil {
		traitDef.Name = keyNode.Value
	}

	r.storeEntityNode(traitDef.ID, keyNode, valueNode)

	if err := traitDef.decode(valueNode); err != nil {
		return nil, StacktraceNewWrapped("decode trait definition", err, location, WithNodePosition(valueNode))
	}

	if traitDef.Source != nil {
		if err := traitDef.collectVariablesIndex(traitDef.Source, 0); err != nil {
			return nil, StacktraceNewWrapped("collect variables index", err, location, WithNodePosition(traitDef.Source))
		}
	}

	return traitDef, nil
}

func (t *TraitDefinition) decode(node *yaml.Node) error {
	if node.Tag == TagNull {
		return nil
	} else if node.Tag == TagInclude {
		traitFrag, err := t.raml.parseTraitFragment(t.raml.noteIncludeRef(node, t.Location))
		if err != nil {
			return StacktraceNewWrapped("parse trait fragment", err, t.Location, WithNodePosition(node))
		}
		t.Link = traitFrag
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("trait definition must be a mapping node", t.Location, WithNodePosition(node))
	}

	content := make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		fragmentPath, rn, err := t.raml.resolveInclude(valueNode, t.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve include", err, t.Location, WithNodePosition(valueNode))
		}
		switch keyNode.Value {
		case FacetUsage:
			var usage string
			if err := rn.Decode(&usage); err != nil {
				return StacktraceNewWrapped("decode usage", err, t.Location, WithNodePosition(valueNode))
			}
			t.Usage = MakeNode(usage, keyNode, valueNode, t.Location, fragmentPath)
		default:
			content = append(content, keyNode, valueNode)
		}
	}
	t.Source = &yaml.Node{
		Kind:    node.Kind,
		Tag:     node.Tag,
		Content: content,
		Line:    node.Line,
		Column:  node.Column,
	}
	return nil
}

func (t *TraitDefinition) collectVariablesIndex(node *yaml.Node, idx int) error {
	return collectVariablesIndex(t.Location, node, idx, t.NodeVariableIndex, t.DeclaredVariables)
}

type Trait struct {
	ID int64

	Name   string
	Params map[string]*yaml.Node

	// Definition is set during trait application and points to the resolved TraitDefinition.
	Definition *TraitDefinition

	Location string
	ValuePos stacktrace.Position
	// anchorFrag is the fragment scope at this trait reference's site; its uses:
	// map governs resolution of dotted trait names (e.g. "traitsLib.pageable").
	anchorFrag ReferenceResolver
	raml       *RAML
}

func (r *RAML) makeTraits(valueNode *yaml.Node, location string) ([]*Trait, error) {
	switch valueNode.Kind {
	case yaml.ScalarNode:
		if valueNode.Tag == TagNull {
			return nil, nil
		}
		trait, err := r.makeTrait(valueNode, location)
		if err != nil {
			return nil, StacktraceNewWrapped("make trait", err, location, WithNodePosition(valueNode))
		}
		return []*Trait{trait}, nil
	case yaml.SequenceNode:
		traits := make([]*Trait, len(valueNode.Content))
		for i, node := range valueNode.Content {
			trait, err := r.makeTrait(node, location)
			if err != nil {
				return nil, StacktraceNewWrapped("make trait", err, location, WithNodePosition(node))
			}
			traits[i] = trait
		}
		return traits, nil
	default:
		return nil, StacktraceNew("traits must be either sequence or scalar node", location, WithNodePosition(valueNode))
	}
}

func (r *RAML) makeTrait(valueNode *yaml.Node, location string) (*Trait, error) {
	pctx := r.currentParseCtx()
	trait := &Trait{
		ID:         r.generateSequenceID(),
		Location:   location,
		anchorFrag: pctx.AnchorFrag,
		raml:       r,
		ValuePos:   NewNodePosition(valueNode),
	}

	if err := trait.decode(valueNode); err != nil {
		return nil, StacktraceNewWrapped("decode trait", err, location, WithNodePosition(valueNode))
	}

	return trait, nil
}

func (t *Trait) decode(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		t.Name = node.Value
	case yaml.MappingNode:
		keyNode := node.Content[0]
		valueNode := node.Content[1]
		t.Name = keyNode.Value
		// Store parameter values as YAML nodes to support complex values (mappings, sequences)
		t.Params = make(map[string]*yaml.Node, len(valueNode.Content)/2)
		for i := 0; i < len(valueNode.Content); i += 2 {
			key := valueNode.Content[i]
			val := valueNode.Content[i+1]
			t.Params[key.Value] = val
		}
	default:
		return StacktraceNew("trait must be either scalar or mapping node", t.Location, WithNodePosition(node))
	}
	return nil
}

// applyTemplateAction applies one of the RAML-specified string transformation functions
// to value. If action is empty or unrecognised, value is returned unchanged.
func applyTemplateAction(value, action string) string {
	switch action {
	case ActionUppercase:
		return strings.ToUpper(value)
	case ActionLowercase:
		return strings.ToLower(value)
	case ActionUpperCamelCase:
		return toUpperCamelCase(value)
	case ActionLowerCamelCase:
		return toLowerCamelCase(value)
	case ActionUpperUnderscore:
		return strings.ToUpper(toUnderscoreCase(value))
	case ActionLowerUnderscore:
		return toUnderscoreCase(value)
	case ActionUpperHyphen:
		return strings.ToUpper(toHyphenCase(value))
	case ActionLowerHyphen:
		return toHyphenCase(value)
	case ActionSingularize:
		return singularize(value)
	case ActionPluralize:
		return pluralize(value)
	default:
		return value
	}
}

func toUpperCamelCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	upperNext := true

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch c {
		case ' ', '_', '-':
			upperNext = true
			continue
		}

		if upperNext {
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			upperNext = false
		} else {
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
		}

		b.WriteByte(c)
	}

	return b.String()
}

func toLowerCamelCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	upperNext := false
	first := true

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch c {
		case ' ', '_', '-':
			upperNext = true
			continue
		}

		if first {
			first = false
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
		} else if upperNext {
			upperNext = false
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
		} else {
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
		}

		b.WriteByte(c)
	}

	return b.String()
}

func toUnderscoreCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)

	for i := 0; i < len(s); i++ {
		c := s[i]

		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			c += 'a' - 'A'
		}

		b.WriteByte(c)
	}

	return b.String()
}

func toHyphenCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)

	for i := 0; i < len(s); i++ {
		c := s[i]

		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			c += 'a' - 'A'
		}

		b.WriteByte(c)
	}

	return b.String()
}

func singularize(s string) string {
	if s == "" {
		return s
	}
	return pluralizeClient.Singular(s)
}

func pluralize(s string) string {
	if s == "" {
		return s
	}
	return pluralizeClient.Plural(s)
}
