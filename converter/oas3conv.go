package converter

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	raml "github.com/acronis/go-raml/v3"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

const (
	oas3SchemeHTTP   = "http"
	oas3SchemeBearer = "bearer"
	oas3ExtRamlType  = "x-raml-type"
)

// OAS3Converter converts a parsed and unwrapped RAML raml.APIFragment into an
// OAS3Document. The raml.APIFragment must have been parsed with at least
// OptWithUnwrap(); OptWithValidate() is recommended but not required.
type OAS3Converter struct {
	schema *OAS3SchemaConverter
}

// NewOAS3Converter creates a ready-to-use document converter.
func NewOAS3Converter() *OAS3Converter {
	return &OAS3Converter{
		schema: NewOAS3SchemaConverter(),
	}
}

// Convert converts the given raml.APIFragment to an OAS3Document.
// It returns an error if the fragment has not been unwrapped.
func (c *OAS3Converter) Convert(api *raml.APIFragment) (*OAS3Document, error) {
	doc := &OAS3Document{
		OpenAPI: "3.0.3",
		Paths:   orderedmap.New[string, *OAS3PathItem](),
	}

	c.convertInfo(api, doc)

	c.convertServers(api, doc)
	c.convertTags(api, doc)
	c.convertGlobalSecurity(api, doc)

	// Named types → components/schemas. Must run before paths so that
	// recursive schemas triggered by path body shapes land in the same map.
	if err := c.convertNamedTypes(api); err != nil {
		return nil, fmt.Errorf("convert named types: %w", err)
	}

	if err := c.convertPaths(api, doc); err != nil {
		return nil, fmt.Errorf("convert paths: %w", err)
	}

	if err := c.convertSecuritySchemes(api, doc); err != nil {
		return nil, fmt.Errorf("convert security schemes: %w", err)
	}

	// Attach accumulated component schemas (named types + recursive refs).
	if schemas := c.schema.Components(); schemas.Len() > 0 {
		doc.Components.Schemas = schemas
	}

	return doc, nil
}

// ---- info & servers ----------------------------------------------------------

func (c *OAS3Converter) convertInfo(api *raml.APIFragment, doc *OAS3Document) {
	if api.Title != nil {
		doc.Info.Title = api.Title.Value
	}
	if api.Description != nil {
		doc.Info.Description = api.Description.Value
	}
	if api.Version != nil {
		doc.Info.Version = api.Version.Value
	}
}

// convertServers builds the servers array from RAML baseUri + baseUriParameters.
//
// RAML baseUri may contain {version} and custom template variables. Each
// variable that has an explicit baseUriParameters entry is converted to an
// OAS3ServerVariable. The special {version} variable uses the api.Version
// value as its default.
func (c *OAS3Converter) convertServers(api *raml.APIFragment, doc *OAS3Document) {
	if api.BaseURI == nil {
		return
	}

	server := OAS3Server{URL: api.BaseURI.Value}

	if l := api.BaseURIParameters.Len(); l > 0 {
		server.Variables = orderedmap.New[string, OAS3ServerVariable](l)
		for pair := api.BaseURIParameters.Oldest(); pair != nil; pair = pair.Next() {
			server.Variables.Set(pair.Key, c.makeServerVariable(pair.Key, pair.Value, api.Version))
		}
	}

	doc.Servers = []OAS3Server{server}
}

func (c *OAS3Converter) makeServerVariable(
	name string, shape *raml.BaseShape, version *raml.ScalarFacet[string],
) OAS3ServerVariable {
	sv := OAS3ServerVariable{}
	if name == "version" && version != nil {
		sv.Default = version.Value
	}
	if shape.Description != nil {
		sv.Description = shape.Description.Value
	}
	for _, e := range shape.Enum {
		if s, ok := e.Value.Raw.(string); ok {
			sv.Enum = append(sv.Enum, s)
		}
	}
	if sv.Default == "" && shape.Default != nil {
		if s, ok := shape.Default.Value.Raw.(string); ok {
			sv.Default = s
		}
	}
	return sv
}

