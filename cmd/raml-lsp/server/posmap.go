package server

import (
	raml "github.com/acronis/go-raml/v3"
	protocol "github.com/tliron/glsp/protocol_3_16"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// PosEntry represents a named node at a specific position in a RAML source file.
// It is used both for hover (to surface type info) and definition (to navigate to
// the definition of the type it references).
type PosEntry struct {
	// Source position — 1-based, matching go-raml's stacktrace.Position.
	Line int
	Col  int

	Shape    *raml.BaseShape
	LibLink  *raml.LibraryLink
	LibAlias string // the alias written in the document, e.g. "lib" in "lib.TestType"

	// BuiltinType is non-empty when the entry refers to a RAML built-in type keyword
	// (e.g. "string", "integer"). Shape and LibLink are nil in this case.
	BuiltinType string

	// TraitDef is non-nil when the entry refers to a RAML trait (definition site or
	// usage site). At usage sites DestPath is populated; at the definition site it is empty.
	TraitDef *raml.TraitDefinition
	// RTDef is non-nil when the entry refers to a RAML resource type.
	RTDef *raml.ResourceTypeDefinition
	// SecSchemeDef is non-nil when the entry refers to a RAML security scheme.
	SecSchemeDef *raml.SecuritySchemeDefinition

	// DestPath is the absolute OS path of the definition target (empty if none).
	DestPath string
	DestLine int
	DestCol  int
}

// EntityID returns the stable int64 ID of the entity this entry refers to,
// used as the key for ReferenceIndex.Find. Returns (0, false) when the entry
// carries no indexable entity (e.g. BuiltinType-only entries).
func (e PosEntry) EntityID() (int64, bool) {
	switch {
	case e.Shape != nil:
		return e.Shape.ID, true
	case e.LibLink != nil:
		return e.LibLink.ID, true
	case e.TraitDef != nil:
		return e.TraitDef.ID, true
	case e.RTDef != nil:
		return e.RTDef.ID, true
	case e.SecSchemeDef != nil:
		return e.SecSchemeDef.ID, true
	}
	return 0, false
}

// PositionMap maps source lines to the PosEntries on that line for a single file.
// Hit-testing: for a cursor at (line L, char C), find all entries on line L with
// Col ≤ C and pick the one with the largest Col.
type PositionMap struct {
	// lineEntries[1-based line] = entries on that line, in insertion order.
	lineEntries map[int][]PosEntry
}

func newPositionMap() *PositionMap {
	return &PositionMap{lineEntries: make(map[int][]PosEntry)}
}

func (m *PositionMap) add(e PosEntry) {
	m.lineEntries[e.Line] = append(m.lineEntries[e.Line], e)
}

// HitTest returns the best PosEntry for a 0-based LSP position, converting to 1-based
// internally. Returns (entry, true) or (PosEntry{}, false) when nothing is found.
func (m *PositionMap) HitTest(lspLine, lspChar uint32) (PosEntry, bool) {
	line := int(lspLine) + 1 // LSP 0-based → RAML 1-based
	char := int(lspChar) + 1 // LSP 0-based → RAML 1-based
	entries := m.lineEntries[line]
	best := PosEntry{}
	found := false
	for _, e := range entries {
		if e.Col <= char {
			if !found || e.Col > best.Col {
				best = e
				found = true
			}
		}
	}
	return best, found
}

// addUsesValueEntries adds posmap entries for each entry in the uses: map of
// the fragment at loc.
//
// Two entries are emitted per library:
//   - Key entry at link.KeyPos (the alias name, e.g. "ParentLib") — the
//     declaration, symmetric with walkShape's definition entry. HitTest returns
//     this when the cursor is on the alias keyword in the uses: section.
//   - Value entry at link.ValuePos (the path string) — navigates to the file.
func addUsesValueEntries(r *raml.RAML, loc string, pm *PositionMap) {
	frag := r.GetFragment(loc)
	if frag == nil {
		return
	}
	var addEntries func(uses *orderedmap.OrderedMap[string, *raml.LibraryLink])
	addEntries = func(uses *orderedmap.OrderedMap[string, *raml.LibraryLink]) {
		for pair := uses.Oldest(); pair != nil; pair = pair.Next() {
			link := pair.Value
			if link == nil || link.ValuePos.Line == 0 || link.Link == nil {
				continue
			}
			// Declaration entry: alias key position (e.g. the "ParentLib:" key).
			pm.add(PosEntry{
				Line:     link.KeyPos.Line,
				Col:      link.KeyPos.Column,
				LibLink:  link,
				LibAlias: pair.Key,
				DestPath: link.Link.GetLocation(),
				DestLine: 1,
				DestCol:  1,
			})
			// Value entry: path string position (e.g. "libraries/parent-lib.raml").
			pm.add(PosEntry{
				Line:     link.ValuePos.Line,
				Col:      link.ValuePos.Column,
				LibLink:  link,
				LibAlias: pair.Key,
				DestPath: link.Link.GetLocation(),
				DestLine: 1,
				DestCol:  1,
			})
		}
	}
	switch f := frag.(type) {
	case *raml.Library:
		addEntries(f.Uses)
	case *raml.APIFragment:
		addEntries(f.Uses)
	}
}

// walkShape adds base to pm and recursively descends into object properties,
// pattern properties, array items, and union members.
//
// A visited guard is not required here: the traversal only follows concrete
// child shapes (Properties, Items, AnyOf) and never chases Alias/Link/Inherits
// references. Any structural self-recursion would already have been replaced
// with a RecursiveShape sentinel by the parser, which has no children and
// terminates the walk naturally.
func walkShape(base *raml.BaseShape, pm *PositionMap, visited map[int64]struct{}) {
	if base == nil {
		return
	}
	if _, ok := visited[base.ID]; ok {
		return
	}
	visited[base.ID] = struct{}{}

	if base.Name != "" {
		e := buildEntry(base)
		// If a TypeExprRef lands at the same position as the definition entry
		// (happens for body shapes whose position was never overridden to a key
		// node), merge that ref's destination into the single entry so HitTest
		// doesn't have two conflicting entries with non-deterministic results.
		for _, ref := range base.TypeExprRefs {
			if ref.Resolved != nil && ref.Line == base.KeyPos.Line && ref.Column == base.KeyPos.Column {
				e.Shape = ref.Resolved
				e.DestPath = ref.Resolved.Location
				e.DestLine = ref.Resolved.KeyPos.Line
				e.DestCol = ref.Resolved.KeyPos.Column
				break
			}
		}
		pm.add(e)
	}

	// Emit one posmap entry per type reference in the expression so that
	// go-to-definition fires at the exact position where a type name was written,
	// not at the defined type's own key position. Skip refs already merged above.
	for _, ref := range base.TypeExprRefs {
		if ref.Resolved != nil {
			if ref.Line == base.KeyPos.Line && ref.Column == base.KeyPos.Column {
				continue // merged into the definition entry above
			}
			pm.add(PosEntry{
				Line:     ref.Line,
				Col:      ref.Column,
				Shape:    ref.Resolved,
				DestPath: ref.Resolved.Location,
				DestLine: ref.Resolved.KeyPos.Line,
				DestCol:  ref.Resolved.KeyPos.Column,
			})
		} else if ref.LibraryLink != nil {
			pm.add(PosEntry{
				Line:     ref.Line,
				Col:      ref.Column,
				LibLink:  ref.LibraryLink,
				LibAlias: ref.LibraryAlias,
				// Navigate to the `lib:` key in the same file's uses: section.
				DestPath: ref.LibraryLink.Location,
				DestLine: ref.LibraryLink.KeyPos.Line,
				DestCol:  ref.LibraryLink.KeyPos.Column,
			})
		} else if ref.BuiltinType != "" {
			pm.add(PosEntry{
				Line:        ref.Line,
				Col:         ref.Column,
				BuiltinType: ref.BuiltinType,
			})
		}
	}

	// For shapes whose type was resolved directly at parse time (builtin types
	// like `type: string` or `name: integer`), the RDT visitor is never invoked
	// so TypeExprRefs stays empty. Fall back to TypeExpr for hover.
	// Only fire when the TypeExpr value literally spells the builtin name: for
	// shorthand array expressions like `Agent[]` the shape type is "array" but
	// TypeExpr.Value is "Agent[]", so the TypeExprRef for the inner type lives
	// on the anonymous items shape and must not be masked here.
	if len(base.TypeExprRefs) == 0 && base.TypeExpr != nil && base.TypeExpr.Value == base.Type {
		if _, ok := ramlBuiltinTypeDocs[base.Type]; ok {
			pm.add(PosEntry{
				Line:        base.TypeExpr.ValuePos.Line,
				Col:         base.TypeExpr.ValuePos.Column,
				BuiltinType: base.Type,
			})
		}
	}

	switch s := base.Shape.(type) {
	case *raml.ObjectShape:
		if s == nil {
			break
		}
		for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
			walkShape(pair.Value.Base, pm, visited)
		}
		for pair := s.PatternProperties.Oldest(); pair != nil; pair = pair.Next() {
			walkShape(pair.Value.Base, pm, visited)
		}
	case *raml.ArrayShape:
		if s != nil && s.Items != nil {
			walkShape(s.Items, pm, visited)
		}
	case *raml.UnionShape:
		if s != nil {
			for _, member := range s.AnyOf {
				walkShape(member, pm, visited)
			}
		}
	}
}

