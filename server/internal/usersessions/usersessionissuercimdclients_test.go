package usersessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The create path scopes its INSERT ... SELECT source to a live issuer the
// caller can reach, so a sibling project's issuer yields no rows rather than an
// orphan write.
func TestCreateUserSessionIssuerCimdClient_SiblingProjectIssuerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "create-cimd-sibling")

	_, err := ti.service.CreateUserSessionIssuerCimdClient(ctx, &gen.CreateUserSessionIssuerCimdClientPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: sp.issuerID.String(),
		ClientIDMetadataURI: "https://attacker.example.com/client",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetUserSessionIssuerCimdClient_SiblingProjectNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "get-cimd-sibling")

	_, err := ti.service.GetUserSessionIssuerCimdClient(ctx, &gen.GetUserSessionIssuerCimdClientPayload{
		ID:               sp.cimdID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListUserSessionIssuerCimdClients_ExcludesSiblingProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "list-cimd-sibling")

	listed, err := ti.service.ListUserSessionIssuerCimdClients(ctx, &gen.ListUserSessionIssuerCimdClientsPayload{
		UserSessionIssuerID: sp.issuerID.String(),
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, listed.Items)
}

func TestDeleteUserSessionIssuerCimdClient_SiblingProjectNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "delete-cimd-sibling")

	err := ti.service.DeleteUserSessionIssuerCimdClient(ctx, &gen.DeleteUserSessionIssuerCimdClientPayload{
		ID:               sp.cimdID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	live, err := repo.New(ti.conn).GetUserSessionIssuerCimdClientByID(ctx, repo.GetUserSessionIssuerCimdClientByIDParams{
		ID:             sp.cimdID,
		ProjectID:      sp.projectID,
		OrganizationID: "",
	})
	require.NoError(t, err, "sibling project's CIMD grant must survive the caller's delete")
	require.False(t, live.Deleted)
}
