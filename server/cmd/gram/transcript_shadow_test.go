package gram

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func newTranscriptShadowCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()
	c := newOrgEncryptionCLIContext(t, nil)
	set := flag.NewFlagSet("transcript-shadow", flag.ContinueOnError)
	// Apply the flags without their environment variables, so a developer's
	// shell cannot change what these tests see. Parallel tests cannot use
	// t.Setenv, so the isolation lives here.
	for _, f := range []cli.Flag{transcriptShadowFlag()} {
		sliceFlag, ok := f.(*cli.StringSliceFlag)
		require.True(t, ok)
		withoutEnv := *sliceFlag
		withoutEnv.EnvVars = nil
		require.NoError(t, withoutEnv.Apply(set))
	}
	for key, value := range values {
		require.NoError(t, set.Set(key, value))
	}
	return cli.NewContext(c.App, set, c)
}

func TestTranscriptShadowDisabled(t *testing.T) {
	t.Setenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS", "")
	require.NoError(t, os.Unsetenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS"))
	c := newTranscriptShadowCLIContext(t, nil)
	require.Empty(t, c.StringSlice(chatTranscriptShadowProjectIDsFlag))
	shadow, err := configureTranscriptShadow(t.Context(), c, nil, nil, nil, orgEncryptionDeps{}, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	require.Nil(t, shadow.Store)
	require.Nil(t, shadow.Assets)
	require.Empty(t, shadow.ProjectIDs)
}

func TestTranscriptShadowProjectIDs(t *testing.T) {
	t.Parallel()
	first := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	second := uuid.MustParse("10000000-0000-4000-8000-000000000002")
	ids, err := parseTranscriptShadowProjectIDs([]string{first.String(), " " + second.String() + " ", first.String()})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first, second}, ids)
	ids, err = parseTranscriptShadowProjectIDs([]string{first.String()})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first}, ids)
	for _, value := range []string{"", " ", "not-a-uuid", uuid.Nil.String()} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			_, err := parseTranscriptShadowProjectIDs([]string{first.String(), value})
			require.Error(t, err)
			require.NotContains(t, err.Error(), first.String())
		})
	}
}

func TestTranscriptShadowRejectsInvalidConfiguration(t *testing.T) {
	for _, value := range []string{"", " ", "not-a-uuid", uuid.Nil.String(), "10000000-0000-4000-8000-000000000001,"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS", "")
			require.NoError(t, os.Unsetenv("GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS"))
			c := newTranscriptShadowCLIContext(t, map[string]string{chatTranscriptShadowProjectIDsFlag: value})
			_, err := configureTranscriptShadow(t.Context(), c, nil, nil, nil, orgEncryptionDeps{}, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
			require.Error(t, err)
		})
	}
}

func TestTranscriptShadowFlags(t *testing.T) {
	t.Parallel()
	for _, item := range append(orgEncryptionFlags(), orgEncryptionServiceFlags()...) {
		require.NotContains(t, item.Names(), chatTranscriptShadowProjectIDsFlag)
	}
	for _, command := range []*cli.Command{newAdminCommand(), newNetworkIngressWorkerCommand()} {
		for _, item := range command.Flags {
			require.NotContains(t, item.Names(), chatTranscriptShadowProjectIDsFlag, command.Name)
		}
	}
	for _, command := range []*cli.Command{newStartCommand(), newWorkerCommand(), newMCPCommand(), newNetworkIngressServerCommand()} {
		requireFlag(t, command.Flags, chatTranscriptShadowProjectIDsFlag)
	}
}

