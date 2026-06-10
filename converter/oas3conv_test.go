package converter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	raml "github.com/acronis/go-raml/v3"
	"github.com/stretchr/testify/require"
)

// TestMergeJSONExtensions covers the edge-cases of the helper that inlines
// x-* extension fields into an already-marshalled JSON object.
func TestMergeJSONExtensions(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		ext      map[string]any
		wantJSON string
	}{
		{
			name:     "no extensions returns base unchanged",
			base:     `{"type":"string"}`,
			ext:      nil,
			wantJSON: `{"type":"string"}`,
		},
		{
			name:     "extensions merged into non-empty object",
			base:     `{"type":"object"}`,
			ext:      map[string]any{"x-foo": "bar"},
			wantJSON: `{"type":"object","x-foo":"bar"}`,
		},
		{
			name:     "extensions merged into empty object",
			base:     `{}`,
			ext:      map[string]any{"x-foo": "bar"},
			wantJSON: `{"x-foo":"bar"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mergeJSONExtensions([]byte(tt.base), tt.ext)
			require.NoError(t, err)
			// Compare as parsed JSON to be order-independent.
			var gotV, wantV any
			require.NoError(t, json.Unmarshal(got, &gotV))
			require.NoError(t, json.Unmarshal([]byte(tt.wantJSON), &wantV))
			require.Equal(t, wantV, gotV)
		})
	}
}

// mustParseAPI writes content to a temp file, parses it as a RAML 1.0
// APIFragment with OptWithUnwrap(), and returns the fragment.
func mustParseAPI(t *testing.T, content string) *raml.APIFragment {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "api.raml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	r, err := raml.ParseFromPathCtx(context.Background(), path, raml.OptWithUnwrap())
	require.NoError(t, err)
	api, ok := r.EntryPoint().(*raml.APIFragment)
	require.True(t, ok, "entry point must be *raml.APIFragment")
	return api
}

// mustConvert converts api and fails the test on error.
func mustConvert(t *testing.T, api *raml.APIFragment) *OAS3Document {
	t.Helper()
	doc, err := NewOAS3Converter().Convert(api)
	require.NoError(t, err)
	return doc
}

// TestOAS3Converter_Info verifies that RAML title, description, version, and
// baseUri map to OAS3 info + servers.
func TestOAS3Converter_Info(t *testing.T) {
	const fixture = `#%RAML 1.0
title: My API
description: A test API
version: v2
baseUri: https://api.example.com/{version}
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	require.Equal(t, "3.0.3", doc.OpenAPI)
	require.Equal(t, "My API", doc.Info.Title)
	require.Equal(t, "A test API", doc.Info.Description)
	require.Equal(t, "v2", doc.Info.Version)

	require.Len(t, doc.Servers, 1)
	require.Equal(t, "https://api.example.com/{version}", doc.Servers[0].URL)
}

// TestOAS3Converter_SimplePaths verifies that RAML endpoints with GET and POST
// operations are emitted as OAS3 path items with the correct HTTP methods.
func TestOAS3Converter_SimplePaths(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Items API
version: v1
/items:
  get:
    description: List items
    responses:
      200:
        description: OK
  post:
    description: Create item
    responses:
      201:
        description: Created
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	item, ok := doc.Paths.Get("/items")
	require.True(t, ok, "path /items must exist")

	require.NotNil(t, item.Get, "GET must be set")
	require.Equal(t, "List items", item.Get.Description)
	_, hasOK := item.Get.Responses.Get("200")
	require.True(t, hasOK, "GET must have 200 response")

	require.NotNil(t, item.Post, "POST must be set")
	require.Equal(t, "Create item", item.Post.Description)
	_, hasCreated := item.Post.Responses.Get("201")
	require.True(t, hasCreated, "POST must have 201 response")
}

// TestOAS3Converter_QueryParameters verifies that RAML queryParameters are
// emitted as OAS3 query parameters with correct name, type, and required flag.
func TestOAS3Converter_QueryParameters(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Search API
version: v1
/search:
  get:
    queryParameters:
      q:
        type: string
        required: true
        description: Search query
      limit:
        type: integer
        required: false
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	item, ok := doc.Paths.Get("/search")
	require.True(t, ok)
	require.NotNil(t, item.Get)

	params := item.Get.Parameters
	require.Len(t, params, 2)

	q := params[0]
	require.Equal(t, "q", q.Name)
	require.Equal(t, "query", q.In)
	require.True(t, q.Required)
	require.Equal(t, "Search query", q.Description)
	require.NotNil(t, q.Schema)
	require.Equal(t, "string", q.Schema.Type)

	limit := params[1]
	require.Equal(t, "limit", limit.Name)
	require.Equal(t, "query", limit.In)
	require.False(t, limit.Required)
	require.NotNil(t, limit.Schema)
	require.Equal(t, "integer", limit.Schema.Type)
}

// TestOAS3Converter_NamedTypeRef verifies that a RAML type referenced in a
// request body is emitted as $ref to components/schemas and that the named type
// is registered in components/schemas.
func TestOAS3Converter_NamedTypeRef(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Types API
version: v1
types:
  Widget:
    type: object
    properties:
      id:
        type: integer
      name:
        type: string
/widgets:
  post:
    body:
      application/json:
        type: Widget
    responses:
      201:
        description: Created
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	// components/schemas must contain Widget
	require.NotNil(t, doc.Components.Schemas, "components/schemas must be present")
	widgetSchema, ok := doc.Components.Schemas.Get("Widget")
	require.True(t, ok, "Widget must be in components/schemas")
	require.Equal(t, "object", widgetSchema.Type)
	_, hasID := widgetSchema.Properties.Get("id")
	require.True(t, hasID, "Widget must have id property")
	_, hasName := widgetSchema.Properties.Get("name")
	require.True(t, hasName, "Widget must have name property")

	// The POST body must reference Widget via $ref
	item, ok := doc.Paths.Get("/widgets")
	require.True(t, ok)
	require.NotNil(t, item.Post)
	require.NotNil(t, item.Post.RequestBody)
	mt, ok := item.Post.RequestBody.Content.Get("application/json")
	require.True(t, ok, "application/json body must exist")
	require.Equal(t, "#/components/schemas/Widget", mt.Schema.Ref)
}

// TestOAS3Converter_UnusedNamedTypeNotEmitted verifies that a named type
// defined in RAML types: but never referenced from a path is NOT emitted to
// components/schemas (avoids linter warnings about unused components).
func TestOAS3Converter_UnusedNamedTypeNotEmitted(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Types API
version: v1
types:
  UnusedType:
    type: object
    properties:
      x:
        type: string
/ping:
  get:
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	if doc.Components.Schemas != nil {
		_, found := doc.Components.Schemas.Get("UnusedType")
		require.False(t, found, "UnusedType must not appear in components/schemas")
	}
}

// TestOAS3Converter_InlineObjectBody verifies that an inline object body
// (not typed as a named type) is emitted with its properties inline.
func TestOAS3Converter_InlineObjectBody(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Inline API
version: v1
/items:
  post:
    body:
      application/json:
        type: object
        properties:
          name:
            type: string
          count:
            type: integer
    responses:
      201:
        description: Created
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	item, ok := doc.Paths.Get("/items")
	require.True(t, ok)
	require.NotNil(t, item.Post.RequestBody)
	mt, ok := item.Post.RequestBody.Content.Get("application/json")
	require.True(t, ok)

	schema := mt.Schema
	require.Empty(t, schema.Ref, "inline body must not use $ref")
	require.Equal(t, "object", schema.Type)
	require.NotNil(t, schema.Properties)
	_, hasName := schema.Properties.Get("name")
	require.True(t, hasName)
	_, hasCount := schema.Properties.Get("count")
	require.True(t, hasCount)
}

// TestOAS3Converter_NullableUnion verifies that a RAML union of a type with nil
// (e.g. `string | nil`) is emitted as nullable:true with the base type set.
func TestOAS3Converter_NullableUnion(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Nullable API
version: v1
/resource:
  get:
    responses:
      200:
        description: OK
        body:
          application/json:
            type: object
            properties:
              optionalName:
                type: string | nil
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	item, ok := doc.Paths.Get("/resource")
	require.True(t, ok)
	resp, ok := item.Get.Responses.Get("200")
	require.True(t, ok)
	mt, ok := resp.Content.Get("application/json")
	require.True(t, ok)

	props := mt.Schema.Properties
	require.NotNil(t, props)
	nameSchema, ok := props.Get("optionalName")
	require.True(t, ok)
	require.Equal(t, "string", nameSchema.Type)
	require.True(t, nameSchema.Nullable, "string | nil must be nullable")
}

// TestOAS3Converter_ResponseBody verifies that a RAML response body is emitted
// with the correct status code key and content media type schema.
func TestOAS3Converter_ResponseBody(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Response API
version: v1
/items:
  get:
    responses:
      200:
        description: List of items
        body:
          application/json:
            type: object
            properties:
              items:
                type: array
                items:
                  type: string
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	item, ok := doc.Paths.Get("/items")
	require.True(t, ok)
	resp, ok := item.Get.Responses.Get("200")
	require.True(t, ok)
	require.Equal(t, "List of items", resp.Description)

	mt, ok := resp.Content.Get("application/json")
	require.True(t, ok)
	require.Equal(t, "object", mt.Schema.Type)
	require.NotNil(t, mt.Schema.Properties)
	itemsProp, ok := mt.Schema.Properties.Get("items")
	require.True(t, ok)
	require.Equal(t, "array", itemsProp.Type)
	require.NotNil(t, itemsProp.Items)
	require.Equal(t, "string", itemsProp.Items.Type)
}

// TestOAS3Converter_SecuritySchemeBasic verifies that a RAML Basic Authentication
// security scheme is emitted as type:http, scheme:basic in components.
func TestOAS3Converter_SecuritySchemeBasic(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Secure API
version: v1
securitySchemes:
  basic:
    type: Basic Authentication
/protected:
  get:
    securedBy: [basic]
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	require.NotNil(t, doc.Components.SecuritySchemes)
	ss, ok := doc.Components.SecuritySchemes.Get("basic")
	require.True(t, ok)
	require.Equal(t, "http", ss.Type)
	require.Equal(t, "basic", ss.Scheme)

	// The operation must reference the security scheme.
	item, ok := doc.Paths.Get("/protected")
	require.True(t, ok)
	require.Len(t, item.Get.Security, 1)
	_, hasBasic := item.Get.Security[0]["basic"]
	require.True(t, hasBasic)
}

// TestOAS3Converter_URIParameters verifies that RAML uriParameters on a path
// are emitted as OAS3 path parameters with required:true.
func TestOAS3Converter_URIParameters(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Resource API
version: v1
/items/{itemId}:
  uriParameters:
    itemId:
      type: string
      description: The item ID
  get:
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	item, ok := doc.Paths.Get("/items/{itemId}")
	require.True(t, ok)
	require.Len(t, item.Parameters, 1)

	p := item.Parameters[0]
	require.Equal(t, "itemId", p.Name)
	require.Equal(t, "path", p.In)
	require.True(t, p.Required, "path parameters must be required")
	require.Equal(t, "The item ID", p.Description)
	require.NotNil(t, p.Schema)
	require.Equal(t, "string", p.Schema.Type)
}

// TestOAS3Converter_DefaultResponse verifies that an operation with no
// explicit responses gets a synthetic "default" response placeholder.
func TestOAS3Converter_DefaultResponse(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Minimal API
version: v1
/ping:
  get: {}
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	item, ok := doc.Paths.Get("/ping")
	require.True(t, ok)
	_, hasDefault := item.Get.Responses.Get("default")
	require.True(t, hasDefault, "operation with no responses must have a default placeholder")
}

// TestOAS3Converter_ExtensionsOnEmptySchema is a regression test for the
// mergeJSONExtensions bug: when an OAS3Schema has all omitempty fields
// (marshals to "{}") but has Extensions set, the merge must produce valid JSON
// rather than "{,…}".
//
// Trigger: an `any`-typed body with a RAML annotation produces an OAS3Schema
// where every struct field is zero/omitempty (no type, no format, …) yet
// Extensions is non-nil, causing the base to marshal as "{}".
func TestOAS3Converter_ExtensionsOnEmptySchema(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Annotation API
version: v1
annotationTypes:
  custom:
    type: string
types:
  TaggedAny:
    type: any
    (custom): hello
/resource:
  get:
    responses:
      200:
        description: OK
        body:
          application/json:
            type: TaggedAny
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	// Serialising must not error and must be valid JSON.
	data, err := doc.MarshalJSON()
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out), "MarshalJSON produced invalid JSON: %s", data)

	// The TaggedAny component must carry the x-custom extension.
	components, ok := out["components"].(map[string]any)
	require.True(t, ok, "components must be present")
	schemas, ok := components["schemas"].(map[string]any)
	require.True(t, ok, "components/schemas must be present")
	taggedAny, ok := schemas["TaggedAny"].(map[string]any)
	require.True(t, ok, "TaggedAny must be in components/schemas")
	require.Equal(t, "hello", taggedAny["x-custom"])
}

