package raml

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/acronis/go-stacktrace"
)

const (
	RFC2616 = "Mon, 02 Jan 2006 15:04:05 GMT"
	// NOTE: time.DateTime uses "2006-01-02 15:04:05" format which is different from date-time defined in RAML spec.
	DateTime = "2006-01-02T15:04:05"
)

func (r *RAML) MakeEnum(v *yaml.Node, location string) (Nodes, error) {
	if v.Kind == yaml.AliasNode {
		v = v.Alias
	}
	if v.Kind != yaml.SequenceNode {
		return nil, StacktraceNew("enum must be sequence node", location, WithNodePosition(v))
	}
	enums := make(Nodes, len(v.Content))
	for i, v := range v.Content {
		n, err := r.makeRootNode(nil, v, location)
		if err != nil {
			return nil, StacktraceNewWrapped("make node enum", err, location, WithNodePosition(v))
		}
		enums[i] = n
	}
	return enums, nil
}

func isCompatibleFileTypes(source, target []*Node[string]) bool {
	for _, v := range target {
		found := false
		for _, e := range source {
			if v.Value == e.Value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func isCompatibleEnum(source Nodes, target Nodes) bool {
	// Target enum must be a subset of source enum.
	for _, v := range target {
		found := false
		for _, e := range source {
			if semanticEqual(e.Value.Raw, v.Value.Raw) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// validateEnum checks whether v matches one of s.Enum's allowed values.
// The caller must ensure s.Enum is non-nil.
func (s *BaseShape) validateEnum(v any) error {
	for _, e := range s.Enum {
		if semanticEqual(e.Value.Raw, v) {
			return nil
		}
	}
	return fmt.Errorf("value must be one of (%s)", s.Enum.String())
}

type FormatFacets struct {
	Format *ScalarFacet[string]
}

type IntegerFacets struct {
	Minimum    *ScalarFacet[*big.Int]
	Maximum    *ScalarFacet[*big.Int]
	MultipleOf *ScalarFacet[*big.Rat]
}

type scalarShape struct{}

func (scalarShape) IsScalar() bool {
	return true
}

type IntegerShape struct {
	scalarShape
	*BaseShape

	FormatFacets
	IntegerFacets
}

func (s *IntegerShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *IntegerShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *IntegerShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *IntegerShape) alias(source Shape) (Shape, error) {
	ss, err := checkAliasType[*IntegerShape](s, source)
	if err != nil {
		return nil, err
	}
	s.Minimum = ss.Minimum
	s.Maximum = ss.Maximum
	s.MultipleOf = ss.MultipleOf
	s.Format = ss.Format
	return s, nil
}

func (s *IntegerShape) validate(v any, _ string) error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	// Normalize the input to *big.Int so all constraint checks are uniform.
	// For json.Number, the Int64 fast path avoids a big.Rat allocation
	// in the common case; it falls back to big.Rat parsing for large or
	// decimal-looking values (e.g. "1.0").
	var val *big.Int
	switch v := v.(type) {
	case int:
		val = big.NewInt(int64(v))
	case int8:
		val = big.NewInt(int64(v))
	case int16:
		val = big.NewInt(int64(v))
	case int32:
		val = big.NewInt(int64(v))
	case int64:
		val = big.NewInt(v)
	case uint:
		val = new(big.Int).SetUint64(uint64(v))
	case uint8:
		val = big.NewInt(int64(v))
	case uint16:
		val = big.NewInt(int64(v))
	case uint32:
		val = big.NewInt(int64(v))
	case uint64:
		val = new(big.Int).SetUint64(v)
	// json/yaml unmarshal plain numbers as float64
	case float64:
		val = big.NewInt(int64(v))
	case *big.Int:
		val = v
	case json.Number:
		if i, err := v.Int64(); err == nil {
			val = big.NewInt(i)
		} else {
			r, ok := new(big.Rat).SetString(string(v))
			if !ok || !r.IsInt() {
				return fmt.Errorf("invalid type, got non-integer json.Number %q", string(v))
			}
			val = r.Num()
		}
	default:
		return fmt.Errorf("invalid type, got %T, expected a numeric type or json.Number", v)
	}

	if s.Minimum != nil && val.Cmp(s.Minimum.Value) < 0 {
		return fmt.Errorf("value must be greater than %s", s.Minimum.Value.String())
	}
	if s.Maximum != nil && val.Cmp(s.Maximum.Value) > 0 {
		return fmt.Errorf("value must be less than %s", s.Maximum.Value.String())
	}
	if s.MultipleOf != nil {
		if !new(big.Rat).Quo(new(big.Rat).SetInt(val), s.MultipleOf.Value).IsInt() {
			return fmt.Errorf("value must be a multiple of %s", s.MultipleOf.Value.RatString())
		}
	}
	if s.Format != nil {
		// SetOfIntegerFormats encodes bit-width as 0=int8, 1=int16, 2=int32, 3=int64.
		// Valid range is [-limit, limit) where limit = 1 << (bits-1).
		size := SetOfIntegerFormats[s.Format.Value]
		if val.IsInt64() {
			// Fits in int64: pure arithmetic, no big.Int allocations.
			// size 3 (int64 / long) covers the full int64 range, so no check needed.
			if size < 3 {
				i := val.Int64()
				limit := int64(1) << (uint(8)<<uint(size) - 1)
				if i < -limit || i >= limit {
					return fmt.Errorf("value %d is out of range for format %s", i, s.Format.Value)
				}
			}
		} else {
			// Exceeds int64: use big.Int range check.
			// The Lsh formula works for size 3 too: 1<<63 is math.MaxInt64+1.
			limit := new(big.Int).Lsh(big.NewInt(1), (uint(8)<<uint(size))-1)
			if val.Cmp(new(big.Int).Neg(limit)) < 0 || val.Cmp(limit) >= 0 {
				return fmt.Errorf("value %s is out of range for format %s", val.String(), s.Format.Value)
			}
		}
	}

	return nil
}

func (s *IntegerShape) inherit(source Shape) (Shape, error) {
	ss, err := checkInheritType[*IntegerShape](s, source)
	if err != nil {
		return nil, err
	}
	if s.Minimum == nil {
		s.Minimum = ss.Minimum
	} else if ss.Minimum != nil && s.Minimum.Value.Cmp(ss.Minimum.Value) < 0 {
		return nil, StacktraceNew("minimum constraint violation", s.Location,
			stacktrace.WithPosition(&s.Minimum.ValuePos),
			stacktrace.WithInfo("source", ss.Minimum.Value),
			stacktrace.WithInfo("target", s.Minimum.Value))
	}
	if s.Maximum == nil {
		s.Maximum = ss.Maximum
	} else if ss.Maximum != nil && s.Maximum.Value.Cmp(ss.Maximum.Value) > 0 {
		return nil, StacktraceNew("maximum constraint violation", s.Location,
			stacktrace.WithPosition(&s.Maximum.ValuePos),
			stacktrace.WithInfo("source", ss.Maximum.Value),
			stacktrace.WithInfo("target", s.Maximum.Value))
	}
	if s.MultipleOf == nil {
		s.MultipleOf = ss.MultipleOf
	} else if ss.MultipleOf != nil {
		// Child's multipleOf must itself be a multiple of the parent's so that
		// every value allowed by the child is also allowed by the parent.
		quotient := new(big.Rat).Quo(s.MultipleOf.Value, ss.MultipleOf.Value)
		if !quotient.IsInt() {
			return nil, StacktraceNew("multipleOf constraint violation", s.Location,
				stacktrace.WithPosition(&s.MultipleOf.ValuePos),
				stacktrace.WithInfo("source", ss.MultipleOf.Value),
				stacktrace.WithInfo("target", s.MultipleOf.Value))
		}
	}
	if s.Format == nil {
		s.Format = ss.Format
	} else if ss.Format != nil && SetOfIntegerFormats[s.Format.Value] != SetOfIntegerFormats[ss.Format.Value] {
		return nil, StacktraceNew("format constraint violation", s.Location,
			stacktrace.WithPosition(&s.Format.ValuePos),
			stacktrace.WithInfo("source", ss.Format.Value),
			stacktrace.WithInfo("target", s.Format.Value))
	}
	return s, nil
}

func (s *IntegerShape) check() error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	if s.Minimum != nil && s.Maximum != nil && s.Minimum.Value.Cmp(s.Maximum.Value) > 0 {
		return StacktraceNew("minimum must be less than or equal to maximum", s.Location,
			stacktrace.WithPosition(&s.KeyPos))
	}
	if s.Format != nil {
		if _, ok := SetOfIntegerFormats[s.Format.Value]; !ok {
			return StacktraceNew("invalid format", s.Location, stacktrace.WithPosition(&s.Format.ValuePos))
		}
	}
	return nil
}

func (s *IntegerShape) String() string {
	var facets []string
	if s.Minimum != nil {
		facets = append(facets, fmt.Sprintf("minimum:%s", s.Minimum.Value))
	}
	if s.Maximum != nil {
		facets = append(facets, fmt.Sprintf("maximum:%s", s.Maximum.Value))
	}
	if s.MultipleOf != nil {
		facets = append(facets, fmt.Sprintf("multipleOf:%s", s.MultipleOf.Value.RatString()))
	}
	if s.Format != nil && s.Format.Value != "" {
		facets = append(facets, fmt.Sprintf("format:%s", s.Format.Value))
	}
	return fmt.Sprintf("IntegerShape{facets:[%s]}", strings.Join(facets, ","))
}

func (s *IntegerShape) unmarshalYAMLNode(node, valueNode *yaml.Node) error {
	switch node.Value {
	case FacetMinimum:
		fragmentPath, rn, err := s.raml.resolveInclude(valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(valueNode))
		}
		if rn.Tag != TagInt {
			return StacktraceNew("minimum must be integer", s.Location, WithNodePosition(valueNode))
		}
		rn, exts, err := s.raml.resolveAnnotatedScalar(rn, s.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve value node", err, s.Location, WithNodePosition(valueNode))
		}
		num, ok := big.NewInt(0).SetString(rn.Value, 10)
		if !ok {
			return StacktraceNew("invalid minimum value", s.Location, WithNodePosition(valueNode))
		}
		s.Minimum = MakeScalarFacet(num, node, valueNode, s.Location, fragmentPath, exts)
	case FacetMaximum:
		fragmentPath, rn, err := s.raml.resolveInclude(valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(valueNode))
		}
		if rn.Tag != TagInt {
			return StacktraceNew("maximum must be integer", s.Location, WithNodePosition(valueNode))
		}
		rn, exts, err := s.raml.resolveAnnotatedScalar(rn, s.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve value node", err, s.Location, WithNodePosition(valueNode))
		}
		num, ok := big.NewInt(0).SetString(rn.Value, 10)
		if !ok {
			return StacktraceNew("invalid maximum value", s.Location, WithNodePosition(valueNode))
		}
		s.Maximum = MakeScalarFacet(num, node, valueNode, s.Location, fragmentPath, exts)
	case FacetMultipleOf:
		fragmentPath, rn, err := s.raml.resolveInclude(valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(valueNode))
		}
		rn, exts, err := s.raml.resolveAnnotatedScalar(rn, s.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve value node", err, s.Location, WithNodePosition(valueNode))
		}
		num, ok := new(big.Rat).SetString(rn.Value)
		if !ok {
			return StacktraceNew("invalid multipleOf value", s.Location, WithNodePosition(valueNode))
		}
		if num.Sign() == 0 {
			return StacktraceNew("multipleOf must not be zero", s.Location, WithNodePosition(valueNode))
		}
		s.MultipleOf = MakeScalarFacet(num, node, valueNode, s.Location, fragmentPath, exts)
	case FacetFormat:
		sn, err := MakeScalarFacetYAML[string](s.raml, node, valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("make scalar node", err, s.Location, WithNodePosition(valueNode))
		}
		s.Format = sn
	default:
		n, err := s.raml.makeRootNode(node, valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
		}
		s.CustomShapeFacets.Set(node.Value, n)
	}
	return nil
}

func (s *IntegerShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]
		if err := s.unmarshalYAMLNode(node, valueNode); err != nil {
			return StacktraceNewWrapped("unmarshal yaml node", err, s.Location, WithNodePosition(node))
		}
	}
	return nil
}

