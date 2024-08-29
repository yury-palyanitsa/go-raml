package raml

import (
	"reflect"
	"testing"
)

func TestExample_clone(t *testing.T) {
	type fields struct {
		Id                     string
		Name                   string
		DisplayName            string
		Description            string
		Value                  *Node
		CustomDomainProperties CustomDomainProperties
		Location               string
		Position               Position
		raml                   *RAML
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *Example
	}{
		{
			name:   "Test Example clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Example{},
		},
		{
			name: "Test Example clone with non-nil",
			fields: fields{
				Id:                     "id",
				Name:                   "name",
				DisplayName:            "display name",
				Description:            "description",
				Value:                  &Node{},
				CustomDomainProperties: CustomDomainProperties{},
				Location:               "location",
				Position:               Position{},
				raml:                   &RAML{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Example{
				Id:                     "id",
				Name:                   "name",
				DisplayName:            "display name",
				Description:            "description",
				Value:                  &Node{},
				CustomDomainProperties: CustomDomainProperties{},
				Location:               "location",
				Position:               Position{},
				raml:                   nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Example{
				Id:                     tt.fields.Id,
				Name:                   tt.fields.Name,
				DisplayName:            tt.fields.DisplayName,
				Description:            tt.fields.Description,
				Value:                  tt.fields.Value,
				CustomDomainProperties: tt.fields.CustomDomainProperties,
				Location:               tt.fields.Location,
				Position:               tt.fields.Position,
				raml:                   tt.fields.raml,
			}
			if got := e.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}
