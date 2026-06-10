package raml

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

type SecuritySchemeDefinition struct {
	ID   int64
	Name string

	Type        *ScalarFacet[SecuritySchemeType]
	DisplayName *ScalarFacet[string]
	Description *ScalarFacet[string]
	Settings    SecuritySchemeSettings
	DescribedBy *SecuritySchemeDescription

	Link *SecuritySchemeFragment

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}

func (r *RAML) makeSecuritySchemeDefinition(keyNode, node *yaml.Node, location string) (*SecuritySchemeDefinition, error) {
	keyPos := NewNodePosition(node)
	if keyNode != nil {
		keyPos = NewNodePosition(keyNode)
	}
	securitySchemeDef := &SecuritySchemeDefinition{
		ID:                     r.generateSequenceID(),
		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),

		Location: location,
		KeyPos:   keyPos,
		ValuePos: NewNodePosition(node),
		raml:     r,
	}
	if keyNode != nil {
		securitySchemeDef.Name = keyNode.Value
	}

	r.storeEntityNode(securitySchemeDef.ID, keyNode, node)

	if err := securitySchemeDef.decode(node); err != nil {
		return nil, StacktraceNewWrapped("decode security scheme definition", err, location, WithNodePosition(node))
	}

	return securitySchemeDef, nil
}

func (r *RAML) makeNullSecuritySchemeDefinition(node *yaml.Node, location string) *SecuritySchemeDefinition {
	return &SecuritySchemeDefinition{
		ID:       r.generateSequenceID(),
		Type:     &ScalarFacet[SecuritySchemeType]{Value: NullAuthType},
		Settings: &NullAuthScheme{Location: location},

		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),

		Location: location,
		KeyPos:   NewNodePosition(node),
		ValuePos: NewNodePosition(node),
		raml:     r,
	}
}

func (ssd *SecuritySchemeDefinition) decode(node *yaml.Node) error {
	if node.Tag == TagInclude {
		securitySchemeFrag, err := ssd.raml.parseSecuritySchemeFragment(ssd.raml.noteIncludeRef(node, ssd.Location))
		if err != nil {
			return StacktraceNewWrapped("parse security scheme fragment", err, ssd.Location, WithNodePosition(node))
		}
		ssd.Link = securitySchemeFrag
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("security scheme definition must be a mapping node", ssd.Location, WithNodePosition(node))
	}

	var typeNode *yaml.Node
	var settingsNode *yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		switch keyNode.Value {
		case FacetType:
			sn, err := MakeScalarFacetYAML[SecuritySchemeType](ssd.raml, keyNode, valueNode, ssd.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, ssd.Location, WithNodePosition(valueNode))
			}
			ssd.Type = sn
			typeNode = valueNode
		case FacetDisplayName:
			sn, err := MakeScalarFacetYAML[string](ssd.raml, keyNode, valueNode, ssd.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, ssd.Location, WithNodePosition(valueNode))
			}
			ssd.DisplayName = sn
		case FacetDescription:
			sn, err := MakeScalarFacetYAML[string](ssd.raml, keyNode, valueNode, ssd.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, ssd.Location, WithNodePosition(valueNode))
			}
			ssd.Description = sn
		case FacetDescribedBy:
			describedBy, err := ssd.raml.makeSecuritySchemeDescription(ssd.Location, keyNode, valueNode)
			if err != nil {
				return StacktraceNewWrapped("make security scheme description", err, ssd.Location)
			}
			ssd.DescribedBy = describedBy
		case FacetSettings:
			settingsNode = valueNode
		default:
			if IsCustomDomainExtensionNode(keyNode.Value) {
				de, err := ssd.raml.unmarshalCustomDomainExtension(ssd.Location, keyNode, valueNode)
				if err != nil {
					return StacktraceNewWrapped("unmarshal custom domain extension", err, ssd.Location, WithNodePosition(valueNode))
				}
				ssd.CustomDomainProperties.Set(de.Name, de)
			} else {
				return StacktraceNew("unknown key in security scheme definition", ssd.Location, WithNodePosition(keyNode), stacktrace.WithInfo("key", keyNode.Value))
			}
		}
	}
	if typeNode == nil {
		return StacktraceNew("security scheme definition must have a type", ssd.Location, stacktrace.WithPosition(&ssd.KeyPos))
	}

	settings, err := ssd.raml.MakeSecuritySchemeSettings(ssd.Type.Value, typeNode, settingsNode, ssd.Location)
	if err != nil {
		return StacktraceNewWrapped("make security scheme settings", err, ssd.Location)
	}
	ssd.Settings = settings

	return nil
}

