package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// refusalFixture is a mandatory permitter that records refusals, for a caller
// whose context newRefusalFixture returns.
type refusalFixture struct {
	permitter mandatoryPermitter
	orgID     string
	read      func() []*authzv1.Challenge
}

// newRefusalFixture returns the context of a caller, user_b, holding the
// standard grant that lets a member use their own personal connections.
func newRefusalFixture(t *testing.T) (context.Context, refusalFixture) {
	t.Helper()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_auth_client_refusal")
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	require.NoError(t, authclientbackfill.New(pool).SeedOrganizationFixture(t.Context(), orgID))

	ownConnections := authz.NewGrantWithSelector(authz.ScopeMCPAuth, authz.Selector{
		authz.SelectorKeyResourceKind:    authz.ResourceKindMCPAuthClient,
		authz.SelectorKeyResourceID:      authz.WildcardResource,
		authz.SelectorKeyConnectionOwner: authz.SelectorValueRepresentedUser,
		authz.SelectorKeyClientType:      authz.AuthClientTypeRemoteSession,
	})
	engine := authz.NewEngine(testenv.NewLogger(t), pool, func(context.Context, string) (bool, error) { return true, nil }, workos.NewStubClient())

	read := func() []*authzv1.Challenge {
		rows, err := testrepo.New(pool).ListPublishOutboxRows(t.Context())
		require.NoError(t, err)
		var challenges []*authzv1.Challenge
		for _, row := range rows {
			if row.Topic != string(proto.MessageName(&authzv1.Challenge{})) {
				continue
			}
			message := &authzv1.Challenge{}
			require.NoError(t, proto.Unmarshal(row.Message, message))
			challenges = append(challenges, message)
		}
		return challenges
	}

	session := "session_b"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID, UserID: "user_b", SessionID: &session, AccountType: "enterprise",
	})

	return authz.GrantsToContext(ctx, []authz.Grant{ownConnections}), refusalFixture{
		permitter: mandatoryPermitter{engine: engine, refusals: newOnceLog()},
		orgID:     orgID,
		read:      read,
	}
}

func (f refusalFixture) personalUse(owner string, serverID uuid.UUID) authClientPermitRequest {
	return authClientPermitRequest{
		OrganizationID: f.orgID, ProjectID: uuid.New(), ServerID: serverID, ClientID: uuid.New(), ClientType: authClientTypeRemoteSession,
		RegistrationID: validUUID(uuid.New()), CredentialKind: "", CredentialID: uuid.Nil,
		Subject: urn.SessionSubject{Kind: urn.SessionSubjectKindUser, ID: owner},
	}
}

func TestMandatoryPermitterRecordsARefusedUseOnce(t *testing.T) {
	t.Parallel()

	ctx, f := newRefusalFixture(t)
	serverID := uuid.New()
	use := f.personalUse("user_a", serverID)

	for range 3 {
		permitted, err := f.permitter.Permitted(ctx, use)
		require.NoError(t, err)
		require.False(t, permitted, "another member's personal connection serves only them")
	}

	challenges := f.read()
	require.Len(t, challenges, 1, "a refusal repeated on every request is recorded once")
	challenge := challenges[0]
	require.Equal(t, "user:user_b", challenge.GetPrincipalUrn())
	require.Equal(t, "deny", challenge.GetOutcome())
	require.Equal(t, "connection_owner_mismatch", challenge.GetReason())
	require.Equal(t, string(authz.ScopeMCPAuth), challenge.GetScope())
	require.Equal(t, use.ClientID.String(), challenge.GetResourceId())

	selector, err := authz.SelectorFromRow([]byte(challenge.GetSelector()))
	require.NoError(t, err)
	require.Equal(t, serverID.String(), selector[authz.ChallengeKeyMCPServerID])
	require.NotContains(t, selector, authz.SelectorKeyConnectionOwner)
	require.NotContains(t, selector, authz.SelectorKeyUpstreamClient)

	other := use
	other.ServerID = uuid.New()
	permitted, err := f.permitter.Permitted(ctx, other)
	require.NoError(t, err)
	require.False(t, permitted)
	require.Len(t, f.read(), 2, "a refusal on another server is its own check")
}

func TestMandatoryPermitterRecordsNothingForAnAllowedUse(t *testing.T) {
	t.Parallel()

	ctx, f := newRefusalFixture(t)

	permitted, err := f.permitter.Permitted(ctx, f.personalUse("user_b", uuid.New()))
	require.NoError(t, err)
	require.True(t, permitted, "a member's own personal connection serves them")
	require.Empty(t, f.read())
}

func TestMandatoryPermitterRecordsNothingForAPreview(t *testing.T) {
	t.Parallel()

	ctx, f := newRefusalFixture(t)

	use := f.personalUse("user_a", uuid.New())
	permitted, err := f.permitter.Permitted(authz.ForListing(ctx), use)
	require.NoError(t, err)
	require.False(t, permitted)
	require.Empty(t, f.read(), "a consent or catalog preview is not a use")

	permitted, err = f.permitter.Permitted(ctx, use)
	require.NoError(t, err)
	require.False(t, permitted)
	require.Len(t, f.read(), 1, "a preview does not hold back the next real refusal")
}
