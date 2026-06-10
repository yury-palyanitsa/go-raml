package converter

type JSONLDGraph struct {
	Context map[string]any   `json:"@context"`
	Graph   []map[string]any `json:"@graph"`
}

type JSONLDRef struct {
	ID string `json:"@id"`
}

// MakeJSONLDRef returns a JSON-LD IRI reference node: {"@id": id}.
func MakeJSONLDRef(id string) JSONLDRef { return JSONLDRef{ID: id} }

// Pre-allocated sh:datatype slices for every XSD/RAML scalar type.
// Each is a one-element []JSONLDRef wrapping a single @id node, shared
// across all shape nodes of the same type so only one allocation is needed
// per type instead of one allocation per shape instance.
var (
	shDatatypeXSDString       = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#string"}}
	shDatatypeXSDBoolean      = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#boolean"}}
	shDatatypeXSDDateTime     = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#dateTime"}}
	shDatatypeXSDDate         = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#date"}}
	shDatatypeXSDTime         = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#time"}}
	shDatatypeXSDDateTimeOnly = []JSONLDRef{{ID: "raml-shapes:dateTimeOnly"}}
	shDatatypeXSDByte         = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#byte"}}
	shDatatypeXSDShort        = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#short"}}
	shDatatypeXSDInt          = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#int"}}
	shDatatypeXSDLong         = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#long"}}
	shDatatypeXSDInteger      = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#integer"}}
	shDatatypeXSDFloat        = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#float"}}
	shDatatypeXSDDouble       = []JSONLDRef{{ID: "http://www.w3.org/2001/XMLSchema#double"}}
	shDatatypeRAMLNumber      = []JSONLDRef{{ID: "http://a.ml/vocabularies/shapes#number"}}
)

