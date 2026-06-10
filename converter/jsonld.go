package converter

// JSONLDConverter produces an AMF-compatible JSON-LD graph as a plain Go map
// for the parsed RAML API document.  The returned map uses the compact JSON-LD
// format (prefixed property names, @context block) and can be encoded by the
// caller with json.Marshal, json.MarshalIndent, or any other JSON encoder.
//
// Vocabulary references
//
//	apiContract  http://a.ml/vocabularies/apiContract#
//	core         http://a.ml/vocabularies/core#
//	doc          http://a.ml/vocabularies/document#
//	raml-shapes  http://a.ml/vocabularies/shapes#
//	data         http://a.ml/vocabularies/data#
//	sh           http://www.w3.org/ns/shacl#
//	security     http://a.ml/vocabularies/security#
//	xsd          http://www.w3.org/2001/XMLSchema#

import (
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"

	raml "github.com/acronis/go-raml/v3"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// jsonLDSeg URL-encodes a single path segment (e.g. "/users" → "%2Fusers").
func jsonLDSeg(s string) string { return url.PathEscape(s) }

// setIfStr adds key→value only when value is non-empty.
func setIfStr(node map[string]any, key, value string) {
	if value != "" {
		node[key] = value
	}
}

// JSONLDConverter is a stateless converter that serialises a parsed *raml.RAML
// document into an AMF-compatible JSON-LD graph.  Per-call state is held in
// an internal jsonLDSer created by each Convert call.
type JSONLDConverter struct{}

// NewJSONLDConverter returns a new JSONLDConverter.
func NewJSONLDConverter() *JSONLDConverter {
	return &JSONLDConverter{}
}

// Convert serialises the entry point of r into an AMF-compatible JSON-LD graph.
// It dispatches on the concrete type of r.EntryPoint() using a type switch.
func (c *JSONLDConverter) Convert(r *raml.RAML) (*JSONLDGraph, error) {
	transformed := r.IsUnwrapped()
	switch f := r.EntryPoint().(type) {
	case *raml.APIFragment:
		return c.convertAPI(f, r, transformed)
	case *raml.Library:
		return c.convertLibrary(f, transformed)
	case *raml.DataTypeFragment:
		return c.convertDataType(f, transformed)
	case *raml.NamedExample:
		return c.convertNamedExample(f, transformed)
	default:
		return nil, fmt.Errorf("unsupported entry point type %T", r.EntryPoint())
	}
}

func (c *JSONLDConverter) convertAPI(api *raml.APIFragment, r *raml.RAML, transformed bool) (*JSONLDGraph, error) {
	s := newJSONLDSer()
	if !transformed {
		s.preRegisterReferencedFragments(r, api.Location)
	}
	s.preRegisterTypes("#/declarations/types/", api.Types)
	s.preRegisterAnnotationTypes("#/declarations/annotations/", api.AnnotationTypes)

	webAPIID := s.serWebAPI(api)

	var declares []JSONLDRef
	declares = append(declares, s.serDeclaredTypes("#/declarations/types/", api.Types)...)
	declares = append(declares, s.serDeclaredAnnotationTypes("#/declarations/annotations/", api.AnnotationTypes)...)
	declares = append(declares, s.serDeclaredSecuritySchemes("#/declarations/securitySchemes/", api.SecuritySchemes)...)
	declares = append(declares, s.serDeclaredTraits("#/declarations/traits/", api.Traits)...)
	declares = append(declares, s.serDeclaredResourceTypes("#/declarations/resourceTypes/", api.ResourceTypes)...)

	docNode := map[string]any{
		"@id":         "",
		"@type":       typeDocumentUnit,
		"doc:encodes": MakeJSONLDRef(webAPIID),
		"doc:root":    true,
	}
	if len(declares) > 0 {
		docNode["doc:declares"] = declares
	}
	if !transformed {
		if refs := s.serReferences(r, api.Location); len(refs) > 0 {
			docNode["doc:references"] = refs
		}
	}
	s.addProcessingData(docNode, "RAML 1.0", transformed)
	return s.graph, nil
}

func (c *JSONLDConverter) convertLibrary(lib *raml.Library, transformed bool) (*JSONLDGraph, error) {
	s := newJSONLDSer()
	s.preRegisterTypes("#/declarations/types/", lib.Types)
	s.preRegisterAnnotationTypes("#/declarations/annotations/", lib.AnnotationTypes)

	var declares []JSONLDRef
	declares = append(declares, s.serDeclaredTypes("#/declarations/types/", lib.Types)...)
	declares = append(declares, s.serDeclaredAnnotationTypes("#/declarations/annotations/", lib.AnnotationTypes)...)
	declares = append(declares, s.serDeclaredSecuritySchemes("#/declarations/securitySchemes/", lib.SecuritySchemes)...)

	docNode := map[string]any{
		"@id":   "",
		"@type": typeModuleUnit,
	}
	if lib.Usage != nil {
		docNode["doc:usage"] = lib.Usage.Value
	}
	if len(declares) > 0 {
		docNode["doc:declares"] = declares
	}
	s.addAnnotations(docNode, lib.CustomDomainProperties)
	s.addProcessingData(docNode, "RAML 1.0", transformed)
	return s.graph, nil
}

func (c *JSONLDConverter) convertDataType(dt *raml.DataTypeFragment, transformed bool) (*JSONLDGraph, error) {
	s := newJSONLDSer()
	shapeID := ""
	if dt.Shape != nil {
		shapeID = s.serShape(dt.Shape, "#/type")
	}
	docNode := map[string]any{
		"@id":   "",
		"@type": typeDataTypeFragment,
	}
	if shapeID != "" {
		docNode["doc:encodes"] = MakeJSONLDRef(shapeID)
	}
	s.addProcessingData(docNode, "RAML 1.0", transformed)
	return s.graph, nil
}

func (c *JSONLDConverter) convertNamedExample(ne *raml.NamedExample, transformed bool) (*JSONLDGraph, error) {
	s := newJSONLDSer()
	docNode := map[string]any{
		"@id":   "",
		"@type": typeNamedExampleFragment,
	}
	if l := ne.Map.Len(); l > 0 {
		exRefs := make([]JSONLDRef, 0, l)
		for pair := ne.Map.Oldest(); pair != nil; pair = pair.Next() {
			exID := "#/examples/example/" + jsonLDSeg(pair.Key)
			s.push(s.makeExampleNode(exID, pair.Value))
			exRefs = append(exRefs, MakeJSONLDRef(exID))
		}
		docNode["apiContract:examples"] = exRefs
	}
	s.addProcessingData(docNode, "RAML 1.0", transformed)
	return s.graph, nil
}

// ---------------------------------------------------------------------------
// Internal per-call state
// ---------------------------------------------------------------------------

// jsonLDSer holds serialisation state for one Convert call.
type jsonLDSer struct {
	graph      *JSONLDGraph
	shapeIDs   map[int64]string   // BaseShape.ID → assigned @id
	serialised map[int64]struct{} // BaseShape.ID → already emitted
}

func newJSONLDSer() *jsonLDSer {
	return &jsonLDSer{
		graph: &JSONLDGraph{
			Context: jsonLDContext,
			Graph:   make([]map[string]any, 0, 64),
		},
		shapeIDs:   make(map[int64]string),
		serialised: make(map[int64]struct{}),
	}
}

func (s *jsonLDSer) push(node map[string]any) {
	s.graph.Graph = append(s.graph.Graph, node)
}

// preRegisterTypes pre-assigns @id values for a set of named shapes so that
// forward references within the graph resolve correctly before the shapes are
// actually serialised.
func (s *jsonLDSer) preRegisterTypes(prefix string, types *orderedmap.OrderedMap[string, *raml.BaseShape]) {
	for pair := types.Oldest(); pair != nil; pair = pair.Next() {
		s.shapeIDs[pair.Value.ID] = prefix + jsonLDSeg(pair.Key)
	}
}

// serDeclaredTypes serialises each shape and returns IRI reference nodes
// suitable for use as doc:declares values.
func (s *jsonLDSer) serDeclaredTypes(prefix string, types *orderedmap.OrderedMap[string, *raml.BaseShape]) []JSONLDRef {
	l := types.Len()
	if l == 0 {
		return nil
	}
	refs := make([]JSONLDRef, 0, l)
	for pair := types.Oldest(); pair != nil; pair = pair.Next() {
		id := s.serShape(pair.Value, prefix+jsonLDSeg(pair.Key))
		refs = append(refs, MakeJSONLDRef(id))
	}
	return refs
}

// preRegisterAnnotationTypes pre-assigns @id values for annotation type shapes,
// mapping each BaseShape.ID to "<prefix><name>/schema" so that the wrapping
// doc:DomainProperty node can use the bare "<prefix><name>" ID.
func (s *jsonLDSer) preRegisterAnnotationTypes(prefix string, types *orderedmap.OrderedMap[string, *raml.BaseShape]) {
	for pair := types.Oldest(); pair != nil; pair = pair.Next() {
		s.shapeIDs[pair.Value.ID] = prefix + jsonLDSeg(pair.Key) + "/schema"
	}
}

// serDeclaredAnnotationTypes emits each annotation type wrapped in an AMF-compatible
// doc:DomainProperty + rdf:Property node (with raml-shapes:schema pointing to the
// underlying shape) and returns IRI reference nodes for doc:declares.
func (s *jsonLDSer) serDeclaredAnnotationTypes(
	prefix string,
	types *orderedmap.OrderedMap[string, *raml.BaseShape],
) []JSONLDRef {
	l := types.Len()
	if l == 0 {
		return nil
	}
	refs := make([]JSONLDRef, 0, l)
	for pair := types.Oldest(); pair != nil; pair = pair.Next() {
		name := pair.Key
		bs := pair.Value
		domainPropID := prefix + jsonLDSeg(name)
		schemaID := domainPropID + "/schema"
		// Emit the underlying shape with the /schema ID.
		s.serShape(bs, schemaID)
		// Emit the doc:DomainProperty wrapper.
		dpNode := map[string]any{
			"@id":                domainPropID,
			"@type":              typeDomainProperty,
			"core:name":          name,
			"raml-shapes:schema": MakeJSONLDRef(schemaID),
		}
		s.push(dpNode)
		refs = append(refs, MakeJSONLDRef(domainPropID))
	}
	return refs
}

// addProcessingData pushes a doc:APIContractProcessingData node and links it
// from the document root via doc:processingData.  sourceSpec is the AMF
// source specification string (e.g. "RAML 1.0"); transformed mirrors whether
// UnwrapShapes was called (doc:transformed in AMF output).
func (s *jsonLDSer) addProcessingData(docNode map[string]any, sourceSpec string, transformed bool) {
	pdID := "#/processingData"
	s.push(map[string]any{
		"@id":             pdID,
		"@type":           typeAPIContractProcessingData,
		"doc:transformed": transformed,
		"doc:sourceSpec":  sourceSpec,
	})
	docNode["doc:processingData"] = MakeJSONLDRef(pdID)
	s.push(docNode)
}

// ---------------------------------------------------------------------------
// WebAPI
// ---------------------------------------------------------------------------

func (s *jsonLDSer) serWebAPI(api *raml.APIFragment) string {
	id := "#/web-api"
	node := map[string]any{
		"@id":   id,
		"@type": typeWebAPI,
	}

	if api.Title != nil {
		node["core:name"] = api.Title.Value
	}
	if api.Description != nil {
		node["core:description"] = api.Description.Value
	}
	if api.Version != nil {
		node["core:version"] = api.Version.Value
	}

	if l := len(api.Protocols); l > 0 {
		protos := make([]string, l)
		for i, p := range api.Protocols {
			protos[i] = p.Value
		}
		node["apiContract:scheme"] = protos
	}

	if l := len(api.MediaType); l > 0 {
		mts := make([]string, l)
		for i, mt := range api.MediaType {
			mts[i] = mt.Value
		}
		node["apiContract:accepts"] = mts
		node["apiContract:contentType"] = mts
	}

	if api.BaseURI != nil {
		serverID := id + "/server/" + jsonLDSeg(api.BaseURI.Value)
		srvNode := map[string]any{
			"@id":              serverID,
			"@type":            typeServer,
			"core:urlTemplate": api.BaseURI.Value,
		}
		if l := api.BaseURIParameters.Len(); l > 0 {
			vars := make([]JSONLDRef, 0, l)
			for pair := api.BaseURIParameters.Oldest(); pair != nil; pair = pair.Next() {
				pID := serverID + "/variable/parameter/path/" + jsonLDSeg(pair.Key)
				pNode := s.makeParamNode(pID, pair.Key, pair.Value, "path", true)
				s.push(pNode)
				vars = append(vars, MakeJSONLDRef(pID))
			}
			srvNode["apiContract:variable"] = vars
		}
		s.push(srvNode)
		node["apiContract:server"] = []JSONLDRef{MakeJSONLDRef(serverID)}
	}

	if l := len(api.Documentation); l > 0 {
		docRefs := make([]JSONLDRef, 0, l)
		for i, d := range api.Documentation {
			slug := ""
			if d.Title != nil {
				slug = d.Title.Value
			}
			if slug == "" {
				slug = strconv.Itoa(i)
			}
			dID := id + "/documentation/creative-work/" + jsonLDSeg(slug)
			dNode := map[string]any{
				"@id":   dID,
				"@type": typeCreativeWork,
			}
			if d.Title != nil {
				dNode["core:title"] = d.Title.Value
			}
			if d.Content != nil {
				dNode["core:description"] = d.Content.Value
			}
			s.push(dNode)
			docRefs = append(docRefs, MakeJSONLDRef(dID))
		}
		node["core:documentation"] = docRefs
	}

	// Collect all endpoints recursively (AMF flattens the tree).
	if l := api.EndPoints.Len(); l > 0 {
		epRefs := make([]JSONLDRef, 0, l)
		for pair := api.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
			s.collectEndpoints(pair.Value, &epRefs)
		}
		node["apiContract:endpoint"] = epRefs
	}

	if refs := s.serSecuredBy(api.SecuredBy, id); len(refs) > 0 {
		node["security:security"] = refs
	}
	s.addAnnotations(node, api.CustomDomainProperties)
	s.push(node)
	return id
}

