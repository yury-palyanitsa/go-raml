package server

import (
	"strconv"
	"strings"

	raml "github.com/acronis/go-raml/v3"
	"github.com/acronis/go-stacktrace"
	protocol "github.com/tliron/glsp/protocol_3_16"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// appendMapSection is a generic helper that appends a named section DocumentSymbol
// built from an ordered map to syms and returns the extended slice.
// posOf extracts KeyPos from a map value, returning zero Position for nil values.
// conv converts each key-value pair to a DocumentSymbol.
// keep filters entries; a nil keep includes all entries.
func appendMapSection[V any](
	syms []protocol.DocumentSymbol,
	name string,
	kind protocol.SymbolKind,
	m *orderedmap.OrderedMap[string, V],
	posOf func(V) stacktrace.Position,
	conv func(string, V) protocol.DocumentSymbol,
	keep func(V) bool,
) []protocol.DocumentSymbol {
	if m == nil || m.Len() == 0 {
		return syms
	}
	children := make([]protocol.DocumentSymbol, 0, m.Len())
	var firstPos stacktrace.Position
	for pair := m.Oldest(); pair != nil; pair = pair.Next() {
		if keep != nil && !keep(pair.Value) {
			continue
		}
		if firstPos.Line == 0 {
			firstPos = posOf(pair.Value)
		}
		children = append(children, conv(pair.Key, pair.Value))
	}
	if len(children) == 0 {
		return syms
	}
	return append(syms, sectionSymbol(name, kind, firstPos, children))
}

// keyPosOf helpers avoid repeating the nil-guard + KeyPos pattern in every
// appendMapSection call.
func ssDefKeyPos(v *raml.SecuritySchemeDefinition) stacktrace.Position {
	if v != nil {
		return v.KeyPos
	}
	return stacktrace.Position{}
}
func rtDefKeyPos(v *raml.ResourceTypeDefinition) stacktrace.Position {
	if v != nil {
		return v.KeyPos
	}
	return stacktrace.Position{}
}
func traitDefKeyPos(v *raml.TraitDefinition) stacktrace.Position {
	if v != nil {
		return v.KeyPos
	}
	return stacktrace.Position{}
}
func libLinkKeyPos(v *raml.LibraryLink) stacktrace.Position {
	if v != nil {
		return v.KeyPos
	}
	return stacktrace.Position{}
}
func baseShapeKeyPos(v *raml.BaseShape) stacktrace.Position {
	if v != nil {
		return v.KeyPos
	}
	return stacktrace.Position{}
}

// apifragSymbols produces the outline for a RAML 1.0 API (root) fragment.
// The layout mirrors the ALS outline:
//
//	title / version / baseUri  (scalar metadata)
//	securitySchemes  → each scheme
//	resourceTypes    → each resource type
//	traits           → each trait
//	uses             → each library alias
//	types            → each declared type
//	Security         (api-level securedBy)
//	documentation    → each doc item
//	/endpoint …      (hierarchical endpoint/operation tree)
func apifragSymbols(f *raml.APIFragment) []protocol.DocumentSymbol {
	var syms []protocol.DocumentSymbol

	// --- scalar metadata ---
	if f.Title != nil {
		syms = append(syms, scalarKeySymbol("title", f.Title.Value, f.Title.KeyPos))
	}
	if f.Version != nil {
		syms = append(syms, scalarKeySymbol("version", f.Version.Value, f.Version.KeyPos))
	}
	if f.BaseURI != nil {
		syms = append(syms, scalarKeySymbol("baseUri", f.BaseURI.Value, f.BaseURI.KeyPos))
	}

	syms = appendMapSection(syms, "securitySchemes", protocol.SymbolKindKey, f.SecuritySchemes,
		ssDefKeyPos, securitySchemeToSymbol, nil)
	syms = appendMapSection(syms, "resourceTypes", protocol.SymbolKindModule, f.ResourceTypes,
		rtDefKeyPos, resourceTypeToSymbol, nil)
	syms = appendMapSection(syms, "traits", protocol.SymbolKindObject, f.Traits,
		traitDefKeyPos, traitToSymbol, nil)
	syms = appendMapSection(syms, "uses", protocol.SymbolKindNamespace, f.Uses,
		libLinkKeyPos, libraryLinkToSymbol, nil)
	syms = appendMapSection(syms, "types", protocol.SymbolKindClass, f.Types,
		baseShapeKeyPos,
		func(_ string, v *raml.BaseShape) protocol.DocumentSymbol { return shapeToSymbol(v) },
		func(v *raml.BaseShape) bool { return v != nil })

	// --- securedBy (API-level) ---
	if len(f.SecuredBy) > 0 {
		syms = append(syms, securedByToSymbol("Security", f.SecuredBy))
	}

	// --- documentation ---
	if len(f.Documentation) > 0 {
		syms = append(syms, documentationSectionSymbol(f.Documentation))
	}

	// --- endpoints ---
	for pair := f.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		syms = append(syms, endpointToSymbol(pair.Value, f.Location))
	}

	return syms
}

