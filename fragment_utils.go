package raml

import (
	"fmt"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// resolveReference resolves a reference name by first checking the local map,
// then checking uses for dotted references.
//
// For a reference like "lib.TypeName", it:
//  1. Checks if "lib.TypeName" exists as a local key (some fragments allow this)
//  2. Looks up "lib" in uses, then "TypeName" in the linked library
//
// For a reference like "TypeName", it:
//  1. Checks the local map directly
func resolveReference[T any](
	localMap *orderedmap.OrderedMap[string, T],
	uses *orderedmap.OrderedMap[string, *LibraryLink],
	refName string,
	getFromLink func(lib *Library, suffix string) (T, bool),
) (T, error) {
	before, after, found := CutReferenceName(refName)

	var zero T

	if !found {
		// Local reference (no dot)
		if localMap == nil {
			return zero, fmt.Errorf("reference \"%s\" not found", refName)
		}
		rr, ok := localMap.Get(refName)
		if !ok {
			return zero, fmt.Errorf("reference \"%s\" not found", refName)
		}
		return rr, nil
	}

	// Dotted reference - first try local lookup (some fragments allow "lib.Type" as a local key)
	if localMap != nil {
		rr, hasLocal := localMap.Get(refName)
		if hasLocal {
			return rr, nil
		}
	}

	// External reference via uses
	if uses == nil {
		return zero, fmt.Errorf("library \"%s\" not found", before)
	}
	lib, ok := uses.Get(before)
	if !ok {
		return zero, fmt.Errorf("library \"%s\" not found", before)
	}
	if lib.Link == nil {
		return zero, fmt.Errorf("library \"%s\" not resolved", before)
	}
	ref, ok := getFromLink(lib.Link, after)
	if !ok {
		return zero, fmt.Errorf("reference \"%s\" not found", after)
	}
	return ref, nil
}

// resolveLibraryReference resolves a reference that always requires a dotted
// library reference (no local types supported). Used by DataTypeFragment.
func resolveLibraryReference[T any](
	uses *orderedmap.OrderedMap[string, *LibraryLink],
	refName string,
	getFromLink func(lib *Library, suffix string) (T, bool),
) (T, error) {
	before, after, found := CutReferenceName(refName)
	var zero T
	if !found {
		return zero, fmt.Errorf("invalid reference \"%s\"", refName)
	}
	if uses == nil {
		return zero, fmt.Errorf("library \"%s\" not found", before)
	}
	lib, ok := uses.Get(before)
	if !ok {
		return zero, fmt.Errorf("library \"%s\" not found", before)
	}
	if lib.Link == nil {
		return zero, fmt.Errorf("library \"%s\" not resolved", before)
	}
	ref, ok := getFromLink(lib.Link, after)
	if !ok {
		return zero, fmt.Errorf("reference \"%s\" not found", after)
	}
	return ref, nil
}
