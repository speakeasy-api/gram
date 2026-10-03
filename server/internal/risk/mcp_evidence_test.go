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

func TestMCPFindingEvidenceStoreEncryptsAndExpiresExecutionPayloads(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	orgID := authCtx.ActiveOrganizationID
	projectID := *authCtx.ProjectID

	now := time.Now().UTC()
	executionID := uuid.NewString()
	payload := `{"query":"raw MCP credential"}`
	require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
		OrganizationID: orgID,
		ProjectID:      projectID,
		CreatedAt:      now,
		Findings:       []risk.MCPFindingEvidence{{ID: uuid.New(), Match: "raw MCP credential"}},
		Execution:      &risk.MCPExecutionPayload{ExecutionID: executionID, Phase: "request", Payload: payload},
	}))
	// A second policy's findings on the same phase keep the first payload.
	require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
		OrganizationID: orgID,
		ProjectID:      projectID,
		CreatedAt:      now.Add(time.Minute),
		Findings:       []risk.MCPFindingEvidence{{ID: uuid.New(), Match: "other"}},
		Execution:      &risk.MCPExecutionPayload{ExecutionID: executionID, Phase: "request", Payload: "replacement"},
	}))

	row, err := riskrepo.New(ti.conn).GetMCPExecutionEvidence(ctx, riskrepo.GetMCPExecutionEvidenceParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		ExecutionID:    executionID,
		Phase:          "request",
		Now:            conv.ToPGTimestamptz(now),
	})
	require.NoError(t, err)
	require.NotContains(t, row.PayloadEncrypted, "raw MCP credential")

	revealed, err := ti.findingEvidence.RevealExecutionPayload(ctx, orgID, projectID, executionID, "request", now)
	require.NoError(t, err)
	require.Equal(t, payload, revealed.Payload)
	require.WithinDuration(t, now.Add(90*24*time.Hour), revealed.ExpiresAt, time.Millisecond)

	_, err = ti.findingEvidence.RevealExecutionPayload(ctx, orgID, projectID, executionID, "response", now)
	require.ErrorIs(t, err, risk.ErrMCPFindingEvidenceNotStored)

	expiredExecutionID := uuid.NewString()
	require.NoError(t, ti.findingEvidence.Store(ctx, risk.MCPFindingEvidenceBatch{
		OrganizationID: orgID,
		ProjectID:      projectID,
		CreatedAt:      now.Add(-91 * 24 * time.Hour),
		Findings:       []risk.MCPFindingEvidence{{ID: uuid.New(), Match: "expired"}},
		Execution:      &risk.MCPExecutionPayload{ExecutionID: expiredExecutionID, Phase: "response", Payload: "expired payload"},
	}))
	_, err = ti.findingEvidence.RevealExecutionPayload(ctx, orgID, projectID, expiredExecutionID, "response", now)
	require.ErrorIs(t, err, risk.ErrMCPFindingEvidenceNotStored)

	cleaned, err := riskrepo.New(ti.conn).CleanupExpiredMCPExecutionEvidenceBatch(ctx, 500)
	require.NoError(t, err)
	require.Equal(t, int64(1), cleaned)
	cleaned, err = riskrepo.New(ti.conn).CleanupExpiredMCPExecutionEvidenceBatch(ctx, 500)
	require.NoError(t, err)
	require.Zero(t, cleaned)
	_, err = ti.findingEvidence.RevealExecutionPayload(ctx, orgID, projectID, executionID, "request", now)
	require.NoError(t, err, "cleanup keeps live payloads")
}
