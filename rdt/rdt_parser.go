// Code generated from ./RdtParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package rdt // RdtParser

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/antlr4-go/antlr/v4"
)

// Suppress unused import errors
var _ = fmt.Printf
var _ = strconv.Itoa
var _ = sync.Once{}

type RdtParser struct {
	*antlr.BaseParser
}

var RdtParserParserStaticData struct {
	once                   sync.Once
	serializedATN          []int32
	LiteralNames           []string
	SymbolicNames          []string
	RuleNames              []string
	PredictionContextCache *antlr.PredictionContextCache
	atn                    *antlr.ATN
	decisionToDFA          []*antlr.DFA
}

func rdtparserParserInit() {
	staticData := &RdtParserParserStaticData
	staticData.LiteralNames = []string{
		"", "'('", "')'", "'|'", "'[]'", "'?'", "'.'", "'string'", "'integer'",
		"'number'", "'boolean'", "'datetime'", "'time-only'", "'datetime-only'",
		"'date-only'", "'file'", "'nil'", "'any'", "'array'", "'object'", "'union'",
	}
	staticData.SymbolicNames = []string{
		"", "LPAREN", "RPAREN", "PIPE", "ARRAY_NOTATION", "OPTIONAL_NOTATION",
		"DOT", "STRING_TYPE", "INTEGER_TYPE", "NUMBER_TYPE", "BOOLEAN_TYPE",
		"DATETIME_TYPE", "TIME_ONLY_TYPE", "DATETIME_ONLY_TYPE", "DATE_ONLY_TYPE",
		"FILE_TYPE", "NIL_TYPE", "ANY_TYPE", "ARRAY_TYPE", "OBJECT_TYPE", "UNION_TYPE",
		"IDENTIFIER", "WS",
	}
	staticData.RuleNames = []string{
		"entrypoint", "expression", "union", "type", "primitive", "group", "reference",
	}
	staticData.PredictionContextCache = antlr.NewPredictionContextCache()
	staticData.serializedATN = []int32{
		4, 1, 22, 77, 2, 0, 7, 0, 2, 1, 7, 1, 2, 2, 7, 2, 2, 3, 7, 3, 2, 4, 7,
		4, 2, 5, 7, 5, 2, 6, 7, 6, 1, 0, 1, 0, 1, 0, 1, 1, 1, 1, 1, 2, 1, 2, 5,
		2, 22, 8, 2, 10, 2, 12, 2, 25, 9, 2, 1, 2, 1, 2, 5, 2, 29, 8, 2, 10, 2,
		12, 2, 32, 9, 2, 1, 2, 5, 2, 35, 8, 2, 10, 2, 12, 2, 38, 9, 2, 1, 3, 1,
		3, 1, 3, 3, 3, 43, 8, 3, 1, 3, 5, 3, 46, 8, 3, 10, 3, 12, 3, 49, 9, 3,
		1, 3, 3, 3, 52, 8, 3, 1, 4, 1, 4, 1, 5, 1, 5, 5, 5, 58, 8, 5, 10, 5, 12,
		5, 61, 9, 5, 1, 5, 1, 5, 5, 5, 65, 8, 5, 10, 5, 12, 5, 68, 9, 5, 1, 5,
		1, 5, 1, 6, 1, 6, 1, 6, 3, 6, 75, 8, 6, 1, 6, 0, 0, 7, 0, 2, 4, 6, 8, 10,
		12, 0, 1, 1, 0, 7, 20, 79, 0, 14, 1, 0, 0, 0, 2, 17, 1, 0, 0, 0, 4, 19,
		1, 0, 0, 0, 6, 42, 1, 0, 0, 0, 8, 53, 1, 0, 0, 0, 10, 55, 1, 0, 0, 0, 12,
		71, 1, 0, 0, 0, 14, 15, 3, 2, 1, 0, 15, 16, 5, 0, 0, 1, 16, 1, 1, 0, 0,
		0, 17, 18, 3, 4, 2, 0, 18, 3, 1, 0, 0, 0, 19, 36, 3, 6, 3, 0, 20, 22, 5,
		22, 0, 0, 21, 20, 1, 0, 0, 0, 22, 25, 1, 0, 0, 0, 23, 21, 1, 0, 0, 0, 23,
		24, 1, 0, 0, 0, 24, 26, 1, 0, 0, 0, 25, 23, 1, 0, 0, 0, 26, 30, 5, 3, 0,
		0, 27, 29, 5, 22, 0, 0, 28, 27, 1, 0, 0, 0, 29, 32, 1, 0, 0, 0, 30, 28,
		1, 0, 0, 0, 30, 31, 1, 0, 0, 0, 31, 33, 1, 0, 0, 0, 32, 30, 1, 0, 0, 0,
		33, 35, 3, 6, 3, 0, 34, 23, 1, 0, 0, 0, 35, 38, 1, 0, 0, 0, 36, 34, 1,
		0, 0, 0, 36, 37, 1, 0, 0, 0, 37, 5, 1, 0, 0, 0, 38, 36, 1, 0, 0, 0, 39,
		43, 3, 8, 4, 0, 40, 43, 3, 10, 5, 0, 41, 43, 3, 12, 6, 0, 42, 39, 1, 0,
		0, 0, 42, 40, 1, 0, 0, 0, 42, 41, 1, 0, 0, 0, 43, 47, 1, 0, 0, 0, 44, 46,
		5, 4, 0, 0, 45, 44, 1, 0, 0, 0, 46, 49, 1, 0, 0, 0, 47, 45, 1, 0, 0, 0,
		47, 48, 1, 0, 0, 0, 48, 51, 1, 0, 0, 0, 49, 47, 1, 0, 0, 0, 50, 52, 5,
		5, 0, 0, 51, 50, 1, 0, 0, 0, 51, 52, 1, 0, 0, 0, 52, 7, 1, 0, 0, 0, 53,
		54, 7, 0, 0, 0, 54, 9, 1, 0, 0, 0, 55, 59, 5, 1, 0, 0, 56, 58, 5, 22, 0,
		0, 57, 56, 1, 0, 0, 0, 58, 61, 1, 0, 0, 0, 59, 57, 1, 0, 0, 0, 59, 60,
		1, 0, 0, 0, 60, 62, 1, 0, 0, 0, 61, 59, 1, 0, 0, 0, 62, 66, 3, 2, 1, 0,
		63, 65, 5, 22, 0, 0, 64, 63, 1, 0, 0, 0, 65, 68, 1, 0, 0, 0, 66, 64, 1,
		0, 0, 0, 66, 67, 1, 0, 0, 0, 67, 69, 1, 0, 0, 0, 68, 66, 1, 0, 0, 0, 69,
		70, 5, 2, 0, 0, 70, 11, 1, 0, 0, 0, 71, 74, 5, 21, 0, 0, 72, 73, 5, 6,
		0, 0, 73, 75, 5, 21, 0, 0, 74, 72, 1, 0, 0, 0, 74, 75, 1, 0, 0, 0, 75,
		13, 1, 0, 0, 0, 9, 23, 30, 36, 42, 47, 51, 59, 66, 74,
	}
	deserializer := antlr.NewATNDeserializer(nil)
	staticData.atn = deserializer.Deserialize(staticData.serializedATN)
	atn := staticData.atn
	staticData.decisionToDFA = make([]*antlr.DFA, len(atn.DecisionToState))
	decisionToDFA := staticData.decisionToDFA
	for index, state := range atn.DecisionToState {
		decisionToDFA[index] = antlr.NewDFA(state, index)
	}
}

