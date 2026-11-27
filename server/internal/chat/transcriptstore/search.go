package transcriptstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// Search over encrypted transcripts.
//
// SQL cannot match ciphertext, so for a project that keeps shadows a search
// scans the messages in keyset order, a fixed number per chunk: one query per
// chunk returns each message's ciphertext, the store opens it and matches in
// Go with the same ILIKE semantics the SQL used. A shadowed message never
// brings its plaintext body to the scan, so it is matched on the shadow only.
// A message with no shadow is matched on its plaintext, the same fallback every
// read applies. The rows a search returns are read or compared with
// plaintext, so in shadow mode a disagreeing transcript is logged and its
// plaintext served, as in every read. Each call scans a bounded number of
// messages and returns a cursor, so the caller decides whether to keep going. A project off the list
// keeps the single SQL ILIKE query and finishes in one call.

// SearchChunkMessages is how many messages one chunk scans. It matches the
// largest chat.load page (200 messages), so a chunk opens no more shadows than
// a page read already does.
const SearchChunkMessages = 200

// platformBodyBytes is the largest body field the Platform MCP chat tools
// match or return, the 131072-byte cap SearchPlatformChatMessages applies.
const platformBodyBytes = 131072

// ErrInvalidCursor reports a search cursor that this store did not issue or
// that belongs to another generation.
var ErrInvalidCursor = errors.New("invalid transcript search cursor")

// scanRow is one scanned message: its shadow, or its plaintext when it has none.
type scanRow struct {
	id         uuid.UUID
	chatID     uuid.UUID
	ciphertext pgtype.Text
	fallback   transcriptcrypto.ProtectedFields
}

// openScan returns the searchable text of each scanned row. A shadow is opened
// without reading its plaintext source, not even to hash it, so the match is
// decided on the shadow alone. The rows a search returns are compared with
// their plaintext when they are read for display. A row with no shadow keeps its
// plaintext and counts as plaintext_fallback.
func (t *Transcripts) openScan(ctx context.Context, db plainrepo.DBTX, projectID uuid.UUID, rows []scanRow) ([]transcriptcrypto.ProtectedFields, error) {
	s := t.store
	ctx, span := s.tracer.Start(ctx, "transcriptstore.Scan", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.TranscriptShadowMessageCount(len(rows)),
	))
	defer span.End()

	start := time.Now()
	opened, missing, err := t.openScanRows(ctx, db, projectID, rows)
	result := classify(err)
	s.metrics.read.recordDuration(ctx, projectID, time.Since(start), result)
	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		s.metrics.read.recordMessages(ctx, projectID, len(rows), result)
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
		return nil, fmt.Errorf("open encrypted chat transcript for search: %w", err)
	}
	s.metrics.read.recordMessages(ctx, projectID, len(rows)-missing, outcomeSuccess)
	s.metrics.read.recordMessages(ctx, projectID, missing, outcomePlaintextFallback)
	span.SetAttributes(attr.TranscriptShadowPlaintextFallbackCount(missing))
	return opened, nil
}

func (t *Transcripts) openScanRows(ctx context.Context, db plainrepo.DBTX, projectID uuid.UUID, rows []scanRow) ([]transcriptcrypto.ProtectedFields, int, error) {
	opened := make([]transcriptcrypto.ProtectedFields, len(rows))
	missing := 0
	for i, row := range rows {
		if !row.ciphertext.Valid {
			opened[i] = row.fallback
			missing++
		}
	}
	if missing == len(rows) {
		return opened, missing, nil
	}
	organizationID, err := t.organization(ctx, db, projectID)
	if err != nil {
		return nil, 0, err
	}
	codec, err := transcriptcrypto.New(t.store.service, organizationID)
	if err != nil {
		return nil, 0, safe(err)
	}
	for i, row := range rows {
		if !row.ciphertext.Valid {
			continue
		}
		message, err := codec.Decrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.chatID, MessageID: row.id}, row.ciphertext.String)
		if err != nil {
			return nil, 0, safe(err)
		}
		opened[i] = message
	}
	return opened, missing, nil
}

