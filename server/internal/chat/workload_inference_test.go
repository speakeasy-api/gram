package chat

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type workloadInferenceAuthorizerFunc func(context.Context, string) (context.Context, bool, error)

func (f workloadInferenceAuthorizerFunc) AuthorizeWorkloadInference(ctx context.Context, token string) (context.Context, bool, error) {
	return f(ctx, token)
}

func TestWorkloadCompletionBoundary(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	projectSlug := "inference-test"
	for _, tc := range []struct {
		name, header, value string
		denied              bool
	}{
		{name: "credential-bound project"},
		{name: "matching UUID", header: "Gram-Project", value: projectID.String()},
		{name: "matching slug", header: "Gram-Project", value: projectSlug},
		{name: "different project", header: "Gram-Project", value: uuid.NewString(), denied: true},
		{name: "transcript ownership", header: "Gram-Chat-ID", value: uuid.NewString(), denied: true},
		{name: "assistant ownership", header: "Gram-Assistant-ID", value: uuid.NewString(), denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := (&Service{}).WithWorkloadInference(workloadInferenceAuthorizerFunc(func(ctx context.Context, token string) (context.Context, bool, error) {
				require.Equal(t, "test-token", token)
				authCtx := &contextvalues.AuthContext{ActiveOrganizationID: "test-org", ProjectID: &projectID, ProjectSlug: &projectSlug}
				return contextvalues.WithAuthenticatedActor(ctx, authCtx, urn.NewWorkloadPrincipal(uuid.New(), "test-subject")), true, nil
			}))
			r := httptest.NewRequest("POST", "/chat/completions", nil)
			r.Header.Set("Authorization", "Bearer test-token")
			if tc.header != "" {
				r.Header.Set(tc.header, tc.value)
			}
			ctx, authCtx, slot, err := s.authorizeCompletion(r)
			if tc.denied {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, projectID, *authCtx.ProjectID)
			require.Equal(t, workloadInferenceSource, slot)
			require.True(t, isWorkloadInference(ctx))
		})
	}
}

func TestWorkloadCompletionRefusalNeverFallsBack(t *testing.T) {
	t.Parallel()
	denied := errors.New("credential revoked")
	s := (&Service{}).WithWorkloadInference(workloadInferenceAuthorizerFunc(func(ctx context.Context, token string) (context.Context, bool, error) { return ctx, true, denied }))
	r := httptest.NewRequest("POST", "/chat/completions", nil)
	r.Header.Set("Authorization", "Bearer revoked-token")
	r.Header.Set("Gram-Key", "other-identity")
	_, _, _, err := s.authorizeCompletion(r)
	require.ErrorIs(t, err, denied)
}
