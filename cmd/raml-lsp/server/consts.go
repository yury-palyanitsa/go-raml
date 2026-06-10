package server

import raml "github.com/acronis/go-raml/v3"

// ramlKeyDocs maps RAML facet / keyword names to their RAML 1.0 spec
// descriptions, shown as hover documentation in the completion list.
// Keys not present here get no documentation tooltip.
var ramlKeyDocs = map[string]string{
	// API / Library root
	raml.FacetTitle:             "A short, plain-text label for the API.",
	raml.FacetVersion:           "The version of the API, for example `v1`.",
	raml.FacetBaseUri:           "A URI that serves as the base for URIs of all resources. Can be a template URI, e.g. `https://api.example.com/{version}`.",
	raml.FacetBaseUriParameters: "Named parameters used in the `baseUri` template.",
	raml.FacetProtocols:         "The protocols supported by the API. Must be `HTTP`, `HTTPS`, or both.",
	raml.FacetMediaType:         "The default media type(s) for request and response bodies, e.g. `application/json`. Overridable per body.",
	raml.FacetDocumentation:     "A sequence of user-guide documents for the API. Each item must have a `title` and `content`.",
	raml.FacetTypes:             "Declarations of (data) types for use within the API.",
	raml.FacetAnnotationTypes:   "Declarations of annotation types for use by annotations.",
	raml.FacetSecuritySchemes:   "Declarations of security schemes for use within the API.",
	raml.FacetUses:              "Imports external RAML libraries into the current file, making their types, traits, and other assets available via a namespace prefix.",

	// Common type facets
	raml.FacetType:        "The type which this type extends or wraps. Can be a built-in type (`object`, `array`, `string`, etc.) or a user-defined type name.",
	raml.FacetDisplayName: "An alternate, human-friendly name.",
	raml.FacetDescription: "A substantial, human-friendly description. Supports [Markdown](https://github.github.com/gfm/).",
	raml.FacetDefault:     "A default value used when the instance of this type is entirely absent from the request or response.",
	raml.FacetRequired:    "Whether the property is required. **Default:** true.",
	raml.FacetExample:     "A single example of an instance of this type. Mutually exclusive with `examples`.",
	raml.FacetExamples:    "Multiple named examples of this type. Each key is a unique example identifier. Mutually exclusive with `example`.",
	raml.FacetFacets:      "User-defined facets that subtype declarations can specialise. The value is a map of facet name to type declaration.",
	raml.FacetEnum:        "An enumeration of all valid values for instances of this type. An instance MUST equal one of these values.",
	raml.FacetXml:         "Configuration for XML serialization of this type (attribute, wrapped, name, namespace, prefix).",

	// Object type
	raml.FacetProperties:           "The properties that instances of this object type can or must have.",
	raml.FacetAdditionalProperties: "Whether object instances may contain properties beyond those declared in `properties`. **Default:** true.",
	raml.FacetDiscriminator:        "Name of a property used to determine the concrete type at runtime (for unions/inheritance). Must match a declared property name.",
	raml.FacetDiscriminatorValue:   "The value stored in the discriminator property that identifies this type. **Default:** the type name.",
	raml.FacetMinProperties:        "Minimum number of properties an object instance must have.",
	raml.FacetMaxProperties:        "Maximum number of properties an object instance may have.",

	// Array type
	raml.FacetItems:       "The type that every element of this array must conform to. Can be a type name or an inline type declaration.",
	raml.FacetMinItems:    "Minimum number of items in the array. **Default:** 0.",
	raml.FacetMaxItems:    "Maximum number of items in the array. **Default:** 2147483647.",
	raml.FacetUniqueItems: "If `true`, all items in the array must be unique.",

	// String type
	raml.FacetPattern:   "A regular expression that string values must match.",
	raml.FacetMinLength: "Minimum length of the string value. **Default:** 0.",
	raml.FacetMaxLength: "Maximum length of the string value. **Default:** 2147483647.",

	// Number / Integer type
	raml.FacetMinimum:    "The minimum numeric value (inclusive).",
	raml.FacetMaximum:    "The maximum numeric value (inclusive).",
	raml.FacetMultipleOf: "A numeric value is valid only if dividing it by this keyword's value yields an integer.",
	raml.FacetFormat:     "The format of the value. See type-specific documentation for allowed values (e.g. `int32`, `float`, `rfc3339`).",

	// File type
	raml.FacetFileTypes: "A list of valid MIME content-type strings for file values. Use `*/*` to allow any type.",

	// Endpoint (resource) keys
	raml.FacetUriParameters: "Detailed type declarations for URI template parameters of this resource.",
	raml.FacetIs:            "A list of traits to apply to all methods of this resource, or to this specific method.",
	raml.FacetSecuredBy:     "The security schemes that apply to this resource or method. Use `null` in the list to indicate the method may be called without authentication.",
	raml.FacetResourceTypes: "Declarations of resource types for use within the API.",
	raml.FacetTraits:        "Declarations of traits for use within the API.",

	// HTTP method keys (operation declarations inside a resource block)
	raml.MethodGet:     "Define the GET operation for this resource.",
	raml.MethodPost:    "Define the POST operation for this resource.",
	raml.MethodPut:     "Define the PUT operation for this resource.",
	raml.MethodDelete:  "Define the DELETE operation for this resource.",
	raml.MethodPatch:   "Define the PATCH operation for this resource.",
	raml.MethodHead:    "Define the HEAD operation for this resource.",
	raml.MethodOptions: "Define the OPTIONS operation for this resource.",

	// Method body keys
	raml.FacetQueryParameters: "Detailed type declarations for the query parameters of this method. Mutually exclusive with `queryString`.",
	raml.FacetQueryString:     "The query string as a whole (typed as a scalar or object). Mutually exclusive with `queryParameters`.",
	raml.FacetHeaders:         "Detailed type declarations for the request or response headers.",
	raml.FacetBody:            "The request or response body. Keys are media-type strings; values are type declarations.",
	raml.FacetResponses:       "The possible HTTP responses. Keys are status codes; values are response declarations.",
}

