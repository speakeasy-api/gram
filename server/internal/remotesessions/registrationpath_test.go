package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestChooseRegistration(t *testing.T) {
	t.Parallel()

	const (
		https    = "https://idp.example.com/register"
		loopback = "http://localhost:8080/register"
	)
	basic := conv.PtrEmpty(string(remotesessions.TokenEndpointAuthMethodBasic))
	tests := []struct {
		name         string
		capabilities remotesessions.RegistrationCapabilities
		policy       remotesessions.RegistrationPolicy
		want         remotesessions.RegistrationPath
	}{
		{name: "cimd first prefers cimd", capabilities: remotesessions.RegistrationCapabilities{RegistrationEndpoint: https, ClientIDMetadataDocumentSupported: true}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderCIMDFirst}, want: remotesessions.RegistrationPathCIMD},
		{name: "dcr first prefers dcr", capabilities: remotesessions.RegistrationCapabilities{RegistrationEndpoint: https, ClientIDMetadataDocumentSupported: true}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCRFirst}, want: remotesessions.RegistrationPathDCR},
		{name: "dcr first falls back to cimd", capabilities: remotesessions.RegistrationCapabilities{ClientIDMetadataDocumentSupported: true}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCRFirst}, want: remotesessions.RegistrationPathCIMD},
		{name: "dcr only ignores cimd", capabilities: remotesessions.RegistrationCapabilities{ClientIDMetadataDocumentSupported: true}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCROnly}, want: remotesessions.RegistrationPathManual},
		{name: "zero order ignores cimd", capabilities: remotesessions.RegistrationCapabilities{ClientIDMetadataDocumentSupported: true}, policy: remotesessions.RegistrationPolicy{}, want: remotesessions.RegistrationPathManual},
		{name: "loopback endpoint refused by default", capabilities: remotesessions.RegistrationCapabilities{RegistrationEndpoint: loopback}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCROnly}, want: remotesessions.RegistrationPathManual},
		{name: "loopback endpoint allowed by policy", capabilities: remotesessions.RegistrationCapabilities{RegistrationEndpoint: loopback}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCROnly, AllowLoopbackRegistrationEndpoint: true}, want: remotesessions.RegistrationPathDCR},
		{name: "required secret method not accepted", capabilities: remotesessions.RegistrationCapabilities{RegistrationEndpoint: https, TokenEndpointAuthMethodsSupported: []string{"none"}}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCRFirst, RequireClientSecret: true, TokenEndpointAuthMethod: basic}, want: remotesessions.RegistrationPathManual},
		{name: "required secret method accepted", capabilities: remotesessions.RegistrationCapabilities{RegistrationEndpoint: https, TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_basic"}}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCRFirst, RequireClientSecret: true, TokenEndpointAuthMethod: basic}, want: remotesessions.RegistrationPathDCR},
		{name: "required secret method with unenumerated methods", capabilities: remotesessions.RegistrationCapabilities{RegistrationEndpoint: https}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCRFirst, RequireClientSecret: true, TokenEndpointAuthMethod: basic}, want: remotesessions.RegistrationPathDCR},
		{name: "requested method without a required secret is not checked", capabilities: remotesessions.RegistrationCapabilities{RegistrationEndpoint: https, TokenEndpointAuthMethodsSupported: []string{"none"}}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderDCRFirst, TokenEndpointAuthMethod: basic}, want: remotesessions.RegistrationPathDCR},
		{name: "cimd refusing public clients", capabilities: remotesessions.RegistrationCapabilities{TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"}, ClientIDMetadataDocumentSupported: true}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderCIMDFirst}, want: remotesessions.RegistrationPathManual},
		{name: "neither", capabilities: remotesessions.RegistrationCapabilities{}, policy: remotesessions.RegistrationPolicy{Order: remotesessions.RegistrationOrderCIMDFirst, AllowLoopbackRegistrationEndpoint: true}, want: remotesessions.RegistrationPathManual},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, remotesessions.ChooseRegistration(test.capabilities, test.policy))
		})
	}
}
