package raml

import (
	"gopkg.in/yaml.v3"
)

type CustomDomainProperties map[string]*DomainExtension

func (c *CustomDomainProperties) clone(cloning *_cloning) CustomDomainProperties {
	if c == nil {
		return nil
	}
	if cloned, ok := cloning.cloned[c]; ok {
		return cloned.(CustomDomainProperties)
	}
	clone := make(CustomDomainProperties, len(*c))
	for k, v := range *c {
		clone[k] = v.clone(cloning)
	}
	cloning.cloned[c] = clone
	return clone
}

type CustomShapeFacets map[string]*Node

func (c *CustomShapeFacets) clone(cloning *_cloning) CustomShapeFacets {
	if c == nil {
		return nil
	}
	if cloned, ok := cloning.cloned[c]; ok {
		return cloned.(CustomShapeFacets)
	}
	clone := make(CustomShapeFacets, len(*c))
	for k, v := range *c {
		clone[k] = v.clone(cloning)
	}
	cloning.cloned[c] = clone
	return clone
}

type CustomShapeFacetDefinitions map[string]Property // Object properties share the same syntax with custom shape facets.

func (c *CustomShapeFacetDefinitions) clone(cloning *_cloning) CustomShapeFacetDefinitions {
	if c == nil {
		return nil
	}
	if cloned, ok := cloning.cloned[c]; ok {
		return cloned.(CustomShapeFacetDefinitions)
	}
	clone := make(CustomShapeFacetDefinitions, len(*c))
	for k, v := range *c {
		clone[k] = *v.clone(cloning)
	}
	cloning.cloned[c] = clone
	return clone
}

type DomainExtension struct {
	Id        string
	Name      string
	Extension *Node
	DefinedBy *Shape

	Location string
	Position
	raml *RAML
}

func (e *DomainExtension) clone(cloning *_cloning) *DomainExtension {
	if cloned, ok := cloning.cloned[e]; ok {
		return cloned.(*DomainExtension)
	}
	clone := &DomainExtension{
		Id:        e.Id,
		Name:      e.Name,
		Extension: e.Extension.clone(cloning),
		Location:  e.Location,
		Position:  e.Position,
		raml:      cloning.raml,
	}
	if e.DefinedBy != nil {
		clonedShape := (*e.DefinedBy).clone(cloning)
		clone.DefinedBy = &clonedShape
	}
	cloning.cloned[e] = clone
	return clone
}

func (r *RAML) unmarshalCustomDomainExtension(location string, keyNode *yaml.Node, valueNode *yaml.Node) (string, *DomainExtension, error) {
	name := keyNode.Value[1 : len(keyNode.Value)-1]
	if name == "" {
		return "", nil, NewError("annotation name must not be empty", location, WithNodePosition(keyNode))
	}
	n, err := r.makeNode(valueNode, location)
	if err != nil {
		return "", nil, NewWrappedError("make node", err, location, WithNodePosition(valueNode))
	}
	de := &DomainExtension{
		Name:      name,
		Extension: n,
		Location:  location,
		Position:  Position{keyNode.Line, keyNode.Column},
		raml:      r,
	}
	r.domainExtensions = append(r.domainExtensions, de)
	return name, de, nil
}

func IsCustomDomainExtensionNode(name string) bool {
	return name != "" && name[0] == '(' && name[len(name)-1] == ')'
}
