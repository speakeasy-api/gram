package adminmcp

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func newFunctionTimeoutWriter(f writeFixture) *functionTimeoutWriter {
	return &functionTimeoutWriter{runner: f.runner, writes: WriteConfig{Enabled: true}}
}

func TestFunctionTimeoutApplies(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_function_timeout_execute")
	writer := newFunctionTimeoutWriter(f)
	ctx := writeContext(t, f)
	before, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgA)
	require.NoError(t, err)
	other, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgB)
	require.NoError(t, err)

	input := FunctionTimeoutInput{OrganizationID: f.orgA, FunctionTimeoutSeconds: 300}
	receipt, err := writer.apply(ctx, input)
	require.NoError(t, err)
	require.Equal(t, f.orgA, receipt.Before.OrganizationID)
	require.Equal(t, 60, receipt.Before.Seconds)
	require.True(t, receipt.Before.IsDefault)
	require.Equal(t, 300, receipt.After.Seconds)
	require.False(t, receipt.After.IsDefault)
	require.Contains(t, receipt.SideEffects, "redeployed")

	after, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgA)
	require.NoError(t, err)
	require.True(t, after.FunctionTimeoutSeconds.Valid)
	require.Equal(t, int32(300), after.FunctionTimeoutSeconds.Int32)
	after.FunctionTimeoutSeconds, after.UpdatedAt = before.FunctionTimeoutSeconds, before.UpdatedAt
	require.Equal(t, before, after, "only the function timeout and its update timestamp change")
	otherAfter, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgB)
	require.NoError(t, err)
	require.Equal(t, other, otherAfter)

	_, err = writer.apply(ctx, input)
	require.ErrorIs(t, err, errFunctionTimeoutNoChange, "repeating the call changes nothing")
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationFunctionTimeout, f.orgA, "executed"))
}

func TestFunctionTimeoutRestoresDefault(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_function_timeout_default")
	writer := newFunctionTimeoutWriter(f)
	ctx := writeContext(t, f)

	_, err := writer.apply(ctx, FunctionTimeoutInput{OrganizationID: f.orgA, UseDefault: true})
	require.ErrorIs(t, err, errFunctionTimeoutNoChange, "an organization on the default has nothing to clear")

	_, err = writer.apply(ctx, FunctionTimeoutInput{OrganizationID: f.orgA, FunctionTimeoutSeconds: 45})
	require.NoError(t, err)
	receipt, err := writer.apply(ctx, FunctionTimeoutInput{OrganizationID: f.orgA, UseDefault: true})
	require.NoError(t, err)
	require.Equal(t, 45, receipt.Before.Seconds)
	require.Equal(t, 60, receipt.After.Seconds)
	require.True(t, receipt.After.IsDefault)

	after, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgA)
	require.NoError(t, err)
	require.False(t, after.FunctionTimeoutSeconds.Valid, "restoring the default clears the stored value")

	// Setting 60 explicitly pins it, which is a change from the default.
	receipt, err = writer.apply(ctx, FunctionTimeoutInput{OrganizationID: f.orgA, FunctionTimeoutSeconds: 60})
	require.NoError(t, err)
	require.False(t, receipt.After.IsDefault)
}

func TestFunctionTimeoutRefusesInvalidInput(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_function_timeout_invalid")
	writer := newFunctionTimeoutWriter(f)
	for _, input := range []FunctionTimeoutInput{
		{OrganizationID: f.orgA},
		{OrganizationID: f.orgA, FunctionTimeoutSeconds: 29},
		{OrganizationID: f.orgA, FunctionTimeoutSeconds: 601},
		{OrganizationID: f.orgA, FunctionTimeoutSeconds: -30},
		{OrganizationID: f.orgA, FunctionTimeoutSeconds: 120, UseDefault: true},
	} {
		_, err := writer.apply(writeContext(t, f), input)
		require.ErrorIs(t, err, errFunctionTimeoutInput, "input %+v", input)
	}
	for _, organizationID := range []string{"", " " + f.orgA, "synthetic-a"} {
		_, err := writer.apply(writeContext(t, f), FunctionTimeoutInput{OrganizationID: organizationID, FunctionTimeoutSeconds: 120})
		require.Error(t, err, "organization %q", organizationID)
	}
	org, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
	require.NoError(t, err)
	require.False(t, org.FunctionTimeoutSeconds.Valid)
	require.Zero(t, countWriteEvents(t, f.db, OperationSetOrganizationFunctionTimeout, f.orgA, "executed"))
}

func TestFunctionTimeoutConcurrentCallsWriteOnce(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_function_timeout_concurrent")
	writer := newFunctionTimeoutWriter(f)
	ctx := writeContext(t, f)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = writer.apply(ctx, FunctionTimeoutInput{OrganizationID: f.orgA, FunctionTimeoutSeconds: 240})
		})
	}
	wg.Wait()
	require.NotEqual(t, errs[0] == nil, errs[1] == nil, "exactly one call changes the setting")
	require.ErrorIs(t, errors.Join(errs[0], errs[1]), errFunctionTimeoutNoChange, "the other call finds nothing to do")
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationFunctionTimeout, f.orgA, "executed"))
}

func TestFunctionTimeoutRequiresAuthority(t *testing.T) {
	t.Parallel()
	writer := &functionTimeoutWriter{}
	_, err := writer.apply(t.Context(), FunctionTimeoutInput{})
	require.ErrorIs(t, err, ErrWriteDisabled)
	writer.writes = WriteConfig{Enabled: true}
	_, err = writer.apply(t.Context(), FunctionTimeoutInput{})
	require.ErrorIs(t, err, ErrWriteIdentity)
	_, err = writer.apply(writePrincipalContext(t, []string{ScopeRead}), FunctionTimeoutInput{})
	require.ErrorIs(t, err, ErrWriteScope)
}
