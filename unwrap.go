package raml

import (
	"fmt"

	"github.com/acronis/go-stacktrace"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// propagateURIParameters walks the endpoint tree and ensures every endpoint's
// URIParameters contains all path template parameters from ancestor endpoints,
// in path order (ancestor params first, own params after).
func (r *RAML) propagateURIParameters(api *APIFragment) {
	empty := orderedmap.New[string, *BaseShape](0)
	for pair := api.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		propagateURIParametersRecursive(pair.Value, empty)
	}
}

func propagateURIParametersRecursive(ep *EndPoint, inherited *orderedmap.OrderedMap[string, *BaseShape]) {
	// Build the complete ordered params for this endpoint:
	// inherited (ancestor-declared) first, then own (endpoint-declared).
	complete := orderedmap.New[string, *BaseShape](inherited.Len() + ep.URIParameters.Len())
	for pair := inherited.Oldest(); pair != nil; pair = pair.Next() {
		complete.Set(pair.Key, pair.Value)
	}
	for pair := ep.URIParameters.Oldest(); pair != nil; pair = pair.Next() {
		complete.Set(pair.Key, pair.Value)
	}
	ep.URIParameters = complete

	for pair := ep.EndPoints.Oldest(); pair != nil; pair = pair.Next() {
		propagateURIParametersRecursive(pair.Value, complete)
	}
}

func (r *RAML) unwrapTypes() *stacktrace.StackTrace {
	var acc stacktrace.Accumulator
	for location, shapes := range r.fragmentTypeDefinitions {
		for _, shape := range shapes {
			if shape == nil {
				acc.Add(StacktraceNew("shape is nil", location,
					stacktrace.WithType(StacktraceTypeUnwrapping)))
				continue
			}
			_, err := r.UnwrapShape(shape)
			if err != nil {
				acc.Add(StacktraceNewWrapped("unwrap shape", err, location,
					stacktrace.WithType(StacktraceTypeUnwrapping), stacktrace.WithPosition(&shape.KeyPos)))
				continue
			}
		}
	}
	return acc.Result()
}

func (r *RAML) unwrapTraitDefinitions(traitsDefs *orderedmap.OrderedMap[string, *TraitDefinition]) {
	for pair := traitsDefs.Oldest(); pair != nil; pair = pair.Next() {
		trait := pair.Value
		if trait.Link != nil {
			traitsDefs.Set(pair.Key, trait.Link.Trait)
		}
	}
}

func (r *RAML) unwrapSecuritySchemeDefinitions(securitySchemeDefs *orderedmap.OrderedMap[string, *SecuritySchemeDefinition]) {
	for pair := securitySchemeDefs.Oldest(); pair != nil; pair = pair.Next() {
		securitySchemeDef := pair.Value
		if securitySchemeDef.Link != nil {
			linked := securitySchemeDef.Link.SecurityScheme
			if linked.Name == "" {
				linked.Name = pair.Key
			}
			securitySchemeDefs.Set(pair.Key, linked)
		}
	}
}

func (r *RAML) unwrapResourceTypeDefinitions(rtDefs *orderedmap.OrderedMap[string, *ResourceTypeDefinition]) {
	for pair := rtDefs.Oldest(); pair != nil; pair = pair.Next() {
		rtDef := pair.Value
		if rtDef.Link != nil {
			rtDefs.Set(pair.Key, rtDef.Link.ResourceType)
		}
	}
}

func (r *RAML) unwrapAPIFragment(f *APIFragment) *stacktrace.StackTrace {
	// applyTraits and applySecuritySchemes are now called in parseFragment before
	// resolveShapes, so shapes from template compilation are properly resolved.
	r.unwrapTraitDefinitions(f.Traits)
	r.unwrapResourceTypeDefinitions(f.ResourceTypes)
	r.unwrapSecuritySchemeDefinitions(f.SecuritySchemes)
	return nil
}