type NumberFacets struct {
	// Minimum and maximum are unset since there's no theoretical minimum and maximum for numbers by default
	Minimum    *ScalarFacet[*big.Rat]
	Maximum    *ScalarFacet[*big.Rat]
	MultipleOf *ScalarFacet[*big.Rat]
}

type NumberShape struct {
	scalarShape
	*BaseShape

	FormatFacets
	NumberFacets
}

func (s *NumberShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *NumberShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *NumberShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *NumberShape) alias(source Shape) (Shape, error) {
	ss, err := checkAliasType[*NumberShape](s, source)
	if err != nil {
		return nil, err
	}
	s.Minimum = ss.Minimum
	s.Maximum = ss.Maximum
	s.MultipleOf = ss.MultipleOf
	s.Format = ss.Format
	return s, nil
}

func (s *NumberShape) validate(v any, _ string) error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	var numVal *big.Rat
	switch n := v.(type) {
	// go-yaml unmarshals integers as int; json/encoding produces float64 or json.Number.
	// All standard Go integer and float types, plus *big.Int and *big.Rat, are accepted
	// so callers do not need to cast before validating.
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		*big.Int,
		json.Number:
	case *big.Rat:
		numVal = n
	default:
		return fmt.Errorf("invalid type, got %T, expected a numeric type or json.Number", v)
	}

	num := func() *big.Rat {
		if numVal == nil {
			numVal, _ = new(big.Rat).SetString(fmt.Sprintf("%v", v))
		}
		return numVal
	}
	if s.Minimum != nil && num().Cmp(s.Minimum.Value) < 0 {
		return fmt.Errorf("value must be greater than or equal to %s", s.Minimum.Value.RatString())
	}
	if s.Maximum != nil && num().Cmp(s.Maximum.Value) > 0 {
		return fmt.Errorf("value must be less than or equal to %s", s.Maximum.Value.RatString())
	}
	if s.MultipleOf != nil {
		quotient := new(big.Rat).Quo(num(), s.MultipleOf.Value)
		if !quotient.IsInt() {
			return fmt.Errorf("value must be a multiple of %s", s.MultipleOf.Value.RatString())
		}
	}
	if s.Format != nil {
		fv, _ := num().Float64()
		switch s.Format.Value {
		case "float":
			if fv > math.MaxFloat32 || fv < -math.MaxFloat32 {
				return fmt.Errorf("value %v is out of range for format float", fv)
			}
			// double: all float64 values are valid
		}
	}

	return nil
}

