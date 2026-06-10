package raml

import (
	"fmt"
	"hash/maphash"
	"math"
	"math/big"
	"slices"
	"strings"
)

func CutLast(s string, sep byte) (string, string, bool) {
	i := strings.LastIndexByte(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

var ErrNil error

// isValidProtocol reports whether a single protocol string is HTTP or HTTPS (case-insensitive).
func isValidProtocol(p string) bool {
	lower := strings.ToLower(p)
	return lower == "http" || lower == "https"
}

// isValidMediaType checks if a string is a valid MIME type format.
// A valid MIME type has the format "type/subtype" where type and subtype
// are non-empty and contain only alphanumeric characters, hyphens, and dots.
func isValidMediaType(mediaType string) bool {
	typ, sub, ok := strings.Cut(mediaType, "/")
	if !ok || typ == "" || sub == "" {
		return false
	}

	return validMediaToken(typ) && validMediaToken(sub)
}

func validMediaToken(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]

		if (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' ||
			c == '.' ||
			c == '+' {
			continue
		}

		return false
	}

	return true
}

// duplicateItems returns the indices of the first pair of equal items, or (-1, -1)
// if all items are unique. For small slices (≤ 20) it uses O(n²) comparison with
// semanticEqual — no allocations. For larger slices it uses a maphash.Hash
// written to incrementally (no per-item []byte allocation) with collision
// resolution via semanticEqual, so there are no false positives.
// Adapted from https://github.com/santhosh-tekuri/jsonschema/blob/180cde331debc44accf6ebd941e666732ecb0f8a/util.go#L334
func duplicateItems(arr []any) (int, int) {
	if len(arr) <= 20 {
		for i := 1; i < len(arr); i++ {
			for j := 0; j < i; j++ {
				if semanticEqual(arr[i], arr[j]) {
					return j, i
				}
			}
		}
		return -1, -1
	}

	m := make(map[uint64][]int)
	var h maphash.Hash
	for i, item := range arr {
		h.Reset()
		writeItemHash(item, &h)
		hash := h.Sum64()
		if indexes, ok := m[hash]; ok {
			for _, j := range indexes {
				if semanticEqual(item, arr[j]) {
					return j, i
				}
			}
		}
		m[hash] = append(m[hash], i)
	}
	return -1, -1
}

// semanticEqual reports whether two RAML-decoded values are equal under
// JSON-Schema semantics:
//   - nil, bool, string: identical type and value.
//   - Numeric types (int, uint, float64): compared by exact rational value
//     via big.Rat, so int(1) and float64(1.0) are equal without float64
//     precision loss for large integers.
//   - map[string]any: key-set and recursive value equality.
//   - []any: element-wise recursive equality.
func semanticEqual(a, b any) bool {
	switch a := a.(type) {
	case nil:
		return b == nil
	case bool:
		b, ok := b.(bool)
		return ok && a == b
	case string:
		b, ok := b.(string)
		return ok && a == b
	case int, uint, float64:
		switch b.(type) {
		case int, uint, float64:
		default:
			return false
		}
		na, ok1 := new(big.Rat).SetString(fmt.Sprint(a))
		nb, ok2 := new(big.Rat).SetString(fmt.Sprint(b))
		return ok1 && ok2 && na.Cmp(nb) == 0
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for k, va := range a {
			vb, ok := b[k]
			if !ok || !semanticEqual(va, vb) {
				return false
			}
		}
		return true
	case []any:
		b, ok := b.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i := range a {
			if !semanticEqual(a[i], b[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// writeItemHash writes a type-tagged, order-independent hash of v into h.
// Handles the concrete types produced by go-yaml: nil, bool, string,
// int, uint, float64, map[string]any, and []any.
// Adapted from: https://github.com/santhosh-tekuri/jsonschema/blob/180cde331debc44accf6ebd941e666732ecb0f8a/util.go#L366
func writeItemHash(v any, h *maphash.Hash) {
	switch v := v.(type) {
	case nil:
		_ = h.WriteByte(0)
	case bool:
		_ = h.WriteByte(1)
		if v {
			_ = h.WriteByte(1)
		} else {
			_ = h.WriteByte(0)
		}
	case string:
		_ = h.WriteByte(2)
		_, _ = h.WriteString(v)
	case int:
		_ = h.WriteByte(3)
		n := uint64(v)
		buf := [8]byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24), byte(n >> 32), byte(n >> 40), byte(n >> 48), byte(n >> 56)}
		_, _ = h.Write(buf[:])
	case uint:
		_ = h.WriteByte(4)
		n := v
		buf := [8]byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24), byte(n >> 32), byte(n >> 40), byte(n >> 48), byte(n >> 56)}
		_, _ = h.Write(buf[:])
	case float64:
		_ = h.WriteByte(5)
		n := math.Float64bits(v)
		buf := [8]byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24), byte(n >> 32), byte(n >> 40), byte(n >> 48), byte(n >> 56)}
		_, _ = h.Write(buf[:])
	case map[string]any:
		_ = h.WriteByte(6)
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			writeItemHash(k, h)
			writeItemHash(v[k], h)
		}
	case []any:
		_ = h.WriteByte(7)
		for _, item := range v {
			writeItemHash(item, h)
		}
	}
}

func chompImplicitOptional(nodeName string) (string, bool) {
	nameLen := len(nodeName)
	if nodeName != "" && nodeName[nameLen-1] == '?' {
		return nodeName[:nameLen-1], true
	}
	return nodeName, false
}