// addAnnotationEntries adds posmap entries for every DomainExtension (annotation
// usage) in the parse result so that (MyAnnotation) and (lib.MyAnnotation) keys
// support hover and go-to-definition.
//
// The annotation name inside the parentheses starts one column after the '('.
// For library-qualified names (lib.AnnType) we emit two entries: one for the
// library prefix (navigates to the lib: key in uses:) and one for the type name
// (navigates to the annotation type definition).
func addAnnotationEntries(r *raml.RAML, getOrCreate func(string) *PositionMap) {
	for _, de := range r.GetAllAnnotationsPtr() {
		if de.DefinedBy == nil {
			continue
		}
		pm := getOrCreate(de.Location)
		nameStartCol := de.KeyPos.Column + 1 // skip the leading '('

		prefix, _, found := raml.CutReferenceName(de.Name)
		if found {
			// Library-qualified: "lib.AnnType"
			if libLink := r.GetLibraryLinkByPrefix(prefix, de.Location); libLink != nil {
				pm.add(PosEntry{
					Line:     de.KeyPos.Line,
					Col:      nameStartCol,
					LibLink:  libLink,
					LibAlias: prefix,
					DestPath: libLink.Location,
					DestLine: libLink.KeyPos.Line,
					DestCol:  libLink.KeyPos.Column,
				})
			}
			pm.add(PosEntry{
				Line:     de.KeyPos.Line,
				Col:      nameStartCol + len(prefix) + 1, // after "lib."
				Shape:    de.DefinedBy,
				DestPath: de.DefinedBy.Location,
				DestLine: de.DefinedBy.KeyPos.Line,
				DestCol:  de.DefinedBy.KeyPos.Column,
			})
		} else {
			pm.add(PosEntry{
				Line:     de.KeyPos.Line,
				Col:      nameStartCol,
				Shape:    de.DefinedBy,
				DestPath: de.DefinedBy.Location,
				DestLine: de.DefinedBy.KeyPos.Line,
				DestCol:  de.DefinedBy.KeyPos.Column,
			})
		}
	}
}

