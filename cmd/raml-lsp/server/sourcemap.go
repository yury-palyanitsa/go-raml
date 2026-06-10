package server

import (
	raml "github.com/acronis/go-raml/v3"
	"gopkg.in/yaml.v3"
)

// ValSemantic is the semantic role of a YAML value position, derived from
// SourceInfo-driven sub-key scanning (Phase 2) and structural block annotation.
type ValSemantic int8

const (
	// ValSemanticUnknown means no Phase 2 annotation was applied. analyzePosition
	// falls back to key-name heuristics for this line.
	ValSemanticUnknown ValSemantic = iota
	// ValSemanticSuppressed means the field takes free-form text or a structural
	// block — no completions should be offered on the value side.
	ValSemanticSuppressed
	// ValSemanticTypeExpr means the value is a RAML type expression.
	ValSemanticTypeExpr
	// ValSemanticEnum means the value is constrained to a known set of strings.
	// ValKind identifies which set.
	ValSemanticEnum
)

// LineRecord is the per-line entry in a SourceMap. It combines syntactic data
// from the YAML AST (Phase 1) with semantic annotations from SourceInfo-driven
// sub-key scanning and structural block annotation (Phase 1.5 / Phase 2).
type LineRecord struct {
	// ── Syntactic (YAML AST, Phase 1) ───────────────────────────────────────

	// KeyName is the YAML key string for this line (e.g. "type", "description").
	KeyName string
	// KeyEndCol is the 0-based character column immediately after the last
	// character of the key (coincides with the ':' position for normal keys).
	// A cursor with col ≤ KeyEndCol while lineText has no colon means the cursor
	// is editing the key text itself, not the value.
	KeyEndCol int
	// ValInline is true when the YAML value node is on the same line as the key
	// (inline scalar, flow mapping, or flow sequence). When false the value is a
	// block that starts on a subsequent line — offering completions at the key
	// line's value side would be wrong.
	ValInline bool
	// ValNodeKind is the yaml.Kind of the value node. Distinguishes scalar
	// (type expression or scalar facet), mapping (block object), and sequence.
	ValNodeKind yaml.Kind

	// ── Semantic (SourceInfo-driven sub-key scan, Phase 2) ─────────────────

	// Semantic is the completion role for the value side of this line.
	Semantic ValSemantic
	// ValKind is the constrained-value set for ValSemanticEnum lines.
	ValKind ValueKind
}

// SourceMap is a per-fragment flat index of every YAML key line, indexed by
// the 1-based line number from the yaml.v3 AST.
//
// It is built in three passes inside BuildAllIndexes:
//   - Phase 1: buildSourceMapFromYAML walks the raw *yaml.Node and fills in
//     the syntactic (YAML AST) fields (KeyName, KeyEndCol, ValInline,
//     ValNodeKind) of every LineRecord.
//   - Phase 1.5: annotateYAMLStructure applies structural block-role
//     annotations from the YAML AST alone, covering partial/error documents
//     where the model may not have fully parsed.
//   - Phase 2: annotateShapeSubkeys / annotateSecSchemeSubkeys use the
//     SourceInfo companion map to scan entity sub-key nodes directly, marking
//     the semantic role (Semantic, ValKind) of each facet key line without
//     relying on model sub-field positions.
//
// analyzePosition queries the SourceMap first for authoritative answers before
// falling back to text-scanning heuristics.
type SourceMap struct {
	records map[int]*LineRecord
}

func newSourceMap() *SourceMap {
	return &SourceMap{records: make(map[int]*LineRecord)}
}

// Get returns the LineRecord for the given 1-based RAML line, or nil when the
// line is not present (e.g. blank lines, comment lines, or lines the YAML
// parser merged into a parent node).
func (sm *SourceMap) Get(line int) *LineRecord {
	if sm == nil {
		return nil
	}
	return sm.records[line]
}

