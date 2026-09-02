package remotesessions

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The snapshot exists so the well-known surface can re-serve fields the typed
// columns do not model, so anything unrecognised must survive verbatim. What it
// must not do is republish values refreshIssuerMetadata deliberately drops,
// which would make the snapshot laxer than the columns beside it.
func TestSanitizeDiscoverySnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    map[string]any
		wantErr string
	}{
		{
			name: "preserves fields the typed columns do not model",
			raw: `{
				"issuer": "https://idp.example.com",
				"authorization_endpoint": "https://idp.example.com/authorize",
				"token_endpoint": "https://idp.example.com/token",
				"userinfo_endpoint": "https://idp.example.com/userinfo",
				"introspection_endpoint": "https://idp.example.com/introspect",
				"claims_supported": ["sub", "email"],
				"subject_types_supported": ["public"],
				"id_token_signing_alg_values_supported": ["RS256"]
			}`,
			want: map[string]any{
				"issuer":                                "https://idp.example.com",
				"authorization_endpoint":                "https://idp.example.com/authorize",
				"token_endpoint":                        "https://idp.example.com/token",
				"userinfo_endpoint":                     "https://idp.example.com/userinfo",
				"introspection_endpoint":                "https://idp.example.com/introspect",
				"claims_supported":                      []any{"sub", "email"},
				"subject_types_supported":               []any{"public"},
				"id_token_signing_alg_values_supported": []any{"RS256"},
			},
			wantErr: "",
		},
		{
			name: "drops a plaintext revocation endpoint",
			raw: `{
				"issuer": "https://idp.example.com",
				"revocation_endpoint": "http://idp.example.com/revoke"
			}`,
			want:    map[string]any{"issuer": "https://idp.example.com"},
			wantErr: "",
		},
		{
			name: "keeps an https revocation endpoint",
			raw: `{
				"issuer": "https://idp.example.com",
				"revocation_endpoint": "https://idp.example.com/revoke"
			}`,
			want: map[string]any{
				"issuer":              "https://idp.example.com",
				"revocation_endpoint": "https://idp.example.com/revoke",
			},
			wantErr: "",
		},
		{
			name: "keeps a loopback revocation endpoint, matching the typed column rule",
			raw: `{
				"issuer": "https://idp.example.com",
				"revocation_endpoint": "http://127.0.0.1:8080/revoke"
			}`,
			want: map[string]any{
				"issuer":              "https://idp.example.com",
				"revocation_endpoint": "http://127.0.0.1:8080/revoke",
			},
			wantErr: "",
		},
		{
			name: "drops documentation URLs that are not absolute http(s)",
			raw: `{
				"issuer": "https://idp.example.com",
				"service_documentation": "javascript:alert(1)",
				"op_policy_uri": "/relative/policy",
				"op_tos_uri": "https://idp.example.com/tos"
			}`,
			want: map[string]any{
				"issuer":     "https://idp.example.com",
				"op_tos_uri": "https://idp.example.com/tos",
			},
			wantErr: "",
		},
		{
			// encoding/json matches struct fields case-insensitively, so
			// attemptIssuerProbe validated this member and let it through;
			// filtering only the canonical spelling then republished the exact
			// value validation had rejected, over plaintext, from Gram's origin.
			name: "drops a filtered field under any capitalization",
			raw: `{
				"issuer": "https://idp.example.com",
				"REVOCATION_ENDPOINT": "http://idp.example.com/revoke",
				"Service_Documentation": "javascript:alert(1)"
			}`,
			want:    map[string]any{"issuer": "https://idp.example.com"},
			wantErr: "",
		},
		{
			name: "keeps a safe value under a non-canonical capitalization",
			raw: `{
				"issuer": "https://idp.example.com",
				"Revocation_Endpoint": "https://idp.example.com/revoke"
			}`,
			want: map[string]any{
				"issuer":              "https://idp.example.com",
				"Revocation_Endpoint": "https://idp.example.com/revoke",
			},
			wantErr: "",
		},
		{
			// A field whose type is wrong is a document Gram cannot reason
			// about, so it is dropped rather than re-served to clients.
			name: "drops a filtered field carrying a non-string value",
			raw: `{
				"issuer": "https://idp.example.com",
				"revocation_endpoint": {"url": "https://idp.example.com/revoke"}
			}`,
			want:    map[string]any{"issuer": "https://idp.example.com"},
			wantErr: "",
		},
		{
			name:    "an empty body yields no snapshot rather than an error",
			raw:     "",
			want:    nil,
			wantErr: "",
		},
		{
			name:    "a JSON null is not an object",
			raw:     "null",
			want:    nil,
			wantErr: "expected a JSON object",
		},
		{
			name:    "a JSON array is not an object",
			raw:     `["not", "an", "object"]`,
			want:    nil,
			wantErr: "decode discovery document",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := sanitizeDiscoverySnapshot([]byte(tt.raw))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tt.want == nil {
				require.Nil(t, got)
				return
			}

			decoded := map[string]any{}
			require.NoError(t, json.Unmarshal(got, &decoded))
			require.Equal(t, tt.want, decoded)
		})
	}
}
