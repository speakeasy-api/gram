package memoryeval_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// searchWindow is the longest interval one search_chats call may cover, so
// the 60 day history is searched in consecutive windows.
const searchWindow = 31 * 24 * time.Hour

// maxSearchPages bounds paging per window so a broken cursor fails the test
// instead of hanging it. The whole corpus fits in far fewer pages.
const maxSearchPages = 200

// armResult is what one arm call returned.
type armResult struct {
	// output is the exact text the tools returned, as an agent would see it.
	output string

	// chatIDs are the chats the returned items came from.
	chatIDs []uuid.UUID
}

// arm is one way of giving an agent memory. search runs a keyword query over
// the main project's full history; sweep returns everything the arm can show
// for the main project, for leak and isolation checks.
type arm struct {
	name   string
	search func(ctx context.Context, query string) (armResult, error)
	sweep  func(ctx context.Context) (armResult, error)
}

type allowLimiter struct{}

func (allowLimiter) Allow(context.Context, string) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true}, nil
}

func (allowLimiter) AllowN(context.Context, string, int) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true}, nil
}

// searchChatsArm is arm B: today's search_chats with a chat:read grant.
func searchChatsArm(t *testing.T, loaded loadedCorpus) (context.Context, arm) {
	t.Helper()
	engine := authz.NewEngine(testenv.NewLogger(t), loaded.db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	budget := platformmcp.OperationBudget{Connection: allowLimiter{}, Organization: allowLimiter{}}
	service := platformmcp.NewChatSearchService(loaded.db, engine, audit.NewLogger(), budget, "memoryeval-search-key-material")
	require.NotNil(t, service)

	// The operation budget keys external callers by their OAuth connection.
	principal := platformmcp.Principal{UserID: loaded.userID("amara"), OrganizationID: loaded.organizationID, ConnectionID: uuid.NewString(), Generation: uuid.NewString()}
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource)})
	ctx = platformmcp.ContextWithPrincipal(ctx, principal)

	projectID := loaded.projects[memoryeval.ProjectMain].ID.String()
	end := loaded.corpus.Now.Add(24 * time.Hour)
	run := func(ctx context.Context, query string) (armResult, error) {
		var output strings.Builder
		result := armResult{output: "", chatIDs: nil}
		for to := end; to.After(loaded.corpus.Now.AddDate(0, 0, -62)); to = to.Add(-searchWindow) {
			input := platformmcp.SearchChatsInput{ProjectID: projectID, From: to.Add(-searchWindow).Format(time.RFC3339), To: to.Format(time.RFC3339), Query: query, Limit: 20}
			for page := 0; ; page++ {
				if page == maxSearchPages {
					return armResult{}, fmt.Errorf("search %q did not finish paging", query)
				}
				out, err := service.Search(ctx, principal, input)
				if err != nil {
					return armResult{}, fmt.Errorf("search %q: %w", query, err)
				}
				encoded, err := json.Marshal(out)
				if err != nil {
					return armResult{}, fmt.Errorf("encode search output: %w", err)
				}
				output.Write(encoded)
				output.WriteByte('\n')
				for _, hit := range out.Hits {
					result.chatIDs = append(result.chatIDs, uuid.MustParse(hit.ChatID))
				}
				if out.NextCursor == "" {
					break
				}
				input.Cursor = out.NextCursor
			}
		}
		result.output = output.String()
		return result, nil
	}
	return ctx, arm{
		name:   "B: search_chats",
		search: run,
		sweep:  func(ctx context.Context) (armResult, error) { return run(ctx, "") },
	}
}
