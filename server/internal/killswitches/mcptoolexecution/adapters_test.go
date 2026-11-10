package mcptoolexecution

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAuthenticatedUserAdapterCanonicalizeUsesSessionSubjectByteLimit(t *testing.T) {
	t.Parallel()

	adapter := NewAuthenticatedUserPrincipalAdapter(nil)
	tests := []struct {
		name      string
		input     string
		supported bool
	}{
		{name: "ASCII at byte limit", input: strings.Repeat("a", urn.MaxSessionSubjectIDLength), supported: true},
		{name: "ASCII over byte limit", input: strings.Repeat("a", urn.MaxSessionSubjectIDLength+1), supported: false},
		{name: "multibyte at byte limit", input: strings.Repeat("é", urn.MaxSessionSubjectIDLength/2), supported: true},
		{name: "multibyte over byte limit", input: strings.Repeat("é", urn.MaxSessionSubjectIDLength/2+1), supported: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := adapter.Canonicalize("org", test.input)
			require.NoError(t, err)
			key, supported, err := result.Key()
			require.NoError(t, err)
			require.Equal(t, test.supported, supported)
			if supported {
				require.Equal(t, killswitches.PrincipalKey(test.input), key)
				_, err = urn.NewUserSubject(test.input).MarshalText()
				require.NoError(t, err)
			} else {
				_, err = urn.NewUserSubject(test.input).MarshalText()
				require.ErrorIs(t, err, urn.ErrInvalid)
			}
		})
	}
}

func TestMCPServerAdapterCanonicalizeRejectsNilUUID(t *testing.T) {
	t.Parallel()

	adapter := NewMCPServerResourceAdapter(nil)
	for _, input := range []string{uuid.Nil.String(), "  " + uuid.Nil.String() + "  "} {
		result, err := adapter.Canonicalize("org", input)
		require.NoError(t, err)
		_, supported, err := result.Key()
		require.NoError(t, err)
		require.False(t, supported)
	}

	id := uuid.New()
	result, err := adapter.Canonicalize("org", strings.ToUpper(id.String()))
	require.NoError(t, err)
	key, supported, err := result.Key()
	require.NoError(t, err)
	require.True(t, supported)
	require.Equal(t, killswitches.ResourceKey(id.String()), key)
}

func TestAuthenticatedUserAdapterDeriveCandidates(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_user_adapter")
	adapter := NewAuthenticatedUserPrincipalAdapter(conn)
	organization := killswitches.OrganizationID(orgID)

	activeUser := "user_" + uuid.NewString()
	insertUser(t, conn, activeUser, false)
	insertMembership(t, conn, orgID, activeUser, false)

	inactiveUser := "user_" + uuid.NewString()
	insertUser(t, conn, inactiveUser, false)
	insertMembership(t, conn, orgID, inactiveUser, true)

	deletedUser := "user_" + uuid.NewString()
	insertUser(t, conn, deletedUser, true)
	insertMembership(t, conn, orgID, deletedUser, false)

	otherOrg := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrg)
	crossOrgUser := "user_" + uuid.NewString()
	insertUser(t, conn, crossOrgUser, false)
	insertMembership(t, conn, otherOrg, crossOrgUser, false)

	result, err := adapter.DeriveCandidates(t.Context(), organization, testIdentity(t, mcpidentity.KindUserSession, activeUser))
	require.NoError(t, err)
	require.Equal(t, killswitches.PrincipalCandidateResultCandidates, result.Kind())
	require.Equal(t, []killswitches.PrincipalCandidate{{Kind: PrincipalKindUser, Key: killswitches.PrincipalKey(activeUser)}}, result.Candidates())

	for name, userID := range map[string]string{
		"inactive member":     inactiveUser,
		"deleted user":        deletedUser,
		"cross-tenant member": crossOrgUser,
		"unknown user":        "user_" + uuid.NewString(),
	} {
		result, err := adapter.DeriveCandidates(t.Context(), organization, testIdentity(t, mcpidentity.KindUserSession, userID))
		require.NoError(t, err, name)
		require.Equal(t, killswitches.PrincipalCandidateResultUnsupported, result.Kind(), name)
		require.Empty(t, result.Candidates(), name)
	}

	for name, identity := range map[string]mcpidentity.Identity{
		"anonymous":    testIdentity(t, mcpidentity.KindAnonymous, ""),
		"api key":      testIdentity(t, mcpidentity.KindAPIKey, ""),
		"assistant":    testIdentity(t, mcpidentity.KindAssistant, ""),
		"agent":        testIdentity(t, mcpidentity.KindAgent, ""),
		"chat session": testIdentity(t, mcpidentity.KindChatSession, ""),
		"workload":     testIdentity(t, mcpidentity.KindWorkload, ""),
	} {
		result, err := adapter.DeriveCandidates(t.Context(), organization, identity)
		require.NoError(t, err, name)
		require.Equal(t, killswitches.PrincipalCandidateResultUnsupported, result.Kind(), name)
	}

	// Values that merely name a user — caller strings, hollow or padded
	// authoritative claims, unknown provenance — are never quietly promoted.
	_, err = adapter.DeriveCandidates(t.Context(), organization, activeUser)
	require.Error(t, err)
	_, err = adapter.DeriveCandidates(t.Context(), organization, mcpidentity.Identity{})
	require.Error(t, err)
	_, err = adapter.DeriveCandidates(t.Context(), organization, testIdentity(t, mcpidentity.KindUserSession, " "+activeUser))
	require.Error(t, err)
	_, err = adapter.DeriveCandidates(t.Context(), organization, mcpidentity.Identity{})
	require.Error(t, err)
	_, err = adapter.DeriveCandidates(t.Context(), "", testIdentity(t, mcpidentity.KindUserSession, activeUser))
	require.Error(t, err)

	// A membership lookup failure is an infrastructure error, never an
	// unsupported classification and never a candidate.
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = adapter.DeriveCandidates(canceled, organization, testIdentity(t, mcpidentity.KindUserSession, activeUser))
	require.Error(t, err)
}

