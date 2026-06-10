package raml

import "github.com/acronis/go-stacktrace"

// DefaultMaxIncludeSize is the default maximum byte size for a single !include
// file (64 KiB). Override with OptWithMaxIncludeSize.
const DefaultMaxIncludeSize int64 = 1 << 16 // 64 KiB

// SetOfScalarTypes contains a set of scalar types
var SetOfScalarTypes = map[string]struct{}{
	TypeString: {}, TypeInteger: {}, TypeNumber: {}, TypeBoolean: {}, TypeDatetime: {}, TypeDatetimeOnly: {},
	TypeDateOnly: {}, TypeTimeOnly: {}, TypeFile: {},
}

// SetOfBuiltInTypes contains all RAML 1.0 built-in type names that cannot be redefined.
var SetOfBuiltInTypes = map[string]struct{}{
	TypeAny:          {},
	TypeString:       {},
	TypeInteger:      {},
	TypeNumber:       {},
	TypeBoolean:      {},
	TypeNil:          {},
	TypeFile:         {},
	TypeDatetime:     {},
	TypeDatetimeOnly: {},
	TypeDateOnly:     {},
	TypeTimeOnly:     {},
	TypeObject:       {},
	TypeArray:        {},
	TypeUnion:        {},
}

// typeSpecificFacets maps shape types to their built-in facets.
var typeSpecificFacets = map[string]map[string]struct{}{
	TypeObject: {
		FacetProperties:           {},
		FacetAdditionalProperties: {},
		FacetMinProperties:        {},
		FacetMaxProperties:        {},
		FacetDiscriminator:        {},
		FacetDiscriminatorValue:   {},
	},
	TypeArray: {
		FacetItems:       {},
		FacetMinItems:    {},
		FacetMaxItems:    {},
		FacetUniqueItems: {},
	},
	TypeString: {
		FacetPattern:   {},
		FacetMinLength: {},
		FacetMaxLength: {},
	},
	TypeInteger: {
		FacetMinimum:    {},
		FacetMaximum:    {},
		FacetMultipleOf: {},
	},
	TypeNumber: {
		FacetMinimum:    {},
		FacetMaximum:    {},
		FacetMultipleOf: {},
	},
	TypeFile: {
		FacetFileTypes: {},
	},
}

// commonFacets are built-in facets that apply to all shape types and therefore
// cannot be used as custom facet names in a "facets:" declaration.
// Note: FacetStrict and FacetValue are example-level properties, not type-level
// built-in facets, so they are intentionally excluded here.
var commonFacets = map[string]struct{}{
	FacetType:           {},
	FacetFacets:         {},
	FacetExample:        {},
	FacetExamples:       {},
	FacetDefault:        {},
	FacetDescription:    {},
	FacetDisplayName:    {},
	FacetRequired:       {},
	FacetEnum:           {},
	FacetAllowedTargets: {},
	FacetSchema:         {},
}

// SetOfNumberFormats contains a set of number formats.
var SetOfNumberFormats = map[string]struct{}{
	"float": {}, "double": {},
}

// SetOfIntegerFormats contains a set of integer formats.
// The int size value (0-3) is used for format-compatibility checks during inheritance.
var SetOfIntegerFormats = map[string]int8{
	// int is an alias for int32
	// long is an alias for int64
	"int8": 0, "int16": 1, "int32": 2, "int": 2, "int64": 3, "long": 3,
}

// SetOfDateTimeFormats contains a set of date-time formats
var SetOfDateTimeFormats = map[string]struct{}{
	"rfc3339": {}, "rfc2616": {},
}

// Standard types according to specification
const (
	TypeAny          = "any"
	TypeString       = "string"
	TypeInteger      = "integer"
	TypeNumber       = "number"
	TypeBoolean      = "boolean"
	TypeDatetime     = "datetime"
	TypeDatetimeOnly = "datetime-only"
	TypeDateOnly     = "date-only"
	TypeTimeOnly     = "time-only"
	TypeArray        = "array"
	TypeObject       = "object"
	TypeFile         = "file"
	TypeNil          = "nil"
	TypeNull         = "null"
)

