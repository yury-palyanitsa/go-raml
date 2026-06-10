package raml

import (
	"context"
	"testing"

	"gopkg.in/yaml.v3"
)

// makeRTDefFromYAML builds a ResourceTypeDefinition from src under the given
// parse scope (so its anchorFrag reflects the RT's declaration
// site).
func makeRTDefFromYAML(t *testing.T, r *RAML, name, src string, scope ParseCtx) *ResourceTypeDefinition {
	t.Helper()
	r.pushParseCtx(scope)
	defer r.popParseCtx()
	rtDef, err := r.makeResourceTypeDefinition(scalarKeyNode(name), mappingNodeFromYAML(t, src), "rt.raml")
	if err != nil {
		t.Fatalf("makeResourceTypeDefinition: %v", err)
	}
	return rtDef
}

const rtStaticYAML = `
get:
  body:
    application/json:
      type: string
`

func TestCompileResourceTypeSource_StaticBodyUsesRTScope(t *testing.T) {
	r := New(context.Background())
	rtFrag := &stubResolver{location: "rt.raml"}
	apiFrag := &stubResolver{location: "api.raml"}
	rtDef := makeRTDefFromYAML(t, r, "collection", rtStaticYAML, ParseCtx{AnchorFrag: rtFrag})

	ep, st := r.compileResourceTypeSource(
		rtDef, map[string]*yaml.Node{}, map[string]struct{}{"get": {}},
		ParseCtx{AnchorFrag: apiFrag}, "api.raml", "/users", "")
	if st != nil {
		t.Fatalf("compile: %v", st)
	}
	if ep == nil {
		t.Fatalf("nil endpoint")
	}
	if _, ok := ep.Operations.Get("get"); !ok {
		t.Fatalf("get operation missing from compiled RT endpoint")
	}

	endpoint, st := r.decodeSourceEndPoint(ep)
	if st != nil {
		t.Fatalf("decode: %v", st)
	}
	getOp, ok := endpoint.Operations.Get("get")
	if !ok {
		t.Fatalf("get operation missing after decode")
	}
	// Static template content resolves at the resource type's own scope.
	if got := bodyShape(t, getOp, "application/json").anchorFrag; got != rtFrag {
		t.Fatalf("static body anchorFrag = %v, want rt scope %v", got, rtFrag)
	}
}

const rtParamYAML = `
get:
  body:
    application/json:
      type: <<typeName>>
`

func TestCompileResourceTypeSource_ParamSubstitutionMarkedCallerScope(t *testing.T) {
	r := New(context.Background())
	rtFrag := &stubResolver{location: "rt.raml"}
	apiFrag := &stubResolver{location: "api.raml"}
	rtDef := makeRTDefFromYAML(t, r, "collection", rtParamYAML, ParseCtx{AnchorFrag: rtFrag})

	params := map[string]*yaml.Node{
		"typeName": {Kind: yaml.ScalarNode, Tag: TagStr, Value: "string"},
	}
	ep, st := r.compileResourceTypeSource(
		rtDef, params, map[string]struct{}{"get": {}},
		ParseCtx{AnchorFrag: apiFrag}, "api.raml", "/users", "")
	if st != nil {
		t.Fatalf("compile: %v", st)
	}

	// The substituted type scalar must be marked with the caller scope, and the
	// mark must be distributed onto the owning operation's overlay.
	getOp, ok := ep.Operations.Get("get")
	if !ok {
		t.Fatalf("get operation missing")
	}
	var callerMarked bool
	for node, scope := range getOp.provenance {
		if node.Kind == yaml.ScalarNode && node.Value == "string" && scope.AnchorFrag == apiFrag {
			callerMarked = true
		}
	}
	if !callerMarked {
		t.Fatalf("substituted type scalar not marked caller scope; overlay=%#v", getOp.provenance)
	}
}

const rtOptionalYAML = `
get?:
  body:
    application/json:
      type: string
post:
  body:
    application/json:
      type: string
`

func TestCompileResourceTypeSource_OptionalMethodFiltered(t *testing.T) {
	r := New(context.Background())
	rtFrag := &stubResolver{location: "rt.raml"}
	apiFrag := &stubResolver{location: "api.raml"}
	rtDef := makeRTDefFromYAML(t, r, "collection", rtOptionalYAML, ParseCtx{AnchorFrag: rtFrag})

	// Target endpoint defines post only; optional get? must be dropped.
	ep, st := r.compileResourceTypeSource(
		rtDef, map[string]*yaml.Node{}, map[string]struct{}{"post": {}},
		ParseCtx{AnchorFrag: apiFrag}, "api.raml", "/users", "")
	if st != nil {
		t.Fatalf("compile: %v", st)
	}
	if _, ok := ep.Operations.Get("get"); ok {
		t.Fatalf("optional get? should have been filtered out")
	}
	if _, ok := ep.Operations.Get("post"); !ok {
		t.Fatalf("post operation missing")
	}
}

func TestCompileResourceTypeSource_UnexpectedParam(t *testing.T) {
	r := New(context.Background())
	rtFrag := &stubResolver{location: "rt.raml"}
	apiFrag := &stubResolver{location: "api.raml"}
	rtDef := makeRTDefFromYAML(t, r, "collection", rtStaticYAML, ParseCtx{AnchorFrag: rtFrag})

	params := map[string]*yaml.Node{
		"bogus": {Kind: yaml.ScalarNode, Tag: TagStr, Value: "x"},
	}
	_, st := r.compileResourceTypeSource(
		rtDef, params, map[string]struct{}{"get": {}},
		ParseCtx{AnchorFrag: apiFrag}, "api.raml", "/users", "")
	if st == nil {
		t.Fatalf("expected error for unexpected parameter")
	}
}
