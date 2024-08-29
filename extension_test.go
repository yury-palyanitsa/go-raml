package raml

import (
	"reflect"
	"testing"
)

func TestDomainExtension_clone(t *testing.T) {
	type fields struct {
		Id        string
		Name      string
		Extension *Node
		DefinedBy *Shape
		Location  string
		Position  Position
		raml      *RAML
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *DomainExtension
	}{
		{
			name:   "Test DomainExtension clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &DomainExtension{},
		},
		{
			name: "Test DomainExtension clone with non-nil",
			fields: fields{
				Id:        "id",
				Name:      "name",
				Extension: &Node{},
				DefinedBy: func() *Shape {
					var s Shape
					s = &StringShape{}
					return &s
				}(),
				Location: "location",
				Position: Position{},
				raml:     &RAML{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &DomainExtension{
				Id:        "id",
				Name:      "name",
				Extension: &Node{},
				DefinedBy: func() *Shape {
					var s Shape
					s = &StringShape{}
					return &s
				}(),
				Location: "location",
				Position: Position{},
				raml:     nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &DomainExtension{
				Id:        tt.fields.Id,
				Name:      tt.fields.Name,
				Extension: tt.fields.Extension,
				DefinedBy: tt.fields.DefinedBy,
				Location:  tt.fields.Location,
				Position:  tt.fields.Position,
				raml:      tt.fields.raml,
			}
			if got := e.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomDomainProperties_clone(t *testing.T) {
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name string
		c    CustomDomainProperties
		args args
		want CustomDomainProperties
	}{
		{
			name: "Test CustomDomainProperties clone with non-nil",
			c: CustomDomainProperties{
				"key":  &DomainExtension{},
				"key2": &DomainExtension{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: CustomDomainProperties{
				"key":  &DomainExtension{},
				"key2": &DomainExtension{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomShapeFacetDefinitions_clone(t *testing.T) {
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name string
		c    CustomShapeFacetDefinitions
		args args
		want CustomShapeFacetDefinitions
	}{
		{
			name: "Test CustomShapeFacetDefinitions clone with non-nil",
			c: CustomShapeFacetDefinitions{
				"key": Property{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: CustomShapeFacetDefinitions{
				"key": Property{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomShapeFacets_clone(t *testing.T) {
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name string
		c    CustomShapeFacets
		args args
		want CustomShapeFacets
	}{
		{
			name: "Test CustomShapeFacets clone with non-nil",
			c: CustomShapeFacets{
				"key": &Node{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: CustomShapeFacets{
				"key": &Node{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}