// ramlBuiltinTypeDocs maps each RAML 1.0 built-in type name to its Markdown
// documentation, shown in the hover popup when the cursor is on a primitive
// type expression such as `type: string` or `string | integer`.
var ramlBuiltinTypeDocs = map[string]string{
	raml.TypeAny: "**any** — built-in RAML type\n\n" +
		"Every type, whether built-in or user-defined, ultimately inherits from `any`. " +
		"By definition, `any` imposes no restrictions — any data value is valid against it.",

	raml.TypeObject: "**object** — built-in RAML type\n\n" +
		"A structured type whose instances have named properties. " +
		"Properties are declared with the `properties` facet; additional properties are allowed by default. " +
		"Supports facets: `properties`, `minProperties`, `maxProperties`, `additionalProperties`, " +
		"`discriminator`, `discriminatorValue`.",

	raml.TypeArray: "**array** — built-in RAML type\n\n" +
		"An ordered sequence of items, all conforming to the type declared by the `items` facet. " +
		"Can be declared via the `[]` suffix shorthand or the `array` keyword. " +
		"Supports facets: `items`, `minItems`, `maxItems`, `uniqueItems`.",

	raml.TypeString: "**string** — built-in RAML type\n\n" +
		"A sequence of Unicode characters. " +
		"Supports facets: `pattern`, `minLength`, `maxLength`.",

	raml.TypeNumber: "**number** — built-in RAML type\n\n" +
		"A numeric value, either integer or floating-point. " +
		"Supports facets: `minimum`, `maximum`, `format` (`float`, `double`), `multipleOf`.",

	raml.TypeInteger: "**integer** — built-in RAML type\n\n" +
		"A whole number with no fractional part. " +
		"Inherits all facets from `number`: `minimum`, `maximum`, `format` (`int`, `int8`, `int16`, `int32`, `int64`, `long`), `multipleOf`.",

	raml.TypeBoolean: "**boolean** — built-in RAML type\n\n" +
		"A logical value: `true` or `false`. No additional facets.",

	raml.TypeDateOnly: "**date-only** — built-in RAML type\n\n" +
		"The \"full-date\" notation of [RFC 3339](https://tools.ietf.org/html/rfc3339): `yyyy-mm-dd`. " +
		"Does not carry time or time zone-offset information.",

	raml.TypeTimeOnly: "**time-only** — built-in RAML type\n\n" +
		"The \"partial-time\" notation of [RFC 3339](https://tools.ietf.org/html/rfc3339): `hh:mm:ss[.fff...]`. " +
		"Does not carry date or time zone-offset information.",

	raml.TypeDatetimeOnly: "**datetime-only** — built-in RAML type\n\n" +
		"Combined date and time with a `T` separator: `yyyy-mm-ddThh:mm:ss[.fff...]`. No time zone offset.",

	raml.TypeDatetime: "**datetime** — built-in RAML type\n\n" +
		"A point in time. By default uses the RFC 3339 `date-time` format. " +
		"When `format: rfc2616` is set, uses the HTTP date format. " +
		"Supports facet: `format` (`rfc3339` | `rfc2616`).",

	raml.TypeFile: "**file** — built-in RAML type\n\n" +
		"Represents file content transmitted in a request body, typically via multipart forms. " +
		"For text-based media types the content is base64-encoded; for binary media types it is transmitted as raw bytes. " +
		"Supports facets: `fileTypes`, `minLength`, `maxLength`.",

	raml.TypeNil: "**nil** — built-in RAML type\n\n" +
		"Allows only nil/null data values. In YAML: `null` or `~`; in JSON: `null`. " +
		"Using `| nil` in a type expression makes that type nilable. " +
		"For a union of one other type and `nil`, the trailing `?` shorthand is equivalent (e.g. `string?`).",
}

