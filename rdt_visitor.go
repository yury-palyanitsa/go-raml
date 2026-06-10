package raml

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/acronis/go-raml/v3/rdt"
)

// RdtVisitor defines a struct that implements the visitor
type RdtVisitor struct {
	rdt.BaseRdtParserVisitor // Embedding the base visitor class
	raml                     *RAML
}

func NewRdtVisitor(rml *RAML) *RdtVisitor {
	return &RdtVisitor{raml: rml}
}

func (visitor *RdtVisitor) Visit(tree antlr.ParseTree, target *UnknownShape) (Shape, error) {
	// Target is required to isolate anonymous shapes created by Union, Optional and Array syntax.
	// This is done to avoid sharing base shape properties between the original type and implicitly created type.
	switch t := tree.(type) {
	case *rdt.EntrypointContext:
		return visitor.VisitEntrypoint(t, target)
	case *rdt.ExpressionContext:
		return visitor.VisitExpression(t, target)
	case *rdt.TypeContext:
		return visitor.VisitType(t, target)
	case *rdt.PrimitiveContext:
		return visitor.VisitPrimitive(t, target)
	case *rdt.UnionContext:
		return visitor.VisitUnion(t, target)
	case *rdt.GroupContext:
		return visitor.VisitGroup(t, target)
	case *rdt.ReferenceContext:
		return visitor.VisitReference(t, target)
	}
	return nil, fmt.Errorf("unknown node type %T", tree)
}

func (visitor *RdtVisitor) VisitUnionMembers(node antlr.RuleNode, target *UnknownShape) ([]*BaseShape, error) {
	var shapes []*BaseShape
	children := node.GetChildren()
	// Each union member is paired with a pipe separator so we increment by 2.
	// type1 | type2 | type3
	// ^     ^ ^     ^ ^
	// 0     1 2     3 4
	for i := 0; i < len(children); i += 2 {
		baseResolved, implicitAnonShape, _ := visitor.raml.MakeNewShape("", "", target.Location, target.KeyPos, target.ValuePos)
		baseResolved.anchorFrag = target.anchorFrag
		// Propagate the type expression origin so VisitReference can compute
		// each member's file column correctly.
		baseResolved.TypeExpr = target.Base().TypeExpr
		s, err := visitor.Visit(children[i].(antlr.ParseTree), implicitAnonShape.(*UnknownShape))
		if err != nil {
			return nil, fmt.Errorf("visit children: %w", err)
		}
		// Replace with resolved shape
		baseResolved.SetShape(s)
		shapes = append(shapes, baseResolved)
	}
	return shapes, nil
}

func (visitor *RdtVisitor) VisitEntrypoint(ctx *rdt.EntrypointContext, target *UnknownShape) (Shape, error) {
	return visitor.Visit(ctx.GetChildren()[0].(antlr.ParseTree), target)
}

func (visitor *RdtVisitor) VisitExpression(ctx *rdt.ExpressionContext, target *UnknownShape) (Shape, error) {
	return visitor.Visit(ctx.GetChildren()[0].(antlr.ParseTree), target)
}

func (visitor *RdtVisitor) VisitType(ctx *rdt.TypeContext, target *UnknownShape) (Shape, error) {
	children := ctx.GetChildren()
	if len(children) == 0 {
		return nil, fmt.Errorf("empty type")
	}

	// children[0] is the base type, children[1:] are ARRAY_NOTATION/OPTIONAL_NOTATION.
	// Process notations from left to right, recursively building nested wrappers.
	// string[][] -> array of array of string
	// string[]? -> optional array of string
	return visitor.visitTypeNotation(children, 1, target)
}