func (s *NumberShape) inherit(source Shape) (Shape, error) {
	ss, err := checkInheritType[*NumberShape](s, source)
	if err != nil {
		return nil, err
	}
	if s.Minimum == nil {
		s.Minimum = ss.Minimum
	} else if ss.Minimum != nil && s.Minimum.Value.Cmp(ss.Minimum.Value) < 0 {
		return nil, StacktraceNew("minimum constraint violation", s.Location,
			stacktrace.WithPosition(&s.Minimum.ValuePos),
			stacktrace.WithInfo("source", ss.Minimum.Value),
			stacktrace.WithInfo("target", s.Minimum.Value))
	}
	if s.Maximum == nil {
		s.Maximum = ss.Maximum
	} else if ss.Maximum != nil && s.Maximum.Value.Cmp(ss.Maximum.Value) > 0 {
		return nil, StacktraceNew("maximum constraint violation", s.Location,
			stacktrace.WithPosition(&s.Maximum.ValuePos),
			stacktrace.WithInfo("source", ss.Maximum.Value),
			stacktrace.WithInfo("target", s.Maximum.Value))
	}
	if s.MultipleOf == nil {
		s.MultipleOf = ss.MultipleOf
	} else if ss.MultipleOf != nil {
		// Child's multipleOf must be a multiple of the parent's.
		quotient := new(big.Rat).Quo(s.MultipleOf.Value, ss.MultipleOf.Value)
		if !quotient.IsInt() {
			return nil, StacktraceNew("multipleOf constraint violation", s.Location,
				stacktrace.WithPosition(&s.MultipleOf.ValuePos),
				stacktrace.WithInfo("source", ss.MultipleOf.Value),
				stacktrace.WithInfo("target", s.MultipleOf.Value))
		}
	}
	if s.Format == nil {
		s.Format = ss.Format
	} else if ss.Format != nil && s.Format.Value != ss.Format.Value {
		return nil, StacktraceNew("format constraint violation", s.Location,
			stacktrace.WithPosition(&s.Format.ValuePos),
			stacktrace.WithInfo("source", ss.Format.Value),
			stacktrace.WithInfo("target", s.Format.Value))
	}
	return s, nil
}

