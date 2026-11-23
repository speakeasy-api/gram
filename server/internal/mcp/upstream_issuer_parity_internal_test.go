package mcp

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestUpstreamIssuerKeys_AuthoritativeFollowsTheFlag(t *testing.T) {
	t.Parallel()

	column := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	association := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	keys := upstreamIssuerKeys{column: column, association: associationUpstreamIssuer{Provider: association, Providers: 1}, known: true}

	require.Equal(t, column, keys.authoritative(false))
	require.Equal(t, association, keys.other(false))
	require.Equal(t, association, keys.authoritative(true))
	require.Equal(t, column, keys.other(true))

	unknown := upstreamIssuerKeys{column: column, association: associationUpstreamIssuer{Provider: uuid.NullUUID{}, Providers: 0}, known: false}
	require.Equal(t, column, unknown.authoritative(true), "an unread association source never decides")
	require.Equal(t, upstreamIssuerSourceColumn, upstreamIssuerSource(true, unknown))
	require.Equal(t, upstreamIssuerSourceAssociations, upstreamIssuerSource(true, keys))
}

func TestUpstreamIssuerKeys_Outcome(t *testing.T) {
	t.Parallel()

	provider := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	keysWith := func(providers int64) upstreamIssuerKeys {
		sole := uuid.NullUUID{}
		if providers == 1 {
			sole = provider
		}
		return upstreamIssuerKeys{column: provider, association: associationUpstreamIssuer{Provider: sole, Providers: providers}, known: true}
	}

	require.Equal(t, upstreamIssuerMatch, keysWith(1).outcome(true))
	require.Equal(t, upstreamIssuerMatch, keysWith(0).outcome(true), "a server with no association that routes alike matches")
	require.Equal(t, upstreamIssuerDiffer, keysWith(1).outcome(false))
	require.Equal(t, upstreamIssuerNoAssociation, keysWith(0).outcome(false))
	require.Equal(t, upstreamIssuerSeveralProviders, keysWith(2).outcome(false))

	unknown := keysWith(1)
	unknown.known = false
	require.Equal(t, upstreamIssuerNoBoundClients, unknown.outcome(true))
}

func TestSameRoutedUpstream(t *testing.T) {
	t.Parallel()

	token := remotesessions.UpstreamToken{Token: "a", RemoteSessionClientID: uuid.New(), RemoteSessionID: uuid.New()}
	other := remotesessions.UpstreamToken{Token: "b", RemoteSessionClientID: uuid.New(), RemoteSessionID: uuid.New()}
	var none remotesessions.UpstreamToken

	require.True(t, sameRoutedUpstream(token, nil, token, nil))
	require.True(t, sameRoutedUpstream(none, nil, none, nil), "both anonymous")
	require.False(t, sameRoutedUpstream(token, nil, other, nil))
	require.False(t, sameRoutedUpstream(token, nil, none, nil))
	require.False(t, sameRoutedUpstream(token, nil, none, errors.New("refused")))

	duplicate := &upstreamRoutingError{reason: "duplicate_resource", detail: ""}
	unmatched := &upstreamRoutingError{reason: "no_match", detail: ""}
	require.True(t, sameRoutedUpstream(none, duplicate, none, &upstreamRoutingError{reason: "duplicate_resource", detail: "other detail"}))
	require.False(t, sameRoutedUpstream(none, duplicate, none, unmatched))
	require.True(t, sameRoutedUpstream(none, remotesessions.ErrNoValidToken, none, remotesessions.ErrNoValidToken))
}

func TestUpstreamIssuerReconsent(t *testing.T) {
	t.Parallel()

	require.Equal(t, upstreamIssuerReconsentUnchanged, upstreamIssuerReconsent("h", "h", "h", nil))
	require.Equal(t, upstreamIssuerReconsentForced, upstreamIssuerReconsent("h", "h", "x", nil))
	require.Equal(t, upstreamIssuerReconsentAlreadyInvalid, upstreamIssuerReconsent("old", "h", "x", nil))
	require.Equal(t, upstreamIssuerReconsentError, upstreamIssuerReconsent("h", "h", "", errors.New("hash failed")))
}

