package server

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	raml "github.com/acronis/go-raml/v3"
	"github.com/acronis/go-stacktrace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ramlBadMinLength has a string value for minLength which must be a uint64.
// It exercises the full path: StringShape.unmarshalYAMLNodes → FixYamlError →
// walkST → RamlPosToRange, letting us assert both the message format and the
// squiggle range in one integration test.
const ramlBadMinLength = `#%RAML 1.0
title: test
types:
  Foo:
    type: string
    minLength: "not a number"
`

// ramlBadPattern has an integer value for pattern which must be a string.
const ramlBadPattern = `#%RAML 1.0
title: test
types:
  Bar:
    type: string
    pattern: 42
`

// TestExtractDiagnostics_MultiCharRange verifies that a Position with EndColumn
// set produces a range wider than one character in the LSP diagnostic.
func TestExtractDiagnostics_MultiCharRange(t *testing.T) {
	// Simulate a "minLength" key at line 6 col 5 spanning 9 characters.
	st := stacktrace.New("minLength: cannot unmarshal",
		stacktrace.WithLocation("/tmp/api.raml"),
		stacktrace.WithPosition(&stacktrace.Position{
			Line: 6, Column: 5,
			EndLine: 6, EndColumn: 14, // col 5 + len("minLength") = 14 (1-based exclusive)
		}),
	)

	result := ExtractDiagnostics(st, nil)
	var all []protocol.Diagnostic
	for _, diags := range result {
		all = append(all, diags...)
	}

	require.Len(t, all, 1)
	d := all[0]
	assert.Equal(t, uint32(5), d.Range.Start.Line, "RAML line 6 → 0-based 5")
	assert.Equal(t, uint32(4), d.Range.Start.Character, "RAML col 5 → 0-based 4")
	assert.Equal(t, uint32(13), d.Range.End.Character,
		"EndColumn 14 (1-based exclusive) → 0-based exclusive end 13 covers 'minLength'")
	span := d.Range.End.Character - d.Range.Start.Character
	assert.Equal(t, uint32(len("minLength")), span, "squiggle must cover the full key name")
}

// diagProbe is a single declarative test case for LSP publish-diagnostics
// behaviour.  Fill only the fields that matter for each case.
type diagProbe struct {
	name          string
	doc           string // RAML content passed to openDoc
	filename      string // defaults to "api.raml"
	wantClean     bool   // expect zero diagnostics
	wantErrorMin  int    // expect at least this many Error-severity diagnostics
	wantSource    string // every Error diag must carry this source (optional)
	squiggleToken string // first diag range must span exactly this token
	msgContains   string // first diag message must contain this substring
	msgNotRegexp  string // first diag message must NOT match this regexp
}

// diagProbes is the table of LSP diagnostics integration probes.
var diagProbes = []diagProbe{
	{
		name:      "clean file / no diagnostics",
		doc:       ramlValid,
		wantClean: true,
	},
	{
		name:         "undefined type / error diagnostic",
		doc:          ramlInvalidType,
		filename:     "bad.raml",
		wantErrorMin: 1,
		wantSource:   "raml-lsp",
	},
	{
		name:          "bad minLength / squiggle covers value",
		doc:           ramlBadMinLength,
		squiggleToken: "not a number",
	},
	{
		name:          "bad pattern / squiggle covers value and message names facet",
		doc:           ramlBadPattern,
		squiggleToken: "42",
		msgContains:   "pattern",
	},
	{
		name:         "bad minLength / no embedded yaml line numbers in message",
		doc:          ramlBadMinLength,
		msgNotRegexp: `\bline\s+\d+\b`,
	},
}

