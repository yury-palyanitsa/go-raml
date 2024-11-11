package raml

import (
	"regexp"
	"strings"

	"github.com/acronis/go-stacktrace"
	"gopkg.in/yaml.v3"
)

func CutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

type Value[T any] struct {
	Value T
	stacktrace.Position
	Location string
}

func (v *Value[T]) decode(node *yaml.Node, location string) error {
	v.Location = location
	v.Position = stacktrace.Position{Line: node.Line, Column: node.Column}
	if node.Tag == "!include" {
		return nil
	}
	if err := node.Decode(&v.Value); err != nil {
		return StacktraceNewWrapped("decode value", err, v.Location, WithNodePosition(node))
	}
	return nil
}

var ErrNil error

var TemplateVariableRe = regexp.MustCompile(`<<\s*(\w+)\s*(\|\s*(![a-z]+))?\s*>>`)