// collectEndpoints serialises ep and all its descendants, appending IRI refs
// to refs so the caller can add them to apiContract:endpoint.
func (s *jsonLDSer) collectEndpoints(ep *raml.EndPoint, refs *[]JSONLDRef) {
	id := s.serEndpoint(ep)
	*refs = append(*refs, MakeJSONLDRef(id))
	for pair := ep.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		s.collectEndpoints(pair.Value, refs)
	}
}

// ---------------------------------------------------------------------------
// EndPoint
// ---------------------------------------------------------------------------

func (s *jsonLDSer) serEndpoint(ep *raml.EndPoint) string {
	id := "#/web-api/endpoint/" + jsonLDSeg(ep.FullURI)
	node := map[string]any{
		"@id":              id,
		"@type":            typeEndPoint,
		"apiContract:path": ep.FullURI,
	}
	if ep.DisplayName != nil {
		node["core:name"] = ep.DisplayName.Value
	}
	if ep.Description != nil {
		node["core:description"] = ep.Description.Value
	}

	if l := ep.URIParameters.Len(); l > 0 {
		params := make([]JSONLDRef, 0, l)
		for pair := ep.URIParameters.Oldest(); pair != nil; pair = pair.Next() {
			pID := id + "/parameter/parameter/path/" + jsonLDSeg(pair.Key)
			pNode := s.makeParamNode(pID, pair.Key, pair.Value, "path", true)
			s.push(pNode)
			params = append(params, MakeJSONLDRef(pID))
		}
		node["apiContract:parameter"] = params
	}

	if l := ep.Operations.Len(); l > 0 {
		ops := make([]JSONLDRef, 0, l)
		for pair := ep.Operations.Oldest(); pair != nil; pair = pair.Next() {
			opID := s.serOperation(pair.Value, id)
			ops = append(ops, MakeJSONLDRef(opID))
		}
		node["apiContract:supportedOperation"] = ops
	}

	if refs := s.serSecuredBy(ep.SecuredBy, id); len(refs) > 0 {
		node["security:security"] = refs
	}

	// doc:extends — emit a ParametrizedResourceType node for the applied resource type.
	if ep.ResourceType != nil {
		ptID := id + "/extends/resourceType"
		ptNode := map[string]any{
			"@id":        ptID,
			"@type":      typeParametrizedResourceType,
			"core:name":  ep.ResourceType.Name,
			"doc:target": MakeJSONLDRef("#/declarations/resourceTypes/" + jsonLDSeg(ep.ResourceType.Name)),
		}
		s.push(ptNode)
		node["doc:extends"] = []JSONLDRef{MakeJSONLDRef(ptID)}
	}

	s.addAnnotations(node, ep.CustomDomainProperties)
	s.push(node)
	return id
}