// runDiagProbe opens the document, waits for publishDiagnostics, logs the
// annotated document and all received diagnostics, then evaluates the probe
// assertions.  Failures are collected and reported together via t.Fail so the
// log output is always visible.
func runDiagProbe(t *testing.T, p diagProbe) {
	t.Helper()
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	filename := p.filename
	if filename == "" {
		filename = "api.raml"
	}
	uri := openDoc(t, s, nc, dir, filename, p.doc)
	diags := nc.waitDiags(t, uri)

	// ── debug block ─────────────────────────────────────────────────────────
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n=== diagProbe: %s ===\n", p.name)
	for i, line := range strings.Split(p.doc, "\n") {
		fmt.Fprintf(&sb, "  %3d: %s\n", i, line)
	}
	fmt.Fprintf(&sb, "diagnostics (%d):\n", len(diags))
	for i, d := range diags {
		sev := "?"
		if d.Severity != nil {
			switch *d.Severity {
			case protocol.DiagnosticSeverityError:
				sev = "Error"
			case protocol.DiagnosticSeverityWarning:
				sev = "Warning"
			case protocol.DiagnosticSeverityInformation:
				sev = "Info"
			case protocol.DiagnosticSeverityHint:
				sev = "Hint"
			}
		}
		src := ""
		if d.Source != nil {
			src = " source=" + *d.Source
		}
		span := d.Range.End.Character - d.Range.Start.Character
		fmt.Fprintf(&sb, "  [%d] L%d c%d-%d (span=%d) [%s%s] %s\n",
			i,
			d.Range.Start.Line, d.Range.Start.Character, d.Range.End.Character,
			span, sev, src, d.Message)
	}
	// ── assertions ──────────────────────────────────────────────────────────
	var failures []string
	if p.wantClean {
		if len(diags) != 0 {
			failures = append(failures, fmt.Sprintf("WANT clean: got %d diagnostic(s)", len(diags)))
		}
	}
	if p.wantErrorMin > 0 {
		errorCount := 0
		for _, d := range diags {
			if d.Severity != nil && *d.Severity == protocol.DiagnosticSeverityError {
				errorCount++
			}
		}
		if errorCount < p.wantErrorMin {
			failures = append(failures, fmt.Sprintf("WANT >= %d error diag(s): got %d", p.wantErrorMin, errorCount))
		}
		if p.wantSource != "" {
			for _, d := range diags {
				if d.Severity != nil && *d.Severity == protocol.DiagnosticSeverityError {
					if d.Source == nil || *d.Source != p.wantSource {
						got := "<nil>"
						if d.Source != nil {
							got = *d.Source
						}
						failures = append(failures, fmt.Sprintf("WANT source %q: got %q", p.wantSource, got))
					}
				}
			}
		}
	}
	if p.squiggleToken != "" {
		require.NotEmpty(t, diags, "WANT squiggleToken %q but got no diagnostics", p.squiggleToken)
		span := diags[0].Range.End.Character - diags[0].Range.Start.Character
		want := uint32(len(p.squiggleToken))
		if span != want {
			failures = append(failures, fmt.Sprintf("WANT squiggle span=%d (%q): got span=%d", want, p.squiggleToken, span))
		}
	}
	if p.msgContains != "" {
		require.NotEmpty(t, diags, "WANT msgContains %q but got no diagnostics", p.msgContains)
		if !strings.Contains(diags[0].Message, p.msgContains) {
			failures = append(failures, fmt.Sprintf("WANT message to contain %q: got %q", p.msgContains, diags[0].Message))
		}
	}
	if p.msgNotRegexp != "" {
		require.NotEmpty(t, diags, "WANT msgNotRegexp %q but got no diagnostics", p.msgNotRegexp)
		if regexp.MustCompile(p.msgNotRegexp).MatchString(diags[0].Message) {
			failures = append(failures, fmt.Sprintf("WANT message NOT matching %q: got %q", p.msgNotRegexp, diags[0].Message))
		}
	}
	// ── report ───────────────────────────────────────────────────────────────
	t.Log(sb.String())
	for _, f := range failures {
		t.Errorf("%s: %s", p.name, f)
	}
}

// TestLSP_DiagProbes runs all entries in the diagProbes table.
func TestLSP_DiagProbes(t *testing.T) {
	for _, p := range diagProbes {
		p := p
		t.Run(p.name, func(t *testing.T) {
			runDiagProbe(t, p)
		})
	}
}

// TestExtractDiagnostics_NoWrapperNoise verifies that when a StackTrace tree has
// nested errors, only the deepest (most specific) diagnostic is emitted. Wrapper
// messages like "make concrete shape" and the file-path-embedded text that
// stacktrace.Unwrap synthesises for plain fmt.Errorf wrappers must NOT appear
// as separate diagnostics.
func TestExtractDiagnostics_NoWrapperNoise(t *testing.T) {
	import_st := stacktrace.New("decode minLength: cannot decode",
		stacktrace.WithLocation("/tmp/api.raml"),
		stacktrace.WithPosition(stacktrace.NewPosition(16, 5)),
	)
	// Simulate the "unmarshal yaml nodes" wrapper that uses base.KeyPos (line 14).
	unmarshal_st := stacktrace.New("unmarshal yaml nodes",
		stacktrace.WithLocation("/tmp/api.raml"),
		stacktrace.WithPosition(stacktrace.NewPosition(14, 3)),
	).Wrap(import_st)

	result := ExtractDiagnostics(unmarshal_st, nil)

	// Flatten all diagnostics across all URIs.
	var all []protocol.Diagnostic
	for _, diags := range result {
		all = append(all, diags...)
	}

	require.Len(t, all, 1, "only the innermost diagnostic should be emitted, not the wrapper")
	assert.Equal(t, uint32(15), all[0].Range.Start.Line, "line should be 0-based 15 (RAML 1-based 16)")
	assert.Contains(t, all[0].Message, "decode minLength")
}

