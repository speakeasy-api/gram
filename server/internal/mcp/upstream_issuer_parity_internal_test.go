package mcp

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

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
