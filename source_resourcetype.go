package raml

import (
	"github.com/acronis/go-stacktrace"
	"gopkg.in/yaml.v3"
)

// This file builds a stage-1 SourceEndPoint from a resource-type definition,
// the resource-type analogue of mergeTraitDefIntoSource for traits. It is the
// structural bridge between a parsed ResourceTypeDefinition template and the
// IR-level merge primitives (mergeSourceEndPoint): an applied resource type is
// compiled into its own SourceEndPoint, then merged underneath the target.

// distributeOverlay copies every entry of overlay into the provenance overlay of
// ep and of every operation and nested endpoint beneath it (set-if-absent).
//
// Provenance marks key on node identity, so an entry that falls outside a given
// unit's body subtree is simply never looked up by that unit's stage-2 decode.
// Copying the whole overlay into every unit is therefore safe and avoids having
// to partition the compile overlay by subtree ownership.
func distributeOverlay(ep *SourceEndPoint, overlay provenanceOverlay) {
	if len(overlay) == 0 {
		return
	}
	copyOverlay(ep.provenance, overlay)
	for pair := ep.Operations.Oldest(); pair != nil; pair = pair.Next() {
		copyOverlay(pair.Value.provenance, overlay)
	}
	for pair := ep.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		distributeOverlay(pair.Value, overlay)
	}
}

// compileResourceTypeSource compiles a resource-type definition into a stage-1
// SourceEndPoint, substituting params with provenance tracking.
//
// Per decision D2, static template content keeps the resource type's own
// declaration scope (left unmarked, inheriting the returned endpoint's default
// scope), while subtrees produced by parameter substitution are recorded as
// callerScope in the endpoint's provenance overlays so stage-2 decode resolves
// caller-substituted content in the caller's namespace.
//
// existingOps lists the HTTP methods the target endpoint already declares; it
// drives optional-method ("get?") filtering. uri/parent set the synthetic
// endpoint's URI for symmetry with stage-1 decode — mergeSourceEndPoint ignores
// the source URI, so these are presentational only.
func (r *RAML) compileResourceTypeSource(
	rtDef *ResourceTypeDefinition,
	params map[string]*yaml.Node,
	existingOps map[string]struct{},
	callerScope ParseCtx,
	location, uri, parent string,
) (*SourceEndPoint, *stacktrace.StackTrace) {
	if rtDef.Link != nil {
		// Compile in the fragment's own namespace so that type references inside
		// the fragment body resolve against the fragment's uses:, enforcing
		// self-containment. Mirrors ResourceTypeDefinition.compile.
		return r.compileResourceTypeSource(
			rtDef.Link.ResourceType, params, existingOps, callerScope, rtDef.Link.Location, uri, parent)
	}
	if rtDef.Source == nil {
		return nil, nil
	}

	// Validate user-supplied params – reserved names are always accepted.
	for k := range params {
		if reservedParam(k) {
			continue
		}
		if _, ok := rtDef.DeclaredVariables[k]; !ok {
			return nil, StacktraceNew("unexpected parameter", rtDef.Location,
				WithNodePosition(rtDef.Source), stacktrace.WithInfo("parameter", k))
		}
	}

	// Filter optional methods the target endpoint does not define before
	// substitution, then ensure every required non-reserved variable is supplied.
	source := rtDef.normalizeOptionalMethods(rtDef.Source, existingOps)
	requiredVars := rtDef.collectRequiredVariables(source, 0)
	for k := range requiredVars {
		if reservedParam(k) {
			continue
		}
		if _, ok := params[k]; !ok {
			return nil, StacktraceNew("missing required parameter", rtDef.Location,
				WithNodePosition(rtDef.Source), stacktrace.WithInfo("parameter", k))
		}
	}

	overlay := make(provenanceOverlay)
	compiled := compileSourceProvenance(source, params, 0, rtDef.NodeVariableIndex, callerScope, overlay)

	keyNode := &yaml.Node{
		Kind:   yaml.ScalarNode,
		Tag:    TagStr,
		Value:  uri,
		Line:   compiled.Line,
		Column: compiled.Column,
	}
	rtScope := ParseCtx{AnchorFrag: rtDef.anchorFrag}
	r.pushParseCtx(rtScope)
	ep, err := r.makeSourceEndPoint(keyNode, compiled, location, parent)
	r.popParseCtx()
	if err != nil {
		return nil, StacktraceNewWrapped("make resource type source endpoint", err, rtDef.Location,
			WithNodePosition(rtDef.Source))
	}

	distributeOverlay(ep, overlay)
	return ep, nil
}
