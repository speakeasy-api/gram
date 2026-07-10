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

	scanner := NewScanner(nil, func(_ context.Context, _ Input) (*Verdict, error) {
		return &Verdict{
			Matched:          true,
			Confidence:       0.9,
			Rationale:        "matched policy",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		}, nil
	})

	findings := scanner.Scan(t.Context(), "org", "proj", "flag deletes", Config{Model: "", Temperature: nil, FailOpen: true}, judgemessage.New(message.User, "", "delete prod"))
	require.Len(t, findings, 1)
	require.Equal(t, Source, findings[0].Source)
	require.Equal(t, Rule, findings[0].RuleID)
	require.Equal(t, "matched policy", findings[0].Description)
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
		}, nil
	})

	findings := scanner.Scan(t.Context(), "org", "proj", "flag deletes", Config{Model: "", Temperature: nil, FailOpen: true}, judgemessage.New(message.User, "", "hello"))
	require.Empty(t, findings)
}

func TestScannerScanErrorFailOpenReturnsNoFindings(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(nil, func(_ context.Context, _ Input) (*Verdict, error) {
		return nil, errors.New("judge failed")
	})

	findings := scanner.Scan(t.Context(), "org", "proj", "flag deletes", Config{Model: "", Temperature: nil, FailOpen: true}, judgemessage.New(message.User, "", "delete prod"))
	require.Empty(t, findings)
}

func TestScannerScanErrorFailClosedReturnsFinding(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(nil, func(_ context.Context, _ Input) (*Verdict, error) {
		return nil, errors.New("judge failed")
	})

	findings := scanner.Scan(t.Context(), "org", "proj", "flag deletes", Config{Model: "", Temperature: nil, FailOpen: false}, judgemessage.New(message.User, "", "delete prod"))
	require.Len(t, findings, 1)
	require.Equal(t, "Policy judge was unavailable; flagged by fail-closed policy.", findings[0].Description)
}

func TestScannerScanBlankPromptFailClosedReturnsFinding(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(nil, nil)

	findings := scanner.Scan(t.Context(), "org", "proj", "   ", Config{Model: "", Temperature: nil, FailOpen: false}, judgemessage.New(message.User, "", "delete prod"))
	require.Len(t, findings, 1)
	require.Equal(t, "Policy judge was unavailable; flagged by fail-closed policy.", findings[0].Description)
}
