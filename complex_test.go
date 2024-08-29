package raml

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestArrayFacets_clone(t *testing.T) {
	type fields struct {
		Items       *Shape
		MinItems    *uint64
		MaxItems    *uint64
		UniqueItems bool
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *ArrayFacets
	}{
		{
			name:   "Test ArrayFacets clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &ArrayFacets{},
		},
		{
			name: "Test ArrayFacets clone with non-nil",
			fields: fields{
				Items:       nil,
				MinItems:    func() *uint64 { v := uint64(1); return &v }(),
				MaxItems:    func() *uint64 { v := uint64(10); return &v }(),
				UniqueItems: true,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &ArrayFacets{
				Items:       nil,
				MinItems:    func() *uint64 { v := uint64(1); return &v }(),
				MaxItems:    func() *uint64 { v := uint64(10); return &v }(),
				UniqueItems: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &ArrayFacets{
				Items:       tt.fields.Items,
				MinItems:    tt.fields.MinItems,
				MaxItems:    tt.fields.MaxItems,
				UniqueItems: tt.fields.UniqueItems,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArrayShape_clone(t *testing.T) {
	type fields struct {
		BaseShape   BaseShape
		ArrayFacets ArrayFacets
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Shape
	}{
		{
			name: "Test ArrayShape clone",
			fields: fields{
				BaseShape:   BaseShape{},
				ArrayFacets: ArrayFacets{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &ArrayShape{
				BaseShape:   BaseShape{},
				ArrayFacets: ArrayFacets{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ArrayShape{
				BaseShape:   tt.fields.BaseShape,
				ArrayFacets: tt.fields.ArrayFacets,
			}
			if got := a.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectFacets_clone(t *testing.T) {
	type fields struct {
		Discriminator        string
		DiscriminatorValue   any
		AdditionalProperties bool
		Properties           map[string]Property
		MinProperties        *uint64
		MaxProperties        *uint64
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *ObjectFacets
	}{
		{
			name:   "Test ObjectFacets clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &ObjectFacets{},
		},
		{
			name: "Test ObjectFacets clone with non-nil",
			fields: fields{
				Discriminator:        "discriminator",
				DiscriminatorValue:   "discriminatorValue",
				AdditionalProperties: true,
				Properties:           map[string]Property{"key": {}},
				MinProperties:        func() *uint64 { v := uint64(1); return &v }(),
				MaxProperties:        func() *uint64 { v := uint64(10); return &v }(),
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &ObjectFacets{
				Discriminator:        "discriminator",
				DiscriminatorValue:   "discriminatorValue",
				AdditionalProperties: true,
				Properties:           map[string]Property{"key": {}},
				MinProperties:        func() *uint64 { v := uint64(1); return &v }(),
				MaxProperties:        func() *uint64 { v := uint64(10); return &v }(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &ObjectFacets{
				Discriminator:        tt.fields.Discriminator,
				DiscriminatorValue:   tt.fields.DiscriminatorValue,
				AdditionalProperties: tt.fields.AdditionalProperties,
				Properties:           tt.fields.Properties,
				MinProperties:        tt.fields.MinProperties,
				MaxProperties:        tt.fields.MaxProperties,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnionFacets_clone(t *testing.T) {
	type fields struct {
		AnyOf []*Shape
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *UnionFacets
	}{
		{
			name:   "Test UnionFacets clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &UnionFacets{},
		},
		{
			name: "Test UnionFacets clone with non-nil",
			fields: fields{
				AnyOf: []*Shape{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &UnionFacets{
				AnyOf: []*Shape{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &UnionFacets{
				AnyOf: tt.fields.AnyOf,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProperty_clone(t *testing.T) {
	type fields struct {
		Name     string
		Shape    *Shape
		Required bool
		raml     *RAML
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *Property
	}{
		{
			name:   "Test Property clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Property{},
		},
		{
			name: "Test Property clone with non-nil",
			fields: fields{
				Name:     "name",
				Shape:    func() *Shape { var s Shape; s = &StringShape{}; return &s }(),
				Required: true,
				raml:     &RAML{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Property{
				Name:     "name",
				Shape:    func() *Shape { var s Shape; s = &StringShape{}; return &s }(),
				Required: true,
				raml:     nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Property{
				Name:     tt.fields.Name,
				Shape:    tt.fields.Shape,
				Required: tt.fields.Required,
				raml:     tt.fields.raml,
			}
			if got := p.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectShape_clone(t *testing.T) {
	type fields struct {
		BaseShape    BaseShape
		ObjectFacets ObjectFacets
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Shape
	}{
		{
			name:   "Test ObjectShape clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &ObjectShape{},
		},
		{
			name: "Test ObjectShape clone with non-nil",
			fields: fields{
				BaseShape:    BaseShape{},
				ObjectFacets: ObjectFacets{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &ObjectShape{
				BaseShape:    BaseShape{},
				ObjectFacets: ObjectFacets{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ObjectShape{
				BaseShape:    tt.fields.BaseShape,
				ObjectFacets: tt.fields.ObjectFacets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnionShape_clone(t *testing.T) {
	type fields struct {
		BaseShape   BaseShape
		EnumFacets  EnumFacets
		UnionFacets UnionFacets
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Shape
	}{
		{
			name:   "Test UnionShape clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &UnionShape{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &UnionShape{
				BaseShape:   tt.fields.BaseShape,
				EnumFacets:  tt.fields.EnumFacets,
				UnionFacets: tt.fields.UnionFacets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJSONShape_clone(t *testing.T) {
	type fields struct {
		BaseShape BaseShape
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Shape
	}{
		{
			name:   "Test JSONShape clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &JSONShape{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &JSONShape{
				BaseShape: tt.fields.BaseShape,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnknownShape_clone(t *testing.T) {
	type fields struct {
		BaseShape BaseShape
		facets    []*yaml.Node
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Shape
	}{
		{
			name:   "Test UnknownShape clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &UnknownShape{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &UnknownShape{
				BaseShape: tt.fields.BaseShape,
				facets:    tt.fields.facets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}