func TestAuthenticatedUserAdapterValidateCurrentOrganization(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_user_validate")
	adapter := NewAuthenticatedUserPrincipalAdapter(conn)
	organization := killswitches.OrganizationID(orgID)

	activeUser := "user_" + uuid.NewString()
	insertUser(t, conn, activeUser, false)
	insertMembership(t, conn, orgID, activeUser, false)

	valid, err := adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.PrincipalKey(activeUser))
	require.NoError(t, err)
	require.True(t, valid)

	valid, err = adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.PrincipalKey("user_"+uuid.NewString()))
	require.NoError(t, err)
	require.False(t, valid)

	valid, err = adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.PrincipalKey(" malformed "))
	require.NoError(t, err)
	require.False(t, valid)
}

func TestMCPServerAdapterDerive(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_server_adapter")
	adapter := NewMCPServerResourceAdapter(conn)
	organization := killswitches.OrganizationID(orgID)

	project := insertProject(t, conn, orgID, "proj-live", false)
	liveServer := insertMCPServer(t, conn, orgID, project, false)

	deletedServer := insertMCPServer(t, conn, orgID, project, true)

	deletedProject := insertProject(t, conn, orgID, "proj-deleted", true)
	orphanedServer := insertMCPServer(t, conn, orgID, deletedProject, false)

	otherOrg := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrg)
	otherProject := insertProject(t, conn, otherOrg, "proj-other", false)
	crossTenantServer := insertMCPServer(t, conn, otherOrg, otherProject, false)

	result, err := adapter.Derive(t.Context(), organization, ServerSource{FrontingServerID: uuid.NullUUID{UUID: liveServer, Valid: true}})
	require.NoError(t, err)
	key, supported, err := result.Key()
	require.NoError(t, err)
	require.True(t, supported)
	require.Equal(t, killswitches.ResourceKey(liveServer.String()), key)

	// The runtime-derived key and the management-time canonicalized key must
	// agree: hosted and private routes carry the same fronting server ID, so
	// both produce one canonical resource identity.
	canonical, err := adapter.Canonicalize(organization, liveServer.String())
	require.NoError(t, err)
	canonicalKey, supported, err := canonical.Key()
	require.NoError(t, err)
	require.True(t, supported)
	require.Equal(t, key, canonicalKey)

	canonical, err = adapter.Canonicalize(organization, uuid.Nil.String())
	require.NoError(t, err)
	_, supported, err = canonical.Key()
	require.NoError(t, err)
	require.False(t, supported, "the nil UUID can never name a live server")

	// A serving mode with no fronting server is deliberately unsupported.
	result, err = adapter.Derive(t.Context(), organization, ServerSource{FrontingServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	require.NoError(t, err)
	_, supported, err = result.Key()
	require.NoError(t, err)
	require.False(t, supported)

	// Stale or foreign servers are ownership failures, never unsupported and
	// never a key.
	for name, serverID := range map[string]uuid.UUID{
		"cross-tenant server":       crossTenantServer,
		"deleted server":            deletedServer,
		"server in deleted project": orphanedServer,
		"unknown server":            uuid.New(),
	} {
		_, err := adapter.Derive(t.Context(), organization, ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: true}})
		require.ErrorIs(t, err, ErrServerNotInOrganization, name)
	}

	// Caller-provided identifiers never reach ownership validation.
	_, err = adapter.Derive(t.Context(), organization, liveServer.String())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrServerNotInOrganization)

	_, err = adapter.Derive(t.Context(), "", ServerSource{FrontingServerID: uuid.NullUUID{UUID: liveServer, Valid: true}})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrServerNotInOrganization)

	// A lookup failure stays an infrastructure error distinct from an
	// ownership rejection.
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = adapter.Derive(canceled, organization, ServerSource{FrontingServerID: uuid.NullUUID{UUID: liveServer, Valid: true}})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrServerNotInOrganization)
}

func TestMCPServerAdapterValidateCurrentOrganization(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_server_validate")
	adapter := NewMCPServerResourceAdapter(conn)
	organization := killswitches.OrganizationID(orgID)

	project := insertProject(t, conn, orgID, "proj-validate", false)
	liveServer := insertMCPServer(t, conn, orgID, project, false)

	valid, err := adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.ResourceKey(liveServer.String()))
	require.NoError(t, err)
	require.True(t, valid)

	valid, err = adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.ResourceKey(uuid.NewString()))
	require.NoError(t, err)
	require.False(t, valid)

	// Stored canonical keys are lowercase; a non-canonical key is not current.
	valid, err = adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.ResourceKey("not-a-uuid"))
	require.NoError(t, err)
	require.False(t, valid)
}
