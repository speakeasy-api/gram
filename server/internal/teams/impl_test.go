package teams_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/common"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	gen "github.com/speakeasy-api/gram/server/gen/teams"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	testWorkOSOrgID     = "org_workos_test"
	testWorkOSUserID    = "user_workos_test"
	testWorkOSInviterID = "user_workos_inviter"
)

// workosStub builds a mock WorkOS httptest server and returns a *workos.WorkOS backed by it.
func workosStub(t *testing.T, handler http.Handler) *workos.WorkOS {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := usermanagement.NewClient("test_api_key")
	client.Endpoint = srv.URL
	client.HTTPClient = srv.Client()

	return workos.NewForTest(slog.Default(), client)
}

func jsonResp(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// seedWorkOSIDs sets workos_id on the test user and organization so the service
// can resolve Gram IDs to WorkOS IDs.
func seedWorkOSIDs(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext) {
	t.Helper()
	ctx := context.Background()

	_, err := ti.conn.Exec(ctx,
		"UPDATE organization_metadata SET workos_id = $1 WHERE id = $2",
		testWorkOSOrgID, authCtx.ActiveOrganizationID,
	)
	require.NoError(t, err)

	queries := userRepo.New(ti.conn)
	err = queries.SetUserWorkosID(ctx, userRepo.SetUserWorkosIDParams{
		WorkosID: pgtype.Text{String: testWorkOSUserID, Valid: true},
		ID:       authCtx.UserID,
	})
	require.NoError(t, err)
}

// seedInviterUser creates a user in the local DB with the given WorkOS ID and display name,
// so that resolveInviterName can look them up via GetUserByWorkosID.
func seedInviterUser(t *testing.T, ti *testInstance, workosUserID, displayName string) {
	t.Helper()
	ctx := context.Background()

	queries := userRepo.New(ti.conn)

	gramUserID := "inviter-" + workosUserID
	_, err := queries.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          gramUserID,
		Email:       gramUserID + "@example.com",
		DisplayName: displayName,
	})
	require.NoError(t, err)

	err = queries.SetUserWorkosID(ctx, userRepo.SetUserWorkosIDParams{
		WorkosID: pgtype.Text{String: workosUserID, Valid: true},
		ID:       gramUserID,
	})
	require.NoError(t, err)
}

func TestListMembers(t *testing.T) {
	t.Parallel()

	t.Run("returns members from local DB", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestTeamsService(t, nil)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)

		result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		require.NoError(t, err)
		require.NotEmpty(t, result.Members, "should include the test user seeded by InitAuthContext")
		assert.Equal(t, authCtx.UserID, result.Members[0].ID)
		assert.NotEmpty(t, result.Members[0].JoinedAt)
	})

	t.Run("rejects mismatched organization ID", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestTeamsService(t, nil)

		_, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{
			OrganizationID: "wrong-org-id",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})
}