// ---------------------------------------------------------------------------
// Operation
// ---------------------------------------------------------------------------

func (s *jsonLDSer) serOperation(op *raml.Operation, epID string) string {
	id := epID + "/supportedOperation/" + op.Method
	node := map[string]any{
		"@id":                id,
		"@type":              typeOperation,
		"apiContract:method": op.Method,
	}
	if op.DisplayName != nil {
		node["core:name"] = op.DisplayName.Value
	}
	if op.Description != nil {
		node["core:description"] = op.Description.Value
	}
	if l := len(op.Protocols); l > 0 {
		protos := make([]string, l)
		for i, p := range op.Protocols {
			protos[i] = p.Value
		}
		node["apiContract:scheme"] = protos
	}

	// Build Request node when there are query params, headers, bodies, or a queryString.
	qpNum := op.QueryParameters.Len()
	hdrNum := op.Headers.Len()
	hasBodies := op.Request != nil
	hasQS := op.QueryString != nil
	if qpNum > 0 || hdrNum > 0 || hasBodies || hasQS {
		reqID := id + "/expects/request"
		reqNode := map[string]any{
			"@id":   reqID,
			"@type": typeRequest,
		}

		if qpNum > 0 {
			params := make([]JSONLDRef, 0, qpNum)
			for pair := op.QueryParameters.Oldest(); pair != nil; pair = pair.Next() {
				pID := reqID + "/parameter/parameter/query/" + jsonLDSeg(pair.Key)
				pNode := s.makeParamNode(pID, pair.Key, pair.Value.Base, "query", pair.Value.Required)
				s.push(pNode)
				params = append(params, MakeJSONLDRef(pID))
			}
			reqNode["apiContract:parameter"] = params
		}
		if hdrNum > 0 {
			headers := make([]JSONLDRef, 0, hdrNum)
			for pair := op.Headers.Oldest(); pair != nil; pair = pair.Next() {
				hID := reqID + "/parameter/parameter/header/" + jsonLDSeg(pair.Key)
				hNode := s.makeParamNode(hID, pair.Key, pair.Value.Base, "header", pair.Value.Required)
				s.push(hNode)
				headers = append(headers, MakeJSONLDRef(hID))
			}
			reqNode["apiContract:header"] = headers
		}
		if hasBodies {
			if l := op.Request.Bodies.Len(); l > 0 {
				payloads := make([]JSONLDRef, 0, l)
				for pair := op.Request.Bodies.Oldest(); pair != nil; pair = pair.Next() {
					pID := s.serPayload(pair.Value, reqID)
					payloads = append(payloads, MakeJSONLDRef(pID))
				}
				reqNode["apiContract:payload"] = payloads
			}
		}
		if hasQS {
			qsID := s.serShape(op.QueryString, reqID+"/shape/queryString")
			reqNode["apiContract:queryString"] = MakeJSONLDRef(qsID)
		}

		s.push(reqNode)
		node["apiContract:expects"] = []JSONLDRef{MakeJSONLDRef(reqID)}
	}

	if l := op.Responses.Len(); l > 0 {
		resps := make([]JSONLDRef, 0, l)
		for pair := op.Responses.Oldest(); pair != nil; pair = pair.Next() {
			rID := s.serResponse(pair.Value, id)
			resps = append(resps, MakeJSONLDRef(rID))
		}
		node["apiContract:returns"] = resps
	}

	if refs := s.serSecuredBy(op.SecuredBy, id); len(refs) > 0 {
		node["security:security"] = refs
	}

	// doc:extends — emit a ParametrizedTrait node for each applied trait.
	allTraits := append(op.Traits, op.RTTraits...)
	if len(allTraits) > 0 {
		extendsRefs := make([]JSONLDRef, 0, len(allTraits))
		for i, t := range allTraits {
			ptID := id + "/extends/trait/" + strconv.Itoa(i)
			ptNode := map[string]any{
				"@id":        ptID,
				"@type":      typeParametrizedTrait,
				"core:name":  t.Name,
				"doc:target": MakeJSONLDRef("#/declarations/traits/" + jsonLDSeg(t.Name)),
			}
			s.push(ptNode)
			extendsRefs = append(extendsRefs, MakeJSONLDRef(ptID))
		}
		node["doc:extends"] = extendsRefs
	}

	s.addAnnotations(node, op.CustomDomainProperties)
	s.push(node)
	return id
}

func (s *jsonLDSer) serResponse(resp *raml.Response, opID string) string {
	sc := strconv.Itoa(resp.StatusCode)
	id := opID + "/returns/resp/" + sc
	name := sc
	if resp.DisplayName != "" {
		name = resp.DisplayName
	}
	node := map[string]any{
		"@id":                    id,
		"@type":                  typeResponse,
		"apiContract:statusCode": sc,
		"core:name":              name,
	}
	setIfStr(node, "core:description", resp.Description)

	if l := resp.Headers.Len(); l > 0 {
		headers := make([]JSONLDRef, 0, l)
		for pair := resp.Headers.Oldest(); pair != nil; pair = pair.Next() {
			hID := id + "/parameter/parameter/header/" + jsonLDSeg(pair.Key)
			hNode := s.makeParamNode(hID, pair.Key, pair.Value.Base, "header", pair.Value.Required)
			s.push(hNode)
			headers = append(headers, MakeJSONLDRef(hID))
		}
		node["apiContract:header"] = headers
	}

	if l := resp.Bodies.Len(); l > 0 {
		payloads := make([]JSONLDRef, 0, l)
		for pair := resp.Bodies.Oldest(); pair != nil; pair = pair.Next() {
			pID := s.serPayload(pair.Value, id)
			payloads = append(payloads, MakeJSONLDRef(pID))
		}
		node["apiContract:payload"] = payloads
	}

	s.addAnnotations(node, resp.CustomDomainProperties)
	s.push(node)
	return id
}

// ---------------------------------------------------------------------------
// Payload / Body
// ---------------------------------------------------------------------------

func (s *jsonLDSer) serPayload(body *raml.Body, parentID string) string {
	mt := body.MediaType
	if mt == "" {
		mt = "default"
	}
	id := parentID + "/payload/" + jsonLDSeg(mt)
	node := map[string]any{
		"@id":   id,
		"@type": typePayload,
	}
	if body.MediaType != "" {
		node["core:mediaType"] = body.MediaType
	}
	if body.Shape != nil {
		schemaID := s.serShape(body.Shape, id+inlineShapeSuffix(body.Shape, "schema"))
		node["raml-shapes:schema"] = MakeJSONLDRef(schemaID)
	}
	s.push(node)
	return id
}

// ---------------------------------------------------------------------------
// Parameter
// ---------------------------------------------------------------------------

