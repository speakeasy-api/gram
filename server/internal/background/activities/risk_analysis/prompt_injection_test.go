package risk_analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectPromptInjection(t *testing.T) {
	t.Parallel()

	t.Run("empty input returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, DetectPromptInjection(""))
	})

	t.Run("no marker returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, DetectPromptInjection("hello world, ignore previous instructions"))
	})

	t.Run("single match", func(t *testing.T) {
		t.Parallel()
		text := "please run __INJECT__ now"
		findings := DetectPromptInjection(text)
		require.Len(t, findings, 1)
		f := findings[0]
		assert.Equal(t, SourcePromptInjection, f.Source)
		assert.Equal(t, promptInjectionRuleStub, f.RuleID)
		assert.Equal(t, promptInjectionStubMarker, f.Match)
		assert.Equal(t, promptInjectionStubMarker, text[f.StartPos:f.EndPos])
		assert.InDelta(t, 1.0, f.Confidence, 0.0001)
	})

	t.Run("multiple non-overlapping matches", func(t *testing.T) {
		t.Parallel()
		findings := DetectPromptInjection("__INJECT__ and again __INJECT__")
		require.Len(t, findings, 2)
		assert.Less(t, findings[0].StartPos, findings[1].StartPos)
	})
}
