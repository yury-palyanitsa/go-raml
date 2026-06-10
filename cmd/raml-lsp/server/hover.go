package server

import (
	"fmt"
	"strings"

	raml "github.com/acronis/go-raml/v3"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// hover is the textDocument/hover LSP handler.
func (s *Server) hover(ctx *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	uri := NormalizeURI(string(params.TextDocument.URI))

	text, ok := s.store.Get(uri)
	if !ok {
		return nil, nil
	}
	result := s.cache.FlushPending(s.cache.RootFor(uri))
	fc := result.FileContext(uri)

	var lines docLines
	if result != nil {
		lines = result.Lines
	} else {
		lines = parseDoc(text)
	}

	target := resolveHoverTarget(params, lines, fc)
	if target == nil {
		return nil, nil
	}
	if target.Pos != nil {
		return dispatchPosEntryHover(*target.Pos), nil
	}
	docText := facetKeyDoc(target.KeyName, target.DocEntry)
	if docText == "" {
		return nil, nil
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: docText},
		Range:    &target.Range,
	}, nil
}

// HoverTarget is the result of resolveHoverTarget. Exactly one of Pos (model-
// based) or KeyName (key-doc fallback) is populated.
type HoverTarget struct {
	// Pos is non-nil for model-based hover (shape, library link, trait, etc.).
	Pos *PosEntry
	// KeyName and DocEntry are set for the key-doc fallback path.
	KeyName  string
	DocEntry *ContextEntry
	Range    protocol.Range
}

// resolveHoverTarget determines what (if anything) should be shown when the
// cursor is at params.Position. It mirrors the role that analyzePosition plays
// for completion: a single function that answers "what is here?" using first
// the model index (PositionMap hit-test) then a text-level fallback (YAML key
// name lookup), returning nil when nothing hoverable is found.
func resolveHoverTarget(params *protocol.HoverParams, lines docLines, fc *FileContext) *HoverTarget {
	// Model path: position map has an entry at (line, col).
	if fc.PM != nil {
		if entry, ok := fc.PM.HitTest(params.Position.Line, params.Position.Character); ok {
			return &HoverTarget{Pos: &entry}
		}
	}

	// Key-doc fallback: cursor is over a YAML mapping key; return its spec docs.
	lspLine := params.Position.Line
	if int(lspLine) >= len(lines) {
		return nil
	}
	li := lines[lspLine]
	colonIdx := findYAMLKeyColon(li.raw)
	// Cursor must be within the key token (after leading whitespace, before colon).
	if colonIdx < 0 || int(params.Position.Character) < li.indent || int(params.Position.Character) > colonIdx {
		return nil
	}
	keyName := strings.TrimSpace(li.raw[:colonIdx])
	if keyName == "" {
		return nil
	}
	rng := protocol.Range{
		Start: protocol.Position{Line: lspLine, Character: uint32(li.indent)},
		End:   protocol.Position{Line: lspLine, Character: uint32(li.indent) + uint32(len(keyName))},
	}
	return &HoverTarget{
		KeyName:  keyName,
		DocEntry: fc.CI.Query(int(lspLine) + 1), // nil-safe via ContextIndex.Query
		Range:    rng,
	}
}

// dispatchPosEntryHover routes a model-based PosEntry to the appropriate hover
// renderer, replacing the inline type-switch that lived in the old hover().
func dispatchPosEntryHover(e PosEntry) *protocol.Hover {
	switch {
	case e.LibLink != nil:
		return libraryLinkHover(e)
	case e.Shape != nil:
		return shapeHover(e)
	case e.BuiltinType != "":
		return builtinTypeHover(e.BuiltinType)
	case e.TraitDef != nil:
		return traitDefHover(e)
	case e.RTDef != nil:
		return rtDefHover(e)
	case e.SecSchemeDef != nil:
		return secSchemeDefHover(e)
	}
	return nil
}

// facetKeyDoc returns the markdown documentation for a RAML mapping key at
// the given context entry (nil = root level). Per-context overrides in
// ramlKeyDocsByKind take precedence over the shared ramlKeyDocs table.
// Numeric 3-digit keys (HTTP status codes) are looked up in httpStatusPhrases
// and httpStatusDescriptions regardless of context.
func facetKeyDoc(keyName string, entry *ContextEntry) string {
	if entry != nil {
		if m, ok := ramlKeyDocsByKind[entry.Kind]; ok {
			if doc, ok := m[keyName]; ok {
				return doc
			}
		}
	}
	if doc := statusCodeDoc(keyName); doc != "" {
		return doc
	}
	if doc := httpMethodDoc(keyName); doc != "" {
		return doc
	}
	return ramlKeyDocs[keyName]
}

