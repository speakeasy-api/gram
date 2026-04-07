package accesstest

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// AlwaysEnabledFeatureChecker is a FeatureChecker that reports every feature as enabled.
// Use it in tests that need to pass a non-nil FeatureChecker to access.NewManager.
type AlwaysEnabledFeatureChecker struct{}

func (AlwaysEnabledFeatureChecker) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	return true, nil
}