func (s *jsonLDSer) makeParamNode(id, name string, bs *raml.BaseShape, binding string, required bool) map[string]any {
	node := map[string]any{
		"@id":                   id,
		"@type":                 typeParameter,
		"core:name":             name,
		"apiContract:paramName": name,
		"apiContract:binding":   binding,
		"apiContract:required":  required,
	}
	if bs != nil {
		if bs.Description != nil {
			node["core:description"] = bs.Description.Value
		}
		schemaID := s.serShape(bs, id+inlineShapeSuffix(bs, "schema"))
		node["raml-shapes:schema"] = MakeJSONLDRef(schemaID)
	}
	return node
}

// ---------------------------------------------------------------------------
// Shapes
// ---------------------------------------------------------------------------

// shapeTypes returns the @type array for a BaseShape's concrete type.
func shapeTypes(bs *raml.BaseShape) []string {
	switch bs.Shape.(type) {
	case *raml.StringShape, *raml.IntegerShape, *raml.NumberShape, *raml.BooleanShape,
		*raml.DateTimeShape, *raml.DateTimeOnlyShape, *raml.DateOnlyShape, *raml.TimeOnlyShape:
		return typeScalarShape
	case *raml.NilShape:
		return typeNilShape
	case *raml.AnyShape:
		return typeAnyShape
	case *raml.FileShape:
		return typeFileShape
	case *raml.ArrayShape:
		return typeArrayShape
	case *raml.ObjectShape:
		return typeNodeShape
	case *raml.UnionShape:
		return typeUnionShape
	case *raml.JSONShape:
		if view, err := bs.Shape.(*raml.JSONShape).AsShape(); err == nil && view != nil {
			return shapeTypes(view.Base())
		}
		return typeSchemaShape
	case *raml.RecursiveShape:
		return typeRecursiveShape
	default:
		return typeAnyShape
	}
}

// isScalarShape reports whether the BaseShape wraps a scalar shape.
func isScalarShape(bs *raml.BaseShape) bool {
	if bs == nil {
		return false
	}
	switch bs.Shape.(type) {
	case *raml.StringShape, *raml.IntegerShape, *raml.NumberShape, *raml.BooleanShape,
		*raml.DateTimeShape, *raml.DateTimeOnlyShape, *raml.DateOnlyShape, *raml.TimeOnlyShape, *raml.NilShape:
		return true
	}
	return false
}

// inlineShapeSuffix returns the path segment to append to a parent IRI when
// emitting an inline shape with the given field name.
func inlineShapeSuffix(bs *raml.BaseShape, field string) string {
	if isScalarShape(bs) {
		return "/scalar/" + field
	}
	return "/" + field
}

// shapeKindSeg returns the AMF "kind" path segment for an inline shape.
func shapeKindSeg(bs *raml.BaseShape) string {
	if bs == nil {
		return "shape"
	}
	switch bs.Shape.(type) {
	case *raml.StringShape, *raml.IntegerShape, *raml.NumberShape, *raml.BooleanShape,
		*raml.DateTimeShape, *raml.DateTimeOnlyShape, *raml.DateOnlyShape, *raml.TimeOnlyShape:
		return "scalar"
	case *raml.NilShape:
		return "nil"
	case *raml.FileShape:
		return "file"
	case *raml.ArrayShape:
		return "array"
	case *raml.ObjectShape:
		return "node"
	case *raml.UnionShape:
		return "union"
	case *raml.JSONShape:
		if view, err := bs.Shape.(*raml.JSONShape).AsShape(); err == nil && view != nil {
			return shapeKindSeg(view.Base())
		}
		return "schema"
	case *raml.RecursiveShape:
		return "recursive"
	default:
		return "shape"
	}
}

// serShape serialises bs and returns its @id.
func (s *jsonLDSer) serShape(bs *raml.BaseShape, contextID string) string {
	if bs == nil {
		return ""
	}
	// Assign ID on first encounter (may have been pre-registered).
	id, known := s.shapeIDs[bs.ID]
	if !known {
		if !bs.IsUnwrapped() && bs.Alias != nil {
			for cur := bs.Alias; cur != nil; cur = cur.Alias {
				if canonicalID, found := s.shapeIDs[cur.ID]; found {
					s.shapeIDs[bs.ID] = canonicalID
					return canonicalID
				}
			}
		}
		id = contextID
		s.shapeIDs[bs.ID] = id
	}
	// Already emitted — return ID only.
	if _, ok := s.serialised[bs.ID]; ok {
		return id
	}
	s.serialised[bs.ID] = struct{}{}

	// Recursive shapes are just a pointer back to the head.
	if rs, ok := bs.Shape.(*raml.RecursiveShape); ok {
		headID := ""
		if rs.Head != nil {
			headID = s.shapeIDs[rs.Head.ID]
			if headID == "" {
				headID = "#/declarations/types/" + jsonLDSeg(rs.Head.Name)
			}
		}
		node := map[string]any{
			"@id":           id,
			"@type":         shapeTypes(bs),
			"doc:recursive": true,
		}
		if headID != "" {
			node["raml-shapes:fixPoint"] = MakeJSONLDRef(headID)
		}
		s.push(node)
		return id
	}

	node := map[string]any{
		"@id":   id,
		"@type": shapeTypes(bs),
	}

	shaclName := bs.Name
	if shaclName == "" {
		if i := strings.LastIndexByte(id, '/'); i >= 0 {
			shaclName = id[i+1:]
		}
	}
	if shaclName != "" {
		node["sh:name"] = shaclName
	}
	if bs.DisplayName != nil {
		node["core:name"] = bs.DisplayName.Value
	}
	if bs.Description != nil {
		node["core:description"] = bs.Description.Value
	}
	if bs.Default != nil {
		defaultRaw := bs.Default.Value.Raw
		dvID := id + "/" + dataNodeSuffix(defaultRaw, 1)
		dvNode := s.serDataNode(dvID, lastSeg(dvID), defaultRaw)
		s.push(dvNode)
		node["sh:defaultValue"] = MakeJSONLDRef(dvID)
		node["sh:defaultValueStr"] = fmt.Sprintf("%v", defaultRaw)
	}
	if len(bs.Enum) > 0 {
		listID := id + "/list"
		listNode := map[string]any{
			"@id":   listID,
			"@type": "rdfs:Seq",
		}
		for i, e := range bs.Enum {
			idxStr := strconv.Itoa(i + 1)
			memberID := id + "/in/scalar_" + idxStr
			memberName := "scalar_" + idxStr
			memberNode := s.serDataNode(memberID, memberName, e.Value.Raw)
			s.push(memberNode)
			listNode["rdfs:_"+idxStr] = MakeJSONLDRef(memberID)
		}
		s.push(listNode)
		node["sh:in"] = MakeJSONLDRef(listID)
	}
	if !bs.IsUnwrapped() && len(bs.Inherits) > 0 {
		refs := make([]JSONLDRef, 0, len(bs.Inherits))
		for _, parent := range bs.Inherits {
			parentID := "#/declarations/types/" + jsonLDSeg(parent.Name)
			pID := s.serShape(parent, parentID)
			refs = append(refs, MakeJSONLDRef(pID))
		}
		node["raml-shapes:inherits"] = refs
	} else if !bs.IsUnwrapped() && bs.Alias != nil {
		aliasTargetID := s.shapeIDs[bs.Alias.ID]
		if aliasTargetID == "" {
			aliasTargetID = "#/declarations/types/" + jsonLDSeg(bs.Alias.Name)
		}
		node["doc:link-target"] = []JSONLDRef{MakeJSONLDRef(aliasTargetID)}
		node["doc:link-label"] = bs.Alias.Name
	}

	s.serShapeConstraints(bs, node, id)

	if bs.XML != nil {
		xmlID := id + "/xml"
		xmlNode := map[string]any{
			"@id":   xmlID,
			"@type": typeXMLSerializer,
		}
		if bs.XML.Attribute != nil {
			xmlNode["raml-shapes:xmlAttribute"] = bs.XML.Attribute.Value
		}
		if bs.XML.Wrapped != nil {
			xmlNode["raml-shapes:xmlWrapped"] = bs.XML.Wrapped.Value
		}
		if bs.XML.Name != nil {
			xmlNode["raml-shapes:xmlName"] = bs.XML.Name.Value
		}
		if bs.XML.Namespace != nil {
			xmlNode["raml-shapes:xmlNamespace"] = bs.XML.Namespace.Value
		}
		if bs.XML.Prefix != nil {
			xmlNode["raml-shapes:xmlPrefix"] = bs.XML.Prefix.Value
		}
		s.push(xmlNode)
		node["raml-shapes:xmlSerialization"] = MakeJSONLDRef(xmlID)
	}

	if bs.Example != nil {
		exID := id + "/examples/example/value"
		s.push(s.makeExampleNode(exID, bs.Example))
		node["apiContract:examples"] = []JSONLDRef{MakeJSONLDRef(exID)}
	} else if bs.Examples != nil {
		if l := bs.Examples.Map.Len(); l > 0 {
			exRefs := make([]JSONLDRef, 0, l)
			for pair := bs.Examples.Map.Oldest(); pair != nil; pair = pair.Next() {
				exID := id + "/examples/example/" + jsonLDSeg(pair.Key)
				s.push(s.makeExampleNode(exID, pair.Value))
				exRefs = append(exRefs, MakeJSONLDRef(exID))
			}
			node["apiContract:examples"] = exRefs
		}
	}

	s.addAnnotations(node, bs.CustomDomainProperties)
	s.push(node)
	return id
}

