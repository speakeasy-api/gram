package ssosignins

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type recordingRevoker struct {
	mu      sync.Mutex
	calls   []string
	failFor string
	ctxErrs []error
}

func (r *recordingRevoker) revoke(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id)
	r.ctxErrs = append(r.ctxErrs, ctx.Err())
	if id == r.failFor {
		return errors.New("unavailable")
	}
	return nil
}

type tokenRevoker struct{ *recordingRevoker }

func (t tokenRevoker) RevokeToken(ctx context.Context, jti string) error { return t.revoke(ctx, jti) }

type idpRevoker struct{ *recordingRevoker }

func (i idpRevoker) RevokeSession(ctx context.Context, id string) error { return i.revoke(ctx, id) }

func TestFinishRevocation_PushesEverythingDetachedFromTheRequest(t *testing.T) {
	t.Parallel()

	tokens := &recordingRevoker{mu: sync.Mutex{}, calls: nil, failFor: "jti-2", ctxErrs: nil}
	idp := &recordingRevoker{mu: sync.Mutex{}, calls: nil, failFor: "", ctxErrs: nil}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	FinishRevocation(ctx, testenv.NewLogger(t), tokenRevoker{tokens}, idpRevoker{idp}, []string{"jti-1", "jti-2", "jti-3"}, []string{"session-1", ""})

	require.Equal(t, []string{"jti-1", "jti-2", "jti-3"}, tokens.calls, "one failed push never stops the rest")
	require.Equal(t, []string{"session-1"}, idp.calls)
	for _, err := range append(tokens.ctxErrs, idp.ctxErrs...) {
		require.NoError(t, err, "a cancelled request never cancels the pushes")
	}
}

func TestRecordExchange_SkipsWithoutTouchingTheDatabase(t *testing.T) {
	t.Parallel()

	for _, p := range []ExchangeParams{
		{Flow: FlowMCPAuthorize, WorkOSOrganizationID: "org_1", ExpectedOrganizationID: "o1", UserID: "u1", WorkOSSessionID: "s1", AuthenticationMethod: MethodSSO, Impersonated: true},
		{Flow: FlowDashboard, WorkOSOrganizationID: "org_1", ExpectedOrganizationID: "", UserID: "u1", WorkOSSessionID: "", AuthenticationMethod: MethodSSO, Impersonated: false},
		{Flow: FlowDashboard, WorkOSOrganizationID: "", ExpectedOrganizationID: "", UserID: "u1", WorkOSSessionID: "s1", AuthenticationMethod: MethodSSO, Impersonated: false},
	} {
		got := RecordExchange(t.Context(), testenv.NewLogger(t), nil, p)
		require.False(t, got.ID.Valid)
	}
}
