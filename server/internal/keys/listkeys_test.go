package keys_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	httpkeys "github.com/speakeasy-api/gram/server/gen/http/keys/server"
	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestKeysService_ListKeys(t *testing.T) {
	t.Parallel()
	t.Run("list empty keys", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestKeysService(t)

		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Empty(t, result.Keys)
	})

	t.Run("list keys after creating some", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestKeysService(t)

		// Create a few keys
		key1, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "key-1",
			Scopes:       []string{"consumer"},
		})
		require.NoError(t, err)

		key2, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "key-2",
			Scopes:       []string{"consumer"},
		})
		require.NoError(t, err)

		// List keys
		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Keys, 2)

		// Check that keys are returned correctly
		keyNames := make([]string, len(result.Keys))
		for i, key := range result.Keys {
			keyNames[i] = key.Name
		}
		require.ElementsMatch(t, []string{"key-1", "key-2"}, keyNames)

		// Verify key fields
		for _, key := range result.Keys {
			require.NotEmpty(t, key.ID)
			require.NotEmpty(t, key.Name)
			require.NotEmpty(t, key.OrganizationID)
			require.NotEmpty(t, key.CreatedByUserID)
			require.Nil(t, key.Key) // Key should not be returned in list
			require.NotEmpty(t, key.KeyPrefix)
			require.Equal(t, []string{"consumer"}, key.Scopes)
			require.NotEmpty(t, key.CreatedAt)
			require.NotEmpty(t, key.UpdatedAt)
			require.Nil(t, key.LastAccessedAt) // Newly created keys have not been accessed

			// Verify the key prefix follows expected format
			require.Contains(t, key.KeyPrefix, "gram_local_")
		}

		// Verify the created keys match the listed ones
		for _, createdKey := range []*gen.Key{key1, key2} {
			found := false
			for _, listedKey := range result.Keys {
				if listedKey.ID == createdKey.ID {
					found = true
					require.Equal(t, createdKey.Name, listedKey.Name)
					require.Equal(t, createdKey.OrganizationID, listedKey.OrganizationID)
					require.Equal(t, createdKey.ProjectID, listedKey.ProjectID)
					require.Equal(t, createdKey.CreatedByUserID, listedKey.CreatedByUserID)
					require.Equal(t, createdKey.KeyPrefix, listedKey.KeyPrefix)
					require.Equal(t, createdKey.Scopes, listedKey.Scopes)
					require.Equal(t, createdKey.CreatedAt, listedKey.CreatedAt)
					require.Equal(t, createdKey.UpdatedAt, listedKey.UpdatedAt)
					require.Equal(t, createdKey.LastAccessedAt, listedKey.LastAccessedAt)
					break
				}
			}
			require.True(t, found, "created key %s not found in list", createdKey.ID)
		}
	})

	t.Run("unauthorized without auth context", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestKeysService(t)

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		_, err := ti.service.ListKeys(ctxWithoutAuth, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("LastAccessedAt is populated after key authentication", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestKeysService(t)

		// Create a key
		createdKey, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "auth-test-key",
			Scopes:       []string{"consumer"},
		})
		require.NoError(t, err)
		require.NotNil(t, createdKey.Key)

		// Initially, LastAccessedAt should be nil
		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)

		var keyBeforeAuth *gen.Key
		for _, k := range result.Keys {
			if k.ID == createdKey.ID {
				keyBeforeAuth = k
				break
			}
		}
		require.NotNil(t, keyBeforeAuth)
		require.Nil(t, keyBeforeAuth.LastAccessedAt)

		// Authenticate using the key (this should update LastAccessedAt)
		_, err = ti.keyAuth.KeyBasedAuth(ctx, *createdKey.Key, []string{"consumer"})
		require.NoError(t, err)

		// Now LastAccessedAt should be populated
		result, err = ti.service.ListKeys(ctx, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)

		var keyAfterAuth *gen.Key
		for _, k := range result.Keys {
			if k.ID == createdKey.ID {
				keyAfterAuth = k
				break
			}
		}
		require.NotNil(t, keyAfterAuth)
		require.NotNil(t, keyAfterAuth.LastAccessedAt)

		// Verify the timestamp is in RFC3339 format and is not zero
		lastAccessed, err := time.Parse(time.RFC3339, *keyAfterAuth.LastAccessedAt)
		require.NoError(t, err)
		require.False(t, lastAccessed.IsZero())

		// Verify the timestamp is truncated to minute precision (no seconds)
		require.Equal(t, 0, lastAccessed.Second())
		require.Equal(t, 0, lastAccessed.Nanosecond())
	})
}

