package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const (
	attachedIssuer = "user_issuer"
	attachedOwner  = "user_owner"
	attachedOther  = "user_other"
)

// attachedPolicies are the policies an agent key is admitted under, and the
// connection owner's live policy.
type attachedPolicies struct {
	credential, agent, agentOwner, issuer, owner []Grant
	ownerMember                                  bool
}

func ownConnectionGrant() Grant {
	return NewGrantWithSelector(ScopeMCPAuth, PersonalAuthClientSelector())
}

func allowingAttachedPolicies() attachedPolicies {
	own := []Grant{ownConnectionGrant()}
	return attachedPolicies{credential: own, agent: own, agentOwner: own, issuer: own, owner: own, ownerMember: true}
}

// decideAttached decides check for an agent key issued by attachedIssuer,
// with consent from attachedOwner applied when consent is set.
func decideAttached(t *testing.T, p attachedPolicies, consent bool, check Check) MandatoryDecision {
	t.Helper()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
		DevMode: false, AdmitPrincipalCredential: nil, AdmitPrincipalCredentialWithDBTX: nil, AdmitWorkloadSession: nil, AdmitWorkloadSessionInTx: nil,
		LoadUserPolicy: func(_ context.Context, organizationID, userID string) ([]Grant, bool, error) {
			if organizationID != "org_123" || userID != attachedOwner {
				return nil, false, errors.New("unexpected policy read")
			}
			return p.owner, p.ownerMember, nil
		},
	})
	ctx := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), RepresentedUserPolicy{UserID: attachedIssuer, Policy: p.issuer}, false, p.credential, p.agent, p.agentOwner)
	authorization, err := engine.PrepareMandatory(ctx)
	require.NoError(t, err)
	if consent {
		given, err := engine.AttachedConnectionConsent(ctx, "org_123", attachedOwner)
		require.NoError(t, err)
		authorization = authorization.WithAttachedConnection(given)
	}
	require.Equal(t, MandatoryCallerAgent, authorization.Caller())
	require.Equal(t, attachedIssuer, authorization.RepresentedUserID(), "the key still acts for its issuer")
	decision, err := authorization.DecideAuthClientAdmission("org_123", MandatoryServerPrivate, check)
	require.NoError(t, err)
	return decision
}

func connectionBlock(owner string) Grant {
	return NewGrantWithSelector(ScopeMCPBlockedAuth, ConnectionAuthClientSelector(authUpstreamU, owner))
}

// A connection its owner attached to an agent serves the agent whatever member
// issued the key, while the owner, the issuer and the agent all still allow it.
func TestAttachedConnectionServesAKeyFromAnotherMember(t *testing.T) {
	t.Parallel()

	require.True(t, decideAttached(t, allowingAttachedPolicies(), true, personalClientCheck(attachedOwner)).Allowed)
	require.False(t, decideAttached(t, allowingAttachedPolicies(), false, personalClientCheck(attachedOwner)).Allowed,
		"without the owner's attachment another member's connection is refused")
}

func TestAttachedConnectionStillBoundedByEveryPolicy(t *testing.T) {
	t.Parallel()

	connect := []Grant{NewGrant(ScopeMCPConnect, WildcardResource)}
	cases := map[string]func(p *attachedPolicies){
		"the owner's policy no longer allows it": func(p *attachedPolicies) { p.owner = connect },
		"the owner is no longer a member":        func(p *attachedPolicies) { p.ownerMember = false },
		"the owner blocked the agent on it": func(p *attachedPolicies) {
			p.agent = []Grant{ownConnectionGrant(), connectionBlock(attachedOwner)}
		},
		"the issuer may not use the client": func(p *attachedPolicies) { p.issuer = connect },
		"the issuer is blocked on the client": func(p *attachedPolicies) {
			p.issuer = []Grant{ownConnectionGrant(), NewGrantWithSelector(ScopeMCPBlockedAuth, authSelector(authClientA, nil))}
		},
		"the key's own policy does not reach it":     func(p *attachedPolicies) { p.credential = connect },
		"the agent's policy does not reach it":       func(p *attachedPolicies) { p.agent = connect },
		"the agent owner's policy does not reach it": func(p *attachedPolicies) { p.agentOwner = connect },
		"the issuer's grant names only their own connection": func(p *attachedPolicies) {
			p.issuer = []Grant{NewGrantWithSelector(ScopeMCPAuth, ConnectionAuthClientSelector(authUpstreamU, attachedIssuer))}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := allowingAttachedPolicies()
			change(&p)
			require.False(t, decideAttached(t, p, true, personalClientCheck(attachedOwner)).Allowed)
		})
	}
}

