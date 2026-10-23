package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	usagegen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/stretchr/testify/require"
)

// Embedding the unused methods makes an unexpected upstream operation fail;
// only ordinary non-streaming completions are expected in these tests.
type workloadInferenceCompletionClient struct {
	openrouter.CompletionClient
	requests []openrouter.CompletionRequest
}

func (c *workloadInferenceCompletionClient) GetCompletion(_ context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	c.requests = append(c.requests, request)
	message := openrouter.CreateMessageAssistant("workload reply")
	return &openrouter.CompletionResponse{Message: &message, MessageID: "workload-completion", Model: "test-model"}, nil
}

type workloadInferenceBilling struct {
	billing.Repository
	exhausted bool
}

func (b *workloadInferenceBilling) GetStoredPeriodUsage(ctx context.Context, orgID string) (*usagegen.PeriodUsage, error) {
	if !b.exhausted {
		usage, err := b.Repository.GetStoredPeriodUsage(ctx, orgID)
		if err != nil {
			return nil, fmt.Errorf("read test period usage: %w", err)
		}
		return usage, nil
	}
	used, included := 10, 10
	return &usagegen.PeriodUsage{Credits: &used, IncludedCredits: &included}, nil
}

//nolint:paralleltest,tparallel // Subtests share an upstream recorder and deliberately mutate the same billing and workload state in sequence.
func TestWorkloadInference_CompletionEndToEnd(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	issuer := f.sharedIssuer(t)
	enableWorkloadInference(t, f)
	token := requireWorkloadInferenceToken(t, f, issuer)
	upstream := &workloadInferenceCompletionClient{}
	billingRepo := &workloadInferenceBilling{Repository: billing.NewStubClient(f.ti.logger, f.ti.tracerProvider)}
	svc := chat.NewService(f.ti.logger, f.ti.tracerProvider, f.ti.conn, f.ti.sessionManager,
		f.ti.chatSessionsManager, nil, upstream, nil, posthog.New(t.Context(), f.ti.logger, "", "", ""), nil, nil, f.ti.authzEngine, assistanttokens.New("workload-inference-test-secret", f.ti.conn, f.ti.authzEngine, f.ti.principalCredentials, f.ti.cacheAdapter),
		billingRepo, f.ti.audit).WithWorkloadInference(f.ti.service)
	complete := func(bearer string, headers map[string]string) (*httptest.ResponseRecorder, error) {
		req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hello"}],"stream":false}`))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		if err := svc.HandleCompletion(w, req); err != nil {
			return w, fmt.Errorf("complete workload request: %w", err)
		}
		return w, nil
	}
	for _, source := range []string{"", "gram", "assistant", "risk-analysis"} {
		t.Run("source="+source, func(t *testing.T) {
			before := len(upstream.requests)
			w, err := complete(token, map[string]string{"X-Gram-Source": source})
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code)
			require.Contains(t, w.Body.String(), "workload reply")
			require.Len(t, upstream.requests, before+1)
			request := upstream.requests[before]
			require.Equal(t, f.fx.orgID, request.OrgID)
			require.Equal(t, f.fx.target.ProjectID.String(), request.ProjectID)
			require.Equal(t, billing.ModelUsageSourcePlayground, request.UsageSource)
			require.Equal(t, billing.ModelUsageSourcePlayground, request.KeySlot)
			require.Equal(t, openrouter.KeyTypeChat, request.KeyType)
			require.Equal(t, uuid.Nil, request.ChatID)
		})
	}
	t.Run("matching project", func(t *testing.T) {
		_, err := complete(token, map[string]string{"Gram-Project": f.fx.target.ProjectID.String()})
		require.NoError(t, err)
	})
	for _, header := range []string{"Gram-Chat-ID", "Gram-Assistant-ID", "Gram-Project"} {
		t.Run("reject "+header, func(t *testing.T) {
			before := len(upstream.requests)
			_, err := complete(token, map[string]string{header: uuid.NewString()})
			require.Error(t, err)
			require.Len(t, upstream.requests, before, "rejected attribution must never reach upstream")
		})
	}
	t.Run("assertion is not a completion credential", func(t *testing.T) {
		before := len(upstream.requests)
		_, err := complete(f.assertion(t, issuer), nil)
		require.Error(t, err)
		require.Len(t, upstream.requests, before)
	})
	t.Run("MCP credential is not a completion credential", func(t *testing.T) {
		w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), issuerWorkloadForm(f.assertion(t, issuer)))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		before := len(upstream.requests)
		_, err := complete(decodeSharedTokenResponse(t, w).AccessToken, nil)
		require.Error(t, err)
		require.Len(t, upstream.requests, before)
	})
	t.Run("credit exhaustion blocks workload upstream", func(t *testing.T) {
		require.NoError(t, orgrepo.New(f.ti.conn).SetAccountType(t.Context(), orgrepo.SetAccountTypeParams{ID: f.fx.orgID, GramAccountType: string(billing.TierBase)}))
		billingRepo.exhausted = true
		defer func() { billingRepo.exhausted = false }()
		before := len(upstream.requests)
		_, err := complete(token, map[string]string{"X-Gram-Source": "gram"})
		require.Error(t, err)
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeInsufficientCredits, shareable.Code)
		require.Len(t, upstream.requests, before)
	})
	t.Run("assignment revocation reaches completion boundary", func(t *testing.T) {
		unassignWorkloadAgent(t, t.Context(), f.ti, f.assignmentID)
		before := len(upstream.requests)
		_, err := complete(token, nil)
		require.Error(t, err)
		require.Len(t, upstream.requests, before)
	})
}
