package raml

import (
	"fmt"
	"strings"

	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"
)

type EndPoint struct {
	ID int64

	URI           string
	FullURI       string
	URIParameters *orderedmap.OrderedMap[string, *BaseShape]

	DisplayName *ScalarFacet[string]
	Description *ScalarFacet[string]

	ResourceType *ResourceType
	Traits       []*Trait
	// RTTraits holds traits contributed by the applied resource type (endpoint-level).
	// Kept separate from Traits to preserve spec-defined application order:
	// method → resource → RT-method → RT-resource.
	RTTraits []*Trait
	// TODO: Maybe introduce "Security" with resolved security scheme?
	SecuredBy []*SecurityScheme
	// explicitSecuredBy is true when the endpoint's own RAML source explicitly
	// declared a securedBy facet.  Used in applySecuritySchemes to replace
	// (rather than append to) the inherited API-level global security on
	// child operations that have no explicit securedBy of their own.
	explicitSecuredBy bool

	EndPoints  *orderedmap.OrderedMap[string, *EndPoint]
	Operations *orderedmap.OrderedMap[string, *Operation]

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}

func (e *EndPoint) unmarshalURIParameters(node *yaml.Node) error {
	if node.Tag == TagNull {
		return nil
	} else if node.Kind != yaml.MappingNode {
		return StacktraceNew("uriParameters must be a mapping node", e.Location, WithNodePosition(node))
	}

	e.URIParameters = orderedmap.New[string, *BaseShape](len(node.Content) / 2)
	for j := 0; j != len(node.Content); j += 2 {
		keyNode := node.Content[j]
		valueNode := node.Content[j+1]

		shape, err := e.raml.makeNewShapeYAML(keyNode, valueNode, e.Location)
		if err != nil {
			return StacktraceNewWrapped("make new shape yaml", err, e.Location, WithNodePosition(keyNode))
		}
		e.URIParameters.Set(keyNode.Value, shape)
		e.raml.PutTypeDefinitionIntoFragment(e.Location, shape)
	}
	return nil
}

// validateAndSynthesizeURIParameters is the shared core used by both
// EndPoint.validateURIParameters and APIFragment.validateBaseURIParameters.
//
// Pass 1: parse uri, build a lookup set, and synthesize a minimal string
// BaseShape for every template variable that lacks an explicit declaration.
// Pass 2: cross-check every declared parameter against the set and validate
// its constraint values — both in one iteration.
//
// uriPos is the source position of the URI template string itself (the
// endpoint key, the baseUri facet value, …); it is attached to "parse uri
// template" failures so the error points at the malformed template rather
// than at the file root.
func validateAndSynthesizeURIParameters(uri string, params *orderedmap.OrderedMap[string, *BaseShape], location string, uriPos stacktrace.Position) (*orderedmap.OrderedMap[string, *BaseShape], error) {
	uriExprs, st := extractURITemplateParamNames(uri, location, uriPos)
	if st != nil {
		return nil, st
	}
	l := len(uriExprs)
	uriParamSet := make(map[string]struct{}, l)
	if l > 0 && params == nil {
		params = orderedmap.New[string, *BaseShape](l)
	}
	for _, expr := range uriExprs {
		uriParamSet[expr.Name] = struct{}{}
		if _, ok := params.Get(expr.Name); !ok {
			// Synthesize an implicit string parameter for every {name} placeholder
			// that has no explicit uriParameters entry. The StringShape must hold a
			// back-pointer to the BaseShape, and the BaseShape must be marked
			// unwrapped so that consumers (e.g. the OAS3 converter) can process it
			// without requiring a full unwrap pass.
			base := &BaseShape{
				Name:     expr.Name,
				Type:     "string",
				Location: location,
			}
			base.Shape = &StringShape{BaseShape: base}
			base.SetUnwrapped()
			params.Set(expr.Name, base)
		}
	}
	for pair := params.Oldest(); pair != nil; pair = pair.Next() {
		if _, ok := uriParamSet[pair.Key]; !ok {
			return nil, StacktraceNew("uri parameter is not used", location,
				stacktrace.WithPosition(&pair.Value.KeyPos), stacktrace.WithInfo("parameter", pair.Key))
		}
		if err := validateURIParamNoSlash(pair.Value, location); err != nil {
			return nil, StacktraceNewWrapped("uri parameter value contains slash", err, location,
				stacktrace.WithPosition(&pair.Value.KeyPos), stacktrace.WithInfo("parameter", pair.Key))
		}
	}
	return params, nil
}

func (e *EndPoint) validateURIParameters() error {
	params, err := validateAndSynthesizeURIParameters(e.URI, e.URIParameters, e.Location, e.KeyPos)
	if err != nil {
		return err
	}
	e.URIParameters = params
	return nil
}

// uriTemplateExpression holds a single parsed RFC 6570 template variable expression.
// Operator is the expression operator: empty string for simple expansion (Level 1),
// "+" for reserved expansion, or "#" for fragment expansion (both Level 2).
// Name is the variable name.
type uriTemplateExpression struct {
	Operator string
	Name     string
}

