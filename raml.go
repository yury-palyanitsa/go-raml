package raml

import (
	"container/list"
	"context"
	"fmt"
	"reflect"
)

// RAML is a store for all fragments and shapes.
// WARNING: Not thread-safe
type RAML struct {
	fragmentsCache map[string]Fragment // Library, NamedExample, DataType
	fragmentShapes map[string]map[string]*Shape
	shapes         []*Shape
	// entryPoint is a Library, NamedExample or DataType fragment that is used as an entry point for the resolution.
	entryPoint Fragment

	// May be reused for both validation and resolution.
	domainExtensions []*DomainExtension
	// Temporary storage for unresolved shapes.
	unresolvedShapes list.List

	// ctx is a context of the RAML, for future use.
	ctx context.Context
}

// EntryPoint returns the entry point of the RAML.
func (r *RAML) EntryPoint() Fragment {
	return r.entryPoint
}

// SetEntryPoint sets the entry point of the RAML.
func (r *RAML) SetEntryPoint(entryPoint Fragment) *RAML {
	r.entryPoint = entryPoint
	return r
}

// GetLocation returns the location of the RAML.
func (r *RAML) GetLocation() string {
	if r.entryPoint == nil {
		return ""
	}
	return r.entryPoint.GetLocation()
}

// GetAllAnnotationsPtr returns all annotations as pointers.
func (r *RAML) GetAllAnnotationsPtr() []*DomainExtension {
	var annotations []*DomainExtension
	for _, de := range r.domainExtensions {
		annotations = append(annotations, de)
	}
	return annotations
}

// GetAllAnnotations returns all annotations.
func (r *RAML) GetAllAnnotations() []DomainExtension {
	var annotations []DomainExtension
	for _, de := range r.domainExtensions {
		annotations = append(annotations, *de)
	}
	return annotations
}

// New creates a new RAML.
func New(ctx context.Context) *RAML {
	return &RAML{
		fragmentShapes:   make(map[string]map[string]*Shape),
		fragmentsCache:   make(map[string]Fragment),
		domainExtensions: make([]*DomainExtension, 0),
		ctx:              ctx,
	}
}

// GetShapes returns all shapes.
func (r *RAML) GetShapes() []Shape {
	var shapes []Shape
	for _, shape := range r.shapes {
		shapes = append(shapes, *shape)
	}
	return shapes
}

// GetShapePtrs returns all shapes as pointers.
func (r *RAML) GetShapePtrs() []*Shape {
	return r.shapes
}

func (r *RAML) PutShapePtr(shape *Shape) {
	r.shapes = append(r.shapes, shape)
}

// GetFragmentShapesPtr returns fragment shapes as pointers.
func (r *RAML) GetFragmentShapesPtr(location string) map[string]*Shape {
	return r.fragmentShapes[location]
}

// GetFragmentShapes returns fragment shapes.
func (r *RAML) GetFragmentShapes(location string) map[string]Shape {
	shapes := r.fragmentShapes[location]
	res := make(map[string]Shape)
	for k, v := range shapes {
		res[k] = *v
	}
	return res
}

// GetFromFragmentPtr returns a shape from a fragment as a pointer.
func (r *RAML) GetFromFragmentPtr(location string, typeName string) (*Shape, error) {
	loc, ok := r.fragmentShapes[location]
	if !ok {
		return nil, fmt.Errorf("location %s not found", location)
	}
	return loc[typeName], nil
}

// GetFromFragment returns a shape from a fragment.
func (r *RAML) GetFromFragment(location string, typeName string) (Shape, error) {
	loc, ok := r.fragmentShapes[location]
	if !ok {
		return nil, fmt.Errorf("location %s not found", location)
	}
	return *loc[typeName], nil
}

// PutIntoFragment puts a shape into a fragment.
func (r *RAML) PutIntoFragment(name string, location string, shape *Shape) {
	loc, ok := r.fragmentShapes[location]
	if !ok {
		loc = make(map[string]*Shape)
		r.fragmentShapes[location] = loc
	}
	loc[name] = shape
}