type SecuritySchemeDescription struct {
	ID int64

	Headers         *orderedmap.OrderedMap[string, Property]
	QueryParameters *orderedmap.OrderedMap[string, Property] // TODO: Maybe can be combined?
	QueryString     *BaseShape
	Responses       *orderedmap.OrderedMap[int, *Response]

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	stacktrace.Position
	raml *RAML
}

func (r *RAML) makeSecuritySchemeDescription(location string, keyNode, valueNode *yaml.Node) (*SecuritySchemeDescription, error) {
	ssd := &SecuritySchemeDescription{
		Headers:   orderedmap.New[string, Property](0),
		Responses: orderedmap.New[int, *Response](0),

		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
		Location:               location,
		raml:                   r,
	}

	r.storeEntityNode(ssd.ID, keyNode, valueNode)

	if err := ssd.decode(valueNode); err != nil {
		return nil, StacktraceNewWrapped("decode security scheme description", err, location)
	}

	return ssd, nil
}

func (o *SecuritySchemeDescription) decode(node *yaml.Node) error {
	if node.Tag == TagNull {
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("security scheme describedBy must be a mapping node", o.Location, WithNodePosition(node))
	}

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		switch keyNode.Value {
		case FacetHeaders:
			headers, err := o.raml.unmarshalHeaders(valueNode, o.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal headers", err, o.Location, WithNodePosition(valueNode))
			}
			o.Headers = headers
		case FacetQueryParameters:
			if o.QueryString != nil {
				return StacktraceNew("queryParameters and queryString are mutually exclusive", o.Location, WithNodePosition(valueNode))
			}
			params, err := o.raml.unmarshalQueryParameters(valueNode, o.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal query parameters", err, o.Location, WithNodePosition(valueNode))
			}
			o.QueryParameters = params
		case FacetQueryString:
			if o.QueryParameters != nil {
				return StacktraceNew("queryParameters and queryString are mutually exclusive", o.Location, WithNodePosition(valueNode))
			}
			shape, err := o.raml.unmarshalQueryString(keyNode, valueNode, o.Location)
			if err != nil {
				return StacktraceNewWrapped("unmarshal query string", err, o.Location, WithNodePosition(valueNode))
			}
			o.QueryString = shape
		case FacetResponses:
			responses, err := o.raml.makeResponses(valueNode, o.Location)
			if err != nil {
				return StacktraceNewWrapped("make responses", err, o.Location, WithNodePosition(valueNode))
			}
			o.Responses = responses
		default:
			if IsCustomDomainExtensionNode(keyNode.Value) {
				de, err := o.raml.unmarshalCustomDomainExtension(o.Location, keyNode, valueNode)
				if err != nil {
					return StacktraceNewWrapped("unmarshal custom domain extension", err, o.Location, WithNodePosition(valueNode))
				}
				o.CustomDomainProperties.Set(de.Name, de)
			} else {
				return StacktraceNew("unknown key in describedBy", o.Location, WithNodePosition(keyNode), stacktrace.WithInfo("key", keyNode.Value))
			}
		}
	}
	return nil
}

func (o *SecuritySchemeDescription) makeOperation() *Operation {
	return &Operation{
		Headers:         o.Headers,
		QueryParameters: o.QueryParameters,
		QueryString:     o.QueryString,
		Responses:       o.Responses,

		CustomDomainProperties: o.CustomDomainProperties,
	}
}