func (s *NumberShape) check() error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	if s.Minimum != nil && s.Maximum != nil && s.Minimum.Value.Cmp(s.Maximum.Value) > 0 {
		return StacktraceNew("minimum must be less than or equal to maximum", s.Location,
			stacktrace.WithPosition(&s.KeyPos))
	}
	if s.Format != nil {
		if _, ok := SetOfNumberFormats[s.Format.Value]; !ok {
			return StacktraceNew("invalid format", s.Location, stacktrace.WithPosition(&s.Format.ValuePos))
		}
	}
	return nil
}

func (s *NumberShape) String() string {
	var facets []string
	if s.Minimum != nil {
		facets = append(facets, fmt.Sprintf("minimum:%v", s.Minimum.Value))
	}
	if s.Maximum != nil {
		facets = append(facets, fmt.Sprintf("maximum:%v", s.Maximum.Value))
	}
	if s.MultipleOf != nil {
		facets = append(facets, fmt.Sprintf("multipleOf:%v", s.MultipleOf.Value))
	}
	if s.Format != nil && s.Format.Value != "" {
		facets = append(facets, fmt.Sprintf("format:%s", s.Format.Value))
	}
	return fmt.Sprintf("NumberShape{facets:[%s]}", strings.Join(facets, ","))
}

func (s *NumberShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]
		switch node.Value {
		case FacetMinimum:
			fragmentPath, rn, err := s.raml.resolveInclude(valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("minimum", err, s.Location, WithNodePosition(valueNode))
			}
			rn, exts, err := s.raml.resolveAnnotatedScalar(rn, s.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve value node", err, s.Location, WithNodePosition(valueNode))
			}
			num, ok := new(big.Rat).SetString(rn.Value)
			if !ok {
				return StacktraceNew("invalid minimum value", s.Location, WithNodePosition(valueNode))
			}
			s.Minimum = MakeScalarFacet(num, node, valueNode, s.Location, fragmentPath, exts)
		case FacetMaximum:
			fragmentPath, rn, err := s.raml.resolveInclude(valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("maximum", err, s.Location, WithNodePosition(valueNode))
			}
			rn, exts, err := s.raml.resolveAnnotatedScalar(rn, s.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve value node", err, s.Location, WithNodePosition(valueNode))
			}
			num, ok := new(big.Rat).SetString(rn.Value)
			if !ok {
				return StacktraceNew("invalid maximum value", s.Location, WithNodePosition(valueNode))
			}
			s.Maximum = MakeScalarFacet(num, node, valueNode, s.Location, fragmentPath, exts)
		case FacetFormat:
			sn, err := MakeScalarFacetYAML[string](s.raml, node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, s.Location, WithNodePosition(valueNode))
			}
			s.Format = sn
		case FacetMultipleOf:
			fragmentPath, rn, err := s.raml.resolveInclude(valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("multipleOf", err, s.Location, WithNodePosition(valueNode))
			}
			rn, exts, err := s.raml.resolveAnnotatedScalar(rn, s.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve value node", err, s.Location, WithNodePosition(valueNode))
			}
			num, ok := new(big.Rat).SetString(rn.Value)
			if !ok {
				return StacktraceNew("invalid multipleOf value", s.Location, WithNodePosition(valueNode))
			}
			if num.Sign() == 0 {
				return StacktraceNew("multipleOf must not be zero", s.Location, WithNodePosition(valueNode))
			}
			s.MultipleOf = MakeScalarFacet(num, node, valueNode, s.Location, fragmentPath, exts)
		default:
			n, err := s.raml.makeRootNode(node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
			}
			s.CustomShapeFacets.Set(node.Value, n)
		}
	}
	return nil
}