// All DDL is confined to a disposable database. Preflight runs in read-only
// transactions so even a compatible check cannot modify data or provision keys.
func TestTranscriptShadowSchemaCompatibility(t *testing.T) {
	t.Parallel()
	infra, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	const columns = `message_id uuid NOT NULL, project_id uuid NOT NULL,
 organization_id text NOT NULL, ciphertext text NOT NULL, source_hash bytea NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()`
	type schemaCase struct {
		name, ddl string
		ok        bool
	}
	tests := []schemaCase{
		{name: "missing relation"},
		{name: "missing conflict target", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ")"},
		{name: "nonunique target", ddl: "CREATE TABLE chat_message_transcripts (" + columns + "); CREATE INDEX ON chat_message_transcripts (message_id)"},
		{name: "composite target", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", UNIQUE (message_id, project_id))"},
		{name: "partial target", ddl: "CREATE TABLE chat_message_transcripts (" + columns + "); CREATE UNIQUE INDEX ON chat_message_transcripts (message_id) WHERE ciphertext <> ''"},
		{name: "expression target", ddl: "CREATE TABLE chat_message_transcripts (" + columns + "); CREATE UNIQUE INDEX ON chat_message_transcripts ((message_id::text))"},
		{name: "deferrable target", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", UNIQUE (message_id) DEFERRABLE)"},
		{name: "deferrable and immediate targets", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", UNIQUE (message_id) DEFERRABLE); CREATE UNIQUE INDEX ON chat_message_transcripts (message_id)"},
		{name: "updated_at without default", ddl: "CREATE TABLE chat_message_transcripts (" + strings.ReplaceAll(columns, " DEFAULT clock_timestamp()", "") + ", PRIMARY KEY (message_id))"},
		{name: "created_at without default", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", PRIMARY KEY (message_id), created_at timestamptz NOT NULL)"},
		{name: "extra required column without default", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", PRIMARY KEY (message_id), extra text NOT NULL)"},
		{name: "defaulted timestamps", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", PRIMARY KEY (message_id), created_at timestamptz NOT NULL DEFAULT clock_timestamp())", ok: true},
		{name: "nullable extra column", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", PRIMARY KEY (message_id), extra text)", ok: true},
		{name: "defaulted extra column", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", PRIMARY KEY (message_id), extra text NOT NULL DEFAULT '')", ok: true},
		{name: "generated extra column", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", PRIMARY KEY (message_id), extra text NOT NULL GENERATED ALWAYS AS (ciphertext) STORED)", ok: true},
		{name: "identity extra column", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", PRIMARY KEY (message_id), extra bigint NOT NULL GENERATED ALWAYS AS IDENTITY)", ok: true},
		{name: "primary key", ddl: "CREATE TABLE chat_message_transcripts (" + columns + ", PRIMARY KEY (message_id))", ok: true},
		{name: "unique index with included column", ddl: "CREATE TABLE chat_message_transcripts (" + columns + "); CREATE UNIQUE INDEX ON chat_message_transcripts (message_id) INCLUDE (project_id)", ok: true},
	}
	for _, column := range []struct{ name, typ string }{
		{"message_id", "uuid"}, {"project_id", "uuid"}, {"organization_id", "text"},
		{"ciphertext", "text"}, {"source_hash", "bytea"}, {"updated_at", "timestamptz"},
	} {
		base := "CREATE TABLE chat_message_transcripts (" + columns + ", UNIQUE (message_id)); "
		tests = append(tests,
			schemaCase{name: "missing " + column.name, ddl: base + "ALTER TABLE chat_message_transcripts DROP COLUMN " + column.name + " CASCADE"},
			schemaCase{name: "nullable " + column.name, ddl: base + "ALTER TABLE chat_message_transcripts ALTER COLUMN " + column.name + " DROP NOT NULL"},
			schemaCase{name: "wrong type " + column.name, ddl: "CREATE TABLE chat_message_transcripts (" + strings.Replace(strings.ReplaceAll(columns, " DEFAULT clock_timestamp()", ""), column.name+" "+column.typ, column.name+" integer", 1) + ", UNIQUE (message_id))"},
		)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, err := infra.CloneTestDatabase(t, "transcript_shadow_preflight")
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), "DROP TABLE IF EXISTS chat_message_transcripts") //nolint:glint // notestingrawsql: isolated DDL fixture for catalog compatibility checks.
			require.NoError(t, err)
			if tc.ddl != "" {
				_, err = db.Exec(t.Context(), tc.ddl) //nolint:glint // notestingrawsql: isolated incompatible schema fixture.
				require.NoError(t, err)
			}
			if tc.name == "deferrable and immediate targets" {
				// Prove PostgreSQL infers both indexes and rejects the deferred
				// arbiter even when an immediate alternative exists. Roll back
				// this isolated trial; production preflight only reads catalogs.
				trial, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: rollback-only PostgreSQL behavior regression test.
				require.NoError(t, err)
				//nolint:glint // notestingrawsql: isolated rollback-only conflict inference probe.
				_, insertErr := trial.Exec(t.Context(), `INSERT INTO chat_message_transcripts
(message_id, project_id, organization_id, ciphertext, source_hash)
VALUES ('10000000-0000-4000-8000-000000000001', '10000000-0000-4000-8000-000000000002', 'example', 'example', '\x00')
ON CONFLICT (message_id) DO UPDATE SET ciphertext = EXCLUDED.ciphertext`)
				require.NoError(t, trial.Rollback(t.Context()))
				var pgErr *pgconn.PgError
				require.ErrorAs(t, insertErr, &pgErr)
				require.Equal(t, "55000", pgErr.Code)
				require.Contains(t, pgErr.Message, "deferrable")
			}
			tx, err := db.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly}) //nolint:glint // notestingrawsql: proves preflight is read-only.
			require.NoError(t, err)
			defer func() { require.NoError(t, tx.Rollback(t.Context())) }()
			err = checkTranscriptShadowSchema(t.Context(), tx)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, "chat transcript shadow schema is unavailable or incompatible")
				// Empty dependencies would panic if provisioning or activation were
				// attempted. A nonexistent project also proves schema runs first.
				c := newTranscriptShadowCLIContext(t, map[string]string{chatTranscriptShadowProjectIDsFlag: "10000000-0000-4000-8000-000000000001"})
				_, err := configureTranscriptShadow(t.Context(), c, db, &chat.ChatMessageWriter{}, assetstest.NewTestBlobStore(t), orgEncryptionDeps{
					Service: &orgencryption.Service{}, Provisioner: &orgencryption.Provisioner{},
				}, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
				require.EqualError(t, err, "chat transcript shadow schema is unavailable or incompatible")
			}
		})
	}
}

