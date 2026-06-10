package raml

import (
	"strconv"
	"strings"

	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

type HTTPAction interface {
	appendBody(k, v *yaml.Node, mediaType string) error
}

type Operation struct {
	ID int64

	DisplayName *ScalarFacet[string]
	Description *ScalarFacet[string]

	Traits []*Trait

	Protocols []*Node[string]
	SecuredBy []*SecurityScheme
	// explicitSecuredBy is true when the operation's own RAML source explicitly
	// declared a securedBy facet (as opposed to inheriting the API-level default).
	// Used during trait/resource-type merge to avoid duplicating the global
	// securedBy onto operations that never declared their own.
	explicitSecuredBy bool

	// RTTraits holds traits contributed by the applied resource type (method-level).
	// These are kept separate from Traits (method's own) to preserve the correct
	// application order defined by the spec: method → resource → RT-method → RT-resource.
	RTTraits []*Trait

	Method          string
	Headers         *orderedmap.OrderedMap[string, Property]
	QueryParameters *orderedmap.OrderedMap[string, Property]
	QueryString     *BaseShape
	Request         *Request
	Responses       *orderedmap.OrderedMap[int, *Response]

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}

func (r *RAML) unmarshalHeaders(node *yaml.Node, location string) (*orderedmap.OrderedMap[string, Property], error) {
	location = r.locationOf(node, location)
	if node.Tag == TagNull {
		return nil, nil
	} else if node.Kind != yaml.MappingNode {
		return nil, StacktraceNew("headers must be a mapping node", location, WithNodePosition(node))
	}

	headers := orderedmap.New[string, Property](len(node.Content) / 2)
	for j := 0; j != len(node.Content); j += 2 {
		keyNode := node.Content[j]
		valueNode := node.Content[j+1]

		entryLoc := r.locationOf(valueNode, location)
		propertyName, hasImplicitOptional := chompImplicitOptional(keyNode.Value)
		property, err := r.makeProperty(keyNode, valueNode, propertyName, entryLoc, hasImplicitOptional)
		if err != nil {
			return nil, StacktraceNewWrapped("make property", err, entryLoc,
				WithNodePosition(keyNode))
		}
		headers.Set(property.Name, property)
		r.PutTypeDefinitionIntoFragment(entryLoc, property.Base)
	}
	return headers, nil
}

func (r *RAML) unmarshalQueryParameters(node *yaml.Node, location string) (*orderedmap.OrderedMap[string, Property], error) {
	location = r.locationOf(node, location)
	if node.Tag == TagNull {
		return nil, nil
	} else if node.Kind != yaml.MappingNode {
		return nil, StacktraceNew("queryParameters must be a mapping node", location, WithNodePosition(node))
	}

	queryParameters := orderedmap.New[string, Property](len(node.Content) / 2)
	for j := 0; j != len(node.Content); j += 2 {
		keyNode := node.Content[j]
		valueNode := node.Content[j+1]

		entryLoc := r.locationOf(valueNode, location)
		propertyName, hasImplicitOptional := chompImplicitOptional(keyNode.Value)
		property, err := r.makeProperty(keyNode, valueNode, propertyName, entryLoc, hasImplicitOptional)
		if err != nil {
			return nil, StacktraceNewWrapped("make property", err, entryLoc,
				WithNodePosition(keyNode))
		}
		queryParameters.Set(property.Name, property)
		r.PutTypeDefinitionIntoFragment(entryLoc, property.Base)
	}
	return queryParameters, nil
}

func (r *RAML) unmarshalQueryString(k, v *yaml.Node, location string) (*BaseShape, error) {
	location = r.locationOf(v, location)
	shape, err := r.makeNewShapeYAML(k, v, location)
	if err != nil {
		return nil, StacktraceNewWrapped("make new shape yaml", err, location, WithNodePosition(k))
	}
	r.PutTypeDefinitionIntoFragment(location, shape)
	return shape, nil
}

type Request struct {
	ID int64

	Bodies *orderedmap.OrderedMap[string, *Body]

	// NOTE: Request cannot have custom annotations, those are defined on Operation level in RAML.

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}

func (r *RAML) makeRequest(k, v *yaml.Node, location string) (*Request, error) {
	location = r.locationOf(v, location)
	request := &Request{
		ID:     r.generateSequenceID(),
		Bodies: orderedmap.New[string, *Body](),

		raml:     r,
		KeyPos:   NewNodePosition(k),
		ValuePos: NewNodePosition(v),
		Location: location,
	}

	r.storeEntityNode(request.ID, k, v)

	if err := r.decodeMediaTypeNode(request, k, v, location); err != nil {
		return nil, StacktraceNewWrapped("decode media type node", err, location, WithNodePosition(v))
	}

	return request, nil
}

func (r *Request) appendBody(k, v *yaml.Node, mediaType string) error {
	body, err := r.raml.makeBody(k, v, r.Location, mediaType)
	if err != nil {
		return StacktraceNewWrapped("make body", err, r.Location, WithNodePosition(k))
	}
	r.Bodies.Set(mediaType, body)
	return nil
}

func (r *RAML) decodeMediaTypeNode(action HTTPAction, k, v *yaml.Node, location string) error {
	// TODO: Common for request/responses
	switch v.Kind {
	case yaml.ScalarNode:
		if v.Tag == TagNull {
			return nil
		}
		if r.globalMediaType == nil {
			return StacktraceNew("explicit media type is required", location, WithNodePosition(k))
		}
		for _, mediaType := range r.globalMediaType {
			if err := action.appendBody(k, v, mediaType); err != nil {
				return StacktraceNewWrapped("append request body", err, location, WithNodePosition(k))
			}
		}
	case yaml.MappingNode:
		mediaTypeNodes, err := r.collectMediaTypes(v, location)
		if err != nil {
			return StacktraceNewWrapped("collect media types", err, location, WithNodePosition(k))
		}
		if mediaTypeNodes == nil {
			if r.globalMediaType == nil {
				return StacktraceNew("explicit media type is required", location, WithNodePosition(k))
			}
			for _, mediaType := range r.globalMediaType {
				if err = action.appendBody(k, v, mediaType); err != nil {
					return StacktraceNewWrapped("append request body", err, location, WithNodePosition(k))
				}
			}
		} else {
			for i := 0; i != len(mediaTypeNodes); i += 2 {
				keyNode := mediaTypeNodes[i]
				valueNode := mediaTypeNodes[i+1]
				if err = action.appendBody(keyNode, valueNode, keyNode.Value); err != nil {
					return StacktraceNewWrapped("append request body", err, location, WithNodePosition(keyNode))
				}
			}
		}
	default:
		return StacktraceNew("request must be either scalar or mapping node", location, WithNodePosition(k))
	}
	return nil
}

func (r *RAML) collectMediaTypes(node *yaml.Node, location string) ([]*yaml.Node, error) {
	var foreignNodes []*yaml.Node
	var mediaTypes []*yaml.Node
	for i := 0; i != len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		if strings.IndexByte(keyNode.Value, '/') == -1 {
			foreignNodes = append(foreignNodes, keyNode)
		} else {
			mediaTypes = append(mediaTypes, keyNode, valueNode)
		}
	}

	// If there are media types and there are foreign nodes, then it is an error.
	if mediaTypes != nil && len(foreignNodes) > 0 {
		st := StacktraceNew("Found unexpected keys instead of valid media types", location, WithNodePosition(node))
		for _, fNode := range foreignNodes {
			st = st.Append(StacktraceNew("Unexpected key", location, WithNodePosition(fNode)))
		}
		return nil, st
	}

	return mediaTypes, nil
}