// Consent covers the owner's connection only: the issuer's own connection is
// decided as before, and nobody else's is reachable.
func TestAttachedConnectionConsentCoversOnlyTheOwnersConnection(t *testing.T) {
	t.Parallel()

	require.True(t, decideAttached(t, allowingAttachedPolicies(), true, personalClientCheck(attachedIssuer)).Allowed, "the issuer's own connection")
	require.False(t, decideAttached(t, allowingAttachedPolicies(), true, personalClientCheck(attachedOther)).Allowed, "a third member's connection")

	wildcard := []Grant{NewGrant(ScopeMCPAuth, WildcardResource)}
	p := attachedPolicies{credential: wildcard, agent: wildcard, agentOwner: wildcard, issuer: wildcard, owner: wildcard, ownerMember: true}
	require.False(t, decideAttached(t, p, true, personalClientCheck(attachedOther)).Allowed, "no grant reaches a connection nobody attached")
}

// Consent applies only to an agent acting for a represented user, in the
// consent's organization, and a shared admission carries only the current
// decision's consent.
func TestAttachedConnectionConsentNeedsAnAgentActingForSomeone(t *testing.T) {
	t.Parallel()

	own := []Grant{ownConnectionGrant()}
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
		DevMode: false, AdmitPrincipalCredential: nil, AdmitPrincipalCredentialWithDBTX: nil, AdmitWorkloadSession: nil, AdmitWorkloadSessionInTx: nil,
		LoadUserPolicy: func(context.Context, string, string) ([]Grant, bool, error) { return own, true, nil },
	})
	consent, err := engine.AttachedConnectionConsent(t.Context(), "org_123", attachedOwner)
	require.NoError(t, err)

	workload := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), RepresentedUserPolicy{UserID: "", Policy: nil}, false, own, own, own)
	authorization, err := engine.PrepareMandatory(workload)
	require.NoError(t, err)
	decision, err := authorization.WithAttachedConnection(consent).Decide("org_123", personalClientCheck(attachedOwner))
	require.NoError(t, err)
	require.False(t, decision.Allowed, "a caller acting for no one gets no consent")

	elsewhere, err := engine.AttachedConnectionConsent(t.Context(), "org_other", attachedOwner)
	require.NoError(t, err)
	agent := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), RepresentedUserPolicy{UserID: attachedIssuer, Policy: own}, false, own, own, own)
	authorization, err = engine.PrepareMandatory(agent)
	require.NoError(t, err)
	decision, err = authorization.WithAttachedConnection(elsewhere).Decide("org_123", personalClientCheck(attachedOwner))
	require.NoError(t, err)
	require.False(t, decision.Allowed, "consent from another organization is ignored")

	authorization, err = engine.PrepareMandatory(agent)
	require.NoError(t, err)
	decision, err = authorization.WithAttachedConnection(consent).Decide("org_123", personalClientCheck(attachedOwner))
	require.NoError(t, err)
	require.True(t, decision.Allowed, "the agent acting for someone gets the consent")
	decision, err = authorization.WithAttachedConnection(consent).WithAttachedConnection(AttachedConnectionConsent{}).Decide("org_123", personalClientCheck(attachedOwner))
	require.NoError(t, err)
	require.False(t, decision.Allowed, "a shared admission drops consent the current decision does not carry")
}

// A user calling for themselves is not an agent: consent changes nothing.
func TestAttachedConnectionConsentIgnoredForUsers(t *testing.T) {
	t.Parallel()

	own := []Grant{ownConnectionGrant()}
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
		DevMode: false, AdmitPrincipalCredential: nil, AdmitPrincipalCredentialWithDBTX: nil, AdmitWorkloadSession: nil, AdmitWorkloadSessionInTx: nil,
		LoadUserPolicy: func(context.Context, string, string) ([]Grant, bool, error) { return own, true, nil },
	})
	consent, err := engine.AttachedConnectionConsent(t.Context(), "org_123", attachedOwner)
	require.NoError(t, err)
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: attachedIssuer})
	ctx = GrantsToContext(ctx, own)
	authorization, err := engine.PrepareMandatory(ctx)
	require.NoError(t, err)
	require.Equal(t, MandatoryCallerUser, authorization.Caller())
	decision, err := authorization.WithAttachedConnection(consent).Decide("org_123", personalClientCheck(attachedOwner))
	require.NoError(t, err)
	require.False(t, decision.Allowed)
}
