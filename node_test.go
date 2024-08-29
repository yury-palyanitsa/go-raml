package raml

import (
	"reflect"
	"testing"
)

func TestNode_clone(t *testing.T) {
	type fields struct {
		Id       string
		Value    any
		Link     *Node
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
		want   *Node
	}{
		{
			name:   "Test Node clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Node{},
		},
		{
			name: "Test Node clone with non-nil",
			fields: fields{
				Id:       "id",
				Value:    "value",
				Link:     &Node{},
				Location: "location",
				Position: Position{},
				raml:     &RAML{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Node{
				Id:       "id",
				Value:    "value",
				Link:     &Node{},
				Location: "location",
				Position: Position{},
				raml:     nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Node{
				Id:       tt.fields.Id,
				Value:    tt.fields.Value,
				Link:     tt.fields.Link,
				Location: tt.fields.Location,
				Position: tt.fields.Position,
				raml:     tt.fields.raml,
			}
			if got := n.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNodes_clone(t *testing.T) {
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name string
		n    Nodes
		args args
		want Nodes
	}{
		{
			name: "Test Nodes clone with non-nil",
			n:    []*Node{},
			args: args{
				cloning: newCloning(nil),
			},
			want: []*Node{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}