type LengthFacets struct {
	MaxLength *ScalarFacet[uint64]
	MinLength *ScalarFacet[uint64]
}

type StringFacets struct {
	LengthFacets
	Pattern *ScalarFacet[*regexp.Regexp]
}

type StringShape struct {
	scalarShape
	*BaseShape

	StringFacets
}

func (s *StringShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *StringShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *StringShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *StringShape) alias(source Shape) (Shape, error) {
	ss, err := checkAliasType[*StringShape](s, source)
	if err != nil {
		return nil, err
	}
	s.MinLength = ss.MinLength
	s.MaxLength = ss.MaxLength
	s.Pattern = ss.Pattern
	return s, nil
}

func (s *StringShape) validate(v any, _ string) error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	i, ok := v.(string)
	if !ok {
		return fmt.Errorf("invalid type, got %T, expected string", v)
	}

	strLen := uint64(len(i))
	if s.MinLength != nil && strLen < s.MinLength.Value {
		return fmt.Errorf("length must be greater than %d", s.MinLength.Value)
	}
	if s.MaxLength != nil && strLen > s.MaxLength.Value {
		return fmt.Errorf("length must be less than %d", s.MaxLength.Value)
	}
	if s.Pattern != nil && !s.Pattern.Value.MatchString(i) {
		return fmt.Errorf("must match pattern %s", s.Pattern.Value.String())
	}

	return nil
}

func (s *StringShape) inherit(source Shape) (Shape, error) {
	ss, err := checkInheritType[*StringShape](s, source)
	if err != nil {
		return nil, err
	}
	if s.MinLength == nil {
		s.MinLength = ss.MinLength
	} else if ss.MinLength != nil && s.MinLength.Value < ss.MinLength.Value {
		return nil, StacktraceNew("minLength constraint violation", s.Location,
			stacktrace.WithPosition(&s.MinLength.ValuePos),
			stacktrace.WithInfo("source", ss.MinLength.Value),
			stacktrace.WithInfo("target", s.MinLength.Value))
	}
	if s.MaxLength == nil {
		s.MaxLength = ss.MaxLength
	} else if ss.MaxLength != nil && s.MaxLength.Value > ss.MaxLength.Value {
		return nil, StacktraceNew("maxLength constraint violation", s.Location,
			stacktrace.WithPosition(&s.MaxLength.ValuePos),
			stacktrace.WithInfo("source", ss.MaxLength.Value),
			stacktrace.WithInfo("target", s.MaxLength.Value))
	}
	// FIXME: Patterns are merged unconditionally, but ideally they should be validated against intersection of their DFAs
	if s.Pattern == nil {
		s.Pattern = ss.Pattern
	}
	return s, nil
}

func (s *StringShape) check() error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	if s.MinLength != nil && s.MaxLength != nil && s.MinLength.Value > s.MaxLength.Value {
		return StacktraceNew("minLength must be less than or equal to maxLength",
			s.Location, stacktrace.WithPosition(&s.MinLength.ValuePos))
	}
	return nil
}

func (s *StringShape) String() string {
	var facets []string
	if s.MinLength != nil {
		facets = append(facets, fmt.Sprintf("minLength:%d", s.MinLength.Value))
	}
	if s.MaxLength != nil {
		facets = append(facets, fmt.Sprintf("maxLength:%d", s.MaxLength.Value))
	}
	if s.Pattern != nil {
		facets = append(facets, fmt.Sprintf("pattern:%s", s.Pattern.Value))
	}
	return fmt.Sprintf("StringShape{facets:[%s]}", strings.Join(facets, ","))
}

func (s *StringShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]
		switch node.Value {
		case FacetMinLength:
			sn, err := MakeScalarFacetYAML[uint64](s.raml, node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("minLength", err, s.Location, WithNodePosition(valueNode))
			}
			s.MinLength = sn
		case FacetMaxLength:
			sn, err := MakeScalarFacetYAML[uint64](s.raml, node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("maxLength", err, s.Location, WithNodePosition(valueNode))
			}
			s.MaxLength = sn
		case FacetPattern:
			fragmentPath, rn, err := s.raml.resolveInclude(valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve include", err, s.Location, WithNodePosition(valueNode))
			}
			rn, exts, err := s.raml.resolveAnnotatedScalar(rn, s.Location)
			if err != nil {
				return StacktraceNewWrapped("resolve value node", err, s.Location, WithNodePosition(valueNode))
			}
			if rn.Tag != TagStr {
				return StacktraceNew("pattern must be string", s.Location, WithNodePosition(valueNode))
			}
			re, err := regexp.Compile(rn.Value)
			if err != nil {
				return StacktraceNewWrapped("decode pattern", err, s.Location, WithNodePosition(valueNode))
			}
			s.Pattern = MakeScalarFacet(re, node, valueNode, s.Location, fragmentPath, exts)
		default:
			n, err := s.raml.makeRootNode(node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
			}
			s.CustomShapeFacets.Set(node.Value, n)
		}
	}
	return nil
}

