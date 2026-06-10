package raml

import (
	"testing"

	"github.com/acronis/go-stacktrace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractURITemplateParamNames(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      []uriTemplateExpression
		wantErr   string
		wantCol   int // 1-based column of the offending byte (uriPos.Column is 1)
	}{
		// Level 1: simple expansion
		{
			name: "simple variable",
			uri:  "{var}",
			want: []uriTemplateExpression{{Operator: "", Name: "var"}},
		},
		{
			name: "simple path segment",
			uri:  "/users/{userId}",
			want: []uriTemplateExpression{{Operator: "", Name: "userId"}},
		},
		{
			name: "multiple simple variables",
			uri:  "/users/{userId}/posts/{postId}",
			want: []uriTemplateExpression{
				{Operator: "", Name: "userId"},
				{Operator: "", Name: "postId"},
			},
		},
		{
			name: "no template variables",
			uri:  "/users/me/profile",
			want: nil,
		},

		// Level 2: reserved and fragment expansion
		{
			name: "reserved expansion (+)",
			uri:  "{+var}",
			want: []uriTemplateExpression{{Operator: "+", Name: "var"}},
		},
		{
			name: "reserved expansion in path",
			uri:  "https://api.example.com/{+version}",
			want: []uriTemplateExpression{{Operator: "+", Name: "version"}},
		},
		{
			name: "fragment expansion (#)",
			uri:  "X{#var}",
			want: []uriTemplateExpression{{Operator: "#", Name: "var"}},
		},

		// Error cases — message no longer carries position/URI; that information
		// lives on the stacktrace Position (Column anchored at uriPos.Column + offset).
		{
			name:    "unclosed brace",
			uri:     "/users/{userId",
			wantErr: "unclosed '{'",
			wantCol: 1 + 7, // '{' at byte offset 7
		},
		{
			name:    "unexpected closing brace",
			uri:     "/users/}userId",
			wantErr: "unexpected '}'",
			wantCol: 1 + 7,
		},
		{
			name:    "nested opening brace",
			uri:     "/users/{{userId}",
			wantErr: "nested '{'",
			wantCol: 1 + 8,
		},
		{
			name:    "empty expression",
			uri:     "/users/{}",
			wantErr: "empty expression",
			wantCol: 1 + 7,
		},
		{
			name:    "operator only no name",
			uri:     "/users/{+}",
			wantErr: "empty expression",
			wantCol: 1 + 7,
		},
		{
			name:    "space in variable name",
			uri:     "/users/{ userId }",
			wantErr: `invalid character ' ' in variable name`,
			wantCol: 1 + 8, // first ' ' inside the expression
		},
		{
			name:    "invalid character in variable name",
			uri:     "/users/{my!var}",
			wantErr: `invalid character '!' in variable name`,
			wantCol: 1 + 10, // '!' at offset 10
		},
		{
			name:    "dot at start of variable name",
			uri:     "/{.var}",
			wantErr: `invalid '.' in variable name`,
			wantCol: 1 + 2, // '.' at offset 2
		},
		{
			name:    "invalid pct-encoded sequence",
			uri:     "/{my%ZZvar}",
			wantErr: `invalid pct-encoded sequence in variable name`,
			wantCol: 1 + 4, // '%' at offset 4
		},
		// Valid pct-encoded name
		{
			name: "valid pct-encoded variable name",
			uri:  "/{my%20var}",
			want: []uriTemplateExpression{{Operator: "", Name: "my%20var"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uriPos := stacktrace.Position{Line: 1, Column: 1}
			got, st := extractURITemplateParamNames(tt.uri, "test.raml", uriPos)
			if tt.wantErr != "" {
				require.NotNil(t, st)
				assert.Equal(t, tt.wantErr, st.Message)
				assert.Equal(t, tt.wantCol, st.Position.Column,
					"position column must point at the offending byte")
				return
			}
			require.Nil(t, st)
			assert.Equal(t, tt.want, got)
		})
	}
}
