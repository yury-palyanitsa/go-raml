package server

import (
	"os"
	"path/filepath"
	"strings"

	raml "github.com/acronis/go-raml/v3"
	protocol "github.com/tliron/glsp/protocol_3_16"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

func kindPtr(k protocol.CompletionItemKind) *protocol.CompletionItemKind {
	kk := k
	return &kk
}

func detailPtr(s string) *string { return &s }

// typeExprCompletions returns completion items for a type expression context.
func typeExprCompletions(r *raml.RAML, filePath string) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	// Built-in scalars.
	for _, name := range builtInTypeNames {
		items = append(items, protocol.CompletionItem{
			Label: name,
			Kind:  kindPtr(protocol.CompletionItemKindKeyword),
		})
	}

	// Local types.
	for name, base := range r.GetFragmentTypePtrs(filePath) {
		item := typeNameToItem(name, base, protocol.CompletionItemKindClass)
		items = append(items, item)
	}

	// Local annotation types.
	for name, base := range r.GetAnnotationTypeDefinitionsFromFragment(filePath) {
		item := typeNameToItem(name, base, protocol.CompletionItemKindInterface)
		items = append(items, item)
	}

	// Library-imported types: `alias.TypeName`
	frag := r.GetFragment(filePath)
	for pair := usesFrom(frag).Oldest(); pair != nil; pair = pair.Next() {
		alias := pair.Key
		link := pair.Value
		if link == nil || link.Link == nil {
			continue
		}
		lib := link.Link
		for tp := lib.Types.Oldest(); tp != nil; tp = tp.Next() {
			label := alias + "." + tp.Key
			item := typeNameToItem(label, tp.Value, protocol.CompletionItemKindClass)
			item.Detail = detailPtr(alias)
			items = append(items, item)
		}
	}

	return items
}

// statusCodeCompletions returns completion items for HTTP status codes.
// Used when the cursor is typing a key directly inside a `responses:` block.
func statusCodeCompletions() []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(httpStatusPhrases))
	for code, phrase := range httpStatusPhrases {
		detail := phrase
		item := protocol.CompletionItem{
			Label:    code,
			Kind:     kindPtr(protocol.CompletionItemKindEnumMember),
			Detail:   &detail,
			SortText: &code,
		}
		if doc := statusCodeDoc(code); doc != "" {
			item.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: doc}
		}
		items = append(items, item)
	}
	return items
}

// typedObjectKeyCompletions returns CompletionItems for the property keys of
// an object shape. To be used when the cursor is typing a key inside a
// structured annotation (CDP) or custom-facet value body.
//
// existing is the set of sibling keys already present at the cursor indent;
// those properties are excluded from the results.  Required properties sort
// before optional ones.
func typedObjectKeyCompletions(shape *raml.BaseShape, existing map[string]struct{}) []protocol.CompletionItem {
	props := resolveObjectProperties(shape)
	if props == nil {
		return nil
	}
	items := make([]protocol.CompletionItem, 0, props.Len())
	for pair := props.Oldest(); pair != nil; pair = pair.Next() {
		if _, skip := existing[pair.Key]; skip {
			continue
		}
		prop := &pair.Value
		item := protocol.CompletionItem{
			Label: pair.Key,
			Kind:  kindPtr(protocol.CompletionItemKindProperty),
		}
		if prop.Base != nil {
			typeName := prop.Base.Type
			if typeName != "" {
				suffix := ""
				if !prop.Required {
					suffix = "?"
				}
				detail := typeName + suffix
				item.Detail = &detail
			}
			if prop.Base.Description != nil && prop.Base.Description.Value != "" {
				item.Documentation = protocol.MarkupContent{
					Kind:  protocol.MarkupKindMarkdown,
					Value: prop.Base.Description.Value,
				}
			}
		}
		// Required properties sort before optional ones.
		sortPrefix := "1"
		if !prop.Required {
			sortPrefix = "2"
		}
		sortText := sortPrefix + pair.Key
		item.SortText = &sortText
		items = append(items, item)
	}
	return items
}