// Special non-standard types
const (
	TypeUnion     = "union"     // Can be used in RAML
	TypeJSON      = "json"      // Cannot be used in RAML
	TypeComposite = "composite" // Cannot be used in RAML
	TypeRecursive = "recursive" // Cannot be used in RAML
)

const (
	TagNull      = "!!null"
	TagInclude   = "!include"
	TagStr       = "!!str"
	TagTimestamp = "!!timestamp"
	TagInt       = "!!int"
)

const (
	FacetFormat               = "format"
	FacetEnum                 = "enum"
	FacetMinimum              = "minimum"
	FacetMaximum              = "maximum"
	FacetMultipleOf           = "multipleOf"
	FacetMinLength            = "minLength"
	FacetMaxLength            = "maxLength"
	FacetPattern              = "pattern"
	FacetFileTypes            = "fileTypes"
	FacetAdditionalProperties = "additionalProperties"
	FacetProperties           = "properties"
	FacetMinProperties        = "minProperties"
	FacetMaxProperties        = "maxProperties"
	FacetItems                = "items"
	FacetMinItems             = "minItems"
	FacetMaxItems             = "maxItems"
	FacetUniqueItems          = "uniqueItems"
	FacetDiscriminator        = "discriminator"
	FacetDiscriminatorValue   = "discriminatorValue"
	FacetValue                = "value"
	FacetDescription          = "description"
	FacetDisplayName          = "displayName"
	FacetStrict               = "strict"
	FacetRequired             = "required"
	FacetType                 = "type"
	FacetFacets               = "facets"
	FacetExample              = "example"
	FacetExamples             = "examples"
	FacetDefault              = "default"
	FacetAllowedTargets       = "allowedTargets"
	FacetSchema               = "schema"
	FacetHeaders              = "headers"
	FacetQueryParameters      = "queryParameters"
	FacetQueryString          = "queryString"
	FacetResponses            = "responses"
	FacetBody                 = "body"
	FacetAnnotations          = "annotations"
	FacetProtocols            = "protocols"
	FacetSchemes              = "schemes"
	FacetSecuredBy            = "securedBy"
	FacetIs                   = "is"
	FacetTraits               = "traits"
	FacetResourceTypes        = "resourceTypes"
	FacetUses                 = "uses"
	FacetUriParameters        = "uriParameters"
	FacetXml                  = "xml"

	// API / Library / fragment root keys
	FacetTitle             = "title"
	FacetUsage             = "usage"
	FacetVersion           = "version"
	FacetBaseUri           = "baseUri"
	FacetBaseUriParameters = "baseUriParameters"
	FacetMediaType         = "mediaType"
	FacetDocumentation     = "documentation"
	FacetTypes             = "types"
	FacetSchemas           = "schemas" // deprecated alias for "types"
	FacetAnnotationTypes   = "annotationTypes"
	FacetSecuritySchemes   = "securitySchemes"

	// Security scheme keys
	FacetDescribedBy = "describedBy"
	FacetSettings    = "settings"

	// Documentation item keys
	FacetContent = "content"
)

// HTTP method name constants (lowercase, as they appear in RAML resource blocks).
const (
	MethodGet     = "get"
	MethodPost    = "post"
	MethodPut     = "put"
	MethodDelete  = "delete"
	MethodPatch   = "patch"
	MethodHead    = "head"
	MethodOptions = "options"
	MethodTrace   = "trace"
	MethodConnect = "connect"
)

// OAuth security scheme settings keys.
const (
	// Shared OAuth 1.0 / 2.0
	FacetAuthorizationUri = "authorizationUri"

	// OAuth 1.0 settings
	FacetRequestTokenUri     = "requestTokenUri"
	FacetTokenCredentialsUri = "tokenCredentialsUri"
	FacetSignatures          = "signatures"

	// OAuth 2.0 settings
	FacetAccessTokenUri      = "accessTokenUri"
	FacetAuthorizationGrants = "authorizationGrants"
	FacetScopes              = "scopes"
)