// IsBlockSeqEnum reports whether this record describes a YAML key whose value
// is a block sequence (not inline) and whose semantic is a constrained enum.
// Sequence-item lines (starting with `- `) under such a key should offer the
// same enum completions as the parent key's value side.
func (r *LineRecord) IsBlockSeqEnum() bool {
	return r != nil && r.Semantic == ValSemanticEnum && !r.ValInline && r.ValNodeKind == yaml.SequenceNode
}

// isBlockSeqTypeExpr reports whether this record describes a YAML key whose
// value is a block sequence (not inline) and whose semantic is a type expression.
// Sequence-item lines under such a key (e.g. `type:\n- TypeA\n- TypeB`) should
// offer type-expression completions.
func (r *LineRecord) isBlockSeqTypeExpr() bool {
	return r != nil && r.Semantic == ValSemanticTypeExpr && !r.ValInline && r.ValNodeKind == yaml.SequenceNode
}

// ── Phase 1: YAML AST walk ───────────────────────────────────────────────────

// yamlRootMapping unwraps a yaml.DocumentNode and returns the root
// yaml.MappingNode. It returns nil when node is nil or not a mapping.
func yamlRootMapping(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

// buildSourceMapFromYAML populates sm by recursively walking the root
// *yaml.Node of a RAML fragment. All semantic fields remain ValSemanticUnknown
// after this call; Phase 1.5 and Phase 2 passes fill them in.
func buildSourceMapFromYAML(root *yaml.Node, sm *SourceMap) {
	walkYAMLMapping(yamlRootMapping(root), sm)
}

// walkYAMLMapping records a LineRecord for every key-value pair in node and
// recurses into nested mapping/sequence values.
func walkYAMLMapping(node *yaml.Node, sm *SourceMap) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode == nil || valNode == nil || keyNode.Kind != yaml.ScalarNode {
			continue
		}
		// yaml.v3 Column is 1-based; KeyEndCol is stored 0-based so it can be
		// compared directly with len(lineText) (the 0-based cursor column).
		keyStart := keyNode.Column - 1
		rec := &LineRecord{
			KeyName:     keyNode.Value,
			KeyEndCol:   keyStart + len(keyNode.Value),
			ValInline:   valNode.Line == keyNode.Line,
			ValNodeKind: valNode.Kind,
		}
		// Only record the first occurrence per line (handles YAML duplicate keys
		// and the edge case where !include expands to a multi-document stream).
		if _, exists := sm.records[keyNode.Line]; !exists {
			sm.records[keyNode.Line] = rec
		}
		// Recurse into value nodes so nested mappings are also indexed.
		switch valNode.Kind {
		case yaml.MappingNode:
			walkYAMLMapping(valNode, sm)
		case yaml.SequenceNode:
			for _, item := range valNode.Content {
				if item != nil && item.Kind == yaml.MappingNode {
					walkYAMLMapping(item, sm)
				}
			}
		}
	}
}

// typedContainerAt performs a top-down walk of the retained YAML AST
// (r.GetSourceNode) to find the annotation (CDP) or custom-facet key whose
// value block contains the cursor, then descends that value to collect the
// JSON-Pointer-style path from the root key down to the cursor level.
//
// This mirrors how JSON Schema language servers resolve cursor context:
// parse → AST with positions → cursor-in-range check → walk parent chain.
// No text scanning or SourceMap parent links are needed; the YAML node tree
// encodes the full structure including same-indent block sequences.
//
// Returns ("", nil) when the cursor is not inside a typed CDP/facet body.
func typedContainerAt(
	r *raml.RAML,
	filePath string,
	lspLine int,
	cursorCol int, // 1-based (yaml.v3 convention): lspCol+1
	entry *ContextEntry,
) (rootKey string, path []string) {
	if r == nil {
		return "", nil
	}
	root := r.GetSourceNode(filePath)
	if root == nil {
		return "", nil
	}
	m := yamlRootMapping(root)
	if m == nil {
		return "", nil
	}
	// yaml.v3 lines are 1-based; lspLine is 0-based.
	ramlLine := lspLine + 1
	return findCDPContainer(m, ramlLine, cursorCol, entry, r, filePath)
}

