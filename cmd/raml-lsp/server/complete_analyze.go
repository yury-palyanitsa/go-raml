package server

import (
	"regexp"
	"strings"

	raml "github.com/acronis/go-raml/v3"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ---- Position analysis ----

// CursorRole classifies the kind of completion the cursor position calls for.
type CursorRole int

const (
	// RoleSuppressed indicates no completions apply at this position (inside a
	// free-form map key slot, an annotation-value body, a custom-facet value
	// body, or a structural declaration that carries no completable token).
	RoleSuppressed CursorRole = iota
	// RoleKey means the cursor is typing a YAML mapping key.
	RoleKey
	// RoleTypeExpr means the cursor is typing a RAML type expression.
	RoleTypeExpr
	// RoleScalarValue means the cursor is on the value side of a key that takes
	// a constrained set of scalar values. ValueKind identifies which set.
	RoleScalarValue
	// RoleAnnotationName means the cursor is inside an opening parenthesis.
	RoleAnnotationName
	// RoleLibType means the cursor is after a library alias dot. LibAlias carries the alias.
	RoleLibType
	// RoleIncludePath means the cursor is on the path argument of a !include tag.
	// PartialPath and PathStart are set.
	RoleIncludePath
	// RoleUsesPath means the cursor is on the file-path value of a uses: entry.
	// PartialPath and PathStart are set.
	RoleUsesPath
	// RoleStatusCode means the cursor is typing an HTTP status code key directly
	// under a `responses:` block.
	RoleStatusCode
	// RoleTypedObjectKey means the cursor is typing a key inside a structured
	// annotation (CDP) or custom-facet value body. TypedShape carries the
	// expected object shape at the current nesting depth.
	RoleTypedObjectKey
)

// CursorPosition fully characterises the cursor for completion purposes.
// Produced by analyzePosition; consumed by the completion dispatch in server.go.
type CursorPosition struct {
	Role CursorRole
	// Entry is the innermost structural RAML node at the cursor (nil = root).
	Entry *ContextEntry
	// ValueKind identifies which constrained-value set to offer (RoleScalarValue).
	ValueKind ValueKind
	// FragKind is the fragment kind of the current document (RoleKey).
	// Pre-computed by analyzePosition so the dispatch needs no additional model query.
	FragKind raml.FragmentKind
	// LibAlias is the library alias prefix (RoleLibType).
	LibAlias string
	// PartialPath and PathStart are for RoleIncludePath and RoleUsesPath.
	PartialPath string
	PathStart   uint32
	// TypedShape is the expected object shape at the cursor level (RoleTypedObjectKey).
	TypedShape *raml.BaseShape
}

// ValueKind identifies the semantic kind of a constrained scalar value at a
// RoleScalarValue cursor position. Each constant corresponds to a specific
// RAML facet whose valid value set is enumerated or model-driven. Using a
// typed enum here eliminates string comparisons in scalarValueCompletions.
// See raml.SetOfIntegerFormats, raml.SetOfNumberFormats, raml.SecuritySchemeType, etc.
type ValueKind int

const (
	// ValueKindNone is the zero value; scalarValueCompletions returns nil.
	ValueKindNone ValueKind = iota
	// ValueKindBool is for required, additionalProperties, uniqueItems → true | false.
	ValueKindBool
	// ValueKindFormat is for format → integer formats (int32, int64, long …) or
	// number formats (float, double) depending on shape type.
	ValueKindFormat
	// ValueKindProtocols is for protocols → HTTP | HTTPS.
	ValueKindProtocols
	// ValueKindMediaType is for mediaType → common MIME type suggestions.
	ValueKindMediaType
	// ValueKindSecuredBy is for securedBy → security scheme names from the parsed model.
	ValueKindSecuredBy
	// ValueKindIs is for is → trait names from the parsed model.
	ValueKindIs
	// ValueKindResourceType is for type: inside an endpoint block → resource type names.
	ValueKindResourceType
	// ValueKindSecSchemeType is for type: inside a security scheme block
	// → raml.SecuritySchemeType constants (OAuth 2.0, Basic Authentication, …).
	ValueKindSecSchemeType
)

var (
	// Matches the library prefix in `lib.partial` at end of line.
	reLibDot = regexp.MustCompile(`\b(\w+)\.\w*$`)

	// Matches annotation position: ends with `(partial`
	reAnnotation = regexp.MustCompile(`\(\w*$`)

	// Matches a !include tag followed by at least one space and an optional path
	// at the end of the line, e.g. "!include schemas/" or "!include ".
	reIncludeTag = regexp.MustCompile(`!include\s+(\S*)$`)
)

// analyzePosition is the single entry point for all cursor-context analysis.
// It returns a fully-specified CursorPosition by combining:
//   - Syntactic context from the per-fragment SourceMap (YAML AST position data)
//   - Semantic context from the SourceMap's SourceInfo-driven Phase 2 annotations
//   - Structural context entries from the ContextIndex block ranges
//   - Text-based fallbacks for lines not covered by the SourceMap
//     (e.g. when OptWithRawSource was not set or the document is malformed)
//
// lineText is the document line up to (not including) the cursor character.
// lspLine is 0-based. lines is the full pre-parsed document. fc.CI, fc.RAML,
// and fc.FilePath may be nil/zero when the model has not yet been parsed.
func analyzePosition(
	lineText string,
	lspLine int,
	lines docLines,
	fc *FileContext,
) CursorPosition {
	ci := fc.CI
	r := fc.RAML
	filePath := fc.FilePath
	ramlLine := lspLine + 1 // LSP 0-based → RAML 1-based
	lspCol := len(lineText) // cursor column = number of chars up to cursor

	// ─ 0. RAML header line ────────────────────────────────────────────────────
	// The very first line (#%RAML 1.0 …) is never completable.
	if lspLine == 0 {
		fullLine := ""
		if len(lines) > 0 {
			fullLine = strings.TrimLeft(lines[0].raw, " \t")
		}
		if strings.HasPrefix(fullLine, "#%RAML") {
			return CursorPosition{Role: RoleSuppressed}
		}
	}

	// ─ 1. !include path ─────────────────────────────────────────────────────
	if partial, start, ok := detectIncludeArg(lineText); ok {
		return CursorPosition{Role: RoleIncludePath, PartialPath: partial, PathStart: start}
	}

	colonIdx := findYAMLKeyColon(lineText)

	// ─ 2. Uses: file-path value ────────────────────────────────────────────────
	if colonIdx >= 0 {
		if ci.IsUsesEntryLine(ramlLine) || isUnderUsesBlock(lines, lspLine) {
			rest := lineText[colonIdx+1:]
			trimOff := len(rest) - len(strings.TrimLeft(rest, " \t"))
			return CursorPosition{
				Role:        RoleUsesPath,
				PartialPath: rest[trimOff:],
				PathStart:   uint32(colonIdx + 1 + trimOff),
			}
		}
	}

	// ─ 3. Annotation name: cursor ends with `(word` ───────────────────────────
	if reAnnotation.MatchString(lineText) {
		return CursorPosition{Role: RoleAnnotationName}
	}

	// ─ 4. Library-qualified identifier: cursor ends with `alias.partial` ────────
	if m := reLibDot.FindStringSubmatch(lineText); m != nil {
		return CursorPosition{Role: RoleLibType, LibAlias: m[1]}
	}

	entry := resolveEntry(lspLine, lines, ci)

	// ─ 5. No colon → cursor is typing a YAML mapping key ─────────────────────
	if colonIdx < 0 {
		// Check the SourceMap: if this line has a recorded key and the cursor
		// is within the key name text, suppress (user is editing the key itself).
		if rec := ci.GetLineRecord(ramlLine); rec != nil && lspCol <= rec.KeyEndCol {
			return CursorPosition{Role: RoleSuppressed}
		}
		if freeParent := freeformParentKey(lines, lspLine); freeParent != "" {
			if freeParent == raml.FacetResponses {
				return CursorPosition{Role: RoleStatusCode, Entry: entry}
			}
			return CursorPosition{Role: RoleSuppressed}
		}
		parentKey, parentKeyLine := nearestParentKey(lines, lspLine)

		// Sequence item line (starts with `- `): if the nearest parent mapping
		// key holds a block-sequence enum or type-expression SourceMap record,
		// offer completions for that key's value type.
		//
		// Two sub-cases are handled:
		//   - Items more indented than their key: nearestParentKey finds it.
		//   - Items at the same indent as their key (valid YAML): nearestParentKey
		//     skips same-indent lines, so nearestSeqKey is tried as a fallback.
		if lspLine < len(lines) {
			if t := lines[lspLine].trimmed; t == "-" || strings.HasPrefix(t, "- ") {
				checkSeqParent := func(keyLine int) (CursorPosition, bool) {
					if keyLine < 0 {
						return CursorPosition{}, false
					}
					pkRec := ci.GetLineRecord(keyLine + 1)
					if pkRec.IsBlockSeqEnum() {
						return CursorPosition{Role: RoleScalarValue, ValueKind: pkRec.ValKind, Entry: entry}, true
					}
					if pkRec.isBlockSeqTypeExpr() {
						return CursorPosition{Role: RoleTypeExpr, Entry: entry}, true
					}
					return CursorPosition{}, false
				}
				if cp, ok := checkSeqParent(parentKeyLine); ok {
					return cp
				}
				_, seqKeyLine := nearestSeqKey(lines, lspLine)
				if seqKeyLine != parentKeyLine {
					if cp, ok := checkSeqParent(seqKeyLine); ok {
						return cp
					}
				}
			}
		}

		// Check whether the cursor is inside the value body of an annotation
		// (CDP) or custom-facet key using the YAML AST.  This correctly handles
		// same-indent block sequences, multi-level nesting, and all other YAML
		// structural forms.
		if r != nil {
			// cursorCol is 1-based (yaml.v3 convention).
			if rootKey, path := typedContainerAt(r, filePath, lspLine, lspCol+1, entry); rootKey != "" {
				shape := resolveAnnotationOrFacetShape(rootKey, path, entry, r, filePath)
				if shape != nil && resolveObjectProperties(shape) != nil {
					return CursorPosition{Role: RoleTypedObjectKey, TypedShape: shape}
				}
				return CursorPosition{Role: RoleSuppressed}
			}
		}

		if ci != nil && entry == nil && cursorIsIndented(lines, uint32(lspLine)) {
			return CursorPosition{Role: RoleSuppressed}
		}
		if parentKey != "" {
			if raml.IsCustomDomainExtensionNode(parentKey) || isCustomFacetValueOf(parentKey, entry, r, filePath) {
				return CursorPosition{Role: RoleSuppressed}
			}
		}
		fk := fragKindFromLines(lines)
		if r != nil {
			fk = fragKindFrom(r, filePath)
		}
		return CursorPosition{Role: RoleKey, Entry: entry, FragKind: fk}
	}

	// ─ 6. Cursor is on the value side (colonIdx >= 0) ────────────────────────
	key := strings.TrimSpace(lineText[:colonIdx])

	// 6a. SourceMap lookup — the authoritative, model+syntax aware path.
	//
	// This single check replaces the previous combination of:
	//   - IsTypeValueLine  (old step 6a, SourceInfo-driven scalar type expr lines)
	//   - IsFacetKeyLine   (old step 6b, SourceInfo-driven scalar facet key lines)
	//   - per-key switch   (old step 6c, constrained-value key names)
	//   - EntryAt heuristic (old step 6d, buggy CKType/CKProperty fallback)
	//
	// Failure modes addressed:
	//   - Cursor ≤ KeyEndCol: cursor is on the key text, not the value (Bug C/D).
	//   - !ValInline: value is a block mapping/sequence below this line; offering
	//     inline completions here is wrong (scalar-vs-mapping bug).
	//   - ValSemanticSuppressed: free-text field, no completions (Bug B).
	//   - ValSemanticTypeExpr + ValInline: genuine inline type expression (correct).
	//   - ValSemanticEnum + ValInline: constrained value set (correct).
	if rec := ci.GetLineRecord(ramlLine); rec != nil {
		// Cursor is still on the key name text (before or at the colon).
		if lspCol <= rec.KeyEndCol {
			return CursorPosition{Role: RoleSuppressed}
		}
		// Value is a block below the key line (e.g. `address:` with indented body).
		if !rec.ValInline {
			return CursorPosition{Role: RoleSuppressed}
		}
		// Semantic role is known from model or structural annotation.
		switch rec.Semantic {
		case ValSemanticSuppressed:
			return CursorPosition{Role: RoleSuppressed}
		case ValSemanticTypeExpr:
			return CursorPosition{Role: RoleTypeExpr, Entry: entry}
		case ValSemanticEnum:
			return CursorPosition{Role: RoleScalarValue, ValueKind: rec.ValKind, Entry: entry}
		}
		// ValSemanticUnknown: YAML AST has a record but annotateYAMLStructure and
		// the model walk both left it unannotated (unknown extension key, OAS-only
		// facet, etc.). Fall through to structural fallbacks.
	}

	// Structural context fallbacks (no SourceMap record available).
	// These fire when OptWithRawSource was not set or the YAML could not be
	// decoded — i.e. in unit tests or very early parses.
	if ci != nil {
		// Declaration-line check: cursor is on the key line of a structural block.
		// Endpoint/operation key lines carry a structural block value (never inline
		// completable). CKType/CKProperty key lines carry an inline type expression.
		if atEntry := ci.EntryAt(ramlLine); atEntry != nil {
			switch atEntry.Kind {
			case CKEndpoint, CKOperation:
				return CursorPosition{Role: RoleSuppressed}
			case CKType, CKProperty:
				return CursorPosition{Role: RoleTypeExpr, Entry: entry}
			}
		}
		// Inside any structural block: all remaining unknown keys are suppressed.
		if entry != nil {
			return CursorPosition{Role: RoleSuppressed}
		}
	}

	if strings.HasPrefix(key, "/") || raml.IsHTTPMethod(key) {
		return CursorPosition{Role: RoleSuppressed}
	}
	return CursorPosition{Role: RoleTypeExpr}
}

// resolveEntry returns the ContextEntry for lspLine, applying the LastBefore
// trailing-blank-line fallback so that a cursor on a blank line appended after
// the last facet of a type body still resolves to the enclosing type entry.
func resolveEntry(lspLine int, lines docLines, ci *ContextIndex) *ContextEntry {
	if ci == nil {
		return nil
	}
	ramlLine := lspLine + 1
	if entry := ci.Query(ramlLine); entry != nil {
		return entry
	}
	if !cursorIsIndented(lines, uint32(lspLine)) {
		return nil
	}
	fallback := ci.LastBefore(ramlLine)
	if fallback == nil {
		return nil
	}
	cursorIndent := 0
	if lspLine < len(lines) {
		cursorIndent = lines[lspLine].indent
	}
	entryKeyLSP := fallback.StartLine - 1 // RAML 1-based → LSP 0-based
	entryKeyIndent := 0
	if entryKeyLSP >= 0 && entryKeyLSP < len(lines) {
		entryKeyIndent = lines[entryKeyLSP].indent
	}
	if cursorIndent > entryKeyIndent {
		return fallback
	}
	return nil
}

// nearestParentKey returns the YAML mapping key name and its 0-based line
// index at the nearest non-blank line above lspLine that has strictly lower
// indentation. Returns ("", -1) when no such line exists.
func nearestParentKey(lines docLines, lspLine int) (string, int) {
	if lspLine >= len(lines) {
		return "", -1
	}
	curIndent := lines[lspLine].indent
	for i := lspLine - 1; i >= 0; i-- {
		li := lines[i]
		if li.trimmed == "" {
			continue
		}
		if li.indent >= curIndent {
			continue
		}
		if idx := strings.IndexByte(li.trimmed, ':'); idx > 0 {
			return li.trimmed[:idx], i
		}
		return "", i
	}
	return "", -1
}

// nearestSeqKey finds the mapping key that introduces the block sequence whose
// items appear at lspLine. Unlike nearestParentKey, it accepts a key at the
// same indentation as the items — YAML allows block-sequence indicators at the
// same column as their mapping key (e.g. `type:\n- A\n- B` all at indent 4).
// It scans upward, skips sibling sequence items at the same (or greater)
// indent, and returns the first mapping key it finds at indent ≤ curIndent.
// Returns ("", -1) when no such key is found.
func nearestSeqKey(lines docLines, lspLine int) (string, int) {
	if lspLine >= len(lines) {
		return "", -1
	}
	curIndent := lines[lspLine].indent
	for i := lspLine - 1; i >= 0; i-- {
		li := lines[i]
		if li.trimmed == "" {
			continue
		}
		// Skip sibling sequence items at the same or greater indent.
		if li.indent >= curIndent && (li.trimmed == "-" || strings.HasPrefix(li.trimmed, "- ")) {
			continue
		}
		if li.indent <= curIndent {
			if idx := strings.IndexByte(li.trimmed, ':'); idx > 0 {
				return li.trimmed[:idx], i
			}
		}
		return "", i
	}
	return "", -1
}

// isCustomFacetValueOf reports whether key names a custom facet that is
// inherited by the type described by entry. Returns false when r or entry
// are nil or when the type cannot be resolved.
func isCustomFacetValueOf(key string, entry *ContextEntry, r *raml.RAML, filePath string) bool {
	if entry == nil || r == nil || entry.ShapeName == "" {
		return false
	}
	shape, err := r.GetTypeFromFragmentPtr(filePath, entry.ShapeName)
	if err != nil || shape == nil {
		return false
	}
	return hasFacetInInherits(shape.Inherits, key)
}

// resolveAnnotationOrFacetShape resolves the root annotation or custom-facet
// shape identified by rootKey, then navigates it along path (produced by
// typedContainerAt) to return the BaseShape expected at the cursor level.
// Returns nil when the root shape cannot be resolved or the path is invalid.
func resolveAnnotationOrFacetShape(rootKey string, path []string, entry *ContextEntry, r *raml.RAML, filePath string) *raml.BaseShape {
	var rootShape *raml.BaseShape
	if raml.IsCustomDomainExtensionNode(rootKey) {
		inner := rootKey[1 : len(rootKey)-1] // strip parens
		if dotIdx := strings.IndexByte(inner, '.'); dotIdx >= 0 {
			alias, name := inner[:dotIdx], inner[dotIdx+1:]
			frag := r.GetFragment(filePath)
			link, ok := usesFrom(frag).Get(alias)
			if !ok || link == nil || link.Link == nil || link.Link.AnnotationTypes == nil {
				return nil
			}
			base, ok := link.Link.AnnotationTypes.Get(name)
			if !ok || base == nil {
				return nil
			}
			rootShape = base
		} else {
			shape, err := r.GetAnnotationTypeFromFragmentPtr(filePath, inner)
			if err != nil || shape == nil {
				return nil
			}
			rootShape = shape
		}
	} else {
		if entry == nil || entry.ShapeName == "" {
			return nil
		}
		shape, err := r.GetTypeFromFragmentPtr(filePath, entry.ShapeName)
		if err != nil || shape == nil {
			return nil
		}
		rootShape = findFacetDef(shape.Inherits, rootKey)
		if rootShape == nil {
			return nil
		}
	}
	return navigateTypedPath(rootShape, path)
}

// navigateTypedPath descends shape along path steps.
// "[]" steps unwrap one level of array (ArrayShape → Items).
// Any other step is a property key lookup in an ObjectShape.
// Returns nil if any step cannot be resolved.
func navigateTypedPath(shape *raml.BaseShape, path []string) *raml.BaseShape {
	cur := shape
	for _, step := range path {
		if cur == nil {
			return nil
		}
		if step == "[]" {
			cur = resolveArrayItems(cur)
		} else {
			props := resolveObjectProperties(cur)
			if props == nil {
				return nil
			}
			prop, ok := props.Get(step)
			if !ok {
				return nil
			}
			cur = prop.Base
		}
	}
	return cur
}

// findYAMLKeyColon returns the index of the YAML key-value separator colon in
// line, or -1 if none is found. The separator colon must be followed by a
// space, a tab, or the end of the string (cursor is immediately after it).
// Lines are expected to have been normalised by parseDoc (no trailing \r).
func findYAMLKeyColon(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			next := i + 1
			if next >= len(line) || line[next] == ' ' || line[next] == '\t' {
				return i
			}
		}
	}
	return -1
}

