package converter

import (
	"encoding/json"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// mergeJSONExtensions merges x-* extension fields into an already-marshalled
// JSON object. base must be a valid JSON object ending with '}'.
func mergeJSONExtensions(base []byte, ext map[string]any) ([]byte, error) {
	if len(ext) == 0 {
		return base, nil
	}
	extBytes, err := json.Marshal(ext)
	if err != nil {
		return nil, err
	}
	// base[:len(base)-1] strips the closing '}'. If the object is empty ({})
	// that leaves just '{', so we must not insert a comma before the first
	// extension key — the comma is only needed when there are existing fields.
	isEmpty := len(base) == 2 // '{' + '}'
	merged := make([]byte, 0, len(base)+len(extBytes)-1)
	merged = append(merged, base[:len(base)-1]...)
	if !isEmpty {
		merged = append(merged, ',')
	}
	merged = append(merged, extBytes[1:]...)
	return merged, nil
}

// OAS3Document is the root OpenAPI 3.0 document object.
//
// https://spec.openapis.org/oas/v3.0.3#openapi-object
type OAS3Document struct {
	OpenAPI    string                                        `json:"openapi"              yaml:"openapi"`
	Info       OAS3Info                                      `json:"info"                 yaml:"info"`
	Servers    []OAS3Server                                  `json:"servers,omitempty"    yaml:"servers,omitempty"`
	Paths      *orderedmap.OrderedMap[string, *OAS3PathItem] `json:"paths"                yaml:"paths"`
	Components OAS3Components                                `json:"components,omitempty" yaml:"components,omitempty"`
	Security   []OAS3SecurityRequirement                     `json:"security,omitempty"   yaml:"security,omitempty"`
	Tags       []OAS3Tag                                     `json:"tags,omitempty"       yaml:"tags,omitempty"`
	Extensions map[string]any                                `json:"-"                    yaml:"-"`
}

// OAS3Info holds API metadata.
//
// https://spec.openapis.org/oas/v3.0.3#info-object
type OAS3Info struct {
	Title       string `json:"title"                 yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Version     string `json:"version"               yaml:"version"`
}

// OAS3Server describes a server endpoint for the API.
//
// https://spec.openapis.org/oas/v3.0.3#server-object
type OAS3Server struct {
	URL         string                                             `json:"url"                   yaml:"url"`
	Description string                                             `json:"description,omitempty" yaml:"description,omitempty"`
	Variables   *orderedmap.OrderedMap[string, OAS3ServerVariable] `json:"variables,omitempty"   yaml:"variables,omitempty"`
}

// OAS3ServerVariable describes a substitution variable in a server URL template.
//
// https://spec.openapis.org/oas/v3.0.3#server-variable-object
type OAS3ServerVariable struct {
	Default     string   `json:"default"               yaml:"default"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"        yaml:"enum,omitempty"`
}

// OAS3PathItem holds the operations available on a single path.
//
// https://spec.openapis.org/oas/v3.0.3#path-item-object
type OAS3PathItem struct {
	Summary     string           `json:"summary,omitempty"     yaml:"summary,omitempty"`
	Description string           `json:"description,omitempty" yaml:"description,omitempty"`
	Get         *OAS3Operation   `json:"get,omitempty"         yaml:"get,omitempty"`
	Put         *OAS3Operation   `json:"put,omitempty"         yaml:"put,omitempty"`
	Post        *OAS3Operation   `json:"post,omitempty"        yaml:"post,omitempty"`
	Delete      *OAS3Operation   `json:"delete,omitempty"      yaml:"delete,omitempty"`
	Options     *OAS3Operation   `json:"options,omitempty"     yaml:"options,omitempty"`
	Head        *OAS3Operation   `json:"head,omitempty"        yaml:"head,omitempty"`
	Patch       *OAS3Operation   `json:"patch,omitempty"       yaml:"patch,omitempty"`
	Trace       *OAS3Operation   `json:"trace,omitempty"       yaml:"trace,omitempty"`
	Parameters  []*OAS3Parameter `json:"parameters,omitempty"  yaml:"parameters,omitempty"`
}

// OAS3Operation describes a single API operation on a path.
//
// https://spec.openapis.org/oas/v3.0.3#operation-object
type OAS3Operation struct {
	Tags        []string                                      `json:"tags,omitempty"        yaml:"tags,omitempty"`
	Summary     string                                        `json:"summary,omitempty"     yaml:"summary,omitempty"`
	Description string                                        `json:"description,omitempty" yaml:"description,omitempty"`
	OperationID string                                        `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Parameters  []*OAS3Parameter                              `json:"parameters,omitempty"  yaml:"parameters,omitempty"`
	RequestBody *OAS3RequestBody                              `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses   *orderedmap.OrderedMap[string, *OAS3Response] `json:"responses"             yaml:"responses"`
	Security    []OAS3SecurityRequirement                     `json:"security,omitempty"    yaml:"security,omitempty"`
	Deprecated  bool                                          `json:"deprecated,omitempty"  yaml:"deprecated,omitempty"`
	Extensions  map[string]any                                `json:"-"                     yaml:"-"`
}

// OAS3Parameter describes a single operation parameter.
//
// https://spec.openapis.org/oas/v3.0.3#parameter-object
type OAS3Parameter struct {
	Name            string      `json:"name"                      yaml:"name"`
	In              string      `json:"in"                        yaml:"in"` // "path", "query", "header", "cookie"
	Description     string      `json:"description,omitempty"     yaml:"description,omitempty"`
	Required        bool        `json:"required,omitempty"        yaml:"required,omitempty"`
	Deprecated      bool        `json:"deprecated,omitempty"      yaml:"deprecated,omitempty"`
	AllowEmptyValue bool        `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Schema          *OAS3Schema `json:"schema,omitempty"          yaml:"schema,omitempty"`
	Example         any         `json:"example,omitempty"         yaml:"example,omitempty"`
}

// OAS3RequestBody describes the request body of an operation.
//
// https://spec.openapis.org/oas/v3.0.3#request-body-object
type OAS3RequestBody struct {
	Description string                                         `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool                                           `json:"required,omitempty"    yaml:"required,omitempty"`
	Content     *orderedmap.OrderedMap[string, *OAS3MediaType] `json:"content"               yaml:"content"`
}

// OAS3MediaType holds the schema for a specific media type in a request or response.
//
// https://spec.openapis.org/oas/v3.0.3#media-type-object
type OAS3MediaType struct {
	Schema  *OAS3Schema `json:"schema,omitempty"  yaml:"schema,omitempty"`
	Example any         `json:"example,omitempty" yaml:"example,omitempty"`
}

// OAS3Response describes a single response from an API operation.
//
// https://spec.openapis.org/oas/v3.0.3#response-object
type OAS3Response struct {
	Description string                                         `json:"description"       yaml:"description"`
	Headers     *orderedmap.OrderedMap[string, *OAS3Header]    `json:"headers,omitempty" yaml:"headers,omitempty"`
	Content     *orderedmap.OrderedMap[string, *OAS3MediaType] `json:"content,omitempty" yaml:"content,omitempty"`
}

// OAS3Header describes a single header in a response (or request via describedBy).
//
// https://spec.openapis.org/oas/v3.0.3#header-object
type OAS3Header struct {
	Description string      `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool        `json:"required,omitempty"    yaml:"required,omitempty"`
	Schema      *OAS3Schema `json:"schema,omitempty"      yaml:"schema,omitempty"`
}

// OAS3Components holds reusable objects for the API specification.
//
// https://spec.openapis.org/oas/v3.0.3#components-object
type OAS3Components struct {
	Schemas         *orderedmap.OrderedMap[string, *OAS3Schema]         `json:"schemas,omitempty"         yaml:"schemas,omitempty"`
	SecuritySchemes *orderedmap.OrderedMap[string, *OAS3SecurityScheme] `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
}

// OAS3SecurityScheme defines a security scheme for the API.
//
// https://spec.openapis.org/oas/v3.0.3#security-scheme-object
type OAS3SecurityScheme struct {
	// Type is one of: "apiKey", "http", "oauth2", "openIdConnect".
	// Non-standard RAML schemes are emitted with type "http" and an x-raml-type extension.
	Type             string          `json:"type"                       yaml:"type"`
	Description      string          `json:"description,omitempty"      yaml:"description,omitempty"`
	Name             string          `json:"name,omitempty"             yaml:"name,omitempty"`   // apiKey only
	In               string          `json:"in,omitempty"               yaml:"in,omitempty"`     // apiKey only
	Scheme           string          `json:"scheme,omitempty"           yaml:"scheme,omitempty"` // http only
	BearerFormat     string          `json:"bearerFormat,omitempty"     yaml:"bearerFormat,omitempty"`
	Flows            *OAS3OAuthFlows `json:"flows,omitempty"            yaml:"flows,omitempty"` // oauth2 only
	OpenIDConnectURL string          `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"`
	Extensions       map[string]any  `json:"-"                          yaml:"-"`
}

// OAS3OAuthFlows holds the OAuth 2.0 flow configurations.
//
// https://spec.openapis.org/oas/v3.0.3#oauth-flows-object
type OAS3OAuthFlows struct {
	Implicit          *OAS3OAuthFlow `json:"implicit,omitempty"          yaml:"implicit,omitempty"`
	Password          *OAS3OAuthFlow `json:"password,omitempty"          yaml:"password,omitempty"`
	ClientCredentials *OAS3OAuthFlow `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCode *OAS3OAuthFlow `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty"`
}

// OAS3OAuthFlow describes an OAuth 2.0 flow.
//
// https://spec.openapis.org/oas/v3.0.3#oauth-flow-object
type OAS3OAuthFlow struct {
	AuthorizationURL string                                 `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenURL         string                                 `json:"tokenUrl,omitempty"         yaml:"tokenUrl,omitempty"`
	RefreshURL       string                                 `json:"refreshUrl,omitempty"       yaml:"refreshUrl,omitempty"`
	Scopes           *orderedmap.OrderedMap[string, string] `json:"scopes"                     yaml:"scopes"`
}

// OAS3SecurityRequirement maps security scheme names to required scopes.
// An empty map entry means no scopes required.
//
// https://spec.openapis.org/oas/v3.0.3#security-requirement-object
type OAS3SecurityRequirement map[string][]string

// OAS3Tag adds metadata to a tag used in operations.
//
// https://spec.openapis.org/oas/v3.0.3#tag-object
type OAS3Tag struct {
	Name        string `json:"name"                  yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// MarshalJSON implements custom JSON marshalling so that Extensions on
// OAS3Document are emitted inline alongside the standard fields.
func (d *OAS3Document) MarshalJSON() ([]byte, error) {
	type Alias OAS3Document
	base, err := json.Marshal((*Alias)(d))
	if err != nil {
		return nil, err
	}
	return mergeJSONExtensions(base, d.Extensions)
}

// MarshalJSON implements custom JSON marshalling so that Extensions on
// OAS3Operation are emitted inline alongside the standard fields.
func (o *OAS3Operation) MarshalJSON() ([]byte, error) {
	type Alias OAS3Operation
	base, err := json.Marshal((*Alias)(o))
	if err != nil {
		return nil, err
	}
	return mergeJSONExtensions(base, o.Extensions)
}

// MarshalJSON implements custom JSON marshalling so that Extensions on
// OAS3SecurityScheme are emitted inline alongside the standard fields.
func (s *OAS3SecurityScheme) MarshalJSON() ([]byte, error) {
	type Alias OAS3SecurityScheme
	base, err := json.Marshal((*Alias)(s))
	if err != nil {
		return nil, err
	}
	return mergeJSONExtensions(base, s.Extensions)
}