// httpMethodDoc returns markdown hover text for an HTTP method key (e.g. "get",
// "post"). Returns "" when keyName is not a recognised HTTP method.
func httpMethodDoc(keyName string) string {
	if !raml.IsHTTPMethod(keyName) {
		return ""
	}
	upper := strings.ToUpper(keyName)
	desc := ramlKeyDocs[keyName]
	var doc string
	if desc == "" {
		doc = "**" + upper + "**"
	} else {
		doc = "**" + upper + "**\n\n" + desc
	}
	doc += "\n\n[MDN Reference](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Methods/" + upper + ")"
	return doc
}

// statusCodeDoc returns markdown hover text for an HTTP status code string
// (e.g. "200", "404"). Returns "" when the string is not a known status code.
func statusCodeDoc(code string) string {
	phrase, ok := httpStatusPhrases[code]
	if !ok {
		return ""
	}
	doc := "**" + code + " " + phrase + "**"
	if desc, ok := httpStatusDescriptions[code]; ok {
		doc += "\n\n" + desc
	}
	doc += "\n\n[MDN Reference](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Status/" + code + ")"
	return doc
}

// libraryLinkHover builds an LSP Hover response for a library-prefix reference.
func libraryLinkHover(entry PosEntry) *protocol.Hover {
	if entry.LibLink == nil {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s**: `%s`", entry.LibAlias, entry.LibLink.Value)
	if entry.LibLink.Link != nil && entry.LibLink.Link.Usage != nil && entry.LibLink.Link.Usage.Value != "" {
		fmt.Fprintf(&sb, "\n\n%s", entry.LibLink.Link.Usage.Value)
	}
	rng := RamlPosToRangeNamed(entry.Line, entry.Col, len(entry.LibAlias))
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: sb.String(),
		},
		Range: &rng,
	}
}

// shapeHover builds an LSP Hover response for the given shape.
// entry.Line/Col point to the reference site, which may differ from
// entry.Shape.KeyPos at the definition site.
func shapeHover(entry PosEntry) *protocol.Hover {
	if entry.Shape == nil {
		return nil
	}
	md := buildHoverMarkdown(entry.Shape)
	if md == "" {
		return nil
	}
	rng := RamlPosToRangeNamed(entry.Line, entry.Col, len(entry.Shape.Name))
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: md,
		},
		Range: &rng,
	}
}

func buildHoverMarkdown(base *raml.BaseShape) string {
	var sb strings.Builder

	typeName := base.Type
	if typeName == "" {
		typeName = "any"
	}
	// Show parent type(s).
	if len(base.Inherits) > 0 {
		parents := make([]string, 0, len(base.Inherits))
		for _, p := range base.Inherits {
			if p != nil {
				parents = append(parents, "**"+p.Name+"**")
			}
		}
		fmt.Fprintf(&sb, "**%s** (`%s`) extends %s", base.Name, typeName, strings.Join(parents, ", "))
	} else if base.Alias != nil {
		fmt.Fprintf(&sb, "**%s** (`%s`) aliased to **%s**", base.Name, typeName, base.Alias.Name)
	} else {
		fmt.Fprintf(&sb, "**%s** (`%s`)", base.Name, typeName)
	}

	// DisplayName is a human-readable label; show it when it differs from the identifier.
	if base.DisplayName != nil && base.DisplayName.Value != "" && base.DisplayName.Value != base.Name {
		fmt.Fprintf(&sb, "\n\n*%s*", base.DisplayName.Value)
	}

	if base.Description != nil && base.Description.Value != "" {
		fmt.Fprintf(&sb, "\n\n%s", base.Description.Value)
	}

	// Append concrete shape facets.
	facets := shapeFactsMarkdown(base)
	if facets != "" {
		fmt.Fprintf(&sb, "\n\n%s", facets)
	}

	return sb.String()
}

