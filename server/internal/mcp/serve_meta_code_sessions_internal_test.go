package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestCodeMemberSessionWaitFailuresStayMemberScoped(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"connect", "ping", "recheck"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			logger := testenv.NewLogger(t)
			pool := newCodeMemberSessions(logger, 0, 0, 0, 1)
			refusal := &metaMemberError{message: `server "remote" is no longer approved`}
			scope := &codeSessionScope{pool: pool, callerScope: &codeCallerScope{key: "caller"}, recheck: func(context.Context, string) (context.Context, *metaGateContext, metaMember, error) {
				return nil, nil, metaMember{}, fmt.Errorf("refresh execution authority: %w", refusal)
			}}
			member := metaMember{slug: "remote"}
			dial := memberDial{sessionKey: "route"}
			key, err := newCodeMemberSessionKey(scope, codeMemberDestination(member)+"\x00"+dial.sessionKey)
			require.NoError(t, err)
			entry := &codeMemberEntry{key: key, slot: make(chan struct{}, 1)}
			if stage == "connect" {
				entry.slot <- struct{}{}
			} else {
				verifying := make(chan struct{})
				if stage == "recheck" {
					close(verifying)
				}
				entry.current = &pooledMemberSession{created: time.Now(), leases: 1, verifying: verifying}
			}
			pool.entries[key] = entry
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			ctx = context.WithValue(ctx, codeMemberSessionsKey{}, scope)
			_, session, err := (&Service{}).openMemberSession(ctx, logger, dial, nil, member)
			require.Nil(t, session)
			memberErr, ok := errors.AsType[*metaMemberError](err)
			require.True(t, ok, "listing must skip this member rather than fail the inventory: %v", err)
			if stage == "recheck" {
				require.ErrorIs(t, err, refusal)
				require.Equal(t, refusal.message, memberErr.message)
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, `server "remote" did not answer: upstream unreachable`, memberErr.message)
			}
			require.Equal(t, memberErr.message, err.Error(), "tools/call exposes only the member's public refusal")
			body, marshalErr := marshalMetaToolError(t.Context(), logger, mcpjsonrpc.NumberID(1), err.Error())
			require.NoError(t, marshalErr)
			require.Contains(t, string(body), `"isError":true`)
			require.NotContains(t, string(body), "refresh execution authority")
			require.NotContains(t, string(body), "context deadline")
		})
	}
}

func TestCodeMemberSessionPreservesAdmissionRefusal(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{codemode.ErrExecutionRevoked, codemode.ErrToolUnavailable} {
		err := pooledMemberFailure(metaMember{slug: "remote"}, fmt.Errorf("recheck admission after waiting: %w", cause))
		require.ErrorIs(t, err, cause)
		_, ok := errors.AsType[*metaMemberError](err)
		require.True(t, ok)
		require.Equal(t, fmt.Sprintf(`server "remote": %s`, cause), err.Error())
	}
}

func TestCodeMemberSessionRequiresCallerScope(t *testing.T) {
	t.Parallel()
	for _, caller := range []*codeCallerScope{nil, {}} {
		scope := &codeSessionScope{callerScope: caller}
		ctx := context.WithValue(t.Context(), codeMemberSessionsKey{}, scope)
		_, session, err := (&Service{}).openMemberSession(ctx, testenv.NewLogger(t), memberDial{}, nil, metaMember{slug: "remote"})
		require.Nil(t, session)
		memberErr, ok := errors.AsType[*metaMemberError](err)
		require.True(t, ok)
		require.Equal(t, `server "remote" requires an authorized caller scope`, memberErr.message)
		require.Equal(t, memberErr.message, err.Error())
		pooledErr, ok := errors.AsType[*pooledMemberError](err)
		require.True(t, ok)
		require.ErrorContains(t, pooledErr.cause, "requires caller scope and destination")
	}
}

func newInMemoryMemberSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err := mcp.NewServer(&mcp.Implementation{Name: "member", Version: "1"}, nil).Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "gateway", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	return session
}