// serShapeConstraints fills type-specific RDF triples into node.
func (s *jsonLDSer) serShapeConstraints(bs *raml.BaseShape, node map[string]any, id string) {
	switch shape := bs.Shape.(type) {
	case *raml.StringShape:
		node["sh:datatype"] = shDatatypeXSDString
		if shape.Pattern != nil {
			node["sh:pattern"] = shape.Pattern.Value.String()
		}
		if shape.MinLength != nil {
			node["sh:minLength"] = shape.MinLength.Value
		}
		if shape.MaxLength != nil {
			node["sh:maxLength"] = shape.MaxLength.Value
		}

	case *raml.IntegerShape:
		node["sh:datatype"] = integerShDatatype(shape.Format)
		if shape.Minimum != nil {
			node["sh:minInclusive"] = shape.Minimum.Value.Int64()
		}
		if shape.Maximum != nil {
			node["sh:maxInclusive"] = shape.Maximum.Value.Int64()
		}
		if shape.MultipleOf != nil {
			node["raml-shapes:multipleOf"] = shape.MultipleOf.Value
		}
		if shape.Format != nil {
			node["raml-shapes:format"] = shape.Format.Value
		}

	case *raml.NumberShape:
		node["sh:datatype"] = numberShDatatype(shape.Format)
		if shape.Minimum != nil {
			f, _ := shape.Minimum.Value.Float64()
			node["sh:minInclusive"] = f
		}
		if shape.Maximum != nil {
			f, _ := shape.Maximum.Value.Float64()
			node["sh:maxInclusive"] = f
		}
		if shape.MultipleOf != nil {
			f, _ := shape.MultipleOf.Value.Float64()
			node["raml-shapes:multipleOf"] = f
		}
		if shape.Format != nil {
			node["raml-shapes:format"] = shape.Format.Value
		}

	case *raml.BooleanShape:
		node["sh:datatype"] = shDatatypeXSDBoolean

	case *raml.DateTimeShape:
		node["sh:datatype"] = shDatatypeXSDDateTime
		if shape.Format != nil {
			node["raml-shapes:format"] = shape.Format.Value
		}

	case *raml.DateTimeOnlyShape:
		node["sh:datatype"] = shDatatypeXSDDateTimeOnly

	case *raml.DateOnlyShape:
		node["sh:datatype"] = shDatatypeXSDDate

	case *raml.TimeOnlyShape:
		node["sh:datatype"] = shDatatypeXSDTime

	case *raml.NilShape:
		// NilShapeModel has no datatype field; identity is carried by @type alone.

	case *raml.FileShape:
		if len(shape.FileTypes) > 0 {
			fts := make([]string, len(shape.FileTypes))
			for i, ft := range shape.FileTypes {
				fts[i] = ft.Value
			}
			node["raml-shapes:fileType"] = fts
		}
		if shape.MinLength != nil {
			node["sh:minLength"] = shape.MinLength.Value
		}
		if shape.MaxLength != nil {
			node["sh:maxLength"] = shape.MaxLength.Value
		}

	case *raml.ArrayShape:
		if shape.Items != nil {
			itemID := s.serShape(shape.Items, id+"/items")
			node["raml-shapes:items"] = MakeJSONLDRef(itemID)
		}
		if shape.MinItems != nil {
			node["sh:minCount"] = shape.MinItems.Value
		}
		if shape.MaxItems != nil {
			node["sh:maxCount"] = shape.MaxItems.Value
		}
		if shape.UniqueItems != nil {
			node["raml-shapes:uniqueItems"] = shape.UniqueItems.Value
		}

	case *raml.ObjectShape:
		totalProps := shape.Properties.Len() + shape.PatternProperties.Len()
		if totalProps > 0 {
			props := make([]JSONLDRef, 0, totalProps)
			for pair := shape.Properties.Oldest(); pair != nil; pair = pair.Next() {
				propID := id + "/property/" + jsonLDSeg(pair.Key)
				propNode := s.makePropertyNode(propID, &pair.Value)
				s.push(propNode)
				props = append(props, MakeJSONLDRef(propID))
			}
			i := 0
			for pair := shape.PatternProperties.Oldest(); pair != nil; pair = pair.Next() {
				ppID := id + "/property/pattern/" + strconv.Itoa(i)
				ppNode := s.makePatternPropertyNode(ppID, &pair.Value)
				s.push(ppNode)
				props = append(props, MakeJSONLDRef(ppID))
				i++
			}
			node["sh:property"] = props
		}
		closed := shape.AdditionalProperties != nil && !shape.AdditionalProperties.Value
		node["sh:closed"] = closed
		if shape.MinProperties != nil {
			node["raml-shapes:minProperties"] = shape.MinProperties.Value
		}
		if shape.MaxProperties != nil {
			node["raml-shapes:maxProperties"] = shape.MaxProperties.Value
		}
		if shape.Discriminator != nil {
			node["raml-shapes:discriminator"] = shape.Discriminator.Value
		}
		if shape.DiscriminatorValue != nil {
			node["raml-shapes:discriminatorValue"] = shape.DiscriminatorValue.Value.Raw
		}

	case *raml.UnionShape:
		if len(shape.AnyOf) > 0 {
			anyOf := make([]JSONLDRef, 0, len(shape.AnyOf))
			counts := map[string]int{}
			for _, member := range shape.AnyOf {
				kind := shapeKindSeg(member)
				name := member.Name
				if name == "" {
					name = "default-" + kind
				}
				key := kind + "/" + name
				counts[key]++
				if counts[key] > 1 {
					name = name + "_" + strconv.Itoa(counts[key]-1)
				}
				memberID := s.serShape(member, id+"/anyOf/"+kind+"/"+name)
				anyOf = append(anyOf, MakeJSONLDRef(memberID))
			}
			node["raml-shapes:anyOf"] = anyOf
		}

	case *raml.JSONShape:
		if view, err := shape.AsShape(); err == nil && view != nil {
			s.serShapeConstraints(view.Base(), node, id)
		} else {
			node["doc:raw"] = shape.Raw
			node["core:mediaType"] = "application/schema+json"
		}
	}
}

