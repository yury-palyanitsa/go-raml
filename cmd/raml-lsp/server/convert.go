package server

import (
	raml "github.com/acronis/go-raml/v3"
	"github.com/acronis/go-stacktrace"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// RamlPosToLSP converts 1-based RAML line/col to a 0-based LSP Position.
func RamlPosToLSP(line, col int) protocol.Position {
	l := uint32(0)
	c := uint32(0)
	if line > 0 {
		l = uint32(line - 1)
	}
	if col > 0 {
		c = uint32(col - 1)
	}
	return protocol.Position{Line: l, Character: c}
}

// RamlPosToSingleCharRange returns a one-character range at the given 1-based position.
func RamlPosToSingleCharRange(line, col int) protocol.Range {
	start := RamlPosToLSP(line, col)
	return protocol.Range{
		Start: start,
		End:   protocol.Position{Line: start.Line, Character: start.Character + 1},
	}
}

// RamlPosToRange converts a *stacktrace.Position to an LSP Range.
// When Position.EndColumn is set the range spans from Column to EndColumn
// (both 1-based inclusive), producing a multi-character squiggle; otherwise
// a single-character range is returned.
func RamlPosToRange(pos *stacktrace.Position) protocol.Range {
	if pos == nil {
		return protocol.Range{}
	}
	start := RamlPosToLSP(pos.Line, pos.Column)
	endLine := start.Line
	if pos.EndLine > 0 {
		endLine = uint32(pos.EndLine - 1)
	}
	// EndColumn is 1-based exclusive (column after the last character, matching
	// stacktrace convention). LSP end character is 0-based exclusive.
	// Conversion: 0-based-exclusive = EndColumn - 1.
	endChar := start.Character + 1
	if pos.EndColumn > 0 {
		endChar = uint32(pos.EndColumn - 1)
	}
	return protocol.Range{
		Start: start,
		End:   protocol.Position{Line: endLine, Character: endChar},
	}
}

// RamlPosToRangeNamed returns a range covering nameLen characters from the given position.
func RamlPosToRangeNamed(line, col, nameLen int) protocol.Range {
	start := RamlPosToLSP(line, col)
	end := uint32(0)
	if start.Character+uint32(nameLen) > start.Character {
		end = start.Character + uint32(nameLen)
	}
	return protocol.Range{
		Start: start,
		End:   protocol.Position{Line: start.Line, Character: end},
	}
}

// NormalizeURI canonicalizes a file:// URI to a consistent form so that cache
// keys, diagnostic URIs, and client-supplied URIs all compare equal.
// On Windows the drive-letter colon must not be percent-encoded
// (e.g. "file:///c%3A/foo" → "file:///c:/foo").
func NormalizeURI(uri string) string {
	return raml.PathToFileURI(raml.FileURIToPath(uri))
}

func ptrTo[T any](v T) *T {
	return &v
}