// TestOAS3Converter_ImplicitURIParameter verifies that a path template variable
// with no explicit uriParameters block (e.g. /{brand}) is converted correctly
// rather than failing with "shape is not unwrapped". Regression test for the
// synthesized BaseShape in validateAndSynthesizeURIParameters.
func TestOAS3Converter_ImplicitURIParameter(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Brand API
version: v1
/brands:
  /{brand}:
    get:
      responses:
        200:
          description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	item, ok := doc.Paths.Get("/brands/{brand}")
	require.True(t, ok, "path /brands/{brand} must exist")
	require.NotNil(t, item.Get)

	// The implicit {brand} parameter must appear as a path parameter.
	require.Len(t, item.Parameters, 1)
	p := item.Parameters[0]
	require.Equal(t, "brand", p.Name)
	require.Equal(t, "path", p.In)
	require.True(t, p.Required)
	require.NotNil(t, p.Schema)
	require.Equal(t, "string", p.Schema.Type)
}

// TestOAS3Converter_NestedPaths verifies that RAML nested resources are
// flattened into individual OAS3 path entries.
func TestOAS3Converter_NestedPaths(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Nested API
version: v1
/users:
  get:
    responses:
      200:
        description: OK
  /active:
    get:
      responses:
        200:
          description: OK
    /recent:
      get:
        responses:
          200:
            description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	paths := []string{"/users", "/users/active", "/users/active/recent"}
	for _, p := range paths {
		_, ok := doc.Paths.Get(p)
		require.True(t, ok, "path %q must exist", p)
	}
}