// RdtParserInit initializes any static state used to implement RdtParser. By default the
// static state used to implement the parser is lazily initialized during the first call to
// NewRdtParser(). You can call this function if you wish to initialize the static state ahead
// of time.
func RdtParserInit() {
	staticData := &RdtParserParserStaticData
	staticData.once.Do(rdtparserParserInit)
}

// NewRdtParser produces a new parser instance for the optional input antlr.TokenStream.
func NewRdtParser(input antlr.TokenStream) *RdtParser {
	RdtParserInit()
	this := new(RdtParser)
	this.BaseParser = antlr.NewBaseParser(input)
	staticData := &RdtParserParserStaticData
	this.Interpreter = antlr.NewParserATNSimulator(this, staticData.atn, staticData.decisionToDFA, staticData.PredictionContextCache)
	this.RuleNames = staticData.RuleNames
	this.LiteralNames = staticData.LiteralNames
	this.SymbolicNames = staticData.SymbolicNames
	this.GrammarFileName = "RdtParser.g4"

	return this
}

// RdtParser tokens.
const (
	RdtParserEOF                = antlr.TokenEOF
	RdtParserLPAREN             = 1
	RdtParserRPAREN             = 2
	RdtParserPIPE               = 3
	RdtParserARRAY_NOTATION     = 4
	RdtParserOPTIONAL_NOTATION  = 5
	RdtParserDOT                = 6
	RdtParserSTRING_TYPE        = 7
	RdtParserINTEGER_TYPE       = 8
	RdtParserNUMBER_TYPE        = 9
	RdtParserBOOLEAN_TYPE       = 10
	RdtParserDATETIME_TYPE      = 11
	RdtParserTIME_ONLY_TYPE     = 12
	RdtParserDATETIME_ONLY_TYPE = 13
	RdtParserDATE_ONLY_TYPE     = 14
	RdtParserFILE_TYPE          = 15
	RdtParserNIL_TYPE           = 16
	RdtParserANY_TYPE           = 17
	RdtParserARRAY_TYPE         = 18
	RdtParserOBJECT_TYPE        = 19
	RdtParserUNION_TYPE         = 20
	RdtParserIDENTIFIER         = 21
	RdtParserWS                 = 22
)

