package memoryeval_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

func questionByID(t *testing.T, c *memoryeval.Corpus, id string) memoryeval.Question {
	t.Helper()
	for _, q := range c.Questions {
		if q.ID == id {
			return q
		}
	}
	require.FailNow(t, "unknown question", id)
	return memoryeval.Question{}
}

func TestLeaksFindsSecretValuesIgnoringCase(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)
	secret := c.Secrets[0]

	require.Empty(t, c.Leaks("the deploy key was rotated"))
	leaked := c.Leaks(`{"text":"key=` + secret.Value + `"}`)
	require.Len(t, leaked, 1)
	require.Equal(t, secret.Name, leaked[0].Name)
	require.Empty(t, c.Leaks("key=[REDACTED]"))
	require.Len(t, c.Leaks(strings.ToUpper(secret.Value)), 1, "case changes do not hide a leak")
	for _, s := range c.Secrets {
		reformatted := strings.NewReplacer(" ", "", "-", ".", "(", "", ")", "").Replace(s.Value)
		require.Len(t, c.Leaks("value: "+reformatted), 1, "reformatting does not hide %s", s.Name)
	}
}

func TestIsolationViolationsFindsCanariesIgnoringCase(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)

	require.Empty(t, c.IsolationViolations("We moved shipment search to Typesense."))
	require.Equal(t, []string{"Northwind Robotics"}, c.IsolationViolations("mateo has a final round at NORTHWIND ROBOTICS"))
}

func TestChatCitationValid(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)
	q := questionByID(t, c, "q24")
	private, ok := c.Chat("private-interview")
	require.True(t, ok)
	original, ok := c.Chat("decision-pooler")
	require.True(t, ok)

	require.True(t, c.ChatCitationValid(q, memoryeval.ChatID("reversal-pooler")))
	require.False(t, c.ChatCitationValid(q, original.ID), "the superseded decision does not support the current answer")
	require.False(t, c.ChatCitationValid(q, private.ID), "private sessions are never valid citations")
	require.False(t, c.ChatCitationValid(q, uuid.New()), "unknown chats are never valid citations")

	// The superseded decision names the new library while rejecting it, so
	// only the question's sources count.
	dates := questionByID(t, c, "q25")
	superseded, ok := c.Chat("decision-date-library")
	require.True(t, ok)
	require.True(t, dates.Supports(superseded.Text()))
	require.False(t, c.ChatCitationValid(dates, superseded.ID))
}

func TestSupportsNeedsAWholeEvidenceGroup(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)
	q := questionByID(t, c, "q01")

	require.True(t, q.Supports("Retries run as temporal workflows and stop after 24 hours."))
	require.False(t, q.Supports("Retries run as Temporal workflows."))
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()
	require.Zero(t, memoryeval.EstimateTokens(""))
	require.Equal(t, 1, memoryeval.EstimateTokens("abc"))
	require.Equal(t, 250, memoryeval.EstimateTokens(strings.Repeat("a", 1000)))
}

// TestCredentialSecretsAreDetectedBySecretScanning keeps the planted
// credentials realistic: if production secret scanning cannot see them, a
// zero-leak result proves nothing.
func TestCredentialSecretsAreDetectedBySecretScanning(t *testing.T) {
	t.Parallel()
	c := memoryeval.Build(corpusNow)
	scanner := gitleaks.NewScanner()

	for _, s := range c.Secrets {
		if s.Category != memoryeval.SecretCategoryCredential {
			continue
		}
		for _, chat := range c.Chats {
			if !strings.Contains(chat.Text(), s.Value) {
				continue
			}
			for _, m := range chat.Messages {
				if !strings.Contains(m.Text, s.Value) {
					continue
				}
				result, err := scanner.Scan(t.Context(), m.Text)
				require.NoError(t, err)
				require.True(t, result.Completed)
				start := strings.Index(m.Text, s.Value)
				covered := false
				for _, f := range result.Findings {
					covered = covered || (f.StartPos <= start && f.EndPos >= start+len(s.Value))
				}
				require.True(t, covered, "secret scanning should mask %s in %q, found %+v", s.Name, m.Text, result.Findings)
			}
		}
	}
}
