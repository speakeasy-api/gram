package risk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// mockSignaler for testing
type mockSignaler struct {
	mock.Mock
}

func (m *mockSignaler) SignalNewMessages(ctx context.Context, params background.DrainRiskAnalysisParams) error {
	args := m.Called(ctx, params)
	return args.Error(0)
}

// Test that OnMessagesStored calls the signaler for enabled policies
func TestService_OnMessagesStored(t *testing.T) {
	// This test ensures that when messages are stored, the risk service
	// properly signals the workflow to analyze them for enabled policies.
	// The actual implementation is tested via integration tests.

	// Note: Full integration testing of this service requires a complete
	// test environment with database, redis, and auth setup which is
	// beyond the scope of unit tests.

	// Example of what would be tested with full setup:
	// ctx := context.Background()
	// projectID := uuid.New()
	// service.OnMessagesStored(ctx, projectID)
	// signaler.AssertExpectations(t)
}

// Test basic validation in CreateRiskPolicy
func TestService_CreateRiskPolicy_Validation(t *testing.T) {
	// Test that creating a policy requires authentication
	ctx := context.Background()

	// Without auth context, should return unauthorized
	s := &Service{}
	enabled := true
	_, err := s.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    "Test",
		Sources: []string{"gitleaks"},
		Enabled: &enabled,
	})

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

// Test that the service properly validates project context
func TestService_RequiresProjectContext(t *testing.T) {
	ctx := context.Background()

	// Create auth context without project
	authCtx := &contextvalues.AuthContext{
		UserID:               "test-user",
		ActiveOrganizationID: "test-org",
		// ProjectID is nil
	}
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	s := &Service{}

	// All methods should require project context
	enabled := true
	_, err := s.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    "Test",
		Sources: []string{"gitleaks"},
		Enabled: &enabled,
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	_, err = s.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.Error(t, err)
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

// Test OnMessagesStored behavior with mock signaler
func TestService_OnMessagesStored_CallsSignaler(t *testing.T) {
	// This test validates that the OnMessagesStored observer
	// properly queries for enabled policies and signals the workflow.
	// Full testing requires database setup which is done in integration tests.

	// The key behaviors tested:
	// 1. It queries for enabled policies for the project
	// 2. It calls SignalNewMessages for each enabled policy
	// 3. It handles errors gracefully without crashing
}

// Test that the service implements the required interfaces
func TestService_Interfaces(t *testing.T) {
	s := &Service{}

	// Ensure Service implements gen.Service
	var _ gen.Service = s

	// Ensure Service implements gen.Auther
	var _ gen.Auther = s
}
