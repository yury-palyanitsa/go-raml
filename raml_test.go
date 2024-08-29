package raml

import (
	"container/list"
	"context"
	"reflect"
	"testing"
)

func TestRAML_Clone(t *testing.T) {
	type fields struct {
		fragmentsCache   map[string]Fragment
		fragmentShapes   map[string]map[string]*Shape
		shapes           []*Shape
		entryPoint       Fragment
		domainExtensions []*DomainExtension
		unresolvedShapes list.List
		ctx              context.Context
	}
	type args struct {
		ctx context.Context
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		want   *RAML
		fix    func(got, want *RAML)
	}{
		{
			name:   "Test RAML clone with nil",
			fields: fields{},
			args: args{
				ctx: nil,
			},
			want: &RAML{},
			fix:  func(got, want *RAML) {},
		},
		{
			name: "Test RAML clone with non-nil",
			fields: fields{
				fragmentsCache: map[string]Fragment{
					"key": &Library{
						Id:                     "id",
						Usage:                  "usage",
						AnnotationTypes:        map[string]*Shape{},
						Types:                  map[string]*Shape{},
						Uses:                   map[string]*LibraryLink{},
						CustomDomainProperties: CustomDomainProperties{},
						Location:               "location",
						raml:                   nil,
					},
					"nilkey": nil,
				},
				fragmentShapes: map[string]map[string]*Shape{
					"key": {
						"nilkey": nil,
						"key": func() *Shape {
							var s Shape
							s = &StringShape{}
							return &s
						}(),
					},
					"nilkey": nil,
				},
				shapes: []*Shape{
					func() *Shape {
						var s Shape
						s = &StringShape{}
						return &s
					}(),
				},
				domainExtensions: []*DomainExtension{
					{},
				},
				entryPoint: &Library{},
			},
			args: args{
				nil,
			},
			want: &RAML{
				fragmentsCache: map[string]Fragment{
					"key": &Library{
						Id:                     "id",
						Usage:                  "usage",
						AnnotationTypes:        map[string]*Shape{},
						Types:                  map[string]*Shape{},
						Uses:                   map[string]*LibraryLink{},
						CustomDomainProperties: CustomDomainProperties{},
						Location:               "location",
						raml:                   nil,
					},
					"nilkey": nil,
				},
				fragmentShapes: map[string]map[string]*Shape{
					"key": {
						"nilkey": nil,
						"key": func() *Shape {
							var s Shape
							s = &StringShape{}
							return &s
						}(),
					},
					"nilkey": nil,
				},
				shapes: []*Shape{
					func() *Shape {
						var s Shape
						s = &StringShape{}
						return &s
					}(),
				},
				domainExtensions: []*DomainExtension{
					{},
				},
				entryPoint: &Library{},
			},
			fix: func(got, want *RAML) {
				want.fragmentsCache["key"].(*Library).raml = got

				s := want.fragmentShapes["key"]["key"]
				ss := *s
				ss.Base().raml = got

				s = want.shapes[0]
				ss = *s
				ss.Base().raml = got

				want.domainExtensions[0].raml = got
				want.entryPoint.(*Library).raml = got
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RAML{
				fragmentsCache:   tt.fields.fragmentsCache,
				fragmentShapes:   tt.fields.fragmentShapes,
				shapes:           tt.fields.shapes,
				entryPoint:       tt.fields.entryPoint,
				domainExtensions: tt.fields.domainExtensions,
				unresolvedShapes: tt.fields.unresolvedShapes,
				ctx:              tt.fields.ctx,
			}
			got := r.Clone(tt.args.ctx)
			tt.fix(got, tt.want)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Clone() = %v, want %v", got, tt.want)
			}
		})
	}
}