// findCDPContainer walks a yaml.MappingNode top-down, finding the key whose
// value span contains ramlLine and that is an annotation or custom-facet root.
// When the containing key is neither, the function recurses into the value.
func findCDPContainer(
	node *yaml.Node,
	ramlLine int,
	cursorCol int,
	entry *ContextEntry,
	r *raml.RAML,
	filePath string,
) (string, []string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return "", nil
	}
	kv := mappingKeyContaining(node, ramlLine, cursorCol)
	if kv == nil {
		return "", nil
	}
	if raml.IsCustomDomainExtensionNode(kv.key) || isCustomFacetValueOf(kv.key, entry, r, filePath) {
		return kv.key, descendValuePath(kv.val, ramlLine, cursorCol)
	}
	// Not a CDP/facet root: recurse into the value mapping or sequence.
	switch kv.val.Kind {
	case yaml.MappingNode:
		return findCDPContainer(kv.val, ramlLine, cursorCol, entry, r, filePath)
	case yaml.SequenceNode:
		if item := seqItemContaining(kv.val, ramlLine); item != nil && item.Kind == yaml.MappingNode {
			return findCDPContainer(item, ramlLine, cursorCol, entry, r, filePath)
		}
	}
	return "", nil
}

// descendValuePath descends into a yaml.Node value collecting JSON-Pointer-
// style path steps (["prop", "[]", "sub"] etc.) that lead from the value's
// top level down to the line containing ramlLine.  Returns an empty slice
// when the cursor sits at the immediate top level of the value.
func descendValuePath(node *yaml.Node, ramlLine, cursorCol int) []string {
	var path []string
	cur := node
	for {
		switch cur.Kind {
		case yaml.MappingNode:
			step := mappingKeyContaining(cur, ramlLine, cursorCol)
			if step == nil {
				return path // cursor is at this mapping level; no deeper
			}
			path = append(path, step.key)
			cur = step.val
		case yaml.SequenceNode:
			item := seqItemContaining(cur, ramlLine)
			if item == nil || item.Kind != yaml.MappingNode {
				return path
			}
			path = append(path, "[]")
			cur = item
		default:
			return path
		}
	}
}

// yamlKeyVal is a key-value pair from a YAML mapping node.
type yamlKeyVal struct {
	key string
	val *yaml.Node
}

// mappingKeyContaining returns the key whose value span contains ramlLine at
// cursorCol (1-based, yaml.v3 convention), or nil when the cursor is at the
// mapping's own level.
func mappingKeyContaining(node *yaml.Node, ramlLine, cursorCol int) *yamlKeyVal {
	for i := 0; i+1 < len(node.Content); i += 2 {
		k, v := node.Content[i], node.Content[i+1]
		if k.Line > ramlLine {
			break
		}
		if i+2 < len(node.Content) && node.Content[i+2].Line <= ramlLine {
			continue
		}
		// Non-empty inline scalar: its span is its own line only.
		if v.Kind == yaml.ScalarNode && v.Value != "" && ramlLine > v.Line {
			return nil
		}
		// A block mapping/sequence value is always more indented than its key.
		// If the cursor is on a later line but at or before the key's column,
		// it is a sibling — not inside the value block.
		if ramlLine > k.Line && (v.Kind == yaml.MappingNode || v.Kind == yaml.SequenceNode) && cursorCol <= k.Column {
			return nil
		}
		return &yamlKeyVal{k.Value, v}
	}
	return nil
}

// seqItemContaining returns the sequence item whose span contains ramlLine.
func seqItemContaining(node *yaml.Node, ramlLine int) *yaml.Node {
	for j, item := range node.Content {
		if item == nil || item.Line > ramlLine {
			break
		}
		if j+1 < len(node.Content) && node.Content[j+1] != nil && node.Content[j+1].Line <= ramlLine {
			continue
		}
		return item
	}
	return nil
}