// Pre-allocated @type slices for every JSON-LD node category.
// Each variable is the canonical []string shared across all instances of that
// node type, so no new slice is allocated per node.
var (
	// Document / fragment units
	typeDocumentUnit           = []string{"doc:Document", "doc:Fragment", "doc:Module", "doc:Unit"}
	typeModuleUnit             = []string{"doc:Module", "doc:Unit"}
	typeDataTypeFragment       = []string{"raml-shapes:DataTypeFragment", "doc:Fragment", "doc:Unit"}
	typeNamedExampleFragment   = []string{"apiContract:NamedExampleFragment", "doc:Fragment", "doc:Unit"}
	typeSecuritySchemeFragment = []string{"security:SecuritySchemeFragment", "doc:Fragment", "doc:Unit"}
	typeResourceTypeFragment   = []string{"apiContract:ResourceTypeFragment", "doc:Fragment", "doc:Unit"}
	typeTraitFragment          = []string{"apiContract:TraitFragment", "doc:Fragment", "doc:Unit"}
	typeExternalFragment       = []string{"doc:ExternalFragment", "doc:Fragment", "doc:Unit"}
	typeExternalDomainElement  = []string{"doc:ExternalDomainElement", "doc:DomainElement"}

	// Processing data
	typeAPIContractProcessingData = []string{"doc:APIContractProcessingData"}
	typeBaseUnitProcessingData    = []string{"doc:BaseUnitProcessingData"}

	// WebAPI / API contract
	typeWebAPI                   = []string{"apiContract:WebAPI", "apiContract:API", "doc:RootDomainElement", "doc:DomainElement"}
	typeServer                   = []string{"apiContract:Server", "doc:DomainElement"}
	typeCreativeWork             = []string{"core:CreativeWork", "doc:DomainElement"}
	typeEndPoint                 = []string{"apiContract:EndPoint", "doc:DomainElement"}
	typeOperation                = []string{"apiContract:Operation", "core:Operation", "doc:DomainElement"}
	typeRequest                  = []string{"apiContract:Request", "core:Request", "apiContract:Message", "doc:DomainElement"}
	typeResponse                 = []string{"apiContract:Response", "core:Response", "apiContract:Message", "doc:DomainElement"}
	typePayload                  = []string{"apiContract:Payload", "core:Payload", "doc:DomainElement"}
	typeParameter                = []string{"apiContract:Parameter", "core:Parameter", "doc:DomainElement"}
	typeParametrizedResourceType = []string{"apiContract:ParametrizedResourceType", "doc:ParametrizedDeclaration", "doc:DomainElement"}
	typeParametrizedTrait        = []string{"apiContract:ParametrizedTrait", "doc:ParametrizedDeclaration", "doc:DomainElement"}
	typeExample                  = []string{"apiContract:Example", "doc:DomainElement"}
	typeTrait                    = []string{"apiContract:Trait", "doc:AbstractDeclaration", "doc:DomainElement"}
	typeResourceType             = []string{"apiContract:ResourceType", "doc:AbstractDeclaration", "doc:DomainElement"}

	// Shape / schema nodes
	typeDomainProperty = []string{"doc:DomainProperty", "rdf:Property", "doc:DomainElement"}
	typeXMLSerializer  = []string{"raml-shapes:XMLSerializer", "doc:DomainElement"}
	typePropertyShape  = []string{"sh:PropertyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeAnyShape       = []string{"raml-shapes:AnyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeScalarShape    = []string{"raml-shapes:ScalarShape", "raml-shapes:AnyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeNilShape       = []string{"raml-shapes:NilShape", "raml-shapes:AnyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeFileShape      = []string{"raml-shapes:FileShape", "raml-shapes:AnyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeArrayShape     = []string{"raml-shapes:ArrayShape", "raml-shapes:AnyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeNodeShape      = []string{"sh:NodeShape", "raml-shapes:AnyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeUnionShape     = []string{"raml-shapes:UnionShape", "raml-shapes:AnyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeSchemaShape    = []string{"raml-shapes:SchemaShape", "raml-shapes:AnyShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}
	typeRecursiveShape = []string{"raml-shapes:RecursiveShape", "sh:Shape", "raml-shapes:Shape", "doc:DomainElement"}

	// Data nodes
	typeDataObject = []string{"data:Object", "data:Node", "doc:DomainElement"}
	typeDataArray  = []string{"data:Array", "rdf:Seq", "data:Node", "doc:DomainElement"}
	typeDataScalar = []string{"data:Scalar", "data:Node", "doc:DomainElement"}

	// Security
	typeSecurityScheme             = []string{"security:SecurityScheme", "doc:DomainElement"}
	typeOAuth2Settings             = []string{"security:OAuth2Settings", "security:Settings", "doc:DomainElement"}
	typeOAuth2Flow                 = []string{"security:OAuth2Flow", "doc:DomainElement"}
	typeSecurityScope              = []string{"security:Scope", "doc:DomainElement"}
	typeOAuth1Settings             = []string{"security:OAuth1Settings", "security:Settings", "doc:DomainElement"}
	typeHttpSettings               = []string{"security:HttpSettings", "security:Settings", "doc:DomainElement"}
	typeSecurityRequirement        = []string{"security:SecurityRequirement", "doc:DomainElement"}
	typeParametrizedSecurityScheme = []string{"security:ParametrizedSecurityScheme", "doc:DomainElement"}
)

// jsonLDContext is the @context block emitted in every document.
var jsonLDContext = map[string]any{
	"@base":       "amf://id",
	"sh":          "http://www.w3.org/ns/shacl#",
	"raml-shapes": "http://a.ml/vocabularies/shapes#",
	"data":        "http://a.ml/vocabularies/data#",
	"doc":         "http://a.ml/vocabularies/document#",
	"apiContract": "http://a.ml/vocabularies/apiContract#",
	"core":        "http://a.ml/vocabularies/core#",
	"security":    "http://a.ml/vocabularies/security#",
	"rdf":         "http://www.w3.org/1999/02/22-rdf-syntax-ns#",
	"rdfs":        "http://www.w3.org/2000/01/rdf-schema#",
}