// TestOAS3Converter_IntermediateContainerNotEmitted verifies that RAML resources
// that act as containers (description/uriParameters only, no HTTP methods) are
// NOT emitted as OAS3 path items. Linters flag such entries because the path
// template variables are not matched to any operation. URI parameters are
// already propagated to child paths by the unwrap step.
func TestOAS3Converter_IntermediateContainerNotEmitted(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Container API
version: v1
/applications/{application_id}:
  description: Application container.
  uriParameters:
    application_id:
      type: string
  /bindings:
    description: Manages application bindings.
    /tenants:
      get:
        description: List tenant bindings.
        responses:
          200:
            description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	// Container paths without operations must not appear.
	_, hasContainer := doc.Paths.Get("/applications/{application_id}")
	require.False(t, hasContainer, "container path with no operations must not be emitted")

	_, hasBind := doc.Paths.Get("/applications/{application_id}/bindings")
	require.False(t, hasBind, "intermediate path with no operations must not be emitted")

	// The leaf path with an operation must be emitted with all inherited params.
	item, ok := doc.Paths.Get("/applications/{application_id}/bindings/tenants")
	require.True(t, ok, "leaf path must be emitted")
	require.NotNil(t, item.Get)

	// application_id must be present on the leaf path item (propagated from parent).
	require.Len(t, item.Parameters, 1)
	require.Equal(t, "application_id", item.Parameters[0].Name)
	require.Equal(t, "path", item.Parameters[0].In)
}

