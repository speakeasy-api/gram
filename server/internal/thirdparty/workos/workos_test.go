package workos

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/common"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

func testClient(t *testing.T, handler http.Handler) *WorkOS {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := usermanagement.NewClient("test_api_key")
	client.Endpoint = server.URL
	client.HTTPClient = server.Client()

	return NewForTest(slog.Default(), client)
}

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestNilClient(t *testing.T) {
	t.Parallel()

	var w *WorkOS

	t.Run("all methods return error on nil receiver", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		_, err := w.GetUserByEmail(ctx, "test@example.com")
		require.Error(t, err)

		_, err = w.ListUsersInOrg(ctx, "org_123")
		require.Error(t, err)

		_, err = w.SendInvitation(ctx, usermanagement.SendInvitationOpts{
			Email: "test@example.com",
		})
		require.Error(t, err)

		_, err = w.ListInvitations(ctx, "org_123")
		require.Error(t, err)

		_, err = w.RevokeInvitation(ctx, "inv_123")
		require.Error(t, err)

		_, err = w.ResendInvitation(ctx, "inv_123")
		require.Error(t, err)

		_, err = w.FindInvitationByToken(ctx, "token_123")
		require.Error(t, err)

		_, err = w.GetUser(ctx, "user_123")
		require.Error(t, err)

		_, err = w.GetInvitation(ctx, "inv_123")
		require.Error(t, err)

		err = w.DeleteOrganizationMembership(ctx, "mem_123")
		require.Error(t, err)

		_, err = w.GetOrgMembership(ctx, "user_123", "org_123")
		require.Error(t, err)
	})
}

func TestListUsersInOrg(t *testing.T) {
	t.Parallel()

	t.Run("returns all users in org", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/user_management/users")
			assert.Equal(t, "org_123", r.URL.Query().Get("organization_id"))

			jsonResponse(rw, http.StatusOK, usermanagement.ListUsersResponse{
				Data: []usermanagement.User{
					{ID: "user_1", Email: "alice@example.com", FirstName: "Alice", LastName: "Smith"},
					{ID: "user_2", Email: "bob@example.com", FirstName: "Bob"},
				},
				ListMetadata: common.ListMetadata{},
			})
		}))

		users, err := w.ListUsersInOrg(context.Background(), "org_123")
		require.NoError(t, err)
		assert.Len(t, users, 2)
		assert.Equal(t, "user_1", users[0].ID)
		assert.Equal(t, "alice@example.com", users[0].Email)
		assert.Equal(t, "user_2", users[1].ID)
	})

	t.Run("paginates through multiple pages", func(t *testing.T) {
		t.Parallel()
		callCount := 0
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				jsonResponse(rw, http.StatusOK, usermanagement.ListUsersResponse{
					Data: []usermanagement.User{
						{ID: "user_1", Email: "alice@example.com"},
					},
					ListMetadata: common.ListMetadata{After: "user_1"},
				})
			} else {
				assert.Equal(t, "user_1", r.URL.Query().Get("after"))
				jsonResponse(rw, http.StatusOK, usermanagement.ListUsersResponse{
					Data: []usermanagement.User{
						{ID: "user_2", Email: "bob@example.com"},
					},
					ListMetadata: common.ListMetadata{},
				})
			}
		}))

		users, err := w.ListUsersInOrg(context.Background(), "org_123")
		require.NoError(t, err)
		assert.Len(t, users, 2)
		assert.Equal(t, 2, callCount)
	})

	t.Run("returns error on API failure", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			http.Error(rw, `{"message":"internal error"}`, http.StatusInternalServerError)
		}))

		_, err := w.ListUsersInOrg(context.Background(), "org_123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list users from workos")
	})
}

func TestSendInvitation(t *testing.T) {
	t.Parallel()

	t.Run("sends invitation successfully", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/user_management/invitations", r.URL.Path)

			var body usermanagement.SendInvitationOpts
			_ = json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "invitee@example.com", body.Email)
			assert.Equal(t, "org_123", body.OrganizationID)
			assert.Equal(t, "user_456", body.InviterUserID)

			jsonResponse(rw, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_789",
				Email:          "invitee@example.com",
				State:          usermanagement.Pending,
				Token:          "tok_abc",
				OrganizationID: "org_123",
				InviterUserID:  "user_456",
				ExpiresAt:      "2026-04-02T00:00:00Z",
				CreatedAt:      "2026-03-26T00:00:00Z",
				UpdatedAt:      "2026-03-26T00:00:00Z",
			})
		}))

		inv, err := w.SendInvitation(context.Background(), usermanagement.SendInvitationOpts{
			Email:          "invitee@example.com",
			OrganizationID: "org_123",
			InviterUserID:  "user_456",
			ExpiresInDays:  7,
		})
		require.NoError(t, err)
		assert.Equal(t, "inv_789", inv.ID)
		assert.Equal(t, usermanagement.Pending, inv.State)
		assert.Equal(t, "tok_abc", inv.Token)
	})

	t.Run("returns error on API failure", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			http.Error(rw, `{"message":"conflict"}`, http.StatusConflict)
		}))

		_, err := w.SendInvitation(context.Background(), usermanagement.SendInvitationOpts{
			Email: "invitee@example.com",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send invitation via workos")
	})
}

