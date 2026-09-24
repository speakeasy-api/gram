package mcpregistry

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

const basicRecord = `{"server":{"name":"example.test/demo","description":"Demo","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]}}`

func TestStaleSaveAndUnpublish(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	e, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	next, err := s.Save(ctx, e.ID, Token(e), json.RawMessage(basicRecord))
	require.NoError(t, err)
	require.GreaterOrEqual(t, next.UpdatedAt.Sub(e.UpdatedAt), time.Microsecond)
	_, err = s.SetPublished(ctx, e.ID, Token(e), false)
	require.ErrorIs(t, err, ErrConflict)
	hidden, err := s.SetPublished(ctx, e.ID, Token(next), false)
	require.NoError(t, err)
	saved, err := s.Save(ctx, e.ID, Token(hidden), json.RawMessage(basicRecord))
	require.NoError(t, err)
	require.False(t, saved.Published)
}
func TestStageARejectsEndpointChangeWithoutReferences(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	e, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(strings.ReplaceAll(basicRecord, "https://example.test/mcp", "https://example.test/new")))
	require.ErrorIs(t, err, ErrEndpointStructureImmutable)
	got, err := s.Get(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, Token(e), Token(got))
}
func TestMutationValidation(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	for _, raw := range []string{`{}`, `null`, basicRecord + ` {}`, strings.Replace(basicRecord, `"example.test/demo"`, `""`, 1)} {
		_, err := s.Create(ctx, json.RawMessage(raw))
		var invalid *InvalidError
		require.ErrorAs(t, err, &invalid)
	}
	e, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/other", 1)))
	var invalid *InvalidError
	require.ErrorAs(t, err, &invalid)
	for _, token := range []string{"", "garbage", Token(e) + "0", e.UpdatedAt.Add(time.Nanosecond).Format(time.RFC3339Nano)} {
		_, err = s.Save(ctx, e.ID, token, json.RawMessage(basicRecord))
		require.ErrorIs(t, err, ErrConflict)
	}
	_, err = s.Save(ctx, uuid.New(), Token(e), json.RawMessage(basicRecord))
	require.ErrorIs(t, err, ErrNotFound)
	invalidID := uuid.New()
	err = repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: invalidID, Data: []byte(`{"server":{"name":"example.test/invalid"}}`), Published: true})
	require.NoError(t, err)
	e, err = s.Get(ctx, invalidID)
	require.NoError(t, err)
	e, err = s.SetPublished(ctx, e.ID, Token(e), false)
	require.NoError(t, err)
	_, err = s.SetPublished(ctx, e.ID, Token(e), true)
	require.ErrorAs(t, err, &invalid)
}
func TestConcurrentMutations(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"save-save", "save-unpublish", "create"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, s, _ := newTestService(t)
			e, err := s.Create(ctx, json.RawMessage(basicRecord))
			require.NoError(t, err)
			start := make(chan struct{})
			results := make(chan error, 2)
			for i := range 2 {
				go func() {
					<-start
					var err error
					if mode == "create" {
						_, err = s.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/race", 1)))
					} else if mode == "save-unpublish" && i == 1 {
						_, err = s.SetPublished(ctx, e.ID, Token(e), false)
					} else {
						_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(basicRecord))
					}
					results <- err
				}()
			}
			close(start)
			a, b := <-results, <-results
			if a != nil {
				a, b = b, a
			}
			require.NoError(t, a)
			require.ErrorIs(t, b, ErrConflict)
		})
	}
}