type Response struct {
	ID int64

	DisplayName string
	Description string

	StatusCode int
	Headers    *orderedmap.OrderedMap[string, Property]
	Bodies     *orderedmap.OrderedMap[string, *Body]

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	stacktrace.Position
	raml *RAML
}

func (r *RAML) makeResponses(node *yaml.Node, location string) (*orderedmap.OrderedMap[int, *Response], error) {
	location = r.locationOf(node, location)
	if node.Tag == TagNull {
		return nil, nil
	} else if node.Kind != yaml.MappingNode {
		return nil, StacktraceNew("responses must be a mapping node", location, WithNodePosition(node))
	}

	responses := orderedmap.New[int, *Response](len(node.Content) / 2)
	for j := 0; j != len(node.Content); j += 2 {
		statusCode := node.Content[j]
		data := node.Content[j+1]

		entryLoc := r.locationOf(data, location)
		if !IsStatusCode(statusCode.Value) {
			return nil, StacktraceNew("status code must be a 3-digit number", entryLoc, WithNodePosition(statusCode))
		}
		intStatusCode, err := strconv.Atoi(statusCode.Value)
		if err != nil {
			return nil, StacktraceNewWrapped("parse status code", err, entryLoc, WithNodePosition(statusCode))
		}

		// Check for duplicate status codes
		if _, ok := responses.Get(intStatusCode); ok {
			return nil, StacktraceNew("duplicate status code", entryLoc, WithNodePosition(statusCode))
		}

		response, err := r.makeResponse(statusCode, data, entryLoc, intStatusCode)
		if err != nil {
			return nil, StacktraceNewWrapped("make response", err, entryLoc, WithNodePosition(data))
		}
		responses.Set(intStatusCode, response)
	}
	return responses, nil
}