// Reserved resource type and trait template variable names.
const (
	ParamResourcePath     = "resourcePath"
	ParamResourcePathName = "resourcePathName"
	ParamMethodName       = "methodName"
)

const (
	XmlAttribute = "attribute"
	XmlWrapped   = "wrapped"
	XmlName      = "name"
	XmlNamespace = "namespace"
	XmlPrefix    = "prefix"
)

const (
	ActionUppercase       = "!uppercase"
	ActionLowercase       = "!lowercase"
	ActionUpperCamelCase  = "!uppercamelcase"
	ActionLowerCamelCase  = "!lowercamelcase"
	ActionUpperUnderscore = "!upperunderscorecase"
	ActionLowerUnderscore = "!lowerunderscorecase"
	ActionUpperHyphen     = "!upperhyphencase"
	ActionLowerHyphen     = "!lowerhyphencase"
	ActionSingularize     = "!singularize"
	ActionPluralize       = "!pluralize"
)

var SetOfActions = map[string]struct{}{
	ActionUppercase:       {},
	ActionLowercase:       {},
	ActionUpperCamelCase:  {},
	ActionLowerCamelCase:  {},
	ActionUpperUnderscore: {},
	ActionLowerUnderscore: {},
	ActionUpperHyphen:     {},
	ActionLowerHyphen:     {},
	ActionSingularize:     {},
	ActionPluralize:       {},
}

const (
	DateTimeFormatRFC3339 = "rfc3339"
	DateTimeFormatRFC2616 = "rfc2616"
)

const (
	FormatDateTime = "date-time"
	FormatDate     = "date"
	FormatTime     = "time"
)

const (
	StacktraceTypeUnwrapping stacktrace.Type = "unwrapping"
	StacktraceTypeResolving  stacktrace.Type = "resolving"
	StacktraceTypeParsing    stacktrace.Type = "parsing"
	StacktraceTypeValidating stacktrace.Type = "validating"
	StacktraceTypeReading    stacktrace.Type = "reading"
	StacktraceTypeLoading    stacktrace.Type = "loading"
)

type SecuritySchemeType string

const (
	BasicAuthType       SecuritySchemeType = "Basic Authentication"
	DigestAuthType      SecuritySchemeType = "Digest Authentication"
	PassThroughAuthType SecuritySchemeType = "Pass Through"
	OAuth1AuthType      SecuritySchemeType = "OAuth 1.0"
	OAuth2AuthType      SecuritySchemeType = "OAuth 2.0"
	NullAuthType        SecuritySchemeType = "null"
)

type DomainLocation string

const (
	APIDomain                    DomainLocation = "API"
	DocumentationItemDomain      DomainLocation = "DocumentationItem"
	ResourceDomain               DomainLocation = "Resource"
	MethodDomain                 DomainLocation = "Method"
	ResponseDomain               DomainLocation = "Response"
	RequestBodyDomain            DomainLocation = "RequestBody"
	ResponseBodyDomain           DomainLocation = "ResponseBody"
	TypeDeclarationDomain        DomainLocation = "TypeDeclaration"
	ExampleDomain                DomainLocation = "Example"
	ResourceTypeDomain           DomainLocation = "ResourceType"
	TraitDomain                  DomainLocation = "Trait"
	SecuritySchemeDomain         DomainLocation = "SecurityScheme"
	SecuritySchemeSettingsDomain DomainLocation = "SecuritySchemeSettings"
	AnnotationTypeDomain         DomainLocation = "AnnotationType"
	LibraryDomain                DomainLocation = "Library"
	OverlayDomain                DomainLocation = "Overlay"
	ExtensionDomain              DomainLocation = "Extension"
)