// libTypeCompletions returns types from the specific library identified by alias.
func libTypeCompletions(r *raml.RAML, filePath string, alias string) []protocol.CompletionItem {
	frag := r.GetFragment(filePath)
	uses := usesFrom(frag)
	link, ok := uses.Get(alias)
	if !ok || link == nil || link.Link == nil {
		return nil
	}
	lib := link.Link
	items := make([]protocol.CompletionItem, 0, lib.Types.Len()+lib.AnnotationTypes.Len())
	for pair := lib.Types.Oldest(); pair != nil; pair = pair.Next() {
		item := typeNameToItem(pair.Key, pair.Value, protocol.CompletionItemKindClass)
		items = append(items, item)
	}
	for pair := lib.AnnotationTypes.Oldest(); pair != nil; pair = pair.Next() {
		item := typeNameToItem(pair.Key, pair.Value, protocol.CompletionItemKindInterface)
		items = append(items, item)
	}
	return items
}

// keyCompletions returns structural RAML key suggestions for the given context
// entry. fk drives root-level key set selection. Entry resolution and
// free-form-map suppression are already handled by analyzePosition.
func keyCompletions(entry *ContextEntry, fk raml.FragmentKind, lspLine uint32, lines docLines) []protocol.CompletionItem {
	keys := keysForCtx(entry, fk)
	if len(keys) == 0 {
		return nil
	}

	existing := existingKeysAt(lines, int(lspLine))

	// Context-specific doc overrides: some keys carry different semantics
	// depending on the enclosing node kind (e.g. "type" means the base type in
	// a type declaration but means the resource type in an endpoint block).
	var contextDocs map[string]string
	if entry != nil {
		contextDocs = ramlKeyDocsByKind[entry.Kind]
	}

	items := make([]protocol.CompletionItem, 0, len(keys))
	for _, k := range keys {
		if _, ok := existing[k.label]; ok {
			continue
		}
		item := protocol.CompletionItem{
			Label:  k.label,
			Kind:   kindPtr(protocol.CompletionItemKindKeyword),
			Detail: detailPtr(k.detail),
		}
		doc := contextDocs[k.label]
		if doc == "" {
			doc = ramlKeyDocs[k.label]
		}
		if doc != "" {
			item.Documentation = protocol.MarkupContent{
				Kind:  protocol.MarkupKindMarkdown,
				Value: doc,
			}
		}
		items = append(items, item)
	}
	return items
}

// fragKindFrom maps a parsed RAML fragment to a FragmentKind for root-key selection.
func fragKindFrom(r *raml.RAML, filePath string) raml.FragmentKind {
	if r == nil {
		return raml.FragmentAPI
	}
	switch r.GetFragment(filePath).(type) {
	case *raml.Library:
		return raml.FragmentLibrary
	case *raml.DataTypeFragment:
		return raml.FragmentDataType
	default:
		return raml.FragmentAPI
	}
}

// fragKindFromLines infers the fragment kind from the first document line using
// go-raml's canonical IdentifyFragment; fallback when no parse result is
// available yet. The RAML header must appear on the very first line.
func fragKindFromLines(lines docLines) raml.FragmentKind {
	if len(lines) == 0 {
		return raml.FragmentAPI
	}
	kind, _ := raml.IdentifyFragment(strings.TrimRight(lines[0].raw, " "))
	return kind
}

// keysForCtx returns the key list appropriate for the given context entry.
// entry is nil when cursor is at the fragment root level.
func keysForCtx(entry *ContextEntry, fk raml.FragmentKind) []ramlKey {
	if entry == nil {
		switch fk {
		case raml.FragmentLibrary:
			return libraryRootKeys
		case raml.FragmentDataType:
			return typeKeysForShape("")
		default:
			return apiRootKeys
		}
	}
	switch entry.Kind {
	case CKType, CKProperty:
		return typeKeysForShape(entry.ShapeType)
	case CKEndpoint:
		return endpointKeys
	case CKOperation, CKTrait:
		return methodBodyKeys
	case CKResponse:
		return responseKeys
	case CKSecurityScheme:
		return securitySchemeKeys
	default:
		return nil
	}
}

// typeKeysForShape returns the facet keys valid for a given RAML type.
// An empty shapeType (not yet resolved) returns all type facets.
//
// The per-type facet sets below correspond to raml.typeSpecificFacets in
// consts.go. When go-raml adds or renames facets, both must be kept in sync.
func typeKeysForShape(shapeType string) []ramlKey {
	switch shapeType {
	case raml.TypeObject:
		return objectTypeKeys
	case raml.TypeArray:
		return arrayTypeKeys
	case raml.TypeString:
		return stringTypeKeys
	case raml.TypeNumber, raml.TypeInteger:
		return numberTypeKeys
	case raml.TypeDatetime, raml.TypeDatetimeOnly:
		return datetimeTypeKeys
	case raml.TypeFile:
		return fileTypeKeys
	default:
		return commonTypeKeys
	}
}