func (r *RAML) MakeSecuritySchemeSettings(schemeType SecuritySchemeType, typeNode *yaml.Node, settingsNode *yaml.Node, location string) (SecuritySchemeSettings, error) {
	var schemeSettings SecuritySchemeSettings
	switch schemeType {
	case BasicAuthType:
		schemeSettings = &BasicAuthSchemeSettings{Location: location}
	case DigestAuthType:
		schemeSettings = &DigestAuthSchemeSettings{Location: location}
	case PassThroughAuthType:
		schemeSettings = &PassThroughSchemeSettings{Location: location}
	case OAuth1AuthType:
		schemeSettings = &OAuth1SchemeSettings{
			Location:               location,
			Position:               NewNodePosition(typeNode),
			raml:                   r,
			CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
		}
	case OAuth2AuthType:
		schemeSettings = &OAuth2SchemeSettings{
			Location:               location,
			Position:               NewNodePosition(typeNode),
			raml:                   r,
			CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
		}
	default:
		if strings.HasPrefix(string(schemeType), "x-") {
			schemeSettings = &CustomAuthScheme{Location: location}
		} else {
			return nil, StacktraceNew("unknown security scheme type", location, WithNodePosition(typeNode), stacktrace.WithInfo("type", schemeType))
		}
	}

	if settingsNode != nil && settingsNode.Tag != TagNull {
		if settingsNode.Kind != yaml.MappingNode {
			return nil, StacktraceNew("security scheme settings must be a mapping node", location, WithNodePosition(settingsNode))
		}
		if err := schemeSettings.Decode(settingsNode); err != nil {
			return nil, StacktraceNewWrapped("decode settings", err, location, WithNodePosition(settingsNode))
		}
	}

	// Validate settings after decoding
	if err := schemeSettings.Validate(); err != nil {
		return nil, err
	}

	return schemeSettings, nil
}

type SecuritySchemeSettings interface {
	Decode(node *yaml.Node) error
	Validate() error
}

// OperationParamsApplier is implemented by SecuritySchemeSettings types that
// support operation-level parameter overrides (e.g. OAuth 2.0 scope narrowing).
// ApplyOperationParams validates and compiles the raw params decoded from a
// securedBy mapping entry, returning a typed params struct.
type OperationParamsApplier interface {
	ApplyOperationParams(params map[string]any) (any, error)
}

// OAuth2OperationParams holds the parsed and validated operation-level OAuth 2.0
// parameter overrides from a securedBy mapping entry (e.g. scopes: [ADMIN]).
type OAuth2OperationParams struct {
	Scopes []string
}

type NullAuthScheme struct {
	stacktrace.Position
	Location string
}

func (s *NullAuthScheme) Decode(node *yaml.Node) error {
	return StacktraceNew("null security scheme has no settings", s.Location, WithNodePosition(node))
}

func (s *NullAuthScheme) Validate() error {
	return nil
}

type BasicAuthSchemeSettings struct {
	stacktrace.Position
	Location string
}

func (s *BasicAuthSchemeSettings) Decode(node *yaml.Node) error {
	return StacktraceNew("basic security scheme has no settings", s.Location, WithNodePosition(node))
}

func (s *BasicAuthSchemeSettings) Validate() error {
	return nil
}

type DigestAuthSchemeSettings struct {
	stacktrace.Position
	Location string
}

func (s *DigestAuthSchemeSettings) Decode(node *yaml.Node) error {
	return StacktraceNew("digest security scheme has no settings", s.Location, WithNodePosition(node))
}

func (s *DigestAuthSchemeSettings) Validate() error {
	return nil
}

type PassThroughSchemeSettings struct {
	stacktrace.Position
	Location string
}

func (s *PassThroughSchemeSettings) Decode(node *yaml.Node) error {
	return StacktraceNew("passthrough security scheme has no settings", s.Location, WithNodePosition(node))
}

