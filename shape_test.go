package raml

import (
	"reflect"
	"testing"
)

func TestBaseShape_clone(t *testing.T) {
	type fields struct {
		Id                          string
		Name                        string
		DisplayName                 *string
		Description                 *string
		Type                        string
		Example                     *Example
		Examples                    *Examples
		Inherits                    []*Shape
		Default                     *Node
		Required                    *bool
		unwrapped                   bool
		Link                        *DataType
		CustomShapeFacets           CustomShapeFacets
		CustomShapeFacetDefinitions CustomShapeFacetDefinitions
		CustomDomainProperties      CustomDomainProperties
		raml                        *RAML
		Location                    string
		Position                    Position
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *BaseShape
	}{
		{
			name: "Test BaseShape clone with nil",
			fields: fields{
				Id:                          "",
				Name:                        "",
				DisplayName:                 nil,
				Description:                 nil,
				Type:                        "",
				Example:                     nil,
				Examples:                    nil,
				Inherits:                    nil,
				Default:                     nil,
				Required:                    nil,
				unwrapped:                   false,
				Link:                        nil,
				CustomShapeFacets:           nil,
				CustomShapeFacetDefinitions: nil,
				CustomDomainProperties:      nil,
				raml:                        nil,
				Location:                    "",
				Position:                    Position{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &BaseShape{
				Id:                          "",
				Name:                        "",
				DisplayName:                 nil,
				Description:                 nil,
				Type:                        "",
				Example:                     nil,
				Examples:                    nil,
				Inherits:                    nil,
				Default:                     nil,
				Required:                    nil,
				unwrapped:                   false,
				Link:                        nil,
				CustomShapeFacets:           nil,
				CustomShapeFacetDefinitions: nil,
				CustomDomainProperties:      nil,
				raml:                        nil,
				Location:                    "",
				Position:                    Position{},
			},
		},
		{
			name: "Test BaseShape clone with non-nil",
			fields: fields{
				Id:                          "id",
				Name:                        "name",
				DisplayName:                 func() *string { v := "display name"; return &v }(),
				Description:                 func() *string { v := "description"; return &v }(),
				Type:                        "type",
				Example:                     &Example{},
				Examples:                    &Examples{},
				Inherits:                    []*Shape{},
				Default:                     &Node{},
				Required:                    func() *bool { v := true; return &v }(),
				unwrapped:                   true,
				Link:                        &DataType{},
				CustomShapeFacets:           CustomShapeFacets{},
				CustomShapeFacetDefinitions: CustomShapeFacetDefinitions{},
				CustomDomainProperties:      CustomDomainProperties{},
				raml:                        &RAML{},
				Location:                    "location",
				Position:                    Position{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &BaseShape{
				Id:                          "id",
				Name:                        "name",
				DisplayName:                 func() *string { v := "display name"; return &v }(),
				Description:                 func() *string { v := "description"; return &v }(),
				Type:                        "type",
				Example:                     &Example{},
				Examples:                    &Examples{},
				Inherits:                    []*Shape{},
				Default:                     &Node{},
				Required:                    func() *bool { v := true; return &v }(),
				unwrapped:                   true,
				Link:                        &DataType{},
				CustomShapeFacets:           CustomShapeFacets{},
				CustomShapeFacetDefinitions: CustomShapeFacetDefinitions{},
				CustomDomainProperties:      CustomDomainProperties{},
				raml:                        nil,
				Location:                    "location",
				Position:                    Position{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &BaseShape{
				Id:                          tt.fields.Id,
				Name:                        tt.fields.Name,
				DisplayName:                 tt.fields.DisplayName,
				Description:                 tt.fields.Description,
				Type:                        tt.fields.Type,
				Example:                     tt.fields.Example,
				Examples:                    tt.fields.Examples,
				Inherits:                    tt.fields.Inherits,
				Default:                     tt.fields.Default,
				Required:                    tt.fields.Required,
				unwrapped:                   tt.fields.unwrapped,
				Link:                        tt.fields.Link,
				CustomShapeFacets:           tt.fields.CustomShapeFacets,
				CustomShapeFacetDefinitions: tt.fields.CustomShapeFacetDefinitions,
				CustomDomainProperties:      tt.fields.CustomDomainProperties,
				raml:                        tt.fields.raml,
				Location:                    tt.fields.Location,
				Position:                    tt.fields.Position,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExamples_clone(t *testing.T) {
	type fields struct {
		Id       string
		Examples map[string]*Example
		Link     *NamedExample
		Location string
		Position Position
		raml     *RAML
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *Examples
	}{
		{
			name: "Test Examples clone with nil",
			fields: fields{
				Id:       "",
				Examples: nil,
				Link:     nil,
				Location: "",
				Position: Position{},
				raml:     nil,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Examples{
				Id:       "",
				Examples: nil,
				Link:     nil,
				Location: "",
				Position: Position{},
				raml:     nil,
			},
		},
		{
			name: "Test Examples clone with non-nil",
			fields: fields{
				Id:       "id",
				Examples: map[string]*Example{},
				Link:     &NamedExample{},
				Location: "location",
				Position: Position{},
				raml:     &RAML{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Examples{
				Id:       "id",
				Examples: map[string]*Example{},
				Link:     &NamedExample{},
				Location: "location",
				Position: Position{},
				raml:     nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Examples{
				Id:       tt.fields.Id,
				Examples: tt.fields.Examples,
				Link:     tt.fields.Link,
				Location: tt.fields.Location,
				Position: tt.fields.Position,
				raml:     tt.fields.raml,
			}
			if got := e.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}