// librarySymbols produces the outline for a RAML Library fragment.
func librarySymbols(f *raml.Library) []protocol.DocumentSymbol {
	var syms []protocol.DocumentSymbol

	syms = appendMapSection(syms, "uses", protocol.SymbolKindNamespace, f.Uses,
		libLinkKeyPos, libraryLinkToSymbol, nil)
	syms = appendMapSection(syms, "resourceTypes", protocol.SymbolKindModule, f.ResourceTypes,
		rtDefKeyPos, resourceTypeToSymbol, nil)
	syms = appendMapSection(syms, "traits", protocol.SymbolKindObject, f.Traits,
		traitDefKeyPos, traitToSymbol, nil)
	syms = appendMapSection(syms, "securitySchemes", protocol.SymbolKindKey, f.SecuritySchemes,
		ssDefKeyPos, securitySchemeToSymbol, nil)
	syms = appendMapSection(syms, "types", protocol.SymbolKindClass, f.Types,
		baseShapeKeyPos,
		func(_ string, v *raml.BaseShape) protocol.DocumentSymbol { return shapeToSymbol(v) },
		func(v *raml.BaseShape) bool { return v != nil })

	return syms
}

// sectionSymbol wraps a named list of children into a single container symbol.
// pos is used as an approximation for the container range (typically the first child's key).
func sectionSymbol(name string, kind protocol.SymbolKind, pos stacktrace.Position, children []protocol.DocumentSymbol) protocol.DocumentSymbol {
	rng := protocol.Range{}
	if pos.Line > 0 {
		rng = RamlPosToRangeNamed(pos.Line, pos.Column, len(name))
	}
	return protocol.DocumentSymbol{
		Name:           name,
		Kind:           kind,
		Range:          rng,
		SelectionRange: rng,
		Children:       children,
	}
}

// scalarKeySymbol creates a String-kind leaf symbol for a scalar metadata field
// (title, version, baseUri, …).  The field value is shown as the detail.
func scalarKeySymbol(name, detail string, pos stacktrace.Position) protocol.DocumentSymbol {
	rng := RamlPosToRangeNamed(pos.Line, pos.Column, len(name))
	return protocol.DocumentSymbol{
		Name:           name,
		Detail:         ptrTo(detail),
		Kind:           protocol.SymbolKindString,
		Range:          rng,
		SelectionRange: rng,
	}
}

// securedByToSymbol creates a leaf symbol for a securedBy list (either API-level or
// operation/endpoint-level).  The scheme names are joined as the detail string.
func securedByToSymbol(label string, schemes []*raml.SecurityScheme) protocol.DocumentSymbol {
	names := make([]string, 0, len(schemes))
	var pos stacktrace.Position
	for _, s := range schemes {
		if s == nil {
			continue
		}
		names = append(names, s.Name)
		if pos.Line == 0 {
			pos = s.ValuePos
		}
	}
	rng := RamlPosToRangeNamed(pos.Line, pos.Column, len(label))
	return protocol.DocumentSymbol{
		Name:           label,
		Detail:         ptrTo(strings.Join(names, ", ")),
		Kind:           protocol.SymbolKindString,
		Range:          rng,
		SelectionRange: rng,
	}
}

