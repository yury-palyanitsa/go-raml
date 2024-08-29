package raml

import (
	"reflect"
	"testing"
)

func TestLibraryLink_clone(t *testing.T) {
	type fields struct {
		Id       string
		Value    string
		Link     *Library
		Location string
		Position Position
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *LibraryLink
	}{
		{
			name:   "Test LibraryLink clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &LibraryLink{},
		},
		{
			name: "Test LibraryLink clone with non-nil",
			fields: fields{
				Id:       "id",
				Value:    "value",
				Link:     &Library{},
				Location: "location",
				Position: Position{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &LibraryLink{
				Id:       "id",
				Value:    "value",
				Link:     &Library{},
				Location: "location",
				Position: Position{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &LibraryLink{
				Id:       tt.fields.Id,
				Value:    tt.fields.Value,
				Link:     tt.fields.Link,
				Location: tt.fields.Location,
				Position: tt.fields.Position,
			}
			if got := l.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLibrary_clone(t *testing.T) {
	type fields struct {
		Id                     string
		Usage                  string
		AnnotationTypes        map[string]*Shape
		Types                  map[string]*Shape
		Uses                   map[string]*LibraryLink
		CustomDomainProperties CustomDomainProperties
		Location               string
		raml                   *RAML
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Fragment
	}{
		{
			name:   "Test Library clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Library{},
		},
		{
			name: "Test Library clone with non-nil",
			fields: fields{
				Id:                     "id",
				Usage:                  "usage",
				AnnotationTypes:        map[string]*Shape{},
				Types:                  map[string]*Shape{},
				Uses:                   map[string]*LibraryLink{},
				CustomDomainProperties: CustomDomainProperties{},
				Location:               "location",
				raml:                   &RAML{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &Library{
				Id:                     "id",
				Usage:                  "usage",
				AnnotationTypes:        map[string]*Shape{},
				Types:                  map[string]*Shape{},
				Uses:                   map[string]*LibraryLink{},
				CustomDomainProperties: CustomDomainProperties{},
				Location:               "location",
				raml:                   nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &Library{
				Id:                     tt.fields.Id,
				Usage:                  tt.fields.Usage,
				AnnotationTypes:        tt.fields.AnnotationTypes,
				Types:                  tt.fields.Types,
				Uses:                   tt.fields.Uses,
				CustomDomainProperties: tt.fields.CustomDomainProperties,
				Location:               tt.fields.Location,
				raml:                   tt.fields.raml,
			}
			if got := l.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDataType_clone(t *testing.T) {
	type fields struct {
		Id       string
		Usage    string
		Uses     map[string]*LibraryLink
		Shape    *Shape
		Location string
		raml     *RAML
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Fragment
	}{
		{
			name:   "Test DataType clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &DataType{},
		},
		{
			name: "Test DataType clone with non-nil",
			fields: fields{
				Id:       "id",
				Usage:    "usage",
				Uses:     map[string]*LibraryLink{},
				Shape:    func() *Shape { var s Shape; s = &IntegerShape{}; return &s }(),
				Location: "location",
				raml:     &RAML{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &DataType{
				Id:       "id",
				Usage:    "usage",
				Uses:     map[string]*LibraryLink{},
				Shape:    func() *Shape { var s Shape; s = &IntegerShape{}; return &s }(),
				Location: "location",
				raml:     nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt := &DataType{
				Id:       tt.fields.Id,
				Usage:    tt.fields.Usage,
				Uses:     tt.fields.Uses,
				Shape:    tt.fields.Shape,
				Location: tt.fields.Location,
				raml:     tt.fields.raml,
			}
			if got := dt.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNamedExample_clone(t *testing.T) {
	type fields struct {
		Id       string
		Examples map[string]*Example
		Location string
		raml     *RAML
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   Fragment
	}{
		{
			name:   "Test NamedExample clone with nil",
			fields: fields{},
			args: args{
				cloning: newCloning(nil),
			},
			want: &NamedExample{},
		},
		{
			name: "Test NamedExample clone with non-nil",
			fields: fields{
				Id:       "id",
				Examples: map[string]*Example{},
				Location: "location",
				raml:     &RAML{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &NamedExample{
				Id:       "id",
				Examples: map[string]*Example{},
				Location: "location",
				raml:     nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ne := &NamedExample{
				Id:       tt.fields.Id,
				Examples: tt.fields.Examples,
				Location: tt.fields.Location,
				raml:     tt.fields.raml,
			}
			if got := ne.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}