func (r *RAML) unwrapLibrary(f *Library) *stacktrace.StackTrace {
	r.unwrapTraitDefinitions(f.Traits)
	r.unwrapResourceTypeDefinitions(f.ResourceTypes)
	r.unwrapSecuritySchemeDefinitions(f.SecuritySchemes)
	return nil
}

func (r *RAML) unwrapDataTypeFragment(f *DataTypeFragment) *stacktrace.StackTrace {
	if f.Shape == nil {
		return StacktraceNew("shape is nil", f.Location,
			stacktrace.WithType(StacktraceTypeUnwrapping))
	}
	_, err := r.UnwrapShape(f.Shape)
	if err != nil {
		return StacktraceNewWrapped("unwrap shape", err, f.Location,
			stacktrace.WithType(StacktraceTypeUnwrapping), stacktrace.WithPosition(&f.Shape.KeyPos))
	}
	return nil
}

func (r *RAML) unwrapFragments() *stacktrace.StackTrace {
	var acc stacktrace.Accumulator
	acc.Add(r.unwrapTypes())
	for _, frag := range r.fragmentsCache {
		switch f := frag.(type) {
		case *APIFragment:
			acc.Add(r.unwrapAPIFragment(f))
		case *Library:
			acc.Add(r.unwrapLibrary(f))
		case *DataTypeFragment:
			acc.Add(r.unwrapDataTypeFragment(f))
		}
	}
	return acc.Result()
}

// resourcePathName returns the rightmost path segment that is not a URI template parameter.
// e.g. "/users/{userId}/addresses" → "addresses", "/users/{userId}" → "users".
func resourcePathName(fullURI string) string {
	end := len(fullURI)

	for end > 0 {
		// skip trailing '/'
		for end > 0 && fullURI[end-1] == '/' {
			end--
		}
		if end == 0 {
			return ""
		}

		// find segment start
		start := end - 1
		for start >= 0 && fullURI[start] != '/' {
			start--
		}
		start++

		part := fullURI[start:end]

		// skip URI template params like "{userId}"
		if !(len(part) > 2 && part[0] == '{' && part[len(part)-1] == '}') {
			return part
		}

		end = start - 1
	}

	return ""
}

// resolveTraitDefinition looks up a trait by name using lexical scoping: the
// name resolves against the namespace of the document in which it is physically
// written. trait.anchorFrag is that document — the root API for an is: written
// in the API, or the declaring fragment for an is: written inside a ResourceType
// or Trait fragment.
//
// There is no application-site fallback. A trait name written inside a fragment
// resolves against that fragment's own uses:/traits ONLY; it does not leak into
// the including API's trait namespace. To reference an API- or library-defined
// trait from inside a fragment, the author imports it via the fragment's own
// uses: and qualifies the name (e.g. is: [traitsLib.paged]). This keeps typed
// fragments self-contained.
//
// api is used only as a defensive fallback when anchorFrag is unset (e.g. traits
// attached to programmatically constructed shapes).
func (r *RAML) resolveTraitDefinition(api *APIFragment, trait *Trait) (*TraitDefinition, error) {
	resolver := trait.anchorFrag
	if resolver == nil {
		return api.GetTraitDefinition(trait.Name)
	}
	return resolver.GetTraitDefinition(trait.Name)
}

// resolveResourceTypeDefinition looks up a resource type by name using lexical
// scoping, mirroring resolveTraitDefinition. The name resolves against the
// namespace of the document in which the type: entry is physically written
// (rt.anchorFrag): the root API for an endpoint-level type:, or the declaring
// fragment for a parent type: written inside a ResourceType fragment body.
//
// There is no application-site fallback. A resource type name written inside a
// fragment resolves against that fragment's own uses:/resourceTypes ONLY; it does
// not leak into the including API's resourceTypes namespace. To inherit an API- or
// library-defined resource type from inside a fragment, the author imports it via
// the fragment's own uses: and qualifies the name (e.g. type: rtLib.collection).
//
// api is used only as a defensive fallback when anchorFrag is unset (e.g. resource
// types attached to programmatically constructed endpoints).
func (r *RAML) resolveResourceTypeDefinition(api *APIFragment, rt *ResourceType) (*ResourceTypeDefinition, error) {
	resolver := rt.anchorFrag
	if resolver == nil {
		return api.GetResourceTypeDefinition(rt.Name)
	}
	return resolver.GetResourceTypeDefinition(rt.Name)
}