func TestListInvitations(t *testing.T) {
	t.Parallel()

	t.Run("returns all invitations for org", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/user_management/invitations")
			assert.Equal(t, "org_123", r.URL.Query().Get("organization_id"))

			jsonResponse(rw, http.StatusOK, usermanagement.ListInvitationsResponse{
				Data: []usermanagement.Invitation{
					{ID: "inv_1", Email: "alice@example.com", State: usermanagement.Pending},
					{ID: "inv_2", Email: "bob@example.com", State: usermanagement.Accepted},
				},
				ListMetadata: common.ListMetadata{},
			})
		}))

		invites, err := w.ListInvitations(context.Background(), "org_123")
		require.NoError(t, err)
		assert.Len(t, invites, 2)
		assert.Equal(t, "inv_1", invites[0].ID)
		assert.Equal(t, usermanagement.Pending, invites[0].State)
	})

	t.Run("paginates through multiple pages", func(t *testing.T) {
		t.Parallel()
		callCount := 0
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				jsonResponse(rw, http.StatusOK, usermanagement.ListInvitationsResponse{
					Data: []usermanagement.Invitation{
						{ID: "inv_1", Email: "alice@example.com", State: usermanagement.Pending},
					},
					ListMetadata: common.ListMetadata{After: "inv_1"},
				})
			} else {
				assert.Equal(t, "inv_1", r.URL.Query().Get("after"))
				jsonResponse(rw, http.StatusOK, usermanagement.ListInvitationsResponse{
					Data: []usermanagement.Invitation{
						{ID: "inv_2", Email: "bob@example.com", State: usermanagement.Pending},
					},
					ListMetadata: common.ListMetadata{},
				})
			}
		}))

		invites, err := w.ListInvitations(context.Background(), "org_123")
		require.NoError(t, err)
		assert.Len(t, invites, 2)
		assert.Equal(t, 2, callCount)
	})
}

func TestRevokeInvitation(t *testing.T) {
	t.Parallel()

	t.Run("revokes invitation successfully", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/user_management/invitations/inv_123/revoke", r.URL.Path)

			jsonResponse(rw, http.StatusOK, usermanagement.Invitation{
				ID:        "inv_123",
				Email:     "revoked@example.com",
				State:     usermanagement.Revoked,
				RevokedAt: "2026-03-26T00:00:00Z",
			})
		}))

		inv, err := w.RevokeInvitation(context.Background(), "inv_123")
		require.NoError(t, err)
		assert.Equal(t, "inv_123", inv.ID)
		assert.Equal(t, usermanagement.Revoked, inv.State)
	})

	t.Run("returns error on API failure", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			http.Error(rw, `{"message":"not found"}`, http.StatusNotFound)
		}))

		_, err := w.RevokeInvitation(context.Background(), "inv_invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to revoke invitation via workos")
	})
}

func TestResendInvitation(t *testing.T) {
	t.Parallel()

	t.Run("resends invitation successfully", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/user_management/invitations/inv_123/resend", r.URL.Path)

			jsonResponse(rw, http.StatusOK, usermanagement.Invitation{
				ID:        "inv_123",
				Email:     "resend@example.com",
				State:     usermanagement.Pending,
				ExpiresAt: "2026-04-02T00:00:00Z",
				CreatedAt: "2026-03-26T00:00:00Z",
			})
		}))

		inv, err := w.ResendInvitation(context.Background(), "inv_123")
		require.NoError(t, err)
		assert.Equal(t, "inv_123", inv.ID)
		assert.Equal(t, usermanagement.Pending, inv.State)
	})
}

func TestFindInvitationByToken(t *testing.T) {
	t.Parallel()

	t.Run("finds invitation by token", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/user_management/invitations/by_token/tok_abc", r.URL.Path)

			jsonResponse(rw, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_123",
				Email:          "found@example.com",
				State:          usermanagement.Pending,
				Token:          "tok_abc",
				OrganizationID: "org_123",
				InviterUserID:  "user_456",
			})
		}))

		inv, err := w.FindInvitationByToken(context.Background(), "tok_abc")
		require.NoError(t, err)
		assert.Equal(t, "inv_123", inv.ID)
		assert.Equal(t, "found@example.com", inv.Email)
		assert.Equal(t, "tok_abc", inv.Token)
		assert.Equal(t, "org_123", inv.OrganizationID)
	})

	t.Run("returns error when token not found", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			http.Error(rw, `{"message":"not found"}`, http.StatusNotFound)
		}))

		_, err := w.FindInvitationByToken(context.Background(), "tok_invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to find invitation by token via workos")
	})
}