// makePropertyNode builds a sh:PropertyShape node for an object property.
func (s *jsonLDSer) makePropertyNode(id string, prop *raml.Property) map[string]any {
	node := map[string]any{
		"@id":     id,
		"@type":   typePropertyShape,
		"sh:name": prop.Name,
		"sh:path": []JSONLDRef{MakeJSONLDRef("data:" + prop.Name)},
	}
	if prop.Required {
		node["sh:minCount"] = 1
	} else {
		node["sh:minCount"] = 0
	}
	if prop.Base != nil {
		rangeID := s.serShape(prop.Base, id+inlineShapeSuffix(prop.Base, prop.Name))
		node["raml-shapes:range"] = MakeJSONLDRef(rangeID)
	}
	return node
}

// makePatternPropertyNode builds a sh:PropertyShape for a pattern property.
func (s *jsonLDSer) makePatternPropertyNode(id string, pp *raml.PatternProperty) map[string]any {
	pat := pp.Pattern.String()
	node := map[string]any{
		"@id":                     id,
		"@type":                   typePropertyShape,
		"sh:name":                 pat,
		"raml-shapes:patternName": pat,
	}
	if pp.Base != nil {
		rangeID := s.serShape(pp.Base, id+inlineShapeSuffix(pp.Base, pat))
		node["raml-shapes:range"] = MakeJSONLDRef(rangeID)
	}
	return node
}

// ---------------------------------------------------------------------------
// Example
// ---------------------------------------------------------------------------

func (s *jsonLDSer) makeExampleNode(id string, ex *raml.Example) map[string]any {
	node := map[string]any{
		"@id":   id,
		"@type": typeExample,
	}
	name := ex.Name
	if name == "" {
		if i := strings.LastIndexByte(id, '/'); i >= 0 {
			name = id[i+1:]
		} else {
			name = "value"
		}
	}
	node["core:name"] = name
	if ex.DisplayName != nil {
		node["core:displayName"] = ex.DisplayName.Value
	}
	if ex.Description != nil {
		node["core:description"] = ex.Description.Value
	}
	if ex.Data != nil {
		exDataRaw := ex.Data.Value.Raw
		dvID := id + "/" + dataNodeSuffix(exDataRaw, 1)
		dvNode := s.serDataNode(dvID, lastSeg(dvID), exDataRaw)
		s.push(dvNode)
		node["doc:structuredValue"] = MakeJSONLDRef(dvID)
		switch exDataRaw.(type) {
		case map[string]any, []any:
			// complex values — skip raw
		default:
			node["doc:raw"] = fmt.Sprintf("%v", exDataRaw)
		}
	}
	if ex.Strict != nil {
		node["doc:strict"] = ex.Strict.Value
	} else {
		node["doc:strict"] = true
	}
	if ex.Data != nil && ex.Data.Include != nil && ex.Data.Include.AbsURI != "" {
		node["doc:location"] = ex.Data.Include.AbsURI
	}
	s.addAnnotations(node, ex.CustomDomainProperties)
	return node
}

// lastSeg returns the substring after the last '/' in s (or s if none).
func lastSeg(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// dataNodeSuffix returns the AMF-style suffix for a top-level data node.
func dataNodeSuffix(v any, i int) string {
	switch v.(type) {
	case map[string]any:
		return "object_" + strconv.Itoa(i)
	case []any:
		return "array_" + strconv.Itoa(i)
	default:
		return "scalar_" + strconv.Itoa(i)
	}
}

// ---------------------------------------------------------------------------
// Custom Domain Extensions (annotations)
// ---------------------------------------------------------------------------

func (s *jsonLDSer) addAnnotations(node map[string]any,
	ext *orderedmap.OrderedMap[string, *raml.DomainExtension]) {
	l := ext.Len()
	if l == 0 {
		return
	}
	parentID, _ := node["@id"].(string)
	prefix := parentID
	for len(prefix) > 1 && prefix[len(prefix)-1] == '/' {
		prefix = prefix[:len(prefix)-1]
	}

	customDomainProps := make([]JSONLDRef, 0, l)
	for pair := ext.Oldest(); pair != nil; pair = pair.Next() {
		annotName := pair.Key
		def := pair.Value
		var value any
		if def.Extension != nil {
			value = def.Extension.Value.Raw
		}

		defID := "#/declarations/annotations/" + jsonLDSeg(annotName)
		if def.DefinedBy != nil {
			if reg := s.shapeIDs[def.DefinedBy.ID]; reg != "" {
				defID = strings.TrimSuffix(reg, "/schema")
			} else {
				defID = "#/declarations/annotations/" + jsonLDSeg(def.DefinedBy.Name)
			}
		}

		dataNodeID := prefix + "/customDomainProperties/" + jsonLDSeg(annotName) + "/" + dataNodeSuffix(value, 1)
		dataNode := s.serDataNode(dataNodeID, annotName, value)
		dataNode["core:extensionName"] = annotName
		s.push(dataNode)

		node["amf://id"+defID] = MakeJSONLDRef(dataNodeID)

		customDomainProps = append(customDomainProps, MakeJSONLDRef(defID))
	}

	if len(customDomainProps) > 0 {
		node["doc:customDomainProperties"] = customDomainProps
	}
}

func (s *jsonLDSer) serDataNode(id, name string, value any) map[string]any {
	node := map[string]any{
		"@id":       id,
		"core:name": name,
	}
	switch v := value.(type) {
	case map[string]any:
		node["@type"] = typeDataObject
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			childID := id + "/" + jsonLDSeg(k)
			childNode := s.serDataNode(childID, k, v[k])
			s.push(childNode)
			node["data:"+k] = MakeJSONLDRef(childID)
		}
	case []any:
		node["@type"] = typeDataArray
		var members []JSONLDRef
		for i, item := range v {
			memberName := dataNodeSuffix(item, i+1)
			memberID := id + "/member/" + memberName
			memberNode := s.serDataNode(memberID, memberName, item)
			s.push(memberNode)
			members = append(members, MakeJSONLDRef(memberID))
		}
		if len(members) > 0 {
			node["rdfs:member"] = members
		}
	default:
		node["@type"] = typeDataScalar
		if v != nil {
			node["data:value"] = v
			if dt := scalarShDatatype(v); dt != nil {
				node["sh:datatype"] = dt
			}
		}
	}
	return node
}

func scalarShDatatype(v any) []JSONLDRef {
	switch v.(type) {
	case string:
		return shDatatypeXSDString
	case bool:
		return shDatatypeXSDBoolean
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return shDatatypeXSDInteger
	case float32, float64:
		return shDatatypeXSDDouble
	}
	return nil
}

// ---------------------------------------------------------------------------
// XSD datatype helpers
// ---------------------------------------------------------------------------

func integerShDatatype(format *raml.ScalarFacet[string]) []JSONLDRef {
	if format == nil {
		return shDatatypeXSDInteger
	}
	switch format.Value {
	case "int8":
		return shDatatypeXSDByte
	case "int16":
		return shDatatypeXSDShort
	case "int32", "int":
		return shDatatypeXSDInt
	case "int64", "long":
		return shDatatypeXSDLong
	default:
		return shDatatypeXSDInteger
	}
}

func numberShDatatype(format *raml.ScalarFacet[string]) []JSONLDRef {
	if format == nil {
		return shDatatypeRAMLNumber
	}
	switch format.Value {
	case "float":
		return shDatatypeXSDFloat
	case "double":
		return shDatatypeXSDDouble
	default:
		return shDatatypeRAMLNumber
	}
}

// ---------------------------------------------------------------------------
// Security schemes
// ---------------------------------------------------------------------------

func (s *jsonLDSer) serDeclaredSecuritySchemes(
	prefix string,
	schemes *orderedmap.OrderedMap[string, *raml.SecuritySchemeDefinition],
) []JSONLDRef {
	l := schemes.Len()
	if l == 0 {
		return nil
	}
	refs := make([]JSONLDRef, 0, l)
	for pair := schemes.Oldest(); pair != nil; pair = pair.Next() {
		id := s.serSecuritySchemeDefinition(pair.Key, pair.Value, prefix+jsonLDSeg(pair.Key))
		refs = append(refs, MakeJSONLDRef(id))
	}
	return refs
}