type FileFacets struct {
	FileTypes []*Node[string]
}

type FileShape struct {
	scalarShape
	*BaseShape

	LengthFacets
	FileFacets
}

func (s *FileShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *FileShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *FileShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *FileShape) alias(source Shape) (Shape, error) {
	ss, err := checkAliasType[*FileShape](s, source)
	if err != nil {
		return nil, err
	}
	s.MinLength = ss.MinLength
	s.MaxLength = ss.MaxLength
	s.FileTypes = ss.FileTypes
	return s, nil
}

func (s *FileShape) validate(v any, _ string) error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	i, ok := v.(string)
	if !ok {
		return fmt.Errorf("invalid type, got %T, expected string", v)
	}

	// TODO: What is compared, byte size or base64 string size?
	strLen := uint64(len(i))
	if s.MinLength != nil && strLen < s.MinLength.Value {
		return fmt.Errorf("length must be greater than %d", s.MinLength.Value)
	}
	if s.MaxLength != nil && strLen > s.MaxLength.Value {
		return fmt.Errorf("length must be less than %d", s.MaxLength.Value)
	}
	// TODO: Validation against file types

	return nil
}

func (s *FileShape) inherit(source Shape) (Shape, error) {
	ss, err := checkInheritType[*FileShape](s, source)
	if err != nil {
		return nil, err
	}
	if s.MinLength == nil {
		s.MinLength = ss.MinLength
	} else if ss.MinLength != nil && s.MinLength.Value < ss.MinLength.Value {
		return nil, StacktraceNew("minLength constraint violation", s.Location,
			stacktrace.WithPosition(&s.MinLength.ValuePos),
			stacktrace.WithInfo("source", ss.MinLength.Value),
			stacktrace.WithInfo("target", s.MinLength.Value))
	}
	if s.MaxLength == nil {
		s.MaxLength = ss.MaxLength
	} else if ss.MaxLength != nil && s.MaxLength.Value > ss.MaxLength.Value {
		return nil, StacktraceNew("maxLength constraint violation", s.Location,
			stacktrace.WithPosition(&s.MaxLength.ValuePos),
			stacktrace.WithInfo("source", ss.MaxLength.Value),
			stacktrace.WithInfo("target", s.MaxLength.Value))
	}
	if s.FileTypes == nil {
		s.FileTypes = ss.FileTypes
	} else if ss.FileTypes != nil && !isCompatibleFileTypes(ss.FileTypes, s.FileTypes) {
		return nil, StacktraceNew("file types are incompatible", s.Location,
			stacktrace.WithPosition(&s.KeyPos),
			stacktrace.WithInfo("source", ss.FileTypes),
			stacktrace.WithInfo("target", s.FileTypes))
	}
	return s, nil
}

func (s *FileShape) check() error {
	if s.MinLength != nil && s.MaxLength != nil && s.MinLength.Value > s.MaxLength.Value {
		return StacktraceNew("minLength must be less than or equal to maxLength", s.Location,
			stacktrace.WithPosition(&s.MinLength.ValuePos))
	}
	return nil
}

func (s *FileShape) String() string {
	var facets []string
	if s.MinLength != nil {
		facets = append(facets, fmt.Sprintf("minLength:%d", s.MinLength.Value))
	}
	if s.MaxLength != nil {
		facets = append(facets, fmt.Sprintf("maxLength:%d", s.MaxLength.Value))
	}
	if s.FileTypes != nil && len(s.FileTypes) > 0 {
		facets = append(facets, fmt.Sprintf("fileTypes:%d", len(s.FileTypes)))
	}
	return fmt.Sprintf("FileShape{facets:[%s]}", strings.Join(facets, ","))
}

