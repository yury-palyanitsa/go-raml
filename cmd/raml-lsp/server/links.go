package server

import (
	raml "github.com/acronis/go-raml/v3"
	protocol "github.com/tliron/glsp/protocol_3_16"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

const includeTagLen = len("!include ") // 9 — chars to skip to reach the path argument

// BuildDocumentLinks returns all !include and uses: file links present in the
// given source file according to the parsed model.
//
// !include positions come from r.GetIncludeRefs, which is populated during
// parsing by the RAML method resolveInclude. uses: positions come from the
// fragment's LibraryLink map, mirroring addUsesValueEntries in posmap.go.
//
// filePath may be an OS path or a file:// URI; it is normalised internally.
func BuildDocumentLinks(r *raml.RAML, filePath string) []protocol.DocumentLink {
	// Normalise to URI so the internal RAML index lookups (keyed by URI) work
	// regardless of whether the caller passed an OS path or a URI.
	filePath = raml.PathToFileURI(filePath)

	var links []protocol.DocumentLink

	// ---- !include directives -----------------------------------------------
	// r.GetIncludeRefs returns all !include refs recorded during parsing for
	// this file. Each ref carries the 1-based source position of the !include
	// token and the literal path argument.
	refs := r.GetIncludeRefs(filePath)
	for i := range refs {
		ref := &refs[i]
		// Pos.Column (1-based) is the start of the !include token.
		// The path argument follows len("!include ") = 9 chars later.
		startChr := uint32(ref.Pos.Column-1) + uint32(includeTagLen)
		endChr := startChr + uint32(len(ref.Path))
		lspLine := uint32(ref.Pos.Line - 1)

		// ref.AbsPath is a file:// URI.
		target := protocol.DocumentUri(ref.AbsPath)
		links = append(links, protocol.DocumentLink{
			Range: protocol.Range{
				Start: protocol.Position{Line: lspLine, Character: startChr},
				End:   protocol.Position{Line: lspLine, Character: endChr},
			},
			Target: &target,
		})
	}

	// ---- uses: entries -----------------------------------------------------
	// LibraryLink.ValuePos.Column (1-based) points directly at the path string
	// (no tag prefix), so no offset is needed. LibraryLink.Value is the literal
	// path as written; Link.GetLocation() is the resolved absolute URI.
	frag := r.GetFragment(filePath)
	if frag == nil {
		return links
	}
	var uses *orderedmap.OrderedMap[string, *raml.LibraryLink]
	switch f := frag.(type) {
	case *raml.Library:
		uses = f.Uses
	case *raml.APIFragment:
		uses = f.Uses
	}
	for pair := uses.Oldest(); pair != nil; pair = pair.Next() {
		link := pair.Value
		if link == nil || link.ValuePos.Line == 0 || link.Link == nil {
			continue
		}
		startChr := uint32(link.ValuePos.Column - 1)
		endChr := startChr + uint32(len(link.Value))
		lspLine := uint32(link.ValuePos.Line - 1)

		// link.Link.GetLocation() is a file:// URI.
		target := protocol.DocumentUri(link.Link.GetLocation())
		links = append(links, protocol.DocumentLink{
			Range: protocol.Range{
				Start: protocol.Position{Line: lspLine, Character: startChr},
				End:   protocol.Position{Line: lspLine, Character: endChr},
			},
			Target: &target,
		})
	}

	return links
}