func TestRecordUpstreamIssuerParity_LogsADisagreementOnce(t *testing.T) {
	t.Parallel()

	s := &Service{upstreamIssuerParityLog: newOnceLog()}
	key := upstreamIssuerParity{
		organizationID:   "org_a",
		serverID:         uuid.New(),
		surface:          upstreamIssuerSurfaceDirect,
		site:             "route",
		keys:             upstreamIssuerKeys{column: uuid.NullUUID{}, association: associationUpstreamIssuer{Provider: uuid.NullUUID{}, Providers: 0}, known: true},
		outcome:          upstreamIssuerNoAssociation,
		fromAssociations: false,
	}
	s.recordUpstreamIssuerParity(t.Context(), discardLogger, key)
	require.False(t, s.upstreamIssuerParityLog.first("org_a|"+key.serverID.String()+"|direct|route|no_association"),
		"the first disagreement claims its dedup key")
}

func TestRecordUpstreamIssuerReconsent_LogsAForcedReconsentOncePerConsenter(t *testing.T) {
	t.Parallel()

	s := &Service{upstreamIssuerParityLog: newOnceLog()}
	serverID := uuid.New()
	check := upstreamIssuerReconsentCheck{
		organizationID: "org_a",
		serverID:       serverID,
		site:           "reuse",
		consenter:      "session-1",
		outcome:        upstreamIssuerReconsentForced,
	}
	s.recordUpstreamIssuerReconsent(t.Context(), discardLogger, check)
	require.False(t, s.upstreamIssuerParityLog.first("reconsent|org_a|"+serverID.String()+"|reuse|session-1|would_reconsent"))
	require.True(t, s.upstreamIssuerParityLog.first("reconsent|org_a|"+serverID.String()+"|reuse|session-2|would_reconsent"),
		"another consenter logs separately")

	check.outcome = upstreamIssuerReconsentUnchanged
	check.consenter = "session-3"
	s.recordUpstreamIssuerReconsent(t.Context(), discardLogger, check)
	require.True(t, s.upstreamIssuerParityLog.first("reconsent|org_a|"+serverID.String()+"|reuse|session-3|unchanged"),
		"an unchanged check logs nothing")
}

func TestRecordUpstreamIssuerParity_CountsErrorsWithoutPanicking(t *testing.T) {
	t.Parallel()

	s := &Service{upstreamIssuerParityLog: newOnceLog()}
	s.recordUpstreamIssuerParity(t.Context(), discardLogger, upstreamIssuerParity{
		organizationID:   "org_a",
		serverID:         uuid.New(),
		surface:          upstreamIssuerSurfaceConsent,
		site:             "fingerprint",
		keys:             upstreamIssuerKeys{column: uuid.NullUUID{}, association: associationUpstreamIssuer{Provider: uuid.NullUUID{}, Providers: 0}, known: false},
		outcome:          upstreamIssuerError,
		fromAssociations: true,
	})
}

func TestDirectUpstreamIssuer_RoutesBothSourcesAndFollowsTheFlag(t *testing.T) {
	t.Parallel()

	column := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	association := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	tokens := map[uuid.UUID]remotesessions.UpstreamToken{
		association.UUID: {Token: "association-token", RemoteSessionClientID: uuid.New(), RemoteSessionID: uuid.New()},
	}
	input := func(org string) directUpstreamIssuerInput {
		return directUpstreamIssuerInput{
			organizationID:   org,
			serverID:         uuid.New(),
			site:             upstreamIssuerSiteRoute,
			tokens:           tokens,
			upstreamResource: "",
			tunneled:         true,
			backendIssuerID:  column,
			choiceGoverns:    false,
			association:      &remotesessions.ServerAssociationIssuer{Known: true, Providers: 1, Provider: association},
		}
	}
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagUpstreamIssuerFromAssociations, "org_on", true)
	s := &Service{upstreamIssuerParityLog: newOnceLog(), features: flags}

	require.Equal(t, column, s.directUpstreamIssuer(t.Context(), discardLogger, input("org_off")), "off, the column routes")
	require.Equal(t, association, s.directUpstreamIssuer(t.Context(), discardLogger, input("org_on")), "on, the associations route")

	choice := input("org_on")
	choice.choiceGoverns = true
	require.Equal(t, column, s.directUpstreamIssuer(t.Context(), discardLogger, choice), "a governing choice is kept")

	unread := input("org_on")
	unread.association = nil
	require.Equal(t, column, s.directUpstreamIssuer(t.Context(), discardLogger, unread), "unread associations never decide")

	unbound := input("org_on")
	unbound.association = &remotesessions.ServerAssociationIssuer{Known: false, Providers: 0, Provider: uuid.NullUUID{}}
	require.Equal(t, column, s.directUpstreamIssuer(t.Context(), discardLogger, unbound))
}

