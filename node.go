package raml

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Nodes []*Node

func (n *Nodes) clone(cloning *_cloning) Nodes {
	if n == nil {
		return nil
	}
	if cloned, ok := cloning.cloned[n]; ok {
		return cloned.(Nodes)
	}
	cloned := make(Nodes, 0)
	for _, node := range *n {
		cloned = append(cloned, node.clone(cloning))
	}
	cloning.cloned[n] = cloned
	return cloned
}

func (n Nodes) String() string {
	vals := make([]string, len(n))
	for i, node := range n {
		vals[i] = node.String()
	}
	return strings.Join(vals, ", ")
}

type Node struct {
	Id    string
	Value any

	Link *Node

	Location string
	Position
	raml *RAML
}

func (n *Node) clone(cloning *_cloning) *Node {
	if n == nil {
		return nil
	}
	if cloned, ok := cloning.cloned[n]; ok {
		return cloned.(*Node)
	}
	clone := &Node{
		Id:       n.Id,
		Value:    n.Value,
		Location: n.Location,
		Position: n.Position,
		raml:     cloning.raml,
	}
	if n.Link != nil {
		clone.Link = n.Link.clone(cloning)
	}
	cloning.cloned[n] = clone
	return clone
}

func (n *Node) String() string {
	return fmt.Sprintf("%v", n.Value)
}

func (r *RAML) makeNode(node *yaml.Node, location string) (*Node, error) {
	n := &Node{Location: location, Position: Position{node.Line, node.Column}, raml: r}

	switch node.Kind {
	default:
		return nil, NewError("unexpected kind", location, WithInfo("node.kind", Stringer(node.Kind)), WithNodePosition(node))
	case yaml.ScalarNode:
		switch node.Tag {
		default:
			if err := node.Decode(&n.Value); err != nil {
				return nil, NewWrappedError("decode scalar", err, location, WithNodePosition(node))
			}
		case "!!str":
			if node.Value != "" && node.Value[0] == '{' {
				if err := json.Unmarshal([]byte(node.Value), &n.Value); err != nil {
					return nil, NewWrappedError("json unmarshal", err, location, WithNodePosition(node))
				}
			} else {
				n.Value = node.Value
			}
		// TODO: In case with includes that are explicitly required to be string value, probably need to introduce a new tag.
		// !includestr sounds like a good candidate.
		case "!include":
			baseDir := filepath.Dir(location)
			fragmentPath := filepath.Join(baseDir, node.Value)
			// TODO: Need to refactor and move out IO logic from this function.
			r, err := ReadRawFile(filepath.Join(baseDir, node.Value))
			if err != nil {
				return nil, NewWrappedError("include: read raw file", err, fragmentPath)
			}
			defer func(r io.ReadCloser) {
				err = r.Close()
				if err != nil {
					log.Fatal(fmt.Errorf("close file error: %w", err))
				}
			}(r)
			// TODO: This logic should be more complex because content type may depend on the header reported by remote server.
			link := &Node{Location: fragmentPath}
			ext := filepath.Ext(node.Value)
			if ext == ".json" {
				d := json.NewDecoder(r)
				if err := d.Decode(&link.Value); err != nil {
					return nil, NewWrappedError("include: json decode", err, link.Location)
				}
			} else if ext == ".yaml" || ext == ".yml" {
				d := yaml.NewDecoder(r)
				if err := d.Decode(&link.Value); err != nil {
					return nil, NewWrappedError("include: yaml decode", err, link.Location)
				}
			} else {
				v, err := io.ReadAll(r)
				if err != nil {
					return nil, NewWrappedError("include: read all", err, link.Location)
				}
				link.Value = v
			}
			n.Link = link
		}
		return n, nil
	case yaml.MappingNode:
		if err := node.Decode(&n.Value); err != nil {
			return nil, NewWrappedError("decode mapping", err, location, WithNodePosition(node))
		}
	case yaml.SequenceNode:
		if err := node.Decode(&n.Value); err != nil {
			return nil, NewWrappedError("decode sequence", err, location, WithNodePosition(node))
		}
	}
	return n, nil
}
