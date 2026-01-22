package raml

import (
	"strings"

	"github.com/acronis/go-stacktrace"
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
}

var ErrNil error
