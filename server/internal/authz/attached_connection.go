package authz

import (
	"context"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AttachedConnectionConsent is a member's consent, given by attaching their
// own personal connection to an agent, that the agent may use that connection
// whoever issued the credential it calls with. It carries the owner's live
// policy, read when the call is decided, and is built only by
// Engine.AttachedConnectionConsent, so a caller cannot widen it.
//
// The caller vouches for the attachment: it must come from the exact,
// tenant-scoped binding lookup token resolution uses, which serves only a
// live attachment the connection's owner made, to an active agent, while the
// owner is still a member. Consent never grants anything on its own: every
// policy the agent is admitted under still has to allow the use.
type AttachedConnectionConsent struct {
	organizationID string
	ownerUserID    string
	// ownerPolicy is the owner's live policy, empty when the owner is no
	// longer an active member, which allows nothing.
	ownerPolicy []Grant
}

// UserPolicyLoader reads userID's live policy in organizationID. member is
// false when the user is no longer an active member, and then policy is empty.
type UserPolicyLoader func(ctx context.Context, organizationID, userID string) (policy []Grant, member bool, err error)

// AttachedConnectionConsent reads ownerUserID's live policy in organizationID
// for consent to an attachment they made. An owner who is no longer an active
// member has an empty policy, so their attachment serves nothing.
func (e *Engine) AttachedConnectionConsent(ctx context.Context, organizationID, ownerUserID string) (AttachedConnectionConsent, error) {
	consent := AttachedConnectionConsent{organizationID: organizationID, ownerUserID: ownerUserID, ownerPolicy: []Grant{}}
	if organizationID == "" || ownerUserID == "" {
		return consent, fmt.Errorf("attached connection consent needs an organization and an owner")
	}
	load := e.loadUserPolicy
	if load == nil {
		load = e.loadMemberPolicy
	}
	policy, member, err := load(ctx, organizationID, ownerUserID)
	if err != nil {
		return consent, fmt.Errorf("load attached connection owner policy: %w", err)
	}
	if member {
		consent.ownerPolicy = append(consent.ownerPolicy, policy...)
	}
	return consent, nil
}

// loadMemberPolicy reads an active member's live policy from the database.
func (e *Engine) loadMemberPolicy(ctx context.Context, organizationID, userID string) ([]Grant, bool, error) {
	principals, err := ResolveUserPrincipals(ctx, e.db, organizationID, userID)
	if err != nil {
		return nil, false, fmt.Errorf("resolve user principals: %w", err)
	}
	// A user who is not an active member still resolves to user:all, whose
	// defaults would otherwise read as their policy.
	user := urn.NewPrincipal(urn.PrincipalTypeUser, userID).String()
	if !slices.ContainsFunc(principals, func(principal urn.Principal) bool { return principal.String() == user }) {
		return nil, false, nil
	}
	policy, err := LoadGrants(ctx, e.db, organizationID, principals)
	if err != nil {
		return nil, false, fmt.Errorf("load user grants: %w", err)
	}
	return policy, true, nil
}

// attachedConnection is consent applied to an admitted agent: owner's
// connection is decided as if owner were the represented user, and owner's
// live policy must allow it too.
type attachedConnection struct {
	owner  string
	policy []Grant
}

// appliesTo reports whether check uses the attached connection: a personal
// auth client check naming its owner.
func (a *attachedConnection) appliesTo(check Check) bool {
	if a == nil || !check.isAuthClientCheck() {
		return false
	}
	owner, personal := check.Dimensions[SelectorKeyConnectionOwner]
	return personal && owner == a.owner
}