func TestInviteMember(t *testing.T) {
	t.Parallel()

	t.Run("sends invitation and resolves inviter name", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_01",
				Email:          "newuser@example.com",
				State:          usermanagement.Pending,
				OrganizationID: testWorkOSOrgID,
				InviterUserID:  testWorkOSUserID,
				ExpiresAt:      "2026-04-02T00:00:00Z",
				CreatedAt:      "2026-03-26T00:00:00Z",
				UpdatedAt:      "2026-03-26T00:00:00Z",
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		result, err := ti.service.InviteMember(ctx, &gen.InviteMemberPayload{
			OrganizationID: authCtx.ActiveOrganizationID,
			Email:          "newuser@example.com",
		})
		require.NoError(t, err)
		assert.Equal(t, "inv_01", result.Invite.ID)
		assert.Equal(t, "newuser@example.com", result.Invite.Email)
		assert.Equal(t, string(usermanagement.Pending), result.Invite.Status)
		// InvitedBy is resolved from the local DB via the test user's display name
		assert.NotEmpty(t, result.Invite.InvitedBy)
	})

	t.Run("rejects mismatched organization ID", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestTeamsService(t, nil)

		_, err := ti.service.InviteMember(ctx, &gen.InviteMemberPayload{
			OrganizationID: "wrong-org-id",
			Email:          "someone@example.com",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})

	t.Run("returns error when WorkOS is not configured", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestTeamsService(t, nil)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)

		_, err := ti.service.InviteMember(ctx, &gen.InviteMemberPayload{
			OrganizationID: authCtx.ActiveOrganizationID,
			Email:          "someone@example.com",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestListInvites(t *testing.T) {
	t.Parallel()

	t.Run("filters to only pending invitations", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.ListInvitationsResponse{
				Data: []usermanagement.Invitation{
					{ID: "inv_01", Email: "a@example.com", State: usermanagement.Pending, InviterUserID: testWorkOSInviterID, CreatedAt: "2026-03-26T00:00:00Z", ExpiresAt: "2026-04-02T00:00:00Z"},
					{ID: "inv_02", Email: "b@example.com", State: usermanagement.Accepted, CreatedAt: "2026-03-25T00:00:00Z", ExpiresAt: "2026-04-01T00:00:00Z"},
					{ID: "inv_03", Email: "c@example.com", State: usermanagement.Revoked, CreatedAt: "2026-03-24T00:00:00Z", ExpiresAt: "2026-03-31T00:00:00Z"},
				},
				ListMetadata: common.ListMetadata{},
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)
		seedInviterUser(t, ti, testWorkOSInviterID, "Alice Inviter")

		result, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		require.NoError(t, err)
		require.Len(t, result.Invites, 1, "only pending invites should be returned")
		assert.Equal(t, "inv_01", result.Invites[0].ID)
		assert.Equal(t, string(usermanagement.Pending), result.Invites[0].Status)
		assert.Equal(t, "Alice Inviter", result.Invites[0].InvitedBy)
	})
}

func TestCancelInvite(t *testing.T) {
	t.Parallel()

	t.Run("cancels invite that belongs to org", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations/inv_01", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_01",
				Email:          "target@example.com",
				State:          usermanagement.Pending,
				OrganizationID: testWorkOSOrgID,
			})
		})
		mux.HandleFunc("/user_management/invitations/inv_01/revoke", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:    "inv_01",
				State: usermanagement.Revoked,
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		err := ti.service.CancelInvite(ctx, &gen.CancelInvitePayload{
			InviteID: "inv_01",
		})
		require.NoError(t, err)
	})

	t.Run("rejects invite belonging to another org", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations/inv_02", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_02",
				Email:          "victim@example.com",
				State:          usermanagement.Pending,
				OrganizationID: "org_different",
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		err := ti.service.CancelInvite(ctx, &gen.CancelInvitePayload{
			InviteID: "inv_02",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestResendInvite(t *testing.T) {
	t.Parallel()

	t.Run("resends invite and resolves inviter name", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations/inv_01", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_01",
				Email:          "resend@example.com",
				State:          usermanagement.Pending,
				OrganizationID: testWorkOSOrgID,
			})
		})
		mux.HandleFunc("/user_management/invitations/inv_01/resend", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_01",
				Email:          "resend@example.com",
				State:          usermanagement.Pending,
				InviterUserID:  testWorkOSInviterID,
				OrganizationID: testWorkOSOrgID,
				ExpiresAt:      "2026-04-02T00:00:00Z",
				CreatedAt:      "2026-03-26T00:00:00Z",
				UpdatedAt:      "2026-03-26T00:00:00Z",
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)
		seedInviterUser(t, ti, testWorkOSInviterID, "Jane Doe")

		result, err := ti.service.ResendInvite(ctx, &gen.ResendInvitePayload{
			InviteID: "inv_01",
		})
		require.NoError(t, err)
		assert.Equal(t, "inv_01", result.Invite.ID)
		assert.Equal(t, "Jane Doe", result.Invite.InvitedBy, "inviter name should be resolved from local DB")
	})

	t.Run("rejects invite belonging to another org", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations/inv_02", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_02",
				OrganizationID: "org_different",
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		_, err := ti.service.ResendInvite(ctx, &gen.ResendInvitePayload{
			InviteID: "inv_02",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestRemoveMember(t *testing.T) {
	t.Parallel()

	t.Run("prevents self-removal", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestTeamsService(t, nil)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)

		err := ti.service.RemoveMember(ctx, &gen.RemoveMemberPayload{
			OrganizationID: authCtx.ActiveOrganizationID,
			UserID:         authCtx.UserID,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot remove yourself")
	})

	t.Run("rejects mismatched organization ID", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestTeamsService(t, nil)

		err := ti.service.RemoveMember(ctx, &gen.RemoveMemberPayload{
			OrganizationID: "wrong-org-id",
			UserID:         "other-user",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})
}

func TestGetInviteInfo(t *testing.T) {
	t.Parallel()

	t.Run("returns invite info by token", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations/by_token/tok_valid", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_01",
				Email:          "invited@example.com",
				State:          usermanagement.Pending,
				Token:          "tok_valid",
				OrganizationID: testWorkOSOrgID,
				InviterUserID:  testWorkOSInviterID,
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		// Seed org WorkOS ID so org name can be resolved, and inviter user for display name.
		seedWorkOSIDs(t, ti, authCtx)
		seedInviterUser(t, ti, testWorkOSInviterID, "The Boss")

		result, err := ti.service.GetInviteInfo(ctx, &gen.GetInviteInfoPayload{
			Token: "tok_valid",
		})
		require.NoError(t, err)
		assert.Equal(t, "The Boss", result.InviterName)
		assert.Equal(t, "invited@example.com", result.Email)
		assert.Equal(t, string(usermanagement.Pending), result.Status)
		assert.NotEmpty(t, result.OrganizationName)
	})

	t.Run("returns error for invalid token", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations/by_token/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		_, err := ti.service.GetInviteInfo(ctx, &gen.GetInviteInfoPayload{
			Token: "tok_invalid",
		})
		require.Error(t, err)
	})
}