// TestExtractDiagnostics_IncludeNotFound verifies that when a !include points to a
// non-existent file, the diagnostic lands in the root (calling) document at the
// include site — not in the ghost non-existent file.
func TestExtractDiagnostics_IncludeNotFound(t *testing.T) {
	// Simulate: parse named example → load resource (missing)
	openErr := stacktrace.New("load resource: no such file",
		stacktrace.WithLocation("/tmp/missing.raml"),
		stacktrace.WithPosition(stacktrace.NewPosition(1, 0)),
	)
	includeCtx := stacktrace.New("parse named example: loading",
		stacktrace.WithLocation("/tmp/api.raml"),
		stacktrace.WithPosition(stacktrace.NewPosition(16, 17)),
	).Wrap(openErr)

	result := ExtractDiagnostics(includeCtx, nil)

	var all []protocol.Diagnostic
	for _, diags := range result {
		all = append(all, diags...)
	}

	require.Len(t, all, 1, "exactly one diagnostic should be emitted")
	d := all[0]
	// Must be in the root (api.raml) file, not the missing file.
	apiURI := raml.PathToFileURI("/tmp/api.raml")
	require.Contains(t, result, apiURI, "diagnostic must be in the root file")
	assert.Equal(t, uint32(15), d.Range.Start.Line, "must point at the !include line (0-based)")
	// Message must be the actual error from the leaf, not the noisy wrapper.
	assert.Contains(t, d.Message, "load resource")
}

// TestExtractDiagnostics_IncludedLibraryError verifies that an error inside an
// existing included library is reported at the include site in the root document,
// not inside the library file.
func TestExtractDiagnostics_IncludedLibraryError(t *testing.T) {
	libErr := stacktrace.New("undefined type: Foo",
		stacktrace.WithLocation("/tmp/lib.raml"),
		stacktrace.WithPosition(stacktrace.NewPosition(5, 7)),
	)
	rootCtx := stacktrace.New("decode uses",
		stacktrace.WithLocation("/tmp/api.raml"),
		stacktrace.WithPosition(stacktrace.NewPosition(3, 5)),
	).Wrap(libErr)

	result := ExtractDiagnostics(rootCtx, nil)

	var all []protocol.Diagnostic
	for _, diags := range result {
		all = append(all, diags...)
	}

	require.Len(t, all, 1)
	d := all[0]
	apiURI := raml.PathToFileURI("/tmp/api.raml")
	require.Contains(t, result, apiURI, "diagnostic must be in the root file")
	assert.Equal(t, uint32(2), d.Range.Start.Line, "must point at the uses: line (0-based)")
	assert.Contains(t, d.Message, "undefined type")
}

// TestLSP_DidChange_RepublishesDiagnostics verifies that editing an open document
// triggers re-parse and a new publishDiagnostics notification.
func TestLSP_DidChange_RepublishesDiagnostics(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	// Start with a valid file.
	uri := openDoc(t, s, nc, dir, "api.raml", ramlValid)
	diags := nc.waitDiags(t, uri)
	assert.Empty(t, diags, "initial state should be clean")

	// Clear captured notifications, then inject an error.
	nc.reset()
	ctx := fakeCtx(nc)
	s.cache.SetNotify(ctx.Notify)
	require.NoError(t, s.didChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri(uri),
			},
			Version: 2,
		},
		ContentChanges: []any{
			protocol.TextDocumentContentChangeEventWhole{Text: ramlInvalidType},
		},
	}))

	diags = nc.waitDiags(t, uri)
	assert.NotEmpty(t, diags, "introducing an error should trigger a diagnostic")

	// Clear and fix the file again.
	nc.reset()
	ctx2 := fakeCtx(nc)
	s.cache.SetNotify(ctx2.Notify)
	require.NoError(t, s.didChange(ctx2, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri(uri),
			},
			Version: 3,
		},
		ContentChanges: []any{
			protocol.TextDocumentContentChangeEventWhole{Text: ramlValid},
		},
	}))

	diags = nc.waitDiags(t, uri)
	assert.Empty(t, diags, "fixing the error should clear diagnostics")
}