// traitRefsToSymbol creates a leaf symbol for a list of applied traits ("is").
func traitRefsToSymbol(traits []*raml.Trait) protocol.DocumentSymbol {
	names := make([]string, 0, len(traits))
	var pos stacktrace.Position
	for _, t := range traits {
		if t == nil {
			continue
		}
		names = append(names, t.Name)
		if pos.Line == 0 {
			pos = t.ValuePos
		}
	}
	rng := RamlPosToRangeNamed(pos.Line, pos.Column, 2) // "is"
	return protocol.DocumentSymbol{
		Name:           "is",
		Detail:         ptrTo(strings.Join(names, ", ")),
		Kind:           protocol.SymbolKindKey,
		Range:          rng,
		SelectionRange: rng,
	}
}

// documentationSectionSymbol creates a "documentation" section with each
// documentation item's title as a child.
func documentationSectionSymbol(items []*raml.DocumentationItem) protocol.DocumentSymbol {
	children := make([]protocol.DocumentSymbol, 0, len(items))
	var firstPos stacktrace.Position
	for _, item := range items {
		if item == nil {
			continue
		}
		if firstPos.Line == 0 {
			firstPos = item.KeyPos
		}
		title := ""
		var pos stacktrace.Position
		if item.Title != nil {
			title = item.Title.Value
			pos = item.Title.KeyPos
		}
		if pos.Line == 0 {
			pos = item.KeyPos
		}
		rng := RamlPosToRangeNamed(pos.Line, pos.Column, len(title))
		children = append(children, protocol.DocumentSymbol{
			Name:           title,
			Kind:           protocol.SymbolKindString,
			Range:          rng,
			SelectionRange: rng,
		})
	}
	rng := protocol.Range{}
	if firstPos.Line > 0 {
		rng = RamlPosToRangeNamed(firstPos.Line, firstPos.Column, len("documentation"))
	}
	return protocol.DocumentSymbol{
		Name:           "documentation",
		Kind:           protocol.SymbolKindKey,
		Range:          rng,
		SelectionRange: rng,
		Children:       children,
	}
}

// shapeDetail returns a concise type expression for display in the outline.
// TypeExpr holds the raw annotation (e.g. "string | number", "string[]");
// Type holds the resolved kind ("object", "string", "array", …).
func shapeDetail(base *raml.BaseShape) string {
	if base.TypeExpr != nil {
		return base.TypeExpr.Value
	}
	return base.Type
}

// shapeToSymbol converts a BaseShape to a DocumentSymbol with nested children
// for object properties.
func shapeToSymbol(base *raml.BaseShape) protocol.DocumentSymbol {
	kind := shapeSymbolKind(base)
	nameLen := len(base.Name)
	rng := RamlPosToRangeNamed(base.KeyPos.Line, base.KeyPos.Column, nameLen)
	detail := shapeDetail(base)

	sym := protocol.DocumentSymbol{
		Name:           base.Name,
		Detail:         ptrTo(detail),
		Kind:           kind,
		Range:          rng,
		SelectionRange: rng,
	}

	if obj, ok := base.Shape.(*raml.ObjectShape); ok && obj != nil {
		sym.Children = propertiesAsSymbols(obj.Properties)
	}

	return sym
}

func propertiesAsSymbols(props *orderedmap.OrderedMap[string, raml.Property]) []protocol.DocumentSymbol {
	l := props.Len()
	if l == 0 {
		return nil
	}
	children := make([]protocol.DocumentSymbol, 0, l)
	for pair := props.Oldest(); pair != nil; pair = pair.Next() {
		p := &pair.Value
		base := p.Base
		if base == nil {
			continue
		}
		// Append "?" to signal optional properties, matching RAML notation.
		name := pair.Key
		if !p.Required {
			name += "?"
		}
		rng := RamlPosToRangeNamed(base.KeyPos.Line, base.KeyPos.Column, len(name))
		detail := shapeDetail(base)

		child := protocol.DocumentSymbol{
			Name:           name,
			Detail:         ptrTo(detail),
			Kind:           protocol.SymbolKindField,
			Range:          rng,
			SelectionRange: rng,
		}
		// Recurse into nested object properties.
		if obj, ok := base.Shape.(*raml.ObjectShape); ok && obj != nil {
			child.Children = propertiesAsSymbols(obj.Properties)
		}
		children = append(children, child)
	}
	return children
}