// GetFragment returns a fragment.
func (r *RAML) GetFragment(location string) Fragment {
	return r.fragmentsCache[location]
}

// PutFragment puts a fragment.
func (r *RAML) PutFragment(location string, fragment Fragment) {
	if _, ok := r.fragmentsCache[location]; !ok {
		r.fragmentsCache[location] = fragment
	}
}

type _cloning struct {
	raml   *RAML
	cloned map[interface{}]interface{}
}

type cloner interface {
	clone(cloning *_cloning) interface{}
}

func cloneAny(cloning *_cloning, obj interface{}) interface{} {
	if clone, ok := cloning.cloned[obj]; ok {
		return clone
	}
	if obj == nil {
		return nil
	}
	if c, ok := obj.(cloner); ok {
		return c.clone(cloning)
	}
	// if obj is pointer, deep copy of the object
	if reflect.TypeOf(obj).Kind() == reflect.Ptr {
		clone := reflect.New(reflect.TypeOf(obj).Elem()).Interface()
		reflect.ValueOf(clone).Elem().Set(reflect.ValueOf(obj).Elem())
		cloning.cloned[obj] = clone
		return clone
	}

	cloning.cloned[obj] = obj
	return obj
}

func newCloning(raml *RAML) *_cloning {
	cloning := &_cloning{
		// raml is the clone of the original RAML.
		raml: raml,
		// cloned is a map of original objects to cloned objects.
		cloned: make(map[interface{}]interface{}),
	}
	cloning.cloned[nil] = nil
	return cloning
}

// Clone makes a deep copy of the RAML with a new context.
func (r *RAML) Clone(ctx context.Context) *RAML {
	if r == nil {
		return nil
	}
	clone := &RAML{}
	clone.ctx = ctx
	cloning := newCloning(clone)
	if r.entryPoint != nil {
		clone.entryPoint = r.entryPoint.clone(cloning)
	}
	// Clone fragments to fragments cache
	if r.fragmentsCache != nil {
		clone.fragmentsCache = make(map[string]Fragment)
		for fragKey, frag := range r.fragmentsCache {
			if frag == nil {
				clone.fragmentsCache[fragKey] = nil
				continue
			}
			clone.fragmentsCache[fragKey] = frag.clone(cloning)
		}
	}
	if r.fragmentShapes != nil {
		clone.fragmentShapes = make(map[string]map[string]*Shape)
		// Clone shapes to shapes and unresolved shapes
		for fragKey, shapes := range r.fragmentShapes {
			if shapes != nil {
				for shapeKey, shape := range shapes {
					if clone.fragmentShapes[fragKey] == nil {
						clone.fragmentShapes[fragKey] = make(map[string]*Shape)
					}
					if shape == nil {
						clone.fragmentShapes[fragKey][shapeKey] = nil
						continue
					}
					clonedShape := (*shape).clone(cloning)
					clone.fragmentShapes[fragKey][shapeKey] = &clonedShape
				}
			} else {
				clone.fragmentShapes[fragKey] = nil
			}
		}
	}
	if r.shapes != nil {
		clone.shapes = make([]*Shape, 0, len(r.shapes))
		for _, shape := range r.shapes {
			shapeClone := (*shape).clone(cloning)
			clone.shapes = append(clone.shapes, &shapeClone)
		}
	}
	if r.domainExtensions != nil {
		clone.domainExtensions = make([]*DomainExtension, 0, len(r.domainExtensions))
		for _, ext := range r.domainExtensions {
			extClone := ext.clone(cloning)
			clone.domainExtensions = append(clone.domainExtensions, extClone)
		}
	}
	// clone unresolved shapes
	if r.unresolvedShapes.Len() > 0 {
		clone.unresolvedShapes = list.List{}
		for e := r.unresolvedShapes.Front(); e != nil; e = e.Next() {
			value := cloneAny(cloning, e.Value)
			clone.unresolvedShapes.PushBack(value)
		}
	}

	return clone
}