func (s *PassThroughSchemeSettings) Validate() error {
	return nil
}

type OAuth1SchemeSettings struct {
	RequestTokenURI     *ScalarFacet[string]
	AuthorizationURI    *ScalarFacet[string]
	TokenCredentialsURI *ScalarFacet[string]
	Signatures          []*Node[string]

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	stacktrace.Position
	Location string
	raml     *RAML
}

func (s *OAuth1SchemeSettings) Decode(node *yaml.Node) error {
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		switch keyNode.Value {
		case FacetRequestTokenUri:
			sn, err := MakeScalarFacetYAML[string](s.raml, keyNode, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, s.Location, WithNodePosition(valueNode))
			}
			s.RequestTokenURI = sn
		case FacetAuthorizationUri:
			sn, err := MakeScalarFacetYAML[string](s.raml, keyNode, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, s.Location, WithNodePosition(valueNode))
			}
			s.AuthorizationURI = sn
		case FacetTokenCredentialsUri:
			sn, err := MakeScalarFacetYAML[string](s.raml, keyNode, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, s.Location, WithNodePosition(valueNode))
			}
			s.TokenCredentialsURI = sn
		case FacetSignatures:
			_, rn, err := s.raml.resolveInclude(valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(valueNode))
			}
			if rn.Kind != yaml.SequenceNode {
				return StacktraceNew("signatures must be an array", s.Location, WithNodePosition(valueNode))
			}
			signatures := make([]*Node[string], len(rn.Content))
			for i, v := range rn.Content {
				fragmentPath, rv, err := s.raml.resolveInclude(v, s.Location)
				if err != nil {
					return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(v))
				}
				signatures[i] = MakeSeqNode(rv.Value, v, s.Location, fragmentPath)
			}
			s.Signatures = signatures
		default:
			if IsCustomDomainExtensionNode(keyNode.Value) {
				de, err := s.raml.unmarshalCustomDomainExtension(s.Location, keyNode, valueNode)
				if err != nil {
					return StacktraceNewWrapped("unmarshal custom domain extension", err, s.Location, WithNodePosition(valueNode))
				}
				s.CustomDomainProperties.Set(de.Name, de)
			} else {
				return StacktraceNew("unknown key in OAuth1 security scheme settings", s.Location, WithNodePosition(keyNode), stacktrace.WithInfo("key", keyNode.Value))
			}
		}
	}
	s.Position = NewNodePosition(node)
	return nil
}

// Validate signatures if present
var validSignatures = map[string]struct{}{
	"HMAC-SHA1": {},
	"RSA-SHA1":  {},
	"PLAINTEXT": {},
}

func (s *OAuth1SchemeSettings) Validate() error {
	// Validate required fields
	if s.RequestTokenURI == nil || s.RequestTokenURI.Value == "" {
		return StacktraceNew("requestTokenUri is required for OAuth 1.0", s.Location, stacktrace.WithPosition(&s.Position))
	}
	if s.AuthorizationURI == nil || s.AuthorizationURI.Value == "" {
		return StacktraceNew("authorizationUri is required for OAuth 1.0", s.Location, stacktrace.WithPosition(&s.Position))
	}
	if s.TokenCredentialsURI == nil || s.TokenCredentialsURI.Value == "" {
		return StacktraceNew("tokenCredentialsUri is required for OAuth 1.0", s.Location, stacktrace.WithPosition(&s.Position))
	}
	for _, sig := range s.Signatures {
		if _, ok := validSignatures[sig.Value]; !ok {
			return StacktraceNew("unknown signature", s.Location, stacktrace.WithPosition(&sig.ValuePos), stacktrace.WithInfo("signature", sig.Value))
		}
	}

	return nil
}

type OAuth2SchemeSettings struct {
	AuthorizationURI    *ScalarFacet[string]
	AccessTokenURI      *ScalarFacet[string]
	AuthorizationGrants []*Node[string]
	Scopes              []*Node[string]

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	stacktrace.Position
	Location string
	raml     *RAML
}

