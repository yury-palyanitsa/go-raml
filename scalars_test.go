package raml

import (
	"math/big"
	"reflect"
	"regexp"
	"testing"
)

func TestEnumFacets_clone(t *testing.T) {
	type fields struct {
		Enum Nodes
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *EnumFacets
	}{
		{
			name: "Test EnumFacets clone with nil",
			fields: fields{
				Enum: nil,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &EnumFacets{
				Enum: nil,
			},
		},
		{
			name: "Test EnumFacets clone with non-nil",
			fields: fields{
				Enum: Nodes{
					&Node{},
				},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &EnumFacets{
				Enum: Nodes{
					&Node{},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &EnumFacets{
				Enum: tt.fields.Enum,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileFacets_clone(t *testing.T) {
	type fields struct {
		FileTypes Nodes
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *FileFacets
	}{
		{
			name: "Test FileFacets clone with nil",
			fields: fields{
				FileTypes: nil,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &FileFacets{
				FileTypes: nil,
			},
		},
		{
			name: "Test FileFacets clone with non-nil",
			fields: fields{
				FileTypes: Nodes{
					&Node{},
				},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &FileFacets{
				FileTypes: Nodes{
					&Node{},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &FileFacets{
				FileTypes: tt.fields.FileTypes,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatFacets_clone(t *testing.T) {
	type fields struct {
		Format *string
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *FormatFacets
	}{
		{
			name: "Test FormatFacets clone with nil",
			fields: fields{
				Format: nil,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &FormatFacets{
				Format: nil,
			},
		},
		{
			name: "Test FormatFacets clone with non-nil",
			fields: fields{
				Format: func() *string { s := "format"; return &s }(),
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &FormatFacets{
				Format: func() *string { s := "format"; return &s }(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &FormatFacets{
				Format: tt.fields.Format,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIntegerFacets_clone(t *testing.T) {
	type fields struct {
		Minimum    *big.Int
		Maximum    *big.Int
		MultipleOf *int64
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *IntegerFacets
	}{
		{
			name: "Test IntegerFacets clone with nil",
			fields: fields{
				Minimum:    nil,
				Maximum:    nil,
				MultipleOf: nil,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &IntegerFacets{
				Minimum:    nil,
				Maximum:    nil,
				MultipleOf: nil,
			},
		},
		{
			name: "Test IntegerFacets clone with non-nil",
			fields: fields{
				Minimum:    big.NewInt(1),
				Maximum:    big.NewInt(2),
				MultipleOf: func() *int64 { i := int64(3); return &i }(),
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &IntegerFacets{
				Minimum:    big.NewInt(1),
				Maximum:    big.NewInt(2),
				MultipleOf: func() *int64 { i := int64(3); return &i }(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &IntegerFacets{
				Minimum:    tt.fields.Minimum,
				Maximum:    tt.fields.Maximum,
				MultipleOf: tt.fields.MultipleOf,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLengthFacets_clone(t *testing.T) {
	type fields struct {
		MaxLength *uint64
		MinLength *uint64
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *LengthFacets
	}{
		{
			name: "Test LengthFacets clone with nil",
			fields: fields{
				MaxLength: nil,
				MinLength: nil,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &LengthFacets{
				MaxLength: nil,
				MinLength: nil,
			},
		},
		{
			name: "Test LengthFacets clone with non-nil",
			fields: fields{
				MaxLength: func() *uint64 { v := uint64(1); return &v }(),
				MinLength: func() *uint64 { v := uint64(10); return &v }(),
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &LengthFacets{
				MaxLength: func() *uint64 { v := uint64(1); return &v }(),
				MinLength: func() *uint64 { v := uint64(10); return &v }(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &LengthFacets{
				MaxLength: tt.fields.MaxLength,
				MinLength: tt.fields.MinLength,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNumberFacets_clone(t *testing.T) {
	type fields struct {
		Minimum    *float64
		Maximum    *float64
		MultipleOf *float64
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *NumberFacets
	}{
		{
			name: "Test NumberFacets clone with nil",
			fields: fields{
				Minimum:    nil,
				Maximum:    nil,
				MultipleOf: nil,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &NumberFacets{
				Minimum:    nil,
				Maximum:    nil,
				MultipleOf: nil,
			},
		},
		{
			name: "Test NumberFacets clone with non-nil",
			fields: fields{
				Minimum:    func() *float64 { v := 1.0; return &v }(),
				Maximum:    func() *float64 { v := 10.0; return &v }(),
				MultipleOf: func() *float64 { v := 3.0; return &v }(),
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &NumberFacets{
				Minimum:    func() *float64 { v := 1.0; return &v }(),
				Maximum:    func() *float64 { v := 10.0; return &v }(),
				MultipleOf: func() *float64 { v := 3.0; return &v }(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &NumberFacets{
				Minimum:    tt.fields.Minimum,
				Maximum:    tt.fields.Maximum,
				MultipleOf: tt.fields.MultipleOf,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringFacets_clone(t *testing.T) {
	type fields struct {
		LengthFacets LengthFacets
		Pattern      *regexp.Regexp
	}
	type args struct {
		cloning *_cloning
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *StringFacets
	}{
		{
			name: "Test StringFacets clone with nil",
			fields: fields{
				LengthFacets: LengthFacets{},
				Pattern:      nil,
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &StringFacets{
				LengthFacets: LengthFacets{},
				Pattern:      nil,
			},
		},
		{
			name: "Test StringFacets clone with non-nil",
			fields: fields{
				LengthFacets: LengthFacets{
					MaxLength: func() *uint64 { v := uint64(1); return &v }(),
					MinLength: func() *uint64 { v := uint64(10); return &v }(),
				},
				Pattern: regexp.MustCompile("pattern"),
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &StringFacets{
				LengthFacets: LengthFacets{
					MaxLength: func() *uint64 { v := uint64(1); return &v }(),
					MinLength: func() *uint64 { v := uint64(10); return &v }(),
				},
				Pattern: regexp.MustCompile("pattern"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &StringFacets{
				LengthFacets: tt.fields.LengthFacets,
				Pattern:      tt.fields.Pattern,
			}
			if got := f.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIntegerShape_clone(t *testing.T) {
	type fields struct {
		BaseShape     BaseShape
		EnumFacets    EnumFacets
		FormatFacets  FormatFacets
		IntegerFacets IntegerFacets
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
			name: "Test IntegerShape clone with nil",
			fields: fields{
				BaseShape:     BaseShape{},
				EnumFacets:    EnumFacets{},
				FormatFacets:  FormatFacets{},
				IntegerFacets: IntegerFacets{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &IntegerShape{
				BaseShape:     BaseShape{},
				EnumFacets:    EnumFacets{},
				FormatFacets:  FormatFacets{},
				IntegerFacets: IntegerFacets{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &IntegerShape{
				BaseShape:     tt.fields.BaseShape,
				EnumFacets:    tt.fields.EnumFacets,
				FormatFacets:  tt.fields.FormatFacets,
				IntegerFacets: tt.fields.IntegerFacets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNumberShape_clone(t *testing.T) {
	type fields struct {
		BaseShape    BaseShape
		EnumFacets   EnumFacets
		FormatFacets FormatFacets
		NumberFacets NumberFacets
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
			name: "Test NumberShape clone with nil",
			fields: fields{
				BaseShape:    BaseShape{},
				EnumFacets:   EnumFacets{},
				FormatFacets: FormatFacets{},
				NumberFacets: NumberFacets{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &NumberShape{
				BaseShape:    BaseShape{},
				EnumFacets:   EnumFacets{},
				FormatFacets: FormatFacets{},
				NumberFacets: NumberFacets{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &NumberShape{
				BaseShape:    tt.fields.BaseShape,
				EnumFacets:   tt.fields.EnumFacets,
				FormatFacets: tt.fields.FormatFacets,
				NumberFacets: tt.fields.NumberFacets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringShape_clone(t *testing.T) {
	type fields struct {
		BaseShape    BaseShape
		EnumFacets   EnumFacets
		StringFacets StringFacets
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
			name: "Test StringShape clone with nil",
			fields: fields{
				BaseShape:    BaseShape{},
				EnumFacets:   EnumFacets{},
				StringFacets: StringFacets{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &StringShape{
				BaseShape:    BaseShape{},
				EnumFacets:   EnumFacets{},
				StringFacets: StringFacets{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &StringShape{
				BaseShape:    tt.fields.BaseShape,
				EnumFacets:   tt.fields.EnumFacets,
				StringFacets: tt.fields.StringFacets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileShape_clone(t *testing.T) {
	type fields struct {
		BaseShape    BaseShape
		LengthFacets LengthFacets
		FileFacets   FileFacets
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
			name: "Test FileShape clone with nil",
			fields: fields{
				BaseShape:    BaseShape{},
				LengthFacets: LengthFacets{},
				FileFacets:   FileFacets{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &FileShape{
				BaseShape:    BaseShape{},
				LengthFacets: LengthFacets{},
				FileFacets:   FileFacets{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &FileShape{
				BaseShape:    tt.fields.BaseShape,
				LengthFacets: tt.fields.LengthFacets,
				FileFacets:   tt.fields.FileFacets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBooleanShape_clone(t *testing.T) {
	type fields struct {
		BaseShape  BaseShape
		EnumFacets EnumFacets
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
			name: "Test BooleanShape clone with nil",
			fields: fields{
				BaseShape:  BaseShape{},
				EnumFacets: EnumFacets{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &BooleanShape{
				BaseShape:  BaseShape{},
				EnumFacets: EnumFacets{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &BooleanShape{
				BaseShape:  tt.fields.BaseShape,
				EnumFacets: tt.fields.EnumFacets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDateTimeShape_clone(t *testing.T) {
	type fields struct {
		BaseShape    BaseShape
		FormatFacets FormatFacets
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
			name: "Test DateTimeShape clone with nil",
			fields: fields{
				BaseShape:    BaseShape{},
				FormatFacets: FormatFacets{},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &DateTimeShape{
				BaseShape:    BaseShape{},
				FormatFacets: FormatFacets{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &DateTimeShape{
				BaseShape:    tt.fields.BaseShape,
				FormatFacets: tt.fields.FormatFacets,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDateTimeOnlyShape_clone(t *testing.T) {
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
			name: "Test DateTimeOnlyShape clone with nil",
			fields: fields{
				BaseShape: BaseShape{raml: &RAML{}},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &DateTimeOnlyShape{
				BaseShape: BaseShape{raml: nil},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &DateTimeOnlyShape{
				BaseShape: tt.fields.BaseShape,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDateOnlyShape_clone(t *testing.T) {
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
			name: "Test DateOnlyShape clone with nil",
			fields: fields{
				BaseShape: BaseShape{raml: &RAML{}},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &DateOnlyShape{
				BaseShape: BaseShape{raml: nil},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &DateOnlyShape{
				BaseShape: tt.fields.BaseShape,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimeOnlyShape_clone(t *testing.T) {
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
			name: "Test TimeOnlyShape clone with nil",
			fields: fields{
				BaseShape: BaseShape{raml: &RAML{}},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &TimeOnlyShape{
				BaseShape: BaseShape{raml: nil},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &TimeOnlyShape{
				BaseShape: tt.fields.BaseShape,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnyShape_clone(t *testing.T) {
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
			name: "Test AnyShape clone with nil",
			fields: fields{
				BaseShape: BaseShape{raml: &RAML{}},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &AnyShape{
				BaseShape: BaseShape{raml: nil},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &AnyShape{
				BaseShape: tt.fields.BaseShape,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNilShape_clone(t *testing.T) {
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
			name: "Test NilShape clone with nil",
			fields: fields{
				BaseShape: BaseShape{raml: &RAML{}},
			},
			args: args{
				cloning: newCloning(nil),
			},
			want: &NilShape{
				BaseShape: BaseShape{raml: nil},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &NilShape{
				BaseShape: tt.fields.BaseShape,
			}
			if got := s.clone(tt.args.cloning); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clone() = %v, want %v", got, tt.want)
			}
		})
	}
}