// addTraitUsageEntry emits a posmap entry for a single trait reference.
// For qualified names "lib.pageable" only the "pageable" part gets a TraitDef
// entry; the "lib" prefix is already covered by uses: entries.
func addTraitUsageEntry(pm *PositionMap, r *raml.RAML, trait *raml.Trait) {
	if trait == nil || trait.Definition == nil || trait.ValuePos.Line == 0 {
		return
	}
	td := trait.Definition
	col := trait.ValuePos.Column
	_, suffix, hasDot := raml.CutReferenceName(trait.Name)
	if hasDot {
		// Emit a lib-prefix entry for the qualifier.
		prefix := trait.Name[:len(trait.Name)-len(suffix)-1]
		if libLink := r.GetLibraryLinkByPrefix(prefix, trait.Location); libLink != nil {
			pm.add(PosEntry{
				Line:     trait.ValuePos.Line,
				Col:      col,
				LibLink:  libLink,
				LibAlias: prefix,
				DestPath: libLink.Location,
				DestLine: libLink.KeyPos.Line,
				DestCol:  libLink.KeyPos.Column,
			})
		}
		col += len(prefix) + 1 // shift past "lib."
	}
	pm.add(PosEntry{
		Line:     trait.ValuePos.Line,
		Col:      col,
		TraitDef: td,
		DestPath: td.Location,
		DestLine: td.KeyPos.Line,
		DestCol:  td.KeyPos.Column,
	})
}

