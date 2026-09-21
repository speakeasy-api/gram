package policyflags

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type memoKey struct{}

type flagState struct {
	enabled bool
	orgSlug string
}

// requestMemo remembers each project flag resolved while serving one request.
type requestMemo struct {
	mu     sync.Mutex
	states map[string]flagState
}

// WithRequestMemo makes every ProjectFlagState lookup under ctx resolve each
// project flag once. Evaluating one transcript scans many inputs, and each
// scan would otherwise repeat the group query and the remote flag check.
// A flag change takes effect on the next request.
func WithRequestMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, memoKey{}, &requestMemo{mu: sync.Mutex{}, states: map[string]flagState{}})
}

// ProjectFlagEnabled reports whether flag is on for the project's organization
// and project groups. Any lookup failure reads as off.
func ProjectFlagEnabled(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) bool {
	on, _ := ProjectFlagState(ctx, logger, queries, flags, orgID, projectID, flag)
	return on
}

// ProjectFlagState resolves the project's flag groups once and reports whether
// flag is on for them, together with the organization slug those groups
// carry so callers that need the slug for telemetry do not repeat the lookup.
// Any lookup failure reads as off with an empty slug.
func ProjectFlagState(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) (enabled bool, orgSlug string) {
	if flags == nil {
		return false, ""
	}
	memo, _ := ctx.Value(memoKey{}).(*requestMemo)
	key := projectID.String() + ":" + string(flag)
	if memo != nil {
		memo.mu.Lock()
		state, ok := memo.states[key]
		memo.mu.Unlock()
		if ok {
			return state.enabled, state.orgSlug
		}
	}
	state := resolveProjectFlag(ctx, logger, queries, flags, orgID, projectID, flag)
	if memo != nil {
		memo.mu.Lock()
		memo.states[key] = state
		memo.mu.Unlock()
	}
	return state.enabled, state.orgSlug
}

func resolveProjectFlag(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) flagState {
	groups, err := queries.GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		logger.WarnContext(ctx, "resolve project flag groups failed", attr.SlogError(err), attr.SlogOrganizationID(orgID), attr.SlogProjectID(projectID.String()))
		return flagState{enabled: false, orgSlug: ""}
	}
	on, err := flags.IsFlagEnabled(ctx, flag, orgID, feature.OrgProjectGroups(groups.OrganizationSlug, groups.ProjectSlug))
	if err != nil {
		logger.WarnContext(ctx, "project flag check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return flagState{enabled: false, orgSlug: ""}
	}
	return flagState{enabled: on, orgSlug: groups.OrganizationSlug}
}
