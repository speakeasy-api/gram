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

type flagMode struct {
	mode    feature.Variant
	orgSlug string
}

// requestMemo remembers each project flag resolved while serving one request.
// Boolean states and engine modes are memoized separately so a request can
// read both contracts of one key without either lookup shadowing the other.
type requestMemo struct {
	mu     sync.Mutex
	states map[string]flagState
	modes  map[string]flagMode
}

// WithRequestMemo makes every ProjectFlagState and ProjectFlagMode lookup
// under ctx resolve each project flag once. Evaluating one transcript scans
// many inputs, and each scan would otherwise repeat the group query and the
// remote flag check. A flag change takes effect on the next request.
func WithRequestMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, memoKey{}, &requestMemo{mu: sync.Mutex{}, states: map[string]flagState{}, modes: map[string]flagMode{}})
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

// ProjectFlagMode resolves the risk engine mode the multivariate flag selects
// for the project's organization, together with the organization slug the
// flag groups carry. It reads the flag's variant and, only when no variant
// resolved, its boolean value, then normalizes both through
// feature.RiskLLMAnalyzerVariant: a known variant wins, an empty variant with
// the boolean on reads as feature.VariantRiskLLMLLM, and anything else reads
// as feature.VariantRiskLLMOff. orgSlug is the organization slug the group
// lookup resolved, whatever the mode; only a failed lookup (of the groups,
// the variant or the boolean) returns feature.VariantRiskLLMOff with an
// empty slug. Under WithRequestMemo the lookup runs once per project and
// flag for the request.
func ProjectFlagMode(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) (mode feature.Variant, orgSlug string) {
	if flags == nil {
		return feature.VariantRiskLLMOff, ""
	}
	memo, _ := ctx.Value(memoKey{}).(*requestMemo)
	if memo == nil {
		resolved, _ := resolveProjectFlagMode(ctx, logger, queries, flags, orgID, projectID, flag)
		return resolved.mode, resolved.orgSlug
	}
	memo.mu.Lock()
	defer memo.mu.Unlock()
	key := "mode:" + projectID.String() + ":" + string(flag)
	resolved, ok := memo.modes[key]
	if !ok {
		// A failed lookup reads as off for this scan only; the next scan
		// retries it rather than inheriting the failure.
		var completed bool
		resolved, completed = resolveProjectFlagMode(ctx, logger, queries, flags, orgID, projectID, flag)
		if completed {
			memo.modes[key] = resolved
		}
	}
	return resolved.mode, resolved.orgSlug
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

// resolveProjectFlagMode reports false as its second result when the lookup
// failed and the returned mode is the fail-safe default.
func resolveProjectFlagMode(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) (flagMode, bool) {
	groups, err := queries.GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		logger.WarnContext(ctx, "resolve project flag groups failed", attr.SlogError(err), attr.SlogOrganizationID(orgID), attr.SlogProjectID(projectID.String()))
		return flagMode{mode: feature.VariantRiskLLMOff, orgSlug: ""}, false
	}
	flagGroups := feature.OrgProjectGroups(groups.OrganizationSlug, groups.ProjectSlug)
	variant, err := feature.FlagVariant(ctx, flags, flag, orgID, flagGroups)
	if err != nil {
		logger.WarnContext(ctx, "project flag variant check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return flagMode{mode: feature.VariantRiskLLMOff, orgSlug: ""}, false
	}
	legacyEnabled := false
	if variant == "" {
		// The boolean read is the transition rule for a key PostHog still
		// serves as boolean; an explicit variant makes it moot.
		legacyEnabled, err = flags.IsFlagEnabled(ctx, flag, orgID, flagGroups)
		if err != nil {
			logger.WarnContext(ctx, "project flag check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
			return flagMode{mode: feature.VariantRiskLLMOff, orgSlug: ""}, false
		}
	}
	return flagMode{mode: feature.RiskLLMAnalyzerVariant(variant, legacyEnabled), orgSlug: groups.OrganizationSlug}, true
}
