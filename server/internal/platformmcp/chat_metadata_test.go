package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

var chatListTestNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// recordingChatReader captures the repository parameters a listing built and
// serves canned rows.
type recordingChatReader struct {
	params      []chatrepo.ListChatsParams
	rows        []chatrepo.ListChatsRow
	err         error
	countParams []chatrepo.CountChatsParams
	count       int64
}

func (r *recordingChatReader) ListChats(_ context.Context, arg chatrepo.ListChatsParams) ([]chatrepo.ListChatsRow, error) {
	r.params = append(r.params, arg)
	return r.rows, r.err
}

func (r *recordingChatReader) CountChats(_ context.Context, arg chatrepo.CountChatsParams) (int64, error) {
	r.countParams = append(r.countParams, arg)
	return r.count, r.err
}

// newChatMetadataService composes the listing without Postgres: the project
// resolver and the chat query are fakes, and the codec is real so references
// and cursors round-trip exactly as they do in production.
func newChatMetadataService(t *testing.T, reader chatMetadataReader, budget OperationBudget) (*ChatMetadataService, ResolvedProject) {
	t.Helper()

	codec := newSubjectReferenceCodec("chat-list-test-key")
	project := ResolvedProject{ID: uuid.New(), Slug: "default", Name: "Project"}
	return &ChatMetadataService{
		projects:   &findingProjects{project: project, calls: nil, err: nil},
		chats:      reader,
		references: codec,
		budget:     budget,
		now:        func() time.Time { return chatListTestNow },
	}, project
}

func chatListRow(id uuid.UUID, createdAt time.Time, userID, externalUserID, accountEmail string, total int64) chatrepo.ListChatsRow {
	return chatrepo.ListChatsRow{
		ID:                   id,
		Title:                pgtype.Text{String: "rotate the production AWS key", Valid: true},
		UserID:               pgtype.Text{String: userID, Valid: userID != ""},
		ExternalUserID:       pgtype.Text{String: externalUserID, Valid: externalUserID != ""},
		Source:               pgtype.Text{String: "claude-code", Valid: true},
		OriginatingClient:    "Claude Code",
		LitellmProxied:       false,
		CreatedAt:            pgtype.Timestamptz{Time: createdAt, InfinityModifier: pgtype.Finite, Valid: true},
		UpdatedAt:            pgtype.Timestamptz{Time: createdAt.Add(time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
		PinnedAt:             pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		NumMessages:          14,
		LastMessageTimestamp: pgtype.Timestamptz{Time: createdAt.Add(10 * time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
		RiskFindingsCount:    2,
		AccountType:          "team",
		AccountEmail:         accountEmail,
		AssistantID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		AssistantName:        pgtype.Text{String: "", Valid: false},
		TotalCount:           total,
	}
}

func chatListRows(count int, total int64) []chatrepo.ListChatsRow {
	rows := make([]chatrepo.ListChatsRow, 0, count)
	for i := range count {
		rows = append(rows, chatListRow(uuid.New(), chatListTestNow.Add(-time.Duration(i+1)*time.Hour), "user_"+strings.Repeat("a", 8), "", "", total))
	}
	return rows
}

// The projection is positive: a field that is not listed here is not served,
// so an addition has to be made deliberately here first. Titles, message
// content, and raw identities are the fields that must never appear.
func TestListChatsOutputProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	reader := &recordingChatReader{rows: []chatrepo.ListChatsRow{chatListRow(uuid.New(), chatListTestNow.Add(-time.Hour), "", "", "dev@example.invalid", 1)}, params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())

	output, err := service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: "",
	})
	require.NoError(t, err)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.ElementsMatch(t, []string{"project", "data", "chats", "total_matches", "limitations"}, keysOf(decoded))

	chats, ok := decoded["chats"].([]any)
	require.True(t, ok)
	require.Len(t, chats, 1)
	chat, ok := chats[0].(map[string]any)
	require.True(t, ok)
	require.ElementsMatch(t, []string{"chat_id", "created_at", "last_message_at", "message_count", "risk_findings_count", "source", "client", "account_type", "masked_identity"}, keysOf(chat))
	require.NotContains(t, string(encoded), "rotate the production AWS key", "titles are content and never leave the server")
	require.NotContains(t, string(encoded), "dev@example.invalid", "raw identities never leave the server")
	require.Equal(t, "d**@e***", chat["masked_identity"])
	require.InDelta(t, 14, chat["message_count"], 0)
	require.InDelta(t, 2, chat["risk_findings_count"], 0)
	require.InDelta(t, 1, decoded["total_matches"], 0)
	require.Contains(t, output.Limitations, "Metadata only")
	require.Equal(t, FreshnessCurrent, output.Envelope.Freshness)
	require.Equal(t, DiagnosticWindowLastWeek, output.Envelope.ResolvedWindow.Window)
	require.False(t, output.Envelope.NoObservations)
}

func keysOf(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	return keys
}

func TestListChatsQueriesTheWindowWithoutSearch(t *testing.T) {
	t.Parallel()

	reader := &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0}
	service, project := newChatMetadataService(t, reader, allowBudget())
	assistantID := uuid.New()

	output, err := service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: project.ID.String(), ProjectSlug: "", Window: "24h", Risk: ChatRiskWithFindings, Source: "claude-code", AssistantID: assistantID.String(), UserReference: "", Limit: 500, Cursor: "",
	})
	require.NoError(t, err)
	require.Len(t, reader.params, 1)
	params := reader.params[0]
	require.Equal(t, project.ID, params.ProjectID)
	require.Equal(t, "true", params.HasRiskFilter)
	require.EqualValues(t, -1, params.MinRiskScore)
	require.Empty(t, params.Search, "search matches titles and emails, so the listing never populates it")
	require.Empty(t, params.UserID)
	require.Empty(t, params.ExternalUserID)
	require.Equal(t, assistantID.String(), params.AssistantID)
	require.Equal(t, []string{"claude-code"}, params.Sources)
	require.Equal(t, chatListTestNow.Add(-24*time.Hour), params.FromTime.Time)
	require.Equal(t, chatListTestNow, params.ToTime.Time)
	require.Equal(t, "last_message_timestamp", params.SortBy, "the chat query lists by activity and has no creation-time order")
	require.Equal(t, "desc", params.SortOrder)
	require.EqualValues(t, 0, params.PageOffset)
	require.EqualValues(t, maxChatListLimit+1, params.PageLimit, "the limit is clamped and one extra row detects another page")

	require.Empty(t, output.Chats)
	require.Zero(t, output.TotalMatches)
	require.Empty(t, output.NextCursor)
	require.True(t, output.Envelope.NoObservations)
	require.Equal(t, project.ID.String(), output.Project.ID)
}

