package mcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func chosenMember(slug string) metaMember {
	return metaMember{serverID: uuid.New(), slug: slug, chosenClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
}

// A member whose token endpoint is slow holds up only its own callers: other
// members resolve meanwhile, and a waiting caller gives up at its own
// deadline.
func TestMemberChoicesTokens_SlowMemberBlocksOnlyItself(t *testing.T) {
	t.Parallel()

	slow, fast := chosenMember("slow"), chosenMember("fast")
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	started := make(chan struct{})
	var calls atomic.Int32
	c := &memberChoices{
		selector: nil,
		resolve: func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			calls.Add(1)
			if member.serverID == slow.serverID {
				close(started)
				<-gate
			}
			return map[uuid.UUID]remotesessions.UpstreamToken{member.chosenClientID.UUID: {Token: member.slug}}, nil
		},
		mu:       sync.Mutex{},
		byServer: map[uuid.UUID]*memberTokens{},
	}

	type result struct {
		tokens map[uuid.UUID]remotesessions.UpstreamToken
		err    error
	}
	slowDone := make(chan result, 1)
	go func() {
		tokens, err := c.tokens(t.Context(), slow)
		slowDone <- result{tokens: tokens, err: err}
	}()
	<-started

	tokens, err := c.tokens(t.Context(), fast)
	require.NoError(t, err)
	require.Equal(t, "fast", tokens[fast.chosenClientID.UUID].Token, "another member resolves while the slow one is in flight")

	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = c.tokens(waitCtx, slow)
	var memberErr *metaMemberError
	require.ErrorAs(t, err, &memberErr, "a waiter gives up at its own deadline")

	release()
	first := <-slowDone
	require.NoError(t, first.err)
	require.Equal(t, "slow", first.tokens[slow.chosenClientID.UUID].Token)
	tokens, err = c.tokens(t.Context(), slow)
	require.NoError(t, err)
	require.Equal(t, "slow", tokens[slow.chosenClientID.UUID].Token)
	require.Equal(t, int32(2), calls.Load(), "each member resolves once per request")
}

// A failed resolution is not kept, so the next caller for the member tries
// again.
func TestMemberChoicesTokens_RetriesAfterAFailure(t *testing.T) {
	t.Parallel()

	member := chosenMember("flaky")
	var calls atomic.Int32
	c := &memberChoices{
		selector: nil,
		resolve: func(context.Context, metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("token endpoint unavailable")
			}
			return map[uuid.UUID]remotesessions.UpstreamToken{}, nil
		},
		mu:       sync.Mutex{},
		byServer: map[uuid.UUID]*memberTokens{},
	}

	_, err := c.tokens(t.Context(), member)
	require.Error(t, err)
	_, err = c.tokens(t.Context(), member)
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())
}

// A session credential whose auth context names another organization is
// refused rather than served outside its recorded choices.
func TestUpstreamSelectorFor_RefusesAMismatchedAuthContext(t *testing.T) {
	t.Parallel()

	s := &Service{
		aiCatalog: aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())),
	}
	endpoint := &ResolvedMcpEndpoint{OrganizationID: chainingTestOrg}
	_, err := s.upstreamSelectorFor(humanChainingContext(t, "user-1", "another-org"), endpoint)
	require.ErrorIs(t, err, errSessionAuthContextMismatch)
}
