package gitleaks_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/speakeasy-api/gram/server/internal/gitleaks"
)

func TestCanonicalRuleID_PrependsSecret(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "secret.anthropic_api_key", gitleaks.CanonicalRuleID("anthropic-api-key"))
	assert.Equal(t, "secret.aws_access_token", gitleaks.CanonicalRuleID("AWS-Access-Token"))
}
