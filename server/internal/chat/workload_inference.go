package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// WorkloadInferenceAuthorizer accepts only explicitly inference-audienced
// sessions and rechecks workload admission and effective policy on every call.
// It reports handled even when a recognized inference credential is refused,
// preventing authentication fallback to a different identity on that request.
type WorkloadInferenceAuthorizer interface {
	AuthorizeWorkloadInference(context.Context, string) (context.Context, bool, error)
}

func (s *Service) WithWorkloadInference(authorizer WorkloadInferenceAuthorizer) *Service {
	s.workloadInference = authorizer
	return s
}

type workloadInferenceContextKey struct{}

func isWorkloadInference(ctx context.Context) bool {
	value, _ := ctx.Value(workloadInferenceContextKey{}).(bool)
	return value
}

func (s *Service) authorizeWorkloadCompletion(r *http.Request) (context.Context, *contextvalues.AuthContext, bool, error) {
	ctx := r.Context()
	fields := strings.Fields(r.Header.Get("Authorization"))
	if s.workloadInference == nil || len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ctx, nil, false, nil
	}
	ctx, handled, err := s.workloadInference.AuthorizeWorkloadInference(ctx, fields[1])
	if err != nil {
		return ctx, nil, handled, fmt.Errorf("authorize workload completion: %w", err)
	}
	if !handled {
		return ctx, nil, false, nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return ctx, nil, true, oops.C(oops.CodeUnauthorized)
	}
	// Workloads have no authority over assistant or transcript ownership. Never
	// forward a caller's claimed identity into capture, linking, or billing.
	if r.Header.Get("Gram-Chat-ID") != "" || r.Header.Get("Gram-Assistant-ID") != "" {
		return ctx, nil, true, oops.C(oops.CodeForbidden)
	}
	if project := r.Header.Get("Gram-Project"); project != "" && project != authCtx.ProjectID.String() && (authCtx.ProjectSlug == nil || project != *authCtx.ProjectSlug) {
		return ctx, nil, true, oops.C(oops.CodeForbidden)
	}
	ctx = context.WithValue(ctx, workloadInferenceContextKey{}, true)
	return ctx, authCtx, true, nil
}

// workloadInferenceSource is a billable customer source and key slot. A
// caller-controlled source cannot select internal exemptions or assistant keys.
var workloadInferenceSource = billing.ModelUsageSourcePlayground