// A person reference round-trips only within the project that minted it, and
// narrows the next listing to the column the identity was recorded under.
func TestListChatsUserReferenceNarrowsTheSamePerson(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	byUser := chatListRow(uuid.New(), chatListTestNow.Add(-time.Hour), "user_01HZX", "", "", 2)
	byExternal := chatListRow(uuid.New(), chatListTestNow.Add(-2*time.Hour), "", "ext-4242", "", 2)
	reader := &recordingChatReader{rows: []chatrepo.ListChatsRow{byUser, byExternal}, params: nil, err: nil, countParams: nil, count: 0}
	service, project := newChatMetadataService(t, reader, allowBudget())

	first, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: "",
	})
	require.NoError(t, err)
	require.Len(t, first.Chats, 2)
	require.Equal(t, "u***", first.Chats[0].MaskedIdentity)
	require.NotEmpty(t, first.Chats[0].UserReference)
	require.Equal(t, "e***", first.Chats[1].MaskedIdentity)
	require.NotEmpty(t, first.Chats[1].UserReference)
	require.NotContains(t, first.Chats[0].UserReference, "user_01HZX")

	// Narrow to the platform user: the reference resolves to user_id and stays
	// usable across a different window and risk filter.
	_, err = service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "30d", Risk: ChatRiskWithoutFindings, Source: "", AssistantID: "", UserReference: first.Chats[0].UserReference, Limit: 0, Cursor: "",
	})
	require.NoError(t, err)
	require.Equal(t, "user_01HZX", reader.params[1].UserID)
	require.Empty(t, reader.params[1].ExternalUserID)

	// Narrow to the external user: the reference resolves to external_user_id.
	_, err = service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: first.Chats[1].UserReference, Limit: 0, Cursor: "",
	})
	require.NoError(t, err)
	require.Empty(t, reader.params[2].UserID)
	require.Equal(t, "ext-4242", reader.params[2].ExternalUserID)

	// The reference is bound to the project that minted it.
	projects, ok := service.projects.(*findingProjects)
	require.True(t, ok)
	projects.project = ResolvedProject{ID: uuid.New(), Slug: "other", Name: "Other"}
	_, err = service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: first.Chats[0].UserReference, Limit: 0, Cursor: "",
	})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	require.Len(t, reader.params, 3, "an unresolvable reference never reaches the database")

	// And to the session that minted it.
	projects.project = project
	other := registrationServicePrincipal()
	_, err = service.List(t.Context(), other, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: first.Chats[0].UserReference, Limit: 0, Cursor: "",
	})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