// TestOAS3Converter_NullSecuredBy verifies that securedBy: [null] (RAML anonymous
// access) is emitted as an empty security requirement {} in OAS3, not {"null":[]}.
func TestOAS3Converter_NullSecuredBy(t *testing.T) {
	const fixture = `#%RAML 1.0
title: Mixed Auth API
version: v1
securitySchemes:
  bearer:
    type: Pass Through
/public:
  get:
    securedBy: [null]
    responses:
      200:
        description: OK
/private:
  get:
    securedBy: [bearer]
    responses:
      200:
        description: OK
/mixed:
  get:
    securedBy: [null, bearer]
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))

	// /public: single empty security requirement
	pub, ok := doc.Paths.Get("/public")
	require.True(t, ok)
	require.Len(t, pub.Get.Security, 1)
	require.Empty(t, pub.Get.Security[0], "null securedBy must produce empty OAS3 security requirement {}")

	// /private: single named requirement
	priv, ok := doc.Paths.Get("/private")
	require.True(t, ok)
	require.Len(t, priv.Get.Security, 1)
	_, hasBearer := priv.Get.Security[0]["bearer"]
	require.True(t, hasBearer)

	// /mixed: one empty + one named requirement
	mixed, ok := doc.Paths.Get("/mixed")
	require.True(t, ok)
	require.Len(t, mixed.Get.Security, 2)
	var foundEmpty, foundBearer bool
	for _, req := range mixed.Get.Security {
		if len(req) == 0 {
			foundEmpty = true
		}
		if _, ok := req["bearer"]; ok {
			foundBearer = true
		}
	}
	require.True(t, foundEmpty, "must have empty requirement for null")
	require.True(t, foundBearer, "must have bearer requirement")
}

func TestOAS3Converter_QueryString_Object(t *testing.T) {
	const fixture = `#%RAML 1.0
title: QS API
version: v1
/resource:
  get:
    queryString:
      type: object
      properties:
        filter:
          type: string
        sort?:
          type: string
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))
	item, ok := doc.Paths.Get("/resource")
	require.True(t, ok)
	require.NotNil(t, item.Get)

	params := item.Get.Parameters
	require.GreaterOrEqual(t, len(params), 2)
	filterParam := params[0]
	sortParam := params[1]
	require.Equal(t, "filter", filterParam.Name)
	require.Equal(t, "query", filterParam.In)
	require.True(t, filterParam.Required)
	require.Equal(t, "string", filterParam.Schema.Type)

	require.Equal(t, "sort", sortParam.Name)
	require.Equal(t, "query", sortParam.In)
	require.Equal(t, "string", sortParam.Schema.Type)
	require.False(t, sortParam.Required)
}