func (r *RAML) applySecuritySchemes(api *APIFragment) *stacktrace.StackTrace {
	for _, endPoint := range r.endPoints {
		if len(endPoint.SecuredBy) > 0 {
			// Endpoint-level security schemes are propagated to operations.
			// When the endpoint explicitly declared securedBy, it overrides the
			// API-level global on operations that have no explicit securedBy of
			// their own — replace rather than append in that case.
			for pair := endPoint.Operations.Oldest(); pair != nil; pair = pair.Next() {
				operation := pair.Value
				if operation.explicitSecuredBy {
					// Operation has its own explicit securedBy — leave it untouched.
					// This covers `securedBy: [null]` overriding inherited security.
				} else if endPoint.explicitSecuredBy {
					// Endpoint has explicit securedBy, replace operation's inherited global.
					operation.SecuredBy = endPoint.SecuredBy
				}
				// Neither has explicit securedBy: both already hold r.globalSecuredBy
				// from decodeSourceOperation / decodeSourceEndPoint — no action needed.
			}
		}
		for pair := endPoint.Operations.Oldest(); pair != nil; pair = pair.Next() {
			// Operation-level traits apply to a single operation
			operation := pair.Value
			for _, security := range operation.SecuredBy {
				if err := r.applySecurityScheme(api, security); err != nil {
					return StacktraceNewWrapped("apply security scheme", err, security.Location, stacktrace.WithPosition(&security.ValuePos),
						stacktrace.WithType(StacktraceTypeUnwrapping))
				}
			}
		}
	}
	return nil
}

func (r *RAML) applySecurityScheme(api *APIFragment, securityScheme *SecurityScheme) *stacktrace.StackTrace {
	// NOTE: If definition is already set, this is null security scheme.
	if securityScheme.Definition != nil {
		return nil
	}
	securitySchemeDef, err := api.GetSecuritySchemeDefinition(securityScheme.Name)
	if err != nil {
		return StacktraceNewWrapped("get security scheme definition", err, securityScheme.Location,
			stacktrace.WithPosition(&securityScheme.ValuePos), stacktrace.WithType(StacktraceTypeUnwrapping))
	}
	securityScheme.Definition = securitySchemeDef
	// NOTE: Security scheme description is not applied to the operation.
	// Apply operation-level parameter overrides (e.g. OAuth 2.0 scope narrowing).
	// Settings may be nil when the scheme was loaded via !include; in that case
	// the actual definition lives in the linked fragment.
	if securityScheme.Params != nil {
		settings := securityScheme.Definition.Settings
		if settings == nil && securityScheme.Definition.Link != nil && securityScheme.Definition.Link.SecurityScheme != nil {
			settings = securityScheme.Definition.Link.SecurityScheme.Settings
		}
		if applier, ok := settings.(OperationParamsApplier); ok {
			compiled, err := applier.ApplyOperationParams(securityScheme.Params)
			if err != nil {
				return StacktraceNew(err.Error(), securityScheme.Location,
					stacktrace.WithPosition(&securityScheme.ValuePos), stacktrace.WithType(StacktraceTypeUnwrapping),
					stacktrace.WithInfo("scheme", securityScheme.Name))
			}
			securityScheme.CompiledParams = compiled
		} else if _, hasScopesKey := securityScheme.Params["scopes"]; hasScopesKey {
			return StacktraceNew("scopes override is only valid for OAuth 2.0 schemes",
				securityScheme.Location, stacktrace.WithPosition(&securityScheme.ValuePos),
				stacktrace.WithType(StacktraceTypeUnwrapping),
				stacktrace.WithInfo("scheme", securityScheme.Name))
		}
	}
	// TODO: Maybe some checks can be implemented? Like method shadowing security status codes, headers and query parameters?
	return nil
}

