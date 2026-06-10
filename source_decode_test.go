package raml

import (
	"context"
	"fmt"
	"testing"
)

// stubResolver is a minimal ReferenceResolver used to give a ParseCtx a
// distinguishable AnchorFrag. Lookups are not exercised by these tests (the
// bodies use the built-in `string` type), so they return errors.
type stubResolver struct{ location string }

func (s *stubResolver) GetLocation() string { return s.location }
func (s *stubResolver) GetReferenceType(string) (*BaseShape, error) {
	return nil, fmt.Errorf("stub: no type")
}
func (s *stubResolver) GetReferenceAnnotationType(string) (*BaseShape, error) {
	return nil, fmt.Errorf("stub: no annotation type")
}
func (s *stubResolver) GetResourceTypeDefinition(string) (*ResourceTypeDefinition, error) {
	return nil, fmt.Errorf("stub: no resource type")
}
func (s *stubResolver) GetTraitDefinition(string) (*TraitDefinition, error) {
	return nil, fmt.Errorf("stub: no trait")
}

// bodyShape returns the request body shape for mediaType, failing the test if
// it is absent.
func bodyShape(t *testing.T, op *Operation, mediaType string) *BaseShape {
	t.Helper()
	if op.Request == nil {
		t.Fatalf("operation has no request")
	}
	body, ok := op.Request.Bodies.Get(mediaType)
	if !ok {
		t.Fatalf("no body for media type %q", mediaType)
	}
	if body.Shape == nil {
		t.Fatalf("body %q has nil shape", mediaType)
	}
	return body.Shape
}

const opBodyYAML = `
body:
  application/json:
    type: string
`

func TestDecodeSourceOperation_BodyUsesDefaultScope(t *testing.T) {
	r := New(context.Background())
	decl := &stubResolver{location: "decl.raml"}

	op := &SourceOperation{
		ID:         1,
		Method:     "get",
		body:       mappingNodeFromYAML(t, opBodyYAML),
		scope:      ParseCtx{AnchorFrag: decl},
		provenance: provenanceOverlay{},
		Location:   "decl.raml",
		raml:       r,
	}

	operation, st := r.decodeSourceOperation(op)
	if st != nil {
		t.Fatalf("unexpected error: %v", st)
	}
	if got := bodyShape(t, operation, "application/json").anchorFrag; got != decl {
		t.Fatalf("body shape anchorFrag = %v, want declaration scope %v", got, decl)
	}
	if len(r.parseCtxStack) != 0 {
		t.Fatalf("parse ctx stack not balanced: len=%d", len(r.parseCtxStack))
	}
}

func TestDecodeSourceOperation_BodyBoundaryUsesCallerScope(t *testing.T) {
	r := New(context.Background())
	decl := &stubResolver{location: "decl.raml"}
	caller := &stubResolver{location: "caller.raml"}

	body := mappingNodeFromYAML(t, opBodyYAML)
	bodyVal := valueOf(body, FacetBody)
	if bodyVal == nil {
		t.Fatalf("body facet value not found")
	}

	op := &SourceOperation{
		ID:         1,
		Method:     "get",
		body:       body,
		scope:      ParseCtx{AnchorFrag: decl},
		provenance: provenanceOverlay{bodyVal: {AnchorFrag: caller}},
		Location:   "decl.raml",
		raml:       r,
	}

	operation, st := r.decodeSourceOperation(op)
	if st != nil {
		t.Fatalf("unexpected error: %v", st)
	}
	if got := bodyShape(t, operation, "application/json").anchorFrag; got != caller {
		t.Fatalf("body shape anchorFrag = %v, want caller scope %v", got, caller)
	}
	if len(r.parseCtxStack) != 0 {
		t.Fatalf("parse ctx stack not balanced: len=%d", len(r.parseCtxStack))
	}
}

func TestDecodeSourceOperation_NilBodyCarriesDirectives(t *testing.T) {
	r := New(context.Background())
	traits := []*Trait{{Name: "secured"}}
	secured := []*SecurityScheme{{}}

	op := &SourceOperation{
		ID:                7,
		Method:            "post",
		Traits:            traits,
		SecuredBy:         secured,
		explicitSecuredBy: true,
		body:              nil,
		scope:             ParseCtx{},
		provenance:        provenanceOverlay{},
		Location:          "decl.raml",
		raml:              r,
	}

	operation, st := r.decodeSourceOperation(op)
	if st != nil {
		t.Fatalf("unexpected error: %v", st)
	}
	if operation.ID != 7 || operation.Method != "post" {
		t.Fatalf("identity not carried over: id=%d method=%q", operation.ID, operation.Method)
	}
	if len(operation.Traits) != 1 || operation.Traits[0].Name != "secured" {
		t.Fatalf("traits not carried over: %#v", operation.Traits)
	}
	if !operation.explicitSecuredBy || len(operation.SecuredBy) != 1 {
		t.Fatalf("explicit securedBy not carried over: explicit=%v len=%d",
			operation.explicitSecuredBy, len(operation.SecuredBy))
	}
}