// RdtParser rules.
const (
	RdtParserRULE_entrypoint = 0
	RdtParserRULE_expression = 1
	RdtParserRULE_union      = 2
	RdtParserRULE_type       = 3
	RdtParserRULE_primitive  = 4
	RdtParserRULE_group      = 5
	RdtParserRULE_reference  = 6
)

// IEntrypointContext is an interface to support dynamic dispatch.
type IEntrypointContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	Expression() IExpressionContext
	EOF() antlr.TerminalNode

	// IsEntrypointContext differentiates from other interfaces.
	IsEntrypointContext()
}

type EntrypointContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyEntrypointContext() *EntrypointContext {
	var p = new(EntrypointContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_entrypoint
	return p
}

func InitEmptyEntrypointContext(p *EntrypointContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_entrypoint
}

func (*EntrypointContext) IsEntrypointContext() {}

func NewEntrypointContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *EntrypointContext {
	var p = new(EntrypointContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = RdtParserRULE_entrypoint

	return p
}

func (s *EntrypointContext) GetParser() antlr.Parser { return s.parser }

func (s *EntrypointContext) Expression() IExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExpressionContext)
}

func (s *EntrypointContext) EOF() antlr.TerminalNode {
	return s.GetToken(RdtParserEOF, 0)
}

func (s *EntrypointContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *EntrypointContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *EntrypointContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case RdtParserVisitor:
		return t.VisitEntrypoint(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *RdtParser) Entrypoint() (localctx IEntrypointContext) {
	localctx = NewEntrypointContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 0, RdtParserRULE_entrypoint)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(14)
		p.Expression()
	}
	{
		p.SetState(15)
		p.Match(RdtParserEOF)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IExpressionContext is an interface to support dynamic dispatch.
type IExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	Union() IUnionContext

	// IsExpressionContext differentiates from other interfaces.
	IsExpressionContext()
}

type ExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyExpressionContext() *ExpressionContext {
	var p = new(ExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_expression
	return p
}

func InitEmptyExpressionContext(p *ExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_expression
}

func (*ExpressionContext) IsExpressionContext() {}

func NewExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ExpressionContext {
	var p = new(ExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = RdtParserRULE_expression

	return p
}

func (s *ExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *ExpressionContext) Union() IUnionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IUnionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IUnionContext)
}

func (s *ExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case RdtParserVisitor:
		return t.VisitExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *RdtParser) Expression() (localctx IExpressionContext) {
	localctx = NewExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 2, RdtParserRULE_expression)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(17)
		p.Union()
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IUnionContext is an interface to support dynamic dispatch.
type IUnionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllType_() []ITypeContext
	Type_(i int) ITypeContext
	AllPIPE() []antlr.TerminalNode
	PIPE(i int) antlr.TerminalNode
	AllWS() []antlr.TerminalNode
	WS(i int) antlr.TerminalNode

	// IsUnionContext differentiates from other interfaces.
	IsUnionContext()
}

type UnionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyUnionContext() *UnionContext {
	var p = new(UnionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_union
	return p
}

func InitEmptyUnionContext(p *UnionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_union
}

func (*UnionContext) IsUnionContext() {}

func NewUnionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *UnionContext {
	var p = new(UnionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = RdtParserRULE_union

	return p
}

func (s *UnionContext) GetParser() antlr.Parser { return s.parser }

func (s *UnionContext) AllType_() []ITypeContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ITypeContext); ok {
			len++
		}
	}

	tst := make([]ITypeContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ITypeContext); ok {
			tst[i] = t.(ITypeContext)
			i++
		}
	}

	return tst
}

func (s *UnionContext) Type_(i int) ITypeContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ITypeContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ITypeContext)
}

func (s *UnionContext) AllPIPE() []antlr.TerminalNode {
	return s.GetTokens(RdtParserPIPE)
}

func (s *UnionContext) PIPE(i int) antlr.TerminalNode {
	return s.GetToken(RdtParserPIPE, i)
}

func (s *UnionContext) AllWS() []antlr.TerminalNode {
	return s.GetTokens(RdtParserWS)
}

func (s *UnionContext) WS(i int) antlr.TerminalNode {
	return s.GetToken(RdtParserWS, i)
}

func (s *UnionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *UnionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *UnionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case RdtParserVisitor:
		return t.VisitUnion(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *RdtParser) Union() (localctx IUnionContext) {
	localctx = NewUnionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 4, RdtParserRULE_union)
	var _la int

	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(19)
		p.Type_()
	}
	p.SetState(36)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 2, p.GetParserRuleContext())
	if p.HasError() {
		goto errorExit
	}
	for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		if _alt == 1 {
			p.SetState(23)
			p.GetErrorHandler().Sync(p)
			if p.HasError() {
				goto errorExit
			}
			_la = p.GetTokenStream().LA(1)

			for _la == RdtParserWS {
				{
					p.SetState(20)
					p.Match(RdtParserWS)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}

				p.SetState(25)
				p.GetErrorHandler().Sync(p)
				if p.HasError() {
					goto errorExit
				}
				_la = p.GetTokenStream().LA(1)
			}
			{
				p.SetState(26)
				p.Match(RdtParserPIPE)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			p.SetState(30)
			p.GetErrorHandler().Sync(p)
			if p.HasError() {
				goto errorExit
			}
			_la = p.GetTokenStream().LA(1)

			for _la == RdtParserWS {
				{
					p.SetState(27)
					p.Match(RdtParserWS)
					if p.HasError() {
						// Recognition error - abort rule
						goto errorExit
					}
				}

				p.SetState(32)
				p.GetErrorHandler().Sync(p)
				if p.HasError() {
					goto errorExit
				}
				_la = p.GetTokenStream().LA(1)
			}
			{
				p.SetState(33)
				p.Type_()
			}

		}
		p.SetState(38)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 2, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ITypeContext is an interface to support dynamic dispatch.
type ITypeContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	Primitive() IPrimitiveContext
	Group() IGroupContext
	Reference() IReferenceContext
	AllARRAY_NOTATION() []antlr.TerminalNode
	ARRAY_NOTATION(i int) antlr.TerminalNode
	OPTIONAL_NOTATION() antlr.TerminalNode

	// IsTypeContext differentiates from other interfaces.
	IsTypeContext()
}

type TypeContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyTypeContext() *TypeContext {
	var p = new(TypeContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_type
	return p
}

func InitEmptyTypeContext(p *TypeContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_type
}

func (*TypeContext) IsTypeContext() {}

func NewTypeContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *TypeContext {
	var p = new(TypeContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = RdtParserRULE_type

	return p
}

func (s *TypeContext) GetParser() antlr.Parser { return s.parser }

func (s *TypeContext) Primitive() IPrimitiveContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IPrimitiveContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IPrimitiveContext)
}

func (s *TypeContext) Group() IGroupContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IGroupContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IGroupContext)
}

func (s *TypeContext) Reference() IReferenceContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IReferenceContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IReferenceContext)
}

func (s *TypeContext) AllARRAY_NOTATION() []antlr.TerminalNode {
	return s.GetTokens(RdtParserARRAY_NOTATION)
}

func (s *TypeContext) ARRAY_NOTATION(i int) antlr.TerminalNode {
	return s.GetToken(RdtParserARRAY_NOTATION, i)
}

func (s *TypeContext) OPTIONAL_NOTATION() antlr.TerminalNode {
	return s.GetToken(RdtParserOPTIONAL_NOTATION, 0)
}