func (s *FileShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]
		switch node.Value {
		case FacetMinLength:
			sn, err := MakeScalarFacetYAML[uint64](s.raml, node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("minLength", err, s.Location, WithNodePosition(valueNode))
			}
			s.MinLength = sn
		case FacetMaxLength:
			sn, err := MakeScalarFacetYAML[uint64](s.raml, node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("maxLength", err, s.Location, WithNodePosition(valueNode))
			}
			s.MaxLength = sn
		case FacetFileTypes:
			if valueNode.Kind != yaml.SequenceNode {
				return StacktraceNew("fileTypes must be sequence node", s.Location, WithNodePosition(valueNode))
			}
			fileTypes := make([]*Node[string], len(valueNode.Content))
			for i, v := range valueNode.Content {
				fragmentPath, rv, err := s.raml.resolveInclude(v, s.Location)
				if err != nil {
					return StacktraceNewWrapped("resolve fileType", err, s.Location, WithNodePosition(v))
				}
				if rv.Tag != TagStr {
					return StacktraceNew("fileTypes item must be a string", s.Location, WithNodePosition(v))
				}
				var str string
				if err := rv.Decode(&str); err != nil {
					return StacktraceNewWrapped("decode fileType", err, s.Location, WithNodePosition(v))
				}
				fileTypes[i] = MakeSeqNode(str, v, s.Location, fragmentPath)
			}
			s.FileTypes = fileTypes
		default:
			n, err := s.raml.makeRootNode(node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
			}
			s.CustomShapeFacets.Set(node.Value, n)
		}
	}
	return nil
}

type BooleanShape struct {
	scalarShape
	*BaseShape
}

func (s *BooleanShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *BooleanShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *BooleanShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *BooleanShape) alias(source Shape) (Shape, error) {
	_, err := checkAliasType[*BooleanShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *BooleanShape) validate(v any, _ string) error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	_, ok := v.(bool)
	if !ok {
		return fmt.Errorf("invalid type, got %T, expected bool", v)
	}
	return nil
}

func (s *BooleanShape) inherit(source Shape) (Shape, error) {
	_, err := checkInheritType[*BooleanShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *BooleanShape) check() error {
	return nil
}

func (s *BooleanShape) String() string {
	return "BooleanShape{facets:[]}"
}

func (s *BooleanShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]

		n, err := s.raml.makeRootNode(node, valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
		}
		s.CustomShapeFacets.Set(node.Value, n)
	}

	return nil
}

type DateTimeShape struct {
	scalarShape
	*BaseShape

	FormatFacets
}

func (s *DateTimeShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *DateTimeShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *DateTimeShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *DateTimeShape) alias(source Shape) (Shape, error) {
	ss, err := checkAliasType[*DateTimeShape](s, source)
	if err != nil {
		return nil, err
	}
	s.Format = ss.Format
	return s, nil
}

func (s *DateTimeShape) validate(v any, _ string) error {
	if s.BaseShape == nil {
		return errors.New("BaseShape is required")
	}
	i, ok := v.(string)
	if !ok {
		return fmt.Errorf("invalid type, got %T, expected string", v)
	}

	if s.Format == nil {
		if _, err := time.Parse(time.RFC3339, i); err != nil {
			return fmt.Errorf("value must match format %s", time.RFC3339)
		}
	} else {
		switch s.Format.Value {
		case DateTimeFormatRFC3339:
			if _, err := time.Parse(time.RFC3339, i); err != nil {
				return fmt.Errorf("value must match format %s", time.RFC3339)
			}
		// TODO: https://www.rfc-editor.org/rfc/rfc7231#section-7.1.1.1
		case DateTimeFormatRFC2616:
			if _, err := time.Parse(RFC2616, i); err != nil {
				return fmt.Errorf("value must match format %s", RFC2616)
			}
		}
	}

	return nil
}

func (s *DateTimeShape) inherit(source Shape) (Shape, error) {
	ss, err := checkInheritType[*DateTimeShape](s, source)
	if err != nil {
		return nil, err
	}
	if s.Format == nil {
		s.Format = ss.Format
	} else if ss.Format != nil && s.Format.Value != ss.Format.Value {
		return nil, StacktraceNew("format constraint violation", s.Location, stacktrace.WithPosition(&s.Format.ValuePos),
			stacktrace.WithInfo("source", ss.Format.Value), stacktrace.WithInfo("target", s.Format.Value))
	}
	return s, nil
}

func (s *DateTimeShape) check() error {
	return nil
}

func (s *DateTimeShape) String() string {
	var facets []string
	if s.Format != nil && s.Format.Value != "" {
		facets = append(facets, fmt.Sprintf("format:%s", s.Format.Value))
	}
	return fmt.Sprintf("DateTimeShape{facets:[%s]}", strings.Join(facets, ","))
}

func (s *DateTimeShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]
		if node.Value == FacetFormat {
			sn, err := MakeScalarFacetYAML[string](s.raml, node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make scalar node", err, s.Location, WithNodePosition(valueNode))
			}
			if _, ok := SetOfDateTimeFormats[sn.Value]; !ok {
				return StacktraceNew("invalid format", s.Location, WithNodePosition(valueNode),
					stacktrace.WithInfo("allowed_formats", SetOfDateTimeFormats))
			}
			s.Format = sn
		} else {
			n, err := s.raml.makeRootNode(node, valueNode, s.Location)
			if err != nil {
				return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
			}
			s.CustomShapeFacets.Set(node.Value, n)
		}
	}
	return nil
}

type DateTimeOnlyShape struct {
	scalarShape
	*BaseShape
}

