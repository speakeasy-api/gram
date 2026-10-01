package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestMCPFindingEvidenceStoreEncryptsAndExpiresMatches(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	now := time.Now().UTC()
	findingID := uuid.New()
	match := "raw MCP credential"
	require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		CreatedAt:      now,
		Findings:       []risk.MCPFindingEvidence{{ID: findingID, Match: match}},
	}))

	ciphertext, err := riskrepo.New(ti.conn).GetMCPFindingEvidence(ctx, riskrepo.GetMCPFindingEvidenceParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		FindingID:      findingID,
		Now:            conv.ToPGTimestamptz(now),
	})
	require.NoError(t, err)
	require.NotEqual(t, match, ciphertext)
	require.NotContains(t, ciphertext, match)

	revealed, err := ti.findingEvidence.Reveal(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, findingID, now)
	require.NoError(t, err)
	require.Equal(t, match, revealed)

	expiredID := uuid.New()
	expiredAt := now.Add(-91 * 24 * time.Hour)
	require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		CreatedAt:      expiredAt,
		Findings:       []risk.MCPFindingEvidence{{ID: expiredID, Match: "expired MCP credential"}},
	}))

	_, err = ti.findingEvidence.Reveal(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, expiredID, now)
	require.ErrorIs(t, err, risk.ErrMCPFindingEvidenceNotStored)

	cleaned, err := riskrepo.New(ti.conn).CleanupExpiredMCPFindingEvidenceBatch(ctx, 500)
	require.NoError(t, err)
	require.Equal(t, int64(1), cleaned)
	cleaned, err = riskrepo.New(ti.conn).CleanupExpiredMCPFindingEvidenceBatch(ctx, 500)
	require.NoError(t, err)
	require.Zero(t, cleaned)
}
