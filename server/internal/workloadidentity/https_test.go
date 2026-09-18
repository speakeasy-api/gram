package workloadidentity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

func TestParseIssuerURL_AcceptsHTTPS(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://token.actions.githubusercontent.com",
		"HTTPS://Token.Actions.GitHubUserContent.com/",
		"https://oidc.example.com:8443/tenant",
	} {
		canonical, err := workloadidentity.ParseIssuerURL(raw)
		require.NoError(t, err, raw)
		require.Equal(t, "https", canonical.Scheme(), raw)
	}
}

func TestParseIssuerURL_RefusesPlainHTTP(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://token.actions.githubusercontent.com",
		"HTTP://token.actions.githubusercontent.com",
		"http://localhost:35291/oauth2-1",
	} {
		_, err := workloadidentity.ParseIssuerURL(raw)
		require.ErrorIs(t, err, workloadidentity.ErrIssuerURLNotHTTPS, raw)
		require.ErrorIs(t, err, workloadidentity.ErrIssuerURLInvalid, raw)
	}
}

func TestParseIssuerURL_MalformedIsNotReportedAsPlainHTTP(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "not-a-url", "ftp://issuer.example.com", "https://issuer.example.com#frag"} {
		_, err := workloadidentity.ParseIssuerURL(raw)
		require.ErrorIs(t, err, workloadidentity.ErrIssuerURLInvalid, raw)
		require.NotErrorIs(t, err, workloadidentity.ErrIssuerURLNotHTTPS, raw)
	}
}

func TestValidateJWKSURI_AcceptsHTTPS(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://token.actions.githubusercontent.com/.well-known/jwks",
		"HTTPS://oidc.example.com:8443/keys",
	} {
		require.NoError(t, workloadidentity.ValidateJWKSURI(raw), raw)
	}
}

func TestValidateJWKSURI_RefusesPlainHTTP(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://token.actions.githubusercontent.com/.well-known/jwks",
		"HTTP://oidc.example.com/keys",
		"http://127.0.0.1:35291/keys",
	} {
		require.ErrorIs(t, workloadidentity.ValidateJWKSURI(raw), workloadidentity.ErrJWKSURINotHTTPS, raw)
	}
}

func TestValidateJWKSURI_MalformedIsNotReportedAsPlainHTTP(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"",
		"https://oidc.example.com/keys#frag",
		"https://user@oidc.example.com/keys",
		"https:///keys",
	} {
		err := workloadidentity.ValidateJWKSURI(raw)
		require.Error(t, err, raw)
		require.NotErrorIs(t, err, workloadidentity.ErrJWKSURINotHTTPS, raw)
	}
}
