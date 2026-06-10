package server

import (
	raml "github.com/acronis/go-raml/v3"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ReferenceIndex maps a stable int64 entity ID to every source location that
// references that entity. Both BaseShape.ID and LibraryLink.ID are int64 so a
// single map covers types and library aliases uniformly.
//
// See ARCHITECTURE.md for design constraints.
type ReferenceIndex struct {
	refs map[int64][]protocol.Location
}

func indexTraitRef(trait *raml.Trait, idx *ReferenceIndex) {
	if trait == nil || trait.Definition == nil || trait.ValuePos.Line == 0 {
		return
	}
	td := trait.Definition
	col := trait.ValuePos.Column
	_, suffix, hasDot := raml.CutReferenceName(trait.Name)
	if hasDot {
		col += len(trait.Name) - len(suffix) // shift to the unqualified part
	}
	loc := protocol.Location{
		URI:   trait.Location,
		Range: RamlPosToRangeNamed(trait.ValuePos.Line, col, len(td.Name)),
	}
	idx.refs[td.ID] = append(idx.refs[td.ID], loc)
}

func indexRTRef(rt *raml.ResourceType, idx *ReferenceIndex) {
	if rt == nil || rt.Definition == nil || rt.ValuePos.Line == 0 {
		return
	}
	rtd := rt.Definition
	col := rt.ValuePos.Column
	_, suffix, hasDot := raml.CutReferenceName(rt.Name)
	if hasDot {
		col += len(rt.Name) - len(suffix)
	}
	loc := protocol.Location{
		URI:   rt.Location,
		Range: RamlPosToRangeNamed(rt.ValuePos.Line, col, len(rtd.Name)),
	}
	idx.refs[rtd.ID] = append(idx.refs[rtd.ID], loc)
}

// Find returns all source locations that reference the entity with the given
// ID. Works for shape IDs, library link IDs, trait definition IDs,
// resource type definition IDs, and security scheme definition IDs.
func (r *ReferenceIndex) Find(id int64) []protocol.Location {
	return r.refs[id]
}

func indexSecSchemeRef(ss *raml.SecurityScheme, idx *ReferenceIndex) {
	if ss == nil || ss.Definition == nil || ss.ValuePos.Line == 0 || ss.Name == "null" {
		return
	}
	ssd := ss.Definition
	col := ss.ValuePos.Column
	_, suffix, hasDot := raml.CutReferenceName(ss.Name)
	if hasDot {
		col += len(ss.Name) - len(suffix) // shift to the unqualified part
	}
	loc := protocol.Location{
		URI:   ss.Location,
		Range: RamlPosToRangeNamed(ss.ValuePos.Line, col, len(ssd.Name)),
	}
	idx.refs[ssd.ID] = append(idx.refs[ssd.ID], loc)
}
