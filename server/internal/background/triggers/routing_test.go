package triggers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func threadRoute(state ThreadRouteState, cursor string) *triggerrepo.TriggerThreadRoute {
	return &triggerrepo.TriggerThreadRoute{
		State:          string(state),
		LastSeenCursor: pgtype.Text{String: cursor, Valid: cursor != ""},
	}
}

func TestDecideRoute(t *testing.T) {
	t.Parallel()

	reply := RoutingFacts{Addressed: false, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: ""}
	mentionInThread := RoutingFacts{Addressed: true, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: ""}
	mentionTopLevel := RoutingFacts{Addressed: true, SelfAuthored: false, InThread: false, Cursor: "2.0", Conversation: true, DedupKey: ""}
	ownMessage := RoutingFacts{Addressed: true, SelfAuthored: true, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: ""}
	reaction := RoutingFacts{Addressed: false, SelfAuthored: false, InThread: false, Cursor: "2.0", Conversation: false, DedupKey: ""}

	subscribed, unsubscribed := ThreadRouteSubscribed, ThreadRouteUnsubscribed

	tests := []struct {
		name     string
		policy   RoutingPolicy
		facts    RoutingFacts
		route    *triggerrepo.TriggerThreadRoute
		action   routeAction
		record   *ThreadRouteState
		advance  bool
		backfill *ThreadBackfill
	}{
		{name: "own message is skipped", policy: RoutingPolicyChannels, facts: ownMessage, route: nil, action: routeSkip, record: nil, advance: false, backfill: nil},
		{name: "extra event bypasses reply rules", policy: RoutingPolicyThreads, facts: reaction, route: nil, action: routeDispatch, record: nil, advance: false, backfill: nil},
		{name: "extra event on left thread is still delivered", policy: RoutingPolicyThreads, facts: reaction, route: threadRoute(ThreadRouteUnsubscribed, "1.0"), action: routeDispatch, record: nil, advance: false, backfill: nil},

		{name: "mentions: reply is skipped", policy: RoutingPolicyMentions, facts: reply, route: nil, action: routeSkip, record: nil, advance: false, backfill: nil},
		{name: "mentions: first mention mid-thread backfills whole thread without following", policy: RoutingPolicyMentions, facts: mentionInThread, route: nil, action: routeDispatch, record: &unsubscribed, advance: false, backfill: &ThreadBackfill{After: ""}},
		{name: "mentions: later mention backfills since last seen", policy: RoutingPolicyMentions, facts: mentionInThread, route: threadRoute(ThreadRouteUnsubscribed, "1.0"), action: routeDispatch, record: &unsubscribed, advance: false, backfill: &ThreadBackfill{After: "1.0"}},
		{name: "mentions: reply on thread routed to the target is delivered", policy: RoutingPolicyMentions, facts: reply, route: threadRoute(ThreadRouteSubscribed, "1.0"), action: routeDispatch, record: nil, advance: true, backfill: nil},

		{name: "threads: reply on unknown thread is skipped", policy: RoutingPolicyThreads, facts: reply, route: nil, action: routeSkip, record: nil, advance: false, backfill: nil},
		{name: "threads: first mention mid-thread subscribes and backfills", policy: RoutingPolicyThreads, facts: mentionInThread, route: nil, action: routeDispatch, record: &subscribed, advance: false, backfill: &ThreadBackfill{After: ""}},
		{name: "threads: top-level mention has nothing to backfill", policy: RoutingPolicyThreads, facts: mentionTopLevel, route: nil, action: routeDispatch, record: &subscribed, advance: false, backfill: nil},
		{name: "threads: reply on subscribed thread only advances the cursor", policy: RoutingPolicyThreads, facts: reply, route: threadRoute(ThreadRouteSubscribed, "1.0"), action: routeDispatch, record: nil, advance: true, backfill: nil},
		{name: "threads: reply on left thread is skipped", policy: RoutingPolicyThreads, facts: reply, route: threadRoute(ThreadRouteUnsubscribed, "1.0"), action: routeSkip, record: nil, advance: false, backfill: nil},
		{name: "threads: mention on left thread re-subscribes and backfills", policy: RoutingPolicyThreads, facts: mentionInThread, route: threadRoute(ThreadRouteUnsubscribed, "1.0"), action: routeDispatch, record: &subscribed, advance: false, backfill: &ThreadBackfill{After: "1.0"}},
		{name: "threads: mention on subscribed thread needs no backfill", policy: RoutingPolicyThreads, facts: mentionInThread, route: threadRoute(ThreadRouteSubscribed, "1.0"), action: routeDispatch, record: &subscribed, advance: false, backfill: nil},

		{name: "channels: reply on unknown thread is delivered untracked", policy: RoutingPolicyChannels, facts: reply, route: nil, action: routeDispatch, record: nil, advance: false, backfill: nil},
		{name: "channels: mention on unknown thread stays untracked", policy: RoutingPolicyChannels, facts: mentionInThread, route: nil, action: routeDispatch, record: nil, advance: false, backfill: nil},
		{name: "channels: reply on left thread is skipped", policy: RoutingPolicyChannels, facts: reply, route: threadRoute(ThreadRouteUnsubscribed, "1.0"), action: routeSkip, record: nil, advance: false, backfill: nil},
		{name: "channels: mention on left thread re-subscribes and backfills", policy: RoutingPolicyChannels, facts: mentionInThread, route: threadRoute(ThreadRouteUnsubscribed, "1.0"), action: routeDispatch, record: &subscribed, advance: false, backfill: &ThreadBackfill{After: "1.0"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			decision := decideRoute(routeInput{policy: tc.policy, facts: tc.facts, route: tc.route})
			require.Equal(t, tc.action, decision.action)
			require.Equal(t, tc.record, decision.record)
			require.Equal(t, tc.advance, decision.advance)
			require.Equal(t, tc.backfill, decision.backfill)
		})
	}
}