var ramlKeyDocsByKind = map[ContextKind]map[string]string{
	CKEndpoint: {
		// In an endpoint block, `type` means "apply a resource type", not
		// "the base data type this shape extends".
		raml.FacetType: "The resource type that this resource inherits. Must be a name declared in `resourceTypes`.",
	},
	CKOperation: {
		// In a method body, `type` is not present, but `is` differs slightly
		// in scope from the resource-level `is`.
		raml.FacetIs: "A list of traits to apply to this specific method.",
	},
}

// httpStatusPhrases maps HTTP status codes to their standard reason phrases.
var httpStatusPhrases = map[string]string{
	"100": "Continue",
	"101": "Switching Protocols",
	"200": "OK",
	"201": "Created",
	"202": "Accepted",
	"203": "Non-Authoritative Information",
	"204": "No Content",
	"206": "Partial Content",
	"301": "Moved Permanently",
	"302": "Found",
	"303": "See Other",
	"304": "Not Modified",
	"307": "Temporary Redirect",
	"308": "Permanent Redirect",
	"400": "Bad Request",
	"401": "Unauthorized",
	"402": "Payment Required",
	"403": "Forbidden",
	"404": "Not Found",
	"405": "Method Not Allowed",
	"406": "Not Acceptable",
	"407": "Proxy Authentication Required",
	"408": "Request Timeout",
	"409": "Conflict",
	"410": "Gone",
	"411": "Length Required",
	"412": "Precondition Failed",
	"413": "Content Too Large",
	"414": "URI Too Long",
	"415": "Unsupported Media Type",
	"416": "Range Not Satisfiable",
	"417": "Expectation Failed",
	"418": "I'm a Teapot",
	"422": "Unprocessable Content",
	"423": "Locked",
	"424": "Failed Dependency",
	"425": "Too Early",
	"426": "Upgrade Required",
	"428": "Precondition Required",
	"429": "Too Many Requests",
	"431": "Request Header Fields Too Large",
	"451": "Unavailable For Legal Reasons",
	"500": "Internal Server Error",
	"501": "Not Implemented",
	"502": "Bad Gateway",
	"503": "Service Unavailable",
	"504": "Gateway Timeout",
	"505": "HTTP Version Not Supported",
	"507": "Insufficient Storage",
	"511": "Network Authentication Required",
}

