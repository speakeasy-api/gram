package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestSelectConsentAuthClientPreselection(t *testing.T) {
	t.Parallel()

	option := func(id uuid.UUID, name, clientType string, state consentAuthClientState) consentAuthClientOption {
		return consentAuthClientOption{ID: id, Name: name, Type: clientType, State: state}
	}
	personal, marketing, engineering, forwarded := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	options := func() []consentAuthClientOption {
		return []consentAuthClientOption{
			option(forwarded, "Forwarded", authClientTypePassThrough, consentClientReady),
			option(personal, "Own account", authClientTypeRemoteSession, consentClientReady),
			option(marketing, "marketing", authClientTypeServiceAccount, consentClientReady),
			option(engineering, "engineering", authClientTypeStaticCredential, consentClientReady),
		}
	}
	selected := func(picked, remembered uuid.UUID, edit func([]consentAuthClientOption)) uuid.UUID {
		auth := &consentServerAuth{ServerID: uuid.New(), Status: consentAuthChoose, Options: options()}
		if edit != nil {
			edit(auth.Options)
		}
		selectConsentAuthClient(auth, picked, remembered)
		if s := auth.Selected(); s != nil {
			return s.ID
		}
		return uuid.Nil
	}

	require.Equal(t, engineering, selected(uuid.Nil, uuid.Nil, nil), "shared clients come first, by name")
	require.Equal(t, personal, selected(uuid.Nil, personal, nil), "a still-usable remembered choice wins over the order")
	require.Equal(t, forwarded, selected(forwarded, personal, nil), "the person's pick wins over the remembered choice")
	require.Equal(t, engineering, selected(uuid.Nil, uuid.New(), nil), "a remembered client no longer offered is ignored")

	ema := uuid.New()
	withEMA := func(options []consentAuthClientOption) {
		options[0] = option(ema, "Identity provider", authClientTypeEMA, consentClientReady)
	}
	require.Equal(t, ema, selected(uuid.Nil, uuid.Nil, withEMA), "a ready identity-provider client comes before everything else")

	connectFirst := func(options []consentAuthClientOption) {
		for i := range options {
			if options[i].ID == personal {
				options[i].State = consentClientConnect
			}
		}
	}
	require.Equal(t, engineering, selected(personal, personal, connectFirst), "a pick that still needs connecting is not selected")

	auth := &consentServerAuth{ServerID: uuid.New(), Status: consentAuthChoose, Options: []consentAuthClientOption{option(personal, "Own account", authClientTypeRemoteSession, consentClientConnect)}}
	selectConsentAuthClient(auth, uuid.Nil, uuid.Nil)
	require.Nil(t, auth.Selected(), "nothing that cannot be approved now is selected")
	require.False(t, auth.Approvable())
	require.NotEmpty(t, auth.Problem())
}

func TestConsentAuthChoices(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_consent_auth_choices")
	require.NoError(t, err)
	user := urn.NewUserSubject("user_1")

	choices := func(t *testing.T, f *authClientFixture, subject urn.SessionSubject, cards []remoteSessionCard, picked uuid.UUID) *consentServerAuth {
		t.Helper()
		got, err := f.service.consentAuthChoices(t.Context(), f.orgID, subject, []consentAuthServer{{id: f.serverID, projectID: f.projectID}}, cards, map[uuid.UUID]uuid.UUID{f.serverID: picked})
		require.NoError(t, err)
		return got[f.serverID]
	}
	staticClient := func(f *authClientFixture) uuid.UUID {
		id := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
		f.copyHeader(id, "X-Api-Key", false, false, text("plain-value"), pgtype.Text{})
		return id
	}

	t.Run("offers every usable client and preselects a shared one", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		shared := staticClient(f)
		personal := f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
		card := remoteSessionCard{ClientID: f.registration.String(), Connected: true}

		auth := choices(t, f, user, []remoteSessionCard{card}, uuid.Nil)
		require.Equal(t, consentAuthChoose, auth.Status)
		require.Len(t, auth.Options, 2)
		require.True(t, auth.ShowPicker())
		require.Equal(t, shared, auth.Selected().ID)

		auth = choices(t, f, user, []remoteSessionCard{card}, personal)
		require.Equal(t, personal, auth.Selected().ID, "the person's pick is selected")
		require.Equal(t, validUUID(f.registration), auth.Selected().registrationID)
	})

	t.Run("a personal client asks for a connection before it can be chosen", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))

		auth := choices(t, f, user, []remoteSessionCard{{ClientID: f.registration.String()}}, uuid.Nil)
		require.Len(t, auth.Options, 1)
		require.Equal(t, consentClientConnect, auth.Options[0].State)
		require.Equal(t, f.registration.String(), auth.Options[0].ConnectClientID)
		require.False(t, auth.Approvable())

		auth = choices(t, f, user, nil, uuid.Nil)
		require.Equal(t, consentClientSetup, auth.Options[0].State, "a registration this sign-in cannot connect needs setup")
	})

	t.Run("an expired personal connection asks to be reconnected", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))

		auth := choices(t, f, user, []remoteSessionCard{{ClientID: f.registration.String(), Connected: true, Expired: true}}, uuid.Nil)
		require.Equal(t, consentClientReconnect, auth.Options[0].State)
		require.Equal(t, f.registration.String(), auth.Options[0].ConnectClientID)
		require.False(t, auth.Approvable())
	})

	t.Run("a caller representing nobody is not offered a personal client", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		shared := staticClient(f)
		f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))

		auth := choices(t, f, urn.NewWorkloadSubject(uuid.New(), "ci"), []remoteSessionCard{{ClientID: f.registration.String(), Connected: true}}, uuid.Nil)
		require.Len(t, auth.Options, 1)
		require.Equal(t, shared, auth.Options[0].ID)
		require.False(t, auth.ShowPicker())
	})

	t.Run("clients the person may not use read as no access", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.service.authClientPermitter = denyPermitter{}
		staticClient(f)

		auth := choices(t, f, user, nil, uuid.Nil)
		require.Equal(t, consentAuthNoAccess, auth.Status)
		require.False(t, auth.Approvable())
	})

	t.Run("no authentication and incomplete setup are told apart", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(t.Context(), authclientbackfill.SetRequiresUpstreamAuthFixtureParams{ID: f.serverID, RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}}))
		auth := choices(t, f, user, nil, uuid.Nil)
		require.Equal(t, consentAuthSetupIncomplete, auth.Status)
		require.False(t, auth.Approvable())

		require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(t.Context(), authclientbackfill.SetRequiresUpstreamAuthFixtureParams{ID: f.serverID, RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true}}))
		auth = choices(t, f, user, nil, uuid.Nil)
		require.Equal(t, consentAuthNoAuth, auth.Status)
		require.True(t, auth.Approvable())
		require.Nil(t, auth.Selected())
	})
}

func TestPersonalReadinessOfIdentityProviderClients(t *testing.T) {
	t.Parallel()

	registration := uuid.New()
	readiness := func(card remoteSessionCard) (consentAuthClientState, string) {
		card.ClientID = registration.String()
		return personalReadiness(authClientTypeEMA, validUUID(registration), []remoteSessionCard{card})
	}
	state, connect := readiness(remoteSessionCard{})
	require.Equal(t, consentClientConnect, state, "a person who has not signed in to the identity provider can connect it")
	require.Equal(t, registration.String(), connect)
	state, connect = readiness(remoteSessionCard{Connected: true, Expired: true})
	require.Equal(t, consentClientReconnect, state)
	require.Equal(t, registration.String(), connect)
}