// addRTUsageEntry emits a posmap entry for a single resource-type reference.
func addRTUsageEntry(pm *PositionMap, r *raml.RAML, rt *raml.ResourceType) {
	if rt == nil || rt.Definition == nil || rt.ValuePos.Line == 0 {
		return
	}
	rtd := rt.Definition
	col := rt.ValuePos.Column
	_, suffix, hasDot := raml.CutReferenceName(rt.Name)
	if hasDot {
		prefix := rt.Name[:len(rt.Name)-len(suffix)-1]
		if libLink := r.GetLibraryLinkByPrefix(prefix, rt.Location); libLink != nil {
			pm.add(PosEntry{
				Line:     rt.ValuePos.Line,
				Col:      col,
				LibLink:  libLink,
				LibAlias: prefix,
				DestPath: libLink.Location,
				DestLine: libLink.KeyPos.Line,
				DestCol:  libLink.KeyPos.Column,
			})
		}
		col += len(prefix) + 1
	}
	pm.add(PosEntry{
		Line:     rt.ValuePos.Line,
		Col:      col,
		RTDef:    rtd,
		DestPath: rtd.Location,
		DestLine: rtd.KeyPos.Line,
		DestCol:  rtd.KeyPos.Column,
	})
}

// addFragmentToSeen recursively visits all fragments reachable from frag.
// allFragmentLocations returns the set of all reachable fragment file paths
// allFragmentLocations returns the set of all reachable fragment file paths.
// It is derived from the provided shapes slice (each shape carries its source
// file path) augmented with fragments reachable through the entry-point's
// uses: graph, so that library files with no shapes are also included.
func allFragmentLocations(frag raml.Fragment, shapes []*raml.BaseShape) map[string]struct{} {
	locs := make(map[string]struct{})
	for _, shape := range shapes {
		if shape != nil {
			locs[shape.Location] = struct{}{}
		}
	}
	addFragmentToSeen(frag, locs)
	return locs
}

// addFragmentToSeen recursively visits all fragments reachable from frag
// via their uses: map and records each location in seen.
func addFragmentToSeen(frag raml.Fragment, seen map[string]struct{}) {
	if frag == nil {
		return
	}
	loc := frag.GetLocation()
	if _, ok := seen[loc]; ok {
		return
	}
	seen[loc] = struct{}{}

	var uses *orderedmap.OrderedMap[string, *raml.LibraryLink]
	switch f := frag.(type) {
	case *raml.APIFragment:
		uses = f.Uses
	case *raml.Library:
		uses = f.Uses
	}
	for pair := uses.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value.Link != nil {
			addFragmentToSeen(pair.Value.Link, seen)
		}
	}
}

func buildEntry(base *raml.BaseShape) PosEntry {
	e := PosEntry{
		Line:  base.KeyPos.Line,
		Col:   base.KeyPos.Column,
		Shape: base,
	}

	// go-to-definition: !include DataType fragments navigate to the included file.
	// Named-type references (Alias, Inherits) are now handled via TypeExprRefs,
	// which carry the exact source position of each type name in the expression.
	if base.Link != nil {
		e.DestPath = base.Link.Location
		e.DestLine = 1
		e.DestCol = 1
	}
	return e
}

// ---- Definition ----

// originNameLen returns the length of the token at the reference site so that
// OriginSelectionRange covers exactly the written identifier.
func originNameLen(e PosEntry) int {
	switch {
	case e.Shape != nil:
		return len(e.Shape.Name)
	case e.LibAlias != "":
		return len(e.LibAlias)
	case e.BuiltinType != "":
		return len(e.BuiltinType)
	case e.TraitDef != nil:
		return len(e.TraitDef.Name)
	case e.RTDef != nil:
		return len(e.RTDef.Name)
	case e.SecSchemeDef != nil:
		return len(e.SecSchemeDef.Name)
	default:
		return 1
	}
}

