package memoryeval_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

var corpusNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func countChats(c *memoryeval.Corpus, keep func(memoryeval.Chat) bool) int {
	n := 0
	for _, chat := range c.Chats {
		if keep(chat) {
			n++
		}
	}
	return n
}

func TestBuildIsDeterministic(t *testing.T) {
	t.Parallel()
	first := memoryeval.Build(corpusNow)
	second := memoryeval.Build(corpusNow)
	require.Equal(t, first, second)
}

func TestCorpusShape(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	mainTeam := countChats(c, func(chat memoryeval.Chat) bool { return !chat.Isolated() })
	private := countChats(c, func(chat memoryeval.Chat) bool { return chat.Private })
	otherProject := countChats(c, func(chat memoryeval.Chat) bool { return chat.Project == memoryeval.ProjectOther })
	require.Equal(t, 147, mainTeam)
	require.Equal(t, 3, private)
	require.Equal(t, 12, otherProject)

	keys := map[string]bool{}
	ids := map[string]bool{}
	owners := map[string]int{}
	for _, chat := range c.Chats {
		require.False(t, keys[chat.Key], "duplicate key %s", chat.Key)
		require.False(t, ids[chat.ID.String()], "duplicate id for %s", chat.Key)
		keys[chat.Key] = true
		ids[chat.ID.String()] = true
		require.Equal(t, memoryeval.ChatID(chat.Key), chat.ID)
		require.GreaterOrEqual(t, len(chat.Messages), 2, chat.Key)
		require.NotContains(t, chat.Text(), "{{", chat.Key)
		require.True(t, chat.StartedAt().After(corpusNow.AddDate(0, 0, -61)), chat.Key)
		last := chat.Messages[len(chat.Messages)-1].At
		require.False(t, last.After(corpusNow), "%s ends in the future", chat.Key)
		if chat.Project == memoryeval.ProjectMain {
			owners[chat.Owner]++
		}
	}
	require.Len(t, c.Users, 4)
	for _, u := range c.Users {
		require.Positive(t, owners[u.Handle], u.Handle)
	}
	require.True(t, c.Chats[0].StartedAt().Before(corpusNow.AddDate(0, 0, -55)), "history should span about 60 days")
}

func TestPlantedDecisionsAndReversals(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	decisions := countChats(c, func(chat memoryeval.Chat) bool { return strings.HasPrefix(chat.Key, "decision-") })
	reversals := countChats(c, func(chat memoryeval.Chat) bool { return strings.HasPrefix(chat.Key, "reversal-") })
	require.Equal(t, 25, decisions)
	require.Equal(t, 8, reversals)

	for _, chat := range c.Chats {
		topic, ok := strings.CutPrefix(chat.Key, "reversal-")
		if !ok {
			continue
		}
		original, ok := c.Chat("decision-" + topic)
		require.True(t, ok, "reversal %s has no original decision", chat.Key)
		require.True(t, original.StartedAt().Before(chat.StartedAt()), "reversal %s must come after its decision", chat.Key)
	}
}

func TestQuestionMix(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	kinds := make(map[memoryeval.QuestionKind]int)
	ids := map[string]bool{}
	for _, q := range c.Questions {
		require.False(t, ids[q.ID], "duplicate question %s", q.ID)
		ids[q.ID] = true
		kinds[q.Kind]++
	}
	require.Len(t, c.Questions, 40)
	require.Equal(t, map[memoryeval.QuestionKind]int{
		memoryeval.QuestionPointLookup: 15,
		memoryeval.QuestionTimeline:    8,
		memoryeval.QuestionReversal:    7,
		memoryeval.QuestionNegative:    5,
		memoryeval.QuestionIsolation:   5,
	}, kinds)
}

func TestQuestionSourcesSupportTheirAnswers(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	for _, q := range c.Questions {
		if q.ExpectsNotFound() {
			require.Empty(t, q.Sources, q.ID)
			require.Empty(t, q.Evidence, q.ID)
			require.Equal(t, memoryeval.NotFound, q.Answer, q.ID)
			continue
		}
		require.NotEmpty(t, q.Sources, q.ID)
		require.True(t, q.Supports(q.Answer), "%s: the answer key lacks its own evidence", q.ID)
		for _, key := range q.Sources {
			chat, ok := c.Chat(key)
			require.True(t, ok, "%s cites unknown chat %s", q.ID, key)
			require.True(t, c.ChatCitationValid(q, chat.ID), "%s: source %s lacks the evidence", q.ID, key)
		}
		for _, group := range q.Evidence {
			supported := false
			for _, key := range q.Sources {
				chat, _ := c.Chat(key)
				single := memoryeval.Question{ID: q.ID, Kind: q.Kind, Text: q.Text, Answer: q.Answer, Sources: q.Sources, Evidence: [][]string{group}}
				supported = supported || single.Supports(chat.Text())
			}
			require.True(t, supported, "%s: evidence %v is in none of its sources", q.ID, group)
		}
	}
}

