package raml

import (
	"github.com/acronis/go-stacktrace"
	"gopkg.in/yaml.v3"
)

// This file implements the structural application of traits to a stage-1
// SourceOperation: the spec's § "Algorithm of Merging Traits and Methods"
// realized as a YAML-tree merge with provenance tracking, before any type
// resolution (stage 2 decode materializes the merged tree).
//
// A trait body is compiled (parameter substitution) with compileSourceProvenance
// — which records caller-substituted subtrees as caller-scoped — and then merged
// underneath the operation's own body with mergeStructuralProvenance — which
// records the grafted (trait-contributed) subtrees as trait-declaration-scoped.
// The operation's own body always wins (§2454: the explicit method node has the
// highest priority).

// mergeTraitDefIntoSource compiles a resolved trait definition with params and
// structurally merges the result underneath op's body, populating op.provenance.
//
// Scope semantics (decision D2):
//   - static trait content        -> trait declaration scope (graft mark);
//   - caller-substituted values    -> callerScope (compile mark, more specific,
//     preserved by the set-if-absent graft marking).
//
// params must already include any reserved parameters (resourcePath,
// resourcePathName, methodName) the caller wishes to inject. Linked (!include)
// traits are followed to their target definition. An empty trait (nil source)
// is a no-op.
func (r *RAML) mergeTraitDefIntoSource(
	op *SourceOperation,
	td *TraitDefinition,
	params map[string]*yaml.Node,
	callerScope ParseCtx,
) *stacktrace.StackTrace {
	if td.Link != nil {
		td = td.Link.Trait
	}
	if td == nil || td.Source == nil {
		return nil
	}
	// Validate supplied params against the trait's declared variables, mirroring
	// ResourceTypeDefinition validation: reserved names are always accepted, every
	// non-reserved supplied param must be declared, and every declared variable
	// must be supplied.
	for k := range params {
		if reservedParam(k) {
			continue
		}
		if _, ok := td.DeclaredVariables[k]; !ok {
			return StacktraceNew("unexpected parameter", td.Location,
				WithNodePosition(td.Source), stacktrace.WithInfo("parameter", k))
		}
	}
	for k := range td.DeclaredVariables {
		if reservedParam(k) {
			continue
		}
		if _, ok := params[k]; !ok {
			return StacktraceNew("missing required parameter", td.Location,
				WithNodePosition(td.Source), stacktrace.WithInfo("parameter", k))
		}
	}
	traitScope := ParseCtx{AnchorFrag: td.anchorFrag}
	compiled := compileSourceProvenance(td.Source, params, 0, td.NodeVariableIndex, callerScope, op.provenance)
	op.body = mergeStructuralProvenance(op.body, compiled, traitScope, op.provenance)
	return nil
}

// copyOverlay copies provenance entries from src into dst with set-if-absent
// semantics, so an existing (more-specific) mark in dst is never overwritten.
func copyOverlay(dst, src provenanceOverlay) {
	for node, scope := range src {
		if _, ok := dst[node]; !ok {
			dst[node] = scope
		}
	}
}

// unionTraits appends to target every trait from source whose name is not
// already present, preserving target ordering and closest-wins precedence.
func unionTraits(target, source []*Trait) []*Trait {
	if len(source) == 0 {
		return target
	}
	seen := make(map[string]struct{}, len(target)+len(source))
	for _, tr := range target {
		seen[tr.Name] = struct{}{}
	}
	for _, tr := range source {
		if _, ok := seen[tr.Name]; ok {
			continue
		}
		seen[tr.Name] = struct{}{}
		target = append(target, tr)
	}
	return target
}

// mergeSourceOperation merges a lower-priority source operation underneath a
// higher-priority target operation (§2454: the target — the more specific
// method declaration — wins). sourceScope is the default lexical scope for
// source structural nodes that carry no more-specific mark in source.provenance.
//
// Source provenance marks are carried into the target overlay (set-if-absent)
// before the body merge so caller-substituted nodes keep their caller scope,
// then structural grafts are recorded as sourceScope.
func mergeSourceOperation(target, source *SourceOperation, sourceScope ParseCtx) {
	copyOverlay(target.provenance, source.provenance)
	target.body = mergeStructuralProvenance(target.body, source.body, sourceScope, target.provenance)
	target.Traits = unionTraits(target.Traits, source.Traits)
	if !target.explicitSecuredBy && source.explicitSecuredBy {
		target.SecuredBy = source.SecuredBy
		target.explicitSecuredBy = true
	}
}

// graftSourceOperation adopts a whole source operation that the target endpoint
// does not declare, setting its default scope to sourceScope so its unmarked
// (template-static) content resolves at the contributing template's namespace.
// Its own provenance overlay (caller-substituted marks) is preserved.
func graftSourceOperation(target *SourceEndPoint, op *SourceOperation, sourceScope ParseCtx) {
	op.scope = sourceScope
	target.Operations.Set(op.Method, op)
}

// mergeSourceEndPoint merges a lower-priority source endpoint (e.g. a compiled
// resource type) underneath a higher-priority target endpoint (§2454: the
// target resource declaration wins). Endpoint-level facets merge structurally;
// operations present in both merge by body; operations only in source are
// grafted; the source's parent resource type (type:) and traits/securedBy fill
// target slots that are empty.
func mergeSourceEndPoint(target, source *SourceEndPoint, sourceScope ParseCtx) {
	copyOverlay(target.provenance, source.provenance)
	target.body = mergeStructuralProvenance(target.body, source.body, sourceScope, target.provenance)
	target.Traits = unionTraits(target.Traits, source.Traits)
	if !target.explicitSecuredBy && source.explicitSecuredBy {
		target.SecuredBy = source.SecuredBy
		target.explicitSecuredBy = true
	}
	if target.ResourceType == nil {
		target.ResourceType = source.ResourceType
	}
	for pair := source.Operations.Oldest(); pair != nil; pair = pair.Next() {
		if existing, ok := target.Operations.Get(pair.Key); ok {
			mergeSourceOperation(existing, pair.Value, sourceScope)
		} else {
			graftSourceOperation(target, pair.Value, sourceScope)
		}
	}
}
