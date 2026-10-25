package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestExpiredListingResultKeepsAccessDenialsAndClassifiesTimeouts(t *testing.T) {
	t.Parallel()

	member := metaMember{slug: "linear"}
	catalog := &memberCatalog{}
	notFound := oops.E(oops.CodeNotFound, nil, "server not found")
	forbidden := oops.E(oops.CodeForbidden, nil, "server forbidden")
	memberFailure := &metaMemberError{message: "server \"linear\" is not connected", reason: reasonNeedsSignIn}

	for name, tc := range map[string]struct {
		result memberDescribeResult
		kept   bool
		reason attemptReason
	}{
		"catalog":               {result: memberDescribeResult{catalog: catalog, err: nil, calls: 1}, kept: true},
		"not found":             {result: memberDescribeResult{catalog: nil, err: notFound, calls: 1}, kept: true},
		"forbidden":             {result: memberDescribeResult{catalog: nil, err: forbidden, calls: 1}, kept: true},
		"wrapped not found":     {result: memberDescribeResult{catalog: nil, err: fmt.Errorf("describe member: %w", notFound), calls: 1}, kept: true},
		"wrapped forbidden":     {result: memberDescribeResult{catalog: nil, err: fmt.Errorf("describe member: %w", forbidden), calls: 1}, kept: true},
		"member failure":        {result: memberDescribeResult{catalog: nil, err: memberFailure, calls: 1}, kept: true},
		"never started":         {result: memberDescribeResult{catalog: nil, err: nil, calls: 0}, kept: false, reason: reasonListingBudgetExhausted},
		"deadline exceeded":     {result: memberDescribeResult{catalog: nil, err: fmt.Errorf("list tools: %w", context.DeadlineExceeded), calls: 1}, kept: false, reason: reasonListingTimeout},
		"other shareable error": {result: memberDescribeResult{catalog: nil, err: oops.E(oops.CodeUnexpected, nil, "unexpected"), calls: 2}, kept: false, reason: reasonListingTimeout},
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
			require.Equal(t, tc.reason, timeout.reason)
			require.Equal(t, tc.result.calls, got.calls)
		})
	}
}

// A credential outage is retried within the listing unless the attempt's own
// time ran out; a misconfigured credential stays a member failure.
func TestMetaMemberClientCredentialErrorRetriesOutages(t *testing.T) {
	t.Parallel()

	member := metaMember{slug: "linear"}
	outage, ok := errors.AsType[memberDiscoveryError](metaMemberClientCredentialError(member, remotesessions.ErrClientCredentialUnavailable))
	require.True(t, ok)
	require.True(t, outage.retryable)

	timedOut, ok := errors.AsType[memberDiscoveryError](metaMemberClientCredentialError(member, fmt.Errorf("%w: %w", remotesessions.ErrRemoteSessionUnavailable, context.DeadlineExceeded)))
	require.True(t, ok)
	require.False(t, timedOut.retryable)

	misconfigured := metaMemberClientCredentialError(member, remotesessions.ErrClientCredentialMisconfigured)
	_, discovery := errors.AsType[memberDiscoveryError](misconfigured)
	require.False(t, discovery)
	_, memberFailure := errors.AsType[*metaMemberError](misconfigured)
	require.True(t, memberFailure)
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