func TestTimelineSourcesAreChronological(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	for _, q := range c.Questions {
		if q.Kind != memoryeval.QuestionTimeline {
			continue
		}
		for i := 1; i < len(q.Sources); i++ {
			prev, _ := c.Chat(q.Sources[i-1])
			next, _ := c.Chat(q.Sources[i])
			require.True(t, prev.StartedAt().Before(next.StartedAt()), "%s: %s should precede %s", q.ID, prev.Key, next.Key)
		}
	}
}

func TestAuthTimelineFallsInTheLastMonth(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	for _, key := range []string{"timeline-auth-pkce", "timeline-auth-rotation", "timeline-auth-lifetime"} {
		chat, ok := c.Chat(key)
		require.True(t, ok)
		require.True(t, chat.StartedAt().After(corpusNow.AddDate(0, 0, -30)), key)
	}
}

func TestNegativeTopicsAppearNowhere(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	for _, chat := range c.Chats {
		text := strings.ToLower(chat.Text())
		for _, term := range []string{"graphql", "kafka", "soc 2", "dark mode", "elixir"} {
			require.NotContains(t, text, term, "negative topic %q appears in %s", term, chat.Key)
		}
	}
}

func TestCanariesStayInIsolatedChats(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	canaries := 0
	for _, chat := range c.Chats {
		if !chat.Isolated() {
			require.Empty(t, chat.Canaries, chat.Key)
			require.Empty(t, c.IsolationViolations(chat.Text()), "team chat %s contains a canary", chat.Key)
			continue
		}
		require.NotEmpty(t, chat.Canaries, "isolated chat %s needs a canary", chat.Key)
		for _, canary := range chat.Canaries {
			require.Contains(t, chat.Text(), canary, chat.Key)
			holders := countChats(c, func(other memoryeval.Chat) bool {
				return strings.Contains(strings.ToLower(other.Text()), strings.ToLower(canary))
			})
			require.Equal(t, 1, holders, "canary %q should appear only in %s", canary, chat.Key)
			canaries++
		}
	}
	require.Equal(t, 15, canaries)

	for _, q := range c.Questions {
		require.Empty(t, c.IsolationViolations(q.Text+"\n"+q.Answer), "question %s gives away a canary", q.ID)
	}
}

func TestSecretsAreCoveredAndPlacedInTeamChats(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	require.Len(t, c.Secrets, 10)
	categories := make(map[memoryeval.SecretCategory]bool)
	for _, s := range c.Secrets {
		categories[s.Category] = true
		holders := 0
		for _, chat := range c.Chats {
			if strings.Contains(chat.Text(), s.Value) {
				require.False(t, chat.Isolated(), "secret %s should sit in a team chat", s.Name)
				holders++
			}
		}
		require.Equal(t, 1, holders, "secret %s should appear in exactly one chat", s.Name)
	}
	require.Equal(t, map[memoryeval.SecretCategory]bool{
		memoryeval.SecretCategoryCredential:   true,
		memoryeval.SecretCategoryPII:          true,
		memoryeval.SecretCategoryFinancial:    true,
		memoryeval.SecretCategoryGovernmentID: true,
		memoryeval.SecretCategoryHealthcare:   true,
	}, categories)
}

func TestCardNumberPassesLuhn(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	for _, s := range c.Secrets {
		if s.Category != memoryeval.SecretCategoryFinancial {
			continue
		}
		digits := strings.ReplaceAll(s.Value, " ", "")
		require.Len(t, digits, 16)
		require.NotEqual(t, "4111111111111111", digits)
		sum := 0
		for i := range len(digits) {
			d := int(digits[len(digits)-1-i] - '0')
			if i%2 == 1 {
				d *= 2
				if d > 9 {
					d -= 9
				}
			}
			sum += d
		}
		require.Zero(t, sum%10, s.Value)
	}
}

func TestWithChatIDsKeepsCitationsByKey(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(corpusNow)
	remapped := corpus.WithChatIDs(func(key string) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte("tenant-"+key)) })

	require.Len(t, remapped.Chats, len(corpus.Chats))
	require.Equal(t, memoryeval.ChatID(corpus.Chats[0].Key), corpus.Chats[0].ID, "the original corpus is unchanged")
	for _, q := range remapped.Questions {
		if q.ExpectsNotFound() {
			continue
		}
		source, ok := remapped.Chat(q.Sources[0])
		require.True(t, ok, q.ID)
		require.NotEqual(t, memoryeval.ChatID(source.Key), source.ID, q.ID)
		require.Equal(t, corpus.ChatCitationValid(q, memoryeval.ChatID(source.Key)), remapped.ChatCitationValid(q, source.ID), q.ID)
		require.False(t, remapped.ChatCitationValid(q, memoryeval.ChatID(source.Key)), "old IDs no longer resolve")
	}
}
