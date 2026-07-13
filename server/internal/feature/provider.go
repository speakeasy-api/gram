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

	// FlagPayload returns the raw JSON payload PostHog attaches to the flag
	// release that matches distinctID, or (nil, nil) when the flag is off, has
	// no payload, or the provider is disabled. groups is used for group-targeted
	// releases the same way as IsFlagEnabled. Callers should fail closed: treat a
	// nil payload or an error as "no clearance".
	FlagPayload(ctx context.Context, flag Flag, distinctID string, groups map[string]string) ([]byte, error)
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

// payloadKey namespaces payload entries so they never collide with the boolean
// entries SetFlag/IsFlagEnabled store under "<distinctID>:<flag>".
func payloadKey(flag Flag, distinctID string) string {
	return "payload:" + distinctID + ":" + string(flag)
}

func (imp *InMemory) FlagPayload(ctx context.Context, flag Flag, distinctID string, groups map[string]string) ([]byte, error) {
	val, ok := (*sync.Map)(imp).Load(payloadKey(flag, distinctID))
	if !ok {
		return nil, nil
	}

	payload, ok := val.([]byte)
	if !ok {
		return nil, nil
	}

	return payload, nil
}

func (imp *InMemory) SetFlagPayload(flag Flag, distinctID string, payload []byte) {
	(*sync.Map)(imp).Store(payloadKey(flag, distinctID), payload)
}

// OrgProjectGroups returns the PostHog group memberships used to evaluate
// org/project-scoped flags. It keys the "organization" group by the org slug
// and the "slug" group by "<orgSlug>/<projectSlug>" — the same group types the
// dashboard (client/dashboard/src/contexts/Telemetry.tsx) and backend event
// capture (server/internal/thirdparty/posthog) register. PostHog caps a project
// at 5 group types and these are the only org/project ones that exist; any
// other group type is silently dropped at ingestion, so a flag release targeting
// it could never match. Empty slug components are omitted. Returns nil when no
// group can be built.
func OrgProjectGroups(orgSlug, projectSlug string) map[string]string {
	if orgSlug == "" {
		return nil
	}

	groups := map[string]string{"organization": orgSlug}
	if projectSlug != "" {
		groups["slug"] = orgSlug + "/" + projectSlug
	}

	return groups
}
