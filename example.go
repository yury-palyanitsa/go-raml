package raml

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// makeExample creates an example from the given value node
func (r *RAML) makeExample(value *yaml.Node, name string, location string) (*Example, error) {
	// TODO: Example can be either a scalar or a map
	n, err := r.makeNode(value, location)
	if err != nil {
		return nil, fmt.Errorf("make node: %w", err)
	}
	return &Example{
		Name:     name,
		Value:    n,
		Location: location,
		Position: Position{Line: value.Line, Column: value.Column},
		raml:     r,
	}, nil
}

// Example represents an example of a shape
type Example struct {
	Id          string
	Name        string
	DisplayName string
	Description string
	Value       *Node

	CustomDomainProperties CustomDomainProperties

	Location string
	Position
	raml *RAML
}

func (e *Example) clone(cloning *_cloning) *Example {
	if e == nil {
		return nil
	}
	if cloned, ok := cloning.cloned[e]; ok {
		return cloned.(*Example)
	}
	clone := &Example{
		Id:          e.Id,
		Name:        e.Name,
		DisplayName: e.DisplayName,
		Description: e.Description,
		Value:       e.Value,
		Location:    e.Location,
		Position:    e.Position,
		raml:        cloning.raml,
	}
	if e.CustomDomainProperties != nil {
		clone.CustomDomainProperties = make(CustomDomainProperties, len(e.CustomDomainProperties))
		for k, v := range e.CustomDomainProperties {
			clone.CustomDomainProperties[k] = v.clone(cloning)
		}
	}
	cloning.cloned[e] = clone
	return clone
}
