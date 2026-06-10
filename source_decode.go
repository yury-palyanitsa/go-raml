package raml

import (
	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

// This file implements stage 2 of the two-stage build: the provenance-aware
// materialization of a stage-1 SourceOperation into a real Operation.
//
// Stage 2 reuses the existing facet decoders (unmarshalHeaders, makeRequest,
// makeResponses, ...) but drives the ParseCtx stack from the provenance overlay
// so that every shape is created under the lexical scope its subtree belongs to:
//   - the SourceOperation's own scope is the default (pushed for the whole body);
//   - a facet value that is a provenance boundary root temporarily overrides that
//     default while its subtree is decoded.
//
// Because BaseShape stamps its anchorFrag from the current ParseCtx at creation
// time, pushing the boundary scope around the facet decode is sufficient to make
// shapes resolve unqualified type references against the correct fragment.
//
// Scope granularity (D3, minimal-first): boundaries are honoured at facet-value
// granularity. Sub-facet boundaries (e.g. a single media type grafted into a
// body that also carries declaration-scoped media types, or a per-property type
// reference) are not yet split — a single BaseShape carries a single anchorFrag.
// Finer granularity is deferred until the structural-merge wiring requires it.

// locationOf returns the file of origin for a YAML node, consulting the active
// provenance overlay. Nodes grafted from a trait or resource type fragment
// were deep-marked by markGraft / compileSourceProvenance, so an overlay hit
// (boundary root or any descendant) yields the contributing fragment's path.
// Nodes that the overlay does not know about are nodes from the document
// being decoded, so the caller's default location is correct for them.
//
// This is the single source of truth for "what file does this node belong
// to?" — used by entity constructors (BaseShape, Body, Request, Response) and
// structural helpers (unmarshalHeaders, makeResponses, …) to attribute their
// Location field and their wrap diagnostics to the right file, even when the
// node sits below a merge-synthesized intermediate container that itself
// carries no overlay entry.
func (r *RAML) locationOf(node *yaml.Node, defaultLoc string) string {
	if node == nil || r.activeOverlay == nil {
		return defaultLoc
	}
	if sc, ok := r.activeOverlay[node]; ok && sc.AnchorFrag != nil {
		return sc.AnchorFrag.GetLocation()
	}
	return defaultLoc
}

// provenanceScopeFor reports the resolution scope a type-bearing YAML node
// should be decoded under, consulting the active provenance overlay.
//
// Priority order is most-specific first:
//  1. The type:/schema: facet VALUE of v, when v is a mapping. A
//     caller-substituted scalar lives here; its mark (callerScope) is more
//     specific than the surrounding grafted subtree's mark and must win.
//  2. v itself. The grafted subtree root carries the
//     trait/RT declaration scope and is reported when no nested substitution
//     overrides it.
//
// Returns ok=false when no overlay is active or no relevant node carries a
// mark.
func (r *RAML) provenanceScopeFor(v *yaml.Node) (ParseCtx, bool) {
	if r.activeOverlay == nil || v == nil {
		return ParseCtx{}, false
	}
	if v.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(v.Content); i += 2 {
			switch v.Content[i].Value {
			case FacetType, FacetSchema:
				if sc, ok := r.activeOverlay[v.Content[i+1]]; ok {
					return sc, true
				}
			}
		}
	}
	if sc, ok := r.activeOverlay[v]; ok {
		return sc, true
	}
	return ParseCtx{}, false
}