func (s *TypeContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *TypeContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *TypeContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case RdtParserVisitor:
		return t.VisitType(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *RdtParser) Type_() (localctx ITypeContext) {
	localctx = NewTypeContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 6, RdtParserRULE_type)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(42)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case RdtParserSTRING_TYPE, RdtParserINTEGER_TYPE, RdtParserNUMBER_TYPE, RdtParserBOOLEAN_TYPE, RdtParserDATETIME_TYPE, RdtParserTIME_ONLY_TYPE, RdtParserDATETIME_ONLY_TYPE, RdtParserDATE_ONLY_TYPE, RdtParserFILE_TYPE, RdtParserNIL_TYPE, RdtParserANY_TYPE, RdtParserARRAY_TYPE, RdtParserOBJECT_TYPE, RdtParserUNION_TYPE:
		{
			p.SetState(39)
			p.Primitive()
		}

	case RdtParserLPAREN:
		{
			p.SetState(40)
			p.Group()
		}

	case RdtParserIDENTIFIER:
		{
			p.SetState(41)
			p.Reference()
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}
	p.SetState(47)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == RdtParserARRAY_NOTATION {
		{
			p.SetState(44)
			p.Match(RdtParserARRAY_NOTATION)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

		p.SetState(49)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	p.SetState(51)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == RdtParserOPTIONAL_NOTATION {
		{
			p.SetState(50)
			p.Match(RdtParserOPTIONAL_NOTATION)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IPrimitiveContext is an interface to support dynamic dispatch.
type IPrimitiveContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	STRING_TYPE() antlr.TerminalNode
	INTEGER_TYPE() antlr.TerminalNode
	NUMBER_TYPE() antlr.TerminalNode
	BOOLEAN_TYPE() antlr.TerminalNode
	DATETIME_TYPE() antlr.TerminalNode
	TIME_ONLY_TYPE() antlr.TerminalNode
	DATETIME_ONLY_TYPE() antlr.TerminalNode
	DATE_ONLY_TYPE() antlr.TerminalNode
	FILE_TYPE() antlr.TerminalNode
	NIL_TYPE() antlr.TerminalNode
	ANY_TYPE() antlr.TerminalNode
	ARRAY_TYPE() antlr.TerminalNode
	OBJECT_TYPE() antlr.TerminalNode
	UNION_TYPE() antlr.TerminalNode

	// IsPrimitiveContext differentiates from other interfaces.
	IsPrimitiveContext()
}

type PrimitiveContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyPrimitiveContext() *PrimitiveContext {
	var p = new(PrimitiveContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_primitive
	return p
}

func InitEmptyPrimitiveContext(p *PrimitiveContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_primitive
}

func (*PrimitiveContext) IsPrimitiveContext() {}

func NewPrimitiveContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *PrimitiveContext {
	var p = new(PrimitiveContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = RdtParserRULE_primitive

	return p
}

func (s *PrimitiveContext) GetParser() antlr.Parser { return s.parser }

func (s *PrimitiveContext) STRING_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserSTRING_TYPE, 0)
}

func (s *PrimitiveContext) INTEGER_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserINTEGER_TYPE, 0)
}

func (s *PrimitiveContext) NUMBER_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserNUMBER_TYPE, 0)
}

func (s *PrimitiveContext) BOOLEAN_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserBOOLEAN_TYPE, 0)
}

func (s *PrimitiveContext) DATETIME_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserDATETIME_TYPE, 0)
}

func (s *PrimitiveContext) TIME_ONLY_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserTIME_ONLY_TYPE, 0)
}

func (s *PrimitiveContext) DATETIME_ONLY_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserDATETIME_ONLY_TYPE, 0)
}

func (s *PrimitiveContext) DATE_ONLY_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserDATE_ONLY_TYPE, 0)
}

func (s *PrimitiveContext) FILE_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserFILE_TYPE, 0)
}

func (s *PrimitiveContext) NIL_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserNIL_TYPE, 0)
}

func (s *PrimitiveContext) ANY_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserANY_TYPE, 0)
}

func (s *PrimitiveContext) ARRAY_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserARRAY_TYPE, 0)
}

func (s *PrimitiveContext) OBJECT_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserOBJECT_TYPE, 0)
}

