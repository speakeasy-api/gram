package mcpregistry

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
)

func publicationDate(t *testing.T, data json.RawMessage) string {
	t.Helper()
	var root struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(data, &root))
	var owned struct {
		PublishedAt string `json:"publishedAt"`
	}
	if raw := root.Meta[publicationNamespace]; len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &owned))
	}
	return owned.PublishedAt
}

func TestPublicationMetadataLifecycle(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	e, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	first := publicationDate(t, e.Data)
	require.NotEmpty(t, first)
	require.Equal(t, e.PublishedAt.UTC().Format(time.RFC3339Nano), first)
	_, err = time.Parse(time.RFC3339Nano, first)
	require.NoError(t, err)
	for _, published := range []bool{true, false, true} {
		e, err = s.SetPublished(ctx, e.ID, Token(e), published)
		require.NoError(t, err)
		require.Equal(t, first, publicationDate(t, e.Data))
		e, err = s.Save(ctx, e.ID, Token(e), e.Data)
		require.NoError(t, err)
		require.Equal(t, first, publicationDate(t, e.Data))
		// Publishers may omit the backend-owned namespace on subsequent saves.
		e, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(basicRecord))
		require.NoError(t, err)
		require.Equal(t, first, publicationDate(t, e.Data))
	}
	for _, published := range []bool{false, true} {
		id := uuid.New()
		raw := strings.ReplaceAll(basicRecord, "example.test/demo", "example.test/"+id.String())
		require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(raw), Published: published}))
		e, err = s.Get(ctx, id)
		require.NoError(t, err)
		require.Empty(t, publicationDate(t, e.Data))
		e, err = s.Save(ctx, id, Token(e), e.Data)
		require.NoError(t, err)
		require.Empty(t, publicationDate(t, e.Data))
		e, err = s.SetPublished(ctx, id, Token(e), published)
		require.NoError(t, err)
		require.Empty(t, publicationDate(t, e.Data))
		e, err = s.SetPublished(ctx, id, Token(e), false)
		require.NoError(t, err)
		e, err = s.SetPublished(ctx, id, Token(e), true)
		require.NoError(t, err)
		require.NotEmpty(t, publicationDate(t, e.Data))
	}
}

func TestReservedPublicationMetadataAndPrecision(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	raw := strings.TrimSuffix(basicRecord, "}") + `,"_meta":{"com.speakeasy.ai/registry":{"publishedAt":"2000-01-01T00:00:00Z"}}}`
	_, err := s.Create(ctx, json.RawMessage(raw))
	require.Error(t, err)
	raw = strings.TrimSuffix(basicRecord, "}") + `,"_meta":{"com.speakeasy.ai/catalog":{"documentationUrl":"https://example.test/docs"},"example.test/raw":{"n":9007199254740993,"d":1.234567890123456789}}}`
	e, err := s.Create(ctx, json.RawMessage(raw))
	require.NoError(t, err)
	require.Contains(t, string(e.Data), "9007199254740993")
	require.Contains(t, string(e.Data), "1.234567890123456789")
	forged := strings.ReplaceAll(string(e.Data), publicationDate(t, e.Data), "2000-01-01T00:00:00Z")
	_, err = s.Save(ctx, e.ID, Token(e), json.RawMessage(forged))
	require.Error(t, err)
	page, err := s.Discover(ctx, DiscoveryOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.JSONEq(t, string(e.Data), string(page.Records[0]))
}

func TestCatalogDocumentationMetadata(t *testing.T) {
	t.Parallel()
	v, err := LoadValidator()
	require.NoError(t, err)
	for _, url := range []string{"https://example.test/docs", "http://example.test/docs", "ftp://example.test/docs", "not a url", "https:///missing-host", "https://example.test /x", "https:/foo", "http:///", "https://?query", "https://@/docs", "https://:80/docs", "https://example.test/docs?q=v#section"} {
		raw := strings.TrimSuffix(basicRecord, "}") + `,"_meta":{"com.speakeasy.ai/catalog":{"documentationUrl":` + strconv.Quote(url) + `}}}`
		if url == "https://example.test/docs" || url == "http://example.test/docs" || url == "https://example.test/docs?q=v#section" {
			require.Empty(t, v.Validate([]byte(raw)))
		} else {
			require.NotEmpty(t, v.Validate([]byte(raw)))
		}
	}
}

func TestPublicationMetadataSizeAdmissionIsAtomic(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	id := uuid.New()
	prefix := strings.TrimSuffix(basicRecord, "}") + `,"padding":"`
	raw := prefix + `"}`
	size, err := repo.New(db).SerializedRegistryRecordBytes(ctx, []byte(raw))
	require.NoError(t, err)
	raw = prefix + strings.Repeat("x", StoredRecordByteLimit-int(size)) + `"}`
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: []byte(raw), Published: false}))
	old, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Len(t, old.Data, StoredRecordByteLimit)
	_, err = s.SetPublished(ctx, id, Token(old), true)
	var invalid *InvalidError
	require.ErrorAs(t, err, &invalid)
	got, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, old, got)
	require.True(t, got.PublishedAt.IsZero())
}

