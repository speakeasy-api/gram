package feature

import (
	"context"
	"sync"
)

type Provider interface {
	// IsFlagEnabled reports whether flag is enabled for the given distinctID.
	// groups carries PostHog group memberships (group type -> group key) so
	// that group-targeted flag releases evaluate correctly server-side; pass
	// nil when the flag is targeted purely by distinct ID. Use
	// OrgProjectGroups to build the org/project groups the dashboard registers.
	IsFlagEnabled(ctx context.Context, flag Flag, distinctID string, groups map[string]string) (bool, error)
}

type InMemory sync.Map

func (imp *InMemory) IsFlagEnabled(ctx context.Context, flag Flag, distinctID string, groups map[string]string) (bool, error) {
	key := distinctID + ":" + string(flag)

	val, ok := (*sync.Map)(imp).Load(key)
	if !ok {
		return false, nil
	}

	enabled, ok := val.(bool)
	if !ok {
		return false, nil
	}

	return enabled, nil
}

func (imp *InMemory) SetFlag(flag Flag, distinctID string, enabled bool) {
	key := distinctID + ":" + string(flag)

	(*sync.Map)(imp).Store(key, enabled)
}

// OrgProjectGroups returns the PostHog group memberships used to evaluate
// org/project-scoped flags, mirroring the groups the dashboard registers (see
// client/dashboard/src/contexts/Telemetry.tsx): "organization_slug" keyed by
// the org slug and "slug" keyed by "<orgSlug>/<projectSlug>". Empty slug
// components are omitted so a release targeting either group matches the same
// way it does for the frontend. Returns nil when no group can be built.
func OrgProjectGroups(orgSlug, projectSlug string) map[string]string {
	if orgSlug == "" {
		return nil
	}

	groups := map[string]string{"organization_slug": orgSlug}
	if projectSlug != "" {
		groups["slug"] = orgSlug + "/" + projectSlug
	}

	return groups
}
