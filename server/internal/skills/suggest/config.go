package suggest

import (
	"fmt"
	"math"
	"time"
)

const Model = "openai/gpt-5.6-terra"

// Config is the complete suggestion policy. DefaultConfig is the production
// policy; fields remain configurable so the policy can be tested without time
// or evidence-heavy fixtures.
type Config struct {
	MinUnreviewedFeedback       int
	RegressionAbsoluteDelta     float64
	MinRegressionScoredSessions uint64
	TrendWindow                 time.Duration
	ActivityWindow              time.Duration
	SuggestionFloor             time.Duration
	MinAdditionalScoredSessions uint64
	DismissedScoredAdvance      uint64
	TranscriptWindow            time.Duration
	MaxTranscripts              int32
	MaxFeedback                 int32
	Model                       string
	Timeout                     time.Duration
	MaxRationaleRunes           int
}

func DefaultConfig() Config {
	return Config{
		MinUnreviewedFeedback:       5,
		RegressionAbsoluteDelta:     0.10,
		MinRegressionScoredSessions: 10,
		TrendWindow:                 90 * 24 * time.Hour,
		ActivityWindow:              7 * 24 * time.Hour,
		SuggestionFloor:             7 * 24 * time.Hour,
		MinAdditionalScoredSessions: 5,
		DismissedScoredAdvance:      10,
		TranscriptWindow:            30 * 24 * time.Hour,
		MaxTranscripts:              3,
		MaxFeedback:                 50,
		Model:                       Model,
		Timeout:                     120 * time.Second,
		MaxRationaleRunes:           2000,
	}
}

func (c Config) validate() error {
	switch {
	case c.MinUnreviewedFeedback <= 0:
		return fmt.Errorf("minimum unreviewed feedback must be positive")
	case math.IsNaN(c.RegressionAbsoluteDelta) || math.IsInf(c.RegressionAbsoluteDelta, 0) || c.RegressionAbsoluteDelta <= 0 || c.RegressionAbsoluteDelta > 1:
		return fmt.Errorf("regression delta must be between zero and one")
	case c.MinRegressionScoredSessions == 0:
		return fmt.Errorf("minimum regression sessions must be positive")
	case c.TrendWindow <= 0 || c.ActivityWindow <= 0 || c.SuggestionFloor <= 0 || c.TranscriptWindow <= 0:
		return fmt.Errorf("suggestion windows must be positive")
	case c.MinAdditionalScoredSessions == 0 || c.DismissedScoredAdvance == 0:
		return fmt.Errorf("scored session advances must be positive")
	case c.MaxTranscripts < 0 || c.MaxTranscripts > 3:
		return fmt.Errorf("maximum transcripts must be between zero and three")
	case c.MaxFeedback <= 0 || c.MaxFeedback > 50:
		return fmt.Errorf("maximum feedback must be between one and 50")
	case c.Model == "":
		return fmt.Errorf("model cannot be empty")
	case c.Timeout <= 0:
		return fmt.Errorf("suggestion timeout must be positive")
	case c.MaxRationaleRunes <= 0:
		return fmt.Errorf("maximum rationale runes must be positive")
	default:
		return nil
	}
}
