package mcp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConsentValidationInitializeTimeoutClosesSession(t *testing.T) {
	t.Parallel()
	for _, mode := range []validationMemberMode{memberHangsInitializeJSON, memberHangsInitializeSSE} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			ctx, fx := seedStandaloneValidationFixture(t, "validate-"+string(mode))
			fx.member.set(mode)
			requireValidated(t, fx)
			requireProbe(t, fx.member.drain(), "token-validate-"+string(mode), false, true)
			session := storedSession(t, ctx, fx)
			require.Equal(t, "unknown", session.ValidationStatus.String)
			require.Equal(t, fx.name+" did not answer in time", session.ValidationReason.String)
		})
	}
}

// A leg that hangs past the probe deadline, the session close included, still leaves the verdict written inside the budget.
func TestConsentValidationHangingLegStillRecordsVerdict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode validationMemberMode
		want string
	}{
		{mode: memberHangsAck, want: "unknown"},
		{mode: memberHangsClose, want: "valid"}, // A failed best-effort DELETE does not undo a successful dry run.
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Parallel()
			ctx, fx := seedStandaloneValidationFixture(t, "validate-budget-"+string(tc.mode))
			fx.member.set(tc.mode)
			started := time.Now()
			requireValidated(t, fx)
			require.Less(t, time.Since(started), validationProbeTimeout+2*time.Second, "the probe, its close included, ends near its budget")
			requireProbe(t, fx.member.drain(), "token-validate-budget-"+string(tc.mode), true, true)
			require.Equal(t, tc.want, storedSession(t, ctx, fx).ValidationStatus.String)
		})
	}
}
