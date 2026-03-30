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
	testWorkOSOrgID  = "org_workos_test"
	testWorkOSUserID = "user_workos_test"
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

	err = userRepo.New(ti.conn).SetUserWorkosID(ctx, userRepo.SetUserWorkosIDParams{
		WorkosID: pgtype.Text{String: testWorkOSUserID, Valid: true},
		ID:       authCtx.UserID,
	})
	require.NoError(t, err)
}

// --- ListMembers ---

func TestListMembers(t *testing.T) {
	t.Parallel()

	t.Run("returns members mapped to Gram user IDs", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		// ListUsersInOrg
		mux.HandleFunc("/user_management/users", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.ListUsersResponse{
				Data: []usermanagement.User{
					{ID: testWorkOSUserID, Email: "alice@example.com", FirstName: "Alice", LastName: "Smith"},
				},
				ListMetadata: common.ListMetadata{},
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		// The WorkOS user is resolved to a Gram user — should have at least one member
		assert.NotEmpty(t, result.Members)
		// Gram user ID should NOT be the WorkOS ID
		for _, m := range result.Members {
			assert.NotEqual(t, testWorkOSUserID, m.ID, "member ID should be Gram user ID, not WorkOS ID")
		}
	})

	t.Run("rejects mismatched organization ID", func(t *testing.T) {
		t.Parallel()

		wos := workosStub(t, http.NewServeMux())
		ctx, ti := newTestTeamsService(t, wos)

		_, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{
			OrganizationID: "wrong-org-id",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})
}

// --- InviteMember ---

func TestInviteMember(t *testing.T) {
	t.Parallel()

	t.Run("sends invitation via WorkOS", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_new",
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
		require.NotNil(t, result)
		assert.Equal(t, "inv_new", result.Invite.ID)
		assert.Equal(t, "newuser@example.com", result.Invite.Email)
		assert.Equal(t, string(usermanagement.Pending), result.Invite.Status)
	})

	t.Run("rejects mismatched organization ID", func(t *testing.T) {
		t.Parallel()

		wos := workosStub(t, http.NewServeMux())
		ctx, ti := newTestTeamsService(t, wos)

		_, err := ti.service.InviteMember(ctx, &gen.InviteMemberPayload{
			OrganizationID: "wrong-org-id",
			Email:          "someone@example.com",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})
}

// --- ListInvites ---

func TestListInvites(t *testing.T) {
	t.Parallel()

	t.Run("returns invitations from WorkOS", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.ListInvitationsResponse{
				Data: []usermanagement.Invitation{
					{ID: "inv_1", Email: "a@example.com", State: usermanagement.Pending, CreatedAt: "2026-03-26T00:00:00Z", ExpiresAt: "2026-04-02T00:00:00Z"},
					{ID: "inv_2", Email: "b@example.com", State: usermanagement.Accepted, CreatedAt: "2026-03-25T00:00:00Z", ExpiresAt: "2026-04-01T00:00:00Z"},
				},
				ListMetadata: common.ListMetadata{},
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		result, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		require.NoError(t, err)
		require.Len(t, result.Invites, 2)
		assert.Equal(t, "inv_1", result.Invites[0].ID)
		assert.Equal(t, "inv_2", result.Invites[1].ID)
	})
}

// --- CancelInvite ---

func TestCancelInvite(t *testing.T) {
	t.Parallel()

	t.Run("cancels invite that belongs to org", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		// GetInvitation to verify ownership
		mux.HandleFunc("/user_management/invitations/inv_abc", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_abc",
				Email:          "target@example.com",
				State:          usermanagement.Pending,
				OrganizationID: testWorkOSOrgID,
			})
		})
		// RevokeInvitation
		mux.HandleFunc("/user_management/invitations/inv_abc/revoke", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:    "inv_abc",
				State: usermanagement.Revoked,
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		err := ti.service.CancelInvite(ctx, &gen.CancelInvitePayload{
			InviteID: "inv_abc",
		})
		require.NoError(t, err)
	})

	t.Run("IDOR: rejects invite belonging to another org", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		// GetInvitation returns invite for a different org
		mux.HandleFunc("/user_management/invitations/inv_other", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_other",
				Email:          "victim@example.com",
				State:          usermanagement.Pending,
				OrganizationID: "org_attacker_does_not_own",
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		err := ti.service.CancelInvite(ctx, &gen.CancelInvitePayload{
			InviteID: "inv_other",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// --- ResendInvite ---

func TestResendInvite(t *testing.T) {
	t.Parallel()

	t.Run("resends invite and resolves inviter name", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		// GetInvitation (ownership check)
		mux.HandleFunc("/user_management/invitations/inv_resend", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_resend",
				Email:          "resend@example.com",
				State:          usermanagement.Pending,
				OrganizationID: testWorkOSOrgID,
			})
		})
		// ResendInvitation
		mux.HandleFunc("/user_management/invitations/inv_resend/resend", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_resend",
				Email:          "resend@example.com",
				State:          usermanagement.Pending,
				InviterUserID:  "wos_inviter_123",
				OrganizationID: testWorkOSOrgID,
				ExpiresAt:      "2026-04-02T00:00:00Z",
				CreatedAt:      "2026-03-26T00:00:00Z",
				UpdatedAt:      "2026-03-26T00:00:00Z",
			})
		})
		// GetUser for inviter name resolution
		mux.HandleFunc("/user_management/users/wos_inviter_123", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.User{
				ID:        "wos_inviter_123",
				Email:     "inviter@example.com",
				FirstName: "Jane",
				LastName:  "Doe",
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		result, err := ti.service.ResendInvite(ctx, &gen.ResendInvitePayload{
			InviteID: "inv_resend",
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "inv_resend", result.Invite.ID)
		assert.Equal(t, "Jane Doe", result.Invite.InvitedBy, "inviter name should be resolved from WorkOS")
	})

	t.Run("IDOR: rejects invite belonging to another org", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user_management/invitations/inv_foreign", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_foreign",
				OrganizationID: "org_somebody_else",
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		_, err := ti.service.ResendInvite(ctx, &gen.ResendInvitePayload{
			InviteID: "inv_foreign",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// --- RemoveMember ---

func TestRemoveMember(t *testing.T) {
	t.Parallel()

	t.Run("prevents self-removal", func(t *testing.T) {
		t.Parallel()

		wos := workosStub(t, http.NewServeMux())
		ctx, ti := newTestTeamsService(t, wos)

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

		wos := workosStub(t, http.NewServeMux())
		ctx, ti := newTestTeamsService(t, wos)

		err := ti.service.RemoveMember(ctx, &gen.RemoveMemberPayload{
			OrganizationID: "wrong-org-id",
			UserID:         "some-other-user",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})
}

// --- GetInviteInfo ---

func TestGetInviteInfo(t *testing.T) {
	t.Parallel()

	t.Run("returns invite info by token", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		// FindInvitationByToken
		mux.HandleFunc("/user_management/invitations/by_token/tok_test", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_info",
				Email:          "invited@example.com",
				State:          usermanagement.Pending,
				Token:          "tok_test",
				OrganizationID: testWorkOSOrgID,
				InviterUserID:  "wos_inviter_456",
			})
		})
		// GetUser for inviter
		mux.HandleFunc("/user_management/users/wos_inviter_456", func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, http.StatusOK, usermanagement.User{
				ID:        "wos_inviter_456",
				Email:     "boss@example.com",
				FirstName: "The",
				LastName:  "Boss",
			})
		})

		wos := workosStub(t, mux)
		ctx, ti := newTestTeamsService(t, wos)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seedWorkOSIDs(t, ti, authCtx)

		result, err := ti.service.GetInviteInfo(ctx, &gen.GetInviteInfoPayload{
			Token: "tok_test",
		})
		require.NoError(t, err)
		assert.Equal(t, "The Boss", result.InviterName)
		assert.Equal(t, "invited@example.com", result.Email)
		assert.Equal(t, string(usermanagement.Pending), result.Status)
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

// --- WorkOS not configured ---

func TestWorkOSNotConfigured(t *testing.T) {
	t.Parallel()

	t.Run("ListMembers returns error when WorkOS is nil", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestTeamsService(t, nil)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)

		_, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("InviteMember returns error when WorkOS is nil", func(t *testing.T) {
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
