package userblock_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	channelsauthnrepo "github.com/speakeasy-api/gram/server/internal/channels/authn/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/signins"
	signinsrepo "github.com/speakeasy-api/gram/server/internal/signins/repo"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/userblock"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestBlacklist_CascadesAllResourcesAndWritesAuditLogs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "userblock_cascade_full")
	require.NoError(t, err)

	uRepo := usersrepo.New(conn)
	oRepo := orgrepo.New(conn)
	pRepo := projectsrepo.New(conn)
	kRepo := keysrepo.New(conn)
	aRepo := accessrepo.New(conn)
	usRepo := usersessionsrepo.New(conn)
	pmRepo := platformrepo.New(conn)
	chRepo := channelsauthnrepo.New(conn)
	siRepo := signinsrepo.New(conn)

	const (
		blockedUserID   = "user-cascade-blocked"
		blockedWorkOSID = "workos-cascade-blocked"
		peerUserID      = "user-cascade-peer"
		adminUserID     = "user-cascade-admin"
		staffUserID     = "user-cascade-staff"
		deletedUserID   = "user-cascade-deleted"

		orgSoleID            = "org-cascade-sole"
		orgWithAdminID       = "org-cascade-with-admin"
		orgWithStaffID       = "org-cascade-with-staff"
		orgWithDeletedPeerID = "org-cascade-with-deleted"
		orgSharedID          = "org-cascade-shared"
		orgAlreadyDisabledID = "org-cascade-already-disabled"
		orgStaffID           = "org-cascade-staff-home"
	)

	// Seed users.
	for _, u := range []usersrepo.UpsertUserParams{
		{ID: blockedUserID, Email: "blocked@example.invalid", DisplayName: "Blocked User", PhotoUrl: conv.ToPGTextEmpty(""), Admin: false},
		{ID: peerUserID, Email: "peer@example.invalid", DisplayName: "Peer User", PhotoUrl: conv.ToPGTextEmpty(""), Admin: false},
		{ID: adminUserID, Email: "admin@example.invalid", DisplayName: "Platform Admin", PhotoUrl: conv.ToPGTextEmpty(""), Admin: true},
		{ID: staffUserID, Email: "staff@example.invalid", DisplayName: "Staff Member", PhotoUrl: conv.ToPGTextEmpty(""), Admin: false},
		{ID: deletedUserID, Email: "departed@example.invalid", DisplayName: "Departed User", PhotoUrl: conv.ToPGTextEmpty(""), Admin: false},
	} {
		_, err := uRepo.UpsertUser(ctx, u)
		require.NoError(t, err)
	}
	require.NoError(t, uRepo.SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		ID:       blockedUserID,
		WorkosID: conv.ToPGText(blockedWorkOSID),
	}))

	// Seed organizations.
	for _, org := range []orgrepo.CreateOrganizationMetadataParams{
		{ID: orgSoleID, Name: "Sole Org", Slug: "cascade-sole"},
		{ID: orgWithAdminID, Name: "Org With Admin", Slug: "cascade-with-admin"},
		{ID: orgWithStaffID, Name: "Org With Staff", Slug: "cascade-with-staff"},
		{ID: orgWithDeletedPeerID, Name: "Org With Deleted Peer", Slug: "cascade-with-deleted"},
		{ID: orgSharedID, Name: "Shared Org", Slug: "cascade-shared"},
		{ID: orgAlreadyDisabledID, Name: "Already Disabled Org", Slug: "cascade-already-disabled"},
		{ID: orgStaffID, Name: "Staff Home Org", Slug: "cascade-staff-home"},
		{ID: constants.DemoOrganizationID, Name: "Demo Workspace", Slug: "cascade-demo"},
	} {
		require.NoError(t, oRepo.CreateOrganizationMetadata(ctx, org))
	}

	// Pre-disable orgAlreadyDisabledID so we can assert its timestamp is preserved.
	disabledSeed, err := adminrepo.New(conn).SetOrganizationAccess(ctx, adminrepo.SetOrganizationAccessParams{
		ID:                 orgAlreadyDisabledID,
		Enabled:            false,
		ExpectedDisabledAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
	})
	require.NoError(t, err)
	require.True(t, disabledSeed.DisabledAt.Valid)
	originalDisabledAt := disabledSeed.DisabledAt.Time

	// Seed memberships.
	addMember := func(orgID, userID string) {
		t.Helper()
		_, err := oRepo.UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: orgID,
			UserID:         conv.ToPGText(userID),
		})
		require.NoError(t, err)
	}
	// Blocked user belongs to all 8 orgs (to verify each inclusion/exclusion rule).
	for _, orgID := range []string{
		orgSoleID, orgWithAdminID, orgWithStaffID, orgWithDeletedPeerID,
		orgSharedID, orgAlreadyDisabledID, orgStaffID, constants.DemoOrganizationID,
	} {
		addMember(orgID, blockedUserID)
	}
	addMember(orgWithAdminID, adminUserID)
	addMember(orgStaffID, staffUserID)
	addMember(orgWithStaffID, staffUserID)
	addMember(orgWithDeletedPeerID, deletedUserID)
	require.NoError(t, uRepo.SoftDeleteOrganizationUserFixture(ctx, usersrepo.SoftDeleteOrganizationUserFixtureParams{
		UserID:         deletedUserID,
		OrganizationID: orgWithDeletedPeerID,
	}))
	addMember(orgSharedID, peerUserID)

	// Projects for sole and shared orgs.
	soleProject, err := pRepo.CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Sole Project",
		Slug:           "sole-project",
		OrganizationID: orgSoleID,
	})
	require.NoError(t, err)
	sharedProject, err := pRepo.CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Shared Project",
		Slug:           "shared-project",
		OrganizationID: orgSharedID,
	})
	require.NoError(t, err)

	// API keys: ordinary key (with RBAC grant) + agent key by blocked user, plus peer's key.
	blockedKey, err := kRepo.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  orgSharedID,
		ProjectID:       uuid.NullUUID{UUID: sharedProject.ID, Valid: true},
		CreatedByUserID: blockedUserID,
		Name:            "Blocked Ordinary Key",
		KeyPrefix:       "sk_blk01",
		KeyHash:         "hash-blocked-ordinary-key",
		Scopes:          []string{"mcp:read"},
	})
	require.NoError(t, err)
	_, err = aRepo.UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: orgSharedID,
		PrincipalUrn:   urn.NewAPIKeyPrincipal(blockedKey.ID),
		Scope:          "project:read",
		Selectors:      []byte(`{"project_id":"` + sharedProject.ID.String() + `"}`),
	})
	require.NoError(t, err)

	blockedAgentKey, err := kRepo.CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID:         orgSoleID,
		CreatedByUserID:        blockedUserID,
		Name:                   "Blocked Agent Key",
		KeyPrefix:              "sk_agt01",
		KeyHash:                "hash-blocked-agent-key",
		SubjectUrn:             conv.ToPGText("agent:" + uuid.NewString()),
		DelegatedGrants:        []byte(`[{"scope":"mcp:read"}]`),
		DelegatedGrantsVersion: pgtype.Int4{Int32: 1, Valid: true},
		ExpiresAt:              conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour)),
	})
	require.NoError(t, err)

	peerKey, err := kRepo.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  orgSharedID,
		ProjectID:       uuid.NullUUID{UUID: sharedProject.ID, Valid: true},
		CreatedByUserID: peerUserID,
		Name:            "Peer Key",
		KeyPrefix:       "sk_per01",
		KeyHash:         "hash-peer-key",
		Scopes:          []string{"mcp:read"},
	})
	require.NoError(t, err)

	// Sign-in in orgSharedID for blocked user.
	recordedSignIn, err := signins.Record(ctx, conn, signins.RecordParams{
		OrganizationID:       orgSharedID,
		UserID:               blockedUserID,
		WorkOSSessionID:      "workos-session-cascade-1",
		AuthenticationMethod: "GoogleOAuth",
		Evidence: signins.Evidence{
			HostedDomain:        "",
			HostedDomainChecked: false,
			TenantID:            "",
			VerifiedEmail:       "blocked@example.invalid",
			IDPAuthenticatedAt:  time.Now().Add(-time.Minute),
		},
		AuthenticatedAt: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	require.True(t, recordedSignIn.ID.Valid)

	// Hosted MCP issuers, OAuth clients, sessions, and consents.
	soleIssuer, err := usRepo.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:               soleProject.ID,
		OrganizationID:          conv.ToPGText(orgSoleID),
		Slug:                    "sole-issuer",
		AuthnChallengeMode:      "required",
		SessionDuration:         pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
		AuthorizationServerMode: conv.ToPGText("endpoint"),
		UseAuthenticationHost:   pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)
	sharedIssuer, err := usRepo.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:               sharedProject.ID,
		OrganizationID:          conv.ToPGText(orgSharedID),
		Slug:                    "shared-issuer",
		AuthnChallengeMode:      "required",
		SessionDuration:         pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
		AuthorizationServerMode: conv.ToPGText("endpoint"),
		UseAuthenticationHost:   pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)

	// 1) Sole-org confidential DCR client used only by blocked user -> revoked.
	soleOwnedClient, err := usRepo.CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		ClientID:                "client-sole-owned",
		ClientSecretHash:        conv.ToPGText("secret-hash-sole"),
		ClientName:              "Sole Owned Confidential Client",
		RedirectUris:            []string{"https://client.example.invalid/cb"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: "client_secret_post",
		ClientJwks:              nil,
		ClientJwksUri:           conv.ToPGTextEmpty(""),
		UserSessionIssuerID:     soleIssuer.ID,
	})
	require.NoError(t, err)

	// 2) Shared-org public DCR client used only by blocked user -> revoked.
	sharedPublicOwnedClient, err := usRepo.CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		ClientID:                "client-shared-public-owned",
		ClientSecretHash:        conv.ToPGTextEmpty(""),
		ClientName:              "Blocked User Desktop Client",
		RedirectUris:            []string{"http://127.0.0.1:4321/cb"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: "none",
		ClientJwks:              nil,
		ClientJwksUri:           conv.ToPGTextEmpty(""),
		UserSessionIssuerID:     sharedIssuer.ID,
	})
	require.NoError(t, err)

	// 3) Shared-org public client used by BOTH blocked user and peer -> preserved.
	sharedPeerClient, err := usRepo.CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		ClientID:                "client-shared-both",
		ClientSecretHash:        conv.ToPGTextEmpty(""),
		ClientName:              "Shared Team Client",
		RedirectUris:            []string{"https://shared.example.invalid/cb"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: "none",
		ClientJwks:              nil,
		ClientJwksUri:           conv.ToPGTextEmpty(""),
		UserSessionIssuerID:     sharedIssuer.ID,
	})
	require.NoError(t, err)

	// 4) Shared-org confidential client used only by blocked user -> preserved in shared org.
	sharedConfidentialClient, err := usRepo.CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		ClientID:                "client-shared-confidential",
		ClientSecretHash:        conv.ToPGText("secret-hash-shared"),
		ClientName:              "Shared Org Confidential App",
		RedirectUris:            []string{"https://app.example.invalid/cb"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: "client_secret_basic",
		ClientJwks:              nil,
		ClientJwksUri:           conv.ToPGTextEmpty(""),
		UserSessionIssuerID:     sharedIssuer.ID,
	})
	require.NoError(t, err)

	// 5) CIMD client used by blocked user -> preserved.
	cimdUserSessionClient, err := usRepo.UpsertUserSessionClientFromCIMD(ctx, usersessionsrepo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     sharedIssuer.ID,
		ClientID:                "https://cimd.example.invalid/client.json",
		ClientName:              "CIMD Public Client",
		RedirectUris:            []string{"https://cimd.example.invalid/cb"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    conv.ToPGTextEmpty(""),
		TokenEndpointAuthMethod: "none",
		ClientJwks:              nil,
		ClientJwksUri:           conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	blockedSubject := urn.NewUserSubject(blockedUserID)
	peerSubject := urn.NewUserSubject(peerUserID)
	future := conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour))

	createUserSession := func(issuerID, clientID uuid.UUID, subject urn.SessionSubject, authorizer string, jti string, signInID uuid.NullUUID) usersessionsrepo.UserSession {
		t.Helper()
		signInUser := conv.ToPGTextEmpty("")
		if signInID.Valid {
			signInUser = conv.ToPGText(blockedUserID)
		}
		row, err := usRepo.CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
			UserSessionClientID:    uuid.NullUUID{UUID: clientID, Valid: true},
			SubjectUrn:             subject,
			AuthorizerUserID:       conv.ToPGTextEmpty(authorizer),
			DelegatedGrants:        nil,
			DelegatedGrantsVersion: pgtype.Int4{Int32: 0, Valid: false},
			Jti:                    jti,
			RefreshTokenHash:       conv.ToPGText("refresh-" + jti),
			RefreshExpiresAt:       future,
			ExpiresAt:              future,
			ToolSelection:          nil,
			Resource:               conv.ToPGTextEmpty(""),
			CredentialID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			UserSessionIssuerID:    issuerID,
			SignInID:               signInID,
			SignInUserID:           signInUser,
		})
		require.NoError(t, err)
		return row
	}

	// Sessions for blocked user: linked sign-in, unlinked sole-org, agent authorized by blocked user, shared/confidential/CIMD.
	linkedSession := createUserSession(sharedIssuer.ID, sharedPublicOwnedClient.ID, blockedSubject, "", "jti-blocked-linked", recordedSignIn.ID)
	soleSession := createUserSession(soleIssuer.ID, soleOwnedClient.ID, blockedSubject, "", "jti-blocked-sole", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	agentSession := createUserSession(sharedIssuer.ID, sharedConfidentialClient.ID, urn.NewAgentSubject(uuid.New()), blockedUserID, "jti-blocked-agent", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	cimdSession := createUserSession(sharedIssuer.ID, cimdUserSessionClient.ID, blockedSubject, "", "jti-blocked-cimd", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	sharedClientBlockedSession := createUserSession(sharedIssuer.ID, sharedPeerClient.ID, blockedSubject, "", "jti-blocked-shared-cli", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	peerSession := createUserSession(sharedIssuer.ID, sharedPeerClient.ID, peerSubject, "", "jti-peer-live", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	// Consents (and consent resource row).
	blockedConsent, err := usRepo.CreateUserSessionConsent(ctx, usersessionsrepo.CreateUserSessionConsentParams{
		SubjectUrn:          blockedSubject,
		RemoteSetHash:       "remote-hash-blocked",
		UserSessionClientID: sharedPublicOwnedClient.ID,
	})
	require.NoError(t, err)
	_, err = usRepo.CreateUserSessionConsentResource(ctx, usersessionsrepo.CreateUserSessionConsentResourceParams{
		ProjectID:            sharedProject.ID,
		UserSessionConsentID: blockedConsent.ID,
		Resource:             "https://mcp.example.invalid/server",
		InventoryHash:        "inv-hash-1",
		RemoteSetHash:        "remote-hash-blocked",
		ToolSelection:        nil,
		SessionDurationHours: 24,
		OrganizationID:       orgSharedID,
	})
	require.NoError(t, err)
	peerConsent, err := usRepo.CreateUserSessionConsent(ctx, usersessionsrepo.CreateUserSessionConsentParams{
		SubjectUrn:          peerSubject,
		RemoteSetHash:       "remote-hash-peer",
		UserSessionClientID: sharedPeerClient.ID,
	})
	require.NoError(t, err)

	// Platform MCP clients, connections, pending auth grant, and sessions.
	platformOwnedClient, err := pmRepo.CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID:              "pmcp-client-owned",
		ClientSecretHash:      conv.ToPGTextEmpty(""),
		ClientName:            "Blocked User Platform Client",
		RedirectUris:          []string{"http://127.0.0.1:5000/cb"},
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
	})
	require.NoError(t, err)
	platformSharedClient, err := pmRepo.CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID:              "pmcp-client-shared",
		ClientSecretHash:      conv.ToPGTextEmpty(""),
		ClientName:            "Shared Platform Client",
		RedirectUris:          []string{"https://shared-pmcp.example.invalid/cb"},
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
	})
	require.NoError(t, err)
	platformCIMDClient, err := pmRepo.UpsertPlatformMCPOAuthClientFromCIMD(ctx, platformrepo.UpsertPlatformMCPOAuthClientFromCIMDParams{
		ClientID:             "https://pmcp-cimd.example.invalid/client.json",
		ClientName:           "Shared CIMD Platform Client",
		RedirectUris:         []string{"https://pmcp-cimd.example.invalid/cb"},
		CacheTtlSeconds:      3600,
		ClientIDMetadataEtag: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	genOwned := uuid.New()
	blockedPlatformConnOwned, err := pmRepo.CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:                     uuid.New(),
		OrganizationID:         orgSoleID,
		SubjectUrn:             blockedSubject.String(),
		OauthClientID:          platformOwnedClient.ID,
		ActiveGeneration:       genOwned,
		AuthorizationExpiresAt: future,
	})
	require.NoError(t, err)

	genShared := uuid.New()
	blockedPlatformConnShared, err := pmRepo.CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:                     uuid.New(),
		OrganizationID:         orgSharedID,
		SubjectUrn:             blockedSubject.String(),
		OauthClientID:          platformSharedClient.ID,
		ActiveGeneration:       genShared,
		AuthorizationExpiresAt: future,
	})
	require.NoError(t, err)

	genCIMD := uuid.New()
	blockedPlatformConnCIMD, err := pmRepo.CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:                     uuid.New(),
		OrganizationID:         orgSharedID,
		SubjectUrn:             blockedSubject.String(),
		OauthClientID:          platformCIMDClient.ID,
		ActiveGeneration:       genCIMD,
		AuthorizationExpiresAt: future,
	})
	require.NoError(t, err)

	genPeer := uuid.New()
	peerPlatformConn, err := pmRepo.CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:                     uuid.New(),
		OrganizationID:         orgSharedID,
		SubjectUrn:             peerSubject.String(),
		OauthClientID:          platformSharedClient.ID,
		ActiveGeneration:       genPeer,
		AuthorizationExpiresAt: future,
	})
	require.NoError(t, err)

	_, err = pmRepo.CreatePlatformMCPAuthorizationGrant(ctx, platformrepo.CreatePlatformMCPAuthorizationGrantParams{
		OrganizationID:        orgSoleID,
		AuthorizationCodeHash: "pmcp-code-hash-blocked",
		OauthClientID:         platformOwnedClient.ID,
		ConnectionID:          blockedPlatformConnOwned.ID,
		ConnectionGeneration:  genOwned,
		RedirectUri:           "http://127.0.0.1:5000/cb",
		CodeChallenge:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExpiresAt:             future,
	})
	require.NoError(t, err)

	_, err = pmRepo.CreatePlatformMCPSession(ctx, platformrepo.CreatePlatformMCPSessionParams{
		ID:                   uuid.New(),
		OrganizationID:       orgSoleID,
		ConnectionID:         blockedPlatformConnOwned.ID,
		OauthClientID:        platformOwnedClient.ID,
		ConnectionGeneration: genOwned,
		Jti:                  "pmcp-jti-blocked-1",
		RefreshTokenHash:     "pmcp-refresh-blocked-1",
		ExpiresAt:            future,
		RefreshExpiresAt:     future,
	})
	require.NoError(t, err)
	_, err = pmRepo.CreatePlatformMCPSession(ctx, platformrepo.CreatePlatformMCPSessionParams{
		ID:                   uuid.New(),
		OrganizationID:       orgSharedID,
		ConnectionID:         blockedPlatformConnShared.ID,
		OauthClientID:        platformSharedClient.ID,
		ConnectionGeneration: genShared,
		Jti:                  "pmcp-jti-blocked-2",
		RefreshTokenHash:     "pmcp-refresh-blocked-2",
		ExpiresAt:            future,
		RefreshExpiresAt:     future,
	})
	require.NoError(t, err)
	_, err = pmRepo.CreatePlatformMCPSession(ctx, platformrepo.CreatePlatformMCPSessionParams{
		ID:                   uuid.New(),
		OrganizationID:       orgSharedID,
		ConnectionID:         peerPlatformConn.ID,
		OauthClientID:        platformSharedClient.ID,
		ConnectionGeneration: genPeer,
		Jti:                  "pmcp-jti-peer-1",
		RefreshTokenHash:     "pmcp-refresh-peer-1",
		ExpiresAt:            future,
		RefreshExpiresAt:     future,
	})
	require.NoError(t, err)

	// Channels OAuth grants and clients (sole-owned vs shared).
	require.NoError(t, chRepo.RegisterClient(ctx, channelsauthnrepo.RegisterClientParams{
		ClientID:                "ch-client-owned",
		ClientName:              "Blocked Channels Client",
		RedirectUris:            []string{"http://127.0.0.1:6000/cb"},
		SecretHash:              "",
		TokenEndpointAuthMethod: "none",
	}))
	require.NoError(t, chRepo.RegisterClient(ctx, channelsauthnrepo.RegisterClientParams{
		ClientID:                "ch-client-shared",
		ClientName:              "Shared Channels Client",
		RedirectUris:            []string{"https://shared-ch.example.invalid/cb"},
		SecretHash:              "",
		TokenEndpointAuthMethod: "none",
	}))
	blockedPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, blockedUserID).String()
	peerPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, peerUserID).String()
	blockedChGrantID, err := chRepo.CreateGrant(ctx, channelsauthnrepo.CreateGrantParams{
		ID:             uuid.New(),
		ClientID:       "ch-client-owned",
		OrganizationID: orgSharedID,
		ProjectID:      sharedProject.ID,
		Principal:      blockedPrincipal,
		Resource:       "https://channels.example.invalid/mcp",
		ExpiresAt:      future,
	})
	require.NoError(t, err)
	require.NoError(t, chRepo.CreateAccessToken(ctx, channelsauthnrepo.CreateAccessTokenParams{
		Jti:            "ch-jti-blocked",
		ExpiresAt:      future,
		GrantID:        blockedChGrantID,
		OrganizationID: orgSharedID,
		ProjectID:      sharedProject.ID,
	}))
	blockedSharedChGrantID, err := chRepo.CreateGrant(ctx, channelsauthnrepo.CreateGrantParams{
		ID:             uuid.New(),
		ClientID:       "ch-client-shared",
		OrganizationID: orgSharedID,
		ProjectID:      sharedProject.ID,
		Principal:      blockedPrincipal,
		Resource:       "https://channels.example.invalid/mcp",
		ExpiresAt:      future,
	})
	require.NoError(t, err)
	require.NoError(t, chRepo.CreateAccessToken(ctx, channelsauthnrepo.CreateAccessTokenParams{
		Jti:            "ch-jti-blocked-shared",
		ExpiresAt:      future,
		GrantID:        blockedSharedChGrantID,
		OrganizationID: orgSharedID,
		ProjectID:      sharedProject.ID,
	}))
	peerChGrantID, err := chRepo.CreateGrant(ctx, channelsauthnrepo.CreateGrantParams{
		ID:             uuid.New(),
		ClientID:       "ch-client-shared",
		OrganizationID: orgSharedID,
		ProjectID:      sharedProject.ID,
		Principal:      peerPrincipal,
		Resource:       "https://channels.example.invalid/mcp",
		ExpiresAt:      future,
	})
	require.NoError(t, err)
	require.NoError(t, chRepo.CreateAccessToken(ctx, channelsauthnrepo.CreateAccessTokenParams{
		Jti:            "ch-jti-peer",
		ExpiresAt:      future,
		GrantID:        peerChGrantID,
		OrganizationID: orgSharedID,
		ProjectID:      sharedProject.ID,
	}))

	// Prime the in-memory gate BEFORE blocking so we can verify Blacklist calls Forget(db).
	gate := userblock.NewGate(conn)
	wasBlocked, err := gate.Blocked(ctx, blockedUserID)
	require.NoError(t, err)
	require.False(t, wasBlocked)

	actorDisplay := "Staff Operator"
	res, err := userblock.Blacklist(ctx, conn, userblock.BlacklistParams{
		UserID: blockedUserID,
		Actor: userblock.Actor{
			Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, "staff-operator-oidc"),
			DisplayName: &actorDisplay,
		},
		Reason:            "abuse",
		StaffOrganization: staff.NewOrganization(orgStaffID),
		AuditLogger:       audit.NewLogger(),
		Logger:            logger,
	})
	require.NoError(t, err)

	// 1. User marked blacklisted and in-memory gate immediately invalidated.
	require.False(t, res.AlreadyBlacklisted)
	require.False(t, res.BlacklistedAt.IsZero())
	isBlockedNow, err := gate.Blocked(ctx, blockedUserID)
	require.NoError(t, err)
	require.True(t, isBlockedNow, "Blacklist must call Forget(db) after commit")

	// 2. Organizations: sole, admin-only-peer, staff-org-peer, deleted-peer disabled; shared, staff, demo, already-disabled untouched.
	disabledIDs := make([]string, 0, len(res.DisabledOrganizations))
	for _, org := range res.DisabledOrganizations {
		disabledIDs = append(disabledIDs, org.ID)
	}
	require.ElementsMatch(t, []string{
		orgSoleID,
		orgWithAdminID,
		orgWithStaffID,
		orgWithDeletedPeerID,
	}, disabledIDs)

	for _, id := range []string{orgSoleID, orgWithAdminID, orgWithStaffID, orgWithDeletedPeerID} {
		meta, err := oRepo.GetOrganizationMetadata(ctx, id)
		require.NoError(t, err)
		require.True(t, meta.DisabledAt.Valid, "expected org %s to be disabled", id)
	}
	for _, id := range []string{orgSharedID, orgStaffID, constants.DemoOrganizationID} {
		meta, err := oRepo.GetOrganizationMetadata(ctx, id)
		require.NoError(t, err)
		require.False(t, meta.DisabledAt.Valid, "expected org %s to remain enabled", id)
	}
	alreadyDisabledMeta, err := oRepo.GetOrganizationMetadata(ctx, orgAlreadyDisabledID)
	require.NoError(t, err)
	require.True(t, alreadyDisabledMeta.DisabledAt.Valid)
	require.True(t, alreadyDisabledMeta.DisabledAt.Time.Equal(originalDisabledAt), "already-disabled org must retain its original disabled_at")

	// 3. API keys soft-revoked via deleted_at + policy grants removed; peer key preserved.
	require.ElementsMatch(t, []uuid.UUID{blockedKey.ID, blockedAgentKey.ID}, res.RevokedAPIKeyIDs)
	revokedOrdinaryKey, err := kRepo.GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: blockedKey.ID, OrganizationID: orgSharedID})
	require.NoError(t, err)
	require.True(t, revokedOrdinaryKey.DeletedAt.Valid)
	require.True(t, revokedOrdinaryKey.Deleted)
	remainingGrants, err := aRepo.GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: orgSharedID,
		PrincipalUrns:  []string{urn.NewAPIKeyPrincipal(blockedKey.ID).String()},
	})
	require.NoError(t, err)
	require.Empty(t, remainingGrants, "ordinary key policy grants must be deleted")

	revokedAgentKeyRow, err := kRepo.GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: blockedAgentKey.ID, OrganizationID: orgSoleID})
	require.NoError(t, err)
	require.True(t, revokedAgentKeyRow.DeletedAt.Valid)
	require.True(t, revokedAgentKeyRow.Deleted)

	peerKeyRow, err := kRepo.GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: peerKey.ID, OrganizationID: orgSharedID})
	require.NoError(t, err)
	require.False(t, peerKeyRow.DeletedAt.Valid)
	require.False(t, peerKeyRow.Deleted)

	// 4. Sign-ins, user sessions, consents, and user-owned hosted OAuth clients revoked; peer/shared/CIMD preserved.
	require.Equal(t, 1, res.RevokedSignInCount)
	liveSignIn, err := signins.LiveSignInForSession(ctx, conn, orgSharedID, blockedUserID, "workos-session-cascade-1")
	require.NoError(t, err)
	require.False(t, liveSignIn.Valid)

	require.ElementsMatch(t, []uuid.UUID{
		linkedSession.ID,
		soleSession.ID,
		agentSession.ID,
		cimdSession.ID,
		sharedClientBlockedSession.ID,
	}, res.RevokedUserSessionIDs)
	for _, sess := range []struct {
		id    uuid.UUID
		orgID string
		proj  uuid.UUID
	}{
		{linkedSession.ID, orgSharedID, sharedProject.ID},
		{soleSession.ID, orgSoleID, soleProject.ID},
		{agentSession.ID, orgSharedID, sharedProject.ID},
		{cimdSession.ID, orgSharedID, sharedProject.ID},
		{sharedClientBlockedSession.ID, orgSharedID, sharedProject.ID},
	} {
		_, err := usRepo.GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{
			ID:             sess.id,
			ProjectID:      sess.proj,
			OrganizationID: sess.orgID,
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	_, err = usRepo.GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{
		ID:             peerSession.ID,
		ProjectID:      sharedProject.ID,
		OrganizationID: orgSharedID,
	})
	require.NoError(t, err, "peer user session must stay active")

	require.ElementsMatch(t, []uuid.UUID{blockedConsent.ID}, res.RevokedUserSessionConsentIDs)
	_, err = usRepo.GetUserSessionConsentByID(ctx, usersessionsrepo.GetUserSessionConsentByIDParams{
		ID:             blockedConsent.ID,
		ProjectID:      sharedProject.ID,
		OrganizationID: orgSharedID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	liveConsentResources, err := usRepo.CountLiveUserSessionConsentResourcesFixture(ctx, usersessionsrepo.CountLiveUserSessionConsentResourcesFixtureParams{
		ProjectID:  sharedProject.ID,
		ConsentIds: []uuid.UUID{blockedConsent.ID},
	})
	require.NoError(t, err)
	require.Zero(t, liveConsentResources)
	_, err = usRepo.GetUserSessionConsentByID(ctx, usersessionsrepo.GetUserSessionConsentByIDParams{
		ID:             peerConsent.ID,
		ProjectID:      sharedProject.ID,
		OrganizationID: orgSharedID,
	})
	require.NoError(t, err, "peer consent must stay active")

	require.ElementsMatch(t, []uuid.UUID{soleOwnedClient.ID, sharedPublicOwnedClient.ID}, res.RevokedUserSessionClientIDs)
	_, err = usRepo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{
		ID:             soleOwnedClient.ID,
		ProjectID:      soleProject.ID,
		OrganizationID: orgSoleID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = usRepo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{
		ID:             sharedPublicOwnedClient.ID,
		ProjectID:      sharedProject.ID,
		OrganizationID: orgSharedID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	for _, preservedID := range []uuid.UUID{sharedPeerClient.ID, sharedConfidentialClient.ID, cimdUserSessionClient.ID} {
		_, err := usRepo.GetUserSessionClientByID(ctx, usersessionsrepo.GetUserSessionClientByIDParams{
			ID:             preservedID,
			ProjectID:      sharedProject.ID,
			OrganizationID: orgSharedID,
		})
		require.NoError(t, err, "shared/confidential/CIMD client %s must stay active", preservedID)
	}

	// Verify durable revocation deliveries were enqueued (user_epoch + JTIs + workos_session).
	backlog, err := siRepo.RevocationDeliveryBacklog(ctx, 10)
	require.NoError(t, err)
	require.Positive(t, backlog.Pending)
	require.Contains(t, res.RevokedJTIs, "jti-blocked-linked")
	require.Contains(t, res.RevokedJTIs, "jti-blocked-agent")
	require.Contains(t, res.RevokedJTIs, "pmcp-jti-blocked-1")

	// 5. Platform MCP: owned DCR client revoked, shared/CIMD clients preserved, all blocked connections/grants/sessions revoked, peer preserved.
	require.ElementsMatch(t, []uuid.UUID{platformOwnedClient.ID}, res.RevokedPlatformMCPOAuthClients)
	_, err = pmRepo.GetActivePlatformMCPOAuthClientByClientID(ctx, "pmcp-client-owned")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = pmRepo.GetActivePlatformMCPOAuthClientByClientID(ctx, "pmcp-client-shared")
	require.NoError(t, err, "shared Platform MCP client must stay active")
	_, err = pmRepo.GetActivePlatformMCPOAuthClientByClientID(ctx, "https://pmcp-cimd.example.invalid/client.json")
	require.NoError(t, err, "CIMD Platform MCP client must stay active")

	require.ElementsMatch(t, []uuid.UUID{
		blockedPlatformConnOwned.ID,
		blockedPlatformConnShared.ID,
		blockedPlatformConnCIMD.ID,
	}, res.RevokedPlatformMCPConnectionID)
	for _, connCheck := range []struct {
		id    uuid.UUID
		orgID string
	}{
		{blockedPlatformConnOwned.ID, orgSoleID},
		{blockedPlatformConnShared.ID, orgSharedID},
		{blockedPlatformConnCIMD.ID, orgSharedID},
	} {
		row, err := pmRepo.GetPlatformMCPConnectionForUpdate(ctx, platformrepo.GetPlatformMCPConnectionForUpdateParams{
			ID:             connCheck.id,
			OrganizationID: connCheck.orgID,
		})
		require.NoError(t, err)
		require.True(t, row.RevokedAt.Valid)
		require.True(t, row.ReauthorizationRequiredAt.Valid)
		require.Equal(t, "connection_revoked", row.ReauthorizationReason.String)
	}
	_, err = pmRepo.GetActivePlatformMCPConnectionByID(ctx, platformrepo.GetActivePlatformMCPConnectionByIDParams{
		ID:             peerPlatformConn.ID,
		OrganizationID: orgSharedID,
	})
	require.NoError(t, err, "peer Platform MCP connection must stay active")

	_, err = pmRepo.GetPlatformMCPAuthorizationGrantForValidation(ctx, platformrepo.GetPlatformMCPAuthorizationGrantForValidationParams{
		OrganizationID:        orgSoleID,
		AuthorizationCodeHash: "pmcp-code-hash-blocked",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "revoked Platform MCP authorization grant must not validate")
	require.Len(t, res.RevokedPlatformMCPGrantIDs, 1)

	require.Len(t, res.RevokedPlatformMCPSessionIDs, 2)
	_, err = pmRepo.GetActivePlatformMCPSessionByJTI(ctx, platformrepo.GetActivePlatformMCPSessionByJTIParams{
		OrganizationID: orgSoleID,
		Jti:            "pmcp-jti-blocked-1",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = pmRepo.GetActivePlatformMCPSessionByJTI(ctx, platformrepo.GetActivePlatformMCPSessionByJTIParams{
		OrganizationID: orgSharedID,
		Jti:            "pmcp-jti-blocked-2",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = pmRepo.GetActivePlatformMCPSessionByJTI(ctx, platformrepo.GetActivePlatformMCPSessionByJTIParams{
		OrganizationID: orgSharedID,
		Jti:            "pmcp-jti-peer-1",
	})
	require.NoError(t, err, "peer Platform MCP session must stay active")

	// 6. Channels OAuth grants and sole-owned client revoked; shared client and peer grant preserved.
	require.ElementsMatch(t, []string{"ch-client-owned"}, res.RevokedChannelsOAuthClients)
	require.ElementsMatch(t, []uuid.UUID{blockedChGrantID, blockedSharedChGrantID}, res.RevokedChannelsOAuthGrantIDs)
	_, err = chRepo.GetClient(ctx, "ch-client-owned")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = chRepo.GetClient(ctx, "ch-client-shared")
	require.NoError(t, err)
	_, err = chRepo.GetAccessIdentity(ctx, channelsauthnrepo.GetAccessIdentityParams{
		Jti:            "ch-jti-blocked",
		OrganizationID: orgSharedID,
		ProjectID:      sharedProject.ID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = chRepo.GetAccessIdentity(ctx, channelsauthnrepo.GetAccessIdentityParams{
		Jti:            "ch-jti-peer",
		OrganizationID: orgSharedID,
		ProjectID:      sharedProject.ID,
	})
	require.NoError(t, err, "peer Channels OAuth access token must stay active")

	// 7. Audit logs written for block and every cascaded tenant resource.
	assertAuditCount := func(action audit.Action, want int64) {
		t.Helper()
		got, err := audittest.AuditLogCountByAction(ctx, conn, action)
		require.NoError(t, err)
		require.Equal(t, want, got, "unexpected audit count for %s", action)
	}
	assertAuditCount(audit.ActionOrganizationDisabled, 4)
	assertAuditCount(audit.ActionKeyRevoke, 2)
	assertAuditCount(audit.ActionAccessMemberSignInsRevoke, 2) // orgSharedID (sign-in + sessions) + orgSoleID (unlinked session)
	assertAuditCount(audit.ActionUserSessionRevoke, 5)
	assertAuditCount(audit.ActionUserSessionConsentRevoke, 1)
	assertAuditCount(audit.ActionUserSessionClientRevoke, 2)
	assertAuditCount(audit.ActionAccessMemberBlock, 8) // all 8 orgs the user belonged to

	blockAudit, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionAccessMemberBlock, blockedUserID)
	require.NoError(t, err)
	require.Equal(t, "staff-operator-oidc", blockAudit.ActorID)
	require.Equal(t, actorDisplay, blockAudit.ActorDisplay)
	blockMeta, err := audittest.DecodeAuditData(blockAudit.Metadata)
	require.NoError(t, err)
	require.Equal(t, "abuse", blockMeta["reason"])

	// 8. Re-running Blacklist on the already-blacklisted user is idempotent and writes no duplicate audit rows.
	totalAuditsBeforeRerun, err := audittest.AuditLogCount(ctx, conn)
	require.NoError(t, err)

	second, err := userblock.Blacklist(ctx, conn, userblock.BlacklistParams{
		UserID: blockedUserID,
		Actor: userblock.Actor{
			Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, "staff-operator-oidc"),
			DisplayName: &actorDisplay,
		},
		Reason:            "abuse",
		StaffOrganization: staff.NewOrganization(orgStaffID),
		AuditLogger:       audit.NewLogger(),
		Logger:            logger,
	})
	require.NoError(t, err)
	require.True(t, second.AlreadyBlacklisted)
	require.True(t, second.BlacklistedAt.Equal(res.BlacklistedAt))
	require.Empty(t, second.DisabledOrganizations)
	require.Empty(t, second.RevokedAPIKeyIDs)
	require.Zero(t, second.RevokedSignInCount)
	require.Empty(t, second.RevokedUserSessionIDs)
	require.Empty(t, second.RevokedUserSessionConsentIDs)
	require.Empty(t, second.RevokedUserSessionClientIDs)
	require.Empty(t, second.RevokedPlatformMCPConnectionID)
	require.Empty(t, second.RevokedPlatformMCPSessionIDs)
	require.Empty(t, second.RevokedPlatformMCPOAuthClients)
	require.Empty(t, second.RevokedChannelsOAuthGrantIDs)
	require.Empty(t, second.RevokedChannelsOAuthClients)

	totalAuditsAfterRerun, err := audittest.AuditLogCount(ctx, conn)
	require.NoError(t, err)
	require.Equal(t, totalAuditsBeforeRerun, totalAuditsAfterRerun, "idempotent re-run must not emit duplicate audit rows")
}

func TestBlacklist_UnknownUserReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "userblock_cascade_not_found")
	require.NoError(t, err)

	_, err = userblock.Blacklist(ctx, conn, userblock.BlacklistParams{
		UserID: "user-does-not-exist",
		Actor: userblock.Actor{
			Principal:   urn.NewSystemPrincipal("user-block"),
			DisplayName: nil,
		},
		Reason:            "test",
		StaffOrganization: staff.Organization{},
		AuditLogger:       audit.NewLogger(),
		Logger:            testenv.NewLogger(t),
	})
	require.ErrorIs(t, err, userblock.ErrUserNotFound)
}