// ── Phase 2: SourceInfo-driven semantic annotation ──────────────────────────

// setLineSemantic annotates the LineRecord at the given 1-based line with sem
// and vk. It is a no-op when sm is nil or line ≤ 0. When the record does not
// exist in the map (SourceInfo annotation for lines the YAML walk may have
// missed, e.g. due to !include), a synthetic record is created.
// Each line's annotation is set at most once: both the structural (Phase 1.5)
// and SourceInfo-driven (Phase 2) passes produce the same annotation for every
// shared key, so the first-writer-wins order is safe.
func setLineSemantic(sm *SourceMap, line int, sem ValSemantic, vk ValueKind) {
	if sm == nil || line <= 0 {
		return
	}
	rec := sm.records[line]
	if rec == nil {
		rec = &LineRecord{Semantic: sem, ValKind: vk}
		sm.records[line] = rec
		return
	}
	if rec.Semantic == ValSemanticUnknown {
		rec.Semantic = sem
		rec.ValKind = vk
	}
}

// blockKeyRole pairs a ValSemantic with an optional ValueKind for use in the
// structural block annotation tables.
type blockKeyRole struct {
	sem ValSemantic
	vk  ValueKind
}

// annotateBlockKeys structurally annotates SourceMap records for well-known
// keys within a RAML block (1-based line range [fromLine, toLine]).
//
// This is the structural fallback: it annotates keys like `is:` inside an
// endpoint block or `type:` inside a security scheme when the Phase 2
// SourceInfo-driven scan has not already written an annotation for that line.
// Existing non-Unknown annotations are never overwritten.
func annotateBlockKeys(sm *SourceMap, fromLine, toLine int, roles map[string]blockKeyRole) {
	if sm == nil || fromLine > toLine {
		return
	}
	for line := fromLine; line <= toLine; line++ {
		rec := sm.records[line]
		if rec == nil || rec.Semantic != ValSemanticUnknown {
			continue
		}
		if r, ok := roles[rec.KeyName]; ok {
			rec.Semantic = r.sem
			rec.ValKind = r.vk
		}
	}
}

// ── Pre-defined block role tables ────────────────────────────────────────────
// Each table covers directly-known keys within a specific structural block.
// Free-text keys are marked Suppressed; constrained-enum keys carry their
// ValueKind. Keys absent from the table are left Unknown and resolved at
// analyzePosition time with text-based heuristics.

var securitySchemeBlockRoles = map[string]blockKeyRole{
	raml.FacetType:        {ValSemanticEnum, ValueKindSecSchemeType},
	raml.FacetDisplayName: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDescription: {ValSemanticSuppressed, ValueKindNone},
}

var endpointBlockRoles = map[string]blockKeyRole{
	raml.FacetType:        {ValSemanticEnum, ValueKindResourceType},
	raml.FacetDisplayName: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDescription: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetIs:          {ValSemanticEnum, ValueKindIs},
	raml.FacetSecuredBy:   {ValSemanticEnum, ValueKindSecuredBy},
}

var operationBlockRoles = map[string]blockKeyRole{
	raml.FacetDisplayName: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDescription: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetIs:          {ValSemanticEnum, ValueKindIs},
	raml.FacetSecuredBy:   {ValSemanticEnum, ValueKindSecuredBy},
	raml.FacetProtocols:   {ValSemanticEnum, ValueKindProtocols},
	raml.FacetQueryString: {ValSemanticTypeExpr, ValueKindNone},
}

var traitBlockRoles = map[string]blockKeyRole{
	raml.FacetDisplayName: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDescription: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetIs:          {ValSemanticEnum, ValueKindIs},
	raml.FacetProtocols:   {ValSemanticEnum, ValueKindProtocols},
	raml.FacetQueryString: {ValSemanticTypeExpr, ValueKindNone},
}

