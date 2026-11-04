package access

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The whole-server connect rule a principal can be given on one resource.
const (
	ConnectRuleAllowed    = "allowed"
	ConnectRuleBlocked    = "blocked"
	ConnectRuleNotGranted = "not_granted"
)

var connectRuleLevels = map[string]string{
	ConnectRuleAllowed:    audienceLevelUse,
	ConnectRuleBlocked:    audienceLevelBlocked,
	ConnectRuleNotGranted: "",
}

// ResourceConnectionAudience is who may connect to one resource: the rules
// naming it, the version a save must carry, and the members those rules
// reach once blocks and overrides are applied.
type ResourceConnectionAudience struct {
	ProjectID string
	Version   string
	// Rules are the rules naming this resource, one per principal and level.
	Rules []*gen.SetResourceAudienceEntry
	// Audience is nil for a gateway, whose connect is decided by each server
	// it fronts.
	Audience *gen.ResourceConnectAudience
}

// ReadResourceConnectionAudience reads one resource's connection audience.
// Callers must already have checked that the reader administers the
// organization: the result names the principals each rule reaches.
func (r *RoleManager) ReadResourceConnectionAudience(ctx context.Context, organizationID, resourceID string) (ResourceConnectionAudience, error) {
	projectID, err := r.resourceProjectID(ctx, organizationID, resourceID)
	if err != nil {
		return ResourceConnectionAudience{}, err
	}
	rules, err := audienceRulesSnapshot(ctx, r.db, organizationID, resourceID)
	if err != nil {
		return ResourceConnectionAudience{}, oops.E(oops.CodeUnexpected, err, "read resource audience rules").LogError(ctx, r.logger)
	}
	version, err := r.audienceFingerprint(ctx, r.db, organizationID, resourceID)
	if err != nil {
		return ResourceConnectionAudience{}, err
	}
	audience, err := r.resourceConnectAudience(ctx, organizationID, resourceID, projectID)
	if err != nil {
		return ResourceConnectionAudience{}, err
	}
	return ResourceConnectionAudience{ProjectID: projectID, Version: version, Rules: rules, Audience: audience}, nil
}

// SetResourceConnectRule sets one principal's own whole-server connect rule
// on one resource and leaves every other rule naming it as it is, through the
// same save, guards and audit entry as the audience editor. "allowed" writes
// an unnarrowed connect grant, "blocked" an unnarrowed connect block, and
// "not_granted" removes both. Tool- or annotation-scoped blocks the principal
// holds survive "allowed" and "not_granted", so this never widens access past
// a narrower block someone wrote on purpose.
//
// Only people, roles and everyone (user:all) can be named. Callers must
// already have checked that the actor administers the organization.
func (r *RoleManager) SetResourceConnectRule(ctx context.Context, organizationID string, actor RoleAuditActor, resourceID string, principal urn.Principal, rule, expectedVersion string) (ResourceConnectionAudience, error) {
	level, ok := connectRuleLevels[rule]
	if !ok {
		return ResourceConnectionAudience{}, oops.E(oops.CodeInvalid, nil, "connect rule must be allowed, blocked, or not_granted")
	}
	if principal.Type != urn.PrincipalTypeUser && principal.Type != urn.PrincipalTypeRole {
		return ResourceConnectionAudience{}, oops.E(oops.CodeInvalid, nil, "only people, roles, or everyone can be given a connect rule here")
	}

	current, err := audienceRulesSnapshot(ctx, r.db, organizationID, resourceID)
	if err != nil {
		return ResourceConnectionAudience{}, oops.E(oops.CodeUnexpected, err, "read resource audience rules").LogError(ctx, r.logger)
	}
	target := principal.String()
	entries := make([]*gen.SetResourceAudienceEntry, 0, len(current)+1)
	for _, existing := range current {
		if existing.PrincipalUrn == target {
			narrowed := len(existing.Tools) > 0 || len(existing.Dispositions) > 0
			switch {
			case existing.Level == audienceLevelUse:
				continue
			case existing.Level == audienceLevelBlocked && (!narrowed || level == audienceLevelBlocked):
				continue
			}
		}
		entries = append(entries, existing)
	}
	if level != "" {
		entries = append(entries, &gen.SetResourceAudienceEntry{PrincipalUrn: target, Level: level, Tools: nil, Dispositions: nil})
	}

	// The save re-checks the version under its lock, so a change made between
	// this read and the write is still a conflict.
	if _, err := r.saveResourceAudience(ctx, organizationID, actor, resourceID, entries, expectedVersion); err != nil {
		return ResourceConnectionAudience{}, err
	}
	return r.ReadResourceConnectionAudience(ctx, organizationID, resourceID)
}