// decodeSourceOperation materializes a stage-1 SourceOperation into a real
// Operation, applying provenance scopes while decoding its type-bearing body.
//
// Errors are accumulated per facet rather than failing fast, so a single broken
// facet does not discard the rest of the operation — the partial-result
// tolerance the LSP relies on. The returned Operation is always non-nil.
func (r *RAML) decodeSourceOperation(op *SourceOperation) (*Operation, *stacktrace.StackTrace) {
	securedBy := op.SecuredBy
	if !op.explicitSecuredBy {
		securedBy = r.globalSecuredBy
	}

	operation := &Operation{
		ID:                op.ID,
		Method:            op.Method,
		Traits:            op.Traits,
		RTTraits:          op.RTTraits,
		SecuredBy:         securedBy,
		explicitSecuredBy: op.explicitSecuredBy,

		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),

		raml:     r,
		KeyPos:   op.KeyPos,
		ValuePos: op.ValuePos,
		Location: op.Location,
	}

	if op.body == nil {
		return operation, nil
	}

	// The body root may itself be a provenance boundary (e.g. an operation with
	// no own body whose entire body was grafted from a trait). Use that scope as
	// the base; otherwise the operation's own declaration scope.
	bodyScope := op.scope
	if s, ok := op.provenance[op.body]; ok {
		bodyScope = s
	}
	r.pushParseCtx(bodyScope)
	defer r.popParseCtx()

	return operation, operation.decodeBodyScoped(op.body, op.provenance)
}

// decodeBodyScoped decodes the type-bearing remainder of an operation body,
// pushing the provenance scope of each facet value (when it is a boundary root)
// for the duration of that facet's decode. Facet errors are accumulated.
//
// Effective file attribution (the Location stamped on entities and error
// frames raised below) is resolved one layer deeper, at each entity
// constructor / structural helper entry, via RAML.locationOf. That single
// rule survives merge-synthesized intermediate containers (where this
// per-facet hook would miss because the synthesized container has no overlay
// entry, only its grafted children do).
func (o *Operation) decodeBodyScoped(node *yaml.Node, overlay provenanceOverlay) *stacktrace.StackTrace {
	if node.Tag == TagNull {
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("operation must be a mapping node", o.Location, WithNodePosition(node))
	}

	prev := o.raml.activeOverlay
	o.raml.activeOverlay = overlay
	defer func() { o.raml.activeOverlay = prev }()

	var acc stacktrace.Accumulator
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		pushed := false
		if scope, ok := overlay[valueNode]; ok {
			o.raml.pushParseCtx(scope)
			pushed = true
		}
		acc.Add(o.decodeField(keyNode, valueNode))
		if pushed {
			o.raml.popParseCtx()
		}
	}
	return acc.Result()
}