// ChatSearch selects one call of the in-chat search: messages of a generation
// whose text matches Query, each with ContextSize messages around it, up to
// MatchLimit matches over the whole search. Cursor is empty on the first call
// and the previous page's NextCursor after that.
type ChatSearch struct {
	ProjectID   uuid.UUID
	ChatID      uuid.UUID
	Generation  int32
	Query       string
	Cursor      string
	ContextSize int64
	MatchLimit  int32
}

// ChatSearchPage is what one call found: the matches and their context rows,
// oldest first, folded into segments by the caller. Windows from consecutive
// pages may share messages; the caller merges them by seq.
type ChatSearchPage struct {
	Rows []ListSearchWindowedMessagesRow
	// More is true when part of the generation is still unscanned; call again
	// with NextCursor.
	More       bool
	NextCursor string
	// Scanned counts the messages scanned so far by this search the way
	// GetChatEntryTotals counts Total (user, assistant and tool messages, and
	// any message with tool calls), so the two compare as progress. A project
	// off the list searches everything in one call and leaves it 0.
	Scanned int64
}

// chatSearchCursor is where a chunked chat search resumes. It is opaque to
// callers.
type chatSearchCursor struct {
	Generation int32 `json:"g"`
	// CreatedAtMicros and Seq are the keyset position of the last scanned row.
	CreatedAtMicros int64 `json:"t"`
	Seq             int64 `json:"s"`
	// WindowEnd is the ordinal of the last row any earlier page returned, so
	// the next page can join a window that starts right after it.
	WindowEnd int64 `json:"w"`
	Matches   int32 `json:"m"`
	Scanned   int64 `json:"n"`
}

const chatSearchCursorPrefix = "cs1."

func encodeChatSearchCursor(c chatSearchCursor) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode chat search cursor: %w", err)
	}
	return chatSearchCursorPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeChatSearchCursor(value string) (chatSearchCursor, error) {
	var c chatSearchCursor
	encoded, ok := strings.CutPrefix(value, chatSearchCursorPrefix)
	if !ok {
		return c, ErrInvalidCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return c, ErrInvalidCursor
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, ErrInvalidCursor
	}
	if c.Seq < 1 || c.Matches < 0 || c.Scanned < 0 || c.WindowEnd < 0 {
		return c, ErrInvalidCursor
	}
	return c, nil
}