// ---- tags (from documentation) -----------------------------------------------

// convertTags maps RAML documentation items to OAS3 tags.
// This preserves the title and content of each documentation item.
// OAS3 tags carry only a name and description; the full markdown content is
// placed in description.
func (c *OAS3Converter) convertTags(api *raml.APIFragment, doc *OAS3Document) {
	if len(api.Documentation) == 0 {
		return
	}
	doc.Tags = make([]OAS3Tag, 0, len(api.Documentation))
	for _, di := range api.Documentation {
		tag := OAS3Tag{}
		if di.Title != nil {
			tag.Name = di.Title.Value
		}
		if di.Content != nil {
			tag.Description = di.Content.Value
		}
		doc.Tags = append(doc.Tags, tag)
	}
}

// ---- global security ---------------------------------------------------------

func (c *OAS3Converter) convertGlobalSecurity(api *raml.APIFragment, doc *OAS3Document) {
	for _, ss := range api.SecuredBy {
		doc.Security = append(doc.Security, securityRequirementFromScheme(ss))
	}
}

// ---- named types → components/schemas ----------------------------------------

// convertNamedTypes registers all top-level API types into the schema
// converter's namedTypes set. The actual components/schemas entries are built
// lazily the first time each type is referenced from a path schema; unused
// types are never emitted to avoid linter warnings about unused components.
func (c *OAS3Converter) convertNamedTypes(api *raml.APIFragment) error {
	for pair := api.Types.Oldest(); pair != nil; pair = pair.Next() {
		shape := pair.Value
		if !shape.IsUnwrapped() {
			return fmt.Errorf("type %q is not unwrapped; parse with OptWithUnwrap()", shape.Name)
		}
		c.schema.namedTypes[shape.Name] = struct{}{}
	}
	return nil
}

// ---- paths -------------------------------------------------------------------

func (c *OAS3Converter) convertPaths(api *raml.APIFragment, doc *OAS3Document) error {
	for pair := api.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		ep := pair.Value
		if err := c.convertEndpoint(ep, doc); err != nil {
			return fmt.Errorf("endpoint %q: %w", ep.FullURI, err)
		}
	}
	return nil
}

func (c *OAS3Converter) convertEndpoint(ep *raml.EndPoint, doc *OAS3Document) error {
	// Convert {param} URI to OAS3 path format — RAML and OAS3 both use {name}
	// so no transformation is needed.
	path := ep.FullURI

	// Only emit a path item when the endpoint has at least one HTTP operation.
	// Intermediate RAML container resources (description/parameters only, no
	// methods) must not appear in the OAS3 paths object: OAS3 linters flag path
	// template expressions in operationless path items as unmatched parameters.
	// URI parameters are already propagated to every child endpoint by the
	// unwrap step, so skipping the container entry is safe.
	if ep.Operations.Len() > 0 {
		item, exists := doc.Paths.Get(path)
		if !exists {
			item = &OAS3PathItem{}
			doc.Paths.Set(path, item)
		}

		if ep.DisplayName != nil {
			item.Summary = ep.DisplayName.Value
		}
		if ep.Description != nil {
			item.Description = ep.Description.Value
		}

		// Path-level URI parameters shared across all operations on this path.
		pathParams, err := c.convertURIParameters(ep)
		if err != nil {
			return fmt.Errorf("uri parameters: %w", err)
		}
		item.Parameters = pathParams

		for opPair := ep.Operations.Oldest(); opPair != nil; opPair = opPair.Next() {
			oas3Op, err := c.convertOperation(opPair.Value)
			if err != nil {
				return fmt.Errorf("operation %s: %w", opPair.Key, err)
			}
			setPathItemOperation(item, opPair.Key, oas3Op)
		}
	}

	// Recurse into nested endpoints regardless of whether this one has operations.
	for childPair := ep.EndPoints.Oldest(); childPair != nil; childPair = childPair.Next() {
		if err := c.convertEndpoint(childPair.Value, doc); err != nil {
			return err
		}
	}

	return nil
}