// decodeField decodes a single operation facet (key/value pair). It mirrors the
// per-facet handling of the legacy Operation.decode but returns a *StackTrace so
// callers can accumulate errors. The stage-1 directive facets (is:, securedBy:)
// are already stripped from the body, but they are handled here defensively.
func (o *Operation) decodeField(keyNode, valueNode *yaml.Node) *stacktrace.StackTrace {
	switch keyNode.Value {
	case FacetDisplayName:
		node, err := MakeScalarFacetYAML[string](o.raml, keyNode, valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("make scalar node", err, o.Location, WithNodePosition(valueNode))
		}
		o.DisplayName = node
	case FacetDescription:
		node, err := MakeScalarFacetYAML[string](o.raml, keyNode, valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("make scalar node", err, o.Location, WithNodePosition(valueNode))
		}
		o.Description = node
	case FacetProtocols:
		if valueNode.Kind != yaml.SequenceNode {
			return StacktraceNew("protocols must be an array", o.Location, WithNodePosition(valueNode))
		}
		protocols := make([]*Node[string], len(valueNode.Content))
		for i, v := range valueNode.Content {
			fragmentPath, rv, err := o.raml.resolveInclude(v, o.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve include", err, o.Location, WithNodePosition(v))
			}
			if !isValidProtocol(rv.Value) {
				return StacktraceNew("unknown protocol", o.Location, WithNodePosition(v))
			}
			protocols[i] = MakeSeqNode(rv.Value, v, o.Location, fragmentPath)
		}
		if len(protocols) == 0 {
			return StacktraceNew("protocols must not be empty", o.Location, WithNodePosition(valueNode))
		}
		o.Protocols = protocols
	case FacetSecuredBy:
		securitySchemes, err := o.raml.makeSecuritySchemes(valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("make security schemes", err, o.Location, WithNodePosition(valueNode))
		}
		o.SecuredBy = securitySchemes
		o.explicitSecuredBy = true
	case FacetHeaders:
		headers, err := o.raml.unmarshalHeaders(valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("unmarshal headers", err, o.Location, WithNodePosition(valueNode))
		}
		o.Headers = headers
	case FacetQueryParameters:
		if o.QueryString != nil {
			return StacktraceNew("queryParameters and queryString are mutually exclusive", o.Location, WithNodePosition(keyNode))
		}
		params, err := o.raml.unmarshalQueryParameters(valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("unmarshal query parameters", err, o.Location, WithNodePosition(valueNode))
		}
		o.QueryParameters = params
	case FacetQueryString:
		if o.QueryParameters != nil {
			return StacktraceNew("queryParameters and queryString are mutually exclusive", o.Location, WithNodePosition(keyNode))
		}
		shape, err := o.raml.unmarshalQueryString(keyNode, valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("unmarshal query string", err, o.Location, WithNodePosition(valueNode))
		}
		o.QueryString = shape
	case FacetBody:
		request, err := o.raml.makeRequest(keyNode, valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("make request", err, o.Location, WithNodePosition(valueNode))
		}
		o.Request = request
	case FacetResponses:
		responses, err := o.raml.makeResponses(valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("make responses", err, o.Location, WithNodePosition(valueNode))
		}
		o.Responses = responses
	case FacetIs:
		traits, err := o.raml.makeTraits(valueNode, o.Location)
		if err != nil {
			return StacktraceNewWrapped("make traits", err, o.Location, WithNodePosition(valueNode))
		}
		o.Traits = traits
	default:
		if IsCustomDomainExtensionNode(keyNode.Value) {
			de, err := o.raml.unmarshalCustomDomainExtension(o.Location, keyNode, valueNode)
			if err != nil {
				return StacktraceNewWrapped("unmarshal custom domain extension", err, o.Location, WithNodePosition(valueNode))
			}
			o.CustomDomainProperties.Set(de.Name, de)
		} else {
			return StacktraceNew("unknown field", o.Location, WithNodePosition(keyNode), stacktrace.WithInfo("field", keyNode.Value))
		}
	}
	return nil
}

// decodeSourceEndPoint materializes a stage-1 SourceEndPoint into a real
// EndPoint, recursively materializing its child operations and nested endpoints
// and applying provenance scopes while decoding its endpoint-level body facets
// (uriParameters, displayName, description, custom domain extensions).
//
// Like decodeSourceOperation it accumulates errors per child/facet so a single
// broken operation or nested endpoint does not discard the rest of the tree, and
// it registers the materialized endpoint into the RAML endpoint index. The
// returned EndPoint is always non-nil.
func (r *RAML) decodeSourceEndPoint(ep *SourceEndPoint) (*EndPoint, *stacktrace.StackTrace) {
	securedBy := ep.SecuredBy
	if !ep.explicitSecuredBy {
		securedBy = r.globalSecuredBy
	}

	endPoint := &EndPoint{
		ID:                ep.ID,
		URI:               ep.URI,
		FullURI:           ep.FullURI,
		ResourceType:      ep.ResourceType,
		Traits:            ep.Traits,
		RTTraits:          ep.RTTraits,
		SecuredBy:         securedBy,
		explicitSecuredBy: ep.explicitSecuredBy,

		EndPoints:  orderedmap.New[string, *EndPoint](ep.EndPoints.Len()),
		Operations: orderedmap.New[string, *Operation](ep.Operations.Len()),

		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),

		raml:     r,
		KeyPos:   ep.KeyPos,
		ValuePos: ep.ValuePos,
		Location: ep.Location,
	}

	var acc stacktrace.Accumulator

	if ep.body != nil {
		bodyScope := ep.scope
		if s, ok := ep.provenance[ep.body]; ok {
			bodyScope = s
		}
		r.pushParseCtx(bodyScope)
		acc.Add(endPoint.decodeBodyScoped(ep.body, ep.provenance))
		r.popParseCtx()
	}

	for pair := ep.Operations.Oldest(); pair != nil; pair = pair.Next() {
		operation, st := r.decodeSourceOperation(pair.Value)
		acc.Add(st)
		endPoint.Operations.Set(pair.Key, operation)
	}

	for pair := ep.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		child, st := r.decodeSourceEndPoint(pair.Value)
		acc.Add(st)
		endPoint.EndPoints.Set(pair.Key, child)
	}

	if err := endPoint.validateURIParameters(); err != nil {
		acc.Add(StacktraceNewWrapped("validate uri parameters", err, ep.Location, stacktrace.WithPosition(&ep.KeyPos)))
	}

	if _, ok := r.endPoints[endPoint.FullURI]; ok {
		acc.Add(StacktraceNew("duplicated endpoint", ep.Location, stacktrace.WithPosition(&ep.ValuePos),
			stacktrace.WithInfo("uri", endPoint.FullURI)))
	} else {
		r.endPoints[endPoint.FullURI] = endPoint
	}
	return endPoint, acc.Result()
}