// getLineUpToCursor returns the content of the given line from the start up to
// (but not including) the cursor character offset.
func getLineUpToCursor(lines docLines, pos protocol.Position) string {
	lineIdx := int(pos.Line)
	if lineIdx >= len(lines) {
		return ""
	}
	raw := lines[lineIdx].raw
	col := int(pos.Character)
	if col > len(raw) {
		col = len(raw)
	}
	return raw[:col]
}

// ---- YAML scanning helpers ----

// freeformMapParents is the set of RAML keys whose VALUES are maps with
// user-defined (free-form) keys: header names, query-param names, status
// codes, media types.  Completions at the key level inside these maps should
// not suggest structural RAML keys.
var freeformMapParents = map[string]struct{}{
	raml.FacetHeaders:         {},
	raml.FacetQueryParameters: {},
	raml.FacetUriParameters:   {},
	raml.FacetBody:            {},
	raml.FacetResponses:       {},
	raml.FacetSecuritySchemes: {}, // direct children are user-defined scheme names
}

// freeformParentKey returns the name of the freeform-map parent key if the
// cursor at lspLine (0-based) is a direct child key of a freeform map
// (headers:, queryParameters:, uriParameters:, body:, responses:, etc.).
// Returns "" when the cursor is not in that position.
func freeformParentKey(lines docLines, lspLine int) string {
	if lspLine >= len(lines) {
		return ""
	}
	curIndent := lines[lspLine].indent
	if curIndent == 0 {
		return ""
	}
	for i := lspLine - 1; i >= 0; i-- {
		li := lines[i]
		if li.trimmed == "" {
			continue
		}
		if li.indent < curIndent {
			if strings.HasPrefix(li.trimmed, "- ") || strings.HasPrefix(li.trimmed, "#") {
				return ""
			}
			idx := strings.IndexByte(li.trimmed, ':')
			if idx <= 0 {
				return ""
			}
			key := li.trimmed[:idx]
			if _, ok := freeformMapParents[key]; ok {
				return key
			}
			return ""
		}
	}
	return ""
}

