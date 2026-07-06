package gitleaks_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

func TestCanonicalRuleID_PrependsSecret(t *testing.T) {
	t.Parallel()

	require.Equal(t, "secret.anthropic_api_key", gitleaks.CanonicalRuleID("anthropic-api-key"))
	require.Equal(t, "secret.aws_access_token", gitleaks.CanonicalRuleID("AWS-Access-Token"))
}
