package raml

import (
	"context"
	"testing"

	"gopkg.in/yaml.v3"
)

// mappingNodeFromYAML unmarshals src and returns the top-level mapping node.
func mappingNodeFromYAML(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		t.Fatalf("expected document node with content, got kind=%v", doc.Kind)
	}
	return doc.Content[0]
}

// scalarKeyNode builds a scalar key node with the given value, for use as the
// method/uri key passed alongside a value mapping.
func scalarKeyNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: TagStr, Value: value}
}

// bodyKeys returns the ordered list of top-level keys in a stage-1 body node.
func bodyKeys(body *yaml.Node) []string {
	if body == nil {
		return nil
	}
	keys := make([]string, 0, len(body.Content)/2)
	for i := 0; i < len(body.Content); i += 2 {
		keys = append(keys, body.Content[i].Value)
	}
	return keys
}

func containsStr(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}

func TestMakeSourceOperation_DirectivesAndBody(t *testing.T) {
	r := New(context.Background())
	body := mappingNodeFromYAML(t, `
is: [ pageable, secured ]
securedBy: [ oauth ]
displayName: List things
headers:
  X-Trace:
    type: string
responses:
  200:
    body:
      application/json: Thing
`)
	op, err := r.makeSourceOperation("api.raml", scalarKeyNode("get"), body)
	if err != nil {
		t.Fatalf("makeSourceOperation: %v", err)
	}

	if op.Method != "get" {
		t.Errorf("Method = %q, want get", op.Method)
	}
	if len(op.Traits) != 2 || op.Traits[0].Name != "pageable" || op.Traits[1].Name != "secured" {
		t.Errorf("Traits = %+v, want [pageable secured]", op.Traits)
	}
	if !op.explicitSecuredBy || len(op.SecuredBy) != 1 || op.SecuredBy[0].Name != "oauth" {
		t.Errorf("SecuredBy = %+v explicit=%v, want [oauth] explicit=true", op.SecuredBy, op.explicitSecuredBy)
	}

	keys := bodyKeys(op.body)
	if containsStr(keys, FacetIs) || containsStr(keys, FacetSecuredBy) {
		t.Errorf("body must not contain directive keys, got %v", keys)
	}
	for _, want := range []string{"displayName", "headers", "responses"} {
		if !containsStr(keys, want) {
			t.Errorf("body missing type-bearing key %q, got %v", want, keys)
		}
	}
}

func TestMakeSourceOperation_EmptyNullBody(t *testing.T) {
	r := New(context.Background())
	nullVal := &yaml.Node{Kind: yaml.ScalarNode, Tag: TagNull}
	op, err := r.makeSourceOperation("api.raml", scalarKeyNode("delete"), nullVal)
	if err != nil {
		t.Fatalf("makeSourceOperation: %v", err)
	}
	if op.body != nil {
		t.Errorf("body = %+v, want nil for null operation", op.body)
	}
	if len(op.Traits) != 0 || len(op.SecuredBy) != 0 {
		t.Errorf("expected no directives, got Traits=%v SecuredBy=%v", op.Traits, op.SecuredBy)
	}
}

func TestMakeSourceEndPoint_DirectivesOperationsAndNesting(t *testing.T) {
	r := New(context.Background())
	body := mappingNodeFromYAML(t, `
type: collection
is: [ secured ]
securedBy: [ oauth ]
displayName: Products
uriParameters:
  id:
    type: string
get:
  description: list
/items:
  get:
    description: nested list
`)
	ep, err := r.makeSourceEndPoint(scalarKeyNode("/products"), body, "api.raml", "")
	if err != nil {
		t.Fatalf("makeSourceEndPoint: %v", err)
	}

	if ep.URI != "/products" || ep.FullURI != "/products" {
		t.Errorf("URI=%q FullURI=%q, want /products /products", ep.URI, ep.FullURI)
	}
	if ep.ResourceType == nil || ep.ResourceType.Name != "collection" {
		t.Errorf("ResourceType = %+v, want collection", ep.ResourceType)
	}
	if len(ep.Traits) != 1 || ep.Traits[0].Name != "secured" {
		t.Errorf("Traits = %+v, want [secured]", ep.Traits)
	}
	if !ep.explicitSecuredBy || len(ep.SecuredBy) != 1 || ep.SecuredBy[0].Name != "oauth" {
		t.Errorf("SecuredBy = %+v explicit=%v, want [oauth] true", ep.SecuredBy, ep.explicitSecuredBy)
	}

	if _, ok := ep.Operations.Get("get"); !ok {
		t.Errorf("expected get operation, ops=%v", ep.Operations.Len())
	}
	child, ok := ep.EndPoints.Get("/items")
	if !ok {
		t.Fatalf("expected nested /items endpoint")
	}
	if child.FullURI != "/products/items" {
		t.Errorf("nested FullURI = %q, want /products/items", child.FullURI)
	}
	if _, ok := child.Operations.Get("get"); !ok {
		t.Errorf("expected get on nested endpoint")
	}

	keys := bodyKeys(ep.body)
	for _, forbidden := range []string{FacetType, FacetIs, FacetSecuredBy, "get", "/items"} {
		if containsStr(keys, forbidden) {
			t.Errorf("body must not contain %q, got %v", forbidden, keys)
		}
	}
	for _, want := range []string{"displayName", "uriParameters"} {
		if !containsStr(keys, want) {
			t.Errorf("body missing %q, got %v", want, keys)
		}
	}
}

func TestMakeSourceOperation_CapturesScope(t *testing.T) {
	r := New(context.Background())
	lib := &Library{Location: "lib.raml"}
	want := ParseCtx{AnchorFrag: lib}
	r.pushParseCtx(want)
	defer r.popParseCtx()

	op, err := r.makeSourceOperation("api.raml", scalarKeyNode("get"),
		mappingNodeFromYAML(t, "description: x"))
	if err != nil {
		t.Fatalf("makeSourceOperation: %v", err)
	}
	if op.scope != want {
		t.Errorf("scope = %+v, want %+v", op.scope, want)
	}
}