func (visitor *RdtVisitor) visitTypeNotation(children []antlr.Tree, idx int, target *UnknownShape) (Shape, error) {
	// If we've processed all notations, resolve the base type directly.
	if idx >= len(children) {
		return visitor.Visit(children[0].(antlr.ParseTree), target)
	}

	// This is a notation ([] or ?), create the wrapper and recurse for the rest.
	term := children[idx].(antlr.TerminalNode)
	switch term.GetSymbol().GetTokenType() {
	case rdt.RdtLexerARRAY_NOTATION:
		// Create new anonymous shape for items and resolve the rest into it.
		itemsBase, itemsShape, _ := visitor.raml.MakeNewShape("", "", target.Location, target.KeyPos, target.ValuePos)
		itemsBase.anchorFrag = target.anchorFrag
		itemsBase.TypeExpr = target.Base().TypeExpr
		innerShape, err := visitor.visitTypeNotation(children, idx+1, itemsShape.(*UnknownShape))
		if err != nil {
			return nil, fmt.Errorf("visit array items: %w", err)
		}
		itemsBase.SetShape(innerShape)

		// Build the array shape on target's base.
		arrayShape, err := visitor.raml.MakeConcreteShapeYAML(target.Base(), TypeArray, target.facets)
		if err != nil {
			return nil, fmt.Errorf("make array shape: %w", err)
		}
		arrayShape.(*ArrayShape).ArrayFacets.Items = itemsBase
		return arrayShape, nil

	case rdt.RdtLexerOPTIONAL_NOTATION:
		// Create new anonymous shape for union member and resolve the rest into it.
		memberBase, memberShape, _ := visitor.raml.MakeNewShape("", "", target.Location, target.KeyPos, target.ValuePos)
		memberBase.anchorFrag = target.anchorFrag
		memberBase.TypeExpr = target.Base().TypeExpr
		innerShape, err := visitor.visitTypeNotation(children, idx+1, memberShape.(*UnknownShape))
		if err != nil {
			return nil, fmt.Errorf("visit optional member: %w", err)
		}
		memberBase.SetShape(innerShape)

		// Build the union shape on target's base.
		unionShape, err := visitor.raml.MakeConcreteShapeYAML(target.Base(), TypeUnion, target.facets)
		if err != nil {
			return nil, fmt.Errorf("make union shape: %w", err)
		}
		union := unionShape.(*UnionShape)
		nilBase, _, _ := visitor.raml.MakeNewShape("", TypeNil, target.Location, target.KeyPos, target.ValuePos)
		nilBase.anchorFrag = target.anchorFrag
		union.UnionFacets.AnyOf = []*BaseShape{memberBase, nilBase}
		return unionShape, nil

	default:
		return nil, fmt.Errorf("unexpected token at index %d", idx)
	}
}

func (visitor *RdtVisitor) VisitPrimitive(ctx *rdt.PrimitiveContext, target *UnknownShape) (Shape, error) {
	s, err := visitor.raml.MakeConcreteShapeYAML(target.Base(), ctx.GetText(), nil)
	if err != nil {
		return nil, fmt.Errorf("make concrete shape: %w", err)
	}
	// Record the exact source position of each primitive type keyword so that
	// LSP tooling can surface hover documentation at the call site.
	if target.Base().TypeExpr != nil {
		startCol := target.Base().TypeExpr.ValuePos.Column + ctx.GetStart().GetColumn()
		s.Base().TypeExprRefs = append(s.Base().TypeExprRefs, TypeExprRef{
			Line:        target.Base().TypeExpr.ValuePos.Line,
			Column:      startCol,
			BuiltinType: ctx.GetText(),
		})
	}
	return s, nil
}

func (visitor *RdtVisitor) VisitUnion(ctx *rdt.UnionContext, target *UnknownShape) (Shape, error) {
	children := ctx.GetChildren()
	// Single type -> not actually a union
	if len(children) == 1 {
		return visitor.Visit(children[0].(antlr.ParseTree), target)
	}
	// Resolve target shape into union shape since this is the base.
	shape, err := visitor.raml.MakeConcreteShapeYAML(target.Base(), TypeUnion, target.facets)
	if err != nil {
		return nil, fmt.Errorf("make concrete shape yaml: %w", err)
	}
	//nolint:errcheck // No error check needed because MakeConcreteShapeYAML returns UnionShape for TypeUnion.
	unionShape := shape.(*UnionShape)

	ss, err := visitor.VisitUnionMembers(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("visit children: %w", err)
	}

	unionShape.UnionFacets.AnyOf = ss
	return unionShape, nil
}