// A chat attributed only through a linked account's email is masked but not
// referenceable, because the chat query cannot narrow to an email without also
// matching titles.
func TestListChatsEmailIdentityIsMaskedWithoutReference(t *testing.T) {
	t.Parallel()

	reader := &recordingChatReader{rows: []chatrepo.ListChatsRow{chatListRow(uuid.New(), chatListTestNow.Add(-time.Hour), "", "", "someone@example.invalid", 1)}, params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())

	output, err := service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: "",
	})
	require.NoError(t, err)
	require.Len(t, output.Chats, 1)
	require.Equal(t, "s***@e***", output.Chats[0].MaskedIdentity)
	require.Empty(t, output.Chats[0].UserReference)
}

func TestListChatsCursorResumesOnlyTheSameQuery(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	reader := &recordingChatReader{rows: chatListRows(3, 7), params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())

	first, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "7d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: "",
	})
	require.NoError(t, err)
	require.Len(t, first.Chats, 2, "the extra row detects the next page and is dropped")
	require.Equal(t, 7, first.TotalMatches)
	require.NotEmpty(t, first.NextCursor)
	require.NotContains(t, first.NextCursor, "o:2", "cursors are opaque")

	second, err := service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "7d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: first.NextCursor,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, reader.params[1].PageOffset)
	require.NotEmpty(t, second.NextCursor)

	// The same cursor against a different window, filter, or session is a
	// different page of a different question.
	for _, input := range []ListChatsInput{
		{ProjectID: "", ProjectSlug: "", Window: "24h", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: first.NextCursor},
		{ProjectID: "", ProjectSlug: "", Window: "7d", Risk: ChatRiskWithFindings, Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: first.NextCursor},
		{ProjectID: "", ProjectSlug: "", Window: "7d", Risk: "", Source: "codex", AssistantID: "", UserReference: "", Limit: 2, Cursor: first.NextCursor},
	} {
		_, err := service.List(t.Context(), principal, input)
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	}
	_, err = service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "7d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: first.NextCursor,
	})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	_, err = service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "7d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: "not-a-cursor",
	})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	require.Len(t, reader.params, 2, "a refused cursor never reaches the database")

	// A reference cannot be spent as a cursor.
	_, err = service.List(t.Context(), principal, ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "7d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 2, Cursor: first.Chats[0].UserReference,
	})
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

