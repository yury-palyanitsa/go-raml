package raml

// TestPositions verifies that the Line stored on every named model element
// matches the 1-based line in the RAML source where that element's NAME
// (the YAML key) appears — not the line where its value body begins.
//
// Run with: go test -run TestPositions -v

import (
	"path/filepath"
	"strings"
	"testing"
)

// posLibraryRAML is a self-contained Library document covering every type and
// property kind that TestPositions inspects. Keep it small and tightly
// formatted so the source lines are obvious at a glance.
const posLibraryRAML = `#%RAML 1.0 Library

types:
  Paging:
    type: object
    properties:
      cursor: string

  Obj1: object

  StringShape:
    type: string

  ObjectWithPropertiesType:
    type: object
    properties:
      required?:
        required: true
        type: object
      optional:
        required: false
        type: object

  ArrayShape:
    type: array
    items: string

  UnionType:
    type: string | number

  InlineType: string

  A:
    properties:
      a: string

  B:
    type: A
    properties:
      b: string
`

// posAPIRAML is the minimal API document needed to assert endpoint/operation
// positions. It deliberately has no includes so it parses standalone.
const posAPIRAML = `#%RAML 1.0
title: Test API

/:
  get:
    responses:
      200:
`

// splitLines returns the 1-based-indexable lines of a RAML source string.
func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// parseInlineRAML parses content directly from memory via ParseFromString.
// baseDir is required to be absolute by the parser but is used only to derive
// the document's synthetic file URI — no files are written. The synthetic
// path is returned so callers can look the parsed fragment up via
// GetFragment / GetFragmentTypePtrs.
func parseInlineRAML(t *testing.T, baseDir, fileName, content string) (*RAML, string) {
	t.Helper()
	r, err := ParseFromString(content, fileName, baseDir, OptWithValidate())
	if err != nil {
		t.Logf("%s parse warnings: %v", fileName, err)
	}
	return r, filepath.Join(baseDir, fileName)
}

// checkLine asserts that lines[gotLine-1] contains needle and logs the result.
func checkLine(t *testing.T, lines []string, label string, gotLine int, needle string) {
	t.Helper()
	if gotLine < 1 || gotLine > len(lines) {
		t.Errorf("%-50s Line=%d out of range (file has %d lines)", label, gotLine, len(lines))
		return
	}
	actual := lines[gotLine-1]
	if !strings.Contains(actual, needle) {
		// find where needle actually is
		for i, l := range lines {
			if strings.Contains(l, needle) {
				t.Errorf("%-50s Line=%d %q  — want line %d which has %q", label, gotLine, actual, i+1, needle)
				return
			}
		}
		t.Errorf("%-50s Line=%d %q  — needle %q not found in file", label, gotLine, actual, needle)
	} else {
		t.Logf("%-50s Line=%d OK  (%q)", label, gotLine, strings.TrimSpace(actual))
	}
}

func TestPositions(t *testing.T) {
	tmp := t.TempDir()

	// ── library ────────────────────────────────────────────────────────────
	libRAML, libPath := parseInlineRAML(t, tmp, "library.raml", posLibraryRAML)
	libLines := splitLines(posLibraryRAML)

	libTypes := libRAML.GetFragmentTypePtrs(libPath)
	libChecks := []struct{ name, needle string }{
		{"Paging", "  Paging:"},
		{"Obj1", "  Obj1:"},
		{"StringShape", "  StringShape:"},
		{"ObjectWithPropertiesType", "  ObjectWithPropertiesType:"},
		{"ArrayShape", "  ArrayShape:"},
		{"UnionType", "  UnionType:"},
		{"A", "  A:"},
		{"B", "  B:"},
		{"InlineType", "  InlineType:"},
	}
	for _, tc := range libChecks {
		bs := libTypes[tc.name]
		if bs == nil {
			t.Errorf("type %q not found in library", tc.name)
			continue
		}
		checkLine(t, libLines, "lib type "+tc.name, bs.KeyPos.Line, tc.needle)
	}

	// Properties inside ObjectWithPropertiesType
	owpt := libTypes["ObjectWithPropertiesType"]
	if owpt != nil {
		if obj, ok := owpt.Shape.(*ObjectShape); ok {
			propChecks := []struct{ name, needle string }{
				{"required?", "      required?:"},
				{"optional", "      optional:"},
			}
			for _, pc := range propChecks {
				if p, ok := obj.Properties.Get(pc.name); ok {
					checkLine(t, libLines, "lib prop "+pc.name, p.Base.KeyPos.Line, pc.needle)
				} else {
					t.Errorf("property %q not found", pc.name)
				}
			}
		}
	}

	// Properties inside Paging
	paging := libTypes["Paging"]
	if paging != nil {
		if obj, ok := paging.Shape.(*ObjectShape); ok {
			if p, ok := obj.Properties.Get("cursor"); ok {
				checkLine(t, libLines, "lib prop Paging.cursor", p.Base.KeyPos.Line, "      cursor:")
			}
		}
	}

	// ── api ────────────────────────────────────────────────────────────────
	apiRAML, apiPath := parseInlineRAML(t, tmp, "api.raml", posAPIRAML)
	apiLines := splitLines(posAPIRAML)

	apiFrag, ok := apiRAML.GetFragment(apiPath).(*APIFragment)
	if !ok || apiFrag == nil {
		t.Fatal("api is not an APIFragment")
	}

	// Root endpoint
	root, ok := apiFrag.EndPoints.Get("/")
	if !ok {
		t.Fatal("endpoint / not found")
	}
	checkLine(t, apiLines, "api endpoint /", root.KeyPos.Line, "/:")

	// GET operation on /
	get, ok := root.Operations.Get("get")
	if !ok {
		t.Fatal("operation GET / not found")
	}
	checkLine(t, apiLines, "api operation GET /", get.KeyPos.Line, "  get:")
}
