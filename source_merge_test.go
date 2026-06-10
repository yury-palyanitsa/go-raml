package raml

import (
	"context"
	"testing"

	"gopkg.in/yaml.v3"
)

// makeTraitDefFromYAML builds a TraitDefinition from src under the given parse
// scope (so its anchorFrag reflects the trait's declaration site).
func makeTraitDefFromYAML(t *testing.T, r *RAML, name, src string, scope ParseCtx) *TraitDefinition {
	t.Helper()
	r.pushParseCtx(scope)
	defer r.popParseCtx()
	td, err := r.makeTraitDefinition(scalarKeyNode(name), mappingNodeFromYAML(t, src), "trait.raml")
	if err != nil {
		t.Fatalf("makeTraitDefinition: %v", err)
	}
	return td
}

const traitWithBodyYAML = `
body:
  application/json:
    type: string
`

func TestMergeTraitDefIntoSource_WholeBodyGraftUsesTraitScope(t *testing.T) {
	r := New(context.Background())
	traitFrag := &stubResolver{location: "trait.raml"}
	td := makeTraitDefFromYAML(t, r, "withBody", traitWithBodyYAML, ParseCtx{AnchorFrag: traitFrag})

	// Empty operation declared in a different document.
	op := &SourceOperation{
		ID:         1,
		Method:     "get",
		body:       nil,
		scope:      ParseCtx{AnchorFrag: &stubResolver{location: "api.raml"}},
		provenance: provenanceOverlay{},
		Location:   "api.raml",
		raml:       r,
	}

	caller := ParseCtx{AnchorFrag: &stubResolver{location: "api.raml"}}
	r.mergeTraitDefIntoSource(op, td, map[string]*yaml.Node{}, caller)

	if op.body == nil {
		t.Fatalf("trait body was not grafted onto empty operation")
	}
	operation, st := r.decodeSourceOperation(op)
	if st != nil {
		t.Fatalf("decode: %v", st)
	}
	if got := bodyShape(t, operation, "application/json").anchorFrag; got != traitFrag {
		t.Fatalf("grafted body shape anchorFrag = %v, want trait scope %v", got, traitFrag)
	}
}

const traitWithResponsesYAML = `
responses:
  200:
    body:
      application/json:
        type: string
`

func TestMergeTraitDefIntoSource_OwnBodyWinsTraitFacetGrafted(t *testing.T) {
	r := New(context.Background())
	traitFrag := &stubResolver{location: "trait.raml"}
	td := makeTraitDefFromYAML(t, r, "withResponses", traitWithResponsesYAML, ParseCtx{AnchorFrag: traitFrag})

	apiFrag := &stubResolver{location: "api.raml"}
	// Operation with its own body; trait contributes a responses facet only.
	r.pushParseCtx(ParseCtx{AnchorFrag: apiFrag})
	op, err := r.makeSourceOperation("api.raml", scalarKeyNode("get"),
		mappingNodeFromYAML(t, "body:\n  application/json:\n    type: string\n"))
	r.popParseCtx()
	if err != nil {
		t.Fatalf("makeSourceOperation: %v", err)
	}

	r.mergeTraitDefIntoSource(op, td, map[string]*yaml.Node{}, ParseCtx{AnchorFrag: apiFrag})

	// The operation retains its own body and gains the trait's responses.
	if valueOf(op.body, FacetBody) == nil {
		t.Fatalf("operation lost its own body facet")
	}
	respVal := valueOf(op.body, FacetResponses)
	if respVal == nil {
		t.Fatalf("trait responses facet was not grafted")
	}
	if scope, ok := op.provenance[respVal]; !ok || scope.AnchorFrag != traitFrag {
		t.Fatalf("grafted responses facet not marked trait scope: ok=%v scope=%+v", ok, scope)
	}

	operation, st := r.decodeSourceOperation(op)
	if st != nil {
		t.Fatalf("decode: %v", st)
	}
	// Own body resolves at the operation's own (api) scope.
	if got := bodyShape(t, operation, "application/json").anchorFrag; got != apiFrag {
		t.Fatalf("own body shape anchorFrag = %v, want api scope %v", got, apiFrag)
	}
	if operation.Responses == nil || operation.Responses.Len() == 0 {
		t.Fatalf("responses were not materialized")
	}
}

const traitWithParamTypeYAML = `
body:
  application/json:
    type: <<typeName>>
`

func TestMergeTraitDefIntoSource_ParamSubstitutionMarkedCallerScope(t *testing.T) {
	r := New(context.Background())
	traitFrag := &stubResolver{location: "trait.raml"}
	td := makeTraitDefFromYAML(t, r, "withParam", traitWithParamTypeYAML, ParseCtx{AnchorFrag: traitFrag})

	apiFrag := &stubResolver{location: "api.raml"}
	op := &SourceOperation{
		ID:         1,
		Method:     "get",
		body:       nil,
		scope:      ParseCtx{AnchorFrag: apiFrag},
		provenance: provenanceOverlay{},
		Location:   "api.raml",
		raml:       r,
	}

	params := map[string]*yaml.Node{
		"typeName": {Kind: yaml.ScalarNode, Tag: TagStr, Value: "string"},
	}
	r.mergeTraitDefIntoSource(op, td, params, ParseCtx{AnchorFrag: apiFrag})

	// The substituted type scalar must be marked with the caller scope.
	var callerMarked bool
	for node, scope := range op.provenance {
		if node.Kind == yaml.ScalarNode && node.Value == "string" && scope.AnchorFrag == apiFrag {
			callerMarked = true
		}
	}
	if !callerMarked {
		t.Fatalf("substituted type scalar not marked caller scope; overlay=%#v", op.provenance)
	}
}