// ---- Static key tables ----

func includePathCompletions(baseDir, partialPath string, pos protocol.Position, pathStartChar uint32, filterExts ...string) []protocol.CompletionItem {
	// Determine the filesystem directory to enumerate.
	osPart := filepath.FromSlash(partialPath)
	lookupDir := filepath.Join(baseDir, filepath.Dir(osPart))

	// Prefix to prepend to each entry name to form the full relative path label.
	dirPrefix := ""
	if i := strings.LastIndex(partialPath, "/"); i >= 0 {
		dirPrefix = partialPath[:i+1]
	}

	entries, err := os.ReadDir(lookupDir)
	if err != nil {
		return nil
	}

	// TextEdit replaces the already-typed path fragment with the chosen path.
	editRange := protocol.Range{
		Start: protocol.Position{Line: pos.Line, Character: pathStartChar},
		End:   pos,
	}

	var items []protocol.CompletionItem
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue // skip hidden entries
		}
		var label string
		var kind protocol.CompletionItemKind
		if entry.IsDir() {
			label = dirPrefix + name + "/"
			kind = protocol.CompletionItemKindFolder
		} else {
			if len(filterExts) > 0 {
				ext := strings.ToLower(filepath.Ext(name))
				matched := false
				for _, fe := range filterExts {
					if ext == fe {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
			label = dirPrefix + name
			kind = protocol.CompletionItemKindFile
		}
		items = append(items, protocol.CompletionItem{
			Label:      label,
			Kind:       kindPtr(kind),
			FilterText: &label,
			TextEdit: protocol.TextEdit{
				Range:   editRange,
				NewText: label,
			},
		})
	}
	return items
}

// typeNameToItem constructs a CompletionItem for a named type.
func typeNameToItem(name string, base *raml.BaseShape, kind protocol.CompletionItemKind) protocol.CompletionItem {
	item := protocol.CompletionItem{
		Label: name,
		Kind:  kindPtr(kind),
	}
	if base != nil {
		if base.Type != "" && base.Type != raml.TypeString {
			item.Detail = detailPtr(base.Type)
		}
		if base.DisplayName != nil && base.DisplayName.Value != "" && base.DisplayName.Value != name {
			doc := "*" + base.DisplayName.Value + "*"
			if base.Description != nil && base.Description.Value != "" {
				doc += "\n\n" + base.Description.Value
			}
			item.Documentation = protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: doc}
		} else if base.Description != nil && base.Description.Value != "" {
			item.Documentation = base.Description.Value
		}
	}
	return item
}

// usesFrom extracts the Uses ordered-map from a fragment (Library or APIFragment).
// Returns an empty non-nil map when not applicable.
func usesFrom(frag raml.Fragment) *orderedmap.OrderedMap[string, *raml.LibraryLink] {
	if frag == nil {
		return orderedmap.New[string, *raml.LibraryLink]()
	}
	switch f := frag.(type) {
	case *raml.Library:
		if f.Uses != nil {
			return f.Uses
		}
	case *raml.APIFragment:
		if f.Uses != nil {
			return f.Uses
		}
	}
	return orderedmap.New[string, *raml.LibraryLink]()
}

func boolCompletions() []protocol.CompletionItem {
	return []protocol.CompletionItem{
		{Label: "true", Kind: kindPtr(protocol.CompletionItemKindKeyword)},
		{Label: "false", Kind: kindPtr(protocol.CompletionItemKindKeyword)},
	}
}

// enumKindCompletions wraps a string slice into CompletionItems with the
// given kind.
func enumKindCompletions(values []string, kind protocol.CompletionItemKind) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, len(values))
	for i, v := range values {
		items[i] = protocol.CompletionItem{Label: v, Kind: kindPtr(kind)}
	}
	return items
}