// SearchChat runs one call of the in-chat search. For a project that keeps
// shadows it scans one chunk of SearchChunkMessages messages after the cursor
// and returns the windows around that chunk's matches. For any other project
// it runs the SQL ILIKE search over the whole generation and More is false.
func (t *Transcripts) SearchChat(ctx context.Context, db DBTX, s ChatSearch) (ChatSearchPage, error) {
	if !t.Enabled(s.ProjectID) {
		if s.Cursor != "" {
			return ChatSearchPage{}, ErrInvalidCursor
		}
		rows, err := t.ListSearchWindowedMessages(ctx, db, SearchWindow{ProjectID: s.ProjectID, ChatID: s.ChatID, Generation: s.Generation, Query: s.Query, MatchLimit: s.MatchLimit, ContextSize: s.ContextSize})
		if err != nil {
			return ChatSearchPage{}, err
		}
		return ChatSearchPage{Rows: rows, More: false, NextCursor: "", Scanned: 0}, nil
	}

	cursor := chatSearchCursor{Generation: s.Generation, CreatedAtMicros: 0, Seq: 0, WindowEnd: 0, Matches: 0, Scanned: 0}
	if s.Cursor != "" {
		decoded, err := decodeChatSearchCursor(s.Cursor)
		if err != nil {
			return ChatSearchPage{}, err
		}
		if decoded.Generation != s.Generation {
			return ChatSearchPage{}, ErrInvalidCursor
		}
		cursor = decoded
	}

	scanned, err := t.scanChatChunk(ctx, db, s, cursor)
	if err != nil {
		return ChatSearchPage{}, err
	}
	chunk, matches, more := scanned.rows, scanned.matches, scanned.more

	var rows []ListSearchWindowedMessagesRow
	if len(matches) > 0 {
		rows, err = t.listSearchMatchWindows(ctx, db, s, matches, cursor.WindowEnd)
		if err != nil {
			return ChatSearchPage{}, err
		}
	}

	page := ChatSearchPage{Rows: rows, More: more, NextCursor: "", Scanned: cursor.Scanned + scanned.counted}
	if !more || len(chunk) == 0 {
		page.More = false
		return page, nil
	}
	last := chunk[len(chunk)-1]
	next := chatSearchCursor{
		Generation:      s.Generation,
		CreatedAtMicros: last.CreatedAt.Time.UnixMicro(),
		Seq:             last.Seq,
		WindowEnd:       cursor.WindowEnd,
		Matches:         cursor.Matches + int32(len(matches)), //nolint:gosec // bounded by SearchChunkMessages
		Scanned:         page.Scanned,
	}
	if len(rows) > 0 {
		next.WindowEnd = max(next.WindowEnd, rows[len(rows)-1].Rn)
	}
	page.NextCursor, err = encodeChatSearchCursor(next)
	if err != nil {
		return ChatSearchPage{}, err
	}
	return page, nil
}

// chatChunk is one scanned chunk of a chat search: the rows scanned, how many
// of them GetChatEntryTotals counts, the IDs of those that match, and whether
// messages remain after them.
type chatChunk struct {
	rows    []plainrepo.ScanChatSearchChunkRow
	counted int64
	matches []uuid.UUID
	more    bool
}