func (s *OAuth2SchemeSettings) Decode(node *yaml.Node) error {
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		switch keyNode.Value {
		case FacetAuthorizationUri:
			sn, err := MakeScalarFacetYAML[string](s.raml, keyNode, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, s.Location, WithNodePosition(valueNode))
			}
			s.AuthorizationURI = sn
		case FacetAccessTokenUri:
			sn, err := MakeScalarFacetYAML[string](s.raml, keyNode, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, s.Location, WithNodePosition(valueNode))
			}
			s.AccessTokenURI = sn
		case FacetAuthorizationGrants:
			_, rn, err := s.raml.resolveInclude(valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(valueNode))
			}
			if rn.Kind != yaml.SequenceNode {
				return StacktraceNew("authorizationGrants must be an array", s.Location, WithNodePosition(valueNode))
			}
			authorizationGrants := make([]*Node[string], len(rn.Content))
			for i, v := range rn.Content {
				fragmentPath, rv, err := s.raml.resolveInclude(v, s.Location)
				if err != nil {
					return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(v))
				}
				authorizationGrants[i] = MakeSeqNode(rv.Value, v, s.Location, fragmentPath)
			}
			s.AuthorizationGrants = authorizationGrants
		case FacetScopes:
			_, rn, err := s.raml.resolveInclude(valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(valueNode))
			}
			if rn.Kind != yaml.SequenceNode {
				return StacktraceNew("scopes must be an array", s.Location, WithNodePosition(valueNode))
			}
			scopes := make([]*Node[string], len(rn.Content))
			for i, v := range rn.Content {
				fragmentPath, rv, err := s.raml.resolveInclude(v, s.Location)
				if err != nil {
					return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(v))
				}
				scopes[i] = MakeSeqNode(rv.Value, v, s.Location, fragmentPath)
			}
			s.Scopes = scopes
		default:
			if IsCustomDomainExtensionNode(keyNode.Value) {
				de, err := s.raml.unmarshalCustomDomainExtension(s.Location, keyNode, valueNode)
				if err != nil {
					return StacktraceNewWrapped("unmarshal custom domain extension", err, s.Location, WithNodePosition(valueNode))
				}
				s.CustomDomainProperties.Set(de.Name, de)
			} else {
				return StacktraceNew("unknown key in OAuth2 security scheme settings", s.Location, WithNodePosition(keyNode), stacktrace.WithInfo("key", keyNode.Value))
			}
		}
	}
	s.Position = NewNodePosition(node)
	return nil
}

// ApplyOperationParams implements OperationParamsApplier for OAuth 2.0 schemes.
// It validates that any scopes override in params is a subset of the scheme's
// declared scopes and returns the compiled OAuth2 operation params.
func (s *OAuth2SchemeSettings) ApplyOperationParams(params map[string]any) (any, error) {
	raw, ok := params["scopes"]
	if !ok {
		return &OAuth2OperationParams{}, nil
	}
	overrides, err := coerceScopeList(raw)
	if err != nil {
		return nil, fmt.Errorf("decode scopes: %w", err)
	}
	declared := make(map[string]struct{}, len(s.Scopes))
	for _, sc := range s.Scopes {
		declared[sc.Value] = struct{}{}
	}
	for _, sc := range overrides {
		if _, ok := declared[sc]; !ok {
			return nil, fmt.Errorf("scope is not declared by the security scheme: %s", sc)
		}
	}
	return &OAuth2OperationParams{Scopes: overrides}, nil
}

func coerceScopeList(v any) ([]string, error) {
	switch t := v.(type) {
	case []string:
		return t, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("scope entry must be a string, got %T", e)
			}
			out = append(out, s)
		}
		return out, nil
	case string:
		return []string{t}, nil
	default:
		return nil, fmt.Errorf("scopes must be a list of strings, got %T", v)
	}
}

