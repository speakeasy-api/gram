package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
			scope := &codeSessionScope{pool: pool, callerScope: &codeCallerScope{key: "caller"}, recheck: func(context.Context, string) (context.Context, error) {
				return nil, fmt.Errorf("refresh execution authority: %w", refusal)
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
			_, session, err := (&Service{}).openMemberSession(ctx, logger, dial, member)
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
		_, session, err := (&Service{}).openMemberSession(ctx, testenv.NewLogger(t), memberDial{}, metaMember{slug: "remote"})
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