// makeSourceOpUnderScope builds a stage-1 SourceOperation under the given scope.
func makeSourceOpUnderScope(t *testing.T, r *RAML, scope ParseCtx, location, method, body string) *SourceOperation {
	t.Helper()
	r.pushParseCtx(scope)
	defer r.popParseCtx()
	op, err := r.makeSourceOperation(location, scalarKeyNode(method), mappingNodeFromYAML(t, body))
	if err != nil {
		t.Fatalf("makeSourceOperation: %v", err)
	}
	return op
}

func TestMergeSourceOperation_TargetBodyWinsSourceFacetGrafted(t *testing.T) {
	r := New(context.Background())
	apiFrag := &stubResolver{location: "api.raml"}
	rtFrag := &stubResolver{location: "rt.raml"}

	target := makeSourceOpUnderScope(t, r, ParseCtx{AnchorFrag: apiFrag}, "api.raml", "get",
		"body:\n  application/json:\n    type: string\n")
	source := makeSourceOpUnderScope(t, r, ParseCtx{AnchorFrag: rtFrag}, "rt.raml", "get",
		"responses:\n  200:\n    body:\n      application/json:\n        type: string\n")

	mergeSourceOperation(target, source, ParseCtx{AnchorFrag: rtFrag})

	if valueOf(target.body, FacetBody) == nil {
		t.Fatalf("target lost its own body")
	}
	respVal := valueOf(target.body, FacetResponses)
	if respVal == nil {
		t.Fatalf("source responses facet not grafted")
	}
	if scope, ok := target.provenance[respVal]; !ok || scope.AnchorFrag != rtFrag {
		t.Fatalf("grafted responses not marked source scope: ok=%v scope=%+v", ok, scope)
	}
}

func TestMergeSourceEndPoint_OperationsMergedAndGrafted(t *testing.T) {
	r := New(context.Background())
	apiFrag := &stubResolver{location: "api.raml"}
	rtFrag := &stubResolver{location: "rt.raml"}

	// Target endpoint: declares get only.
	r.pushParseCtx(ParseCtx{AnchorFrag: apiFrag})
	target, err := r.makeSourceEndPoint(scalarKeyNode("/users"),
		mappingNodeFromYAML(t, "get:\n  body:\n    application/json:\n      type: string\n"),
		"api.raml", "")
	r.popParseCtx()
	if err != nil {
		t.Fatalf("target makeSourceEndPoint: %v", err)
	}

	// Source (resource type) endpoint: declares get (responses) and post (new).
	r.pushParseCtx(ParseCtx{AnchorFrag: rtFrag})
	source, err := r.makeSourceEndPoint(scalarKeyNode("/users"),
		mappingNodeFromYAML(t, "get:\n  responses:\n    200:\n      body:\n        application/json:\n          type: string\npost:\n  body:\n    application/json:\n      type: string\n"),
		"rt.raml", "")
	r.popParseCtx()
	if err != nil {
		t.Fatalf("source makeSourceEndPoint: %v", err)
	}

	mergeSourceEndPoint(target, source, ParseCtx{AnchorFrag: rtFrag})

	// get merged: keeps own body, gains responses.
	getOp, ok := target.Operations.Get("get")
	if !ok {
		t.Fatalf("get operation missing after merge")
	}
	if valueOf(getOp.body, FacetBody) == nil || valueOf(getOp.body, FacetResponses) == nil {
		t.Fatalf("get not merged: keys=%v", bodyKeys(getOp.body))
	}
	// post grafted from source: default scope set to source scope.
	postOp, ok := target.Operations.Get("post")
	if !ok {
		t.Fatalf("post operation not grafted from source")
	}
	if postOp.scope.AnchorFrag != rtFrag {
		t.Fatalf("grafted post op scope = %+v, want rt scope", postOp.scope)
	}

	// End-to-end: materialize and check scopes.
	ep, st := r.decodeSourceEndPoint(target)
	if st != nil {
		t.Fatalf("decode: %v", st)
	}
	mGet, _ := ep.Operations.Get("get")
	if got := bodyShape(t, mGet, "application/json").anchorFrag; got != apiFrag {
		t.Fatalf("get own body anchorFrag = %v, want api scope", got)
	}
	mPost, _ := ep.Operations.Get("post")
	if got := bodyShape(t, mPost, "application/json").anchorFrag; got != rtFrag {
		t.Fatalf("grafted post body anchorFrag = %v, want rt scope", got)
	}
}
