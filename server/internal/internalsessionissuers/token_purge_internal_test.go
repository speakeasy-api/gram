package internalsessionissuers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type issuerTokenPurgerFunc func(context.Context, string) error

func (f issuerTokenPurgerFunc) PurgeEMAIssuerTokens(ctx context.Context, organizationID string) error {
	return f(ctx, organizationID)
}

func TestIssuerTokenPurgeWithoutProvisioner(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure_is_logged"}[fail], func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			called := false
			s := &Service{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			s.SetEMAIssuerTokenPurger(issuerTokenPurgerFunc(func(ctx context.Context, organizationID string) error {
				called = true
				require.Equal(t, "org_test", organizationID)
				require.NoError(t, ctx.Err(), "a disconnected caller must not cancel credential cleanup")
				deadline, bounded := ctx.Deadline()
				require.True(t, bounded)
				require.WithinDuration(t, time.Now().Add(tokenPurgeTimeout), deadline, time.Second)
				if fail {
					return errors.New("database unavailable")
				}
				return nil
			}))
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			s.purgeIssuerTokens(ctx, "org_test")
			require.True(t, called)
			if fail {
				require.Contains(t, logs.String(), "cleanup failed after containment")
			} else {
				require.Empty(t, logs.String())
			}
		})
	}
}
