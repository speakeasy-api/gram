package memoryeval_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// scorecard holds the mechanical results for one arm. None of it needs an
// LLM, so it runs in CI on every pull request.
type scorecard struct {
	arm string

	// leaks are the planted secrets the sweep returned, by category.
	leaks map[memoryeval.SecretCategory][]string

	// privateCanaries and otherProjectCanaries are isolation violations.
	privateCanaries      []string
	otherProjectCanaries []string

	// retrieved counts answerable questions whose source chats all came back
	// from keyword queries on the question's evidence terms.
	retrieved  int
	answerable int

	// missed lists the questions that were not retrieved.
	missed []string

	// tokensPerQuestion is the mean tool output per answerable question.
	tokensPerQuestion int
}

func (s scorecard) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "arm %s\n", s.arm)
	for _, category := range []memoryeval.SecretCategory{memoryeval.SecretCategoryCredential, memoryeval.SecretCategoryPII, memoryeval.SecretCategoryFinancial, memoryeval.SecretCategoryGovernmentID, memoryeval.SecretCategoryHealthcare} {
		fmt.Fprintf(&b, "  leaks %-14s %d %v\n", category, len(s.leaks[category]), s.leaks[category])
	}
	fmt.Fprintf(&b, "  isolation private  %d %v\n", len(s.privateCanaries), s.privateCanaries)
	fmt.Fprintf(&b, "  isolation project  %d %v\n", len(s.otherProjectCanaries), s.otherProjectCanaries)
	fmt.Fprintf(&b, "  retrieval          %d/%d missed %v\n", s.retrieved, s.answerable, s.missed)
	fmt.Fprintf(&b, "  tokens/question    %d\n", s.tokensPerQuestion)
	return b.String()
}

// probe runs the mechanical checks against one arm.
func probe(ctx context.Context, t *testing.T, corpus *memoryeval.Corpus, a bench.Arm) scorecard {
	t.Helper()
	card := scorecard{arm: a.Name, leaks: make(map[memoryeval.SecretCategory][]string)}

	swept, err := a.Sweep(ctx)
	require.NoError(t, err)
	for _, secret := range corpus.Leaks(swept.Output) {
		card.leaks[secret.Category] = append(card.leaks[secret.Category], secret.Name)
	}
	for _, canary := range corpus.IsolationViolations(swept.Output) {
		if canaryIsPrivate(corpus, canary) {
			card.privateCanaries = append(card.privateCanaries, canary)
		} else {
			card.otherProjectCanaries = append(card.otherProjectCanaries, canary)
		}
	}

	tokens := 0
	for _, q := range corpus.Questions {
		if q.ExpectsNotFound() {
			continue
		}
		card.answerable++
		var found []uuid.UUID
		for _, group := range q.Evidence {
			result, err := a.Search(ctx, group[0])
			require.NoError(t, err)
			tokens += memoryeval.EstimateTokens(result.Output)
			found = append(found, result.ChatIDs...)
		}
		if retrievedAll(q, found) {
			card.retrieved++
		} else {
			card.missed = append(card.missed, q.ID)
		}
	}
	card.tokensPerQuestion = tokens / max(card.answerable, 1)
	return card
}

func canaryIsPrivate(corpus *memoryeval.Corpus, canary string) bool {
	for _, chat := range corpus.Chats {
		if slices.Contains(chat.Canaries, canary) {
			return chat.Private
		}
	}
	return false
}

func retrievedAll(q memoryeval.Question, found []uuid.UUID) bool {
	for _, key := range q.Sources {
		if !slices.Contains(found, memoryeval.ChatID(key)) {
			return false
		}
	}
	return true
}

// TestSearchTeamMemoryMechanical scores search_team_memory on the corpus. The
// RFC bar is zero secret leaks and zero isolation violations.
func TestSearchTeamMemoryMechanical(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(corpusNow)
	loaded := loadCorpus(t, "memoryeval_search_team_memory", corpus)
	writeProxySummaries(t, loaded)
	memoryArm, err := bench.SearchTeamMemoryArm(testenv.NewLogger(t), loaded.target())
	require.NoError(t, err)

	card := probe(t.Context(), t, corpus, memoryArm)
	t.Log("\n" + card.String())

	require.Empty(t, card.leaks[memoryeval.SecretCategoryCredential], "search_team_memory masks credentials")
	require.Empty(t, card.privateCanaries, "personal-account sessions never reach team memory")
	require.Empty(t, card.otherProjectCanaries, "team memory is scoped to one project")
}

// TestSearchChatsBaselineMechanical scores today's search_chats on the
// corpus. It pins what the baseline is expected to get right, so a regression
// in the harness or in search shows up here, and logs the rest of the
// scorecard (run with -v to see it).
func TestSearchChatsBaselineMechanical(t *testing.T) {
	t.Parallel()
	corpus := memoryeval.Build(corpusNow)
	loaded := loadCorpus(t, "memoryeval_search_chats", corpus)
	searchArm, err := bench.SearchChatsArm(testenv.NewLogger(t), loaded.target())
	require.NoError(t, err)

	card := probe(t.Context(), t, corpus, searchArm)
	t.Log("\n" + card.String())

	require.Empty(t, card.leaks[memoryeval.SecretCategoryCredential], "search_chats masks credentials with secret scanning")
	require.Empty(t, card.otherProjectCanaries, "search_chats is scoped to one project")
	require.Equal(t, card.answerable, card.retrieved, "every answerable question's source chats are returned by a keyword search for their evidence terms")
}
