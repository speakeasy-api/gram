package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

const (
	// entryKeyToolCall prefixes the key of the row that requests a tool call.
	entryKeyToolCall = "tool-call:"

	// entryKeyToolResult prefixes the key of the row that carries its result.
	// A call and its result share the id but stay two entries.
	entryKeyToolResult = "tool-result:"

	// modelToolUseIDPrefix is the prefix Anthropic mints tool-use ids with. An
	// id without it is a capture-side fallback (a per-session synthetic key, a
	// tool name, a trace id) that differs between sources and must not link rows.
	modelToolUseIDPrefix = "toolu_"

	// openSegmentRowLimit bounds how many rows of the linked chat an unkeyed
	// row is compared against. The segment is the tail after the last shared
	// tool call; a turn rarely holds more than a few dozen rows between tool
	// calls, and 500 keeps a runaway transcript from loading a whole chat on
	// every write.
	openSegmentRowLimit = 500
)

// transcriptRow is the capture-source-neutral view of one row about to be
// written: only what entry identity is derived from.
type transcriptRow struct {
	id         uuid.UUID
	chatID     uuid.UUID
	role       string
	content    string
	toolCalls  []byte
	toolCallID string
	userID     string
	userEmail  string

	// sessionID is the raw agent session id the capture source saw. Empty
	// means the row belongs to no agent session and takes no part in matching.
	sessionID string
}

type transcriptEntry struct {
	transcriptRow

	// keys are the entry keys this row would own as a primary.
	keys []string

	// primary is the stored row that already represents this entry, when one
	// exists. Set during resolve, or during commit when a concurrent writer
	// recorded the key first.
	primary uuid.NullUUID
}

// transcript is one write batch resolved against what is already stored.
//
// One agent session is often delivered by more than one capture source: Claude
// Code hooks write it to the chat named by its session id, and the Anthropic
// inference hook writes the same transcript to a chat of its own. Without a
// link every message is stored, scanned and metered twice. The transcript
// decides, per row about to be written, whether another stored row already
// represents the same entry. When it does, the new row is stored as a
// secondary (chat_messages.primary_message_id) and takes no further part in
// risk scanning, metering or downstream events.
//
// Two copies are the same entry only when they share an id the model minted
// once: the tool-use id of a tool call or its result. Prompts and replies
// carry no such id, so they are matched by text only inside the linked chat's
// open segment, the rows after the last tool call both chats share.
type transcript struct {
	projectID uuid.UUID
	entries   []transcriptEntry
}

