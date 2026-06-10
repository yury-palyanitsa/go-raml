package raml

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/acronis/go-stacktrace"

	"github.com/acronis/go-raml/v3/rdt"
)

/*
resolveShapes resolves all unresolved (UnknownShape) shapes in the RAML.

NOTE: Unresolved shapes is a linked list that is populated by the YAML parser. Shape parsing occurs in two places:

1. During the RAML fragment parsing. Shapes that could not be determined during the parsing process
are stored in `unresolvedShapes` as UnknownShape. UnknownShape includes YAML nodes that can be parsed later.

2. During the shape resolution. YAML parser is invoked on YAML nodes that UnknownShape has stored.

This helps to avoid additional traversals of nested shapes since the traverse is already done by YAML parser and it will
generate UnknownShapes and add them to `unresolvedShapes` recursively as they occur.
*/
func (r *RAML) resolveShapes() error {
	var acc stacktrace.Accumulator
	for len(r.unresolvedShapes) > 0 {
		base := r.unresolvedShapes[0]
		r.unresolvedShapes = r.unresolvedShapes[1:]
		if err := r.resolveShape(base); err != nil {
			acc.Add(StacktraceNewWrapped("resolve shape", err, base.Location,
				stacktrace.WithPosition(&base.KeyPos),
				stacktrace.WithType(StacktraceTypeResolving)))
		}
	}
	if st := acc.Result(); st != nil {
		return st
	}
	return nil
}

// resolveDomainExtensions resolves all domain extensions in the RAML.
func (r *RAML) resolveDomainExtensions() error {
	var acc stacktrace.Accumulator
	for _, de := range r.domainExtensions {
		if err := r.resolveDomainExtension(de); err != nil {
			acc.Add(StacktraceNewWrapped("resolve domain extension", err, de.Location,
				stacktrace.WithPosition(&de.KeyPos),
				stacktrace.WithType(StacktraceTypeResolving)))
		}
	}
	if st := acc.Result(); st != nil {
		return st
	}
	return nil
}

func (r *RAML) resolveDomainExtension(de *DomainExtension) error {
	var ref *BaseShape
	var err error
	if de.anchorFrag != nil {
		ref, err = de.anchorFrag.GetReferenceAnnotationType(de.Name)
	} else {
		ref, err = r.GetReferencedAnnotationType(de.Name, de.Location)
	}
	if err != nil {
		return StacktraceNewWrapped("get referenced shape", err, de.Location, stacktrace.WithPosition(&de.KeyPos))
	}

	de.DefinedBy = ref

	return nil
}

func (r *RAML) resolveMultipleInheritance(base *BaseShape, shape *UnknownShape) error {
	inherits := base.Inherits
	for _, inherit := range inherits {
		if err := r.resolveShape(inherit); err != nil {
			return StacktraceNewWrapped("resolve inherit", err, base.Location, stacktrace.WithPosition(&base.KeyPos))
		}
	}
	// Multiple inheritance validation to be performed in a separate validation stage
	_, err := r.MakeConcreteShapeYAML(base, inherits[0].Type, shape.facets)
	if err != nil {
		return StacktraceNewWrapped("make concrete shape", err, base.Location, stacktrace.WithPosition(&base.KeyPos))
	}
	return nil
}

func (r *RAML) resolveLink(base *BaseShape, shape *UnknownShape) error {
	linkShape := base.Link.Shape
	if err := r.resolveShape(linkShape); err != nil {
		return StacktraceNewWrapped("resolve link shape", err, base.Location, stacktrace.WithPosition(&base.KeyPos))
	}
	_, err := r.MakeConcreteShapeYAML(base, linkShape.Type, shape.facets)
	if err != nil {
		return StacktraceNewWrapped("make concrete shape", err, base.Location, stacktrace.WithPosition(&base.KeyPos))
	}
	return nil
}

type CustomErrorListener struct {
	*antlr.DefaultErrorListener // Embed default which ensures we fit the interface
	Stacktrace                  *stacktrace.StackTrace
	position                    stacktrace.Position
	location                    string
}

func (c *CustomErrorListener) SyntaxError(
	_ antlr.Recognizer,
	offendingSymbol any,
	_, _ int,
	msg string,
	_ antlr.RecognitionException,
) {
	symbolInfoOpt := stacktrace.WithInfo("offendingSymbol", offendingSymbol)
	posOpt := stacktrace.WithPosition(
		&stacktrace.Position{
			Line:   c.position.Line,
			Column: c.position.Column,
		},
	)
	if c.Stacktrace == nil {
		c.Stacktrace = StacktraceNew("antlr error", c.location)
	}
	c.Stacktrace = c.Stacktrace.Append(StacktraceNew(msg, c.location, posOpt, symbolInfoOpt))
}

// resolveShape resolves an unknown shape in-place.
// NOTE: This function is not thread-safe. Use Clone() to create a copy of the shape before resolving if necessary.
func (r *RAML) resolveShape(base *BaseShape) error {
	shape := base.Shape
	if shape == nil {
		return StacktraceNew("shape is nil", base.Location, stacktrace.WithPosition(&base.KeyPos))
	}

	// Skip already resolved shapes
	unknownShape, ok := shape.(*UnknownShape)
	if !ok {
		return nil
	}

	// Detect circular resolution: if this shape is already mid-resolution, it's a type cycle.
	if base.ShapeVisited {
		return StacktraceNew("cyclic type reference", base.Location,
			stacktrace.WithPosition(&base.KeyPos))
	}
	base.ShapeVisited = true
	defer func() { base.ShapeVisited = false }()

	if base.Link != nil {
		err := r.resolveLink(base, unknownShape)
		if err != nil {
			return StacktraceNewWrapped("resolve link", err, base.Location,
				stacktrace.WithPosition(&base.KeyPos))
		}
		return nil
	}

	shapeType := base.Type
	if shapeType == TypeComposite {
		// Special case for multiple inheritance
		err := r.resolveMultipleInheritance(base, unknownShape)
		if err != nil {
			return StacktraceNewWrapped("resolve multiple inheritance", err, base.Location,
				stacktrace.WithPosition(&base.KeyPos))
		}
		return nil
	}

	is := antlr.NewInputStream(shapeType)
	customListener := &CustomErrorListener{
		location: base.Location,
		position: base.KeyPos,
	}
	lexer := rdt.NewRdtLexer(is)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(customListener)

	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)

	rdtParser := rdt.NewRdtParser(tokens)
	rdtParser.RemoveErrorListeners()
	rdtParser.AddErrorListener(customListener)

	visitor := NewRdtVisitor(r)
	tree := rdtParser.Entrypoint()

	if customListener.Stacktrace != nil {
		return customListener.Stacktrace
	}

	_, err := visitor.Visit(tree, unknownShape)
	if err != nil {
		return StacktraceNewWrapped("visit type expression", err, base.Location,
			stacktrace.WithPosition(&base.KeyPos))
	}
	// Do not set the resulting shape to the base shape, since it is already set in the visitor.
	return nil
}