// TestLSP_SecuritySchemeDescribedByDiagnostic verifies that when a security scheme
// fragment (loaded via !include) uses describedBy with a response body type that is
// not defined within the fragment itself, the resulting diagnostic:
//   - is reported at all (i.e. the error is not silently dropped), and
//   - does NOT land on the is: / trait line of the operation.
//
// This mirrors the pattern found in production APIs (e.g. account-server) where
// each security scheme is an independent fragment file with describedBy that
// references types such as errorScheme defined only in the parent API document.
// The anti-pattern: a security scheme fragment that is not self-contained —
// its describedBy body references a type from the parent API, not the fragment.
func TestLSP_SecuritySchemeDescribedByDiagnostic(t *testing.T) {
	s := newTestServer()
	nc := &notifyCapture{}
	dir := t.TempDir()

	// Security scheme fragment: uses describedBy with a body type (errorBody)
	// that is intentionally NOT defined within this fragment — it only exists in
	// the parent API.  This is the anti-pattern that causes type-resolution
	// failures rooted in the fragment file.
	schemeContent := `#%RAML 1.0 SecurityScheme
type: x-api-key
describedBy:
  headers:
    Cookie:
      type: string
  responses:
    401:
      body:
        application/json:
          type: errorBody
`

	// Write the scheme fragment to disk so the !include can be resolved.
	secDir := filepath.Join(dir, "security")
	require.NoError(t, os.MkdirAll(secDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(secDir, "scheme.raml"), []byte(schemeContent), 0o644))

	// Main API: mirrors the account-server pattern:
	//   - security scheme loaded via !include (separate file, uses describedBy)
	//   - errorBody type defined in the API, not in the scheme fragment
	//   - an operation with both securedBy: and is: (valid trait)
	//
	// Line numbers (0-based) in this literal:
	//   0:  #%RAML 1.0
	//   1:  title: ...
	//   2:  types:
	//   3:    errorBody:
	//   4:      type: object
	//   5:      properties:
	//   6:        message: string
	//   7:  securitySchemes:
	//   8:    myScheme: !include security/scheme.raml
	//   9:  traits:
	//  10:    hasUnauthorized:
	//  11:      responses:
	//  12:        401:
	//  13:          body:
	//  14:            application/json:
	//  15:              type: errorBody
	//  16:  /items:
	//  17:    get:
	//  18:      securedBy:
	//  19:        - myScheme
	//  20:      is:
	//  21:        - hasUnauthorized
	//  22:      responses:
	//  23:        200:
	apiContent := `#%RAML 1.0
title: Security Scheme Antipattern
types:
  errorBody:
    type: object
    properties:
      message: string
securitySchemes:
  myScheme: !include security/scheme.raml
traits:
  hasUnauthorized:
    responses:
      401:
        body:
          application/json:
            type: errorBody
/items:
  get:
    securedBy:
      - myScheme
    is:
      - hasUnauthorized
    responses:
      200:
`

	uri := openDoc(t, s, nc, dir, "api.raml", apiContent)
	diags := nc.waitDiags(t, uri)

	// ── debug log ────────────────────────────────────────────────────────────
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n=== TestLSP_SecuritySchemeDescribedByDiagnostic ===\n")
	for i, line := range strings.Split(apiContent, "\n") {
		fmt.Fprintf(&sb, "  %3d: %s\n", i, line)
	}
	fmt.Fprintf(&sb, "diagnostics (%d):\n", len(diags))
	for i, d := range diags {
		sev := "?"
		if d.Severity != nil {
			switch *d.Severity {
			case protocol.DiagnosticSeverityError:
				sev = "Error"
			case protocol.DiagnosticSeverityWarning:
				sev = "Warning"
			}
		}
		fmt.Fprintf(&sb, "  [%d] L%d c%d-%d [%s] %s\n",
			i, d.Range.Start.Line, d.Range.Start.Character, d.Range.End.Character,
			sev, d.Message)
	}
	t.Log(sb.String())

	// ── assertions ───────────────────────────────────────────────────────────
	require.NotEmpty(t, diags, "expected at least one diagnostic from the type-resolution failure in describedBy")

	// The diagnostic must land at the !include site on the securitySchemes
	// declaration line (line 8, 0-based: "  myScheme: !include security/scheme.raml").
	// Before the fix it was misattributed to the is:/trait line (line 21).
	const schemeIncludeLine = uint32(8)
	var found bool
	for _, d := range diags {
		if d.Severity == nil || *d.Severity != protocol.DiagnosticSeverityError {
			continue
		}
		assert.Equal(t, schemeIncludeLine, d.Range.Start.Line,
			"diagnostic must land at the !include site (line %d), got line %d; message: %s",
			schemeIncludeLine, d.Range.Start.Line, d.Message)
		assert.Contains(t, d.Message, "errorBody",
			"diagnostic message should reference the unresolvable type name")
		found = true
	}
	assert.True(t, found, "no error-severity diagnostic was emitted")
}
