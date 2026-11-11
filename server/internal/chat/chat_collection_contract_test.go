package chat_test

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// collectionFixture is one project holding a session of every ownership
// shape the chat collection paths distinguish, seen from one caller.
type collectionFixture struct {
	ti       *chatTestInstance
	callerID string
	// chats maps a stable label to the seeded chat.
	chats map[string]uuid.UUID
	// sources maps each label to the message source stamped on its chat, so
	// the source listing reveals which chats it scanned.
	sources map[string]string
}

const (
	sessionOwn            = "own"
	sessionOtherUser      = "other user"
	sessionExternal       = "external user"
	sessionAnonymous      = "anonymous"
	sessionOwnedAgent     = "owned agent"
	sessionForeignAgent   = "foreign agent"
	sessionAgentOnOthers  = "owned agent on other user's session"
	contractExternalUser  = "contract-external@example.com"
	contractOtherUserID   = "contract-other-user"
	contractForeignOwner  = "contract-foreign-owner"
	contractSourcePrefix  = "contract-source-"
	contractPageSizeLimit = 50
)

var allContractSessions = []string{
	sessionOwn, sessionOtherUser, sessionExternal, sessionAnonymous,
	sessionOwnedAgent, sessionForeignAgent, sessionAgentOnOthers,
}

func newCollectionFixture(t *testing.T) collectionFixture {
	t.Helper()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	owned := seedAgent(t, ctx, ti, authCtx.UserID, "Contract owned agent")
	foreign := seedAgent(t, ctx, ti, contractForeignOwner, "Contract foreign agent")
	chats := map[string]uuid.UUID{
		sessionOwn:          seedChat(t, ctx, ti, authCtx.UserID, "", "own"),
		sessionOtherUser:    seedChat(t, ctx, ti, contractOtherUserID, "", "other user"),
		sessionExternal:     seedChat(t, ctx, ti, "", contractExternalUser, "external user"),
		sessionAnonymous:    seedChat(t, ctx, ti, "", "", "anonymous"),
		sessionOwnedAgent:   seedAgentChat(t, ctx, ti, owned, "owned agent"),
		sessionForeignAgent: seedAgentChat(t, ctx, ti, foreign, "foreign agent"),
	}
	stamped := seedChat(t, ctx, ti, contractOtherUserID, "", "owned agent on other user's session")
	stampActingAgent(t, ctx, ti, stamped, owned)
	chats[sessionAgentOnOthers] = stamped

	sources := make(map[string]string, len(chats))
	for i, label := range allContractSessions {
		source := contractSourcePrefix + string(rune('a'+i))
		_, err := repo.New(ti.conn).SeedChatMessageWithSource(ctx, repo.SeedChatMessageWithSourceParams{
			ChatID:            chats[label],
			ProjectID:         uuid.NullUUID{UUID: ti.projectID, Valid: true},
			Source:            pgtype.Text{String: source, Valid: true},
			OriginatingClient: pgtype.Text{},
		})
		require.NoError(t, err)
		sources[label] = source
	}
	return collectionFixture{ti: ti, callerID: authCtx.UserID, chats: chats, sources: sources}
}

func (f collectionFixture) label(t *testing.T, id string) string {
	t.Helper()
	for label, chatID := range f.chats {
		if chatID.String() == id {
			return label
		}
	}
	t.Fatalf("listing returned a chat the fixture did not seed: %s", id)
	return ""
}

// listed returns the labels ListChats shows, after checking that the page
// total, the past-the-end fallback count and the CountByUser total agree with
// the page.
func (f collectionFixture) listed(t *testing.T, ctx context.Context) []string {
	t.Helper()
	page, err := f.ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	labels := make([]string, 0, len(page.Chats))
	for _, chat := range page.Chats {
		labels = append(labels, f.label(t, chat.ID))
	}
	require.Equal(t, len(labels), page.Total, "page total counts the same sessions as the page")

	past := defaultPayload()
	past.Offset = contractPageSizeLimit
	empty, err := f.ti.service.ListChats(ctx, past)
	require.NoError(t, err)
	require.Empty(t, empty.Chats)
	require.Equal(t, page.Total, empty.Total, "fallback count scopes like the page")

	counts, err := f.ti.service.CountByUser(ctx, &gen.CountByUserPayload{Limit: contractPageSizeLimit})
	require.NoError(t, err)
	require.Equal(t, page.Total, counts.Total, "per-user count total scopes like the page")

	sort.Strings(labels)
	return labels
}

