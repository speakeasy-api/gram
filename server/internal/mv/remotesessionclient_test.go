package mv

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestRemoteSessionClientIssuedAt(t *testing.T) {
	t.Parallel()
	for _, valid := range []bool{false, true} {
		row := repo.RemoteSessionClient{ClientIDIssuedAt: pgtype.Timestamptz{Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Valid: valid}}
		project, err := BuildRemoteSessionClientView(row, nil)
		require.NoError(t, err)
		global := BuildGlobalRemoteSessionClientView(row)
		if valid {
			require.Equal(t, "2026-01-02T03:04:05Z", *project.ClientIDIssuedAt)
			require.Equal(t, project.ClientIDIssuedAt, global.ClientIDIssuedAt)
		} else {
			require.Nil(t, project.ClientIDIssuedAt, "unknown issuance must not serialize an invalid empty datetime")
			require.Nil(t, global.ClientIDIssuedAt)
		}
	}
}