// isUnderUsesBlock reports whether the cursor (0-based lspLine) is on a mapping
// entry that is directly nested under a top-level `uses:` key.  Used to detect
// the file-path completion context when the parsed model is not yet available.
func isUnderUsesBlock(lines docLines, lspLine int) bool {
	if lspLine >= len(lines) {
		return false
	}
	curIndent := lines[lspLine].indent
	if curIndent == 0 {
		return false
	}
	for i := lspLine - 1; i >= 0; i-- {
		li := lines[i]
		if li.trimmed == "" {
			continue
		}
		if li.indent < curIndent {
			return strings.HasPrefix(li.trimmed, "uses:")
		}
	}
	return false
}

// cursorIsIndented reports whether the cursor line has leading whitespace.
func cursorIsIndented(lines docLines, lspLine uint32) bool {
	if int(lspLine) >= len(lines) {
		return false
	}
	return lines[lspLine].indent > 0
}

// existingKeysAt collects sibling YAML mapping keys already present at the same
// indentation level as the cursor line, scanning both upward and downward.
func existingKeysAt(lines docLines, lspLine int) map[string]struct{} {
	// Determine target indentation from the cursor line itself first.
	// A whitespace-only line (common when the editor auto-indents a blank line)
	// carries indentation even though it has no visible content; treat it the
	// same as a line with content so that sibling-key scanning stays at the
	// correct nesting level.
	targetIndent := -1
	if lspLine < len(lines) {
		if len(lines[lspLine].raw) > 0 {
			targetIndent = lines[lspLine].indent
		}
	}
	if targetIndent < 0 {
		// Cursor is on a blank line. Scan both directions and take the MINIMUM
		// indent of the nearest non-blank lines. Using only the nearest line
		// above would give the wrong target when an indented block (e.g. the
		// body of `uses:`) ends immediately before the cursor.
		above := -1
		for i := lspLine - 1; i >= 0; i-- {
			if lines[i].trimmed != "" {
				above = lines[i].indent
				break
			}
		}
		below := -1
		for i := lspLine + 1; i < len(lines); i++ {
			if lines[i].trimmed != "" {
				below = lines[i].indent
				break
			}
		}
		switch {
		case above >= 0 && below >= 0:
			if below < above {
				targetIndent = below
			} else {
				targetIndent = above
			}
		case above >= 0 && below < 0:
			// Blank at end of file (or no non-blank line downstream). Walk upward
			// to find the shallowest peer indent, so that a cursor after a
			// deeply-indented block (e.g. `uses:` or `types:` body) resolves to
			// the root indent rather than the last child's indent.
			targetIndent = above
			for i := lspLine - 1; i >= 0 && targetIndent > 0; i-- {
				li := lines[i]
				if li.trimmed == "" {
					continue
				}
				if li.indent < targetIndent {
					targetIndent = li.indent
				}
			}
		case below >= 0:
			targetIndent = below
		}
	}
	if targetIndent < 0 {
		return nil
	}

	existing := make(map[string]struct{})

	// Upward scan: collect same-indent sibling keys, stopping at the parent
	// boundary.  A "- key: val" line at indent targetIndent-2 is the opening
	// line of the current sequence-item mapping; its inline key is a sibling.
	for i := lspLine - 1; i >= 0; i-- {
		li := lines[i]
		if li.trimmed == "" {
			continue
		}
		if li.indent < targetIndent {
			if li.indent+2 == targetIndent && strings.HasPrefix(li.trimmed, "- ") {
				rest := li.trimmed[2:]
				if !strings.HasPrefix(rest, "#") {
					if idx := strings.IndexByte(rest, ':'); idx > 0 {
						existing[rest[:idx]] = struct{}{}
					}
				}
			}
			break
		}
		if li.indent == targetIndent {
			if !strings.HasPrefix(li.trimmed, "- ") && !strings.HasPrefix(li.trimmed, "#") {
				if idx := strings.IndexByte(li.trimmed, ':'); idx > 0 {
					existing[li.trimmed[:idx]] = struct{}{}
				}
			}
		}
	}

	// Downward scan: collect same-indent sibling keys below the cursor.
	// Unlike the upward scan, no sequence-item extraction is needed; a "- key:"
	// line below would belong to a different (later) sequence item, not a sibling.
	for i := lspLine + 1; i < len(lines); i++ {
		li := lines[i]
		if li.trimmed == "" {
			continue
		}
		if li.indent < targetIndent {
			break
		}
		if li.indent == targetIndent {
			if !strings.HasPrefix(li.trimmed, "- ") && !strings.HasPrefix(li.trimmed, "#") {
				if idx := strings.IndexByte(li.trimmed, ':'); idx > 0 {
					existing[li.trimmed[:idx]] = struct{}{}
				}
			}
		}
	}

	return existing
}

