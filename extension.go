package raml

import (
	"gopkg.in/yaml.v3"

	"github.com/acronis/go-stacktrace"
)

type DomainExtension struct {
	ID        int64
	Name      string
	Extension *DataNode
	DefinedBy *BaseShape

	Location   string
	KeyPos     stacktrace.Position
	ValuePos   stacktrace.Position
	anchorFrag ReferenceResolver
	raml       *RAML
}

func (r *RAML) unmarshalCustomDomainExtension(
	location string,
	keyNode, valueNode *yaml.Node,
) (*DomainExtension, error) {
	name := keyNode.Value[1 : len(keyNode.Value)-1]
	if name == "" {
		return nil, StacktraceNew("annotation name must not be empty", location,
			WithNodePosition(keyNode))
	}
	n, err := r.makeRootNode(keyNode, valueNode, location)
	if err != nil {
		return nil, StacktraceNewWrapped("make node", err, location,
			WithNodePosition(valueNode))
	}
	de := &DomainExtension{
		ID:         r.generateSequenceID(),
		Name:       name,
		Extension:  n,
		Location:   location,
		KeyPos:     NewNodePosition(keyNode),
		ValuePos:   NewNodePosition(valueNode),
		anchorFrag: r.currentParseCtx().AnchorFrag,
		raml:       r,
	}
	r.domainExtensions = append(r.domainExtensions, de)
	return de, nil
}

func IsCustomDomainExtensionNode(name string) bool {
	return name != "" && name[0] == '(' && name[len(name)-1] == ')'
}