func TestPublicationMetadataCountsTowardPageBudget(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	const budget = 16 << 20
	// Three records fit before publication metadata, but not after it is added.
	for _, name := range []string{"example.test/page-a", "example.test/page-b", "example.test/page-c"} {
		prefix := strings.TrimSuffix(strings.ReplaceAll(basicRecord, "example.test/demo", name), "}") + `,"padding":"`
		size, err := repo.New(db).SerializedRegistryRecordBytes(ctx, []byte(prefix+`"}`))
		require.NoError(t, err)
		raw := prefix + strings.Repeat("x", budget/3-int(size)) + `"}`
		_, err = s.Create(ctx, []byte(raw))
		require.NoError(t, err)
	}
	page, err := s.Discover(ctx, DiscoveryOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Records, 2)
	require.NotEmpty(t, page.NextCursor)
	next, err := s.Discover(ctx, DiscoveryOptions{Limit: 10, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Records, 1)
	require.Empty(t, next.NextCursor)
	for _, raw := range append(page.Records, next.Records...) {
		require.NotEmpty(t, publicationDate(t, raw))
	}
}

func TestMetadataUnicodeAdmission(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	existing, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	for _, key := range []string{string([]byte{0xff}), string([]byte{0xed, 0xa0, 0x80}), `\ud800`, `\udc00`} {
		for _, member := range []string{`"` + key + `":true`, `"value":"` + key + `"`} {
			raw := []byte(strings.TrimSuffix(basicRecord, "}") + `,"_meta":{` + member + `}}`)
			require.Error(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: raw, Published: false}))
			_, err := s.Create(ctx, raw)
			require.ErrorAs(t, err, new(*InvalidError), "%q", raw)
			_, err = s.Save(ctx, existing.ID, Token(existing), raw)
			require.ErrorAs(t, err, new(*InvalidError), "%q", raw)
		}
	}
	for _, member := range []string{`"😀":"�"`, `"\ud83d\ude00":"\ud83d\ude00"`, `"\\ud800":"\\ud800"`} {
		raw := []byte(strings.TrimSuffix(basicRecord, "}") + `,"_meta":{` + member + `}}`)
		raw = []byte(strings.ReplaceAll(string(raw), "example.test/demo", "example.test/"+uuid.NewString()))
		e, err := s.Create(ctx, raw)
		require.NoError(t, err)
		var before, after map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &before))
		require.NoError(t, json.Unmarshal(e.Data, &after))
		var meta map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(after["_meta"], &meta))
		delete(meta, publicationNamespace)
		actual, err := json.Marshal(meta)
		require.NoError(t, err)
		require.JSONEq(t, string(before["_meta"]), string(actual))
	}
}