var rootAPIBlockRoles = map[string]blockKeyRole{
	raml.FacetTitle:       {ValSemanticSuppressed, ValueKindNone},
	raml.FacetVersion:     {ValSemanticSuppressed, ValueKindNone},
	raml.FacetBaseUri:     {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDescription: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetProtocols:   {ValSemanticEnum, ValueKindProtocols},
	raml.FacetMediaType:   {ValSemanticEnum, ValueKindMediaType},
	raml.FacetSecuredBy:   {ValSemanticEnum, ValueKindSecuredBy},
}

// allShapeFacetRoles is the complete role table for every built-in RAML shape
// facet, used by annotateShapeSubkeys (SourceInfo-driven Phase 2) to annotate
// each facet key line with its semantic role. It is a superset of
// commonTypeBodyRoles, covering type-specific facets (format, minimum, pattern,
// properties, discriminator, etc.) that the structural Phase 1.5 pass omits.
var allShapeFacetRoles = map[string]blockKeyRole{
	// Common to all shape types.
	raml.FacetType:        {ValSemanticTypeExpr, ValueKindNone},
	raml.FacetItems:       {ValSemanticTypeExpr, ValueKindNone},
	raml.FacetDisplayName: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDescription: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDefault:     {ValSemanticSuppressed, ValueKindNone},
	raml.FacetEnum:        {ValSemanticSuppressed, ValueKindNone},
	raml.FacetRequired:    {ValSemanticEnum, ValueKindBool},
	raml.FacetFacets:      {ValSemanticSuppressed, ValueKindNone},
	raml.FacetExample:     {ValSemanticSuppressed, ValueKindNone},
	raml.FacetExamples:    {ValSemanticSuppressed, ValueKindNone},
	// Integer / Number.
	raml.FacetFormat:     {ValSemanticEnum, ValueKindFormat},
	raml.FacetMinimum:    {ValSemanticSuppressed, ValueKindNone},
	raml.FacetMaximum:    {ValSemanticSuppressed, ValueKindNone},
	raml.FacetMultipleOf: {ValSemanticSuppressed, ValueKindNone},
	// String.
	raml.FacetMinLength: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetMaxLength: {ValSemanticSuppressed, ValueKindNone},
	raml.FacetPattern:   {ValSemanticSuppressed, ValueKindNone},
	// Array.
	raml.FacetMinItems:    {ValSemanticSuppressed, ValueKindNone},
	raml.FacetMaxItems:    {ValSemanticSuppressed, ValueKindNone},
	raml.FacetUniqueItems: {ValSemanticEnum, ValueKindBool},
	// Object.
	raml.FacetProperties:           {ValSemanticSuppressed, ValueKindNone},
	raml.FacetAdditionalProperties: {ValSemanticEnum, ValueKindBool},
	raml.FacetDiscriminator:        {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDiscriminatorValue:   {ValSemanticSuppressed, ValueKindNone},
	raml.FacetMinProperties:        {ValSemanticSuppressed, ValueKindNone},
	raml.FacetMaxProperties:        {ValSemanticSuppressed, ValueKindNone},
	// File.
	raml.FacetFileTypes: {ValSemanticSuppressed, ValueKindNone},
}

// commonTypeBodyRoles covers context-agnostic facets valid in any RAML shape
// body. Applied by annotateTypeBodyRecursive (Phase 1.5) to annotate type
// declarations even when the RAML model fails to parse.
var commonTypeBodyRoles = map[string]blockKeyRole{
	raml.FacetType:                 {ValSemanticTypeExpr, ValueKindNone},
	raml.FacetItems:                {ValSemanticTypeExpr, ValueKindNone},
	raml.FacetDisplayName:          {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDescription:          {ValSemanticSuppressed, ValueKindNone},
	raml.FacetDefault:              {ValSemanticSuppressed, ValueKindNone},
	raml.FacetEnum:                 {ValSemanticSuppressed, ValueKindNone},
	raml.FacetRequired:             {ValSemanticEnum, ValueKindBool},
	raml.FacetFormat:               {ValSemanticEnum, ValueKindFormat},
	raml.FacetAdditionalProperties: {ValSemanticEnum, ValueKindBool},
	raml.FacetUniqueItems:          {ValSemanticEnum, ValueKindBool},
}

// annotateNodeSubkeys scans the direct key-value pairs in a YAML MappingNode
// and annotates each key line whose name appears in roles. It is the shared
// implementation used by annotateShapeSubkeys and annotateSecSchemeSubkeys.
func annotateNodeSubkeys(sm *SourceMap, v *yaml.Node, roles map[string]blockKeyRole) {
	if v == nil || v.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(v.Content); i += 2 {
		k := v.Content[i]
		if k == nil {
			continue
		}
		if r, ok := roles[k.Value]; ok {
			setLineSemantic(sm, k.Line, r.sem, r.vk)
		}
	}
}

// annotateShapeSubkeys annotates the direct sub-key lines of a shape entity
// using the *yaml.Node pair from SourceInfo (Phase 2).
//
// Two cases are handled:
//   - ScalarNode value: the entity was written as inline shorthand (e.g. `Foo:
//     string`). The entity key line is marked as a type expression.
//   - MappingNode value: the entity body is a block or flow mapping. Each
//     direct child key is looked up in allShapeFacetRoles and annotated with
//     its semantic role. Flow-style mappings (all keys on one line) are handled
//     correctly because yaml.v3 assigns distinct Line values to flow-map keys.
//
// Operating directly on YAML nodes makes this more robust than a model-field
// walk: it works for partial-parse failures where model sub-fields (TypeExpr,
// Required, etc.) are nil, and handles flow-style shapes without line-number
// collisions.
func annotateShapeSubkeys(sm *SourceMap, pair *raml.NodePair) {
	if sm == nil || pair == nil || pair.Value == nil {
		return
	}
	if pair.Value.Kind == yaml.ScalarNode {
		// Inline shorthand: the value IS the type expression (e.g. `Foo: string`).
		if pair.Key != nil && pair.Key.Line > 0 {
			setLineSemantic(sm, pair.Key.Line, ValSemanticTypeExpr, ValueKindNone)
		}
		return
	}
	annotateNodeSubkeys(sm, pair.Value, allShapeFacetRoles)
}

// annotateSecSchemeSubkeys annotates the direct sub-key lines of a security
// scheme entity using the *yaml.Node pair from SourceInfo (Phase 2).
func annotateSecSchemeSubkeys(sm *SourceMap, pair *raml.NodePair) {
	if pair == nil {
		return
	}
	annotateNodeSubkeys(sm, pair.Value, securitySchemeBlockRoles)
}

// annotateRootKeys is like annotateBlockKeys but without a line-range filter.
// It applies role annotations to all records in sm whose key name matches roles
// and whose Semantic is still Unknown. Used for fragment-root-level keys
// (title:, version:, mediaType:, etc.) where the exact line range is unknown.
func annotateRootKeys(sm *SourceMap, roles map[string]blockKeyRole) {
	if sm == nil {
		return
	}
	for _, rec := range sm.records {
		if rec == nil || rec.Semantic != ValSemanticUnknown {
			continue
		}
		if r, ok := roles[rec.KeyName]; ok {
			rec.Semantic = r.sem
			rec.ValKind = r.vk
		}
	}
}

// ── YAML-only structural annotation (Phase 1.5) ─────────────────────────────
// These annotate SourceMap records purely from the YAML AST, without requiring
// the RAML model. Calling annotateYAMLStructure from getCI ensures that
// context-sensitive keys (type:, required:, is:, securedBy:, etc.) are
// annotated even when the RAML model fails to parse.

// annotateYAMLStructure makes a single pass over the root YAML mapping and
// applies structural block-role annotations for all recognisable RAML sections:
// security schemes, type/annotationType bodies (with property recursion),
// endpoint/operation blocks, traits/resourceTypes, and root API scalar keys.
func annotateYAMLStructure(root *yaml.Node, sm *SourceMap) {
	mapping := yamlRootMapping(root)
	if mapping == nil {
		return
	}
	// Root-level API scalar keys (title:, protocols:, mediaType:, etc.).
	annotateRootKeys(sm, rootAPIBlockRoles)
	// Domain-specific structural blocks.
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		topKey := mapping.Content[i]
		topVal := mapping.Content[i+1]
		if topVal == nil || topVal.Kind != yaml.MappingNode {
			continue
		}
		switch topKey.Value {
		case raml.FacetSecuritySchemes:
			for j := 0; j+1 < len(topVal.Content); j += 2 {
				schemeVal := topVal.Content[j+1]
				if schemeVal != nil && schemeVal.Kind == yaml.MappingNode {
					annotateBlockKeys(sm, schemeVal.Line, yamlNodeEndLine(schemeVal), securitySchemeBlockRoles)
				}
			}
		case raml.FacetTypes, raml.FacetAnnotationTypes:
			for j := 0; j+1 < len(topVal.Content); j += 2 {
				annotateTypeBodyRecursive(topVal.Content[j+1], sm)
			}
		case raml.FacetTraits, raml.FacetResourceTypes:
			for j := 0; j+1 < len(topVal.Content); j += 2 {
				traitBody := topVal.Content[j+1]
				if traitBody != nil && traitBody.Kind == yaml.MappingNode {
					annotateBlockKeys(sm, traitBody.Line, yamlNodeEndLine(traitBody), traitBlockRoles)
				}
			}
		default:
			// Endpoint paths are identified by their leading '/'.
			if len(topKey.Value) > 0 && topKey.Value[0] == '/' {
				annotateBlockKeys(sm, topVal.Line, yamlNodeEndLine(topVal), endpointBlockRoles)
				annotateEndpointBodyYAML(topVal, sm)
			}
		}
	}
}

// annotateTypeBodyRecursive annotates commonTypeBodyRoles facets for a RAML
// shape body node and recurses into its properties: sub-map.
func annotateTypeBodyRecursive(body *yaml.Node, sm *SourceMap) {
	if body == nil || body.Kind != yaml.MappingNode {
		return
	}
	annotateBlockKeys(sm, body.Line, yamlNodeEndLine(body), commonTypeBodyRoles)
	for i := 0; i+1 < len(body.Content); i += 2 {
		k, v := body.Content[i], body.Content[i+1]
		if k.Value != raml.FacetProperties || v == nil || v.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(v.Content); j += 2 {
			annotateTypeBodyRecursive(v.Content[j+1], sm)
		}
	}
}

// annotateEndpointBodyYAML recurses into an endpoint block, annotating HTTP
// method (operation) blocks and nested endpoint paths.
func annotateEndpointBodyYAML(body *yaml.Node, sm *SourceMap) {
	if body == nil || body.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(body.Content); i += 2 {
		k, v := body.Content[i], body.Content[i+1]
		if v == nil || v.Kind != yaml.MappingNode {
			continue
		}
		if raml.IsHTTPMethod(k.Value) {
			annotateBlockKeys(sm, v.Line, yamlNodeEndLine(v), operationBlockRoles)
		} else if len(k.Value) > 0 && k.Value[0] == '/' {
			annotateBlockKeys(sm, v.Line, yamlNodeEndLine(v), endpointBlockRoles)
			annotateEndpointBodyYAML(v, sm)
		}
	}
}

// yamlNodeEndLine returns the last source line occupied by node and all its
// descendants (the deepest-right leaf).
func yamlNodeEndLine(node *yaml.Node) int {
	if node == nil {
		return 0
	}
	last := node
	for len(last.Content) > 0 {
		last = last.Content[len(last.Content)-1]
	}
	return last.Line
}