func (c *OAS3Converter) convertURIParameters(ep *raml.EndPoint) ([]*OAS3Parameter, error) {
	l := ep.URIParameters.Len()
	if l == 0 {
		return nil, nil
	}
	params := make([]*OAS3Parameter, 0, l)
	for pair := ep.URIParameters.Oldest(); pair != nil; pair = pair.Next() {
		shape := pair.Value
		schema, err := c.convertShapeToSchema(shape)
		if err != nil {
			return nil, fmt.Errorf("uri parameter %q: %w", pair.Key, err)
		}
		p := &OAS3Parameter{
			Name:     pair.Key,
			In:       "path",
			Required: true, // path parameters are always required in OAS3
			Schema:   schema,
		}
		if shape.Description != nil {
			p.Description = shape.Description.Value
		}
		params = append(params, p)
	}
	return params, nil
}

func (c *OAS3Converter) convertOperation(op *raml.Operation) (*OAS3Operation, error) {
	oas3Op := &OAS3Operation{
		Responses: orderedmap.New[string, *OAS3Response](),
	}

	if op.DisplayName != nil {
		oas3Op.Summary = op.DisplayName.Value
	}
	if op.Description != nil {
		oas3Op.Description = op.Description.Value
	}

	// Query parameters and headers.
	params, err := c.convertParameters(op)
	if err != nil {
		return nil, err
	}
	oas3Op.Parameters = params

	// raml.Request body.
	if op.Request != nil {
		var rb *OAS3RequestBody
		rb, err = c.convertRequestBody(op.Request)
		if err != nil {
			return nil, fmt.Errorf("request body: %w", err)
		}
		oas3Op.RequestBody = rb
	}

	// Responses.
	var respErr error
	for pair := op.Responses.Oldest(); pair != nil; pair = pair.Next() {
		var resp *OAS3Response
		resp, respErr = c.convertResponse(pair.Value)
		if respErr != nil {
			return nil, fmt.Errorf("response %d: %w", pair.Key, respErr)
		}
		oas3Op.Responses.Set(strconv.Itoa(pair.Key), resp)
	}
	if oas3Op.Responses.Len() == 0 {
		// OAS3 requires at least one response; emit a default placeholder.
		oas3Op.Responses.Set("default", &OAS3Response{Description: "Success"})
	}

	// Security.
	for _, ss := range op.SecuredBy {
		oas3Op.Security = append(oas3Op.Security, securityRequirementFromScheme(ss))
	}

	// queryString: expand object properties as query params; otherwise emit as extension.
	if op.QueryString != nil {
		if qsErr := c.applyQueryString(op.QueryString, oas3Op); qsErr != nil {
			return nil, fmt.Errorf("queryString: %w", qsErr)
		}
	}

	// Annotations → extensions (additive: do not overwrite x-query-string set above).
	if l := op.CustomDomainProperties.Len(); l > 0 {
		if oas3Op.Extensions == nil {
			oas3Op.Extensions = make(map[string]any, l)
		}
		for pair := op.CustomDomainProperties.Oldest(); pair != nil; pair = pair.Next() {
			if pair.Value.Extension != nil && pair.Value.Extension.Value != nil {
				oas3Op.Extensions["x-"+pair.Key] = pair.Value.Extension.Value.Raw
			}
		}
	}

	return oas3Op, nil
}

