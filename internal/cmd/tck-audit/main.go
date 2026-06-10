package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/acronis/go-raml/v3"
	"github.com/acronis/go-stacktrace"
)

// fixtureResult contains the test result for a single fixture
type fixtureResult struct {
	fixture    string
	err        error
	bestMsg    string               // most specific message for use in tckErrSpec
	bestPos    *stacktrace.Position // position associated with bestMsg
	allMsgs    []msgPos             // all messages with positions
	errorFound bool
}

type msgPos struct {
	msg string
	pos *stacktrace.Position
}

func main() {
	tckDir := flag.String("tck-dir", "C:\\Sources\\go-raml\\raml-tck", "Path to raml-tck directory")
	maxFixtures := flag.Int("max", 1000, "Maximum number of fixtures to test")
	startPattern := flag.String("start", "", "Start testing from this pattern")
	format := flag.String("format", "text", "Output format: text or go")
	flag.Parse()

	if _, err := os.Stat(*tckDir); err != nil {
		fmt.Printf("raml-tck directory not found at %s: %v\n", *tckDir, err)
		os.Exit(1)
	}

	fixtures := findInvalidFixtures(*tckDir)
	sort.Strings(fixtures)

	if *startPattern != "" {
		found := false
		var filtered []string
		for _, f := range fixtures {
			if found || strings.Contains(f, *startPattern) {
				found = true
				filtered = append(filtered, f)
			}
		}
		fixtures = filtered
	}

	if len(fixtures) > *maxFixtures {
		fixtures = fixtures[:*maxFixtures]
	}

	results := make([]fixtureResult, 0, len(fixtures))
	for _, fixture := range fixtures {
		results = append(results, testFixture(*tckDir, fixture))
	}

	switch *format {
	case "go":
		printGoCode(results)
	default:
		printText(results)
	}
}

// printText prints human-readable output.
func printText(results []fixtureResult) {
	fmt.Printf("Tested %d fixtures\n\n", len(results))
	for i, r := range results {
		fmt.Printf("%d. %s\n", i+1, r.fixture)
		if r.errorFound {
			fmt.Printf("   Best message: %s\n", r.bestMsg)
			if r.bestPos != nil {
				fmt.Printf("   Position: Line %d Col %d - EndLine %d EndCol %d\n",
					r.bestPos.Line, r.bestPos.Column, r.bestPos.EndLine, r.bestPos.EndColumn)
			}
			if len(r.allMsgs) > 1 {
				fmt.Printf("   All messages:\n")
				for j, mp := range r.allMsgs {
					posInfo := ""
					if mp.pos != nil {
						posInfo = fmt.Sprintf(" [%d:%d-%d:%d]", mp.pos.Line, mp.pos.Column, mp.pos.EndLine, mp.pos.EndColumn)
					}
					fmt.Printf("     %d) %s%s\n", j+1, mp.msg, posInfo)
				}
			}
		} else {
			fmt.Printf("   NO ERROR (fixture should fail but does not)\n")
		}
		fmt.Println()
	}
}

// printGoCode outputs a []tckErrSpec{} draft as Go source.
func printGoCode(results []fixtureResult) {
	fmt.Println("// Auto-generated draft -- review each entry before committing.")
	fmt.Println("// Fixtures marked NO_ERROR currently produce no error and need implementation fixes.")
	fmt.Println("[]tckErrSpec{")
	for _, r := range results {
		fixture := filepath.ToSlash(r.fixture)
		if !r.errorFound {
			// Comment out entries that don't produce errors yet.
			fmt.Printf("\t// TODO: currently produces no error -- needs parser fix\n")
			fmt.Printf("\t// {\n")
			fmt.Printf("\t// \tfixture:     %q,\n", fixture)
			fmt.Printf("\t// \tmsgContains: \"TODO\",\n")
			fmt.Printf("\t// \tline: -1, col: -1, endLine: -1, endCol: -1,\n")
			fmt.Printf("\t// },\n")
		} else {
			pos := r.bestPos
			line, col, endLine, endCol := -1, -1, -1, -1
			if pos != nil {
				line = pos.Line
				col = pos.Column
				endLine = pos.EndLine
				endCol = pos.EndColumn
			}
			// Print all messages as a comment chain above for easy review.
			if len(r.allMsgs) > 1 {
				parts := make([]string, 0, len(r.allMsgs))
				for _, mp := range r.allMsgs {
					parts = append(parts, fmt.Sprintf("%q", mp.msg))
				}
				fmt.Printf("\t// error chain: %s\n", strings.Join(parts, " -> "))
			}

			// Use the most specific trailing leaf of a colon-chained message.
			msgContains := leafSegment(r.bestMsg)

			fmt.Printf("\t{\n")
			fmt.Printf("\t\tfixture:     %q,\n", fixture)
			fmt.Printf("\t\tmsgContains: %q,\n", msgContains)
			fmt.Printf("\t\tline: %d, col: %d, endLine: %d, endCol: %d,\n", line, col, endLine, endCol)
			fmt.Printf("\t},\n")
		}
	}
	fmt.Println("}")
}