func TestDirectUpstreamIssuer_RemoteGrantsRouteByResourceUnderEitherSource(t *testing.T) {
	t.Parallel()

	column := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	association := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	grant := remotesessions.UpstreamToken{Token: "grant", Resource: "https://upstream.example.com/mcp", RemoteSessionClientID: uuid.New(), RemoteSessionID: uuid.New()}
	tokens := map[uuid.UUID]remotesessions.UpstreamToken{association.UUID: grant}

	keys := upstreamIssuerKeys{column: column, association: associationUpstreamIssuer{Provider: association, Providers: 1}, known: true}
	columnToken, columnErr := routeUpstreamToken(t.Context(), discardLogger, tokens, grant.Resource, false, keys.column)
	associationToken, associationErr := routeUpstreamToken(t.Context(), discardLogger, tokens, grant.Resource, false, keys.association.Provider)
	require.Equal(t, upstreamIssuerMatch, keys.outcome(sameRoutedUpstream(columnToken, columnErr, associationToken, associationErr)),
		"a subject's grant routes by its recorded resource, so the source does not matter")
}

func TestMemberUpstreamIssuer_OtherSourceAndGoverningChoice(t *testing.T) {
	t.Parallel()

	column := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	association := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	keys := upstreamIssuerKeys{column: column, association: associationUpstreamIssuer{Provider: association, Providers: 1}, known: true}
	member := metaMember{remoteSessionIssuerID: column, upstreamIssuer: keys, upstreamIssuerFromAssociations: false, tunneledServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, slug: "member"}

	require.False(t, memberChoiceGoverns(member))
	require.Equal(t, association, memberWithOtherIssuer(member).remoteSessionIssuerID)

	on := member
	on.upstreamIssuerFromAssociations = true
	on.remoteSessionIssuerID = association
	require.False(t, memberChoiceGoverns(on))
	require.Equal(t, column, memberWithOtherIssuer(on).remoteSessionIssuerID)

	chosen := member
	chosen.chosenClientID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	require.True(t, memberChoiceGoverns(chosen))

	replaced := member
	replaced.remoteSessionIssuerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	require.True(t, memberChoiceGoverns(replaced), "a consent-validated client replacing the issuer governs")

	tokens := map[uuid.UUID]remotesessions.UpstreamToken{association.UUID: {Token: "association-token", RemoteSessionClientID: uuid.New(), RemoteSessionID: uuid.New()}}
	routed, routeErr := routeMetaMemberToken(tokens, member, "")
	other, otherErr := routeMetaMemberToken(tokens, memberWithOtherIssuer(member), "")
	require.Equal(t, upstreamIssuerDiffer, keys.outcome(sameRoutedUpstream(routed, routeErr, other, otherErr)),
		"a tunneled member keyed by the column dials anonymously where its associations would send their credential")

	hosted, hostedErr := hostedMemberTokens(tokens, member)
	otherHosted, otherHostedErr := hostedMemberTokens(tokens, memberWithOtherIssuer(member))
	require.Empty(t, hosted)
	require.Equal(t, "association-token", soleToken(otherHosted).Token)
	require.False(t, sameRoutedUpstream(soleToken(hosted), hostedErr, soleToken(otherHosted), otherHostedErr))
}
