package raml

import "gopkg.in/yaml.v3"

// This file implements the structural merge defined by the RAML 1.0 spec
// § "Algorithm of Merging Traits and Methods". The merge operates purely on
// YAML declaration trees — no type resolution, no shapes — exactly as the spec
// describes. It is the core of stage 2's structural phase: a trait/resource-type
// branch (source, lower priority) is merged underneath a method/resource branch
// (target, higher priority).
//
// Spec rules (applied to two mapping nodes):
//  1. Properties defined only in target remain unchanged.
//  2. Target receives properties defined only in source.
//  3. Properties defined in both:
//       * scalar     -> target's own value wins (explicit node wins).
//       * collection -> merged by value (union, target-first, deduplicated).
//       * object     -> recurse rules 1-3.
//
// Grafted source subtrees are reused by pointer (not deep-copied), so a later
// stage can record their distinct lexical scope in a provenance overlay keyed by
// node identity.

// mergeStructural merges a source declaration node underneath a higher-priority
// target node, returning the merged node per spec § "Algorithm of Merging Traits
// and Methods". Neither input is mutated; the result reuses child node pointers
// from both inputs to preserve identity for provenance tracking.
func mergeStructural(target, source *yaml.Node) *yaml.Node {
	return mergeStructuralProvenance(target, source, ParseCtx{}, nil)
}

// mergeStructuralProvenance is mergeStructural with provenance recording. Every
// source-only subtree it grafts into the result (rule 2 for mappings, union
// append for sequences) is recorded in overlay as belonging to sourceScope —
// the lexical scope of the trait/resource-type the source branch came from — so
// that stage-2 decode resolves grafted (e.g. trait-contributed) content against
// the template's own namespace rather than the application site.
//
// Marks are set-if-absent: a node already in overlay (e.g. a caller-substituted
// node marked by compileSourceProvenance) keeps its existing, more-specific
// scope. When overlay is nil no marks are recorded and the function behaves
// exactly like the pure structural merge.
func mergeStructuralProvenance(target, source *yaml.Node, sourceScope ParseCtx, overlay provenanceOverlay) *yaml.Node {
	switch {
	case target == nil:
		if source != nil {
			markGraft(overlay, source, sourceScope)
		}
		return source
	case source == nil:
		return target
	}
	// Kind disagreement: the explicit (target) node wins unchanged.
	if target.Kind != source.Kind {
		return target
	}
	switch target.Kind {
	case yaml.MappingNode:
		return mergeMappingNodes(target, source, sourceScope, overlay)
	case yaml.SequenceNode:
		return mergeSequenceNodes(target, source, sourceScope, overlay)
	default:
		// Scalar / alias: target's own value remains unchanged.
		return target
	}
}

// markGraft records the grafted subtree rooted at node as belonging to scope
// in overlay. The whole subtree is marked, not just its root, so that
// stage-2 decoders looking up the scope of a shape buried inside the graft
// (e.g. a response body's type facet sitting several levels below the
// per-response-code graft root) still find the trait/RT declaration scope.
//
// A node already in overlay defines its OWN scope domain (e.g. a complex
// parameter that the caller supplied: compileSourceProvenance marked its
// root with callerScope so the whole spliced subtree must resolve in the
// caller's namespace). The walk stops there — it neither overwrites the
// root mark nor descends into its descendants — so caller-scoped content
// inside a graft retains the caller's namespace.
func markGraft(overlay provenanceOverlay, node *yaml.Node, scope ParseCtx) {
	if overlay == nil || node == nil {
		return
	}
	if _, ok := overlay[node]; ok {
		return
	}
	overlay[node] = scope
	for _, child := range node.Content {
		markGraft(overlay, child, scope)
	}
}

// isOpaqueDataFacet reports whether a mapping key holds an opaque user-data
// value (an example instance or a default value) rather than a RAML structural
// object. The spec's "object -> recurse" merge rule applies to RAML declaration
// structure (body, properties, responses, …), not to arbitrary data: a method's
// own example replaces the template's example wholesale instead of being merged
// key-by-key. Once such a key is reached the merge stops descending, so a literal
// "example" key inside example data is never misinterpreted.
func isOpaqueDataFacet(key string) bool {
	switch key {
	case FacetExample, FacetExamples, FacetDefault:
		return true
	default:
		return false
	}
}

// mergeMappingNodes implements rules 1-3 for two mapping nodes. Target keys win;
// source-only keys are grafted; keys present in both recurse.
func mergeMappingNodes(target, source *yaml.Node, sourceScope ParseCtx, overlay provenanceOverlay) *yaml.Node {
	merged := &yaml.Node{
		Kind:   yaml.MappingNode,
		Tag:    target.Tag,
		Line:   target.Line,
		Column: target.Column,
	}
	// Index source key -> value node for rule-3 lookup and rule-2 detection.
	sourceVal := make(map[string]*yaml.Node, len(source.Content)/2)
	for i := 0; i+1 < len(source.Content); i += 2 {
		sourceVal[source.Content[i].Value] = source.Content[i+1]
	}

	// Pass 1: walk target keys in order. Rule 1 (target-only) keeps the value;
	// rule 3 (in both) recurses, except for opaque data facets where the target
	// value wins wholesale.
	targetKeys := make(map[string]struct{}, len(target.Content)/2)
	for i := 0; i+1 < len(target.Content); i += 2 {
		k := target.Content[i]
		v := target.Content[i+1]
		targetKeys[k.Value] = struct{}{}
		if sv, ok := sourceVal[k.Value]; ok && !isOpaqueDataFacet(k.Value) {
			v = mergeStructuralProvenance(v, sv, sourceScope, overlay)
		}
		merged.Content = append(merged.Content, k, v)
	}

	// Pass 2: graft source-only keys (rule 2), preserving source ordering and
	// node identity. The grafted value subtree belongs to the source scope.
	for i := 0; i+1 < len(source.Content); i += 2 {
		k := source.Content[i]
		v := source.Content[i+1]
		if _, ok := targetKeys[k.Value]; ok {
			continue
		}
		markGraft(overlay, v, sourceScope)
		merged.Content = append(merged.Content, k, v)
	}
	return merged
}

// mergeSequenceNodes implements the "collection merged by value" rule: the
// resulting sequence is the union of target and source items, target items
// first, with duplicates (by structural value equality) removed.
func mergeSequenceNodes(target, source *yaml.Node, sourceScope ParseCtx, overlay provenanceOverlay) *yaml.Node {
	merged := &yaml.Node{
		Kind:   yaml.SequenceNode,
		Tag:    target.Tag,
		Line:   target.Line,
		Column: target.Column,
	}
	merged.Content = append(merged.Content, target.Content...)
	for _, sv := range source.Content {
		dup := false
		for _, tv := range merged.Content {
			if nodeValueEqual(tv, sv) {
				dup = true
				break
			}
		}
		if !dup {
			markGraft(overlay, sv, sourceScope)
			merged.Content = append(merged.Content, sv)
		}
	}
	return merged
}

// nodeValueEqual reports whether two YAML nodes have the same structural value,
// used to deduplicate merged collection items. Scalars compare by tag and value;
// sequences compare element-wise in order; mappings compare key/value pairs in
// order. Comments, positions and anchors are ignored.
func nodeValueEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case yaml.ScalarNode:
		return a.Tag == b.Tag && a.Value == b.Value
	case yaml.SequenceNode, yaml.MappingNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !nodeValueEqual(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	default:
		return a.Tag == b.Tag && a.Value == b.Value
	}
}