func (visitor *RdtVisitor) VisitGroup(ctx *rdt.GroupContext, target *UnknownShape) (Shape, error) {
	// First and last nodes are terminal nodes
	// ( expression )
	// ^     ^      ^
	// 0     1      2
	return visitor.Visit(ctx.GetChildren()[1].(antlr.ParseTree), target)
}

func (visitor *RdtVisitor) VisitReference(ctx *rdt.ReferenceContext, target *UnknownShape) (Shape, error) {
	shapeType := ctx.GetText()

	// Both qualified (lib.TypeName) and unqualified references resolve via the
	// anchor fragment captured at shape creation time. anchorFrag is the lexical
	// scope established via the ParseCtx stack (and, for shapes materialized from
	// stage-1 grafted trait/RT bodies, via the provenance overlay): it is the
	// fragment whose uses: governs both bare names and the "lib." prefix. The
	// physical-location fallback covers shapes built without a parse context
	// (e.g. unwrap-time clones, tests using a bare BaseShape).
	var ref *BaseShape
	var err error
	if target.anchorFrag != nil {
		if target.IsAnnotationType {
			ref, err = target.anchorFrag.GetReferenceAnnotationType(shapeType)
		} else {
			ref, err = target.anchorFrag.GetReferenceType(shapeType)
		}
	} else {
		if target.IsAnnotationType {
			ref, err = visitor.raml.GetReferencedAnnotationType(shapeType, target.Location)
		} else {
			ref, err = visitor.raml.GetReferencedType(shapeType, target.Location)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("get referenced shape: %w", err)
	}
	if ref.ID == target.ID {
		return nil, fmt.Errorf("self recursion %s", shapeType)
	}
	if errResolveShape := visitor.raml.resolveShape(ref); errResolveShape != nil {
		return nil, fmt.Errorf("resolve: %w", errResolveShape)
	}
	s, err := visitor.raml.MakeConcreteShapeYAML(target.Base(), ref.Type, target.facets)
	if err != nil {
		return nil, fmt.Errorf("make concrete shape: %w", err)
	}
	// If target.facets is nil (makeNewShapeYAML returned nil instead of empty array) then reference is an alias.
	if target.facets == nil {
		s.Base().Alias = ref
	} else {
		s.Base().Inherits = append(s.Base().Inherits, ref)
	}
	// Record the exact source position(s) of this type-name reference so LSP tooling
	// can provide go-to-definition and hover at the exact reference site.
	// ANTLR column is 0-based within the expression string; TypeExprCol is the file
	// column of the expression's first character (1-based).
	if target.Base().TypeExpr != nil {
		startCol := target.Base().TypeExpr.ValuePos.Column + ctx.GetStart().GetColumn()
		if prefix, _, found := CutReferenceName(shapeType); found {
			// Qualified reference "lib.TypeName": emit one ref for the library
			// prefix (navigates to the library file) and one for the type name
			// (navigates to the type definition).
			if libLink := visitor.raml.GetLibraryLinkByPrefix(prefix, target.Location); libLink != nil {
				s.Base().TypeExprRefs = append(s.Base().TypeExprRefs, TypeExprRef{
					Line:         target.Base().TypeExpr.ValuePos.Line,
					Column:       startCol,
					LibraryLink:  libLink,
					LibraryAlias: prefix,
				})
			}
			s.Base().TypeExprRefs = append(s.Base().TypeExprRefs, TypeExprRef{
				Line:     target.Base().TypeExpr.ValuePos.Line,
				Column:   startCol + len(prefix) + 1, // +1 for the "."
				Resolved: ref,
			})
		} else {
			s.Base().TypeExprRefs = append(s.Base().TypeExprRefs, TypeExprRef{
				Line:     target.Base().TypeExpr.ValuePos.Line,
				Column:   startCol,
				Resolved: ref,
			})
		}
	}
	return s, nil
}