// createListedKeys creates ordinary keys through the service, in order, and
// returns their IDs.
func createListedKeys(t *testing.T, ctx context.Context, ti *testInstance, names ...string) []string {
	t.Helper()
	ids := make([]string, 0, len(names))
	for _, name := range names {
		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: name, Scopes: []string{"consumer"}})
		require.NoError(t, err)
		ids = append(ids, key.ID)
	}
	return ids
}

// createSystemKey inserts a key the way plugin distribution mints one. The
// service reserves the plugins- prefix, so these never come from CreateKey.
func createSystemKey(t *testing.T, ctx context.Context, ti *testInstance, organizationID, name string) string {
	t.Helper()
	key, err := keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  organizationID,
		ProjectID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CreatedByUserID: testAuthContext(t, ctx).UserID,
		Name:            name,
		KeyPrefix:       "gram_local_system",
		KeyHash:         "system-hash-" + uuid.NewString(),
		Scopes:          []string{"hooks"},
	})
	require.NoError(t, err)
	return key.ID.String()
}

// setKeysCreatedAt pins created_at on the given keys, so tests can force ties
// on the first half of the page ordering.
func setKeysCreatedAt(t *testing.T, ctx context.Context, ti *testInstance, at time.Time, ids ...string) {
	t.Helper()
	parsed := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		parsed = append(parsed, uuid.MustParse(id))
	}
	err := testrepo.New(ti.conn).SetAPIKeysCreatedAtFixture(ctx, testrepo.SetAPIKeysCreatedAtFixtureParams{
		CreatedAt:      pgtype.Timestamptz{Time: at, InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: testAuthContext(t, ctx).ActiveOrganizationID,
		Ids:            parsed,
	})
	require.NoError(t, err)
}

// listAllPages follows next_cursor until it runs out and returns every page.
func listAllPages(t *testing.T, ctx context.Context, ti *testInstance, limit int, search *string) [][]*gen.Key {
	t.Helper()
	var pages [][]*gen.Key
	var cursor *string
	for range 100 {
		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Cursor: cursor, Limit: &limit, Search: search})
		require.NoError(t, err)
		require.LessOrEqual(t, len(result.Keys), limit)
		pages = append(pages, result.Keys)
		if result.NextCursor == nil {
			return pages
		}
		require.Len(t, result.Keys, limit, "only the last page may be short")
		cursor = result.NextCursor
	}
	require.FailNow(t, "next_cursor never ran out")
	return nil
}

func keyIDs(keys []*gen.Key) []string {
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, key.ID)
	}
	return ids
}