func (s *DateTimeOnlyShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *DateTimeOnlyShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *DateTimeOnlyShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *DateTimeOnlyShape) alias(source Shape) (Shape, error) {
	_, err := checkAliasType[*DateTimeOnlyShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DateTimeOnlyShape) validate(v any, _ string) error {
	i, ok := v.(string)
	if !ok {
		return fmt.Errorf("invalid type, got %T, expected string", v)
	}

	if _, err := time.Parse(DateTime, i); err != nil {
		return fmt.Errorf("value must match format %s", DateTime)
	}

	return nil
}

func (s *DateTimeOnlyShape) inherit(source Shape) (Shape, error) {
	_, err := checkInheritType[*DateTimeOnlyShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DateTimeOnlyShape) check() error {
	return nil
}

func (s *DateTimeOnlyShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]

		n, err := s.raml.makeRootNode(node, valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
		}
		s.CustomShapeFacets.Set(node.Value, n)
	}
	return nil
}

type DateOnlyShape struct {
	scalarShape
	*BaseShape
}

func (s *DateOnlyShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *DateOnlyShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *DateOnlyShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *DateOnlyShape) alias(source Shape) (Shape, error) {
	_, err := checkAliasType[*DateOnlyShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DateOnlyShape) validate(v any, _ string) error {
	i, ok := v.(string)
	if !ok {
		return fmt.Errorf("invalid type, got %T, expected string", v)
	}

	if _, err := time.Parse(time.DateOnly, i); err != nil {
		return fmt.Errorf("value must match format %s", time.DateOnly)
	}

	return nil
}

func (s *DateOnlyShape) inherit(source Shape) (Shape, error) {
	_, err := checkInheritType[*DateOnlyShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DateOnlyShape) check() error {
	return nil
}

func (s *DateOnlyShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]

		n, err := s.raml.makeRootNode(node, valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
		}
		s.CustomShapeFacets.Set(node.Value, n)
	}
	return nil
}

type TimeOnlyShape struct {
	scalarShape
	*BaseShape
}

func (s *TimeOnlyShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *TimeOnlyShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *TimeOnlyShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *TimeOnlyShape) alias(source Shape) (Shape, error) {
	_, err := checkAliasType[*TimeOnlyShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *TimeOnlyShape) validate(v any, _ string) error {
	i, ok := v.(string)
	if !ok {
		return fmt.Errorf("invalid type, got %T, expected string", v)
	}

	if _, err := time.Parse(time.TimeOnly, i); err != nil {
		return fmt.Errorf("value must match format %s", time.TimeOnly)
	}

	return nil
}

func (s *TimeOnlyShape) inherit(source Shape) (Shape, error) {
	_, err := checkInheritType[*TimeOnlyShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *TimeOnlyShape) check() error {
	return nil
}

func (s *TimeOnlyShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]

		n, err := s.raml.makeRootNode(node, valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
		}
		s.CustomShapeFacets.Set(node.Value, n)
	}
	return nil
}

type AnyShape struct {
	scalarShape
	*BaseShape
}

func (s *AnyShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *AnyShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *AnyShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *AnyShape) alias(source Shape) (Shape, error) {
	_, err := checkAliasType[*AnyShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Validate checks if the value is nil, implements Shape interface
func (s *AnyShape) validate(_ any, _ string) error {
	return nil
}

func (s *AnyShape) inherit(source Shape) (Shape, error) {
	_, err := checkInheritType[*AnyShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *AnyShape) check() error {
	return nil
}

func (s *AnyShape) String() string {
	return "AnyShape{}"
}

func (s *AnyShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]

		n, err := s.raml.makeRootNode(node, valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
		}
		s.CustomShapeFacets.Set(node.Value, n)
	}
	return nil
}

type NilShape struct {
	scalarShape
	*BaseShape
}

func (s *NilShape) Base() *BaseShape {
	return s.BaseShape
}

func (s *NilShape) cloneShallow(base *BaseShape) Shape {
	return s.clone(base, nil)
}

func (s *NilShape) clone(base *BaseShape, _ map[int64]*BaseShape) Shape {
	c := *s
	c.BaseShape = base
	return &c
}

func (s *NilShape) alias(source Shape) (Shape, error) {
	_, err := checkAliasType[*NilShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Validate checks if the value is nil, implements Shape interface
func (s *NilShape) validate(v any, _ string) error {
	if v != nil {
		return fmt.Errorf("invalid type, got %T, expected nil", v)
	}
	return nil
}

func (s *NilShape) inherit(source Shape) (Shape, error) {
	_, err := checkInheritType[*NilShape](s, source)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *NilShape) check() error {
	return nil
}

func (s *NilShape) String() string {
	return "NilShape{}"
}

func (s *NilShape) unmarshalYAMLNodes(v []*yaml.Node) error {
	for i := 0; i != len(v); i += 2 {
		node := v[i]
		valueNode := v[i+1]

		n, err := s.raml.makeRootNode(node, valueNode, s.Location)
		if err != nil {
			return StacktraceNewWrapped("make node", err, s.Location, WithNodePosition(valueNode))
		}
		s.CustomShapeFacets.Set(node.Value, n)
	}
	return nil
}
