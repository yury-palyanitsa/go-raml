package server

import (
	raml "github.com/acronis/go-raml/v3"
	"github.com/acronis/go-stacktrace"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ExpandDiagnosticRanges widens single-character diagnostic ranges in rootURI
// to cover the full YAML scalar token at each error site. Diagnostics emitted
// at other URIs (e.g. the empty string for position-less errors) are untouched.
// lines must be the pre-parsed content of the file identified by rootURI.
func ExpandDiagnosticRanges(diags map[string][]protocol.Diagnostic, rootURI string, lines docLines) {
	ds, ok := diags[rootURI]
	if !ok {
		return
	}
	for i, d := range ds {
		if d.Range.End.Character-d.Range.Start.Character != 1 {
			continue
		}
		lspLine := int(d.Range.Start.Line)
		if lspLine >= len(lines) {
			continue
		}
		// Convert 0-based LSP column to 1-based RAML column expected by scalarTokenLen.
		col1 := int(d.Range.Start.Character) + 1
		if n := scalarTokenLen(lines[lspLine].raw, col1); n > 1 {
			ds[i].Range.End.Character = d.Range.Start.Character + uint32(n)
		}
	}
	diags[rootURI] = ds
}

// scalarTokenLen returns the number of runes occupied by the YAML scalar token
// starting at col (1-based) within line. For quoted scalars (single or double
// quote) the length includes both quote characters. For unquoted scalars it
// extends to the first bare '#' comment marker or end of line, with trailing
// whitespace stripped. Returns 1 when the token cannot be determined.
func scalarTokenLen(line string, col int) int {
	runes := []rune(line)
	if col < 1 || col > len(runes) {
		return 1
	}
	start := col - 1 // 0-based index
	ch := runes[start]
	if ch == '"' || ch == '\'' {
		for i := start + 1; i < len(runes); i++ {
			if runes[i] == ch {
				return i - start + 1
			}
		}
		return len(runes) - start
	}
	// Unquoted scalar: scan to bare '#' (comment) or end of line.
	end := len(runes)
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == '#' && runes[i-1] == ' ' {
			end = i
			break
		}
	}
	// Strip trailing whitespace.
	for end > start && (runes[end-1] == ' ' || runes[end-1] == '\t') {
		end--
	}
	n := end - start
	if n < 1 {
		return 1
	}
	return n
}

// IncludeSiteLookup returns the !include / uses: site of `includedURI` in
// `containerURI`. When no direct include of `includedURI` is recorded in
// `containerURI`, the lookup walks the parent chain transitively (a → b → c
// with the leaf error in c reports against a). Returns ok=false when no
// include path connects `includedURI` to `containerURI`.
type IncludeSiteLookup func(includedURI, containerURI string) (pos stacktrace.Position, ok bool)

// includeSiteLookupFor builds an IncludeSiteLookup backed by the !include
// graph recorded on r during parsing. It returns nil for a nil RAML so the
// caller can pass it straight through to ExtractDiagnostics without a guard.
//
// The lookup walks the parent chain from `included` upward: each iteration
// jumps to the file that included the current one, until it lands on
// `container` (success) or runs out of parents / revisits a file (cycle —
// failure). The final hop's Pos is the !include / uses: site to anchor on.
func includeSiteLookupFor(r *raml.RAML) IncludeSiteLookup {
	if r == nil {
		return nil
	}
	// Reverse the (source → list-of-includes) map into (included → first
	// parent). Multiple parents for the same included file are rare in
	// practice and the first one is sufficient for attribution.
	parent := make(map[string]raml.IncludeRef)
	for _, refs := range r.GetAllIncludeRefs() {
		for _, ref := range refs {
			if _, exists := parent[ref.AbsPath]; !exists {
				parent[ref.AbsPath] = ref
			}
		}
	}
	return func(included, container string) (stacktrace.Position, bool) {
		visited := make(map[string]bool, 4)
		cur := included
		for {
			if visited[cur] {
				return stacktrace.Position{}, false
			}
			visited[cur] = true
			ref, ok := parent[cur]
			if !ok {
				return stacktrace.Position{}, false
			}
			if ref.SourcePath == container {
				return ref.Pos, true
			}
			cur = ref.SourcePath
		}
	}
}

// ExtractDiagnostics converts a go-raml error (expected to be *stacktrace.StackTrace)
// into a map of file URI → []Diagnostic. All diagnostics are emitted into the root
// document (the first file seen in the stacktrace), so squiggles always land in the
// file the user has open rather than in included or non-existent files.
//
// includeSite is optional. When supplied, any diagnostic whose stacktrace
// chain crosses out of the root file (e.g. a type-resolution failure inside
// a security-scheme fragment loaded via !include) is anchored at the
// !include / uses: site in the root document instead of at its first
// position-less ancestor. Pass nil to retain the prior behaviour.
func ExtractDiagnostics(err error, includeSite IncludeSiteLookup) map[string][]protocol.Diagnostic {
	result := make(map[string][]protocol.Diagnostic)
	if err == nil {
		return result
	}
	st, ok := stacktrace.Unwrap(err)
	if !ok {
		// Plain Go error without location info.
		result[""] = []protocol.Diagnostic{{
			Range:    protocol.Range{},
			Severity: ptrTo(protocol.DiagnosticSeverityError),
			Message:  err.Error(),
			Source:   ptrTo("raml-lsp"),
		}}
		return result
	}
	walkST(st, "", result, includeSite)
	return result
}

