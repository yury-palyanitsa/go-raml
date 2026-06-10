package raml

import (
	"strings"

	"github.com/acronis/go-stacktrace"
	"gopkg.in/yaml.v3"
)

// This file implements the two-stage build driver: it takes the stage-1
// SourceEndPoint IR produced during decode, applies resource-type and trait
// directives by structural merge (per RAML 1.0 § "Algorithm of Merging Traits
// and Resource Types"), and materializes the merged IR into real EndPoint trees
// in a single stage-2 decode pass — replacing the legacy eager-decode +
// applyResourceTypes/applyTraits/Operation.merge pipeline.
//
// Ordering guarantees (§ Phase 1, closest-wins):
//   - resource types: a resource's own declarations win over the resource type;
//     a resource type's own declarations win over its parent resource type.
//   - traits applied to an operation, in priority order:
//       1. method's own traits      (SourceOperation.Traits)
//       2. resource's own traits     (SourceEndPoint.Traits)
//       3. resource type method traits (SourceOperation.RTTraits)
//       4. resource type resource traits (SourceEndPoint.RTTraits)
//     Resource-type-contributed traits are routed into the RTTraits slices by
//     the resource-type merge so this interleaving is preserved.

// scalarStrNode builds a plain string scalar YAML node for a template parameter
// value.
func scalarStrNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: TagStr, Value: v}
}

// buildEndPoints resolves directives on every stage-1 endpoint and materializes
// them into the API's EndPoint tree. Errors are accumulated so a single broken
// endpoint does not discard the rest (partial-result tolerance).
func (r *RAML) buildEndPoints(api *APIFragment) *stacktrace.StackTrace {
	var acc stacktrace.Accumulator
	// First resolve directives (resource types, traits) across the whole tree so
	// that all template-contributed shapes exist before the single decode pass.
	for _, sep := range api.sourceEndPoints {
		acc.Add(r.resolveSourceEndPoint(api, sep))
	}
	for _, sep := range api.sourceEndPoints {
		endPoint, st := r.decodeSourceEndPoint(sep)
		acc.Add(st)
		api.EndPoints.Set(endPoint.URI, endPoint)
	}
	return acc.Result()
}

// resolveSourceEndPoint applies the resource-type chain and traits to a stage-1
// endpoint and recurses into its nested endpoints. It mutates the IR in place;
// no shapes are created here (that happens in the later decode pass).
func (r *RAML) resolveSourceEndPoint(api *APIFragment, sep *SourceEndPoint) *stacktrace.StackTrace {
	var acc stacktrace.Accumulator
	if sep.ResourceType != nil {
		acc.Add(r.applyResourceTypeToSource(api, sep, sep.ResourceType, make(map[string]struct{})))
	}
	acc.Add(r.applyTraitsToSource(api, sep))
	for pair := sep.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		acc.Add(r.resolveSourceEndPoint(api, pair.Value))
	}
	return acc.Result()
}

// applyResourceTypeToSource resolves a resource type by name, compiles it into a
// stage-1 endpoint and merges it underneath sep. The parent resource type (the
// compiled endpoint's own type:) is applied first so the merge order matches the
// spec (closest declaration wins). visited prevents infinite recursion.
func (r *RAML) applyResourceTypeToSource(
	api *APIFragment, sep *SourceEndPoint, rt *ResourceType, visited map[string]struct{},
) *stacktrace.StackTrace {
	if rt == nil {
		return nil
	}
	if _, ok := visited[rt.Name]; ok {
		return nil
	}
	visited[rt.Name] = struct{}{}

	rtDef, err := r.resolveResourceTypeDefinition(api, rt)
	if err != nil {
		return StacktraceNewWrapped("get resource type definition", err, rt.Location,
			stacktrace.WithPosition(&rt.ValuePos), stacktrace.WithType(StacktraceTypeUnwrapping))
	}
	rt.Definition = rtDef

	params := make(map[string]*yaml.Node, len(rt.Params)+2)
	for k, v := range rt.Params {
		params[k] = scalarStrNode(v)
	}
	params[ParamResourcePath] = scalarStrNode(sep.FullURI)
	params[ParamResourcePathName] = scalarStrNode(resourcePathName(sep.FullURI))

	existingOps := make(map[string]struct{}, sep.Operations.Len())
	for pair := sep.Operations.Oldest(); pair != nil; pair = pair.Next() {
		existingOps[pair.Key] = struct{}{}
	}

	// Caller scope = the endpoint's own declaration scope, so parameter values
	// (e.g. unqualified type names) resolve against the applying document.
	callerScope := sep.scope
	parent := strings.TrimSuffix(sep.FullURI, sep.URI)
	compiled, st := r.compileResourceTypeSource(rtDef, params, existingOps, callerScope, sep.Location, sep.URI, parent)
	if st != nil {
		return StacktraceNewWrapped("compile resource type", st, rt.Location,
			stacktrace.WithPosition(&rt.ValuePos), stacktrace.WithType(StacktraceTypeUnwrapping))
	}
	if compiled == nil {
		return nil
	}

	// Apply the parent resource type (type:) to the compiled endpoint first.
	if compiled.ResourceType != nil {
		if st := r.applyResourceTypeToSource(api, compiled, compiled.ResourceType, visited); st != nil {
			return st
		}
	}

	mergeResourceTypeIntoSource(sep, compiled)
	return nil
}