var stdGrants = map[string]struct{}{
	"authorization_code": {},
	"implicit":           {},
	"password":           {},
	"client_credentials": {},
}

func (s *OAuth2SchemeSettings) Validate() error {
	// Validate required fields
	if s.AccessTokenURI == nil || s.AccessTokenURI.Value == "" {
		return StacktraceNew("accessTokenUri is required for OAuth 2.0", s.Location, stacktrace.WithPosition(&s.Position))
	}
	// Validate authorization grants if present
	// RAML 1.0 spec: authorization_code, password, client_credentials, implicit, or an absolute URI
	for _, grant := range s.AuthorizationGrants {
		if _, ok := stdGrants[grant.Value]; ok {
			continue
		}
		// Allow absolute URIs for custom grants
		if u, err := url.Parse(grant.Value); err == nil && u.IsAbs() {
			continue
		}
		return StacktraceNew("unknown authorization grant", s.Location, stacktrace.WithPosition(&grant.ValuePos), stacktrace.WithInfo("grant", grant))
	}

	return nil
}

type CustomAuthScheme struct {
	stacktrace.Position
	Location string
}

func (s *CustomAuthScheme) Decode(node *yaml.Node) error {
	return StacktraceNew("custom security scheme has no settings", s.Location, WithNodePosition(node))
}

func (s *CustomAuthScheme) Validate() error {
	return nil
}

type SecurityScheme struct {
	ID int64

	Name           string
	Definition     *SecuritySchemeDefinition
	Params         map[string]any
	CompiledParams any

	Location string
	ValuePos stacktrace.Position
	raml     *RAML
}

func (r *RAML) makeSecuritySchemes(valueNode *yaml.Node, location string) ([]*SecurityScheme, error) {
	switch valueNode.Kind {
	case yaml.ScalarNode:
		if valueNode.Tag == TagNull {
			return nil, nil
		}
		securityScheme, err := r.makeSecurityScheme(valueNode, location)
		if err != nil {
			return nil, StacktraceNewWrapped("make security scheme", err, location, WithNodePosition(valueNode))
		}
		return []*SecurityScheme{securityScheme}, nil
	case yaml.SequenceNode:
		securitySchemes := make([]*SecurityScheme, len(valueNode.Content))
		for i, node := range valueNode.Content {
			securityScheme, err := r.makeSecurityScheme(node, location)
			if err != nil {
				return nil, StacktraceNewWrapped("make security scheme", err, location, WithNodePosition(node))
			}
			securitySchemes[i] = securityScheme
		}
		return securitySchemes, nil
	default:
		return nil, StacktraceNew("security schemes must be either sequence or scalar node", location, WithNodePosition(valueNode))
	}
}

func (r *RAML) makeSecurityScheme(valueNode *yaml.Node, location string) (*SecurityScheme, error) {
	securityScheme := &SecurityScheme{
		ID:       r.generateSequenceID(),
		Location: location,
		raml:     r,
		ValuePos: NewNodePosition(valueNode),
	}

	if err := securityScheme.decode(valueNode); err != nil {
		return nil, StacktraceNewWrapped("decode security scheme", err, location, WithNodePosition(valueNode))
	}

	return securityScheme, nil
}

func (ss *SecurityScheme) decode(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == TagNull {
			ss.Name = "null"
			ss.Definition = ss.raml.makeNullSecuritySchemeDefinition(node, ss.Location)
		} else {
			ss.Name = node.Value
		}
	case yaml.MappingNode:
		keyNode := node.Content[0]
		valueNode := node.Content[1]
		ss.Name = keyNode.Value
		if err := valueNode.Decode(&ss.Params); err != nil {
			return StacktraceNewWrapped("decode security scheme parameters", err, ss.Location, WithNodePosition(valueNode))
		}
	default:
		return StacktraceNew("security scheme must be either scalar or mapping node", ss.Location, WithNodePosition(node))
	}
	return nil
}