// httpStatusDescriptions maps HTTP status codes to one-sentence descriptions.
var httpStatusDescriptions = map[string]string{
	"100": "The server has received the request headers; the client should proceed to send the request body.",
	"101": "The server agrees to switch protocols as requested by the client.",
	"200": "The request succeeded.",
	"201": "The request succeeded and a new resource was created as a result.",
	"202": "The request has been received but not yet acted upon.",
	"203": "The request succeeded but the response metadata may have been modified by a transforming proxy.",
	"204": "The request succeeded. There is no content to send in the response body.",
	"206": "The server is delivering only part of the resource due to a range request.",
	"301": "The URI of the requested resource has been changed permanently.",
	"302": "The URI of the requested resource has been changed temporarily.",
	"303": "The server is redirecting the client to a different resource via a GET request.",
	"304": "Used with conditional requests; the response has not been modified since the last request.",
	"307": "The URI has been temporarily changed; the client must repeat the request using the same HTTP method.",
	"308": "The URI has been permanently changed; the client must repeat the request using the same HTTP method.",
	"400": "The server cannot process the request due to a client error (malformed syntax, invalid framing, etc.).",
	"401": "The client must authenticate itself to get the requested response.",
	"402": "Reserved for future use.",
	"403": "The client does not have access rights to the content — the server refuses to authorize.",
	"404": "The server cannot find the requested resource.",
	"405": "The HTTP method is known but has been disabled for this resource.",
	"406": "No content matching the `Accept` headers sent in the request is available.",
	"407": "Authentication via a proxy is required before the request can be served.",
	"408": "The server timed out waiting for the request.",
	"409": "The request conflicts with the current state of the server.",
	"410": "The requested resource is permanently gone; no forwarding address is known.",
	"411": "The server requires a `Content-Length` header in the request.",
	"412": "The client indicated preconditions in its headers that the server does not meet.",
	"413": "The request body is larger than limits defined by the server.",
	"414": "The URI requested is longer than the server is willing to interpret.",
	"415": "The media format of the requested data is not supported by the server.",
	"416": "The range specified in the `Range` header cannot be fulfilled.",
	"417": "The expectation in the `Expect` request-header field cannot be met by the server.",
	"418": "The server refuses to brew coffee because it is, permanently, a teapot.",
	"422": "The request was well-formed but contains semantic errors and cannot be followed.",
	"423": "The resource being accessed is locked.",
	"424": "The request failed because it depended on another request that failed.",
	"425": "The server is unwilling to risk processing a request that might be replayed.",
	"426": "The client must switch to a different protocol before making this request.",
	"428": "The origin server requires the request to be conditional.",
	"429": "The user has sent too many requests in a given amount of time (rate limiting).",
	"431": "The server is unwilling to process the request because its header fields are too large.",
	"451": "The user is requesting an illegal resource, e.g. a resource censored by law.",
	"500": "The server has encountered a situation it does not know how to handle.",
	"501": "The HTTP method is not supported by the server and cannot be handled.",
	"502": "The server, acting as a gateway, received an invalid response from an upstream server.",
	"503": "The server is not ready to handle the request (offline or overloaded).",
	"504": "The server, acting as a gateway, did not get a response in time from an upstream server.",
	"505": "The HTTP version used in the request is not supported by the server.",
	"507": "The server has insufficient storage to fulfill the request.",
	"511": "The client needs to authenticate to gain network access.",
}


type ramlKey struct{ label, detail string }

var apiRootKeys = []ramlKey{
	{raml.FacetTitle, "string · required"},
	{raml.FacetVersion, "string"},
	{raml.FacetBaseUri, "URI template"},
	{raml.FacetBaseUriParameters, "map"},
	{raml.FacetProtocols, "HTTP | HTTPS"},
	{raml.FacetMediaType, "MIME type"},
	{raml.FacetDocumentation, "list"},
	{raml.FacetDescription, "markdown"},
	{raml.FacetUses, "map"},
	{raml.FacetTypes, "map"},
	{raml.FacetAnnotationTypes, "map"},
	{raml.FacetTraits, "map"},
	{raml.FacetResourceTypes, "map"},
	{raml.FacetSecuritySchemes, "map"},
}

var libraryRootKeys = []ramlKey{
	{raml.FacetUses, "map"},
	{raml.FacetTypes, "map"},
	{raml.FacetAnnotationTypes, "map"},
	{raml.FacetTraits, "map"},
	{raml.FacetResourceTypes, "map"},
	{raml.FacetSecuritySchemes, "map"},
	{raml.FacetDescription, "markdown"},
}

// commonTypeKeys are valid for every shape type.
var commonTypeKeys = []ramlKey{
	{raml.FacetType, "type expr"},
	{raml.FacetDisplayName, "string"},
	{raml.FacetDescription, "markdown"},
	{raml.FacetDefault, "any"},
	{raml.FacetRequired, "boolean"},
	{raml.FacetExample, "any"},
	{raml.FacetExamples, "map"},
	{raml.FacetFacets, "map"},
	{raml.FacetEnum, "list"},
	{raml.FacetXml, "map"},
}

var objectTypeKeys = append(append([]ramlKey{}, commonTypeKeys...), []ramlKey{
	{raml.FacetProperties, "map"},
	{raml.FacetAdditionalProperties, "boolean"},
	{raml.FacetDiscriminator, "property name"},
	{raml.FacetDiscriminatorValue, "string"},
	{raml.FacetMinProperties, "integer ≥ 0"},
	{raml.FacetMaxProperties, "integer ≥ 0"},
}...)

var arrayTypeKeys = append(append([]ramlKey{}, commonTypeKeys...), []ramlKey{
	{raml.FacetItems, "type expr"},
	{raml.FacetMinItems, "integer ≥ 0"},
	{raml.FacetMaxItems, "integer ≥ 0"},
	{raml.FacetUniqueItems, "boolean"},
}...)