func TestStageAPositionalEvidence(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	raw := `{"server":{"name":"example.test/evidence","description":"Demo","version":"1","remotes":[{"type":"streamable-http","url":"https://example.test/one"},{"type":"sse","url":"https://example.test/two"}]},"_meta":{"com.pulsemcp/server-version":{"remotes[0]":{"tools":[{"name":"one"}],"authOptions":[{"type":"oauth"}]},"remotes[1]":{"tools":[{"name":"two"}],"authOptions":[{"type":"bearer"}]}}}}`
	e, err := s.Create(ctx, json.RawMessage(raw))
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))
	server, ok := doc["server"].(map[string]any)
	require.True(t, ok)
	remotes, ok := server["remotes"].([]any)
	require.True(t, ok)
	for _, changed := range [][]any{{remotes[1], remotes[0]}, {remotes[1], remotes[0], remotes[1]}} {
		server["remotes"] = changed
		data, err := json.Marshal(doc)
		require.NoError(t, err)
		_, err = s.Save(ctx, e.ID, Token(e), data)
		require.ErrorIs(t, err, ErrEndpointStructureImmutable)
		got, err := s.Get(ctx, e.ID)
		require.NoError(t, err)
		require.Equal(t, e.Data, got.Data)
		require.Equal(t, Token(e), Token(got))
	}
	server["remotes"] = remotes
	meta, ok := doc["_meta"].(map[string]any)
	require.True(t, ok)
	evidence, ok := meta["com.pulsemcp/server-version"].(map[string]any)
	require.True(t, ok)
	other, err := json.Marshal(evidence["remotes[1]"])
	require.NoError(t, err)
	delete(evidence, "remotes[0]")
	removed, err := json.Marshal(doc)
	require.NoError(t, err)
	e, err = s.Save(ctx, e.ID, Token(e), removed)
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal(e.Data, &stored))
	storedMeta, ok := stored["_meta"].(map[string]any)
	require.True(t, ok)
	slots, ok := storedMeta["com.pulsemcp/server-version"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, slots, "remotes[0]")
	retained, err := json.Marshal(slots["remotes[1]"])
	require.NoError(t, err)
	require.JSONEq(t, string(other), string(retained))
	_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(strings.ReplaceAll(string(removed), "https://example.test/one", "https://example.test/new")))
	require.ErrorIs(t, err, ErrEndpointStructureImmutable)
}
func TestStageAProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b  string
		equal bool
	}{
		{`[{"type":"sse","url":"https://example.test","variables":{"x":{"default":"a"}}}]`, `[{"type":"sse","url":"https://example.test","variables":{"x":{"default":"b"}}}]`, false},
		{`[{"type":"sse","url":"https://example.test","variables":{"x":9007199254740992}}]`, `[{"type":"sse","url":"https://example.test","variables":{"x":9007199254740993}}]`, false},
		{`[{"type":"sse","url":"https://example.test"}]`, `[]`, false},
		{`[]`, `[{"type":"sse","url":"https://example.test"}]`, false},
		{`[{"type":"sse","url":"https://example.test"}]`, `[{"type":"streamable-http","url":"https://example.test"}]`, false},
		{`[{"type":"sse","url":"https://example.test","variables":{"a":1,"b":2}}]`, `[{"url":"https://example.test","type":"sse","variables":{"b":2,"a":1},"headers":[]}]`, true},
	} {
		err := checkStructure(json.RawMessage(`{"server":{"remotes":`+tc.a+`}}`), json.RawMessage(`{"server":{"remotes":`+tc.b+`}}`))
		if tc.equal {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrEndpointStructureImmutable)
		}
	}
}
func TestTokenParsing(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	e, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	for _, token := range []string{strings.Replace(Token(e), "Z", "1Z", 1), "2026-01-01T00:00:00,123Z", " " + Token(e)} {
		_, err = s.SetPublished(ctx, e.ID, token, false)
		require.ErrorIs(t, err, ErrConflict)
	}
	offset := e.UpdatedAt.In(time.FixedZone("offset", 3600)).Format(time.RFC3339Nano)
	next, err := s.SetPublished(ctx, e.ID, offset, true)
	require.NoError(t, err)
	require.GreaterOrEqual(t, next.UpdatedAt.Sub(e.UpdatedAt), time.Microsecond)
}

func TestStageANumericRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	raw := strings.Replace(basicRecord, `"url":"https://example.test/mcp"`, `"url":"https://example.test/mcp","variables":{"x":{"description":"number extension","extension":[1e3,1.0,9.007199254740993e15]}}`, 1)
	e, err := s.Create(ctx, json.RawMessage(raw))
	require.NoError(t, err)
	e, err = s.Get(ctx, e.ID)
	require.NoError(t, err)
	e, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(raw))
	require.NoError(t, err)
	equivalent := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(raw, "1e3", "1000.0"), "1.0,", "1,"), "9.007199254740993e15", "9007199254740993.0")
	e, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(equivalent))
	require.NoError(t, err)
	_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(strings.ReplaceAll(equivalent, "9007199254740993.0", "9007199254740992")))
	require.ErrorIs(t, err, ErrEndpointStructureImmutable)
	got, err := s.Get(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, e.Data, got.Data)
	require.Equal(t, Token(e), Token(got))
}

func TestInvalidSaveAndHistoricalRepair(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	e, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	invalid := strings.Replace(basicRecord, `"description":"Demo"`, `"description":false`, 1)
	_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(invalid))
	var validation *InvalidError
	require.ErrorAs(t, err, &validation)
	got, err := s.Get(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, e.Data, got.Data)
	require.Equal(t, Token(e), Token(got))
	id := uuid.New()
	invalid = strings.Replace(invalid, "example.test/demo", "example.test/repair", 1)
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(invalid), Published: false}))
	historical, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.NotEmpty(t, s.validator.Validate(historical.Data))
	byName, err := s.GetByName(ctx, "example.test/repair")
	require.NoError(t, err)
	require.Equal(t, id, byName.ID)
	repaired, err := s.Save(ctx, id, Token(historical), json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/repair", 1)))
	require.NoError(t, err)
	require.Empty(t, s.validator.Validate(repaired.Data))
	require.False(t, repaired.Published)
	require.NotEqual(t, Token(historical), Token(repaired))
	require.Equal(t, recordName(historical.Data), recordName(repaired.Data))
	require.NoError(t, checkStructure(historical.Data, repaired.Data))
}
func TestExactJSONNumbers(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{`1`, `1.0`}, {`1000`, `1e3`}, {`-0`, `0.0e100`}, {`9007199254740993`, `9.007199254740993e15`}, {`{"a":[1,2],"b":3}`, `{"b":3.0,"a":[1e0,2]}`}} {
		require.True(t, equalJSON([]byte(pair[0]), []byte(pair[1])))
	}
	for _, pair := range [][2]string{{`9007199254740993`, `9007199254740992`}, {`[1,2]`, `[2,1]`}, {`1e999999999999999999999999`, `1`}, {`1e16777217`, `1`}} {
		require.False(t, equalJSON([]byte(pair[0]), []byte(pair[1])))
	}
}

// The database protects identity, not the full registry document schema.
func TestDatabaseIdentityConstraint(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{}`, `{"server":null}`, `{"server":false}`, `{"server":[]}`, `{"server":{}}`,
		`{"Server":{"name":"io.example/a"}}`, `{"server":{"Name":"io.example/a"}}`,
		`{"server":{"name":null}}`, `{"server":{"name":false}}`, `{"server":{"name":42}}`,
		`{"server":{"name":[]}}`, `{"server":{"name":{}}}`, `{"server":{"name":""}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			ctx, _, db := newTestService(t)
			q := repo.New(db)
			err := q.InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: []byte(raw), Published: false})
			var violation *pgconn.PgError
			require.ErrorAs(t, err, &violation, "missing database identity CHECK: invalid insert succeeded")
			require.Equal(t, pgerrcode.CheckViolation, violation.Code)
			require.Equal(t, "mcp_registry_entries_name_check", violation.ConstraintName)
			id := uuid.New()
			original := []byte(`{"server":{"name":"io.example/original"}}`)
			require.NoError(t, q.InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: original, Published: false}))
			_, err = q.UpdateEntry(ctx, repo.UpdateEntryParams{StoredRecordLimit: StoredRecordByteLimit, ID: id, Data: []byte(raw)})
			require.ErrorAs(t, err, &violation)
			require.Equal(t, pgerrcode.CheckViolation, violation.Code)
			require.Equal(t, "mcp_registry_entries_name_check", violation.ConstraintName)
			retained, err := q.GetEntry(ctx, id)
			require.NoError(t, err)
			require.JSONEq(t, string(original), string(retained.Data))
		})
	}
}