func (s *jsonLDSer) serDeclaredTraits(
	prefix string,
	traits *orderedmap.OrderedMap[string, *raml.TraitDefinition],
) []JSONLDRef {
	l := traits.Len()
	if l == 0 {
		return nil
	}
	refs := make([]JSONLDRef, 0, l)
	for pair := traits.Oldest(); pair != nil; pair = pair.Next() {
		id := prefix + jsonLDSeg(pair.Key)
		node := map[string]any{
			"@id":       id,
			"@type":     typeTrait,
			"core:name": pair.Key,
		}
		s.push(node)
		refs = append(refs, MakeJSONLDRef(id))
	}
	return refs
}

func (s *jsonLDSer) serDeclaredResourceTypes(
	prefix string,
	rts *orderedmap.OrderedMap[string, *raml.ResourceTypeDefinition],
) []JSONLDRef {
	l := rts.Len()
	if l == 0 {
		return nil
	}
	refs := make([]JSONLDRef, 0, l)
	for pair := rts.Oldest(); pair != nil; pair = pair.Next() {
		id := prefix + jsonLDSeg(pair.Key)
		node := map[string]any{
			"@id":       id,
			"@type":     typeResourceType,
			"core:name": pair.Key,
		}
		s.push(node)
		refs = append(refs, MakeJSONLDRef(id))
	}
	return refs
}

func (s *jsonLDSer) serSecuritySchemeDefinition(name string, def *raml.SecuritySchemeDefinition, id string) string {
	var schemeType string
	if def.Type != nil {
		schemeType = string(def.Type.Value)
	}
	node := map[string]any{
		"@id":           id,
		"@type":         typeSecurityScheme,
		"core:name":     name,
		"security:type": schemeType,
	}
	if def.DisplayName != nil {
		node["core:displayName"] = def.DisplayName.Value
	}
	if def.Description != nil {
		node["core:description"] = def.Description.Value
	}

	if def.Settings != nil {
		if settingsID := s.serSecuritySchemeSettings(def.Settings, id+"/settings/default"); settingsID != "" {
			node["security:settings"] = MakeJSONLDRef(settingsID)
		}
	}

	if def.DescribedBy != nil {
		db := def.DescribedBy
		if l := db.Headers.Len(); l > 0 {
			headers := make([]JSONLDRef, 0, l)
			for pair := db.Headers.Oldest(); pair != nil; pair = pair.Next() {
				pID := id + "/parameter/parameter/header/" + jsonLDSeg(pair.Key)
				pNode := s.makeParamNode(pID, pair.Key, pair.Value.Base, "header", pair.Value.Required)
				s.push(pNode)
				headers = append(headers, MakeJSONLDRef(pID))
			}
			node["apiContract:header"] = headers
		}
		if l := db.QueryParameters.Len(); l > 0 {
			params := make([]JSONLDRef, 0, l)
			for pair := db.QueryParameters.Oldest(); pair != nil; pair = pair.Next() {
				pID := id + "/parameter/parameter/query/" + jsonLDSeg(pair.Key)
				pNode := s.makeParamNode(pID, pair.Key, pair.Value.Base, "query", pair.Value.Required)
				s.push(pNode)
				params = append(params, MakeJSONLDRef(pID))
			}
			node["apiContract:parameter"] = params
		}
		if l := db.Responses.Len(); l > 0 {
			resps := make([]JSONLDRef, 0, l)
			for pair := db.Responses.Oldest(); pair != nil; pair = pair.Next() {
				rID := s.serResponse(pair.Value, id)
				resps = append(resps, MakeJSONLDRef(rID))
			}
			node["apiContract:response"] = resps
		}
	}

	s.addAnnotations(node, def.CustomDomainProperties)
	s.push(node)
	return id
}

func (s *jsonLDSer) serSecuritySchemeSettings(settings raml.SecuritySchemeSettings, id string) string {
	switch st := settings.(type) {
	case *raml.OAuth2SchemeSettings:
		node := map[string]any{
			"@id":   id,
			"@type": typeOAuth2Settings,
		}
		if l := len(st.AuthorizationGrants); l > 0 {
			grants := make([]string, l)
			for i, g := range st.AuthorizationGrants {
				grants[i] = g.Value
			}
			node["security:authorizationGrant"] = grants
		}
		if st.AuthorizationURI != nil || st.AccessTokenURI != nil || len(st.Scopes) > 0 {
			flowID := id + "/flows/default-flow"
			flowNode := map[string]any{
				"@id":   flowID,
				"@type": typeOAuth2Flow,
			}
			if st.AuthorizationURI != nil {
				flowNode["security:authorizationUri"] = st.AuthorizationURI.Value
			}
			if st.AccessTokenURI != nil {
				flowNode["security:accessTokenUri"] = st.AccessTokenURI.Value
			}
			if l := len(st.Scopes); l > 0 {
				scopeNodes := make([]map[string]any, l)
				for i, sc := range st.Scopes {
					scopeID := flowID + "/scope/" + sc.Value
					scopeNodes[i] = map[string]any{
						"@id":       scopeID,
						"@type":     typeSecurityScope,
						"core:name": sc.Value,
					}
				}
				flowNode["security:scope"] = scopeNodes
			}
			s.push(flowNode)
			node["security:flows"] = []any{MakeJSONLDRef(flowID)}
		}
		s.push(node)
		return id
	case *raml.OAuth1SchemeSettings:
		node := map[string]any{
			"@id":   id,
			"@type": typeOAuth1Settings,
		}
		if st.RequestTokenURI != nil {
			node["security:requestTokenUri"] = st.RequestTokenURI.Value
		}
		if st.AuthorizationURI != nil {
			node["security:authorizationUri"] = st.AuthorizationURI.Value
		}
		if st.TokenCredentialsURI != nil {
			node["security:tokenCredentialsUri"] = st.TokenCredentialsURI.Value
		}
		if l := len(st.Signatures); l > 0 {
			sigs := make([]string, l)
			for i, sig := range st.Signatures {
				sigs[i] = sig.Value
			}
			node["security:signature"] = sigs
		}
		s.push(node)
		return id
	case *raml.BasicAuthSchemeSettings:
		s.push(map[string]any{
			"@id":             id,
			"@type":           typeHttpSettings,
			"security:scheme": "basic",
		})
		return id
	case *raml.DigestAuthSchemeSettings:
		s.push(map[string]any{
			"@id":             id,
			"@type":           typeHttpSettings,
			"security:scheme": "digest",
		})
		return id
	default:
		return ""
	}
}

func (s *jsonLDSer) serSecuredBy(securedBy []*raml.SecurityScheme, parentID string) []JSONLDRef {
	if len(securedBy) == 0 {
		return nil
	}
	refs := make([]JSONLDRef, 0, len(securedBy))
	for i, ss := range securedBy {
		reqID := parentID + "/security/default-requirement_" + strconv.Itoa(i+1)
		reqNode := map[string]any{
			"@id":   reqID,
			"@type": typeSecurityRequirement,
		}
		if ss.Name != "" {
			pssID := reqID + "/schemes/" + jsonLDSeg(ss.Name)
			pssNode := map[string]any{
				"@id":       pssID,
				"@type":     typeParametrizedSecurityScheme,
				"core:name": ss.Name,
			}
			if ss.Name != "null" {
				pssNode["security:scheme"] = MakeJSONLDRef("#/declarations/securitySchemes/" + jsonLDSeg(ss.Name))
				if p, ok := ss.CompiledParams.(*raml.OAuth2OperationParams); ok && len(p.Scopes) > 0 {
					setID := pssID + "/settings/default"
					flowID := setID + "/flows/default-flow"
					scopeNodes := make([]map[string]any, len(p.Scopes))
					for i, sc := range p.Scopes {
						scopeNodes[i] = map[string]any{
							"@id":       flowID + "/scope/" + sc,
							"@type":     typeSecurityScope,
							"core:name": sc,
						}
					}
					flowNode := map[string]any{
						"@id":            flowID,
						"@type":          typeOAuth2Flow,
						"security:scope": scopeNodes,
					}
					s.push(flowNode)
					setNode := map[string]any{
						"@id":            setID,
						"@type":          typeOAuth2Settings,
						"security:flows": []any{MakeJSONLDRef(flowID)},
					}
					s.push(setNode)
					pssNode["security:settings"] = MakeJSONLDRef(setID)
				}
			}
			s.push(pssNode)
			reqNode["security:schemes"] = []JSONLDRef{MakeJSONLDRef(pssID)}
		}
		s.push(reqNode)
		refs = append(refs, MakeJSONLDRef(reqID))
	}
	return refs
}

