//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestSeedRefusesToDestroyLegacyChannelsContent pins a deletion the seed used
// to do unconditionally.
//
// The superseded channels_state document can still hold a developer's
// proof-of-concept conversations. Reseeding is not consent to delete them, and
// the seed cannot convert them itself — that is an operator decision with an
// identifier mapping attached. So it refuses and names the command, rather
// than quietly dropping the row.
func TestSeedRefusesToDestroyLegacyChannelsContent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := testenv.NewLogger(t)

	db, err := infra.CloneTestDatabase(t, "channelslegacyseeddb")
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)
	spec := LocalSpec()

	// A first seed succeeds and leaves no legacy document behind.
	_, err = seedPostgres(ctx, logger, db, blob, spec)
	require.NoError(t, err)
	var documents int64
	require.NoError(t, db.QueryRow(ctx, "SELECT count(*) FROM channels_state").Scan(&documents)) //nolint:glint // notestingrawsql: drives the superseded channels_state document, which has no production query by design — nothing writes it any more.
	require.Zero(t, documents)

	// A document with content makes the next reseed refuse.
	_, err = db.Exec(ctx, //nolint:glint // notestingrawsql: drives the superseded channels_state document, which has no production query by design — nothing writes it any more.
		`INSERT INTO channels_state (organization_id, state) VALUES ($1, $2::jsonb)`,
		spec.OrgID,
		`{"enabled":true,"instances":{"old":{"id":"old","subjectId":"someone","label":"Old agent","expiresAt":"2099-01-01T00:00:00Z"}},`+
			`"threads":{"t":{"id":"t","title":"Kept"}},`+
			`"messages":[{"id":1,"threadId":"t","instanceId":"old","subjectId":"someone","text":"keep me","idempotencyKey":"k","createdAt":"2026-01-01T00:00:00Z"}],`+
			`"preferences":{},"grants":{},"cursor":1}`)
	require.NoError(t, err)

	_, err = seedPostgres(ctx, logger, db, blob, spec)
	require.Error(t, err, "the seed deleted a legacy document that still held content")
	require.ErrorContains(t, err, "channels-convert")

	// The refusal must not read as "convert, then reseed". Conversion leaves
	// the document in place, writes into the tables the purge clears, and
	// refuses an organization a seed has already populated, so that sequence
	// loses the data it appears to save.
	require.NotContains(t, err.Error(), "then reseed")
	require.ErrorContains(t, err, "Copy anything worth keeping out of this organization")

	// The document survived the refusal.
	var messages int
	require.NoError(t, db.QueryRow(ctx, //nolint:glint // notestingrawsql: drives the superseded channels_state document, which has no production query by design — nothing writes it any more.
		`SELECT jsonb_array_length(state -> 'messages') FROM channels_state WHERE organization_id = $1`, spec.OrgID).Scan(&messages))
	require.Equal(t, 1, messages)

	// Emptying it (as a conversion would, once its contents are carried
	// forward) lets the seed proceed again.
	_, err = db.Exec(ctx, //nolint:glint // notestingrawsql: drives the superseded channels_state document, which has no production query by design — nothing writes it any more.
		`UPDATE channels_state SET state = '{"enabled":true,"instances":{},"threads":{},"messages":[],"preferences":{},"grants":{},"cursor":0}'::jsonb WHERE organization_id = $1`,
		spec.OrgID)
	require.NoError(t, err)
	_, err = seedPostgres(ctx, logger, db, blob, spec)
	require.NoError(t, err)
}