func (c *OAS3Converter) convertParameters(op *raml.Operation) ([]*OAS3Parameter, error) {
	var params []*OAS3Parameter

	// Query parameters.
	for pair := op.QueryParameters.Oldest(); pair != nil; pair = pair.Next() {
		prop := &pair.Value
		schema, err := c.convertShapeToSchema(prop.Base)
		if err != nil {
			return nil, fmt.Errorf("query parameter %q: %w", pair.Key, err)
		}
		p := &OAS3Parameter{
			Name:     pair.Key,
			In:       "query",
			Required: prop.Required,
			Schema:   schema,
		}
		if prop.Base.Description != nil {
			p.Description = prop.Base.Description.Value
		}
		params = append(params, p)
	}

	// Headers.
	for pair := op.Headers.Oldest(); pair != nil; pair = pair.Next() {
		prop := &pair.Value
		schema, err := c.convertShapeToSchema(prop.Base)
		if err != nil {
			return nil, fmt.Errorf("header %q: %w", pair.Key, err)
		}
		p := &OAS3Parameter{
			Name:     pair.Key,
			In:       "header",
			Required: prop.Required,
			Schema:   schema,
		}
		if prop.Base.Description != nil {
			p.Description = prop.Base.Description.Value
		}
		params = append(params, p)
	}

	return params, nil
}

// applyQueryString expands a RAML queryString onto the operation in-place.
func (c *OAS3Converter) applyQueryString(qs *raml.BaseShape, op *OAS3Operation) error {
	qsParams, ext, err := c.convertQueryString(qs)
	if err != nil {
		return err
	}
	if qsParams != nil {
		op.Parameters = append(op.Parameters, qsParams...)
	}
	if ext != nil {
		if op.Extensions == nil {
			op.Extensions = make(map[string]any)
		}
		op.Extensions["x-query-string"] = ext
	}
	return nil
}

// convertQueryString attempts to expand an raml.ObjectShape queryString into
// individual query parameters. Non-object shapes (unions, arrays, etc.) are
// returned as an OAS3Schema for the x-query-string extension instead.
func (c *OAS3Converter) convertQueryString(qs *raml.BaseShape) ([]*OAS3Parameter, *OAS3Schema, error) {
	obj, ok := qs.Shape.(*raml.ObjectShape)
	if !ok {
		// Not expandable — emit as extension.
		schema, err := c.convertShapeToSchema(qs)
		if err != nil {
			return nil, nil, err
		}
		return nil, schema, nil
	}

	l := obj.Properties.Len()
	if l == 0 {
		// Object with no properties — treat as open query string, emit as extension.
		schema, err := c.convertShapeToSchema(qs)
		if err != nil {
			return nil, nil, err
		}
		return nil, schema, nil
	}

	params := make([]*OAS3Parameter, 0, l)
	for pair := obj.Properties.Oldest(); pair != nil; pair = pair.Next() {
		prop := &pair.Value
		schema, err := c.convertShapeToSchema(prop.Base)
		if err != nil {
			return nil, nil, fmt.Errorf("property %q: %w", pair.Key, err)
		}
		p := &OAS3Parameter{
			Name:     pair.Key,
			In:       "query",
			Required: prop.Required,
			Schema:   schema,
		}
		if prop.Base.Description != nil {
			p.Description = prop.Base.Description.Value
		}
		params = append(params, p)
	}
	return params, nil, nil
}

func (c *OAS3Converter) convertRequestBody(req *raml.Request) (*OAS3RequestBody, error) {
	l := req.Bodies.Len()
	if l == 0 {
		return nil, nil //nolint:nilnil // nil request body means absent body
	}
	rb := &OAS3RequestBody{
		Content: orderedmap.New[string, *OAS3MediaType](l),
	}
	for pair := req.Bodies.Oldest(); pair != nil; pair = pair.Next() {
		body := pair.Value
		mt := &OAS3MediaType{}
		if body.Shape != nil {
			schema, err := c.convertShapeToSchema(body.Shape)
			if err != nil {
				return nil, fmt.Errorf("media type %q: %w", pair.Key, err)
			}
			mt.Schema = schema
		}
		rb.Content.Set(pair.Key, mt)
	}
	return rb, nil
}