// lineInfo holds a document line together with its pre-computed indent depth
// and trimmed content, so every scanning helper reuses these values without
// repeating the same string operations across multiple functions.
type lineInfo struct {
	raw     string // original text (indentation preserved for column calculations)
	trimmed string // strings.TrimSpace(raw); empty string for blank lines
	indent  int    // count of leading spaces/tabs
}

// docLines is a pre-parsed document line slice.
type docLines []lineInfo

// parseDoc splits text into lines, normalises CRLF, and pre-computes per-line
// indent and trimmed content so callers never repeat those operations in loops.
func parseDoc(text string) docLines {
	return parseLines(strings.Split(text, "\n"))
}

func parseLines(text []string) docLines {
	lines := make(docLines, len(text))
	for i, r := range text {
		if len(r) > 0 && r[len(r)-1] == '\r' {
			r = r[:len(r)-1]
		}
		t := strings.TrimSpace(r)
		lines[i] = lineInfo{raw: r, trimmed: t, indent: leadingSpaces(r)}
	}
	return lines
}

// leadingSpaces counts leading space/tab characters on a line.
func leadingSpaces(line string) int {
	for i, ch := range line {
		if ch != ' ' && ch != '\t' {
			return i
		}
	}
	return len(line)
}

// ---- Other helpers ----

