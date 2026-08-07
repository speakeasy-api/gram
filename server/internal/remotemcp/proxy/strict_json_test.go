package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateStrictJSONRPCBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "clean tools call",
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"a","arguments":{"x":1}}}`,
			wantErr: "",
		},
		{
			name:    "duplicate top-level method",
			body:    `{"jsonrpc":"2.0","id":1,"method":"ping","method":"tools/call"}`,
			wantErr: `duplicate object member "method"`,
		},
		{
			name:    "duplicate nested params name",
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","name":"forbidden"}}`,
			wantErr: `duplicate object member "name"`,
		},
		{
			name:    "duplicate inside array element",
			body:    `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"items":[{"k":1,"k":2}]}}`,
			wantErr: `duplicate object member "k"`,
		},
		{
			name:    "trailing second message",
			body:    `{"jsonrpc":"2.0","id":1,"method":"ping"}{"jsonrpc":"2.0","id":2,"method":"tools/call"}`,
			wantErr: "trailing data",
		},
		{
			name:    "empty body",
			body:    "",
			wantErr: "empty JSON-RPC message body",
		},
		{
			name:    "whitespace only",
			body:    " \n\t",
			wantErr: "empty JSON-RPC message body",
		},
		{
			name:    "repeated key in distinct sibling objects is fine",
			body:    `{"a":{"k":1},"b":{"k":2}}`,
			wantErr: "",
		},
		{
			name:    "malformed JSON",
			body:    `{"jsonrpc":`,
			wantErr: "read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateStrictJSONRPCBody([]byte(tt.body))
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