func TestSlackRoutingFacts(t *testing.T) {
	t.Parallel()

	config := slackTriggerConfig{FilterExpr: "", EventTypes: nil, Routing: RoutingPolicyThreads, compiledFilter: nil}
	base := slackTriggerEvent{EnvelopeType: "event_callback", EventType: "message", ChannelID: "C1", ThreadID: "1.0", Timestamp: "2.0", UserID: "U1", BotUserID: "UBOT", SelfBotID: "BSELF"}

	tests := []struct {
		name  string
		event func(slackTriggerEvent) slackTriggerEvent
		want  RoutingFacts
	}{
		{
			name:  "ambient thread reply",
			event: func(e slackTriggerEvent) slackTriggerEvent { return e },
			want:  RoutingFacts{Addressed: false, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:C1:2.0"},
		},
		{
			name:  "message mentioning the bot is addressed",
			event: func(e slackTriggerEvent) slackTriggerEvent { e.Text = "hey <@UBOT> look"; return e },
			want:  RoutingFacts{Addressed: true, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:C1:2.0"},
		},
		{
			name:  "app mention shares the message dedup key",
			event: func(e slackTriggerEvent) slackTriggerEvent { e.EventType = "app_mention"; return e },
			want:  RoutingFacts{Addressed: true, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:C1:2.0"},
		},
		{
			name:  "direct message is addressed",
			event: func(e slackTriggerEvent) slackTriggerEvent { e.ChannelID = "D1"; return e },
			want:  RoutingFacts{Addressed: true, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:D1:2.0"},
		},
		{
			name: "button click is addressed and not deduplicated",
			event: func(e slackTriggerEvent) slackTriggerEvent {
				e.EnvelopeType = "interactive"
				e.EventType = "block_actions"
				return e
			},
			want: RoutingFacts{Addressed: true, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: ""},
		},
		{
			name:  "message from the bot user is self-authored",
			event: func(e slackTriggerEvent) slackTriggerEvent { e.UserID = "UBOT"; return e },
			want:  RoutingFacts{Addressed: false, SelfAuthored: true, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:C1:2.0"},
		},
		{
			name:  "message from the bot without a user is self-authored",
			event: func(e slackTriggerEvent) slackTriggerEvent { e.UserID = ""; e.BotID = "BSELF"; return e },
			want:  RoutingFacts{Addressed: false, SelfAuthored: true, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:C1:2.0"},
		},
		{
			name:  "another bot is not self-authored",
			event: func(e slackTriggerEvent) slackTriggerEvent { e.UserID = ""; e.BotID = "BOTHER"; return e },
			want:  RoutingFacts{Addressed: false, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:C1:2.0"},
		},
		{
			name: "any bot message is self-authored when the app's bot is unknown",
			event: func(e slackTriggerEvent) slackTriggerEvent {
				e.UserID = ""
				e.BotID = "BOTHER"
				e.SelfBotID = ""
				return e
			},
			want: RoutingFacts{Addressed: false, SelfAuthored: true, InThread: true, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:C1:2.0"},
		},
		{
			name:  "reaction is outside the conversation rules",
			event: func(e slackTriggerEvent) slackTriggerEvent { e.EventType = "reaction_added"; return e },
			want:  RoutingFacts{Addressed: false, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: false, DedupKey: ""},
		},
		{
			name:  "top-level message is not in a thread",
			event: func(e slackTriggerEvent) slackTriggerEvent { e.ThreadID = ""; return e },
			want:  RoutingFacts{Addressed: false, SelfAuthored: false, InThread: false, Cursor: "2.0", Conversation: true, DedupKey: "slack-message:C1:2.0"},
		},
		{
			name: "edit mentioning the bot is not addressed",
			event: func(e slackTriggerEvent) slackTriggerEvent {
				e.Subtype = "message_changed"
				e.Text = "<@UBOT>"
				return e
			},
			want: RoutingFacts{Addressed: false, SelfAuthored: false, InThread: true, Cursor: "2.0", Conversation: false, DedupKey: ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			facts, ok := config.RoutingFacts(tc.event(base))
			require.True(t, ok)
			require.Equal(t, tc.want, facts)
		})
	}
}

func TestSlackRoutingFactsWithoutPresetDoNotDeduplicate(t *testing.T) {
	t.Parallel()

	config := slackTriggerConfig{FilterExpr: "", EventTypes: nil, Routing: "", compiledFilter: nil}
	facts, ok := config.RoutingFacts(slackTriggerEvent{EnvelopeType: "event_callback", EventType: "app_mention", ChannelID: "C1", Timestamp: "2.0"})
	require.True(t, ok)
	require.Empty(t, facts.DedupKey)
	require.Equal(t, RoutingPolicyChannels, config.RoutingPolicy())
}

func TestSlackRoutingFactsIgnoresChannellessEvents(t *testing.T) {
	t.Parallel()

	config := slackTriggerConfig{FilterExpr: "", EventTypes: nil, Routing: RoutingPolicyThreads, compiledFilter: nil}
	_, ok := config.RoutingFacts(slackTriggerEvent{EnvelopeType: "event_callback", EventType: "team_join", UserID: "U1"})
	require.False(t, ok)
}

func TestSlackFilterWithPreset(t *testing.T) {
	t.Parallel()

	config := slackTriggerConfig{FilterExpr: "", EventTypes: []string{"reaction_added"}, Routing: RoutingPolicyMentions, compiledFilter: nil}
	tests := []struct {
		name  string
		event slackTriggerEvent
		want  bool
	}{
		{name: "posted message bypasses event_types", event: slackTriggerEvent{EventType: "message"}, want: true},
		{name: "file share bypasses event_types", event: slackTriggerEvent{EventType: "message", Subtype: "file_share"}, want: true},
		{name: "app mention bypasses event_types", event: slackTriggerEvent{EventType: "app_mention"}, want: true},
		{name: "button click bypasses event_types", event: slackTriggerEvent{EnvelopeType: "interactive", EventType: "block_actions"}, want: true},
		{name: "message edit is dropped", event: slackTriggerEvent{EventType: "message", Subtype: "message_changed"}, want: false},
		{name: "listed extra passes", event: slackTriggerEvent{EventType: "reaction_added"}, want: true},
		{name: "unlisted extra is dropped", event: slackTriggerEvent{EventType: "member_joined_channel"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Filter(tc.event)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSlackFilterWithPresetAndNoExtras(t *testing.T) {
	t.Parallel()

	config := slackTriggerConfig{FilterExpr: "", EventTypes: nil, Routing: RoutingPolicyThreads, compiledFilter: nil}
	got, err := config.Filter(slackTriggerEvent{EventType: "reaction_added"})
	require.NoError(t, err)
	require.False(t, got)

	got, err = config.Filter(slackTriggerEvent{EventType: "message"})
	require.NoError(t, err)
	require.True(t, got)
}

func TestWithSlackBotIdentityStampsEvent(t *testing.T) {
	t.Parallel()

	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":true,"user_id":"UBOT","bot_id":"BSELF"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	logger := slog.New(slog.DiscardHandler) //nolint:forbidigo // GG006: testenv imports this package
	app := &App{
		logger:             logger,
		slackClient:        slackclient.NewSlackClientWithBaseURL(server.URL, server.Client()),
		slackBotIdentities: cache.NewTypedObjectCache[slackBotIdentity](logger.With(attr.SlogCacheNamespace("test")), cache.NoopCache, cache.SuffixNone),
	}

	got := app.withSlackBotIdentity(
		t.Context(),
		triggerrepo.TriggerInstance{DefinitionSlug: DefinitionSlugSlack},
		map[string]string{"SLACK_BOT_TOKEN": "xoxb-test-token"},
		slackTriggerEvent{EventType: "message", ChannelID: "C1"},
	)

	require.Equal(t, "Bearer xoxb-test-token", <-authorization)
	evt, ok := got.(slackTriggerEvent)
	require.True(t, ok)
	require.Equal(t, "UBOT", evt.BotUserID)
	require.Equal(t, "BSELF", evt.SelfBotID)
}