// extractURITemplateParamNames parses a URI string and returns the variable
// expressions for all {expression} template placeholders.
// It handles RFC 6570 Levels 1 and 2: simple expansion ({var}), reserved
// expansion ({+var}), and fragment expansion ({#var}).
// It returns a *stacktrace.StackTrace for malformed templates: unclosed '{',
// nested '{', unexpected '}', empty expressions, or variable names containing
// characters outside the RFC 6570 varname production (ALPHA / DIGIT / "_" /
// "." / pct-encoded). Errors are positioned at the exact offending byte by
// shifting uriPos.Column by the byte offset of the offender within uri.
func extractURITemplateParamNames(uri string, location string, uriPos stacktrace.Position) ([]uriTemplateExpression, *stacktrace.StackTrace) {
	var exprs []uriTemplateExpression
	for i := 0; i < len(uri); i++ {
		switch uri[i] {
		case '{':
			j := i + 1
			for j < len(uri) && uri[j] != '{' && uri[j] != '}' {
				j++
			}
			if j == len(uri) {
				return nil, uriTemplateStacktrace("unclosed '{'", location, uriPos, i)
			}
			if uri[j] == '{' {
				return nil, uriTemplateStacktrace("nested '{'", location, uriPos, j)
			}
			content := uri[i+1 : j]
			// Detect RFC 6570 Level 2 operator ('+' or '#').
			op := ""
			opLen := 0
			if len(content) > 0 && (content[0] == '+' || content[0] == '#') {
				op = string(content[0])
				content = content[1:]
				opLen = 1
			}
			if content == "" {
				return nil, uriTemplateStacktrace("empty expression", location, uriPos, i)
			}
			if st := validateURIVarName(content, location, uriPos, i+1+opLen); st != nil {
				return nil, st
			}
			exprs = append(exprs, uriTemplateExpression{Operator: op, Name: content})
			i = j
		case '}':
			return nil, uriTemplateStacktrace("unexpected '}'", location, uriPos, i)
		}
	}
	return exprs, nil
}

// uriTemplateStacktrace builds a StackTrace anchored at uriPos shifted by
// offset bytes along the URI string. URIs in RAML are single-line scalars,
// so shifting Column is sufficient to land on the offender; EndColumn marks
// the next character so LSP renders a single-char squiggle.
func uriTemplateStacktrace(msg, location string, uriPos stacktrace.Position, offset int) *stacktrace.StackTrace {
	pos := uriPos
	pos.Column += offset
	pos.EndLine = pos.Line
	pos.EndColumn = pos.Column + 1
	return StacktraceNew(msg, location, stacktrace.WithPosition(&pos))
}

// validateURIVarName checks that name conforms to the RFC 6570 varname production:
//
//	varname = varchar *( "." 1*varchar )
//	varchar = ALPHA / DIGIT / "_" / pct-encoded
//
// nameStart is the byte offset of name's first character within the
// enclosing URI; used to position diagnostics at the exact offending byte.
func validateURIVarName(name, location string, uriPos stacktrace.Position, nameStart int) *stacktrace.StackTrace {
	i := 0
	for i < len(name) {
		c := name[i]
		switch {
		case c == '%':
			// pct-encoded: must be followed by exactly two hex digits.
			if i+2 >= len(name) || !isHexDigit(name[i+1]) || !isHexDigit(name[i+2]) {
				return uriTemplateStacktrace("invalid pct-encoded sequence in variable name",
					location, uriPos, nameStart+i)
			}
			i += 3
		case c == '.':
			// Dot separator is allowed between varchar runs but not at start/end or doubled.
			if i == 0 || i == len(name)-1 || name[i-1] == '.' {
				return uriTemplateStacktrace("invalid '.' in variable name", location, uriPos, nameStart+i)
			}
			i++
		case isVarChar(c):
			i++
		default:
			return uriTemplateStacktrace(fmt.Sprintf("invalid character %q in variable name", c),
				location, uriPos, nameStart+i)
		}
	}
	return nil
}

func isVarChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
}

// validateURIParamNoSlash checks that a URI parameter's constraint values
// (default, enum, example, examples) do not contain a "/" character, which
// is a reserved delimiter in URI paths.
func validateURIParamNoSlash(shape *BaseShape, location string) error {
	containsSlash := func(nv *ValueNode) bool {
		if nv == nil || nv.Scalar == nil {
			return false
		}
		s, ok := nv.Scalar.Value.(string)
		return ok && strings.Contains(s, "/")
	}
	if shape.Default != nil && containsSlash(shape.Default.Value) {
		return StacktraceNew("default value must not contain '/'", location,
			stacktrace.WithPosition(&shape.Default.ValuePos))
	}
	for _, e := range shape.Enum {
		if containsSlash(e.Value) {
			return StacktraceNew("enum value must not contain '/'", location,
				stacktrace.WithPosition(&e.ValuePos))
		}
	}
	if shape.Example != nil && shape.Example.Data != nil && containsSlash(shape.Example.Data.Value) {
		return StacktraceNew("example value must not contain '/'", location,
			stacktrace.WithPosition(&shape.Example.Data.ValuePos))
	}
	if shape.Examples != nil {
		for pair := shape.Examples.Map.Oldest(); pair != nil; pair = pair.Next() {
			ex := pair.Value
			if ex.Data != nil && containsSlash(ex.Data.Value) {
				return StacktraceNew("example value must not contain '/'", location,
					stacktrace.WithPosition(&ex.Data.ValuePos))
			}
		}
	}
	return nil
}

func IsEndPoint(name string) bool {
	return name != "" && name[0] == '/'
}