func (r *RAML) makeResponse(k, v *yaml.Node, location string, statusCode int) (*Response, error) {
	location = r.locationOf(v, location)
	response := &Response{
		ID:         r.generateSequenceID(),
		StatusCode: statusCode,
		Headers:    orderedmap.New[string, Property](0),
		Bodies:     orderedmap.New[string, *Body](0),

		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),

		raml:     r,
		Position: stacktrace.Position{Line: k.Line, Column: k.Column, EndLine: NodeEndLine(v)},
		Location: location,
	}

	r.storeEntityNode(response.ID, k, v)

	if err := response.decode(v); err != nil {
		return nil, StacktraceNewWrapped("decode response", err, location, WithNodePosition(v))
	}

	return response, nil
}

func (r *Response) decode(node *yaml.Node) error {
	if node.Tag == TagNull {
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("responses must be a mapping node", r.Location, WithNodePosition(node))
	}

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		switch keyNode.Value {
		case FacetDisplayName:
			if err := valueNode.Decode(&r.DisplayName); err != nil {
				return StacktraceNewWrapped("decode displayName", err, r.Location, WithNodePosition(valueNode))
			}
		case FacetDescription:
			if err := valueNode.Decode(&r.Description); err != nil {
				return StacktraceNewWrapped("decode description", err, r.Location, WithNodePosition(valueNode))
			}
		case FacetHeaders:
			headers, err := r.raml.unmarshalHeaders(valueNode, r.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal headers", err, r.Location, WithNodePosition(valueNode))
			}
			r.Headers = headers
		case FacetBody:
			if err := r.raml.decodeMediaTypeNode(r, keyNode, valueNode, r.Location); err != nil {
				return StacktraceNewWrapped("decode media type node", err, r.Location, WithNodePosition(valueNode))
			}
		default:
			if IsCustomDomainExtensionNode(keyNode.Value) {
				de, err := r.raml.unmarshalCustomDomainExtension(r.Location, keyNode, valueNode)
				if err != nil {
					return StacktraceNewWrapped("unmarshal custom domain extension", err, r.Location, WithNodePosition(valueNode))
				}
				r.CustomDomainProperties.Set(de.Name, de)
			} else {
				return StacktraceNew("unknown field", r.Location, WithNodePosition(keyNode), stacktrace.WithInfo("field", keyNode.Value))
			}
		}
	}
	return nil
}

func (r *Response) appendBody(k, v *yaml.Node, mediaType string) error {
	body, err := r.raml.makeBody(k, v, r.Location, mediaType)
	if err != nil {
		return StacktraceNewWrapped("make body", err, r.Location, WithNodePosition(k))
	}
	r.Bodies.Set(mediaType, body)
	return nil
}

func IsStatusCode(value string) bool {
	return len(value) == 3 &&
		value[0] >= '1' && value[0] <= '5' &&
		value[1] >= '0' && value[1] <= '9' &&
		value[2] >= '0' && value[2] <= '9'
}

type Body struct {
	ID int64

	MediaType string
	Shape     *BaseShape // can be null!

	// NOTE: Body cannot have annotations. Those are defined on Shape level in RAML.

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}

func (r *RAML) makeBody(k, v *yaml.Node, location string, mediaType string) (*Body, error) {
	location = r.locationOf(v, location)
	body := &Body{
		ID:        r.generateSequenceID(),
		MediaType: mediaType,
		raml:      r,
		KeyPos:    NewNodePosition(k),
		ValuePos:  NewNodePosition(v),
		Location:  location,
	}

	r.storeEntityNode(body.ID, k, v)

	if err := body.decode(k, v); err != nil {
		return nil, StacktraceNewWrapped("decode body", err, location, WithNodePosition(v))
	}

	return body, nil
}

func (b *Body) decode(k, v *yaml.Node) error {
	shape, err := b.raml.makeNewBodyShapeYAML(k, v, b.Location)
	if err != nil {
		return StacktraceNewWrapped("make new shape yaml", err, b.Location, WithNodePosition(v))
	}
	b.Shape = shape
	b.raml.PutTypeDefinitionIntoFragment(b.Location, shape)
	return nil
}