// securedByNameCompletions returns the names of all security schemes declared
// in the file identified by filePath inside r.
func securedByNameCompletions(r *raml.RAML, filePath string) []protocol.CompletionItem {
	if r == nil {
		return nil
	}
	frag := r.GetFragment(filePath)
	var schemes *orderedmap.OrderedMap[string, *raml.SecuritySchemeDefinition]
	switch f := frag.(type) {
	case *raml.APIFragment:
		schemes = f.SecuritySchemes
	case *raml.Library:
		schemes = f.SecuritySchemes
	}
	if l := schemes.Len(); l > 0 {
		items := make([]protocol.CompletionItem, 0, l)
		for pair := schemes.Oldest(); pair != nil; pair = pair.Next() {
			items = append(items, protocol.CompletionItem{
				Label: pair.Key,
				Kind:  kindPtr(protocol.CompletionItemKindValue),
			})
		}
		return items
	}
	return nil
}

// traitNameCompletions returns the names of all traits declared in the file.
func traitNameCompletions(r *raml.RAML, filePath string) []protocol.CompletionItem {
	if r == nil {
		return nil
	}
	frag := r.GetFragment(filePath)
	var traits *orderedmap.OrderedMap[string, *raml.TraitDefinition]
	switch f := frag.(type) {
	case *raml.APIFragment:
		traits = f.Traits
	case *raml.Library:
		traits = f.Traits
	}
	if l := traits.Len(); l > 0 {
		items := make([]protocol.CompletionItem, 0, l)
		for pair := traits.Oldest(); pair != nil; pair = pair.Next() {
			items = append(items, protocol.CompletionItem{
				Label: pair.Key,
				Kind:  kindPtr(protocol.CompletionItemKindValue),
			})
		}
		return items
	}
	return nil
}

// resourceTypeNameCompletions returns the names of all resource types declared
// in the file.
// `resourceTypes:` block and returns its direct child keys as completion items.
func textResourceTypeNames(lines docLines) []protocol.CompletionItem {
	inBlock := false
	var items []protocol.CompletionItem
	for _, li := range lines {
		if li.trimmed == "" {
			continue
		}
		if li.indent == 0 {
			if strings.HasPrefix(li.trimmed, "resourceTypes:") {
				inBlock = true
				continue
			}
			inBlock = false
			continue
		}
		if inBlock && li.indent == 2 {
			if !strings.HasPrefix(li.trimmed, "- ") && !strings.HasPrefix(li.trimmed, "#") {
				if idx := strings.IndexByte(li.trimmed, ':'); idx > 0 {
					items = append(items, protocol.CompletionItem{
						Label: li.trimmed[:idx],
						Kind:  kindPtr(protocol.CompletionItemKindValue),
					})
				}
			}
		}
	}
	return items
}

func resourceTypeNameCompletions(r *raml.RAML, filePath string) []protocol.CompletionItem {
	if r == nil {
		return nil
	}
	frag := r.GetFragment(filePath)
	var rts *orderedmap.OrderedMap[string, *raml.ResourceTypeDefinition]
	switch f := frag.(type) {
	case *raml.APIFragment:
		rts = f.ResourceTypes
	case *raml.Library:
		rts = f.ResourceTypes
	}
	if l := rts.Len(); l > 0 {
		items := make([]protocol.CompletionItem, 0, l)
		for pair := rts.Oldest(); pair != nil; pair = pair.Next() {
			items = append(items, protocol.CompletionItem{
				Label: pair.Key,
				Kind:  kindPtr(protocol.CompletionItemKindValue),
			})
		}
		return items
	}
	return nil
}

// scalarValueCompletions returns completion items for the value side of a
// constrained RAML facet. The required ValueKind is pre-computed by
// analyzePosition (step 6c), so this function is a pure lookup with no
// string comparisons. r may be nil; static completions are still returned.
func scalarValueCompletions(cp CursorPosition, r *raml.RAML, filePath string, lines docLines) []protocol.CompletionItem {
	switch cp.ValueKind {
	case ValueKindBool:
		return boolCompletions()
	case ValueKindFormat:
		return enumKindCompletions(ramlFormatValues, protocol.CompletionItemKindEnumMember)
	case ValueKindProtocols:
		return enumKindCompletions(ramlProtocolValues, protocol.CompletionItemKindEnumMember)
	case ValueKindMediaType:
		return enumKindCompletions(commonMediaTypes, protocol.CompletionItemKindValue)
	case ValueKindSecuredBy:
		if r != nil {
			return securedByNameCompletions(r, filePath)
		}
	case ValueKindIs:
		if r != nil {
			return traitNameCompletions(r, filePath)
		}
	case ValueKindResourceType:
		if r != nil {
			if items := resourceTypeNameCompletions(r, filePath); len(items) > 0 {
				return items
			}
		}
		return textResourceTypeNames(lines)
	case ValueKindSecSchemeType:
		return securitySchemeTypeCompletions()
	}
	return nil
}

