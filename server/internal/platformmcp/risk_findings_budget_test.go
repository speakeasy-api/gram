package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

type budgetTestFindings struct {
	list func(context.Context, Principal, ListRiskFindingsInput) (ListRiskFindingsOutput, error)
}

func (*budgetTestFindings) valid() bool { return true }
func (s *budgetTestFindings) List(ctx context.Context, p Principal, in ListRiskFindingsInput) (ListRiskFindingsOutput, error) {
	return s.list(ctx, p, in)
}

func TestRiskFindingsBudgetChargesBeforeList(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                   string
		connectionAllowed, organizationAllowed bool
		connectionErr, organizationErr         error
		wantErr                                error
		wantOrganization                       bool
	}{
		{name: "allowed", connectionAllowed: true, organizationAllowed: true, wantOrganization: true},
		{name: "connection denied", wantErr: ErrOperationRateLimited},
		{name: "organization denied", connectionAllowed: true, wantOrganization: true, wantErr: ErrOperationRateLimited},
		{name: "connection unavailable", connectionErr: errors.New("store unavailable"), wantErr: ErrOperationBudgetUnavailable},
		{name: "organization unavailable", connectionAllowed: true, organizationErr: errors.New("store unavailable"), wantOrganization: true, wantErr: ErrOperationBudgetUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: tc.connectionAllowed}, err: tc.connectionErr}
			organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: tc.organizationAllowed}, err: tc.organizationErr}
			principal := Principal{ConnectionID: "connection", OrganizationID: "organization"}
			input := ListRiskFindingsInput{Cursor: "page-two"}
			calls := 0
			service := &budgetTestFindings{list: func(_ context.Context, got Principal, in ListRiskFindingsInput) (ListRiskFindingsOutput, error) {
				calls++
				require.Equal(t, principal, got)
				require.Equal(t, input, in)
				require.Len(t, connection.keys, calls)
				require.Len(t, organization.keys, calls)
				return ListRiskFindingsOutput{TotalCount: 7}, nil
			}}
			limited := &budgetedRiskFindings{service: service, budget: OperationBudget{Connection: connection, Organization: organization}}
			require.True(t, limited.valid())
			out, err := limited.List(t.Context(), principal, input)
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, []string{"connection"}, connection.keys)
			if tc.wantOrganization {
				require.Equal(t, []string{"organization"}, organization.keys)
			} else {
				require.Empty(t, organization.keys)
			}
			if tc.wantErr != nil {
				require.Zero(t, calls)
				require.Empty(t, out)
				return
			}
			require.Equal(t, uint64(7), out.TotalCount)
			_, err = limited.List(t.Context(), principal, input)
			require.NoError(t, err)
			require.Equal(t, 2, calls, "each page must charge both budgets")
		})
	}
}

func TestRiskFindingsBudgetFailsClosedWhenMissing(t *testing.T) {
	t.Parallel()
	for _, budget := range []OperationBudget{{}, {Connection: allowOperationLimiter{}}, {Organization: allowOperationLimiter{}}} {
		limited := &budgetedRiskFindings{service: &budgetTestFindings{list: func(context.Context, Principal, ListRiskFindingsInput) (ListRiskFindingsOutput, error) {
			t.Fatal("List called without both limiters")
			return ListRiskFindingsOutput{}, nil
		}}, budget: budget}
		require.False(t, limited.valid())
		_, err := limited.List(t.Context(), Principal{ConnectionID: "connection", OrganizationID: "organization"}, ListRiskFindingsInput{})
		require.ErrorIs(t, err, ErrOperationBudgetUnavailable)
	}
}

func TestRiskFindingsBudgetToolRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err           error
		outcome, code string
	}{
		{ErrOperationRateLimited, "rate_limited", "rate_limited"},
		{ErrOperationBudgetUnavailable, "unavailable", unavailableCode},
	} {
		t.Run(tc.outcome, func(t *testing.T) {
			t.Parallel()
			recorder := &recordingRiskTelemetry{}
			ctx := ContextWithPrincipal(t.Context(), Principal{UserID: "user", OrganizationID: "organization"})
			result, _, err := riskReadToolCall(ctx, recorder, "list_risk_findings", func(Principal) (ListRiskFindingsOutput, error) {
				return ListRiskFindingsOutput{}, tc.err
			})
			require.NoError(t, err)
			require.True(t, result.IsError)
			content, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			require.Contains(t, content.Text, `"code":"`+tc.code+`"`)
			require.Len(t, recorder.events, 1)
			require.Equal(t, tc.outcome, recorder.events[0].Outcome)
		})
	}
}

func TestRiskFindingsReaderRequiresBudget(t *testing.T) {
	t.Parallel()
	service, _, _ := findingsFixture(t)
	reader := (&PostgresReader{}).WithRiskFindings(service, allowBudget())
	limited, ok := reader.riskFindings.(*budgetedRiskFindings)
	require.True(t, ok, "production reader must attach a metered service")
	require.Same(t, service, limited.service)
	require.True(t, limited.valid())

	reader.WithRiskFindings(service, OperationBudget{})
	require.False(t, reader.riskFindings.valid(), "missing production budget must disable the tool")
}

func TestRiskFindingsBudgetPreservesServiceError(t *testing.T) {
	t.Parallel()
	limited := &budgetedRiskFindings{
		service: &budgetTestFindings{list: func(context.Context, Principal, ListRiskFindingsInput) (ListRiskFindingsOutput, error) {
			return ListRiskFindingsOutput{}, ErrRiskReadInvalid
		}},
		budget: allowBudget(),
	}
	_, err := limited.List(t.Context(), Principal{ConnectionID: "connection", OrganizationID: "organization"}, ListRiskFindingsInput{})
	require.ErrorIs(t, err, ErrRiskReadInvalid)
	require.ErrorContains(t, err, "list risk findings")
}
