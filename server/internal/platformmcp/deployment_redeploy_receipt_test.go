package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestDeploymentRedeployReceiptStoreHashesSourceAndExpectedActive(t *testing.T) {
	t.Parallel()
	probe := newReceiptProbe(t, "platform_mcp_deployment_redeploy_receipt_hash")
	store := &deploymentRedeployReceiptStore{db: probe.conn}
	sourceID := uuid.NewString()
	activeID := uuid.NewString()
	deploymentID := uuid.NewString()
	input := RedeployDeploymentInput{
		ProjectID:                  probe.project.ID.String(),
		DeploymentID:               sourceID,
		ExpectedActiveDeploymentID: activeID,
		IdempotencyKey:             "redeploy-receipt-key",
		Confirmed:                  true,
	}
	charges := 0
	mutations := 0
	charge := func(context.Context) error {
		charges++
		return nil
	}
	mutate := func(context.Context, pgx.Tx) (deploymentRedeployReceipt, error) {
		mutations++
		return deploymentRedeployReceipt{
			ProjectID:          probe.project.ID.String(),
			SourceDeploymentID: sourceID,
			DeploymentID:       deploymentID,
		}, nil
	}

	first, err := store.Execute(t.Context(), probe.principal, probe.project, input, charge, mutate)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	stored, err := decodeDeploymentRedeployReceipt(first.ResultPayload)
	require.NoError(t, err)
	require.Equal(t, probe.project.ID.String(), stored.ProjectID)
	require.Equal(t, sourceID, stored.SourceDeploymentID)
	require.Equal(t, deploymentID, stored.DeploymentID)
	require.Equal(t, 1, charges)
	require.Equal(t, 1, mutations)

	replay, err := store.Execute(t.Context(), probe.principal, probe.project, input, charge, mutate)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.ID, replay.ID)
	require.Equal(t, 1, charges, "an exact replay must not be charged")
	require.Equal(t, 1, mutations, "an exact replay must not execute the mutation")

	changedSource := input
	changedSource.DeploymentID = uuid.NewString()
	_, err = store.Execute(t.Context(), probe.principal, probe.project, changedSource, charge, mutate)
	var sourceConflict *deploymentRedeployError
	require.ErrorAs(t, err, &sourceConflict)
	require.Equal(t, "conflict", sourceConflict.Code)
	require.Equal(t, 1, charges, "a changed source under a used key is rejected before charging")
	require.Equal(t, 1, mutations)

	changedActive := input
	changedActive.ExpectedActiveDeploymentID = uuid.NewString()
	_, err = store.Execute(t.Context(), probe.principal, probe.project, changedActive, charge, mutate)
	var activeConflict *deploymentRedeployError
	require.ErrorAs(t, err, &activeConflict)
	require.Equal(t, "conflict", activeConflict.Code)
	require.Equal(t, 1, charges, "a changed expected-active snapshot under a used key is rejected before charging")
	require.Equal(t, 1, mutations)
}

func TestDeploymentRedeployReceiptCodecValidRoundTrip(t *testing.T) {
	t.Parallel()
	receipt := deploymentRedeployReceipt{
		ProjectID:          uuid.NewString(),
		SourceDeploymentID: uuid.NewString(),
		DeploymentID:       uuid.NewString(),
	}
	payload, err := json.Marshal(receipt)
	require.NoError(t, err)

	decoded, err := decodeDeploymentRedeployReceipt(payload)
	require.NoError(t, err)
	require.Equal(t, receipt, decoded)
}

func TestDeploymentRedeployReceiptCodecRejectsCorruption(t *testing.T) {
	t.Parallel()
	projectID := uuid.NewString()
	sourceID := uuid.NewString()
	deploymentID := uuid.NewString()
	corrupt := map[string][]byte{
		"malformed":     []byte(`{"project_id":`),
		"unknown field": []byte(`{"project_id":"` + projectID + `","source_deployment_id":"` + sourceID + `","deployment_id":"` + deploymentID + `","secret":"hidden"}`),
		"zero project":  []byte(`{"project_id":"00000000-0000-0000-0000-000000000000","source_deployment_id":"` + sourceID + `","deployment_id":"` + deploymentID + `"}`),
		"zero source":   []byte(`{"project_id":"` + projectID + `","source_deployment_id":"00000000-0000-0000-0000-000000000000","deployment_id":"` + deploymentID + `"}`),
		"zero deployed": []byte(`{"project_id":"` + projectID + `","source_deployment_id":"` + sourceID + `","deployment_id":"00000000-0000-0000-0000-000000000000"}`),
		"same IDs":      []byte(`{"project_id":"` + projectID + `","source_deployment_id":"` + sourceID + `","deployment_id":"` + sourceID + `"}`),
	}

	for name, payload := range corrupt {
		_, err := decodeDeploymentRedeployReceipt(payload)
		require.ErrorIs(t, err, ErrUnavailable, name)
	}
}

func TestRedeployDeploymentProjectResolutionIsOrganizationBound(t *testing.T) {
	t.Parallel()
	probe := newReceiptProbe(t, "platform_mcp_redeploy_organization_scope")
	reader := NewPostgresReader(testenv.NewLogger(t), probe.conn)
	input := FindMCPInput{ProjectID: probe.project.ID.String()}
	project, err := reader.resolveInventoryProject(t.Context(), probe.principal.OrganizationID, input)
	require.NoError(t, err)
	require.Equal(t, probe.project.ID, project.ID)
	_, err = reader.resolveInventoryProject(t.Context(), "another-organization", input)
	require.ErrorIs(t, err, ErrForbidden)
}