func TestKeysService_ListKeys_Pagination(t *testing.T) {
	t.Parallel()

	t.Run("no limit returns every key and no cursor", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		created := createListedKeys(t, ctx, ti, "legacy-1", "legacy-2", "legacy-3")
		created = append(created, createSystemKey(t, ctx, ti, testAuthContext(t, ctx).ActiveOrganizationID, "plugins-hooks-legacy"))

		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
		require.NoError(t, err)
		require.Nil(t, result.NextCursor)
		require.ElementsMatch(t, created, keyIDs(result.Keys))
	})

	t.Run("limit bounds the page and reports more", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		created := createListedKeys(t, ctx, ti, "page-1", "page-2", "page-3", "page-4", "page-5")

		limit := 2
		first, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Limit: &limit})
		require.NoError(t, err)
		require.Len(t, first.Keys, 2)
		require.NotNil(t, first.NextCursor)
		// Newest first: the last two keys created lead the list.
		require.Equal(t, []string{created[4], created[3]}, keyIDs(first.Keys))

		exact := len(created)
		all, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Limit: &exact})
		require.NoError(t, err)
		require.Len(t, all.Keys, len(created))
		require.Nil(t, all.NextCursor, "a page that holds the last key has no next cursor")
	})

	t.Run("cursor pages cover every key once in order, across created_at ties", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		names := make([]string, 0, 11)
		for i := range 11 {
			names = append(names, fmt.Sprintf("tie-%02d", i))
		}
		created := createListedKeys(t, ctx, ti, names...)
		// Seven keys share one timestamp, so pages of three must split the tie
		// on id. The rest keep their own, later timestamps.
		tied := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
		setKeysCreatedAt(t, ctx, ti, tied, created[:7]...)

		everything, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
		require.NoError(t, err)
		require.Len(t, everything.Keys, len(created))

		for _, limit := range []int{1, 3, 4, 100} {
			pages := listAllPages(t, ctx, ti, limit, nil)
			var walked []string
			for _, page := range pages {
				walked = append(walked, keyIDs(page)...)
			}
			require.Equal(t, keyIDs(everything.Keys), walked, "limit %d: pages must match the unpaged order with no gaps or repeats", limit)
		}
	})

	t.Run("a key created between pages does not shift later pages", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		created := createListedKeys(t, ctx, ti, "stable-1", "stable-2", "stable-3", "stable-4")

		limit := 2
		first, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Limit: &limit})
		require.NoError(t, err)
		createListedKeys(t, ctx, ti, "stable-new")

		second, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Cursor: first.NextCursor, Limit: &limit})
		require.NoError(t, err)
		require.Equal(t, []string{created[1], created[0]}, keyIDs(second.Keys))
		require.Nil(t, second.NextCursor)
	})

	t.Run("search matches names case-insensitively and pages", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		created := createListedKeys(t, ctx, ti, "Deploy-Prod", "deploy-staging", "ci-runner")
		created = append(created, createSystemKey(t, ctx, ti, testAuthContext(t, ctx).ActiveOrganizationID, "plugins-hooks-20260101-000000-abc123"))
		created = append(created, createListedKeys(t, ctx, ti, "my_DEPLOY_key", "deploy%literal")...)

		search := "  DEPLOY "
		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Search: &search})
		require.NoError(t, err)
		require.ElementsMatch(t, []string{created[0], created[1], created[4], created[5]}, keyIDs(result.Keys))

		// System keys are listed and searchable like any other key.
		plugins := "plugins-"
		result, err = ti.service.ListKeys(ctx, &gen.ListKeysPayload{Search: &plugins})
		require.NoError(t, err)
		require.Equal(t, []string{created[3]}, keyIDs(result.Keys))

		// LIKE wildcards in the term match literally.
		for term, want := range map[string][]string{
			"%":   {created[5]},
			"y_d": {created[4]},
			"_":   {created[4]},
		} {
			result, err = ti.service.ListKeys(ctx, &gen.ListKeysPayload{Search: &term})
			require.NoError(t, err)
			require.ElementsMatch(t, want, keyIDs(result.Keys), "search %q", term)
		}

		pages := listAllPages(t, ctx, ti, 1, &search)
		require.Len(t, pages, 4)
		var walked []string
		for _, page := range pages {
			walked = append(walked, keyIDs(page)...)
		}
		require.ElementsMatch(t, []string{created[0], created[1], created[4], created[5]}, walked)

		blank := "   "
		result, err = ti.service.ListKeys(ctx, &gen.ListKeysPayload{Search: &blank})
		require.NoError(t, err)
		require.Len(t, result.Keys, len(created), "a blank search filters nothing")

		missing := "no-such-key"
		limit := 10
		result, err = ti.service.ListKeys(ctx, &gen.ListKeysPayload{Search: &missing, Limit: &limit})
		require.NoError(t, err)
		require.Empty(t, result.Keys)
		require.Nil(t, result.NextCursor)
	})

	t.Run("pages stay inside the caller's organization", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		authCtx := testAuthContext(t, ctx)
		created := createListedKeys(t, ctx, ti, "mine-1", "mine-2", "mine-3")

		// Another organization's keys, newer than ours and with matching names.
		otherOrg := "other-org-" + uuid.NewString()
		for i := range 3 {
			createSystemKey(t, ctx, ti, otherOrg, fmt.Sprintf("mine-other-%d", i))
		}

		var walked []string
		for _, page := range listAllPages(t, ctx, ti, 2, nil) {
			for _, key := range page {
				require.Equal(t, authCtx.ActiveOrganizationID, key.OrganizationID)
			}
			walked = append(walked, keyIDs(page)...)
		}
		require.Equal(t, []string{created[2], created[1], created[0]}, walked)

		search := "mine"
		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Search: &search})
		require.NoError(t, err)
		require.ElementsMatch(t, created, keyIDs(result.Keys))
	})

	t.Run("each page carries its own keys' policies", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)
		withPolicy, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			Name:         "policy-oldest",
			Scopes:       []string{"consumer"},
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project)},
		})
		require.NoError(t, err)
		createListedKeys(t, ctx, ti, "policy-newer-1", "policy-newer-2")

		limit := 2
		first, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Limit: &limit})
		require.NoError(t, err)
		for _, key := range first.Keys {
			require.Empty(t, key.PolicyGrants)
		}
		second, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Cursor: first.NextCursor, Limit: &limit})
		require.NoError(t, err)
		require.Len(t, second.Keys, 1)
		require.Equal(t, withPolicy.ID, second.Keys[0].ID)
		require.Equal(t, withPolicy.PolicyGrants, second.Keys[0].PolicyGrants)
	})

	t.Run("rejects bad cursors, limits and agent combinations", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		for _, cursor := range []string{
			"not base64!",
			base64.RawURLEncoding.EncodeToString([]byte("no-separator")),
			base64.RawURLEncoding.EncodeToString([]byte("yesterday|" + uuid.NewString())),
			base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano) + "|not-a-uuid")),
		} {
			_, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Cursor: &cursor})
			requireCode(t, err, oops.CodeBadRequest, "cursor %q", cursor)
		}

		for _, limit := range []int{0, -1, 101} {
			_, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{Limit: &limit})
			requireCode(t, err, oops.CodeBadRequest, "limit %d", limit)
		}

		agentID := uuid.NewString()
		limit := 10
		_, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: &agentID, Limit: &limit})
		requireCode(t, err, oops.CodeBadRequest)
	})

	t.Run("the HTTP decoder enforces the limit range", func(t *testing.T) {
		t.Parallel()
		decode := httpkeys.DecodeListKeysRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)

		decoded, err := decode(httptest.NewRequest("GET", "/rpc/keys.list?limit=100&search=deploy&cursor=abc", nil))
		require.NoError(t, err)
		require.Equal(t, 100, *decoded.Limit)
		require.Equal(t, "deploy", *decoded.Search)
		require.Equal(t, "abc", *decoded.Cursor)

		for _, limit := range []string{"0", "101"} {
			_, err := decode(httptest.NewRequest("GET", "/rpc/keys.list?limit="+limit, nil))
			require.Error(t, err, "limit %s", limit)
		}

		decoded, err = decode(httptest.NewRequest("GET", "/rpc/keys.list", nil))
		require.NoError(t, err)
		require.Nil(t, decoded.Limit, "no limit stays unset so the legacy full list is returned")
	})
}
