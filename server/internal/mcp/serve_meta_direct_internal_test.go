package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"

	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestExpiredListingResultKeepsAccessDenialsAndClassifiesTimeouts(t *testing.T) {
	t.Parallel()

	member := metaMember{slug: "linear"}
	catalog := &memberCatalog{}
	notFound := oops.E(oops.CodeNotFound, nil, "server not found")
	forbidden := oops.E(oops.CodeForbidden, nil, "server forbidden")
	memberFailure := &metaMemberError{message: "server \"linear\" is not connected"}

	for name, tc := range map[string]struct {
		result memberDescribeResult
		kept   bool
	}{
		"catalog":               {result: memberDescribeResult{catalog: catalog, err: nil}, kept: true},
		"not found":             {result: memberDescribeResult{catalog: nil, err: notFound}, kept: true},
		"forbidden":             {result: memberDescribeResult{catalog: nil, err: forbidden}, kept: true},
		"wrapped not found":     {result: memberDescribeResult{catalog: nil, err: fmt.Errorf("describe member: %w", notFound)}, kept: true},
		"wrapped forbidden":     {result: memberDescribeResult{catalog: nil, err: fmt.Errorf("describe member: %w", forbidden)}, kept: true},
		"member failure":        {result: memberDescribeResult{catalog: nil, err: memberFailure}, kept: true},
		"never started":         {result: memberDescribeResult{catalog: nil, err: nil}, kept: false},
		"deadline exceeded":     {result: memberDescribeResult{catalog: nil, err: fmt.Errorf("list tools: %w", context.DeadlineExceeded)}, kept: false},
		"other shareable error": {result: memberDescribeResult{catalog: nil, err: oops.E(oops.CodeUnexpected, nil, "unexpected")}, kept: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := expiredListingResult(member, tc.result)
			if tc.kept {
				require.Equal(t, tc.result, got)
				return
			}
			require.Nil(t, got.catalog)
			timeout, ok := errors.AsType[memberDiscoveryError](got.err)
			require.True(t, ok, "%v", got.err)
			require.Equal(t, "server \"linear\" did not answer within the gateway listing time", timeout.message)
			require.False(t, timeout.retryable)
			require.False(t, memberAccessDenied(got.err))
		})
	}
}

// A retry that waited for a slot until too little listing time remained keeps
// the upstream's own failure instead of starting an attempt that could only
// time out.
func TestDescribeMetaMemberWithRetryKeepsTheFailureWhenASlotFreesTooLate(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rateLimited := memberDiscoveryError{metaMemberError: &metaMemberError{message: "rate limited"}, status: http.StatusTooManyRequests, retryable: true, retryAfter: 100 * time.Millisecond}
		member := metaMember{slug: "linear", serverID: uuid.New(), projectID: uuid.New(), backend: metaMemberBackendProxied, upstreamUnavailable: rateLimited}
		catalog := &connectionCatalog{Members: map[string]connectionCatalogMember{member.slug: {ServerID: member.serverID, ProjectID: member.projectID}}}
		gate := &metaGateContext{catalog: catalog, execute: true}
		service := &Service{metaRuntime: MetaRuntimeConfig{MemberCallTimeout: time.Second}}
		const listingTime = time.Second
		ctx, cancel := context.WithTimeout(t.Context(), listingTime)
		defer cancel()
		slots := semaphore.NewWeighted(1)

		described := make(chan memberDescribeResult, 1)
		go func() {
			described <- service.describeMetaMemberWithRetry(ctx, testenv.NewLogger(t), gate, member, slots)
		}()
		synctest.Wait()

		// The first attempt failed and its retry is waiting out the backoff.
		// Hold the only slot until less than the retry budget remains, and
		// make any second describe fail differently.
		require.True(t, slots.TryAcquire(1))
		delete(catalog.Members, member.slug)
		time.Sleep(listingTime - minMemberRetryBudget/2) //nolint:forbidigo // synctest advances the fake clock without a wall-clock wait.
		slots.Release(1)

		require.ErrorIs(t, (<-described).err, rateLimited)
	})
}

// The size bound applies to the tool definitions, not the response envelope.
func TestEncodeDirectGatewayToolListBoundsTheCatalog(t *testing.T) {
	t.Parallel()

	catalogOf := func(size int) []*toolListEntry {
		empty, err := json.Marshal([]*toolListEntry{{Name: "a"}})
		require.NoError(t, err)
		return []*toolListEntry{{Name: "a", Description: strings.Repeat("x", size-len(empty))}}
	}
	id := mcpjsonrpc.NumberID(1)

	encoded, err := encodeDirectGatewayToolList(id, catalogOf(maxDirectGatewayCatalogBytes))
	require.NoError(t, err)
	require.Greater(t, len(encoded), maxDirectGatewayCatalogBytes)
	var listing struct {
		Result struct {
			Tools []toolListEntry `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(encoded, &listing))
	require.Len(t, listing.Result.Tools, 1)

	_, err = encodeDirectGatewayToolList(id, catalogOf(maxDirectGatewayCatalogBytes+1))
	tooLarge, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok)
	require.Equal(t, oops.CodeRequestTooLarge, tooLarge.Code)
}
