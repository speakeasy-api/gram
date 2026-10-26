//go:build setupcorpus_freshness

package setupcorpus_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Freshness CI: the pinned export must be recent enough that its guides are
// still inside their revalidation window. This failing is the signal to review
// upstream and bump the pinned version — it is what makes the 90-day review
// commitment enforceable rather than aspirational.
//
// The result depends on the wall clock, not on the change under test, so it is
// build-tagged out of the sharded server suite: a guide aging out would
// otherwise fail every pull request at once. The scheduled setup-corpus
// freshness workflow is its only runner.
func TestPinnedCorpusIsWithinItsRevalidationWindow(t *testing.T) {
	t.Parallel()

	now := time.Now()
	for _, resource := range build(t) {
		require.Falsef(t, now.After(resource.RevalidateBy),
			"setup guide %s was last observed %s and was due revalidation %s: review the guide upstream in mcp-setup-docs and bump the pinned go/vX.Y.Z module version",
			resource.URI, resource.ObservedAt.Format(time.DateOnly), resource.RevalidateBy.Format(time.DateOnly))
	}
}