func (r *RAML) unwrapDomainExtensions() *stacktrace.StackTrace {
	var acc stacktrace.Accumulator
	for _, item := range r.domainExtensions {
		db := item.DefinedBy
		if db == nil {
			continue
		}
		ptr, err := r.GetAnnotationTypeFromFragmentPtr(db.Location, db.Name)
		if err != nil {
			acc.Add(StacktraceNewWrapped("get annotation from fragment", err, db.Location,
				stacktrace.WithPosition(&db.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping)))
			continue
		}
		item.DefinedBy = ptr
	}
	return acc.Result()
}

// UnwrapShapes unwraps all shapes in the RAML in-place.
func (r *RAML) UnwrapShapes() error {
	// We need to invalidate old cache and re-populate it because references will no longer be valid after unwrapping.
	r.shapes = make([]*BaseShape, 0, len(r.shapes))
	if err := r.unwrapFragments(); err != nil {
		return fmt.Errorf("unwrap fragments: %w", err)
	}
	if err := r.markShapeRecursions(); err != nil {
		return fmt.Errorf("mark shape recursions: %w", err)
	}
	// Links to definedBy must be updated after unwrapping.
	if err := r.unwrapDomainExtensions(); err != nil {
		return fmt.Errorf("unwrap domain extensions: %w", err)
	}
	r.unwrapped = true
	return nil
}

// markShapeRecursions marks recursive shapes by replacing the beginning of recursion with RecursiveShape in the RAML.
func (r *RAML) markShapeRecursions() error {
	// TODO: Maybe count shapes here?
	for _, frag := range r.fragmentsCache {
		switch f := frag.(type) {
		case *Library:
			for pair := f.AnnotationTypes.Oldest(); pair != nil; pair = pair.Next() {
				if _, err := r.FindAndMarkRecursion(pair.Value); err != nil {
					return err
				}
			}
			for pair := f.Types.Oldest(); pair != nil; pair = pair.Next() {
				if _, err := r.FindAndMarkRecursion(pair.Value); err != nil {
					return err
				}
			}
		case *APIFragment:
			for pair := f.Types.Oldest(); pair != nil; pair = pair.Next() {
				if _, err := r.FindAndMarkRecursion(pair.Value); err != nil {
					return err
				}
			}
		case *DataTypeFragment:
			if _, err := r.FindAndMarkRecursion(f.Shape); err != nil {
				return err
			}
		}
	}
	return nil
}

// FindAndMarkRecursion finds recursive shapes and replaces them with RecursiveShape.
func (r *RAML) FindAndMarkRecursion(base *BaseShape) (*BaseShape, error) {
	if !base.IsUnwrapped() {
		return nil, StacktraceNew("shape is not unwrapped", base.Location, stacktrace.WithPosition(&base.KeyPos))
	}

	if base.ShapeVisited {
		s := r.MakeRecursiveShape(base)
		s.unwrapped = true
		return s, nil
	}
	base.ShapeVisited = true

	var err error
	switch t := base.Shape.(type) {
	case *ArrayShape:
		err = r.findAndMarkRecursionInArrayShape(t)
	case *ObjectShape:
		err = r.findAndMarkRecursionInObjectShape(t)
	case *UnionShape:
		err = r.findAndMarkRecursionInUnionShape(t)
	}
	if err != nil {
		return nil, err
	}

	// Reset the context to avoid generating recursive shape
	// for trait that points to the same type that defines this trait.
	// This is OK because traits cannot have nested traits and
	// cannot be used as a source for inheritance.
	base.ShapeVisited = false
	err = r.findAndMarkRecursionInCustomShapeFacetDefinitions(base)
	if err != nil {
		return nil, err
	}

	return nil, ErrNil
}