func TestDatabaseIdentityUniqueness(t *testing.T) {
	t.Parallel()
	for _, published := range []bool{false, true} {
		for _, duplicatePublished := range []bool{false, true} {
			ctx, _, db := newTestService(t)
			q := repo.New(db)
			original := []byte(`{"server":{"name":"io.example/duplicate"}}`)
			require.NoError(t, q.InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: original, Published: published}))
			err := q.InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: original, Published: duplicatePublished})
			var violation *pgconn.PgError
			require.ErrorAs(t, err, &violation)
			require.Equal(t, pgerrcode.UniqueViolation, violation.Code)
			require.Equal(t, "mcp_registry_entries_name_key", violation.ConstraintName)
			id := uuid.New()
			require.NoError(t, q.InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(`{"server":{"name":"io.example/other"}}`), Published: duplicatePublished}))
			_, err = q.UpdateEntry(ctx, repo.UpdateEntryParams{StoredRecordLimit: StoredRecordByteLimit, ID: id, Data: original})
			require.ErrorAs(t, err, &violation)
			require.Equal(t, pgerrcode.UniqueViolation, violation.Code)
			require.Equal(t, "mcp_registry_entries_name_key", violation.ConstraintName)
		}
	}
}

func TestSaveExactEndpointKeys(t *testing.T) {
	t.Parallel()
	const remotes = `[{"type":"streamable-http","url":"https://example.test/mcp"}]`
	for _, key := range []string{"Server", "Remotes"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			ctx, s, _ := newTestService(t)
			e, err := s.Create(ctx, json.RawMessage(basicRecord))
			require.NoError(t, err)
			changed := strings.Replace(basicRecord, "https://example.test/mcp", "https://example.test/changed", 1)
			if key == "Server" {
				changed = strings.TrimSuffix(changed, "}") + `,"Server":{"remotes":` + remotes + `}}`
			} else {
				changed = strings.TrimSuffix(changed, "}}") + `,"Remotes":` + remotes + `}}`
			}
			require.Empty(t, s.validator.Validate(json.RawMessage(changed)))
			_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(changed))
			require.ErrorIs(t, err, ErrEndpointStructureImmutable)
			got, err := s.Get(ctx, e.ID)
			require.NoError(t, err)
			require.Equal(t, e.Data, got.Data)
			require.Equal(t, Token(e), Token(got))
		})
	}
	t.Run("unknown metadata", func(t *testing.T) {
		t.Parallel()
		ctx, s, _ := newTestService(t)
		e, err := s.Create(ctx, json.RawMessage(basicRecord))
		require.NoError(t, err)
		updated := strings.TrimSuffix(basicRecord, "}}") + `,"Remotes":{"extension":9007199254740993}},"Server":{"extension":9007199254740993},"extension":9007199254740993}`
		require.Empty(t, s.validator.Validate(json.RawMessage(updated)))
		saved, err := s.Save(ctx, e.ID, Token(e), json.RawMessage(updated))
		require.NoError(t, err)
		require.True(t, equalJSON(json.RawMessage(updated), saved.Data))
		got, err := s.Get(ctx, e.ID)
		require.NoError(t, err)
		require.Equal(t, saved.Data, got.Data)
	})
}

func TestEndpointStructureErrorPaths(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"type", "url", "variables"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			old := json.RawMessage(`{"server":{"remotes":[{}]}}`)
			updated := json.RawMessage(`{"server":{"remotes":[{"` + key + `":null}]}}`)
			err := checkStructure(old, updated)
			require.ErrorIs(t, err, ErrEndpointStructureImmutable)
			require.EqualError(t, err, ErrEndpointStructureImmutable.Error()+": /server/remotes/0/"+key)
		})
	}
}