// applyTraitsToSource applies traits to each operation of sep in spec priority
// order (method → resource → RT-method → RT-resource), deduplicating by name
// (closest occurrence wins). Reserved parameters are injected per the spec.
func (r *RAML) applyTraitsToSource(api *APIFragment, sep *SourceEndPoint) *stacktrace.StackTrace {
	var acc stacktrace.Accumulator
	// The endpoint-level reserved parameters are constant across every operation
	// and trait of this resource; build their scalar nodes once. They are always
	// read-only scalars in compileSourceProvenance (never inserted by pointer or
	// marked in the overlay), so sharing the same node across applications is safe.
	callerScope := sep.scope
	resourcePathNode := scalarStrNode(sep.FullURI)
	resourcePathNameNode := scalarStrNode(resourcePathName(sep.FullURI))
	for pair := sep.Operations.Oldest(); pair != nil; pair = pair.Next() {
		method := pair.Key
		op := pair.Value

		total := len(op.Traits) + len(sep.Traits) + len(op.RTTraits) + len(sep.RTTraits)
		allTraits := make([]*Trait, 0, total)
		allTraits = append(allTraits, op.Traits...)    // 1. method own
		allTraits = append(allTraits, sep.Traits...)   // 2. resource own
		allTraits = append(allTraits, op.RTTraits...)  // 3. RT method
		allTraits = append(allTraits, sep.RTTraits...) // 4. RT resource

		// methodName is constant for every trait applied to this operation.
		methodNode := scalarStrNode(method)
		seen := make(map[string]struct{}, len(allTraits))
		for _, trait := range allTraits {
			if _, ok := seen[trait.Name]; ok {
				continue
			}
			seen[trait.Name] = struct{}{}

			traitDef, err := r.resolveTraitDefinition(api, trait)
			if err != nil {
				acc.Add(StacktraceNewWrapped("get trait definition", err, trait.Location,
					stacktrace.WithPosition(&trait.ValuePos), stacktrace.WithType(StacktraceTypeUnwrapping)))
				continue
			}
			trait.Definition = traitDef

			params := make(map[string]*yaml.Node, len(trait.Params)+3)
			for k, v := range trait.Params {
				params[k] = v
			}
			params[ParamResourcePath] = resourcePathNode
			params[ParamResourcePathName] = resourcePathNameNode
			params[ParamMethodName] = methodNode

			if st := r.mergeTraitDefIntoSource(op, traitDef, params, callerScope); st != nil {
				acc.Add(StacktraceNewWrapped("apply trait", st, trait.Location,
					stacktrace.WithPosition(&trait.ValuePos), stacktrace.WithType(StacktraceTypeUnwrapping)))
			}
		}
	}
	return acc.Result()
}

// mergeSourceOperationBody merges a lower-priority source operation's body
// underneath target (target wins) without touching the trait slices. Used by the
// resource-type merge, which routes resource-type traits into RTTraits.
func mergeSourceOperationBody(target, source *SourceOperation, sourceScope ParseCtx) {
	copyOverlay(target.provenance, source.provenance)
	target.body = mergeStructuralProvenance(target.body, source.body, sourceScope, target.provenance)
	if !target.explicitSecuredBy && source.explicitSecuredBy {
		target.SecuredBy = source.SecuredBy
		target.explicitSecuredBy = true
	}
}

// mergeResourceTypeIntoSource merges a compiled resource-type endpoint (source)
// underneath the applying endpoint (target). The target's own declarations win.
// Resource-type-contributed traits are routed into the RTTraits slices so that
// applyTraitsToSource can order them after the resource's own traits.
//
// The compiled endpoint already carries its own resolution scope (the resource
// type's declaration namespace, or the fragment's own namespace when the type is
// an !include), so static resource-type content is grafted under source.scope and
// each operation keeps the scope it was compiled under — enforcing fragment
// self-containment rather than re-anchoring against the applying document.
func mergeResourceTypeIntoSource(target, source *SourceEndPoint) {
	copyOverlay(target.provenance, source.provenance)
	target.body = mergeStructuralProvenance(target.body, source.body, source.scope, target.provenance)
	// Endpoint-level resource-type traits become RT-resource traits.
	target.RTTraits = append(target.RTTraits, source.Traits...)
	target.SecuredBy = append(target.SecuredBy, source.SecuredBy...)
	if target.ResourceType == nil {
		target.ResourceType = source.ResourceType
	}
	for pair := source.Operations.Oldest(); pair != nil; pair = pair.Next() {
		method := pair.Key
		srcOp := pair.Value
		if existing, ok := target.Operations.Get(method); ok {
			// The resource type's method traits become this operation's RT-method
			// traits; only its body merges structurally.
			existing.RTTraits = append(existing.RTTraits, srcOp.Traits...)
			mergeSourceOperationBody(existing, srcOp, srcOp.scope)
		} else {
			// A method contributed entirely by the resource type: its own traits
			// are RT-method traits (it has no "method-own" declarations here). It
			// keeps the scope it was compiled under.
			srcOp.RTTraits = append(srcOp.RTTraits, srcOp.Traits...)
			srcOp.Traits = nil
			target.Operations.Set(method, srcOp)
		}
	}
}