// walkST walks the stacktrace and emits diagnostics restricted to the root file.
//
// rootURI is the file all diagnostics must land in. It is established the first
// time a node with a non-empty location is encountered, and then held fixed for
// the entire subtree beneath that call. Any descendant whose location is a
// different file returns its message as an "orphan" string carrying the file
// it came from; the first ancestor that is in the root file picks up the
// orphan message and emits it at its own root-file position. When the
// root-file ancestor has no useful position of its own (e.g. a top-level
// "resolve shapes" wrap stamped at the root document's start), the optional
// includeSite lookup recovers the !include / uses: site of the orphan's
// originating file in the root document. This ensures:
//
//   - Errors inside included libraries appear at the !include / uses: site.
//   - Errors for non-existent includes appear at the !include node, not in a
//     ghost file.
//   - Noisy wrapper nodes in the same file are still suppressed.
//
// Returns (emitted bool, orphanMsg string, orphanURI string). orphanURI is
// the originating file of the orphan message, used so an ancestor in the
// root document can resolve the !include site for diagnostics that bubble
// up across file boundaries.
func walkST(st *stacktrace.StackTrace, rootURI string, out map[string][]protocol.Diagnostic, includeSite IncludeSiteLookup) (bool, string, string) {
	if st == nil {
		return false, "", ""
	}

	myURI := ""
	if st.Location != nil {
		myURI = raml.PathToFileURI(string(*st.Location))
	}

	// First non-empty location encountered establishes the root file.
	if rootURI == "" && myURI != "" {
		rootURI = myURI
	}

	childEmitted := false
	var orphanMsg, orphanURI string
	for _, child := range st.List {
		e, om, ou := walkST(child, rootURI, out, includeSite)
		if e {
			childEmitted = true
		} else if om != "" && orphanMsg == "" {
			orphanMsg = om
			orphanURI = ou
		}
	}
	if st.Wrapped != nil {
		e, om, ou := walkST(st.Wrapped, rootURI, out, includeSite)
		if e {
			childEmitted = true
		} else if om != "" && orphanMsg == "" {
			orphanMsg = om
			orphanURI = ou
		}
	}

	// A descendant in the same root file already emitted — this node is noisy
	// context (e.g. "unmarshal yaml nodes", "make concrete shape"). Skip it.
	if childEmitted {
		return true, "", ""
	}

	msg := st.Message
	if msg == "" && st.Err != nil {
		msg = st.Err.Error()
	}

	// Prefer the orphan message: it is the actual error from a child that could
	// not emit at its own (different-file) location.
	emitMsg := msg
	if orphanMsg != "" {
		emitMsg = orphanMsg
	}

	if emitMsg == "" {
		return false, "", ""
	}

	// If this node is not in the root file, propagate the message upward for a
	// root-file ancestor to emit at its position. Carry the orphan's source
	// file so an ancestor can recover the !include site if needed.
	if rootURI != "" && myURI != rootURI {
		propagateURI := myURI
		if orphanURI != "" {
			// A deeper descendant already crossed a file boundary; the original
			// orphan source wins so the !include lookup targets the actual
			// failing file, not the intermediate non-root wrap.
			propagateURI = orphanURI
		}
		return false, emitMsg, propagateURI
	}

	fileURI := myURI
	rng := RamlPosToRange(st.Position)

	// When this node has no useful position of its own and the orphan came
	// from a different file, look up where that file was included into the
	// root document and emit at the !include site instead of landing at the
	// document root (line 0, column 0).
	if orphanURI != "" && orphanURI != rootURI && includeSite != nil && isZeroRange(rng) {
		if pos, ok := includeSite(orphanURI, rootURI); ok {
			rng = RamlPosToRange(&pos)
		}
	}

	// Deduplicate: skip if an identical diagnostic was already emitted.
	for _, existing := range out[fileURI] {
		if existing.Message == emitMsg && existing.Range == rng {
			return true, "", ""
		}
	}

	out[fileURI] = append(out[fileURI], protocol.Diagnostic{
		Range:    rng,
		Severity: ptrTo(protocol.DiagnosticSeverityError),
		Message:  emitMsg,
		Source:   ptrTo("raml-lsp"),
	})
	return true, "", ""
}

// isZeroRange reports whether r anchors at line 0 column 0 — typically the
// fallback produced by a nil stacktrace.Position. Used by walkST to detect
// "this ancestor has no useful position; consult the !include lookup
// instead" before emitting a diagnostic at the top of a document.
func isZeroRange(r protocol.Range) bool {
	return r.Start.Line == 0 && r.Start.Character == 0 && r.End.Line == 0 && r.End.Character == 0
}