func (c *OAS3Converter) convertResponse(resp *raml.Response) (*OAS3Response, error) {
	desc := resp.Description
	if desc == "" {
		if text := http.StatusText(resp.StatusCode); text != "" {
			desc = text
		} else {
			desc = "raml.Response"
		}
	}
	oas3Resp := &OAS3Response{
		Description: desc,
	}

	// Headers.
	if l := resp.Headers.Len(); l > 0 {
		oas3Resp.Headers = orderedmap.New[string, *OAS3Header](l)
		for pair := resp.Headers.Oldest(); pair != nil; pair = pair.Next() {
			prop := &pair.Value
			schema, err := c.convertShapeToSchema(prop.Base)
			if err != nil {
				return nil, fmt.Errorf("header %q: %w", pair.Key, err)
			}
			h := &OAS3Header{
				Required: prop.Required,
				Schema:   schema,
			}
			if prop.Base.Description != nil {
				h.Description = prop.Base.Description.Value
			}
			oas3Resp.Headers.Set(pair.Key, h)
		}
	}

	// Bodies.
	if l := resp.Bodies.Len(); l > 0 {
		oas3Resp.Content = orderedmap.New[string, *OAS3MediaType](l)
		for pair := resp.Bodies.Oldest(); pair != nil; pair = pair.Next() {
			body := pair.Value
			mt := &OAS3MediaType{}
			if body.Shape != nil {
				schema, err := c.convertShapeToSchema(body.Shape)
				if err != nil {
					return nil, fmt.Errorf("media type %q: %w", pair.Key, err)
				}
				mt.Schema = schema
			}
			oas3Resp.Content.Set(pair.Key, mt)
		}
	}

	return oas3Resp, nil
}

// ---- security schemes --------------------------------------------------------

func (c *OAS3Converter) convertSecuritySchemes(api *raml.APIFragment, doc *OAS3Document) error {
	l := api.SecuritySchemes.Len()
	if l == 0 {
		return nil
	}

	doc.Components.SecuritySchemes = orderedmap.New[string, *OAS3SecurityScheme](l)
	for pair := api.SecuritySchemes.Oldest(); pair != nil; pair = pair.Next() {
		name := pair.Key
		def := pair.Value
		// Follow !include links to the actual definition.
		if def.Link != nil {
			def = def.Link.SecurityScheme
		}
		ss, err := c.convertSecurityScheme(def)
		if err != nil {
			return fmt.Errorf("security scheme %q: %w", name, err)
		}
		doc.Components.SecuritySchemes.Set(name, ss)
	}
	return nil
}

func (c *OAS3Converter) convertSecurityScheme(def *raml.SecuritySchemeDefinition) (*OAS3SecurityScheme, error) {
	ss := &OAS3SecurityScheme{}
	if def.Description != nil {
		ss.Description = def.Description.Value
	}
	if def.Type == nil {
		return nil, errors.New("security scheme has no type")
	}

	switch def.Type.Value {
	case raml.BasicAuthType:
		ss.Type = oas3SchemeHTTP
		ss.Scheme = "basic"

	case raml.DigestAuthType:
		ss.Type = oas3SchemeHTTP
		ss.Scheme = "digest"

	case raml.PassThroughAuthType:
		// Pass-through has no direct OAS3 equivalent; emit as x- extension.
		ss.Type = oas3SchemeHTTP
		ss.Scheme = oas3SchemeBearer
		ss.Extensions = map[string]any{oas3ExtRamlType: string(raml.PassThroughAuthType)}

	case raml.OAuth2AuthType:
		ss.Type = "oauth2"
		flows, err := convertOAuth2Flows(def.Settings)
		if err != nil {
			return nil, err
		}
		ss.Flows = flows

	case raml.OAuth1AuthType:
		// OAuth 1.0 has no OAS3 equivalent.
		ss.Type = oas3SchemeHTTP
		ss.Scheme = oas3SchemeBearer
		if settings, ok := def.Settings.(*raml.OAuth1SchemeSettings); ok {
			ext := map[string]any{oas3ExtRamlType: string(raml.OAuth1AuthType)}
			if settings.RequestTokenURI != nil {
				ext["requestTokenUri"] = settings.RequestTokenURI.Value
			}
			if settings.AuthorizationURI != nil {
				ext["authorizationUri"] = settings.AuthorizationURI.Value
			}
			if settings.TokenCredentialsURI != nil {
				ext["tokenCredentialsUri"] = settings.TokenCredentialsURI.Value
			}
			ss.Extensions = map[string]any{"x-raml-oauth1": ext}
		}

	default:
		// Custom x-* scheme.
		ss.Type = oas3SchemeHTTP
		ss.Scheme = oas3SchemeBearer
		ss.Extensions = map[string]any{oas3ExtRamlType: string(def.Type.Value)}
	}

	return ss, nil
}