func TestOAS3Converter_QueryString_NonObject(t *testing.T) {
	const fixture = `#%RAML 1.0
title: QS API
version: v1
/resource:
  get:
    queryString:
      type: string
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))
	item, ok := doc.Paths.Get("/resource")
	require.True(t, ok)
	require.NotNil(t, item.Get)

	require.Empty(t, item.Get.Parameters)
	require.NotNil(t, item.Get.Extensions)
	require.NotNil(t, item.Get.Extensions["x-query-string"])
}

func TestOAS3Converter_OAuth2_Implicit(t *testing.T) {
	const fixture = `#%RAML 1.0
title: OAuth API
version: v1
securitySchemes:
  oauth2:
    type: OAuth 2.0
    settings:
      authorizationUri: https://auth.example.com/oauth/authorize
      accessTokenUri: https://auth.example.com/oauth/token
      authorizationGrants: [implicit]
      scopes: [read, write]
/protected:
  get:
    securedBy: [oauth2]
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))
	ss, ok := doc.Components.SecuritySchemes.Get("oauth2")
	require.True(t, ok)
	require.Equal(t, "oauth2", ss.Type)
	require.NotNil(t, ss.Flows.Implicit)
	require.Equal(t, "https://auth.example.com/oauth/authorize", ss.Flows.Implicit.AuthorizationURL)
}

func TestOAS3Converter_OAuth2_Password(t *testing.T) {
	const fixture = `#%RAML 1.0
title: OAuth API
version: v1
securitySchemes:
  oauth2:
    type: OAuth 2.0
    settings:
      accessTokenUri: https://auth.example.com/oauth/token
      authorizationGrants: [password]
      scopes: [read]
/protected:
  get:
    securedBy: [oauth2]
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))
	ss, ok := doc.Components.SecuritySchemes.Get("oauth2")
	require.True(t, ok)
	require.NotNil(t, ss.Flows.Password)
	require.Equal(t, "https://auth.example.com/oauth/token", ss.Flows.Password.TokenURL)
}

func TestOAS3Converter_OAuth2_ClientCredentials(t *testing.T) {
	const fixture = `#%RAML 1.0
title: OAuth API
version: v1
securitySchemes:
  oauth2:
    type: OAuth 2.0
    settings:
      accessTokenUri: https://auth.example.com/oauth/token
      authorizationGrants: [client_credentials]
      scopes: [admin]
/protected:
  get:
    securedBy: [oauth2]
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))
	ss, ok := doc.Components.SecuritySchemes.Get("oauth2")
	require.True(t, ok)
	require.NotNil(t, ss.Flows.ClientCredentials)
	require.Equal(t, "https://auth.example.com/oauth/token", ss.Flows.ClientCredentials.TokenURL)
}

func TestOAS3Converter_OAuth2_AuthorizationCode(t *testing.T) {
	const fixture = `#%RAML 1.0
title: OAuth API
version: v1
securitySchemes:
  oauth2:
    type: OAuth 2.0
    settings:
      authorizationUri: https://auth.example.com/oauth/authorize
      accessTokenUri: https://auth.example.com/oauth/token
      authorizationGrants: [authorization_code]
      scopes: [read, write]
/protected:
  get:
    securedBy: [oauth2]
    responses:
      200:
        description: OK
`
	doc := mustConvert(t, mustParseAPI(t, fixture))
	ss, ok := doc.Components.SecuritySchemes.Get("oauth2")
	require.True(t, ok)
	require.NotNil(t, ss.Flows.AuthorizationCode)
	require.Equal(t, "https://auth.example.com/oauth/authorize", ss.Flows.AuthorizationCode.AuthorizationURL)
	require.Equal(t, "https://auth.example.com/oauth/token", ss.Flows.AuthorizationCode.TokenURL)
}