func libraryLinkToSymbol(name string, link *raml.LibraryLink) protocol.DocumentSymbol {
	rng := protocol.Range{}
	if link != nil {
		rng = RamlPosToRangeNamed(link.KeyPos.Line, link.KeyPos.Column, len(name))
	}
	detail := ""
	if link != nil {
		detail = link.Value
	}
	return protocol.DocumentSymbol{
		Name:           name,
		Detail:         ptrTo(detail),
		Kind:           protocol.SymbolKindNamespace,
		Range:          rng,
		SelectionRange: rng,
	}
}

func securitySchemeToSymbol(name string, ssd *raml.SecuritySchemeDefinition) protocol.DocumentSymbol {
	rng := protocol.Range{}
	if ssd != nil {
		rng = RamlPosToRangeNamed(ssd.KeyPos.Line, ssd.KeyPos.Column, len(name))
	}
	detail := ""
	if ssd != nil && ssd.Type != nil {
		detail = string(ssd.Type.Value)
	}
	return protocol.DocumentSymbol{
		Name:           name,
		Detail:         ptrTo(detail),
		Kind:           protocol.SymbolKindKey,
		Range:          rng,
		SelectionRange: rng,
	}
}

func resourceTypeToSymbol(name string, rt *raml.ResourceTypeDefinition) protocol.DocumentSymbol {
	rng := protocol.Range{}
	if rt != nil {
		rng = RamlPosToRangeNamed(rt.KeyPos.Line, rt.KeyPos.Column, len(name))
	}
	return protocol.DocumentSymbol{
		Name:           name,
		Kind:           protocol.SymbolKindClass,
		Range:          rng,
		SelectionRange: rng,
	}
}

func traitToSymbol(name string, td *raml.TraitDefinition) protocol.DocumentSymbol {
	rng := protocol.Range{}
	if td != nil {
		rng = RamlPosToRangeNamed(td.KeyPos.Line, td.KeyPos.Column, len(name))
	}
	return protocol.DocumentSymbol{
		Name:           name,
		Kind:           protocol.SymbolKindInterface,
		Range:          rng,
		SelectionRange: rng,
	}
}

func endpointToSymbol(ep *raml.EndPoint, filePath string) protocol.DocumentSymbol {
	name := ep.FullURI
	if name == "" {
		name = ep.URI
	}
	rng := RamlPosToRangeNamed(ep.KeyPos.Line, ep.KeyPos.Column, len(name))

	sym := protocol.DocumentSymbol{
		Name:           name,
		Kind:           protocol.SymbolKindModule,
		Range:          rng,
		SelectionRange: rng,
	}
	if ep.DisplayName != nil {
		sym.Detail = ptrTo(ep.DisplayName.Value)
	}

	// Applied resource type: "type: <name>"
	if ep.ResourceType != nil && ep.ResourceType.Name != "" {
		rt := ep.ResourceType
		rtRng := RamlPosToRangeNamed(rt.ValuePos.Line, rt.ValuePos.Column, len("type"))
		sym.Children = append(sym.Children, protocol.DocumentSymbol{
			Name:           "type",
			Detail:         ptrTo(rt.Name),
			Kind:           protocol.SymbolKindString,
			Range:          rtRng,
			SelectionRange: rtRng,
		})
	}

	// Applied traits: "is: [trait1, trait2]"
	if len(ep.Traits) > 0 {
		sym.Children = append(sym.Children, traitRefsToSymbol(ep.Traits))
	}

	// Endpoint-level securedBy: "security: [scheme1]"
	if len(ep.SecuredBy) > 0 {
		sym.Children = append(sym.Children, securedByToSymbol("security", ep.SecuredBy))
	}

	// Operations (HTTP methods)
	for pair := ep.Operations.Oldest(); pair != nil; pair = pair.Next() {
		sym.Children = append(sym.Children, operationToSymbol(pair.Value, filePath))
	}

	// Nested sub-endpoints
	for pair := ep.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		sym.Children = append(sym.Children, endpointToSymbol(pair.Value, filePath))
	}

	return sym
}