func convertOAuth2Flows(settings raml.SecuritySchemeSettings) (*OAS3OAuthFlows, error) {
	oauth2, ok := settings.(*raml.OAuth2SchemeSettings)
	if !ok {
		return nil, errors.New("expected raml.OAuth2SchemeSettings")
	}

	// Build a shared scopes map (empty descriptions — RAML scopes are name-only).
	scopes := orderedmap.New[string, string](len(oauth2.Scopes))
	for _, s := range oauth2.Scopes {
		scopes.Set(s.Value, "")
	}

	flows := &OAS3OAuthFlows{}

	authURI := ""
	if oauth2.AuthorizationURI != nil {
		authURI = oauth2.AuthorizationURI.Value
	}
	tokenURI := ""
	if oauth2.AccessTokenURI != nil {
		tokenURI = oauth2.AccessTokenURI.Value
	}

	for _, grant := range oauth2.AuthorizationGrants {
		switch grant.Value {
		case "implicit":
			flows.Implicit = &OAS3OAuthFlow{
				AuthorizationURL: authURI,
				Scopes:           scopes,
			}
		case "password":
			flows.Password = &OAS3OAuthFlow{
				TokenURL: tokenURI,
				Scopes:   scopes,
			}
		case "client_credentials":
			flows.ClientCredentials = &OAS3OAuthFlow{
				TokenURL: tokenURI,
				Scopes:   scopes,
			}
		case "authorization_code":
			flows.AuthorizationCode = &OAS3OAuthFlow{
				AuthorizationURL: authURI,
				TokenURL:         tokenURI,
				Scopes:           scopes,
			}
		}
		// Custom URI grants are silently dropped — no OAS3 flow maps to them.
	}

	return flows, nil
}

// ---- helpers -----------------------------------------------------------------

// convertShapeToSchema is a thin wrapper that guards against un-unwrapped shapes.
func (c *OAS3Converter) convertShapeToSchema(base *raml.BaseShape) (*OAS3Schema, error) {
	if base == nil {
		return nil, nil //nolint:nilnil // nil schema is valid (absent schema)
	}
	if !base.IsUnwrapped() {
		return nil, fmt.Errorf("shape %q is not unwrapped", base.Name)
	}
	return c.schema.Visit(base.Shape), nil
}

// securityRequirementFromScheme converts a RAML raml.SecurityScheme reference to an
// OAS3SecurityRequirement. A null scheme maps to an empty requirement (anonymous).
func securityRequirementFromScheme(ss *raml.SecurityScheme) OAS3SecurityRequirement {
	if ss.Name == string(raml.NullAuthType) {
		return OAS3SecurityRequirement{}
	}
	req := OAS3SecurityRequirement{ss.Name: []string{}}
	// If the scheme reference carries compiled scope parameters, include them.
	if p, ok := ss.CompiledParams.(*raml.OAuth2OperationParams); ok {
		req[ss.Name] = p.Scopes
	}
	return req
}

// setPathItemOperation assigns an OAS3Operation to the correct method field.
func setPathItemOperation(item *OAS3PathItem, method string, op *OAS3Operation) {
	switch strings.ToLower(method) {
	case raml.MethodGet:
		item.Get = op
	case raml.MethodPut:
		item.Put = op
	case raml.MethodPost:
		item.Post = op
	case raml.MethodDelete:
		item.Delete = op
	case raml.MethodOptions:
		item.Options = op
	case raml.MethodHead:
		item.Head = op
	case raml.MethodPatch:
		item.Patch = op
	case raml.MethodTrace:
		item.Trace = op
	}
}