func TestGetInvitation(t *testing.T) {
	t.Parallel()

	t.Run("gets invitation by ID", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/user_management/invitations/inv_123", r.URL.Path)

			jsonResponse(rw, http.StatusOK, usermanagement.Invitation{
				ID:             "inv_123",
				Email:          "test@example.com",
				State:          usermanagement.Pending,
				OrganizationID: "org_123",
			})
		}))

		inv, err := w.GetInvitation(context.Background(), "inv_123")
		require.NoError(t, err)
		assert.Equal(t, "inv_123", inv.ID)
		assert.Equal(t, "org_123", inv.OrganizationID)
	})

	t.Run("returns error on API failure", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			http.Error(rw, `{"message":"not found"}`, http.StatusNotFound)
		}))

		_, err := w.GetInvitation(context.Background(), "inv_invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get invitation from workos")
	})
}

func TestGetUser(t *testing.T) {
	t.Parallel()

	t.Run("gets user by ID", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/user_management/users/user_123", r.URL.Path)

			jsonResponse(rw, http.StatusOK, usermanagement.User{
				ID:                "user_123",
				Email:             "alice@example.com",
				FirstName:         "Alice",
				LastName:          "Smith",
				ProfilePictureURL: "https://example.com/photo.jpg",
			})
		}))

		user, err := w.GetUser(context.Background(), "user_123")
		require.NoError(t, err)
		assert.Equal(t, "user_123", user.ID)
		assert.Equal(t, "alice@example.com", user.Email)
		assert.Equal(t, "Alice", user.FirstName)
		assert.Equal(t, "Smith", user.LastName)
	})

	t.Run("returns error on API failure", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			http.Error(rw, `{"message":"not found"}`, http.StatusNotFound)
		}))

		_, err := w.GetUser(context.Background(), "user_invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get user from workos")
	})
}

func TestDeleteOrganizationMembership(t *testing.T) {
	t.Parallel()

	t.Run("deletes membership successfully", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/user_management/organization_memberships/mem_123", r.URL.Path)
			rw.WriteHeader(http.StatusNoContent)
		}))

		err := w.DeleteOrganizationMembership(context.Background(), "mem_123")
		require.NoError(t, err)
	})

	t.Run("returns error on API failure", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			http.Error(rw, `{"message":"forbidden"}`, http.StatusForbidden)
		}))

		err := w.DeleteOrganizationMembership(context.Background(), "mem_invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete organization membership via workos")
	})
}

func TestGetOrgMembership(t *testing.T) {
	t.Parallel()

	t.Run("returns membership when found", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/user_management/organization_memberships")
			assert.Equal(t, "user_123", r.URL.Query().Get("user_id"))
			assert.Equal(t, "org_456", r.URL.Query().Get("organization_id"))

			jsonResponse(rw, http.StatusOK, usermanagement.ListOrganizationMembershipsResponse{
				Data: []usermanagement.OrganizationMembership{
					{
						ID:             "mem_789",
						UserID:         "user_123",
						OrganizationID: "org_456",
						Status:         "active",
					},
				},
				ListMetadata: common.ListMetadata{},
			})
		}))

		membership, err := w.GetOrgMembership(context.Background(), "user_123", "org_456")
		require.NoError(t, err)
		require.NotNil(t, membership)
		assert.Equal(t, "mem_789", membership.ID)
		assert.Equal(t, "user_123", membership.UserID)
		assert.Equal(t, "org_456", membership.OrganizationID)
	})

	t.Run("returns nil when no membership found", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			jsonResponse(rw, http.StatusOK, usermanagement.ListOrganizationMembershipsResponse{
				Data:         []usermanagement.OrganizationMembership{},
				ListMetadata: common.ListMetadata{},
			})
		}))

		membership, err := w.GetOrgMembership(context.Background(), "user_123", "org_456")
		require.NoError(t, err)
		assert.Nil(t, membership)
	})
}

func TestGetUserByEmail(t *testing.T) {
	t.Parallel()

	t.Run("returns user when found", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "alice@example.com", r.URL.Query().Get("email"))

			jsonResponse(rw, http.StatusOK, usermanagement.ListUsersResponse{
				Data: []usermanagement.User{
					{ID: "user_1", Email: "alice@example.com", FirstName: "Alice"},
				},
				ListMetadata: common.ListMetadata{},
			})
		}))

		user, err := w.GetUserByEmail(context.Background(), "alice@example.com")
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, "user_1", user.ID)
		assert.Equal(t, "alice@example.com", user.Email)
	})

	t.Run("returns nil when no user found", func(t *testing.T) {
		t.Parallel()
		w := testClient(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			jsonResponse(rw, http.StatusOK, usermanagement.ListUsersResponse{
				Data:         []usermanagement.User{},
				ListMetadata: common.ListMetadata{},
			})
		}))

		user, err := w.GetUserByEmail(context.Background(), "nobody@example.com")
		require.NoError(t, err)
		assert.Nil(t, user)
	})
}