func operationToSymbol(op *raml.Operation, filePath string) protocol.DocumentSymbol {
	// Use displayName as the symbol name when set; fall back to the HTTP method.
	// This matches the ALS outline which labels operations by their human-readable name.
	name := op.Method
	detail := ""
	if op.DisplayName != nil && op.DisplayName.Value != "" {
		name = op.DisplayName.Value
		detail = op.Method
	}

	opRng := RamlPosToRangeNamed(op.KeyPos.Line, op.KeyPos.Column, len(op.Method))
	sym := protocol.DocumentSymbol{
		Name:           name,
		Detail:         ptrTo(detail),
		Kind:           protocol.SymbolKindClass,
		Range:          opRng,
		SelectionRange: opRng,
	}

	// Operation-level securedBy
	if len(op.SecuredBy) > 0 {
		sym.Children = append(sym.Children, securedByToSymbol("security", op.SecuredBy))
	}

	// Applied traits ("is")
	if len(op.Traits) > 0 {
		sym.Children = append(sym.Children, traitRefsToSymbol(op.Traits))
	}

	// Request (headers, bodies, query parameters)
	if reqSym, ok := operationRequestSymbol(op, filePath, opRng); ok {
		sym.Children = append(sym.Children, reqSym)
	}

	// Responses
	for pair := op.Responses.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value != nil {
			sym.Children = append(sym.Children, responseToSymbol(pair.Value, filePath, opRng))
		}
	}

	return sym
}

// operationRequestSymbol creates a synthetic "Request" symbol that groups an
// operation's headers, query parameters, and request body.  Returns false when
// the operation has no request-level content worth showing.
// fallback is the operation's own range, used for sub-symbols whose position
// originates from an external file (e.g. merged from a trait).
func operationRequestSymbol(op *raml.Operation, filePath string, fallback protocol.Range) (protocol.DocumentSymbol, bool) {
	hasHeaders := op.Headers != nil && op.Headers.Len() > 0
	hasQueryParams := op.QueryParameters != nil && op.QueryParameters.Len() > 0
	hasBodies := op.Request != nil && op.Request.Bodies != nil && op.Request.Bodies.Len() > 0

	if !hasHeaders && !hasQueryParams && !hasBodies {
		return protocol.DocumentSymbol{}, false
	}

	// Use the operation's value position as the "Request" node location.
	reqRng := RamlPosToRangeNamed(op.ValuePos.Line, op.ValuePos.Column, len("Request"))
	reqSym := protocol.DocumentSymbol{
		Name:           "Request",
		Kind:           protocol.SymbolKindObject,
		Range:          reqRng,
		SelectionRange: reqRng,
	}

	// Headers sub-container: "{} Headers"
	if hasHeaders {
		reqSym.Children = append(reqSym.Children, headersToSymbol(op.Headers, filePath, fallback))
	}

	// Query parameters sub-container: "{} Query"
	if hasQueryParams {
		reqSym.Children = append(reqSym.Children, queryParamsToSymbol(op.QueryParameters, filePath, fallback))
	}

	// Body container with typed media types
	if hasBodies {
		reqSym.Children = append(reqSym.Children, bodyContainerSymbol(op.Request.Bodies, filePath, fallback))
	}

	return reqSym, true
}

// responseToSymbol creates a symbol for an HTTP response code with optional
// headers and body children.
// fallback is the parent operation's range, used when the response (or its
// children) originates from an external file such as a trait.
func responseToSymbol(resp *raml.Response, filePath string, fallback protocol.Range) protocol.DocumentSymbol {
	code := strconv.Itoa(resp.StatusCode)
	var rng protocol.Range
	if resp.Location == filePath {
		rng = RamlPosToRangeNamed(resp.Line, resp.Column, len(code))
	} else {
		rng = fallback
	}
	sym := protocol.DocumentSymbol{
		Name:           code,
		Kind:           protocol.SymbolKindEvent,
		Range:          rng,
		SelectionRange: rng,
	}
	if resp.DisplayName != "" {
		sym.Detail = ptrTo(resp.DisplayName)
	}

	if resp.Headers != nil && resp.Headers.Len() > 0 {
		sym.Children = append(sym.Children, headersToSymbol(resp.Headers, filePath, rng))
	}
	if resp.Bodies != nil && resp.Bodies.Len() > 0 {
		sym.Children = append(sym.Children, bodyContainerSymbol(resp.Bodies, filePath, rng))
	}

	return sym
}