// securitySchemeTypeCompletions returns completion items for the `type:` facet
// inside a security scheme definition.
func securitySchemeTypeCompletions() []protocol.CompletionItem {
	return enumKindCompletions(types, protocol.CompletionItemKindEnumMember)
}

// customFacetKeysForEntry returns completion items for the custom facet names
// available when entry is a CKType node. Entry resolution is handled by
// analyzePosition before this function is called.
// existing is the set of keys already written in the type body; facet names
// present in it are excluded from the results.
func customFacetKeysForEntry(entry *ContextEntry, r *raml.RAML, filePath string, existing map[string]struct{}) []protocol.CompletionItem {
	if entry == nil || entry.Kind != CKType || entry.ShapeName == "" {
		return nil
	}
	shape, err := r.GetTypeFromFragmentPtr(filePath, entry.ShapeName)
	if err != nil || shape == nil {
		return nil
	}
	var items []protocol.CompletionItem
	// Seed seen with already-written keys so facets already present in the
	// document body are not offered again.
	seen := make(map[string]struct{}, len(existing))
	for k := range existing {
		seen[k] = struct{}{}
	}
	collectFacetDefItems(shape.Inherits, seen, &items)
	return items
}

// collectFacetDefItems recursively walks the inheritance chain collecting
// custom facet definition names as completion items.
func collectFacetDefItems(inherits []*raml.BaseShape, seen map[string]struct{}, items *[]protocol.CompletionItem) {
	for _, parent := range inherits {
		if parent == nil {
			continue
		}
		for pair := parent.CustomShapeFacetDefinitions.Oldest(); pair != nil; pair = pair.Next() {
			if _, ok := seen[pair.Key]; ok {
				continue
			}
			seen[pair.Key] = struct{}{}
			prop := &pair.Value
			item := protocol.CompletionItem{
				Label: pair.Key,
				Kind:  kindPtr(protocol.CompletionItemKindProperty),
			}
			if prop.Base != nil {
				if prop.Base.Type != "" {
					item.Detail = detailPtr(prop.Base.Type)
				}
				if prop.Base.Description != nil && prop.Base.Description.Value != "" {
					item.Documentation = protocol.MarkupContent{
						Kind:  protocol.MarkupKindMarkdown,
						Value: prop.Base.Description.Value,
					}
				}
			}
			*items = append(*items, item)
		}
		collectFacetDefItems(parent.Inherits, seen, items)
	}
}

// hasFacetInInherits reports whether any shape in the inheritance chain
// defines a custom facet with the given name.
func hasFacetInInherits(inherits []*raml.BaseShape, name string) bool {
	for _, parent := range inherits {
		if parent == nil {
			continue
		}
		if parent.CustomShapeFacetDefinitions != nil {
			if _, ok := parent.CustomShapeFacetDefinitions.Get(name); ok {
				return true
			}
		}
		if hasFacetInInherits(parent.Inherits, name) {
			return true
		}
	}
	return false
}

// annotationKeyCompletions returns completion items for annotation (CDP) keys
// in the form `(annotationName)` for all annotation types visible from filePath.
// These are offered alongside structural RAML keys in the ctxKey completion case.
func annotationKeyCompletions(r *raml.RAML, filePath string) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	// Local annotation types: `(MyAnnotation)`
	for name, base := range r.GetAnnotationTypeDefinitionsFromFragment(filePath) {
		label := "(" + name + ")"
		item := typeNameToItem(label, base, protocol.CompletionItemKindField)
		items = append(items, item)
	}

	// Library-imported annotation types: `(lib.MyAnnotation)`
	frag := r.GetFragment(filePath)
	for pair := usesFrom(frag).Oldest(); pair != nil; pair = pair.Next() {
		alias := pair.Key
		link := pair.Value
		if link == nil || link.Link == nil {
			continue
		}
		lib := link.Link
		for tp := lib.AnnotationTypes.Oldest(); tp != nil; tp = tp.Next() {
			label := "(" + alias + "." + tp.Key + ")"
			item := typeNameToItem(label, tp.Value, protocol.CompletionItemKindField)
			item.Detail = detailPtr(alias)
			items = append(items, item)
		}
	}
	return items
}
