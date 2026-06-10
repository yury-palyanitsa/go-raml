package raml

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// valueOf returns the value node for key in a mapping node, or nil.
func valueOf(m *yaml.Node, key string) *yaml.Node {
	if m == nil {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalarSeq(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		out = append(out, c.Value)
	}
	return out
}

func eqStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestMergeStructural_ProductsExample reproduces the worked example from
// raml-10.md § "Algorithm of Merging Traits and Methods" (the /products
// resource merged with the `collection` resource type).
func TestMergeStructural_ProductsExample(t *testing.T) {
	target := mappingNodeFromYAML(t, `
description: override the description
responses:
  200:
    body:
      application/json:
`) // the method's own branch (higher priority)
	source := mappingNodeFromYAML(t, `
description: a list
headers:
  APIKey:
`) // the resource type branch (lower priority)

	merged := mergeStructural(target, source)

	// Scalar in both -> target (explicit) wins.
	if got := valueOf(merged, "description"); got == nil || got.Value != "override the description" {
		t.Errorf("description = %v, want \"override the description\"", got)
	}
	// Source-only key grafted.
	if valueOf(merged, "headers") == nil {
		t.Errorf("merged missing grafted headers key")
	}
	// Target-only key preserved.
	if valueOf(merged, "responses") == nil {
		t.Errorf("merged missing target-only responses key")
	}
}

// TestMergeStructural_CollectionUnion reproduces the enum-merge example: the
// method enum [mac, unix] merged with the trait enum [win, mac] yields the
// target-first deduplicated union [mac, unix, win].
func TestMergeStructural_CollectionUnion(t *testing.T) {
	target := mappingNodeFromYAML(t, "enum: [ mac, unix ]")
	source := mappingNodeFromYAML(t, "enum: [ win, mac ]")

	merged := mergeStructural(target, source)
	got := scalarSeq(valueOf(merged, "enum"))
	want := []string{"mac", "unix", "win"}
	if !eqStrSlice(got, want) {
		t.Errorf("enum union = %v, want %v", got, want)
	}
}

func TestMergeStructural_ObjectRecurse(t *testing.T) {
	// properties is an object present in both -> recurse: x kept, y grafted.
	target := mappingNodeFromYAML(t, `
properties:
  x:
    type: string
`)
	source := mappingNodeFromYAML(t, `
properties:
  y:
    type: integer
`)
	merged := mergeStructural(target, source)
	props := valueOf(merged, "properties")
	if valueOf(props, "x") == nil || valueOf(props, "y") == nil {
		t.Errorf("expected both x and y after object recurse, got keys %v", bodyKeys(props))
	}
}

func TestMergeStructural_KindMismatchTargetWins(t *testing.T) {
	target := mappingNodeFromYAML(t, "type: string") // scalar value
	source := mappingNodeFromYAML(t, `
type:
  nested: thing
`) // mapping value
	merged := mergeStructural(target, source)
	tv := valueOf(merged, "type")
	if tv == nil || tv.Kind != yaml.ScalarNode || tv.Value != "string" {
		t.Errorf("type = %+v, want scalar \"string\" (explicit target wins on kind mismatch)", tv)
	}
}

func TestMergeStructural_GraftPreservesNodeIdentity(t *testing.T) {
	target := mappingNodeFromYAML(t, "description: own")
	source := mappingNodeFromYAML(t, `
headers:
  APIKey:
`)
	sourceHeaders := valueOf(source, "headers")
	merged := mergeStructural(target, source)
	// The grafted subtree must be the SAME pointer so provenance overlays can
	// key on it.
	if valueOf(merged, "headers") != sourceHeaders {
		t.Errorf("grafted headers node identity not preserved")
	}
}

func TestMergeStructural_NilOperands(t *testing.T) {
	n := mappingNodeFromYAML(t, "a: b")
	if mergeStructural(nil, n) != n {
		t.Errorf("merge(nil, n) should return n")
	}
	if mergeStructural(n, nil) != n {
		t.Errorf("merge(n, nil) should return n")
	}
}
