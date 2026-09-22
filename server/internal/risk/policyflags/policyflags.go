package policyflags

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type memoKey struct{}

type flagState struct {
	enabled bool
	orgSlug string
}

// requestMemo remembers each project flag resolved while serving one request.
type requestMemo struct {
	mu       sync.Mutex
	states   map[string]flagState
	payloads map[string][]byte
}

// WithRequestMemo makes every project flag lookup under ctx resolve once.
// Evaluating one transcript scans many inputs, and each scan would otherwise
// repeat the group query and remote flag lookup. A flag change takes effect on
// the next request.
func WithRequestMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, memoKey{}, &requestMemo{
		mu:       sync.Mutex{},
		states:   map[string]flagState{},
		payloads: map[string][]byte{},
	})
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
	if memo == nil {
		state, _ := resolveProjectFlag(ctx, logger, queries, flags, orgID, projectID, flag)
		return state.enabled, state.orgSlug
	}
	// The lock is held through resolution so concurrent scans that miss the
	// same flag wait for one lookup instead of each making their own.
	memo.mu.Lock()
	defer memo.mu.Unlock()
	key := projectID.String() + ":" + string(flag)
	state, ok := memo.states[key]
	if !ok {
		// A failed lookup reads as off for this scan only; the next scan
		// retries it rather than inheriting the failure.
		var resolved bool
		state, resolved = resolveProjectFlag(ctx, logger, queries, flags, orgID, projectID, flag)
		if resolved {
			memo.states[key] = state
		}
	}
	return state.enabled, state.orgSlug
}

// ProjectFlagPayload resolves the flag payload for the project's organization
// and project groups, memoized per request. Nil means no payload.
func ProjectFlagPayload(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) []byte {
	if flags == nil {
		return nil
	}
	memo, _ := ctx.Value(memoKey{}).(*requestMemo)
	if memo == nil {
		return resolveProjectFlagPayload(ctx, logger, queries, flags, orgID, projectID, flag)
	}
	memo.mu.Lock()
	defer memo.mu.Unlock()
	key := projectID.String() + ":" + string(flag)
	payload, ok := memo.payloads[key]
	if !ok {
		payload = resolveProjectFlagPayload(ctx, logger, queries, flags, orgID, projectID, flag)
		memo.payloads[key] = payload
	}
	return payload
}

// EnforcementMaxContentBytes resolves the enforcement dispatch limit from the
// flag payload, falling back to the default when the flag carries none.
func EnforcementMaxContentBytes(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID) int {
	var config struct {
		MaxContentBytes int `json:"max_content_bytes"`
	}
	payload := ProjectFlagPayload(ctx, logger, queries, flags, orgID, projectID, feature.FlagRiskEnforcementMaxContentBytes)
	if payload == nil || json.Unmarshal(payload, &config) != nil || config.MaxContentBytes <= 0 {
		return enforcereply.DefaultMaxContentBytes
	}
	return min(config.MaxContentBytes, enforcereply.MaxContentBytes)
}

// resolveProjectFlag reports false as its second result when the lookup
// failed and the returned state is the fail-safe default.
func resolveProjectFlag(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) (flagState, bool) {
	groups, err := queries.GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		logger.WarnContext(ctx, "resolve project flag groups failed", attr.SlogError(err), attr.SlogOrganizationID(orgID), attr.SlogProjectID(projectID.String()))
		return flagState{enabled: false, orgSlug: ""}, false
	}
	on, err := flags.IsFlagEnabled(ctx, flag, orgID, feature.OrgProjectGroups(groups.OrganizationSlug, groups.ProjectSlug))
	if err != nil {
		logger.WarnContext(ctx, "project flag check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return flagState{enabled: false, orgSlug: ""}, false
	}
	return flagState{enabled: on, orgSlug: groups.OrganizationSlug}, true
}

func resolveProjectFlagPayload(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) []byte {
	groups, err := queries.GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		logger.WarnContext(ctx, "resolve project flag groups failed", attr.SlogError(err), attr.SlogOrganizationID(orgID), attr.SlogProjectID(projectID.String()))
		return nil
	}
	payload, err := flags.FlagPayload(ctx, flag, orgID, feature.OrgProjectGroups(groups.OrganizationSlug, groups.ProjectSlug))
	if err != nil {
		logger.WarnContext(ctx, "project flag payload check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return nil
	}
	return payload
}