func TestCodeMemberSessionPoolsReplacementOfExpiredSession(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool := newCodeMemberSessions(testenv.NewLogger(t), 0, time.Minute, 0, 4)
	t.Cleanup(func() { pool.close(context.WithoutCancel(ctx), true) })
	key := codeMemberSessionKey{callerScope: codeCallerScope{key: "caller"}, destination: "member"}
	connects := 0
	connect := func(context.Context) (*mcp.ClientSession, *memberRoundTripper, error) {
		connects++
		return newInMemoryMemberSession(t), nil, nil
	}
	recheck := func(ctx context.Context) (context.Context, error) { return ctx, nil }

	first, err := pool.acquire(ctx, key, recheck, connect)
	require.NoError(t, err)
	require.NotNil(t, first.lease)
	pool.release(ctx, first.entry, first.lease)
	first.entry.mu.Lock()
	first.lease.created = time.Now().Add(-2 * time.Minute)
	first.entry.mu.Unlock()

	replacement, err := pool.acquire(ctx, key, recheck, connect)
	require.NoError(t, err)
	require.NotNil(t, replacement.lease, "the session replacing an expired one must stay pooled")
	pool.release(ctx, replacement.entry, replacement.lease)

	reused, err := pool.acquire(ctx, key, recheck, connect)
	require.NoError(t, err)
	require.Same(t, replacement.lease, reused.lease)
	pool.release(ctx, reused.entry, reused.lease)
	require.Equal(t, 2, connects)
}

func TestCodeMemberSessionRekeysWhenRouteChangesDuringWait(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	logger := testenv.NewLogger(t)
	pool := newCodeMemberSessions(logger, 0, 0, 0, 4)
	t.Cleanup(func() { pool.close(context.WithoutCancel(ctx), true) })
	member := metaMember{slug: "remote"}
	stale := memberDial{sessionKey: "revoked-credential"}
	rotated := memberDial{sessionKey: "rotated-credential"}
	scope := &codeSessionScope{pool: pool, callerScope: &codeCallerScope{key: "caller"}, recheck: func(ctx context.Context, _ string) (context.Context, *metaGateContext, metaMember, error) {
		return ctx, &metaGateContext{}, member, nil
	}}
	keyFor := func(dial memberDial) codeMemberSessionKey {
		key, err := newCodeMemberSessionKey(scope, codeMemberDestination(member)+"\x00"+dial.sessionKey)
		require.NoError(t, err)
		return key
	}
	// Another operation's ping of the stale route's session settled while
	// this one waited on it.
	settled := make(chan struct{})
	close(settled)
	staleEntry := &codeMemberEntry{key: keyFor(stale), slot: make(chan struct{}, 1), current: &pooledMemberSession{session: newInMemoryMemberSession(t), created: time.Now(), lastUsed: time.Now(), verifying: settled}}
	pool.entries[staleEntry.key] = staleEntry
	rotatedSession := newInMemoryMemberSession(t)
	rotatedEntry := &codeMemberEntry{key: keyFor(rotated), slot: make(chan struct{}, 1), current: &pooledMemberSession{session: rotatedSession, created: time.Now(), lastUsed: time.Now()}}
	pool.entries[rotatedEntry.key] = rotatedEntry
	redialed := 0
	redial := func(context.Context, *metaGateContext, metaMember) (memberDial, error) {
		redialed++
		return rotated, nil
	}
	ctx = context.WithValue(ctx, codeMemberSessionsKey{}, scope)
	_, session, err := (&Service{}).openMemberSession(ctx, logger, stale, redial, member)
	require.NoError(t, err)
	defer session.release(ctx, logger)
	require.Equal(t, 1, redialed)
	require.Same(t, rotatedSession, session.session, "a refreshed route must not lease the stale route's session")
	require.Equal(t, rotated.sessionKey, session.dial.sessionKey)
}
