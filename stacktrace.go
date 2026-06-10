package raml

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/acronis/go-stacktrace"
	"gopkg.in/yaml.v3"
)

// yamlErrPrefixRe matches the "yaml: line N: " or "line N: " prefix that
// yaml.v3 embeds in TypeError entries and plain syntax-error strings.
// Capture group 1 is the decimal line number.
var yamlErrPrefixRe = regexp.MustCompile(`^(?:yaml:\s+)?line\s+(\d+):\s+`)

// NodeDisplayEndColumn returns the 1-based exclusive end column of a YAML node
// (i.e. the column immediately after the last character, matching LSP range
// conventions). For nodes with a custom tag (e.g. !include), the span covers
// the tag, its trailing space, and the value. For composite (mapping/sequence)
// nodes it returns node.Column unchanged, indicating no displayable extent.
func NodeDisplayEndColumn(node *yaml.Node) int {
	displayLen := len(node.Value)
	// Custom tags (single '!', not standard '!!') appear literally in source:
	// "!include filename.raml" — add tag length plus the separating space.
	if len(node.Tag) > 1 && node.Tag[0] == '!' && node.Tag[1] != '!' {
		displayLen += len(node.Tag) + 1
	}
	if displayLen > 0 {
		return node.Column + displayLen
	}
	return node.Column
}

// NewNodePosition creates a new position from the given node.
// EndLine and EndColumn are set to span the full node value so that LSP
// clients can underline the complete token rather than a single character.
// For nodes with a custom YAML tag (e.g. !include), the visual source text is
// "!tag value", so the span covers the tag, its trailing space, and the value.
func NewNodePosition(node *yaml.Node) stacktrace.Position {
	last := nodeLastLeaf(node)
	return stacktrace.Position{
		Line:      node.Line,
		Column:    node.Column,
		EndLine:   last.Line,
		EndColumn: NodeDisplayEndColumn(last),
	}
}

// WithNodePosition sets the position of the error to the position of the given node.
// EndLine and EndColumn span the full node value so that LSP clients underline
// the complete token (key name or value) rather than a single character.
func WithNodePosition(node *yaml.Node) stacktrace.Option {
	pos := NewNodePosition(node)
	return stacktrace.WithPosition(&pos)
}

// nodeLastLeaf iterates to the deepest last-child descendant of node.
// For scalar/alias nodes it returns node unchanged.
// Iterative (not recursive) to avoid stack overhead on deep trees.
func nodeLastLeaf(node *yaml.Node) *yaml.Node {
	for len(node.Content) > 0 {
		node = node.Content[len(node.Content)-1]
	}
	return node
}

// NodeEndLine returns the last source line occupied by node and all its descendants.
// For scalar/alias nodes this is simply node.Line; for composite nodes it walks
// into the final child.
func NodeEndLine(node *yaml.Node) int {
	if node == nil {
		return 0
	}
	return nodeLastLeaf(node).Line
}

// NodeEndColumn returns the 1-based exclusive end column of the last token in
// node and all its descendants. For scalar/alias nodes this is
// NodeDisplayEndColumn(node); for composite nodes it walks into the final
// child so that a multi-line span ends at the actual last character rather
// than at the composite node's own column.
func NodeEndColumn(node *yaml.Node) int {
	if node == nil {
		return 0
	}
	return NodeDisplayEndColumn(nodeLastLeaf(node))
}

// GetYamlError returns the yaml type error from the given error.
// nil if the error is not a yaml type error.
func GetYamlError(err error) *yaml.TypeError {
	var yamlError *yaml.TypeError
	if errors.As(err, &yamlError) {
		return yamlError
	}
	wErr := errors.Unwrap(err)
	if wErr == nil {
		return nil
	}

	if yamlErr := GetYamlError(wErr); yamlErr != nil {
		toAppend := strings.ReplaceAll(err.Error(), yamlErr.Error(), "")
		toAppend = strings.TrimSuffix(toAppend, ": ")
		// insert the error message in the correct order to the first index
		yamlErr.Errors = append([]string{toAppend}, yamlErr.Errors...)
		return yamlErr
	}
	return nil
}

// FixYamlError strips "yaml: line N: " prefixes from yaml.v3 error messages
// so that LSP diagnostics do not repeat position text already encoded in the
// stacktrace. For TypeError values each individual entry is stripped before
// they are joined. For plain errors the prefix is stripped from the combined
// message string. Returns err unchanged when no yaml prefix is present.
func FixYamlError(err error) error {
	if err == nil {
		return nil
	}
	if yamlErr := GetYamlError(err); yamlErr != nil {
		msgs := make([]string, len(yamlErr.Errors))
		for i, msg := range yamlErr.Errors {
			msgs[i] = yamlErrPrefixRe.ReplaceAllString(msg, "")
		}
		return errors.New(strings.Join(msgs, ": "))
	}
	if orig := err.Error(); yamlErrPrefixRe.MatchString(orig) {
		return errors.New(yamlErrPrefixRe.ReplaceAllString(orig, ""))
	}
	return err
}

// yamlErrorPosition parses the line number from a yaml.v3 error message and
// returns it as a Position. Works for both *yaml.TypeError (uses the first
// individual error) and plain syntax errors. Returns nil when no "line N:"
// pattern is present.
func yamlErrorPosition(err error) *stacktrace.Position {
	if err == nil {
		return nil
	}
	var s string
	if yamlErr := GetYamlError(err); yamlErr != nil && len(yamlErr.Errors) > 0 {
		s = yamlErr.Errors[0]
	} else {
		s = err.Error()
	}
	m := yamlErrPrefixRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	line, e := strconv.Atoi(m[1])
	if e != nil || line <= 0 {
		return nil
	}
	pos := stacktrace.Position{Line: line, Column: 1}
	return &pos
}

func StacktraceNewWrapped(msg string, err error, location string, opts ...stacktrace.Option) *stacktrace.StackTrace {
	// Prepend the extracted yaml error position so that call-site options
	// (e.g. a more specific node position) still take precedence.
	base := []stacktrace.Option{stacktrace.WithLocation(location)}
	if pos := yamlErrorPosition(err); pos != nil {
		base = append(base, stacktrace.WithPosition(pos))
	}
	return stacktrace.NewWrapped(msg, FixYamlError(err), append(base, opts...)...)
}

func StacktraceNew(msg string, location string, opts ...stacktrace.Option) *stacktrace.StackTrace {
	opts = append(opts, stacktrace.WithLocation(location))
	return stacktrace.New(msg, opts...)
}