// The traversal cap bounds the chats handed over in total, not just per page:
// the last page before the cap is trimmed and carries no cursor.
func TestListChatsTraversalIsCapped(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	reader := &recordingChatReader{rows: chatListRows(maxChatListLimit+1, 10_000), params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())
	input := ListChatsInput{ProjectID: "", ProjectSlug: "", Window: "30d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: maxChatListLimit, Cursor: ""}

	handed := 0
	pages := 0
	for {
		output, err := service.List(t.Context(), principal, input)
		require.NoError(t, err)
		handed += len(output.Chats)
		pages++
		if output.NextCursor == "" {
			break
		}
		require.Less(t, pages, 20, "the walk must terminate")
		input.Cursor = output.NextCursor
	}
	require.Equal(t, maxChatListTraversal, handed)
	require.Equal(t, maxChatListTraversal/maxChatListLimit, pages)

	// A page size that does not divide the cap: the page straddling it is
	// trimmed to exactly the remainder and carries no cursor, so the total
	// handed over is still the cap.
	reader.rows = chatListRows(31, 10_000)
	input = ListChatsInput{ProjectID: "", ProjectSlug: "", Window: "30d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 30, Cursor: ""}
	handed, pages = 0, 0
	var last ListChatsOutput
	for {
		output, err := service.List(t.Context(), principal, input)
		require.NoError(t, err)
		handed += len(output.Chats)
		pages++
		last = output
		if output.NextCursor == "" {
			break
		}
		require.Less(t, pages, 30, "the walk must terminate")
		input.Cursor = output.NextCursor
	}
	require.Equal(t, maxChatListTraversal, handed)
	require.Equal(t, 17, pages)
	require.Len(t, last.Chats, maxChatListTraversal%30, "the last page is trimmed to the remainder")

	// A forged offset past the cap is refused even when sealed correctly.
	window, err := resolveWindow(input.Window, chatListTestNow, chatListWindowSpec)
	require.NoError(t, err)
	forged, err := service.references.EncodeScoped(principal, subjectKindCursor, mustChatListScope(t, service, input), formatChatListCursor(maxChatListTraversal+1, window), chatListTestNow)
	require.NoError(t, err)
	input.Cursor = forged
	_, err = service.List(t.Context(), principal, input)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

func mustChatListScope(t *testing.T, service *ChatMetadataService, input ListChatsInput) string {
	t.Helper()
	list, err := normalizeChatList(input)
	require.NoError(t, err)
	list.window, err = resolveWindow(input.Window, chatListTestNow, chatListWindowSpec)
	require.NoError(t, err)
	projects, ok := service.projects.(*findingProjects)
	require.True(t, ok)
	list.projectID = projects.project.ID
	return list.cursorScope()
}

func TestListChatsRefusesInvalidInput(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]ListChatsInput{
		"both selectors":    {ProjectID: uuid.NewString(), ProjectSlug: "default", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: ""},
		"unknown risk":      {ProjectID: "", ProjectSlug: "", Window: "", Risk: "maybe", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: ""},
		"bad assistant id":  {ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "not-a-uuid", UserReference: "", Limit: 0, Cursor: ""},
		"negative limit":    {ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: -1, Cursor: ""},
		"long source":       {ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: strings.Repeat("x", maxChatSourceLength+1), AssistantID: "", UserReference: "", Limit: 0, Cursor: ""},
		"control in source": {ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "claude\x00code", AssistantID: "", UserReference: "", Limit: 0, Cursor: ""},
	} {
		reader := &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0}
		service, _ := newChatMetadataService(t, reader, allowBudget())
		_, err := service.List(t.Context(), registrationServicePrincipal(), input)
		require.ErrorIs(t, err, ErrChatListInvalid, name)
		require.Empty(t, reader.params, name)
	}

	reader := &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, allowBudget())
	_, err := service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "90d", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: "",
	})
	require.ErrorIs(t, err, ErrDiagnosticWindowInvalid)
	require.Empty(t, reader.params)
}

func TestListChatsRefusesWithoutBudgetOrProject(t *testing.T) {
	t.Parallel()

	reader := &recordingChatReader{rows: nil, params: nil, err: nil, countParams: nil, count: 0}
	service, _ := newChatMetadataService(t, reader, OperationBudget{Connection: denyOperationLimiter{}, Organization: allowOperationLimiter{}})
	_, err := service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: "",
	})
	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Empty(t, reader.params, "a throttled caller never reaches the database")

	service, _ = newChatMetadataService(t, reader, allowBudget())
	projects, ok := service.projects.(*findingProjects)
	require.True(t, ok)
	projects.err = ErrRiskReadNotFound
	_, err = service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: uuid.NewString(), ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: "",
	})
	require.ErrorIs(t, err, ErrRiskReadNotFound)
	require.Empty(t, reader.params)

	reader.err = errors.New("private database detail")
	service, _ = newChatMetadataService(t, reader, allowBudget())
	_, err = service.List(t.Context(), registrationServicePrincipal(), ListChatsInput{
		ProjectID: "", ProjectSlug: "", Window: "", Risk: "", Source: "", AssistantID: "", UserReference: "", Limit: 0, Cursor: "",
	})
	require.ErrorContains(t, err, "list project chats")
}

// denyOperationLimiter models a throttled bucket.
type denyOperationLimiter struct{}

func (denyOperationLimiter) Allow(context.Context, string) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: time.Minute}, nil
}

func (denyOperationLimiter) AllowN(context.Context, string, int) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: time.Minute}, nil
}