var stringTypeKeys = append(append([]ramlKey{}, commonTypeKeys...), []ramlKey{
	{raml.FacetPattern, "regex"},
	{raml.FacetMinLength, "integer ≥ 0"},
	{raml.FacetMaxLength, "integer ≥ 0"},
}...)

var numberTypeKeys = append(append([]ramlKey{}, commonTypeKeys...), []ramlKey{
	{raml.FacetMinimum, "number"},
	{raml.FacetMaximum, "number"},
	{raml.FacetMultipleOf, "number > 0"},
	{raml.FacetFormat, "int | long | float | double"},
}...)

var datetimeTypeKeys = append(append([]ramlKey{}, commonTypeKeys...), []ramlKey{
	{raml.FacetFormat, "rfc3339 | rfc2616"},
}...)

var fileTypeKeys = append(append([]ramlKey{}, commonTypeKeys...), []ramlKey{
	{raml.FacetFileTypes, "list of MIME types"},
}...)

var endpointKeys = []ramlKey{
	{raml.FacetDescription, "markdown"},
	{raml.FacetDisplayName, "string"},
	{raml.FacetIs, "list of traits"},
	{raml.FacetType, "resource type"},
	{raml.FacetSecuredBy, "list"},
	{raml.FacetUriParameters, "map"},
	{raml.MethodGet, "operation"},
	{raml.MethodPost, "operation"},
	{raml.MethodPut, "operation"},
	{raml.MethodDelete, "operation"},
	{raml.MethodPatch, "operation"},
	{raml.MethodHead, "operation"},
	{raml.MethodOptions, "operation"},
}

var methodBodyKeys = []ramlKey{
	{raml.FacetDescription, "markdown"},
	{raml.FacetDisplayName, "string"},
	{raml.FacetIs, "list of traits"},
	{raml.FacetSecuredBy, "list"},
	{raml.FacetProtocols, "HTTP | HTTPS"},
	{raml.FacetQueryParameters, "map"},
	{raml.FacetQueryString, "type expr"},
	{raml.FacetHeaders, "map"},
	{raml.FacetBody, "map"},
	{raml.FacetResponses, "map"},
}

// responseKeys are valid inside a single response definition (responses: 200:).
var responseKeys = []ramlKey{
	{raml.FacetDescription, "markdown"},
	{raml.FacetHeaders, "map"},
	{raml.FacetBody, "map"},
}

// securitySchemeKeys are valid inside a single security scheme definition.
var securitySchemeKeys = []ramlKey{
	{raml.FacetType, "required · OAuth 2.0 | OAuth 1.0 | Basic Authentication | Digest Authentication | Pass Through"},
	{raml.FacetDisplayName, "string"},
	{raml.FacetDescription, "markdown"},
	{raml.FacetDescribedBy, "headers, queryParameters, responses"},
	{raml.FacetSettings, "map · type-specific settings"},
}

// ---- Value-side completions for specific known keys ----

// ramlFormatValues lists valid RAML `format:` facet values.
// For number/integer: int, int8, int16, int32, int64, long, float, double.
// For datetime: rfc3339, rfc2616.
var ramlFormatValues = []string{
	"int", "int8", "int16", "int32", "int64", "long",
	"float", "double",
	"rfc2616", "rfc3339",
}

// ramlProtocolValues are the valid `protocols:` values in RAML 1.0.
var ramlProtocolValues = []string{"HTTP", "HTTPS"}

// commonMediaTypes are frequently used MIME types offered for `mediaType:`,
// `body:` key-names, and response body key-names.
var commonMediaTypes = []string{
	"application/json",
	"application/xml",
	"application/x-www-form-urlencoded",
	"multipart/form-data",
	"application/octet-stream",
	"text/plain",
	"text/html",
	"text/xml",
	"application/ld+json",
	"application/yaml",
}

// boolCompletions returns `true` / `false` keyword completions.

var types = []string{
	string(raml.OAuth2AuthType),
	string(raml.OAuth1AuthType),
	string(raml.BasicAuthType),
	string(raml.DigestAuthType),
	string(raml.PassThroughAuthType),
}


// builtInTypeNames lists the RAML 1.0 scalar / built-in type names.
var builtInTypeNames = []string{
	raml.TypeString, raml.TypeNumber, raml.TypeInteger, raml.TypeBoolean,
	raml.TypeDateOnly, raml.TypeTimeOnly, raml.TypeDatetimeOnly, raml.TypeDatetime,
	raml.TypeFile, raml.TypeNil, raml.TypeAny, raml.TypeObject, raml.TypeArray,
}
