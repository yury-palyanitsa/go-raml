package server

import (
	"fmt"
	"slices"
	"strings"

	raml "github.com/acronis/go-raml/v3"
	protocol "github.com/tliron/glsp/protocol_3_16"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// ContextKind identifies the structural role of a RAML model element at the cursor.
// The constants map to the RAML domain-location taxonomy (raml.DomainLocation):
//
//	CKRoot           → (fragment root; no raml.DomainLocation equivalent)
//	CKType           → raml.TypeDeclarationDomain  (also used for annotationType bodies)
//	CKProperty       → raml.TypeDeclarationDomain  (sub-case: property within an object shape)
//	CKEndpoint       → raml.ResourceDomain
//	CKOperation      → raml.MethodDomain
//	CKTrait          → raml.TraitDomain
//	CKResponse       → raml.ResponseDomain
//	CKSecurityScheme → raml.SecuritySchemeDomain
type ContextKind uint8

const (
	CKRoot           ContextKind = iota // fragment root (API, Library, DataType)
	CKType                              // inside a type / annotationType definition block
	CKProperty                          // inside a property (properties:, queryParameters:, etc.)
	CKEndpoint                          // inside an endpoint (/path:) block
	CKOperation                         // inside a method (get:, post:, …) block
	CKTrait                             // inside a trait definition block
	CKResponse                          // inside a response definition (responses: 200:)
	CKSecurityScheme                    // inside a security scheme definition block
)

// ContextEntry records the line range and kind for one model element.
type ContextEntry struct {
	StartLine, EndLine int
	Kind               ContextKind
	ShapeType          string // for CKType/CKProperty: the resolved RAML type (e.g. "object", "string")
	ShapeName          string // for CKType: the declared type name, used to look up custom facet definitions
}

// ContextIndex maps cursor lines to the innermost RAML structural context.
type ContextIndex struct {
	entries        []ContextEntry   // sorted by StartLine
	usesEntryLines map[int]struct{} // 1-based RAML line → present if line holds a uses: entry key
	// sm is the per-fragment SourceMap built from the retained YAML AST and
	// annotated by the RAML model traversal.  It is the authoritative source
	// for per-line syntactic and semantic context.  Nil when OptWithRawSource
	// was not set or when the YAML document could not be decoded at all.
	sm *SourceMap
}

// DebugString returns a human-readable dump of all entries; for test diagnostics.
func (ci *ContextIndex) DebugString() string {
	if ci == nil {
		return "<nil>"
	}
	var sb strings.Builder
	for _, e := range ci.entries {
		fmt.Fprintf(&sb, "  [%d-%d] kind=%d type=%q name=%q\n", e.StartLine, e.EndLine, e.Kind, e.ShapeType, e.ShapeName)
	}
	return sb.String()
}

// IsUsesEntryLine reports whether the given 1-based RAML line holds a uses:
// entry key (e.g. the "myLib" in "  myLib: lib.raml").  When the cursor is on
// the value side of such a line, file-path completion should be offered instead
// of type-expression completion.
func (ci *ContextIndex) IsUsesEntryLine(line int) bool {
	if ci == nil {
		return false
	}
	_, ok := ci.usesEntryLines[line]
	return ok
}

// GetLineRecord returns the SourceMap LineRecord for the given 1-based RAML
// line, or nil when no record exists (blank lines, comment lines, or when
// OptWithRawSource was not enabled for this parse).
func (ci *ContextIndex) GetLineRecord(line int) *LineRecord {
	if ci == nil {
		return nil
	}
	return ci.sm.Get(line)
}

// LastBefore returns the entry with the highest StartLine that is strictly less
// than line. This covers trailing-blank-line situations where the YAML node
// range (EndLine) does not include lines added after the last content line.
// Returns nil when no entry precedes line.
func (ci *ContextIndex) LastBefore(line int) *ContextEntry {
	if ci == nil || len(ci.entries) == 0 {
		return nil
	}
	idx, _ := slices.BinarySearchFunc(ci.entries, line, func(e ContextEntry, l int) int {
		return e.StartLine - l
	})
	if idx == 0 {
		return nil
	}
	return &ci.entries[idx-1]
}

// EntryAt returns the entry whose StartLine exactly equals line, or nil if none.
// This identifies the structural block whose key declaration is at line — e.g.
// an endpoint (`/path:`) or operation (`get:`) that starts at the cursor line.
func (ci *ContextIndex) EntryAt(line int) *ContextEntry {
	if ci == nil {
		return nil
	}
	idx, found := slices.BinarySearchFunc(ci.entries, line, func(e ContextEntry, l int) int {
		return e.StartLine - l
	})
	if !found {
		return nil
	}
	return &ci.entries[idx]
}

// Query returns the innermost ContextEntry whose range strictly contains line
// (1-based). "Strictly" means StartLine < line, so being on the key definition
// line itself does not count as being inside the block.
// Returns nil when no entry matches (caller treats as CKRoot).
func (ci *ContextIndex) Query(line int) *ContextEntry {
	if ci == nil {
		return nil
	}
	// Binary-search for the boundary: all entries before idx have StartLine < line.
	idx, _ := slices.BinarySearchFunc(ci.entries, line, func(e ContextEntry, l int) int {
		return e.StartLine - l
	})
	// Scan backward (decreasing StartLine = decreasing nesting depth) to find the
	// innermost entry whose range actually contains line.
	for i := idx - 1; i >= 0; i-- {
		e := &ci.entries[i]
		if e.EndLine == 0 || line <= e.EndLine {
			return e
		}
	}
	return nil
}

// BuildAllIndexes constructs the PositionMap, ReferenceIndex, and ContextIndex
// for every file touched by r in a single model traversal, replacing the
// earlier combination of BuildPositionMaps + BuildReferenceIndex +
// BuildContextIndexes. Each model node (shape, endpoint, trait, security scheme,
// uses: entry) is now visited once instead of once per index type.
//
// shapes must be r.GetShapes() — all allocated shapes including anonymous ones.
// locs is the pre-computed reachable fragment set from allFragmentLocations.
func BuildAllIndexes(
	r *raml.RAML,
	shapes []*raml.BaseShape,
	locs map[string]struct{},
) (map[string]*PositionMap, *ReferenceIndex, map[string]*ContextIndex, []protocol.DocumentSymbol) {
	si := r.SourceInfo() // nil when OptWithSourceInfo was not passed; all callers below are nil-safe
	posMaps := make(map[string]*PositionMap)
	ctxIndexes := make(map[string]*ContextIndex)
	refIndex := &ReferenceIndex{refs: make(map[int64][]protocol.Location)}

	getPM := func(loc string) *PositionMap {
		if posMaps[loc] == nil {
			posMaps[loc] = newPositionMap()
		}
		return posMaps[loc]
	}
	getCI := func(loc string) *ContextIndex {
		if ctxIndexes[loc] == nil {
			ci := &ContextIndex{}
			// Phase 1: build SourceMap from the retained YAML AST.
			if node := r.GetSourceNode(loc); node != nil {
				ci.sm = newSourceMap()
				buildSourceMapFromYAML(node, ci.sm)
				// Phase 1.5: annotate all recognisable structural blocks from the
				// YAML AST alone (security schemes, type bodies, endpoints, traits,
				// root API keys). Runs before the model walk so that partial/error
				// documents still get accurate completion context.
				annotateYAMLStructure(node, ci.sm)
			}
			ctxIndexes[loc] = ci
		}
		return ctxIndexes[loc]
	}

	// ── 1. Shape traversal ────────────────────────────────────────────────────
	// Each shape is visited once for: posmap entries (walkShape), TypeExprRef
	// reference indexing (refIndex), facet key lines and type value lines
	// (ContextIndex). The visited guard in walkShape prevents duplicate posmap
	// entries when a child shape is encountered both directly and via recursion.
	visitedShapes := make(map[int64]struct{})
	for _, base := range shapes {
		if base == nil {
			continue
		}
		pm := getPM(base.Location)
		ci := getCI(base.Location)

		// Posmap: definition + TypeExprRef entries (recursive, visited-guarded).
		walkShape(base, pm, visitedShapes)

		// RefIndex: TypeExprRef targets.
		for _, ref := range base.TypeExprRefs {
			switch {
			case ref.Resolved != nil:
				l := protocol.Location{
					URI:   base.Location,
					Range: RamlPosToRangeNamed(ref.Line, ref.Column, len(ref.Resolved.Name)),
				}
				refIndex.refs[ref.Resolved.ID] = append(refIndex.refs[ref.Resolved.ID], l)
			case ref.LibraryLink != nil:
				l := protocol.Location{
					URI:   base.Location,
					Range: RamlPosToRangeNamed(ref.Line, ref.Column, len(ref.LibraryAlias)),
				}
				refIndex.refs[ref.LibraryLink.ID] = append(refIndex.refs[ref.LibraryLink.ID], l)
			}
		}

		// Phase 2: SourceInfo-driven sub-key scan — annotates each facet key
		// line directly from the entity's *yaml.Node content, covering both
		// block-style and flow-style shapes without relying on model sub-field
		// positions. Degrades gracefully to a no-op when si is nil.
		annotateShapeSubkeys(ci.sm, si.Get(base.ID))
	}

	// ── 2. Per-fragment traversal ─────────────────────────────────────────────
	// One loop over all reachable fragments: named-type range entries, uses:
	// entries, and the endpoint / trait / security-scheme structural blocks
	// (populating all three indexes simultaneously).
	// Document symbols for the entry-point fragment are built in the same pass
	// using the already-fetched frag, avoiding a second r.GetFragment call.
	var entryLoc string
	if ep := r.EntryPoint(); ep != nil {
		entryLoc = ep.GetLocation()
	}
	var symbols []protocol.DocumentSymbol

	for loc := range locs {
		ci := getCI(loc)
		pm := getPM(loc)
		frag := r.GetFragment(loc)

		// Named type range entries (CKType / CKProperty).
		for _, shape := range r.GetTypeDefinitionsFromFragment(loc) {
			addShapeEntries(ci, shape)
		}
		for _, shape := range r.GetAnnotationTypeDefinitionsFromFragment(loc) {
			addShapeEntries(ci, shape)
		}

		// uses: entries for completion (CI) and file-path navigation (PM).
		addUsesEntryLines(ci, frag)
		addUsesValueEntries(r, loc, pm)

		switch f := frag.(type) {
		case *raml.APIFragment:
			// Root-level API scalar facets: title:, version:, mediaType:, etc.
			// These are not covered by shape traversal; annotate structurally.
			annotateRootKeys(ci.sm, rootAPIBlockRoles)
			walkEndpointsUnified(f.EndPoints, r, getCI, getPM, refIndex)
			// API-level securedBy: posmap usage entries + refIndex.
			for _, ss := range f.SecuredBy {
				if ss != nil {
					addSecSchemeUsageEntry(getPM(ss.Location), r, ss)
					indexSecSchemeRef(ss, refIndex)
				}
			}
			walkTraitsAndRTsUnified(f.Traits, f.ResourceTypes, ci, pm)
			walkSecuritySchemesUnified(f.SecuritySchemes, ci, pm, si)
			if loc == entryLoc {
				symbols = apifragSymbols(f)
			}
		case *raml.Library:
			walkTraitsAndRTsUnified(f.Traits, f.ResourceTypes, ci, pm)
			walkSecuritySchemesUnified(f.SecuritySchemes, ci, pm, si)
			if loc == entryLoc {
				symbols = librarySymbols(f)
			}
		default:
			if loc == entryLoc {
				types := r.GetFragmentTypePtrs(loc)
				for _, base := range types {
					if base != nil {
						symbols = append(symbols, shapeToSymbol(base))
					}
				}
			}
		}
	}

	// ── 3. Global traversals (no per-file context needed) ─────────────────────
	addAnnotationEntries(r, getPM)

	// ── 4. Sort context index entries by StartLine ────────────────────────────
	for _, ci := range ctxIndexes {
		slices.SortFunc(ci.entries, func(a, b ContextEntry) int {
			return a.StartLine - b.StartLine
		})
	}

	return posMaps, refIndex, ctxIndexes, symbols
}

// walkEndpointsUnified recurses the endpoint tree and populates all three
// indexes in a single pass, replacing the separate walkEndpointMap (CI),
// walkEndpointTraitEntries + walkEndpointSecSchemeEntries (PM), and
// walkEndpointRefs (RI) walkers.
func walkEndpointsUnified(
	eps *orderedmap.OrderedMap[string, *raml.EndPoint],
	r *raml.RAML,
	getCI func(string) *ContextIndex,
	getPM func(string) *PositionMap,
	refIndex *ReferenceIndex,
) {
	for pair := eps.Oldest(); pair != nil; pair = pair.Next() {
		ep := pair.Value
		if ep == nil {
			continue
		}
		ci := getCI(ep.Location)
		pm := getPM(ep.Location)

		// ContextIndex: CKEndpoint range entry + structural block annotation.
		if ep.ValuePos.EndLine > 0 {
			ci.entries = append(ci.entries, ContextEntry{
				StartLine: ep.KeyPos.Line,
				EndLine:   ep.ValuePos.EndLine,
				Kind:      CKEndpoint,
			})
			// Structural annotation: annotate well-known endpoint keys (is:,
			// securedBy:, type:, etc.) even when the model partially failed.
			annotateBlockKeys(ci.sm, ep.KeyPos.Line+1, ep.ValuePos.EndLine, endpointBlockRoles)
		}

		// Posmap + RefIndex: endpoint-level trait / resource-type / securedBy.
		for _, trait := range ep.Traits {
			addTraitUsageEntry(pm, r, trait)
			indexTraitRef(trait, refIndex)
		}
		addRTUsageEntry(pm, r, ep.ResourceType)
		indexRTRef(ep.ResourceType, refIndex)
		for _, ss := range ep.SecuredBy {
			addSecSchemeUsageEntry(pm, r, ss)
			indexSecSchemeRef(ss, refIndex)
		}

		// Operations.
		for opPair := ep.Operations.Oldest(); opPair != nil; opPair = opPair.Next() {
			op := opPair.Value
			if op == nil {
				continue
			}
			opCI := getCI(op.Location)
			opPM := getPM(op.Location)

			// ContextIndex: CKOperation entry + structural block annotation.
			if op.ValuePos.EndLine > 0 {
				opCI.entries = append(opCI.entries, ContextEntry{
					StartLine: op.KeyPos.Line,
					EndLine:   op.ValuePos.EndLine,
					Kind:      CKOperation,
				})
				annotateBlockKeys(opCI.sm, op.KeyPos.Line+1, op.ValuePos.EndLine, operationBlockRoles)
			}

			// ContextIndex: CKResponse entries.
			for respPair := op.Responses.Oldest(); respPair != nil; respPair = respPair.Next() {
				resp := respPair.Value
				if resp != nil && resp.Line > 0 {
					opCI.entries = append(opCI.entries, ContextEntry{
						StartLine: resp.Line,
						EndLine:   resp.EndLine,
						Kind:      CKResponse,
					})
				}
			}

			// Posmap + RefIndex: operation-level traits and securedBy.
			for _, trait := range op.Traits {
				addTraitUsageEntry(opPM, r, trait)
				indexTraitRef(trait, refIndex)
			}
			for _, ss := range op.SecuredBy {
				addSecSchemeUsageEntry(opPM, r, ss)
				indexSecSchemeRef(ss, refIndex)
			}
		}

		// Recurse into nested endpoints.
		walkEndpointsUnified(ep.EndPoints, r, getCI, getPM, refIndex)
	}
}

// walkTraitsAndRTsUnified populates both ContextIndex (CKTrait entries) and
// PositionMap (trait and resource-type definition entries) in a single
// iteration, replacing walkTraitMap (CI) and the definition section of
// addTraitAndRTEntries (PM).
func walkTraitsAndRTsUnified(
	traits *orderedmap.OrderedMap[string, *raml.TraitDefinition],
	rts *orderedmap.OrderedMap[string, *raml.ResourceTypeDefinition],
	ci *ContextIndex,
	pm *PositionMap,
) {
	for pair := traits.Oldest(); pair != nil; pair = pair.Next() {
		td := pair.Value
		if td == nil {
			continue
		}
		if td.ValuePos.EndLine > 0 {
			ci.entries = append(ci.entries, ContextEntry{
				StartLine: td.KeyPos.Line,
				EndLine:   td.ValuePos.EndLine,
				Kind:      CKTrait,
			})
			annotateBlockKeys(ci.sm, td.KeyPos.Line+1, td.ValuePos.EndLine, traitBlockRoles)
		}
		if td.KeyPos.Line > 0 {
			pm.add(PosEntry{
				Line:     td.KeyPos.Line,
				Col:      td.KeyPos.Column,
				TraitDef: td,
			})
		}
	}
	for pair := rts.Oldest(); pair != nil; pair = pair.Next() {
		rtd := pair.Value
		if rtd == nil || rtd.KeyPos.Line == 0 {
			continue
		}
		pm.add(PosEntry{
			Line:  rtd.KeyPos.Line,
			Col:   rtd.KeyPos.Column,
			RTDef: rtd,
		})
	}
}

// walkSecuritySchemesUnified populates both ContextIndex (CKSecurityScheme
// entries) and PositionMap (definition entries) in a single pass, replacing
// walkSecuritySchemeMap (CI) and the definition section of
// addSecuritySchemeEntries (PM). si is used for SourceInfo-driven Phase 2
// annotation of security scheme facet key lines.
func walkSecuritySchemesUnified(
	schemes *orderedmap.OrderedMap[string, *raml.SecuritySchemeDefinition],
	ci *ContextIndex,
	pm *PositionMap,
	si *raml.SourceInfo,
) {
	if schemes == nil {
		return
	}
	for pair := schemes.Oldest(); pair != nil; pair = pair.Next() {
		ssd := pair.Value
		if ssd == nil {
			continue
		}
		if ssd.ValuePos.EndLine > 0 {
			ci.entries = append(ci.entries, ContextEntry{
				StartLine: ssd.KeyPos.Line,
				EndLine:   ssd.ValuePos.EndLine,
				Kind:      CKSecurityScheme,
			})
			// Phase 2: SourceInfo-driven sub-key scan for security scheme facets.
			// Degrades gracefully to a no-op when si is nil.
			annotateSecSchemeSubkeys(ci.sm, si.Get(ssd.ID))
			// Structural annotation covers keys not handled by the facet pass
			// (partially-parsed schemes where ssd.Type is nil; won't overwrite).
			annotateBlockKeys(ci.sm, ssd.KeyPos.Line+1, ssd.ValuePos.EndLine, securitySchemeBlockRoles)
		}
		if ssd.KeyPos.Line > 0 && ssd.Name != "" {
			pm.add(PosEntry{
				Line:         ssd.KeyPos.Line,
				Col:          ssd.KeyPos.Column,
				SecSchemeDef: ssd,
			})
		}
	}
}

// addShapeEntries adds a CKType entry for shape and CKProperty entries for its
// direct object properties.
func addShapeEntries(ci *ContextIndex, shape *raml.BaseShape) {
	if shape == nil || shape.ValuePos.EndLine == 0 {
		return
	}
	ci.entries = append(ci.entries, ContextEntry{
		StartLine: shape.KeyPos.Line,
		EndLine:   shape.ValuePos.EndLine,
		Kind:      CKType,
		ShapeType: shape.Type,
		ShapeName: shape.Name,
	})
	obj, ok := shape.Shape.(*raml.ObjectShape)
	if !ok || obj == nil {
		return
	}
	for pair := obj.Properties.Oldest(); pair != nil; pair = pair.Next() {
		prop := &pair.Value
		if prop.Base != nil && prop.Base.ValuePos.EndLine > 0 {
			ci.entries = append(ci.entries, ContextEntry{
				StartLine: prop.Base.KeyPos.Line,
				EndLine:   prop.Base.ValuePos.EndLine,
				Kind:      CKProperty,
				ShapeType: prop.Base.Type,
			})
		}
	}
}

// addUsesEntryLines records the key-position lines of all uses: entries in frag
// into ci.usesEntryLines so that IsUsesEntryLine can detect them later.
func addUsesEntryLines(ci *ContextIndex, frag raml.Fragment) {
	var uses *orderedmap.OrderedMap[string, *raml.LibraryLink]
	switch f := frag.(type) {
	case *raml.APIFragment:
		uses = f.Uses
	case *raml.Library:
		uses = f.Uses
	}
	for pair := uses.Oldest(); pair != nil; pair = pair.Next() {
		link := pair.Value
		if link != nil && link.KeyPos.Line > 0 {
			if ci.usesEntryLines == nil {
				ci.usesEntryLines = make(map[int]struct{})
			}
			ci.usesEntryLines[link.KeyPos.Line] = struct{}{}
		}
	}
}