func TestDecodeSourceOperation_NonExplicitSecuredByInheritsGlobal(t *testing.T) {
	r := New(context.Background())
	global := []*SecurityScheme{{}}
	r.globalSecuredBy = global

	op := &SourceOperation{
		ID:         1,
		Method:     "get",
		body:       nil,
		scope:      ParseCtx{},
		provenance: provenanceOverlay{},
		Location:   "decl.raml",
		raml:       r,
	}

	operation, st := r.decodeSourceOperation(op)
	if st != nil {
		t.Fatalf("unexpected error: %v", st)
	}
	if len(operation.SecuredBy) != 1 || operation.SecuredBy[0] != global[0] {
		t.Fatalf("operation did not inherit global securedBy: %#v", operation.SecuredBy)
	}
	if operation.explicitSecuredBy {
		t.Fatalf("explicitSecuredBy should be false for inherited global")
	}
}

const endpointYAML = `
displayName: Users
get:
  body:
    application/json:
      type: string
/{id}:
  get:
    body:
      application/json:
        type: string
`

func TestDecodeSourceEndPoint_OperationsNestingAndFacets(t *testing.T) {
	r := New(context.Background())
	decl := &stubResolver{location: "decl.raml"}
	r.pushParseCtx(ParseCtx{AnchorFrag: decl})

	keyNode := scalarKeyNode("/users")
	valueNode := mappingNodeFromYAML(t, endpointYAML)
	src, err := r.makeSourceEndPoint(keyNode, valueNode, "decl.raml", "")
	if err != nil {
		t.Fatalf("makeSourceEndPoint: %v", err)
	}
	r.popParseCtx()

	ep, st := r.decodeSourceEndPoint(src)
	if st != nil {
		t.Fatalf("unexpected error: %v", st)
	}
	if ep.URI != "/users" || ep.FullURI != "/users" {
		t.Fatalf("uri not carried over: uri=%q fullURI=%q", ep.URI, ep.FullURI)
	}
	if ep.DisplayName == nil || ep.DisplayName.Value != "Users" {
		t.Fatalf("displayName facet not materialized: %#v", ep.DisplayName)
	}
	if _, ok := ep.Operations.Get("get"); !ok {
		t.Fatalf("get operation not materialized")
	}
	child, ok := ep.EndPoints.Get("/{id}")
	if !ok {
		t.Fatalf("nested endpoint not materialized")
	}
	if child.FullURI != "/users/{id}" {
		t.Fatalf("nested endpoint fullURI = %q, want /users/{id}", child.FullURI)
	}
	if _, ok := child.Operations.Get("get"); !ok {
		t.Fatalf("nested endpoint get operation not materialized")
	}
	// The endpoint is registered into the RAML endpoint index.
	if _, ok := r.endPoints["/users"]; !ok {
		t.Fatalf("endpoint /users not registered in index")
	}
	if _, ok := r.endPoints["/users/{id}"]; !ok {
		t.Fatalf("endpoint /users/{id} not registered in index")
	}
	if len(r.parseCtxStack) != 0 {
		t.Fatalf("parse ctx stack not balanced: len=%d", len(r.parseCtxStack))
	}
}

func TestDecodeSourceEndPoint_OperationBodyUsesEndpointScope(t *testing.T) {
	r := New(context.Background())
	decl := &stubResolver{location: "decl.raml"}
	r.pushParseCtx(ParseCtx{AnchorFrag: decl})

	keyNode := scalarKeyNode("/users")
	valueNode := mappingNodeFromYAML(t, endpointYAML)
	src, err := r.makeSourceEndPoint(keyNode, valueNode, "decl.raml", "")
	if err != nil {
		t.Fatalf("makeSourceEndPoint: %v", err)
	}
	r.popParseCtx()

	ep, st := r.decodeSourceEndPoint(src)
	if st != nil {
		t.Fatalf("unexpected error: %v", st)
	}
	op, _ := ep.Operations.Get("get")
	if got := bodyShape(t, op, "application/json").anchorFrag; got != decl {
		t.Fatalf("operation body shape anchorFrag = %v, want declaration scope %v", got, decl)
	}
}
