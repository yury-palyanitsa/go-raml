package raml

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// buildVarIndex computes the variable index for a template body the same way
// makeTraitDefinition does, so provenance tests exercise real indexing.
func buildVarIndex(t *testing.T, body *yaml.Node) map[int][]VariableInfo {
	t.Helper()
	idxMap := make(map[int][]VariableInfo)
	declared := make(map[string]struct{})
	if err := collectVariablesIndex("tmpl.raml", body, 0, idxMap, declared); err != nil {
		t.Fatalf("collectVariablesIndex: %v", err)
	}
	return idxMap
}

func scalarParam(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: TagStr, Value: v}
}

func TestCompileSourceProvenance_StaticStaysUnmarked(t *testing.T) {
	body := mappingNodeFromYAML(t, `
description: a static description
headers:
  X-Static:
    type: string
`)
	idxMap := buildVarIndex(t, body)
	overlay := provenanceOverlay{}
	caller := ParseCtx{}

	out := compileSourceProvenance(body, nil, 0, idxMap, caller, overlay)
	if out != body {
		t.Errorf("static body should be returned unchanged (same pointer)")
	}
	if len(overlay) != 0 {
		t.Errorf("static body must not populate overlay, got %d entries", len(overlay))
	}
}

func TestCompileSourceProvenance_ScalarSubstitutionMarked(t *testing.T) {
	body := mappingNodeFromYAML(t, `
queryParameters:
  <<paramName>>:
    type: string
`)
	idxMap := buildVarIndex(t, body)
	overlay := provenanceOverlay{}
	caller := ParseCtx{AnchorFrag: &Library{Location: "caller.raml"}}
	params := map[string]*yaml.Node{"paramName": scalarParam("page")}

	out := compileSourceProvenance(body, params, 0, idxMap, caller, overlay)

	qp := valueOf(out, "queryParameters")
	if qp == nil {
		t.Fatalf("missing queryParameters")
	}
	// The substituted key scalar must be present and marked caller-scoped.
	var marked *yaml.Node
	for i := 0; i+1 < len(qp.Content); i += 2 {
		if qp.Content[i].Value == "page" {
			marked = qp.Content[i]
		}
	}
	if marked == nil {
		t.Fatalf("substituted key 'page' not found, keys=%v", bodyKeys(qp))
	}
	sc, ok := overlay[marked]
	if !ok {
		t.Errorf("substituted scalar not recorded in overlay")
	} else if sc != caller {
		t.Errorf("overlay scope = %+v, want caller %+v", sc, caller)
	}
}

func TestCompileSourceProvenance_ComplexParamSpliced(t *testing.T) {
	body := mappingNodeFromYAML(t, `
body:
  application/json: <<schema>>
`)
	idxMap := buildVarIndex(t, body)
	overlay := provenanceOverlay{}
	caller := ParseCtx{AnchorFrag: &Library{Location: "caller.raml"}}
	// Complex (mapping) parameter value.
	complexVal := mappingNodeFromYAML(t, `
type: object
properties:
  id: string
`)
	params := map[string]*yaml.Node{"schema": complexVal}

	out := compileSourceProvenance(body, params, 0, idxMap, caller, overlay)

	bodyNode := valueOf(out, "body")
	spliced := valueOf(bodyNode, "application/json")
	if spliced != complexVal {
		t.Fatalf("expected the complex param node to be spliced in by pointer")
	}
	sc, ok := overlay[complexVal]
	if !ok {
		t.Errorf("spliced complex param not recorded in overlay")
	} else if sc != caller {
		t.Errorf("overlay scope = %+v, want caller %+v", sc, caller)
	}
}

func TestCompileSourceProvenance_MixedStaticAndDynamic(t *testing.T) {
	// One static type ref + one parameter-substituted ref in the same body.
	body := mappingNodeFromYAML(t, `
responses:
  200:
    body:
      application/json: StaticType
  400:
    body:
      application/json: <<errType>>
`)
	idxMap := buildVarIndex(t, body)
	overlay := provenanceOverlay{}
	caller := ParseCtx{AnchorFrag: &Library{Location: "caller.raml"}}
	params := map[string]*yaml.Node{"errType": scalarParam("MyError")}

	out := compileSourceProvenance(body, params, 0, idxMap, caller, overlay)

	// Exactly one boundary root recorded: the substituted scalar only.
	if len(overlay) != 1 {
		t.Errorf("expected exactly 1 overlay entry (the dynamic ref), got %d", len(overlay))
	}
	// The static ref scalar must NOT be in the overlay.
	resp := valueOf(out, "responses")
	r200 := valueOf(resp, "200")
	staticRef := valueOf(valueOf(r200, "body"), "application/json")
	if _, ok := overlay[staticRef]; ok {
		t.Errorf("static type ref must remain unmarked (declaration scope)")
	}
	if staticRef.Value != "StaticType" {
		t.Errorf("static ref value = %q, want StaticType", staticRef.Value)
	}
}

func TestCompileSourceProvenance_ActionApplied(t *testing.T) {
	body := mappingNodeFromYAML(t, "displayName: <<name|!singularize>>")
	idxMap := buildVarIndex(t, body)
	overlay := provenanceOverlay{}
	caller := ParseCtx{}
	params := map[string]*yaml.Node{"name": scalarParam("users")}

	out := compileSourceProvenance(body, params, 0, idxMap, caller, overlay)
	dn := valueOf(out, "displayName")
	if dn == nil || dn.Value != "user" {
		t.Errorf("displayName = %v, want singularized 'user'", dn)
	}
	if _, ok := overlay[dn]; !ok {
		t.Errorf("substituted+actioned scalar should be marked caller-scoped")
	}
}
