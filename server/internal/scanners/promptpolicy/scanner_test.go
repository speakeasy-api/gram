package promptpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
)

func TestScannerScanMatchedReturnsFinding(t *testing.T) {
	t.Parallel()

	var judged Input
	scanner := NewScanner(nil, func(_ context.Context, in Input) (*Verdict, error) {
		judged = in
		return &Verdict{
			Matched:          true,
			Confidence:       0.9,
			Rationale:        "matched policy",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			STokens:          1,
			Completed:        true,
			Model:            "test",
			Provider:         "test",
		}, nil
	})

	result := scanner.Scan(t.Context(), "org", "proj", "user-1", "flag deletes", Config{Temperature: nil, FailOpen: true}, judgemessage.New(message.User, "", "delete prod"))
	require.Len(t, result.Findings, 1)
	require.Equal(t, Source, result.Findings[0].Source)
	require.Equal(t, Rule, result.Findings[0].RuleID)
	require.Equal(t, "matched policy", result.Findings[0].Description)
	require.True(t, result.Completed)
	require.Equal(t, int64(1), result.STokens)
	require.Equal(t, "user-1", judged.UserID, "the scanned user's id must reach the judge input")
}

func TestScannerScanUnmatchedReturnsNoFindings(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(nil, func(_ context.Context, _ Input) (*Verdict, error) {
		return &Verdict{
			Matched:          false,
			Confidence:       0.1,
			Rationale:        "not matched",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			STokens:          1,
			Completed:        true,
			Model:            "test",
			Provider:         "test",
		}, nil
	})

	result := scanner.Scan(t.Context(), "org", "proj", "", "flag deletes", Config{Temperature: nil, FailOpen: true}, judgemessage.New(message.User, "", "hello"))
	require.Empty(t, result.Findings)
}

func TestScannerScanErrorFailOpenReturnsNoFindings(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(nil, func(_ context.Context, _ Input) (*Verdict, error) {
		return nil, errors.New("judge failed")
	})

	result := scanner.Scan(t.Context(), "org", "proj", "", "flag deletes", Config{Temperature: nil, FailOpen: true}, judgemessage.New(message.User, "", "delete prod"))
	require.Empty(t, result.Findings)
}

func TestScannerScanErrorFailClosedReturnsFinding(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(nil, func(_ context.Context, _ Input) (*Verdict, error) {
		return nil, errors.New("judge failed")
	})

	result := scanner.Scan(t.Context(), "org", "proj", "", "flag deletes", Config{Temperature: nil, FailOpen: false}, judgemessage.New(message.User, "", "delete prod"))
	require.Len(t, result.Findings, 1)
	require.Equal(t, "Policy judge was unavailable; flagged by fail-closed policy.", result.Findings[0].Description)
	require.False(t, result.Completed)
	require.Zero(t, result.STokens)
}

func TestScannerScanBlankPromptFailClosedReturnsFinding(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(nil, nil)

	result := scanner.Scan(t.Context(), "org", "proj", "", "   ", Config{Temperature: nil, FailOpen: false}, judgemessage.New(message.User, "", "delete prod"))
	require.Len(t, result.Findings, 1)
	require.Equal(t, "Policy judge was unavailable; flagged by fail-closed policy.", result.Findings[0].Description)
	require.False(t, result.Completed)
}