func newTranscript(projectID uuid.UUID, rows []transcriptRow) *transcript {
	entries := make([]transcriptEntry, len(rows))
	for i, row := range rows {
		entries[i] = transcriptEntry{transcriptRow: row, keys: entryKeys(row), primary: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	}
	return &transcript{projectID: projectID, entries: entries}
}

func transcriptRowsFromWrites(writes []MessageWrite) []transcriptRow {
	rows := make([]transcriptRow, len(writes))
	for i, write := range writes {
		p := write.Params
		rows[i] = transcriptRow{
			id:         p.ID,
			chatID:     p.ChatID,
			role:       p.Role,
			content:    p.Content,
			toolCalls:  p.ToolCalls,
			toolCallID: p.ToolCallID.String,
			userID:     p.UserID.String,
			userEmail:  conv.Default(write.UserEmail, p.ExternalUserID.String),
			sessionID:  write.SessionID,
		}
	}
	return rows
}

func transcriptRowsFromExternalWrites(writes []ExternalMessageWrite) []transcriptRow {
	rows := make([]transcriptRow, len(writes))
	for i, write := range writes {
		p := write.Params
		rows[i] = transcriptRow{
			id:         p.ID,
			chatID:     p.ChatID,
			role:       p.Role,
			content:    p.Content,
			toolCalls:  p.ToolCalls,
			toolCallID: p.ToolCallID.String,
			userID:     p.UserID.String,
			userEmail:  conv.Default(write.UserEmail, p.ExternalUserID.String),
			sessionID:  write.SessionID,
		}
	}
	return rows
}

// entryKeys returns the keys a row owns. Rows outside an agent session own
// none: dashboard chats and provider imports never meet a second copy of
// themselves, and their tool ids are not Anthropic's.
func entryKeys(row transcriptRow) []string {
	if row.sessionID == "" {
		return nil
	}
	var keys []string
	for _, id := range toolCallIDsFromStoredJSON(row.toolCalls) {
		if strings.HasPrefix(id, modelToolUseIDPrefix) {
			keys = append(keys, entryKeyToolCall+id)
		}
	}
	if strings.HasPrefix(row.toolCallID, modelToolUseIDPrefix) {
		keys = append(keys, entryKeyToolResult+row.toolCallID)
	}
	return keys
}

// normalizeBody is the one place text is canonicalised before hashing, so a
// looser rule (collapsing whitespace, stripping injected blocks) changes here.
func normalizeBody(content string) string {
	return strings.TrimSpace(content)
}

// bodyKey identifies a prompt or reply by role and normalised text. It is
// never stored: two identical prompts in one chat are two prompts, so the key
// only means anything inside a segment both chats are known to share.
func bodyKey(role, content string) string {
	h := sha256.New()
	h.Write([]byte(role))
	h.Write([]byte{0})
	h.Write([]byte(normalizeBody(content)))
	return hex.EncodeToString(h.Sum(nil))
}

// sameUser decides whether a stored row may stand in for a new one. User ids
// win when both sides have one; otherwise both must carry the same email.
// Two rows that identify their user differently never match, because a
// client-asserted session id must not attach one user's rows to another's.
func sameUser(storedUserID, storedEmail, userID, email string) bool {
	if storedUserID != "" && userID != "" {
		return storedUserID == userID
	}
	storedEmail, email = conv.NormalizeEmail(storedEmail), conv.NormalizeEmail(email)
	return storedEmail != "" && email != "" && storedEmail == email
}

// resolve finds, without writing, the stored rows that already represent
// entries of this batch. It runs before the batch is inserted so metering and
// publication can leave the secondaries out; commit settles the races.
func (t *transcript) resolve(ctx context.Context, q *repo.Queries) error {
	if err := t.resolveKeys(ctx, q); err != nil {
		return err
	}
	return t.resolveBodies(ctx, q)
}

func (t *transcript) resolveKeys(ctx context.Context, q *repo.Queries) error {
	seen := make(map[string]struct{})
	var keys []string
	for _, entry := range t.entries {
		for _, key := range entry.keys {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}

	hits, err := q.LookupChatEntryKeys(ctx, repo.LookupChatEntryKeysParams{ProjectID: uuid.NullUUID{UUID: t.projectID, Valid: true}, Keys: keys})
	if err != nil {
		return fmt.Errorf("look up chat entry keys: %w", err)
	}
	owners := make(map[string]repo.LookupChatEntryKeysRow, len(hits))
	for _, hit := range hits {
		owners[hit.Key] = hit
	}
	for i := range t.entries {
		entry := &t.entries[i]
		for _, key := range entry.keys {
			owner, ok := owners[key]
			if !ok || !sameUser(owner.UserID.String, owner.ExternalUserID.String, entry.userID, entry.userEmail) {
				continue
			}
			entry.primary = uuid.NullUUID{UUID: owner.MessageID, Valid: true}
			break
		}
	}
	return nil
}

// segmentGroup is the slice of a batch that shares one chat and one session,
// which is the unit text matching works on.
type segmentGroup struct {
	chatID    uuid.UUID
	sessionID string
	indices   []int
}

func (t *transcript) segmentGroups() []segmentGroup {
	var groups []segmentGroup
	byChat := make(map[uuid.UUID]int)
	for i, entry := range t.entries {
		if entry.sessionID == "" {
			continue
		}
		gi, ok := byChat[entry.chatID]
		if !ok {
			gi = len(groups)
			byChat[entry.chatID] = gi
			groups = append(groups, segmentGroup{chatID: entry.chatID, sessionID: entry.sessionID, indices: nil})
		}
		groups[gi].indices = append(groups[gi].indices, i)
	}
	return groups
}

// resolveBodies matches the unkeyed prompts and replies of a batch against
// the linked chat's open segment. The linked chat is the one hooks write the
// same session to, so only the inference side of a pair can resolve here; in
// the usual arrival order (hooks first) that is the side that needs it.
func (t *transcript) resolveBodies(ctx context.Context, q *repo.Queries) error {
	for _, group := range t.segmentGroups() {
		linkedChatID := SessionIDToChatID(group.sessionID)
		if linkedChatID == group.chatID {
			continue
		}
		first := t.entries[group.indices[0]]
		linked, err := q.GetLinkedChatForSession(ctx, repo.GetLinkedChatForSessionParams{ID: linkedChatID, ProjectID: t.projectID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load linked chat for session: %w", err)
		}
		if !sameUser(linked.UserID.String, linked.ExternalUserID.String, first.userID, first.userEmail) {
			continue
		}

		segment, err := t.openSegment(ctx, q, group, linkedChatID)
		if err != nil {
			return err
		}
		candidates := make(map[string][]int, len(segment))
		for i, row := range segment {
			key := bodyKey(row.Role, row.Content)
			candidates[key] = append(candidates[key], i)
		}
		for _, idx := range group.indices {
			entry := &t.entries[idx]
			if entry.primary.Valid || len(entry.keys) > 0 || !isTextRole(entry.role) || normalizeBody(entry.content) == "" {
				continue
			}
			key := bodyKey(entry.role, entry.content)
			matches := candidates[key]
			if len(matches) == 0 {
				continue
			}
			entry.primary = uuid.NullUUID{UUID: segment[matches[0]].ID, Valid: true}
			candidates[key] = matches[1:]
		}
	}
	return nil
}

// openSegment loads the linked chat's rows after the last tool call the two
// chats share. Keyed rows of this batch that resolved into the linked chat
// are anchors too, so the segment starts after the newest of them.
func (t *transcript) openSegment(ctx context.Context, q *repo.Queries, group segmentGroup, linkedChatID uuid.UUID) ([]repo.ListChatOpenSegmentRow, error) {
	after := pgtype.Timestamptz{Time: time.Unix(0, 0).UTC(), Valid: true, InfinityModifier: pgtype.Finite}
	var afterSeq int64
	anchor, err := q.GetLastSharedAnchor(ctx, repo.GetLastSharedAnchorParams{
		ProjectID:    uuid.NullUUID{UUID: t.projectID, Valid: true},
		LinkedChatID: linkedChatID,
		ChatID:       group.chatID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("find last shared anchor: %w", err)
	default:
		after, afterSeq = anchor.CreatedAt, anchor.Seq
	}

	segment, err := q.ListChatOpenSegment(ctx, repo.ListChatOpenSegmentParams{
		ChatID:         linkedChatID,
		ProjectID:      uuid.NullUUID{UUID: t.projectID, Valid: true},
		AfterCreatedAt: after,
		AfterSeq:       afterSeq,
		RowLimit:       openSegmentRowLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list linked chat open segment: %w", err)
	}

	batchAnchors := make(map[uuid.UUID]struct{})
	for _, idx := range group.indices {
		if entry := t.entries[idx]; entry.primary.Valid {
			batchAnchors[entry.primary.UUID] = struct{}{}
		}
	}
	start := 0
	for i, row := range segment {
		if _, ok := batchAnchors[row.ID]; ok {
			start = i + 1
		}
	}
	return segment[start:], nil
}

func isTextRole(role string) bool {
	return role == "user" || role == "assistant"
}

// commit records what resolve decided, after the batch's rows exist: keys for
// the primaries, the primary pointer for the secondaries. Keys are inserted
// first so a concurrent writer of the same entry loses the race here rather
// than later. It returns each secondary row with its primary.
func (t *transcript) commit(ctx context.Context, q *repo.Queries) (map[uuid.UUID]uuid.UUID, error) {
	secondaries := make(map[uuid.UUID]uuid.UUID)
	for i := range t.entries {
		if err := t.commitEntry(ctx, q, &t.entries[i]); err != nil {
			return nil, err
		}
		if entry := t.entries[i]; entry.primary.Valid {
			secondaries[entry.id] = entry.primary.UUID
		}
	}
	return secondaries, nil
}

// commitEntry settles one row. A key conflict means another writer stored the
// same entry between resolve and now; the row then becomes a secondary of that
// owner when the owner is the same user's, and otherwise stays primary without
// the key. Any key this row recorded before the conflict stays with it.
func (t *transcript) commitEntry(ctx context.Context, q *repo.Queries, entry *transcriptEntry) error {
	project := uuid.NullUUID{UUID: t.projectID, Valid: true}
	if !entry.primary.Valid {
		for _, key := range entry.keys {
			_, err := q.InsertChatEntryKey(ctx, repo.InsertChatEntryKeyParams{ProjectID: project, ChatID: entry.chatID, MessageID: entry.id, Key: key})
			if err == nil {
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("record chat entry key: %w", err)
			}
			owners, err := q.LookupChatEntryKeys(ctx, repo.LookupChatEntryKeysParams{ProjectID: project, Keys: []string{key}})
			if err != nil {
				return fmt.Errorf("look up conflicting chat entry key: %w", err)
			}
			if len(owners) == 1 && owners[0].MessageID != entry.id && sameUser(owners[0].UserID.String, owners[0].ExternalUserID.String, entry.userID, entry.userEmail) {
				entry.primary = uuid.NullUUID{UUID: owners[0].MessageID, Valid: true}
				break
			}
		}
	}
	if !entry.primary.Valid {
		return nil
	}
	if err := q.MarkChatMessageSecondary(ctx, repo.MarkChatMessageSecondaryParams{PrimaryMessageID: entry.primary, ID: entry.id, ProjectID: project}); err != nil {
		return fmt.Errorf("mark chat message secondary: %w", err)
	}
	return nil
}

// primaryWrites drops the writes commit marked secondary, so side effects
// (metering, publication) count each entry once.
func primaryWrites(writes []MessageWrite, secondaries map[uuid.UUID]uuid.UUID) []MessageWrite {
	if len(secondaries) == 0 {
		return writes
	}
	kept := make([]MessageWrite, 0, len(writes))
	for _, write := range writes {
		if _, secondary := secondaries[write.Params.ID]; !secondary {
			kept = append(kept, write)
		}
	}
	return kept
}