// ---------------------------------------------------------------------------
// References (doc:references)
// ---------------------------------------------------------------------------

// fragUnitID returns the stable @id for a reference unit at index i.
func fragUnitID(i int) string {
	return "#/refs/" + strconv.Itoa(i)
}

// sortedNonEntryLocs returns sorted fragment locations from r.GetFragments()
// excluding the entry-point location.
func sortedNonEntryLocs(r *raml.RAML, entryLoc string) []string {
	frags := r.GetFragments()
	locs := make([]string, 0, len(frags))
	for loc := range frags {
		if loc != entryLoc {
			locs = append(locs, loc)
		}
	}
	sort.Strings(locs)
	return locs
}

// preRegisterReferencedFragments pre-registers Library type shapes using
// library-specific ID prefixes.
func (s *jsonLDSer) preRegisterReferencedFragments(r *raml.RAML, entryLoc string) {
	if r == nil {
		return
	}
	frags := r.GetFragments()
	locs := sortedNonEntryLocs(r, entryLoc)
	for i, loc := range locs {
		frag, ok := frags[loc]
		if !ok {
			continue
		}
		lib, ok := frag.(*raml.Library)
		if !ok {
			continue
		}
		prefix := "#/refs/" + strconv.Itoa(i) + "/declarations/"
		s.preRegisterTypes(prefix+"types/", lib.Types)
		s.preRegisterAnnotationTypes(prefix+"annotations/", lib.AnnotationTypes)
	}
}

// externalIncludeLocs returns sorted absolute paths of !include targets that
// are NOT typed RAML fragments.
func externalIncludeLocs(r *raml.RAML, fragLocSet map[string]bool) []string {
	seen := make(map[string]bool)
	var locs []string
	for _, irefs := range r.GetAllIncludeRefs() {
		for _, ir := range irefs {
			if ir.AbsPath != "" && !fragLocSet[ir.AbsPath] && !seen[ir.AbsPath] {
				seen[ir.AbsPath] = true
				locs = append(locs, ir.AbsPath)
			}
		}
	}
	sort.Strings(locs)
	return locs
}

// serReferences emits all referenced fragment units into s and returns the
// doc:references array for the document-root node.
func (s *jsonLDSer) serReferences(r *raml.RAML, entryLoc string) []JSONLDRef {
	if r == nil {
		return nil
	}
	frags := r.GetFragments()
	fragLocs := sortedNonEntryLocs(r, entryLoc)
	fragLocSet := make(map[string]bool, len(fragLocs))
	for _, loc := range fragLocs {
		fragLocSet[loc] = true
	}

	refs := make([]JSONLDRef, 0, len(fragLocs))

	for i, loc := range fragLocs {
		frag := frags[loc]
		unitID := fragUnitID(i)
		pdID := unitID + "/processingData"

		switch f := frag.(type) {
		case *raml.Library:
			prefix := "#/refs/" + strconv.Itoa(i) + "/declarations/"
			var declares []JSONLDRef
			declares = append(declares, s.serDeclaredTypes(prefix+"types/", f.Types)...)
			declares = append(declares, s.serDeclaredAnnotationTypes(prefix+"annotations/", f.AnnotationTypes)...)
			declares = append(declares, s.serDeclaredSecuritySchemes(prefix+"securitySchemes/", f.SecuritySchemes)...)
			unitNode := map[string]any{
				"@id":      unitID,
				"@type":    typeModuleUnit,
				"doc:root": false,
			}
			if len(declares) > 0 {
				unitNode["doc:declares"] = declares
			}
			s.push(map[string]any{
				"@id":                      pdID,
				"@type":                    typeAPIContractProcessingData,
				"doc:sourceSpec":           "RAML 1.0",
				"apiContract:modelVersion": "3.8.2",
			})
			unitNode["doc:processingData"] = MakeJSONLDRef(pdID)
			s.push(unitNode)

		case *raml.DataTypeFragment:
			unitNode := map[string]any{
				"@id":      unitID,
				"@type":    typeDataTypeFragment,
				"doc:root": false,
			}
			if f.Shape != nil {
				encID := s.serShape(f.Shape, unitID+"/encodes")
				unitNode["doc:encodes"] = MakeJSONLDRef(encID)
			}
			s.push(map[string]any{
				"@id":             pdID,
				"@type":           typeBaseUnitProcessingData,
				"doc:transformed": false,
			})
			unitNode["doc:processingData"] = MakeJSONLDRef(pdID)
			s.push(unitNode)

		case *raml.SecuritySchemeFragment:
			unitNode := map[string]any{
				"@id":      unitID,
				"@type":    typeSecuritySchemeFragment,
				"doc:root": false,
			}
			s.push(map[string]any{
				"@id":             pdID,
				"@type":           typeBaseUnitProcessingData,
				"doc:transformed": false,
			})
			unitNode["doc:processingData"] = MakeJSONLDRef(pdID)
			s.push(unitNode)

		case *raml.ResourceTypeFragment:
			unitNode := map[string]any{
				"@id":      unitID,
				"@type":    typeResourceTypeFragment,
				"doc:root": false,
			}
			s.push(map[string]any{
				"@id":             pdID,
				"@type":           typeBaseUnitProcessingData,
				"doc:transformed": false,
			})
			unitNode["doc:processingData"] = MakeJSONLDRef(pdID)
			s.push(unitNode)

		case *raml.TraitFragment:
			unitNode := map[string]any{
				"@id":      unitID,
				"@type":    typeTraitFragment,
				"doc:root": false,
			}
			s.push(map[string]any{
				"@id":             pdID,
				"@type":           typeBaseUnitProcessingData,
				"doc:transformed": false,
			})
			unitNode["doc:processingData"] = MakeJSONLDRef(pdID)
			s.push(unitNode)

		case *raml.NamedExample:
			unitNode := map[string]any{
				"@id":      unitID,
				"@type":    typeNamedExampleFragment,
				"doc:root": false,
			}
			s.push(map[string]any{
				"@id":             pdID,
				"@type":           typeBaseUnitProcessingData,
				"doc:transformed": false,
			})
			unitNode["doc:processingData"] = MakeJSONLDRef(pdID)
			s.push(unitNode)
		}
		refs = append(refs, MakeJSONLDRef(unitID))
	}

	// --- ExternalFragment nodes for plain !include targets ---
	extLocs := externalIncludeLocs(r, fragLocSet)
	for j, absPath := range extLocs {
		unitID := fragUnitID(len(fragLocs) + j)
		pdID := unitID + "/processingData"
		encID := unitID + "/encodes"

		extNode := map[string]any{
			"@id":   encID,
			"@type": typeExternalDomainElement,
		}
		if rc, err := r.LoadResource(absPath); err == nil {
			raw, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr == nil {
				extNode["doc:raw"] = string(raw)
			}
		}
		s.push(extNode)

		unitNode := map[string]any{
			"@id":         unitID,
			"@type":       typeExternalFragment,
			"doc:encodes": MakeJSONLDRef(encID),
			"doc:root":    false,
		}
		s.push(map[string]any{
			"@id":             pdID,
			"@type":           typeBaseUnitProcessingData,
			"doc:transformed": false,
		})
		unitNode["doc:processingData"] = MakeJSONLDRef(pdID)
		s.push(unitNode)
		refs = append(refs, MakeJSONLDRef(unitID))
	}

	return refs
}