// DefinitionLocationLink returns an LSP LocationLink for the definition target
// of a PosEntry. OriginSelectionRange covers exactly the token that was clicked,
// so VS Code underlines only that token rather than the whole expression.
// Returns nil when the entry has no known definition.
func DefinitionLocationLink(e PosEntry) *protocol.LocationLink {
	if e.DestPath == "" {
		return nil
	}
	targetURI := e.DestPath
	nameLen := originNameLen(e)
	targetRng := RamlPosToRangeNamed(e.DestLine, e.DestCol, nameLen)
	originRng := RamlPosToRangeNamed(e.Line, e.Col, nameLen)
	return &protocol.LocationLink{
		OriginSelectionRange: &originRng,
		TargetURI:            protocol.DocumentUri(targetURI),
		TargetRange:          targetRng,
		TargetSelectionRange: targetRng,
	}
}

// addSecSchemeUsageEntry emits a posmap entry for a single securedBy reference.
// Null schemes (Definition.Type == "null") are skipped. For lib-qualified names
// "lib.oauth2" the library prefix entry is emitted first, then the scheme name.
func addSecSchemeUsageEntry(pm *PositionMap, r *raml.RAML, ss *raml.SecurityScheme) {
	if ss == nil || ss.Definition == nil || ss.ValuePos.Line == 0 {
		return
	}
	// Skip the null security scheme — it has no meaningful definition to navigate to.
	if ss.Name == "null" {
		return
	}
	ssd := ss.Definition
	col := ss.ValuePos.Column
	_, suffix, hasDot := raml.CutReferenceName(ss.Name)
	if hasDot {
		prefix := ss.Name[:len(ss.Name)-len(suffix)-1]
		if libLink := r.GetLibraryLinkByPrefix(prefix, ss.Location); libLink != nil {
			pm.add(PosEntry{
				Line:     ss.ValuePos.Line,
				Col:      col,
				LibLink:  libLink,
				LibAlias: prefix,
				DestPath: libLink.Location,
				DestLine: libLink.KeyPos.Line,
				DestCol:  libLink.KeyPos.Column,
			})
		}
		col += len(prefix) + 1 // shift past "lib."
	}
	pm.add(PosEntry{
		Line:         ss.ValuePos.Line,
		Col:          col,
		SecSchemeDef: ssd,
		DestPath:     ssd.Location,
		DestLine:     ssd.KeyPos.Line,
		DestCol:      ssd.KeyPos.Column,
	})
}

// resolveObjectProperties resolves the Properties map of an ObjectShape for
// base, following ArrayShape.Items transparently.  It is the canonical way to
// obtain property keys for a shape without duplicating shape-switch logic.
// All shape types (including annotation types) are processed by UnwrapShapes,
// so base.Shape always contains the fully merged property set — callers must
// never walk Inherits themselves.
func resolveObjectProperties(base *raml.BaseShape) *orderedmap.OrderedMap[string, raml.Property] {
	if base == nil {
		return nil
	}
	switch s := base.Shape.(type) {
	case *raml.ObjectShape:
		if s != nil {
			return s.Properties
		}
	case *raml.ArrayShape:
		// Transparent array unwrapping: a Mapping value against an array-typed
		// shape delegates to the item type.
		if s != nil && s.Items != nil {
			return resolveObjectProperties(s.Items)
		}
	}
	return nil
}

// resolveArrayItems returns the Items BaseShape of an ArrayShape for base.
// Used to unwrap one level of array nesting when the parsed VALUE is a YAML
// sequence.  Must only be called on post-unwrap shapes.
func resolveArrayItems(base *raml.BaseShape) *raml.BaseShape {
	if base == nil {
		return nil
	}
	if arr, ok := base.Shape.(*raml.ArrayShape); ok && arr != nil && arr.Items != nil {
		return arr.Items
	}
	return nil
}