// bodyContainerSymbol creates a "{} Body" container whose children are the
// media types, each annotated with their declared type.
// fallback is used for items whose position originates from an external file.
func bodyContainerSymbol(bodies *orderedmap.OrderedMap[string, *raml.Body], filePath string, fallback protocol.Range) protocol.DocumentSymbol {
	children := make([]protocol.DocumentSymbol, 0, bodies.Len())
	var containerRng protocol.Range
	const containerName = "{} Body"
	for pair := bodies.Oldest(); pair != nil; pair = pair.Next() {
		mediaType := pair.Key
		body := pair.Value
		if body == nil {
			continue
		}
		detail := ""
		if body.Shape != nil {
			detail = shapeDetail(body.Shape)
		}
		var bRng protocol.Range
		if body.Location == filePath {
			bRng = RamlPosToRangeNamed(body.KeyPos.Line, body.KeyPos.Column, len(mediaType))
			if containerRng == (protocol.Range{}) {
				containerRng = RamlPosToRangeNamed(body.KeyPos.Line, body.KeyPos.Column, len(containerName))
			}
		} else {
			bRng = fallback
		}
		children = append(children, protocol.DocumentSymbol{
			Name:           mediaType,
			Detail:         ptrTo(detail),
			Kind:           protocol.SymbolKindObject,
			Range:          bRng,
			SelectionRange: bRng,
		})
	}
	if containerRng == (protocol.Range{}) {
		containerRng = fallback
	}
	return protocol.DocumentSymbol{
		Name:           containerName,
		Kind:           protocol.SymbolKindNamespace,
		Range:          containerRng,
		SelectionRange: containerRng,
		Children:       children,
	}
}

// propertyContainerToSymbol builds a named container symbol (e.g. "{} Headers",
// "{} Query") from an ordered map of Properties. Items whose position comes
// from an external file use fallback as their range.
func propertyContainerToSymbol(
	containerName string,
	childKind protocol.SymbolKind,
	props *orderedmap.OrderedMap[string, raml.Property],
	filePath string,
	fallback protocol.Range,
) protocol.DocumentSymbol {
	children := make([]protocol.DocumentSymbol, 0, props.Len())
	var containerRng protocol.Range
	for pair := props.Oldest(); pair != nil; pair = pair.Next() {
		p := &pair.Value
		base := p.Base
		if base == nil {
			continue
		}
		var rng protocol.Range
		if base.Location == filePath {
			rng = RamlPosToRangeNamed(base.KeyPos.Line, base.KeyPos.Column, len(base.Name))
			if containerRng == (protocol.Range{}) {
				containerRng = RamlPosToRangeNamed(base.KeyPos.Line, base.KeyPos.Column, len(containerName))
			}
		} else {
			rng = fallback
		}
		children = append(children, protocol.DocumentSymbol{
			Name:           base.Name,
			Detail:         ptrTo(shapeDetail(base)),
			Kind:           childKind,
			Range:          rng,
			SelectionRange: rng,
		})
	}
	if containerRng == (protocol.Range{}) {
		containerRng = fallback
	}
	return protocol.DocumentSymbol{
		Name:           containerName,
		Kind:           protocol.SymbolKindNamespace,
		Range:          containerRng,
		SelectionRange: containerRng,
		Children:       children,
	}
}

// headersToSymbol creates a "{} Headers" container symbol with each header as a child.
func headersToSymbol(headers *orderedmap.OrderedMap[string, raml.Property], filePath string, fallback protocol.Range) protocol.DocumentSymbol {
	return propertyContainerToSymbol("{} Headers", protocol.SymbolKindField, headers, filePath, fallback)
}

// queryParamsToSymbol creates a "{} Query" container symbol with each parameter as a child.
func queryParamsToSymbol(params *orderedmap.OrderedMap[string, raml.Property], filePath string, fallback protocol.Range) protocol.DocumentSymbol {
	return propertyContainerToSymbol("{} Query", protocol.SymbolKindVariable, params, filePath, fallback)
}

func shapeSymbolKind(base *raml.BaseShape) protocol.SymbolKind {
	if base.Shape == nil {
		return protocol.SymbolKindVariable
	}
	switch base.Shape.(type) {
	case *raml.ObjectShape:
		return protocol.SymbolKindClass
	case *raml.UnionShape:
		return protocol.SymbolKindInterface
	case *raml.ArrayShape:
		return protocol.SymbolKindArray
	default:
		return protocol.SymbolKindVariable
	}
}
