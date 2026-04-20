package risk_analysis_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

func TestScanWithGitleaks_NoSecrets(t *testing.T) {
	t.Parallel()
	findings, err := risk_analysis.ScanWithGitleaks("hello world, this is a normal message")
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestScanWithGitleaks_DetectsAWSKey(t *testing.T) {
	t.Parallel()
	// Use realistic-looking keys — "EXAMPLE" is globally allowlisted by gitleaks.
	content := `Here is my AWS key: AKIAIOSFODNN7REALKEY and secret: wJalrXUtnFEMI/K7MDENG/bPxRfiCYREALKEYXX`
	findings, err := risk_analysis.ScanWithGitleaks(content)
	require.NoError(t, err)
	assert.NotEmpty(t, findings, "expected at least one finding for AWS credentials")
	for _, f := range findings {
		assert.NotEmpty(t, f.RuleID)
		assert.NotEmpty(t, f.Description)
	}
}

func TestScanWithGitleaks_DetectsGitHubToken(t *testing.T) {
	t.Parallel()
	content := `export GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab`
	findings, err := risk_analysis.ScanWithGitleaks(content)
	require.NoError(t, err)
	assert.NotEmpty(t, findings, "expected at least one finding for GitHub token")
}

func TestScanWithGitleaks_EmptyContent(t *testing.T) {
	t.Parallel()
	findings, err := risk_analysis.ScanWithGitleaks("")
	require.NoError(t, err)
	assert.Empty(t, findings)
}
