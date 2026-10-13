package internalsessionissuers_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// publicationLockTestTimeout bounds a broken lock or release regression.
const publicationLockTestTimeout = 30 * time.Second

func TestPublicationKeySetLockSerializesLifecycleKeyWrites(t *testing.T) {
	t.Parallel()
	for _, publicationHolds := range []bool{true, false} {
		name := "publication_blocks_containment"
		if !publicationHolds {
			name = "containment_blocks_publication"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t, false)
			ctx, cancel := context.WithTimeout(ctx, publicationLockTestTimeout)
			defer cancel()
			provisionIssuer(t, ctx, ti)
			issuer, err := jwksrepo.New(ti.conn).GetOrganizationInternalSessionIssuer(ctx, ti.orgID)
			require.NoError(t, err)
			publicationParams := repo.LockSpeakeasyEMAKeySetParams{ID: issuer.ID, OrganizationID: ti.orgID}
			_, err = repo.New(ti.conn).LockSpeakeasyEMAKeySet(ctx, repo.LockSpeakeasyEMAKeySetParams{ID: issuer.ID, OrganizationID: "org_other"})
			require.ErrorIs(t, err, pgx.ErrNoRows, "a publication cannot lock another tenant's key set")
			holder := testenv.BeginTx(t, ctx, ti.conn)
			waiter := testenv.BeginTx(t, ctx, ti.conn)
			queryPattern := "%LockSpeakeasyEMAKeySet%"
			if publicationHolds {
				_, err = repo.New(holder).LockSpeakeasyEMAKeySet(ctx, publicationParams)
				queryPattern = "%LockOrganizationEMAIssuerForKeyWrite%"
			} else {
				_, err = internalsessionissuers.LockIssuerKeys(ctx, jwksrepo.New(holder), ti.orgID)
			}
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				if publicationHolds {
					_, err := internalsessionissuers.LockIssuerKeys(ctx, jwksrepo.New(waiter), ti.orgID)
					done <- err
				} else {
					_, err := repo.New(waiter).LockSpeakeasyEMAKeySet(ctx, publicationParams)
					done <- err
				}
			}()
			testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(holder), queryPattern)
			require.NoError(t, holder.Commit(ctx))
			select {
			case err := <-done:
				require.NoError(t, err, "releasing the key-set lock lets the waiting operation finish")
			case <-ctx.Done():
				t.Fatal("key-set lock did not release")
			}
		})
	}
}
