package remotesessions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// lockRegistrationIssuer serializes rotation with EMA preparation for an issuer.
// Call only after an authorized lookup, before any row/advisory transaction locks.
// The reserved connection holds no transaction during HTTP. The caller must
// re-read mutable rows after acquiring it and defer release before releasing conn.
func lockRegistrationIssuer(ctx context.Context, conn *pgxpool.Conn, issuerID uuid.UUID) (func(), error) {
	q := repo.New(conn)
	key := "ema-registration-issuer:" + issuerID.String()
	if err := q.LockPreparationSubmission(ctx, key); err != nil {
		// A canceled request might still have acquired a session lock server-side.
		_ = conn.Conn().Close(context.Background())
		return nil, fmt.Errorf("lock issuer registration: %w", err)
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := q.UnlockPreparationSubmission(releaseCtx, key); err != nil {
			_ = conn.Conn().Close(context.Background())
		}
	}, nil
}