// addExtensionValueEntries augments position maps with entries for keys inside
// the values of both custom shape facets and annotation (CDP) usages.
//
// For custom facets it also emits an entry for the facet key itself (pointing
// to the facet definition in the parent type's `facets:` block).
// Annotation key entries are handled separately by addAnnotationEntries.
//
// Must be called after r.UnwrapShapes() so that ObjectShape.Properties and
// CustomShapeFacetDefinitions are fully merged.
func addExtensionValueEntries(r *raml.RAML, posMaps map[string]*PositionMap) {
	getOrCreate := func(loc string) *PositionMap {
		if posMaps[loc] == nil {
			posMaps[loc] = newPositionMap()
		}
		return posMaps[loc]
	}

	// Custom shape facet values: emit a key entry for each used facet and walk
	// its value for nested property-key entries.
	for _, base := range r.GetShapes() {
		if base == nil {
			continue
		}
		for pair := base.CustomShapeFacets.Oldest(); pair != nil; pair = pair.Next() {
			anyNode := pair.Value
			if anyNode == nil || anyNode.KeyPos.Line == 0 {
				continue
			}
			defBase := findFacetDef(base.Inherits, pair.Key)
			if defBase == nil {
				continue
			}
			pm := getOrCreate(base.Location)
			pm.add(PosEntry{
				Line:     anyNode.KeyPos.Line,
				Col:      anyNode.KeyPos.Column,
				Shape:    defBase,
				DestPath: defBase.Location,
				DestLine: defBase.KeyPos.Line,
				DestCol:  defBase.KeyPos.Column,
			})
			walkValueNodePropertyEntries(anyNode.Value, defBase, pm)
		}
	}

	// Annotation (CDP) values: walk each extension's value for nested
	// property-key entries.  The annotation key entry itself is emitted by
	// addAnnotationEntries together with hover and go-to-definition metadata.
	for _, de := range r.GetAllAnnotationsPtr() {
		if de == nil || de.DefinedBy == nil || de.Extension == nil || de.Extension.Value == nil {
			continue
		}
		pm := getOrCreate(de.Location)
		walkValueNodePropertyEntries(de.Extension.Value, de.DefinedBy, pm)
	}
}

// findFacetDef traverses the inheritance chain to find the BaseShape that
// defines a custom facet by name. Mirrors the lookup in validateShapeFacets.
func findFacetDef(inherits []*raml.BaseShape, facetName string) *raml.BaseShape {
	for len(inherits) > 0 {
		parent := inherits[0]
		if parent == nil {
			break
		}
		if parent.CustomShapeFacetDefinitions != nil {
			if prop, ok := parent.CustomShapeFacetDefinitions.Get(facetName); ok {
				return prop.Base
			}
		}
		inherits = parent.Inherits
	}
	return nil
}

// walkValueNodePropertyEntries recursively walks a ValueNode tree alongside the
// corresponding type constraint (shape) to emit posmap entries for mapping keys
// that match known object properties. This enables hover and go-to-definition
// for keys at any nesting depth within structured annotation values.
//
// Sequences are handled by unwrapping the ArrayShape's Items and recursing into
// each element, so `(roles): [{name: "x"}]` correctly maps `name:` to the items
// shape's property definition.
func walkValueNodePropertyEntries(nv *raml.ValueNode, base *raml.BaseShape, pm *PositionMap) {
	if nv == nil || base == nil {
		return
	}
	switch {
	case nv.Mapping != nil:
		props := resolveObjectProperties(base)
		if props == nil {
			return
		}
		for i := range nv.Mapping.Entries {
			e := &nv.Mapping.Entries[i]
			if e.KeyPos.Line == 0 {
				continue
			}
			propVal, ok := props.Get(e.Key)
			if !ok || propVal.Base == nil {
				continue
			}
			pm.add(PosEntry{
				Line:     e.KeyPos.Line,
				Col:      e.KeyPos.Column,
				Shape:    propVal.Base,
				DestPath: propVal.Base.Location,
				DestLine: propVal.Base.KeyPos.Line,
				DestCol:  propVal.Base.KeyPos.Column,
			})
			// Recurse for nested object or array values.
			walkValueNodePropertyEntries(e.Value, propVal.Base, pm)
		}
	case nv.Sequence != nil:
		items := resolveArrayItems(base)
		if items == nil {
			return
		}
		for i := range nv.Sequence.Items {
			walkValueNodePropertyEntries(nv.Sequence.Items[i].Value, items, pm)
		}
	}
}

// AugmentPosMapsPostUnwrap augments the position maps with custom facet and
// annotation value entries. Must be called after r.UnwrapShapes() and before
// r.ValidateShapes() so that ObjectShape.Properties and
// CustomShapeFacetDefinitions are fully merged.
func AugmentPosMapsPostUnwrap(r *raml.RAML, posMaps map[string]*PositionMap) {
	addExtensionValueEntries(r, posMaps)
}