// detectIncludeArg reports whether lineText ends with a !include tag on the
// value side of a YAML mapping entry or as a sequence-item value ("- !include").
// On success it returns the partial path already typed after "!include " (may be
// empty) and the 0-based character column where that path starts in the line.
func detectIncludeArg(lineText string) (partialPath string, pathStartChar uint32, ok bool) {
	m := reIncludeTag.FindStringSubmatchIndex(lineText)
	if m == nil {
		return "", 0, false
	}
	includeIdx := m[0]
	colonIdx := findYAMLKeyColon(lineText)
	// Valid when after a YAML key colon or as a bare sequence item.
	isValueSide := (colonIdx >= 0 && includeIdx > colonIdx) ||
		strings.HasPrefix(strings.TrimSpace(lineText), "- !include")
	if !isValueSide {
		return "", 0, false
	}
	// m[2]:m[3] is the capture group start/end for the partial path.
	return lineText[m[2]:m[3]], uint32(m[2]), true
}

// includePathCompletions returns CompletionItems for the file-path argument of
// an !include tag. baseDir is the directory of the current document,
// partialPath is the path fragment already typed (may be empty), pos is the
// cursor position, and pathStartChar is the 0-based column where the path
// argument begins in the line. Each item carries a TextEdit that replaces the
// typed fragment so the client never double-inserts the already-typed prefix.
//
// filterExts, when non-empty, restricts file (not directory) completions to
// entries whose extension (lower-cased) is one of the listed values
// (e.g. ".raml"). Passing no filterExts lists every file.