func shapeFactsMarkdown(base *raml.BaseShape) string {
	if base.Shape == nil {
		return ""
	}
	var lines []string
	switch s := base.Shape.(type) {
	case *raml.StringShape:
		if s.MinLength != nil {
			lines = append(lines, fmt.Sprintf("minLength: %d", s.MinLength.Value))
		}
		if s.MaxLength != nil {
			lines = append(lines, fmt.Sprintf("maxLength: %d", s.MaxLength.Value))
		}
		if s.Pattern != nil {
			lines = append(lines, fmt.Sprintf("pattern: %s", s.Pattern.Value.String()))
		}
	case *raml.IntegerShape:
		if s.Minimum != nil {
			lines = append(lines, fmt.Sprintf("minimum: %v", s.Minimum.Value))
		}
		if s.Maximum != nil {
			lines = append(lines, fmt.Sprintf("maximum: %v", s.Maximum.Value))
		}
		if s.Format != nil {
			lines = append(lines, fmt.Sprintf("format: %s", s.Format.Value))
		}
	case *raml.NumberShape:
		if s.Minimum != nil {
			lines = append(lines, fmt.Sprintf("minimum: %v", s.Minimum.Value))
		}
		if s.Maximum != nil {
			lines = append(lines, fmt.Sprintf("maximum: %v", s.Maximum.Value))
		}
		if s.Format != nil {
			lines = append(lines, fmt.Sprintf("format: %s", s.Format.Value))
		}
	case *raml.ArrayShape:
		if s.MinItems != nil {
			lines = append(lines, fmt.Sprintf("minItems: %d", s.MinItems.Value))
		}
		if s.MaxItems != nil {
			lines = append(lines, fmt.Sprintf("maxItems: %d", s.MaxItems.Value))
		}
		if s.UniqueItems != nil && s.UniqueItems.Value {
			lines = append(lines, "uniqueItems: true")
		}
		if s.Items != nil {
			lines = append(lines, fmt.Sprintf("items: %s", s.Items.Type))
		}
	case *raml.ObjectShape:
		if s.MinProperties != nil {
			lines = append(lines, fmt.Sprintf("minProperties: %d", s.MinProperties.Value))
		}
		if s.MaxProperties != nil {
			lines = append(lines, fmt.Sprintf("maxProperties: %d", s.MaxProperties.Value))
		}
		if l := s.Properties.Len(); l > 0 {
			propStrings := make([]string, 0, l)
			for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
				propStrings = append(propStrings, fmt.Sprintf("%s", pair.Key))
			}
			lines = append(lines, fmt.Sprintf("properties: %s", strings.Join(propStrings, ", ")))
		}
	}
	if len(base.Enum) > 0 {
		lines = append(lines, fmt.Sprintf("enum: %s", base.Enum.String()))
	}
	if len(lines) == 0 {
		return ""
	}
	return "```\n" + strings.Join(lines, "\n") + "\n```"
}

// builtinTypeHover returns an LSP Hover with spec documentation for a RAML built-in type.
func builtinTypeHover(typeName string) *protocol.Hover {
	doc, ok := ramlBuiltinTypeDocs[typeName]
	if !ok {
		return nil
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: doc,
		},
	}
}

// traitDefHover returns an LSP Hover for a trait definition or usage site.
func traitDefHover(entry PosEntry) *protocol.Hover {
	td := entry.TraitDef
	if td == nil {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s** (`trait`)", td.Name)
	if td.Usage != nil && td.Usage.Value != "" {
		fmt.Fprintf(&sb, "\n\n%s", td.Usage.Value)
	}
	rng := RamlPosToRangeNamed(entry.Line, entry.Col, len(td.Name))
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: sb.String(),
		},
		Range: &rng,
	}
}

// rtDefHover returns an LSP Hover for a resource type definition or usage site.
func rtDefHover(entry PosEntry) *protocol.Hover {
	rtd := entry.RTDef
	if rtd == nil {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s** (`resourceType`)", rtd.Name)
	if rtd.Usage != nil && rtd.Usage.Value != "" {
		fmt.Fprintf(&sb, "\n\n%s", rtd.Usage.Value)
	}
	if rtd.Description != nil && rtd.Description.Value != "" {
		fmt.Fprintf(&sb, "\n\n%s", rtd.Description.Value)
	}
	rng := RamlPosToRangeNamed(entry.Line, entry.Col, len(rtd.Name))
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: sb.String(),
		},
		Range: &rng,
	}
}

// secSchemeDefHover returns an LSP Hover for a security scheme definition or usage site.
func secSchemeDefHover(entry PosEntry) *protocol.Hover {
	ssd := entry.SecSchemeDef
	if ssd == nil {
		return nil
	}
	var sb strings.Builder
	schemeType := ""
	if ssd.Type != nil {
		schemeType = string(ssd.Type.Value)
	}
	if schemeType != "" {
		fmt.Fprintf(&sb, "**%s** (`securityScheme`: %s)", ssd.Name, schemeType)
	} else {
		fmt.Fprintf(&sb, "**%s** (`securityScheme`)", ssd.Name)
	}
	if ssd.DisplayName != nil && ssd.DisplayName.Value != "" {
		fmt.Fprintf(&sb, "\n\n%s", ssd.DisplayName.Value)
	}
	if ssd.Description != nil && ssd.Description.Value != "" {
		fmt.Fprintf(&sb, "\n\n%s", ssd.Description.Value)
	}
	rng := RamlPosToRangeNamed(entry.Line, entry.Col, len(ssd.Name))
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: sb.String(),
		},
		Range: &rng,
	}
}