func (r *RAML) findAndMarkRecursionInCustomShapeFacetDefinitions(base *BaseShape) error {
	for pair := base.CustomShapeFacetDefinitions.Oldest(); pair != nil; pair = pair.Next() {
		prop := pair.Value
		rs, err := r.FindAndMarkRecursion(prop.Base)
		if err != nil {
			return fmt.Errorf("find and mark recursion: %w", err)
		}
		if rs != nil {
			prop.Base = rs
			base.CustomShapeFacetDefinitions.Set(pair.Key, prop)
		}
	}
	return nil
}

func (r *RAML) findAndMarkRecursionInArrayShape(t *ArrayShape) error {
	if t.Items != nil {
		rs, err := r.FindAndMarkRecursion(t.Items)
		if err != nil {
			return fmt.Errorf("find and mark recursion: %w", err)
		}
		if rs != nil {
			t.Items = rs
		}
	}
	return nil
}

func (r *RAML) findAndMarkRecursionInObjectShape(t *ObjectShape) error {
	for pair := t.Properties.Oldest(); pair != nil; pair = pair.Next() {
		rs, err := r.FindAndMarkRecursion(pair.Value.Base)
		if err != nil {
			return fmt.Errorf("find and mark recursion: %w", err)
		}
		if rs != nil {
			pair.Value.Base = rs
			t.Properties.Set(pair.Key, pair.Value)
		}
	}
	for pair := t.PatternProperties.Oldest(); pair != nil; pair = pair.Next() {
		rs, err := r.FindAndMarkRecursion(pair.Value.Base)
		if err != nil {
			return fmt.Errorf("find and mark recursion: %w", err)
		}
		if rs != nil {
			pair.Value.Base = rs
			t.PatternProperties.Set(pair.Key, pair.Value)
		}
	}
	return nil
}

func (r *RAML) findAndMarkRecursionInUnionShape(t *UnionShape) error {
	for i, item := range t.AnyOf {
		rs, err := r.FindAndMarkRecursion(item)
		if err != nil {
			return fmt.Errorf("find and mark recursion: %w", err)
		}
		if rs != nil {
			t.AnyOf[i] = rs
		}
	}
	return nil
}