func (s *PrimitiveContext) UNION_TYPE() antlr.TerminalNode {
	return s.GetToken(RdtParserUNION_TYPE, 0)
}

func (s *PrimitiveContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *PrimitiveContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *PrimitiveContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case RdtParserVisitor:
		return t.VisitPrimitive(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *RdtParser) Primitive() (localctx IPrimitiveContext) {
	localctx = NewPrimitiveContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 8, RdtParserRULE_primitive)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(53)
		_la = p.GetTokenStream().LA(1)

		if !((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&2097024) != 0) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IGroupContext is an interface to support dynamic dispatch.
type IGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	LPAREN() antlr.TerminalNode
	Expression() IExpressionContext
	RPAREN() antlr.TerminalNode
	AllWS() []antlr.TerminalNode
	WS(i int) antlr.TerminalNode

	// IsGroupContext differentiates from other interfaces.
	IsGroupContext()
}

type GroupContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyGroupContext() *GroupContext {
	var p = new(GroupContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_group
	return p
}

func InitEmptyGroupContext(p *GroupContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_group
}

func (*GroupContext) IsGroupContext() {}

func NewGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *GroupContext {
	var p = new(GroupContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = RdtParserRULE_group

	return p
}

func (s *GroupContext) GetParser() antlr.Parser { return s.parser }

func (s *GroupContext) LPAREN() antlr.TerminalNode {
	return s.GetToken(RdtParserLPAREN, 0)
}

func (s *GroupContext) Expression() IExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExpressionContext)
}

func (s *GroupContext) RPAREN() antlr.TerminalNode {
	return s.GetToken(RdtParserRPAREN, 0)
}

func (s *GroupContext) AllWS() []antlr.TerminalNode {
	return s.GetTokens(RdtParserWS)
}

func (s *GroupContext) WS(i int) antlr.TerminalNode {
	return s.GetToken(RdtParserWS, i)
}

func (s *GroupContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *GroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *GroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case RdtParserVisitor:
		return t.VisitGroup(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *RdtParser) Group() (localctx IGroupContext) {
	localctx = NewGroupContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 10, RdtParserRULE_group)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(55)
		p.Match(RdtParserLPAREN)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(59)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == RdtParserWS {
		{
			p.SetState(56)
			p.Match(RdtParserWS)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

		p.SetState(61)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	{
		p.SetState(62)
		p.Expression()
	}
	p.SetState(66)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == RdtParserWS {
		{
			p.SetState(63)
			p.Match(RdtParserWS)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

		p.SetState(68)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	{
		p.SetState(69)
		p.Match(RdtParserRPAREN)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IReferenceContext is an interface to support dynamic dispatch.
type IReferenceContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllIDENTIFIER() []antlr.TerminalNode
	IDENTIFIER(i int) antlr.TerminalNode
	DOT() antlr.TerminalNode

	// IsReferenceContext differentiates from other interfaces.
	IsReferenceContext()
}

type ReferenceContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyReferenceContext() *ReferenceContext {
	var p = new(ReferenceContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_reference
	return p
}

func InitEmptyReferenceContext(p *ReferenceContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = RdtParserRULE_reference
}

func (*ReferenceContext) IsReferenceContext() {}

func NewReferenceContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ReferenceContext {
	var p = new(ReferenceContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = RdtParserRULE_reference

	return p
}

func (s *ReferenceContext) GetParser() antlr.Parser { return s.parser }

func (s *ReferenceContext) AllIDENTIFIER() []antlr.TerminalNode {
	return s.GetTokens(RdtParserIDENTIFIER)
}

func (s *ReferenceContext) IDENTIFIER(i int) antlr.TerminalNode {
	return s.GetToken(RdtParserIDENTIFIER, i)
}

func (s *ReferenceContext) DOT() antlr.TerminalNode {
	return s.GetToken(RdtParserDOT, 0)
}

func (s *ReferenceContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ReferenceContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ReferenceContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case RdtParserVisitor:
		return t.VisitReference(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *RdtParser) Reference() (localctx IReferenceContext) {
	localctx = NewReferenceContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 12, RdtParserRULE_reference)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(71)
		p.Match(RdtParserIDENTIFIER)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(74)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == RdtParserDOT {
		{
			p.SetState(72)
			p.Match(RdtParserDOT)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(73)
			p.Match(RdtParserIDENTIFIER)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}
