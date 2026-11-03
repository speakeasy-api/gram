package remotesessionclients_test

import (
	"strings"
	"testing"

	adminhttp "github.com/speakeasy-api/gram/server/gen/http/admin_remote_sessions/server"
	orghttp "github.com/speakeasy-api/gram/server/gen/http/organization_remote_session_clients/server"
	projecthttp "github.com/speakeasy-api/gram/server/gen/http/remote_session_clients/server"
)

// Exercise the generated HTTP boundary, not just the persistence layer.
func TestClientAudienceHTTPValidation(t *testing.T) {
	t.Parallel()

	issuerID := "00000000-0000-4000-8000-000000000001"
	clientID := "test-client"
	validators := []struct {
		name     string
		update   bool
		validate func(*string) error
	}{
		{"project update", true, func(a *string) error {
			return projecthttp.ValidateUpdateRemoteSessionClientRequestBody(&projecthttp.UpdateRemoteSessionClientRequestBody{ID: &issuerID, Audience: a})
		}},
		{"project create", false, func(a *string) error {
			return projecthttp.ValidateCreateRemoteSessionClientRequestBody(&projecthttp.CreateRemoteSessionClientRequestBody{Audience: a, RemoteSessionIssuerID: &issuerID, ClientID: &clientID})
		}},
		{"organization update", true, func(a *string) error {
			return orghttp.ValidateUpdateClientRequestBody(&orghttp.UpdateClientRequestBody{ID: &issuerID, Audience: a})
		}},
		{"organization create", false, func(a *string) error {
			return orghttp.ValidateCreateClientRequestBody(&orghttp.CreateClientRequestBody{Audience: a, RemoteSessionIssuerID: &issuerID, ClientID: &clientID})
		}},
		{"admin update", true, func(a *string) error {
			return adminhttp.ValidateUpdateGlobalClientRequestBody(&adminhttp.UpdateGlobalClientRequestBody{ID: &issuerID, Audience: a})
		}},
		{"admin create", false, func(a *string) error {
			return adminhttp.ValidateCreateGlobalClientRequestBody(&adminhttp.CreateGlobalClientRequestBody{Audience: a, RemoteSessionIssuerID: &issuerID, ClientID: &clientID})
		}},
	}
	for _, v := range validators {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()

			if err := v.validate(nil); err != nil {
				t.Fatalf("omitted audience: %v", err)
			}
			for _, tc := range []struct {
				name, value string
				valid       bool
			}{
				{"clear", "", v.update},
				{"url", "https://api.example.test", true},
				{"max length", strings.Repeat("a", 512), true},
				{"too long", strings.Repeat("a", 513), false},
				{"whitespace", "  ", false},
				{"embedded space", "api audience", false},
				{"control", "api\n", false},
				{"non ASCII", "audienceé", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					err := v.validate(&tc.value)
					if (err == nil) != tc.valid {
						t.Fatalf("valid = %v; error = %v", tc.valid, err)
					}
				})
			}
		})
	}
}
