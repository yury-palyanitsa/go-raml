package raml

import (
	"errors"
	"fmt"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"

	"github.com/acronis/go-stacktrace"
)

const (
	ExampleValue = "value"
)

var ErrValueKeyNotFound = errors.New("value key not found")

func (ex *Example) decode(node *yaml.Node, valueNode *yaml.Node, location string) error {
	switch node.Value {
	case FacetStrict:
		sn, err := MakeScalarFacetYAML[bool](ex.raml, node, valueNode, location)
		if err != nil {
			return StacktraceNewWrapped("make scalar node", err, location, WithNodePosition(valueNode))
		}
		ex.Strict = sn
	case FacetDisplayName:
		sn, err := MakeScalarFacetYAML[string](ex.raml, node, valueNode, location)
		if err != nil {
			return StacktraceNewWrapped("make scalar node", err, location, WithNodePosition(valueNode))
		}
		ex.DisplayName = sn
	case FacetDescription:
		sn, err := MakeScalarFacetYAML[string](ex.raml, node, valueNode, location)
		if err != nil {
			return StacktraceNewWrapped("make scalar node", err, location, WithNodePosition(valueNode))
		}
		ex.Description = sn
	case FacetValue:
		n, err := ex.raml.makeRootNode(node, valueNode, location)
		if err != nil {
			return StacktraceNewWrapped("make node", err, location, WithNodePosition(valueNode))
		}
		ex.Data = n
	default:
		if IsCustomDomainExtensionNode(node.Value) {
			de, err := ex.raml.unmarshalCustomDomainExtension(location, node, valueNode)
			if err != nil {
				return StacktraceNewWrapped("unmarshal custom domain extension", err, location, WithNodePosition(valueNode))
			}
			ex.CustomDomainProperties.Set(de.Name, de)
		} else {
			return StacktraceNew("unknown field", location, WithNodePosition(node),
				stacktrace.WithInfo("field", node.Value))
		}
	}
	return nil
}

func (ex *Example) fill(location string, value *yaml.Node) error {
	var valueKey *yaml.Node
	// First lookup for the "value" key.
	for i := 0; i != len(value.Content); i += 2 {
		node := value.Content[i]
		valueNode := value.Content[i+1]
		if node.Value == ExampleValue {
			valueKey = valueNode
			break
		}
	}

	if valueKey == nil {
		return ErrValueKeyNotFound
	}

	// If "value" key is found, then the example is considered as a map with additional properties
	for i := 0; i != len(value.Content); i += 2 {
		node := value.Content[i]
		valueNode := value.Content[i+1]
		if err := ex.decode(node, valueNode, location); err != nil {
			return fmt.Errorf("decode example: %w", err)
		}
	}
	return nil
}

// makeExample creates an example from the given value node
func (r *RAML) makeExample(value *yaml.Node, name string, location string) (*Example, error) {
	ex := &Example{
		ID:                     r.generateSequenceID(),
		Name:                   name,
		Location:               location,
		KeyPos:                 NewNodePosition(value),
		ValuePos:               NewNodePosition(value),
		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
		raml:                   r,
	}
	// Example can be represented as map in two cases:
	// 1. A value with an example of ObjectShape.
	// 2. A map with the required "value" key that contains the actual example and additional properties of Example.
	if value.Kind == yaml.MappingNode {
		err := ex.fill(location, value)
		if err == nil {
			return ex, nil
		} else if !errors.Is(err, ErrValueKeyNotFound) {
			return nil, fmt.Errorf("fill example from mapping node: %w", err)
		}
	}
	// In all other cases, the example is considered as a value node
	n, err := r.makeRootNode(nil, value, location)
	if err != nil {
		return nil, StacktraceNewWrapped("make node", err, location, WithNodePosition(value))
	}
	ex.Data = n
	return ex, nil
}

// Example represents an example of a shape
type Example struct {
	ID          int64
	Name        string
	DisplayName *ScalarFacet[string]
	Description *ScalarFacet[string]
	Strict      *ScalarFacet[bool]
	Data        *DataNode

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}
