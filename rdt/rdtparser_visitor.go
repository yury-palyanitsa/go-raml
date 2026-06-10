// Code generated from ./RdtParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package rdt // RdtParser

import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by RdtParser.
type RdtParserVisitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by RdtParser#entrypoint.
	VisitEntrypoint(ctx *EntrypointContext) interface{}

	// Visit a parse tree produced by RdtParser#expression.
	VisitExpression(ctx *ExpressionContext) interface{}

	// Visit a parse tree produced by RdtParser#union.
	VisitUnion(ctx *UnionContext) interface{}

	// Visit a parse tree produced by RdtParser#type.
	VisitType(ctx *TypeContext) interface{}

	// Visit a parse tree produced by RdtParser#primitive.
	VisitPrimitive(ctx *PrimitiveContext) interface{}

	// Visit a parse tree produced by RdtParser#group.
	VisitGroup(ctx *GroupContext) interface{}

	// Visit a parse tree produced by RdtParser#reference.
	VisitReference(ctx *ReferenceContext) interface{}
}
