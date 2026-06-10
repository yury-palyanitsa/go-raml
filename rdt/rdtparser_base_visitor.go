// Code generated from ./RdtParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package rdt // RdtParser

import "github.com/antlr4-go/antlr/v4"

type BaseRdtParserVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseRdtParserVisitor) VisitEntrypoint(ctx *EntrypointContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseRdtParserVisitor) VisitExpression(ctx *ExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseRdtParserVisitor) VisitUnion(ctx *UnionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseRdtParserVisitor) VisitType(ctx *TypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseRdtParserVisitor) VisitPrimitive(ctx *PrimitiveContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseRdtParserVisitor) VisitGroup(ctx *GroupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseRdtParserVisitor) VisitReference(ctx *ReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}