// sourced returns the labels whose stamped source the source listing offers.
func (f collectionFixture) sourced(t *testing.T, ctx context.Context) []string {
	t.Helper()
	result, err := f.ti.service.ListSources(ctx, &gen.ListSourcesPayload{})
	require.NoError(t, err)
	labels := []string{}
	for label, source := range f.sources {
		for _, offered := range result.Sources {
			if offered == source {
				labels = append(labels, label)
			}
		}
	}
	sort.Strings(labels)
	return labels
}

// pointReads returns the labels whose transcript LoadChat opens, the labels
// whose metadata LoadChatOverview returns, and the distinct error codes the
// refused reads returned.
func (f collectionFixture) pointReads(t *testing.T, ctx context.Context) (transcripts, overviews []string, refusals []oops.Code) {
	t.Helper()
	transcripts, overviews, refusals = []string{}, []string{}, []oops.Code{}
	for _, label := range allContractSessions {
		id := f.chats[label].String()
		if _, err := f.ti.service.LoadChat(ctx, loadPayload(id)); err == nil {
			transcripts = append(transcripts, label)
		} else {
			refusals = appendCode(refusals, refusalCode(t, err, label))
		}
		if _, err := f.ti.service.LoadChatOverview(ctx, &gen.LoadChatOverviewPayload{ID: id}); err == nil {
			overviews = append(overviews, label)
		} else {
			refusals = appendCode(refusals, refusalCode(t, err, label))
		}
	}
	sort.Strings(transcripts)
	sort.Strings(overviews)
	return transcripts, overviews, refusals
}

func appendCode(codes []oops.Code, code oops.Code) []oops.Code {
	for _, existing := range codes {
		if existing == code {
			return codes
		}
	}
	return append(codes, code)
}

func refusalCode(t *testing.T, err error, label string) oops.Code {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable, label)
	return shareable.Code
}

func sortedLabels(labels ...string) []string {
	out := append([]string{}, labels...)
	sort.Strings(out)
	return out
}