// scanChatChunk scans the chunk after cursor and matches it in Go. It reads no
// plaintext of a shadowed message, so matches are decided on shadows alone.
func (t *Transcripts) scanChatChunk(ctx context.Context, db DBTX, s ChatSearch, cursor chatSearchCursor) (chatChunk, error) {
	chunk, err := plainrepo.New(db).ScanChatSearchChunk(ctx, plainrepo.ScanChatSearchChunkParams{
		ChatID:     s.ChatID,
		ProjectID:  s.ProjectID,
		Generation: s.Generation,
		HasCursor:  cursor.Seq > 0,
		CursorAt:   pgtype.Timestamptz{Time: time.UnixMicro(cursor.CreatedAtMicros).UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		CursorSeq:  cursor.Seq,
		ChunkSize:  SearchChunkMessages + 1,
	})
	if err != nil {
		return chatChunk{}, fmt.Errorf("scan chat search chunk: %w", err)
	}
	more := len(chunk) > SearchChunkMessages
	chunk = chunk[:min(len(chunk), SearchChunkMessages)]

	scan := make([]scanRow, len(chunk))
	for i, r := range chunk {
		scan[i] = scanRow{id: r.ID, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, fallback: transcriptcrypto.ProtectedFields{Content: r.FallbackContent, ContentRaw: r.FallbackContentRaw, ToolCalls: r.FallbackToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}}
	}
	opened, err := t.openScan(ctx, db, s.ProjectID, scan)
	if err != nil {
		return chatChunk{}, err
	}

	var counted int64
	for i, m := range opened {
		if countsAsEntry(chunk[i].Role, m.ToolCalls) {
			counted++
		}
	}

	pattern := compileLike("%" + s.Query + "%")
	remaining := int(s.MatchLimit - cursor.Matches)
	matches := make([]uuid.UUID, 0)
	for i, m := range opened {
		if len(matches) >= remaining {
			break
		}
		if pattern.matchBody(m.Content, m.ToolCalls, m.ContentRaw) {
			matches = append(matches, chunk[i].ID)
		}
	}
	if len(matches) >= remaining {
		// The match limit is reached: the search ends here, like the SQL
		// search's LIMIT on matches.
		more = false
	}

	return chatChunk{rows: chunk, counted: counted, matches: matches, more: more}, nil
}

// countsAsEntry reports whether GetChatEntryTotals counts a message in Total:
// a message with tool calls, or a user, assistant or tool message. Tool calls
// are counted the way capture counts them into the shadow's metadata, which is
// what the totals read for a shadowed message.
func countsAsEntry(role string, toolCalls []byte) bool {
	switch role {
	case "user", "assistant", "tool":
		return true
	}
	return metadataOf("", nil, toolCalls).toolCallCount > 0
}

// listSearchMatchWindows reads the windows around matches for display and
// opens them with the usual checks, so a disagreeing transcript is logged and
// its plaintext served.
func (t *Transcripts) listSearchMatchWindows(ctx context.Context, db DBTX, s ChatSearch, matches []uuid.UUID, bridge int64) ([]ListSearchWindowedMessagesRow, error) {
	windowed, err := plainrepo.New(db).ListSearchMatchWindows(ctx, plainrepo.ListSearchMatchWindowsParams{
		OpenShadows: t.Enabled(s.ProjectID),
		ChatID:      s.ChatID,
		ProjectID:   s.ProjectID,
		Generation:  s.Generation,
		MatchIds:    matches,
		ContextSize: s.ContextSize,
		BridgeRn:    bridge,
	})
	if err != nil {
		return nil, fmt.Errorf("list search match windows: %w", err)
	}
	rows := make([]ListSearchWindowedMessagesRow, len(windowed))
	joined := make([]joinedRow, len(windowed))
	for i := range windowed {
		rows[i] = ListSearchWindowedMessagesRow(windowed[i])
		r := &rows[i]
		joined[i] = joinedRow{body: ProtectedFieldRefs{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: &r.ToolOutcomeNotes, ToolCallSummaries: &r.ToolCallSummaries, StorageError: &r.StorageError}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, s.ProjectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// PlatformChatSearch selects one call of the Platform MCP chat search. Params
// carries the filters, sort order, keyset cursor and the escaped ILIKE
// Pattern; its RowLimit and OpenShadows are set by the method. Limit is the
// most hits to return. ScanBudget bounds the messages one call may scan for a
// project that keeps shadows.
type PlatformChatSearch struct {
	Params     SearchPlatformChatMessagesParams
	Limit      int
	ScanBudget int
}

// PlatformChatSearchPage is one page of hits, newest or oldest first.
type PlatformChatSearchPage struct {
	Rows []SearchPlatformChatMessagesRow
	// More is true when the search may have further hits; continue from
	// NextCreatedAt and NextID.
	More          bool
	NextCreatedAt pgtype.Timestamptz
	NextID        uuid.UUID
	// ScanIncomplete is true when the call stopped because it spent its scan
	// budget, not because it filled the page or reached the end.
	ScanIncomplete bool
	// Scanned counts the messages this call scanned in Go; 0 for a project
	// off the list or a search without a pattern, which SQL answers.
	Scanned int
}

// SearchPlatformChats runs one page of the Platform MCP chat search. A
// project off the list, or a search without a pattern, runs the single SQL
// query. A project that keeps shadows scans chunks in Go until the page is
// full, the messages run out or ScanBudget messages were scanned.
func (t *Transcripts) SearchPlatformChats(ctx context.Context, db DBTX, s PlatformChatSearch) (PlatformChatSearchPage, error) {
	if s.Limit < 1 || s.ScanBudget < 1 {
		return PlatformChatSearchPage{}, ErrInvalid
	}
	if !t.Enabled(s.Params.ProjectID) || s.Params.Pattern == "" {
		params := s.Params
		params.RowLimit = int32(s.Limit + 1) //nolint:gosec // small caller-bounded limit
		rows, err := t.SearchPlatformChatMessages(ctx, db, params)
		if err != nil {
			return PlatformChatSearchPage{}, err
		}
		page := PlatformChatSearchPage{Rows: rows[:min(len(rows), s.Limit)], More: len(rows) > s.Limit, NextCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, NextID: uuid.Nil, ScanIncomplete: false, Scanned: 0}
		if page.More {
			last := page.Rows[len(page.Rows)-1]
			page.NextCreatedAt, page.NextID = last.CreatedAt, last.ID
		}
		return page, nil
	}
	return t.scanPlatformChats(ctx, db, s)
}

func (t *Transcripts) scanPlatformChats(ctx context.Context, db DBTX, s PlatformChatSearch) (PlatformChatSearchPage, error) {
	p := s.Params
	pattern := compileLike(p.Pattern)
	q := plainrepo.New(db)
	page := PlatformChatSearchPage{Rows: []SearchPlatformChatMessagesRow{}, More: false, NextCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, NextID: uuid.Nil, ScanIncomplete: false, Scanned: 0}
	// shadowed lists the hits matched on a transcript, which are compared with
	// their plaintext rows before the page is returned.
	shadowed := make([]uuid.UUID, 0)
	hasCursor, cursorAt, cursorID := p.HasCursor, p.CursorAt, p.CursorID
	for page.Scanned < s.ScanBudget {
		size := min(SearchChunkMessages, s.ScanBudget-page.Scanned)
		chunk, err := q.ScanPlatformChatMessages(ctx, plainrepo.ScanPlatformChatMessagesParams{
			Pattern: p.Pattern, ProjectID: p.ProjectID, OrganizationID: p.OrganizationID,
			FromTime: p.FromTime, ToTime: p.ToTime, ChatIds: p.ChatIds,
			OwnUserID: p.OwnUserID, OwnedAgentIds: p.OwnedAgentIds, OwnerUserID: p.OwnerUserID, Source: p.Source,
			ParticipantID: p.ParticipantID, ParticipantProvider: p.ParticipantProvider,
			HasCursor: hasCursor, OldestFirst: p.OldestFirst, CursorAt: cursorAt, CursorID: cursorID,
			RowLimit: int32(size), //nolint:gosec // bounded by SearchChunkMessages
		})
		if err != nil {
			return PlatformChatSearchPage{}, fmt.Errorf("scan platform chat messages: %w", err)
		}
		scan := make([]scanRow, len(chunk))
		for i, r := range chunk {
			scan[i] = scanRow{id: r.ID, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, fallback: transcriptcrypto.ProtectedFields{Content: r.FallbackContent, ContentRaw: r.FallbackContentRaw, ToolCalls: r.FallbackToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}}
		}
		opened, err := t.openScan(ctx, db, p.ProjectID, scan)
		if err != nil {
			return PlatformChatSearchPage{}, err
		}
		for i, r := range chunk {
			m := opened[i]
			// Fields over the cap are neither matched nor returned, as in SQL,
			// which measures JSON columns by their jsonb text.
			toolCalls, contentRaw := jsonbField(m.ToolCalls), jsonbField(m.ContentRaw)
			over := platformOversized{
				content:    len(m.Content) > platformBodyBytes,
				toolCalls:  toolCalls != nil && len(*toolCalls) > platformBodyBytes,
				contentRaw: contentRaw != nil && len(*contentRaw) > platformBodyBytes,
			}
			platformShape(over)(&m)
			if over.toolCalls {
				toolCalls = nil
			}
			if over.contentRaw {
				contentRaw = nil
			}
			if r.TitleMatch || pattern.matchFields(m.Content, toolCalls, contentRaw) {
				if len(page.Rows) == s.Limit {
					// A further hit proves more remain: continue after the
					// last hit this page returns.
					last := page.Rows[len(page.Rows)-1]
					page.More, page.NextCreatedAt, page.NextID = true, last.CreatedAt, last.ID
					return page, t.checkScanHits(ctx, db, p.ProjectID, page.Rows, shadowed)
				}
				page.Rows = append(page.Rows, SearchPlatformChatMessagesRow{
					ID: r.ID, ChatID: r.ChatID, Seq: r.Seq, CreatedAt: r.CreatedAt, Role: r.Role, Generation: r.Generation,
					Source: r.Source, UserID: r.UserID, ChatTitle: r.ChatTitle, ChatTitleManuallySet: r.ChatTitleManuallySet, OwnerUserID: r.OwnerUserID,
					Content: m.Content, ToolCalls: m.ToolCalls, ContentRaw: m.ContentRaw,
					Oversized: over.content || over.toolCalls || over.contentRaw, ContentOversized: over.content, ToolCallsOversized: over.toolCalls, ContentRawOversized: over.contentRaw,
					ShadowCiphertext: pgtype.Text{String: "", Valid: false}, ShadowCurrent: false,
				})
				if r.ShadowCiphertext.Valid {
					shadowed = append(shadowed, r.ID)
				}
			}
			page.Scanned++
			hasCursor, cursorAt, cursorID = true, r.CreatedAt, r.ID
		}
		if len(chunk) < size {
			// The messages ran out: the search is complete.
			return page, t.checkScanHits(ctx, db, p.ProjectID, page.Rows, shadowed)
		}
	}
	// The scan budget is spent and messages may remain after the last one
	// scanned, whether or not it was a hit.
	page.More, page.ScanIncomplete, page.NextCreatedAt, page.NextID = true, true, cursorAt, cursorID
	return page, t.checkScanHits(ctx, db, p.ProjectID, page.Rows, shadowed)
}

// checkScanHits compares each hit matched on a transcript with its plaintext
// row in shadow mode, as every read does. A hit whose transcript disagrees is
// logged, counted as stale and served from plaintext, capped the same way.
// Only the hits are compared, so the scan never reads the plaintext of every
// message. Encrypted mode has no plaintext to compare.
func (t *Transcripts) checkScanHits(ctx context.Context, db DBTX, projectID uuid.UUID, rows []SearchPlatformChatMessagesRow, ids []uuid.UUID) error {
	if len(ids) == 0 || t.Mode(projectID) != ModeShadow {
		return nil
	}
	plain, err := plainrepo.New(db).ListSearchHitFields(ctx, plainrepo.ListSearchHitFieldsParams{ProjectID: projectID, MessageIds: ids})
	if err != nil {
		return fmt.Errorf("check searched transcripts: %w", safe(err))
	}
	byID := make(map[uuid.UUID]plainrepo.ListSearchHitFieldsRow, len(plain))
	for _, p := range plain {
		byID[p.ID] = p
	}
	stale := 0
	for i := range rows {
		p, ok := byID[rows[i].ID]
		if !ok {
			continue
		}
		r := &rows[i]
		served := transcriptcrypto.ProtectedFields{Content: r.Content, ContentRaw: r.ContentRaw, ToolCalls: r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}
		source := transcriptcrypto.ProtectedFields{Content: p.Content, ContentRaw: p.ContentRaw, ToolCalls: p.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}
		// Plaintext JSON arrives in jsonb's text form, which the cap measures.
		over := platformOversized{
			content:    len(source.Content) > platformBodyBytes,
			toolCalls:  len(source.ToolCalls) > platformBodyBytes,
			contentRaw: len(source.ContentRaw) > platformBodyBytes,
		}
		platformShape(over)(&source)
		if t.store.agrees(ctx, "search", projectID, r.ID, served, source) {
			continue
		}
		stale++
		r.Content, r.ContentRaw, r.ToolCalls = source.Content, source.ContentRaw, source.ToolCalls
		r.Oversized, r.ContentOversized, r.ToolCallsOversized, r.ContentRawOversized = over.content || over.toolCalls || over.contentRaw, over.content, over.toolCalls, over.contentRaw
	}
	t.store.metrics.read.recordMessages(ctx, projectID, stale, outcomeStale)
	return nil
}