// leafSegment returns the last meaningful segment of a ": "-separated error chain,
// unless the last segment is too short (< 6 chars) or a generic path prefix.
func leafSegment(msg string) string {
	// Strip any absolute path prefix like "C:\...\foo.raml:12:3" that stacktrace
	// sometimes emits as part of a message.
	if colonIdx := strings.LastIndex(msg, ".raml:"); colonIdx != -1 {
		// Message is a file path annotation — use the whole thing but trim the path portion.
		after := msg[colonIdx+6:] // skip ".raml:"
		if rest := strings.TrimSpace(after); rest != "" {
			// Just use the original message stripped of file path context.
			// Find the last ": " before the path to get the semantic part.
			subject := msg[:colonIdx+6]
			lastColon := strings.LastIndex(subject, ": ")
			if lastColon != -1 {
				candidate := strings.TrimSpace(subject[:lastColon])
				if len(candidate) >= 6 {
					msg = candidate
				}
			}
		}
	}

	parts := strings.Split(msg, ": ")
	// Walk back from the end to find the first segment that is meaningful (>= 6 chars
	// and not a generic structural prefix like "validate properties").
	generic := map[string]bool{
		"validate properties":               true,
		"validate shapes":                   true,
		"validate shape commons":            true,
		"check domain extension":            true,
		"resolve domain extensions":         true,
		"resolve domain extension":          true,
		"apply resource types":              true,
		"apply traits":                      true,
		"apply security schemes":            true,
		"compile resource type":             true,
		"compile trait":                     true,
		"parse api":                         true,
		"parse library":                     true,
		"parse data type":                   true,
		"parse named example":               true,
		"parse security scheme fragment":    true,
		"parse resource type fragment":      true,
		"parse documentation item fragment": true,
		"unwrap shapes":                     true,
		"resolve shapes":                    true,
		"load resource":                     true,
		"get referenced shape":              true,
	}
	for i := len(parts) - 1; i >= 0; i-- {
		seg := strings.TrimSpace(parts[i])
		if len(seg) >= 6 && !generic[seg] {
			return seg
		}
	}
	// Fall back to full message if nothing better found.
	return msg
}

func findInvalidFixtures(tckDir string) []string {
	var fixtures []string
	err := filepath.Walk(tckDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.Contains(path, "invalid") && strings.HasSuffix(path, ".raml") {
			rel, _ := filepath.Rel(tckDir, path)
			fixtures = append(fixtures, rel)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
	}
	return fixtures
}

func testFixture(tckDir, fixture string) fixtureResult {
	path := filepath.Join(tckDir, fixture)
	_, err := raml.ParseFromPath(path, raml.OptWithUnwrap(), raml.OptWithValidate())

	result := fixtureResult{
		fixture:    fixture,
		err:        err,
		errorFound: err != nil,
	}

	if err != nil {
		var st *stacktrace.StackTrace
		if errors.As(err, &st) {
			result.allMsgs = collectAllMsgPos(st)
			// Determine the best message via pickBestNode (deepest leaf with position),
			// then resolve the position via findFirstMatch — mirroring what findSTNode
			// in the test harness does. This ensures the generated line/col values
			// match exactly what the test will find at verification time.
			best := pickBestNode(st)
			if best != nil {
				result.bestMsg = best.Message
				msgContains := leafSegment(best.Message)
				if matched := findFirstMatch(st, msgContains); matched != nil {
					result.bestPos = matched.Position
				} else {
					result.bestPos = best.Position
				}
			}
		} else {
			result.bestMsg = err.Error()
		}
	}

	return result
}

// findFirstMatch mirrors findSTNode in the test harness: it returns the first
// node in pre-order DFS (Wrapped before List) whose Message contains substr.
func findFirstMatch(root *stacktrace.StackTrace, substr string) *stacktrace.StackTrace {
	if root == nil {
		return nil
	}
	if strings.Contains(root.Message, substr) {
		return root
	}
	if found := findFirstMatch(root.Wrapped, substr); found != nil {
		return found
	}
	for _, child := range root.List {
		if found := findFirstMatch(child, substr); found != nil {
			return found
		}
	}
	return nil
}

// pickBestNode returns the deepest leaf node that has a position, or the deepest
// non-empty node, or the root if nothing better is found.
func pickBestNode(root *stacktrace.StackTrace) *stacktrace.StackTrace {
	var withPos, noPos *stacktrace.StackTrace

	var walk func(n *stacktrace.StackTrace)
	walk = func(n *stacktrace.StackTrace) {
		if n == nil || n.Message == "" {
			return
		}
		isLeaf := n.Wrapped == nil && len(n.List) == 0
		if isLeaf {
			if n.Position != nil {
				withPos = n
			} else {
				noPos = n
			}
		}
		walk(n.Wrapped)
		for _, child := range n.List {
			walk(child)
		}
	}
	walk(root)

	if withPos != nil {
		return withPos
	}
	if noPos != nil {
		return noPos
	}
	return root
}

// collectAllMsgPos walks the stack trace tree and collects all messages with positions.
func collectAllMsgPos(st *stacktrace.StackTrace) []msgPos {
	var result []msgPos
	seen := make(map[string]bool)

	var walk func(n *stacktrace.StackTrace)
	walk = func(n *stacktrace.StackTrace) {
		if n == nil {
			return
		}
		if n.Message != "" && !seen[n.Message] {
			result = append(result, msgPos{msg: n.Message, pos: n.Position})
			seen[n.Message] = true
		}
		walk(n.Wrapped)
		for _, child := range n.List {
			walk(child)
		}
	}
	walk(st)
	return result
}