// TestChatCollectionContract pins, for each kind of caller, which sessions
// every chat collection path reveals and which sessions the point reads open.
// Collections decide project-wide visibility from one project-level chat read
// check and otherwise fall back to the caller's own sessions; point reads
// also honour grants on a single chat. The two answers deliberately differ,
// so a collection must never be rewritten as "the chats a point read allows"
// without a separate decision.
func TestChatCollectionContract(t *testing.T) {
	t.Parallel()

	ownScope := sortedLabels(sessionOwn, sessionOwnedAgent)
	everything := sortedLabels(allContractSessions...)
	none := []oops.Code{}
	forbidden := []oops.Code{oops.CodeForbidden}
	unauthorized := []oops.Code{oops.CodeUnauthorized}

	cases := []struct {
		name string
		ctx  func(t *testing.T, f collectionFixture) context.Context
		// listed is what ListChats, its count and ListSources reveal.
		listed []string
		// transcripts is what LoadChat opens.
		transcripts []string
		// overviews is what LoadChatOverview returns.
		overviews []string
		// refusals is the set of error codes the refused point reads return.
		refusals []oops.Code
	}{
		{
			name: "chat read on every project",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				return grantOrgAdminWithChatRead(t, initSessionCtx(t, f.ti))
			},
			listed:      everything,
			transcripts: everything,
			overviews:   everything,
			refusals:    none,
		},
		{
			name: "chat read naming this project",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				return authztest.WithExactGrants(t, initSessionCtx(t, f.ti), authz.NewGrant(authz.ScopeChatRead, f.ti.projectID.String()))
			},
			// The project-level check lists every session, but a point read
			// asks about the chat itself, which a grant naming the project
			// does not cover.
			listed:      everything,
			transcripts: ownScope,
			overviews:   ownScope,
			refusals:    forbidden,
		},
		{
			name: "chat read naming one other user's session",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				return authztest.WithExactGrants(t, initSessionCtx(t, f.ti), authz.NewGrant(authz.ScopeChatRead, f.chats[sessionOtherUser].String()))
			},
			// A grant on one chat opens that chat but never lists it.
			listed:      ownScope,
			transcripts: sortedLabels(sessionOwn, sessionOwnedAgent, sessionOtherUser),
			overviews:   sortedLabels(sessionOwn, sessionOwnedAgent, sessionOtherUser),
			refusals:    forbidden,
		},
		{
			name: "member without chat grants",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				return authztest.WithExactGrants(t, initSessionCtx(t, f.ti))
			},
			listed:      ownScope,
			transcripts: ownScope,
			overviews:   ownScope,
			refusals:    forbidden,
		},
		{
			name: "visibility check fails",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				// A session without prepared grants makes the project-level
				// check error rather than deny.
				return projectSessionCtx(t, f.ti)
			},
			// Collections fall back to own sessions; point reads on anything
			// else fail as unexpected rather than forbidden.
			listed:      ownScope,
			transcripts: ownScope,
			overviews:   ownScope,
			refusals:    []oops.Code{oops.CodeUnexpected},
		},
		{
			name: "direct legacy API key",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				// RBAC is not enforced for a legacy key. Collections treat the
				// skipped check as "not allowed" and list the key creator's own
				// sessions, while point reads open every project session.
				return contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
					UserID:               f.callerID,
					APIKeyID:             uuid.NewString(),
					APIKeyName:           "contract-key",
					APIKeyScopes:         []string{"producer"},
					ProjectID:            &f.ti.projectID,
					ActiveOrganizationID: f.ti.orgID,
				})
			},
			listed:      ownScope,
			transcripts: everything,
			overviews:   everything,
			refusals:    none,
		},
		{
			name: "chat session token minted by an API key for an external user",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				// The token carries the key creator's user ID. Collections
				// follow the embedded external user, but point reads also
				// open sessions the key creator owns.
				return apiKeyChatSessionTokenCtx(t, f, contractExternalUser)
			},
			listed:      sortedLabels(sessionExternal),
			transcripts: sortedLabels(sessionExternal, sessionOwn),
			overviews:   sortedLabels(sessionExternal, sessionOwn),
			refusals:    unauthorized,
		},
		{
			name: "chat session token minted by an API key without an external user",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				return apiKeyChatSessionTokenCtx(t, f, "")
			},
			listed:      ownScope,
			transcripts: sortedLabels(sessionOwn),
			overviews:   sortedLabels(sessionOwn),
			refusals:    unauthorized,
		},
		{
			name: "chat session token minted by a dashboard session",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				// The token keeps the dashboard user's session but its
				// authorization never prepares grants, so point reads open
				// only what that user owns and every other read fails as
				// unexpected. Collections follow the embedded external user.
				ctx := projectSessionCtx(t, f.ti)
				authCtx, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				embedded := *authCtx
				embedded.ExternalUserID = contractExternalUser
				return contextvalues.SetAuthContext(ctx, &embedded)
			},
			listed:      sortedLabels(sessionExternal),
			transcripts: ownScope,
			overviews:   ownScope,
			refusals:    []oops.Code{oops.CodeUnexpected},
		},
		{
			name: "managed assistant",
			ctx: func(t *testing.T, f collectionFixture) context.Context {
				t.Helper()
				return managedAssistantCtx(t, f.ti, f.callerID)
			},
			listed:      everything,
			transcripts: everything,
			overviews:   everything,
			refusals:    none,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newCollectionFixture(t)
			ctx := tc.ctx(t, f)

			require.Equal(t, tc.listed, f.listed(t, ctx), "ListChats")
			require.Equal(t, tc.listed, f.sourced(t, ctx), "ListSources")
			transcripts, overviews, refusals := f.pointReads(t, ctx)
			require.Equal(t, tc.transcripts, transcripts, "LoadChat")
			require.Equal(t, tc.overviews, overviews, "LoadChatOverview")
			require.Equal(t, tc.refusals, refusals, "point read refusals")
		})
	}
}

// A caller restricted to their own sessions who has no user identity has no
// own sessions to scope to, so every collection refuses rather than listing
// nothing or everything.
func TestChatCollectionContract_OwnScopeWithoutUserIsRefused(t *testing.T) {
	t.Parallel()
	f := newCollectionFixture(t)
	ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
		UserID:               "",
		APIKeyID:             uuid.NewString(),
		APIKeyName:           "contract-key",
		APIKeyScopes:         []string{"producer"},
		ProjectID:            &f.ti.projectID,
		ActiveOrganizationID: f.ti.orgID,
	})

	_, err := f.ti.service.ListChats(ctx, defaultPayload())
	requireOopsCode(t, err, oops.CodeUnauthorized)
	_, err = f.ti.service.ListSources(ctx, &gen.ListSourcesPayload{})
	requireOopsCode(t, err, oops.CodeUnauthorized)
	_, err = f.ti.service.ListSessionLinks(ctx, &gen.ListSessionLinksPayload{ChatIds: []string{f.chats[sessionOwn].String()}})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// apiKeyChatSessionTokenCtx mirrors what chatsessions.Manager.Authorize
// installs for a chat-session token minted by an API key that the fixture's
// caller created: the creator's user ID, the minting key's ID, no key scopes,
// and the embedded external user, if any.
func apiKeyChatSessionTokenCtx(t *testing.T, f collectionFixture, externalUserID string) context.Context {
	t.Helper()
	return contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
		UserID:               f.callerID,
		APIKeyID:             uuid.NewString(),
		APIKeyScopes:         nil,
		ExternalUserID:       externalUserID,
		ProjectID:            &f.ti.projectID,
		ActiveOrganizationID: f.ti.orgID,
	})
}