// Deployed databases keep application tables in a non-public schema reached
// through search_path. The check must find the table where the store's
// unqualified queries do.
func TestTranscriptShadowSchemaFollowsSearchPath(t *testing.T) {
	t.Parallel()
	infra, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "transcript_shadow_search_path")
	require.NoError(t, err)

	//nolint:glint // notestingrawsql: isolated DDL fixture that moves the table into another schema.
	_, err = db.Exec(t.Context(), `DROP TABLE IF EXISTS chat_message_transcripts;
CREATE SCHEMA app;
CREATE TABLE app.chat_message_transcripts (message_id uuid NOT NULL PRIMARY KEY, project_id uuid NOT NULL,
 organization_id text NOT NULL, ciphertext text NOT NULL, source_hash bytea NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp())`)
	require.NoError(t, err)

	tx, err := db.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly}) //nolint:glint // notestingrawsql: proves preflight is read-only.
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(t.Context())) }()
	require.EqualError(t, checkTranscriptShadowSchema(t.Context(), tx), "chat transcript shadow schema is unavailable or incompatible",
		"a table outside the search_path is not the one the store would use")

	_, err = tx.Exec(t.Context(), "SET LOCAL search_path = app") //nolint:glint // notestingrawsql: mirrors the deployed connection's search_path.
	require.NoError(t, err)
	require.NoError(t, checkTranscriptShadowSchema(t.Context(), tx))
}