// decodeBodyScoped decodes the endpoint-level type-bearing remainder (the facets
// retained by stage-1 makeSourceEndPoint: uriParameters, displayName,
// description and custom domain extensions), pushing the provenance scope of
// each facet value when it is a boundary root. Facet errors are accumulated.
func (e *EndPoint) decodeBodyScoped(node *yaml.Node, overlay provenanceOverlay) *stacktrace.StackTrace {
	if node.Tag == TagNull {
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("endpoint must be a mapping node", e.Location, WithNodePosition(node))
	}

	prev := e.raml.activeOverlay
	e.raml.activeOverlay = overlay
	defer func() { e.raml.activeOverlay = prev }()

	var acc stacktrace.Accumulator
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		pushed := false
		if scope, ok := overlay[valueNode]; ok {
			e.raml.pushParseCtx(scope)
			pushed = true
		}
		acc.Add(e.decodeField(keyNode, valueNode))
		if pushed {
			e.raml.popParseCtx()
		}
	}
	return acc.Result()
}

// decodeField decodes a single endpoint-level body facet (key/value pair). It
// handles only the type-bearing/scalar facets retained by stage-1
// makeSourceEndPoint; directive and child facets (type:, is:, securedBy:, HTTP
// methods, nested endpoints) are already consumed in stage 1 and never appear
// here.
func (e *EndPoint) decodeField(keyNode, valueNode *yaml.Node) *stacktrace.StackTrace {
	switch keyNode.Value {
	case FacetDisplayName:
		node, err := MakeScalarFacetYAML[string](e.raml, keyNode, valueNode, e.Location)
		if err != nil {
			return StacktraceNewWrapped("make scalar node", err, e.Location, WithNodePosition(valueNode))
		}
		e.DisplayName = node
	case FacetDescription:
		node, err := MakeScalarFacetYAML[string](e.raml, keyNode, valueNode, e.Location)
		if err != nil {
			return StacktraceNewWrapped("make scalar node", err, e.Location, WithNodePosition(valueNode))
		}
		e.Description = node
	case FacetUriParameters:
		if err := e.unmarshalURIParameters(valueNode); err != nil {
			return StacktraceNewWrapped("unmarshal uri parameters", err, e.Location, WithNodePosition(valueNode))
		}
	default:
		if IsCustomDomainExtensionNode(keyNode.Value) {
			de, err := e.raml.unmarshalCustomDomainExtension(e.Location, keyNode, valueNode)
			if err != nil {
				return StacktraceNewWrapped("unmarshal custom domain extension", err, e.Location, WithNodePosition(valueNode))
			}
			e.CustomDomainProperties.Set(de.Name, de)
		} else {
			return StacktraceNew("unknown field", e.Location, WithNodePosition(keyNode), stacktrace.WithInfo("field", keyNode.Value))
		}
	}
	return nil
}