func (r *RAML) unwrapObjShape(objShape *ObjectShape) error {
	for pair := objShape.Properties.Oldest(); pair != nil; pair = pair.Next() {
		us, err := r.UnwrapShape(pair.Value.Base)
		if err != nil {
			return StacktraceNewWrapped("object property unwrap", err, objShape.Location,
				stacktrace.WithPosition(&objShape.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
		pair.Value.Base = us
		objShape.Properties.Set(pair.Key, pair.Value)
	}
	for pair := objShape.PatternProperties.Oldest(); pair != nil; pair = pair.Next() {
		us, err := r.UnwrapShape(pair.Value.Base)
		if err != nil {
			return StacktraceNewWrapped("object pattern property unwrap", err, objShape.Location,
				stacktrace.WithPosition(&objShape.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
		pair.Value.Base = us
		objShape.PatternProperties.Set(pair.Key, pair.Value)
	}

	return nil
}

func (r *RAML) unwrapArrayShape(arrayShape *ArrayShape) error {
	if arrayShape.Items != nil {
		us, err := r.UnwrapShape(arrayShape.Items)
		if err != nil {
			return StacktraceNewWrapped("array item unwrap", err, arrayShape.Location,
				stacktrace.WithPosition(&arrayShape.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
		arrayShape.Items = us
	}
	return nil
}

func (r *RAML) unwrapUnionShape(unionShape *UnionShape) error {
	for i, item := range unionShape.AnyOf {
		us, err := r.UnwrapShape(item)
		if err != nil {
			return StacktraceNewWrapped("union unwrap", err, unionShape.Location,
				stacktrace.WithPosition(&unionShape.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
		unionShape.AnyOf[i] = us
	}
	return nil
}

// makeMultipleInheritanceShape creates an empty BaseShape whose concrete type matches the
// given parent shape. The orderedmap fields that BaseShape.Inherit and the
// concrete inherit methods check for nil are pre-initialized to empty non-nil
// maps so that those methods always enter the merge loop and never alias into a
// parent's map.
// parents must be the full slice of already-unwrapped parents so that fields
// such as ArrayShape.Items can be pre-initialized based on whichever parent
// first provides them.
func (r *RAML) makeMultipleInheritanceShape(parents []*BaseShape) *BaseShape {
	first := parents[0]
	mis := &BaseShape{
		raml:                        r,
		Location:                    first.Location,
		KeyPos:                      first.KeyPos,
		ValuePos:                    first.ValuePos,
		Type:                        first.Type,
		CustomShapeFacets:           orderedmap.New[string, *DataNode](0),
		CustomShapeFacetDefinitions: orderedmap.New[string, Property](0),
		CustomDomainProperties:      orderedmap.New[string, *DomainExtension](0),
		unwrapped:                   true,
	}
	switch first.Shape.(type) {
	case *ObjectShape:
		obj := &ObjectShape{
			BaseShape: mis,
			ObjectFacets: ObjectFacets{
				Properties:        orderedmap.New[string, Property](0),
				PatternProperties: orderedmap.New[string, PatternProperty](0),
			},
		}
		mis.Shape = obj
	case *ArrayShape:
		arr := &ArrayShape{BaseShape: mis}
		// Find the first parent that defines Items so we can pre-initialize
		// the synthetic Items shape without making a copy. Without this,
		// ArrayShape.inherit would alias that parent's Items pointer into the
		// synthetic on first contact, and subsequent merges would mutate it in place.
		//
		// Skip self-referential Items (A.Items == A) — they are not yet replaced
		// with RecursiveShape at this stage, so we must guard against them
		// explicitly to avoid infinite recursion.
		for _, p := range parents {
			if pa, ok := p.Shape.(*ArrayShape); ok && pa.Items != nil && pa.Items != p {
				arr.Items = r.makeMultipleInheritanceShape([]*BaseShape{pa.Items})
				break
			}
		}
		mis.Shape = arr
	case *UnionShape:
		mis.Shape = &UnionShape{BaseShape: mis}
	case *IntegerShape:
		mis.Shape = &IntegerShape{BaseShape: mis}
	case *NumberShape:
		mis.Shape = &NumberShape{BaseShape: mis}
	case *StringShape:
		mis.Shape = &StringShape{BaseShape: mis}
	case *BooleanShape:
		mis.Shape = &BooleanShape{BaseShape: mis}
	case *FileShape:
		mis.Shape = &FileShape{BaseShape: mis}
	case *DateTimeShape:
		mis.Shape = &DateTimeShape{BaseShape: mis}
	case *DateTimeOnlyShape:
		mis.Shape = &DateTimeOnlyShape{BaseShape: mis}
	case *DateOnlyShape:
		mis.Shape = &DateOnlyShape{BaseShape: mis}
	case *TimeOnlyShape:
		mis.Shape = &TimeOnlyShape{BaseShape: mis}
	case *NilShape:
		mis.Shape = &NilShape{BaseShape: mis}
	default:
		// AnyShape and unknown types — treat as any
		mis.Shape = &AnyShape{BaseShape: mis}
	}
	return mis
}

func (r *RAML) unwrapParents(base *BaseShape) (*BaseShape, error) {
	if len(base.Inherits) == 0 {
		return nil, nil
	}
	inherits := base.Inherits
	// Unwrap all parents before creating the synthetic shape so that
	// makeMultipleInheritanceShape can inspect every parent's fields
	// (e.g. ArrayShape.Items) to decide what to pre-initialize.
	for i := range inherits {
		us, err := r.UnwrapShape(inherits[i])
		if err != nil {
			return nil, StacktraceNewWrapped("parent unwrap", err, base.Location,
				stacktrace.WithPosition(&base.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
		inherits[i] = us
	}
	if len(inherits) == 1 {
		return inherits[0], nil
	}
	// For multiple inheritance, accumulate into a synthetic shape so that
	// the original parent shapes in the registry are not mutated.
	mis := r.makeMultipleInheritanceShape(inherits)
	for i := range inherits {
		var err error
		mis, err = mis.Inherit(inherits[i])
		if err != nil {
			return nil, StacktraceNewWrapped("multiple parents unwrap", err, base.Location,
				stacktrace.WithPosition(&base.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
	}
	return mis, nil
}

// unwrapLink rewrites an !include link as an explicit inheritance edge on the
// base shape. The caller continues the normal unwrap flow, which then resolves
// the linked shape via unwrapParents and preserves its inheritance chain for
// downstream validation.
func (r *RAML) unwrapLink(base *BaseShape) {
	// TODO: Need to distinguish link type between Alias and Inherit.
	base.Inherits = []*BaseShape{base.Link.Shape}
	base.Link = nil
}

func (r *RAML) UnwrapTarget(target Shape) error {
	switch trg := target.(type) {
	case *ArrayShape:
		if err := r.unwrapArrayShape(trg); err != nil {
			return fmt.Errorf("unwrap array shape: %w", err)
		}
	case *ObjectShape:
		if err := r.unwrapObjShape(trg); err != nil {
			return fmt.Errorf("unwrap object shape: %w", err)
		}
	case *UnionShape:
		if err := r.unwrapUnionShape(trg); err != nil {
			return fmt.Errorf("unwrap union shape: %w", err)
		}
	}
	return nil
}

// UnwrapShape recursively copies and unwraps a shape in-place. Use Clone() to create a copy of a shape if necessary.
// Note that this method removes information about links.
func (r *RAML) UnwrapShape(base *BaseShape) (*BaseShape, error) {
	s := base.Shape
	if s == nil {
		return nil, StacktraceNew("shape is nil", base.Location, stacktrace.WithPosition(&base.KeyPos))
	}

	// Skip already unwrapped shapes
	if base.IsUnwrapped() {
		return base, nil
	}
	base.SetUnwrapped()

	// NOTE: An !include link is not inheritance at parse time, but the linked
	// shape is the semantic parent. Convert it into an explicit inheritance edge
	// so the regular Inherits-based unwrap path applies — this keeps the
	// inheritance chain visible to downstream validation.
	if base.Link != nil {
		r.unwrapLink(base)
	}

	// NOTE: Type aliasing is not inheritance and is not used as a source. It must be unwrapped and returned as is.
	if base.Alias != nil {
		us, err := r.UnwrapShape(base.Alias)
		if err != nil {
			return nil, StacktraceNewWrapped("alias unwrap", err, base.Location,
				stacktrace.WithPosition(&base.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
		r.PutShape(base)
		return base.AliasTo(us)
	}

	source, err := r.unwrapParents(base)
	if err != nil {
		return nil, StacktraceNewWrapped("unwrap parents", err, base.Location,
			stacktrace.WithPosition(&base.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
	}

	if err = r.UnwrapTarget(s); err != nil {
		return nil, StacktraceNewWrapped("unwrap target", err, base.Location,
			stacktrace.WithPosition(&base.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
	}

	for pair := base.CustomShapeFacetDefinitions.Oldest(); pair != nil; pair = pair.Next() {
		prop := pair.Value
		us, errUnwrap := r.UnwrapShape(prop.Base)
		if errUnwrap != nil {
			return nil, StacktraceNewWrapped("custom shape facet definition unwrap", errUnwrap, base.Location,
				stacktrace.WithPosition(&base.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
		// Reset custom shape facet definitions since traits cannot have nested traits.
		us.CustomShapeFacetDefinitions = orderedmap.New[string, Property]()
		prop.Base = us
		base.CustomShapeFacetDefinitions.Set(pair.Key, prop)
	}

	if source != nil {
		// Base shape inherits properties of the source shape in-place.
		is, errInherit := base.Inherit(source)
		if errInherit != nil {
			return nil, StacktraceNewWrapped("merge shapes", errInherit, base.Location,
				stacktrace.WithPosition(&base.KeyPos), stacktrace.WithType(StacktraceTypeUnwrapping))
		}
		base = is
	}
	r.PutShape(base)
	return base, nil
}
