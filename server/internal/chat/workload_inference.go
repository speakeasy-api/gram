package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// ProjectScopedCompletionsRouteParam is the chi path parameter holding the
	// project UUID on the project-scoped OpenAI-compatible completions route.
	ProjectScopedCompletionsRouteParam = "projectID"

	// ProjectScopedCompletionsRoute is the project-scoped inference endpoint
	// where direct-bearer workload JWTs are accepted. Harnesses configure
	// `<base>/projects/<project_id>` as their OpenAI base URL.
	ProjectScopedCompletionsRoute = "/projects/{" + ProjectScopedCompletionsRouteParam + "}/chat/completions"
)

// WorkloadInferenceAuthorizer accepts explicitly inference-audienced workload
// sessions and, when projectID is non-nil (from the project-scoped completions
// route), direct-bearer workload JWTs. It rechecks workload admission and
// effective policy on every call, and reports handled even when a recognized
// workload credential is refused so authentication never falls back to another
// identity on that request.
type WorkloadInferenceAuthorizer interface {
	AuthorizeWorkloadInference(ctx context.Context, token string, projectID uuid.UUID) (context.Context, bool, error)
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

func completionPathProjectID(r *http.Request) (uuid.UUID, bool, error) {
	raw := chi.URLParam(r, ProjectScopedCompletionsRouteParam)
	if raw == "" {
		return uuid.Nil, false, nil
	}
	projectID, err := uuid.Parse(raw)
	if err != nil || projectID == uuid.Nil {
		return uuid.Nil, true, oops.C(oops.CodeUnauthorized)
	}
	return projectID, true, nil
}

func (s *Service) authorizeWorkloadCompletion(r *http.Request) (context.Context, *contextvalues.AuthContext, bool, error) {
	ctx := r.Context()
	pathProjectID, hasPathProject, err := completionPathProjectID(r)
	if err != nil {
		return ctx, nil, true, err
	}
	fields := strings.Fields(r.Header.Get("Authorization"))
	if s.workloadInference == nil || len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ctx, nil, false, nil
	}
	ctx, handled, err := s.workloadInference.AuthorizeWorkloadInference(ctx, fields[1], pathProjectID)
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
	if hasPathProject && *authCtx.ProjectID != pathProjectID {
		return ctx, nil, true, oops.C(oops.CodeForbidden)
	}
	// Workloads have no authority over assistant or transcript ownership. Never
	// forward a caller's claimed identity into capture, linking, or billing.
	if r.Header.Get("Gram-Chat-ID") != "" || r.Header.Get("Gram-Assistant-ID") != "" {
		return ctx, nil, true, oops.C(oops.CodeForbidden)
	}
	if project := r.Header.Get("Gram-Project"); project != "" && project != authCtx.ProjectID.String() && (authCtx.ProjectSlug == nil || project != *authCtx.ProjectSlug) {
		return ctx, nil, true, oops.C(oops.CodeForbidden)
	}
	if s.assistantTokens != nil {
		ctx, err = s.assistantTokens.AuthorizeKitWorkload(ctx)
		if err != nil {
			return ctx, nil, true, fmt.Errorf("attribute managed workload: %w", err)
		}
	}
	ctx = context.WithValue(ctx, workloadInferenceContextKey{}, true)
	return ctx, authCtx, true, nil
}

// workloadInferenceSource is a billable customer source and key slot. A
// caller-controlled source cannot select internal exemptions or assistant keys.
var workloadInferenceSource = billing.ModelUsageSourcePlayground

// Kit v0.2.19 uses agentkit-adapter-completions 0.11.0, whose request
// builder puts the ACP session ID in the OpenAI body `user` field.
// This is only a selector into persisted bindings, never authentication.
func kitCompletionSessionID(body []byte) (uuid.UUID, error) {
	var attribution struct {
		User string `json:"user"`
	}
	if err := json.Unmarshal(body, &attribution); err != nil || attribution.User == "" {
		return uuid.Nil, oops.C(oops.CodeForbidden)
	}
	return SessionIDToChatID(attribution.User), nil
}
