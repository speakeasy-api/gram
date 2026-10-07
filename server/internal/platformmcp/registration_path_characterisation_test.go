package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// TestRegistrationPathCharacterisation pins which registration path each
// Platform MCP caller takes for each kind of provider: identity-provider
// attachment (discovery, reuse of a stored provider, and the path its plan
// then commits to) and the inspector's report. The dashboard's decisions for
// the same providers are pinned in the remotesessions package.
func TestRegistrationPathCharacterisation(t *testing.T) {
	t.Parallel()

	const (
		httpsEndpoint    = "https://idp.example.com/register"
		loopbackEndpoint = "http://127.0.0.1:8080/register"
	)
	tests := []struct {
		name          string
		endpoint      string
		methods       []string
		cimd          bool
		wantAttach    remotesessions.RegistrationPath
		wantInspector string
	}{
		{name: "dcr https with client_secret_basic", endpoint: httpsEndpoint, methods: []string{"client_secret_basic"}, wantAttach: remotesessions.RegistrationPathDCR, wantInspector: oauthDiscoveryAvailableDCR},
		{name: "dcr https with unenumerated methods", endpoint: httpsEndpoint, wantAttach: remotesessions.RegistrationPathDCR, wantInspector: oauthDiscoveryAvailableDCR},
		{name: "dcr https public clients only", endpoint: httpsEndpoint, methods: []string{"none"}, wantAttach: remotesessions.RegistrationPathManual, wantInspector: oauthDiscoveryAvailableDCR},
		{name: "dcr https client_secret_post only", endpoint: httpsEndpoint, methods: []string{"client_secret_post"}, wantAttach: remotesessions.RegistrationPathManual, wantInspector: oauthDiscoveryAvailableDCR},
		{name: "dcr loopback", endpoint: loopbackEndpoint, wantAttach: remotesessions.RegistrationPathManual, wantInspector: ""},
		{name: "cimd only", methods: []string{"none"}, cimd: true, wantAttach: remotesessions.RegistrationPathCIMD, wantInspector: oauthDiscoveryAvailableCIMD},
		{name: "cimd with unenumerated methods", cimd: true, wantAttach: remotesessions.RegistrationPathCIMD, wantInspector: oauthDiscoveryAvailableCIMD},
		{name: "cimd refusing public clients", methods: []string{"client_secret_basic"}, cimd: true, wantAttach: remotesessions.RegistrationPathManual, wantInspector: ""},
		{name: "dcr and cimd", endpoint: httpsEndpoint, methods: []string{"client_secret_basic", "none"}, cimd: true, wantAttach: remotesessions.RegistrationPathDCR, wantInspector: oauthDiscoveryAvailableDCR},
		{name: "dcr public clients only and cimd", endpoint: httpsEndpoint, methods: []string{"none"}, cimd: true, wantAttach: remotesessions.RegistrationPathCIMD, wantInspector: oauthDiscoveryAvailableCIMD},
		{name: "dcr loopback and cimd", endpoint: loopbackEndpoint, cimd: true, wantAttach: remotesessions.RegistrationPathCIMD, wantInspector: oauthDiscoveryAvailableCIMD},
		{name: "plain http dcr endpoint", endpoint: "http://idp.example.com/register", wantAttach: remotesessions.RegistrationPathManual, wantInspector: ""},
		{name: "relative dcr endpoint", endpoint: "/register", wantAttach: remotesessions.RegistrationPathManual, wantInspector: ""},
		{name: "neither", methods: []string{"none"}, wantAttach: remotesessions.RegistrationPathManual, wantInspector: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := map[string]any{"client_id_metadata_document_supported": test.cimd}
			if test.endpoint != "" {
				document["registration_endpoint"] = test.endpoint
			}
			if test.methods != nil {
				document["token_endpoint_auth_methods_supported"] = test.methods
			}
			payload, err := json.Marshal(document)
			require.NoError(t, err)

			require.Equal(t, test.wantAttach, attachmentRegistrationPath(test.endpoint, test.cimd, test.methods), "attachment")
			require.Equal(t, test.wantInspector, inspectorRegistration(t, payload), "inspector")
		})
	}
}

// attachmentRegistrationPath is the path identity-provider attachment takes:
// manual when discovery skips the provider (and reuse refuses it), otherwise
// the path its plan commits to.
func attachmentRegistrationPath(endpoint string, cimd bool, methods []string) remotesessions.RegistrationPath {
	switch {
	case !supportsAutomaticClientRegistration(endpoint, cimd, methods):
		return remotesessions.RegistrationPathManual
	case attachmentCanUseDynamicRegistration(endpoint, methods):
		return remotesessions.RegistrationPathDCR
	default:
		return remotesessions.RegistrationPathCIMD
	}
}

// inspectorRegistration is the oauth_discovery value the inspector reports
// for one authorization server metadata document.
func inspectorRegistration(t *testing.T, payload []byte) string {
	t.Helper()
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(payload, &metadata))
	return directRemoteAutomaticRegistration(metadata)
}
