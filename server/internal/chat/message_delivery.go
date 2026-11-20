package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/transcript"
)

// ErrAccountMismatch reports a transcript delivery whose sender is not the
// account that owns the chat, or that shares no identity with the owner to
// compare. Nothing is written. A caller stores such a delivery in a chat of
// its own, named by AccountChatID.
var ErrAccountMismatch = errors.New("transcript delivery sender does not own the chat")

// errNaturalIDTaken reports a natural ID that another stored entry of the chat
// already names. Like a unique violation, it means the load was stale.
var errNaturalIDTaken = errors.New("natural id already names another chat message")

// deliveryAttempts bounds how often a delivery is loaded and merged. The chat
// lock makes a conflict unlikely; the unique indexes catch the rest, and a
// second attempt sees what the other writer stored.
const deliveryAttempts = 2

// TranscriptAccount identifies who sent a transcript delivery, or who owns a
// chat. Any field may be empty.
type TranscriptAccount struct {
	// UserAccountID is the AI account (user_accounts row). uuid.Nil when
	// unknown.
	UserAccountID uuid.UUID
	// UserID is the Speakeasy user.
	UserID string
	// Email is compared without regard to case or surrounding space.
	Email string
}

func (a TranscriptAccount) normalized() TranscriptAccount {
	return TranscriptAccount{
		UserAccountID: a.UserAccountID,
		UserID:        strings.TrimSpace(a.UserID),
		Email:         conv.NormalizeEmail(a.Email),
	}
}

// key names the account by its first identity, in the order sameAccount
// compares them. It is empty for an account with no identity.
func (a TranscriptAccount) key() string {
	a = a.normalized()
	switch {
	case a.UserAccountID != uuid.Nil:
		return "user_account:" + a.UserAccountID.String()
	case a.UserID != "":
		return "user:" + a.UserID
	case a.Email != "":
		return "email:" + a.Email
	default:
		return ""
	}
}

// sameAccount reports whether a sender may write into a chat that owner
// holds. It compares the first identity both have, in the order AI account,
// Speakeasy user, email. Accounts with no identity in common never match.
func sameAccount(owner, sender TranscriptAccount) bool {
	owner, sender = owner.normalized(), sender.normalized()
	switch {
	case owner.UserAccountID != uuid.Nil && sender.UserAccountID != uuid.Nil:
		return owner.UserAccountID == sender.UserAccountID
	case owner.UserID != "" && sender.UserID != "":
		return owner.UserID == sender.UserID
	case owner.Email != "" && sender.Email != "":
		return owner.Email == sender.Email
	default:
		return false
	}
}

// DeliveryTarget is the chat a transcript delivery is stored in. The caller
// creates or ensures the chat row before writing, as for other writes.
type DeliveryTarget struct {
	ProjectID uuid.UUID
	ChatID    uuid.UUID
	// Sender is the account that sent the delivery. It must own the chat.
	Sender TranscriptAccount
}

// DeliveryWrite is a transcript delivery and the row fields its new entries
// take.
type DeliveryWrite struct {
	Delivery transcript.Delivery

	// Template holds what every new row of the delivery shares, in the shape
	// of an imported message: model, user labels, origin, user agent, IP
	// address, source and finish reason in Params, and the metering and
	// provider attribution beside them. Each entry decides its own id, role,
	// content, tool calls, tool call id, message id and created_at. Tree rows
	// carry no external message id, content hash or token counts.
	// Params.ProjectID and Params.ChatID must be zero or match the target.
	Template ExternalMessageWrite

	// Rows refines the template per entry, indexed like Delivery.Entries.
	// Leave it empty, or give one row per entry.
	Rows []DeliveryRow
}

// DeliveryRow holds the row fields of one entry that the transcript does not
// model.
type DeliveryRow struct {
	// Model overrides Template.Params.Model when set.
	Model string
	// FinishReason overrides Template.Params.FinishReason when set.
	FinishReason string
	// ContentRaw is the source message the entry was split from.
	ContentRaw []byte
	// Parts are stored under the entry's row when the entry is new, and
	// dropped when it was stored before. Upload their bodies with
	// WriteContentPartAssets first. The writer sets ChatID, ProjectID and
	// ParentChatMessageID.
	Parts []repo.CreateChatContentPartParams
}

// DeliveryEntry names the row that holds one entry of a delivery.
type DeliveryEntry struct {
	// Index is the entry's position in Delivery.Entries.
	Index int
	// MessageID is the chat_messages row that holds the entry.
	MessageID uuid.UUID
}

// DeliveryResult reports what a delivery changed.
type DeliveryResult struct {
	// Added are the entries stored as new rows, in delivery order. Only these
	// were metered and announced, and of these only the ones that are not
	// synthetic.
	Added []DeliveryEntry
	// Seen are the entries that were stored before. They produce nothing
	// downstream.
	Seen []DeliveryEntry
}

// WriteDelivery merges a transcript delivery into the chat's transcript tree
// and stores what changed, in one transaction:
//
//   - The sender must own the chat (same project, same account; see
//     sameAccount). Otherwise it returns ErrChatNotInProject or
//     ErrAccountMismatch and writes nothing.
//   - Merges into one chat are serialized by a transaction-level advisory
//     lock, taken before the tree is loaded. The unique indexes stay the
//     safety net: a merge that loses a race anyway is rolled back and retried
//     once with a fresh load.
//   - Only the part of the tree the merge needs is loaded, and never message
//     text.
//   - New entries become rows, including the copies a partial compaction
//     makes of kept messages, which carry tree-scoped natural IDs. Entries
//     stored before only add the natural IDs and continuations they bring, and
//     mark a row synthetic when they show it is generated history. Only new
//     rows that are not synthetic are metered, announced and left for risk
//     analysis; synthetic rows (such as compaction summaries) are stored, or
//     marked, already analyzed.
func (w *ChatMessageWriter) WriteDelivery(ctx context.Context, target DeliveryTarget, write DeliveryWrite) (DeliveryResult, error) {
	if err := validateDelivery(target, write); err != nil {
		return DeliveryResult{Added: nil, Seen: nil}, err
	}
	if len(write.Delivery.Entries) == 0 && len(write.Delivery.Links) == 0 {
		return DeliveryResult{Added: nil, Seen: nil}, nil
	}

	organizationID, err := repo.New(w.db).GetProjectOrganizationID(ctx, target.ProjectID)
	if err != nil {
		return DeliveryResult{Added: nil, Seen: nil}, fmt.Errorf("get project organization id: %w", err)
	}

	for attempt := 1; ; attempt++ {
		result, scanned, err := w.writeDeliveryOnce(ctx, organizationID, target, write)
		if err == nil {
			if scanned {
				w.notifyMessagesStored(ctx, target.ProjectID)
			}
			return result, nil
		}
		if attempt >= deliveryAttempts || !isDeliveryConflict(err) {
			return DeliveryResult{Added: nil, Seen: nil}, err
		}
		w.logger.WarnContext(ctx, "transcript delivery conflicted with a concurrent write; retrying",
			attr.SlogError(err),
			attr.SlogChatID(target.ChatID.String()),
			attr.SlogProjectID(target.ProjectID.String()),
		)
	}
}

// writeDeliveryOnce runs one load, merge and store. It reports whether a row
// that risk analysis must scan was stored.
func (w *ChatMessageWriter) writeDeliveryOnce(ctx context.Context, organizationID string, target DeliveryTarget, write DeliveryWrite) (DeliveryResult, bool, error) {
	empty := DeliveryResult{Added: nil, Seen: nil}
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return empty, false, fmt.Errorf("begin transcript delivery transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)

	// The lock comes first: before the owner check and the load, and before
	// anything later in the transaction (Claude Tag metadata) takes the
	// project advisory lock.
	if err := queries.LockChatTranscript(ctx, target.ChatID); err != nil {
		return empty, false, fmt.Errorf("lock chat transcript: %w", err)
	}
	owner, err := queries.GetChatTranscriptOwner(ctx, repo.GetChatTranscriptOwnerParams{ChatID: target.ChatID, ProjectID: target.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, false, ErrChatNotInProject
	}
	if err != nil {
		return empty, false, fmt.Errorf("load chat transcript owner: %w", err)
	}
	ownerAccount := TranscriptAccount{UserAccountID: owner.UserAccountID.UUID, UserID: owner.UserID, Email: owner.Email}
	if !sameAccount(ownerAccount, target.Sender) {
		return empty, false, ErrAccountMismatch
	}

	tree, loaded, err := loadDeliveryTranscript(ctx, queries, target, write.Delivery)
	if err != nil {
		return empty, false, err
	}
	merged, err := tree.Merge(write.Delivery)
	if err != nil {
		return empty, false, fmt.Errorf("merge transcript delivery: %w", err)
	}

	occurredAt := time.Now().UTC()
	plan, err := planDelivery(target, write, merged, loaded, occurredAt)
	if err != nil {
		return empty, false, err
	}
	if err := w.storeDelivery(ctx, tx, organizationID, target, write, plan, occurredAt); err != nil {
		return empty, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, false, fmt.Errorf("commit transcript delivery transaction: %w", err)
	}
	return plan.result, len(plan.writes) > 0, nil
}

func isDeliveryConflict(err error) bool {
	if errors.Is(err, errNaturalIDTaken) {
		return true
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == pgerrcode.UniqueViolation
}

func validateDelivery(target DeliveryTarget, write DeliveryWrite) error {
	if target.ProjectID == uuid.Nil || target.ChatID == uuid.Nil {
		return fmt.Errorf("transcript delivery needs a project and a chat")
	}
	params := write.Template.Params
	if params.ProjectID != uuid.Nil && params.ProjectID != target.ProjectID {
		return fmt.Errorf("transcript delivery template project does not match target")
	}
	if params.ChatID != uuid.Nil && params.ChatID != target.ChatID {
		return fmt.Errorf("transcript delivery template chat does not match target")
	}
	switch write.Delivery.Source {
	case transcript.SourceClaudeTranscript, transcript.SourceAnthropicInference, transcript.SourceCompliance:
	default:
		return fmt.Errorf("unknown transcript delivery source %q", write.Delivery.Source)
	}
	if len(write.Rows) != 0 && len(write.Rows) != len(write.Delivery.Entries) {
		return fmt.Errorf("transcript delivery has %d rows for %d entries", len(write.Rows), len(write.Delivery.Entries))
	}
	for _, e := range write.Delivery.Entries {
		for _, id := range e.IDs {
			if err := validateNaturalID(id); err != nil {
				return err
			}
		}
	}
	for _, l := range write.Delivery.Links {
		if err := validateNaturalID(l.ID); err != nil {
			return err
		}
	}
	return nil
}

// validateNaturalID checks an ID that will be stored.
func validateNaturalID(id transcript.NaturalID) error {
	switch id.Kind {
	case transcript.KindToolCall, transcript.KindToolResult, transcript.KindPrompt, transcript.KindClaudeEntry, transcript.KindComplianceMessage:
	default:
		return fmt.Errorf("unknown natural id kind %q", id.Kind)
	}
	if id.Value == "" || strings.ContainsRune(id.Value, 0) {
		return fmt.Errorf("natural id of kind %q has an empty value or a NUL byte", id.Kind)
	}
	if id.Block < 0 || id.Block > math.MaxInt16 {
		return fmt.Errorf("natural id of kind %q has block %d out of range", id.Kind, id.Block)
	}
	return nil
}

// treeNode is a stored entry without its text, as the merge needs it.
type treeNode struct {
	id          uuid.UUID
	parentID    uuid.NullUUID
	continuesID uuid.NullUUID
	role        string
	synthetic   bool
	contentKey  []byte
	pathKey     []byte
}

// storedNaturalID is a natural ID row. An invalid message id stands for the
// start of the chat.
type storedNaturalID struct {
	messageID uuid.NullUUID
	id        transcript.NaturalID
}

// loadDeliveryTranscript loads the part of the chat's tree that the merge of d
// needs and returns it with the ids of the loaded entries.
func loadDeliveryTranscript(ctx context.Context, queries *repo.Queries, target DeliveryTarget, d transcript.Delivery) (*transcript.Transcript, map[uuid.UUID]struct{}, error) {
	var (
		nodes []treeNode
		ids   []storedNaturalID
		err   error
	)
	if namesParents(d) {
		nodes, ids, err = loadWholeTree(ctx, queries, target)
	} else {
		nodes, ids, err = loadTrajectory(ctx, queries, target, d.Entries)
	}
	if err != nil {
		return nil, nil, err
	}

	byMessage := make(map[uuid.UUID][]transcript.NaturalID, len(nodes))
	var start []transcript.NaturalID
	for _, id := range ids {
		if !id.messageID.Valid {
			start = append(start, id.id)
			continue
		}
		byMessage[id.messageID.UUID] = append(byMessage[id.messageID.UUID], id.id)
	}
	stored := make([]transcript.Stored, 0, len(nodes))
	loaded := make(map[uuid.UUID]struct{}, len(nodes))
	for _, n := range nodes {
		stored = append(stored, transcript.Stored{
			ID:          n.id,
			ParentID:    n.parentID,
			ContinuesID: n.continuesID,
			Role:        transcript.Role(n.role),
			Synthetic:   n.synthetic,
			ContentKey:  n.contentKey,
			PathKey:     n.pathKey,
			IDs:         byMessage[n.id],
		})
		loaded[n.id] = struct{}{}
	}
	tree := transcript.New(uuid.NewV7)
	tree.Load(stored)
	tree.LoadStartAliases(start)
	return tree, loaded, nil
}

// namesParents reports a delivery that places entries by naming their
// parents, such as a Claude transcript. Those names can point anywhere in the
// tree. A delivery that names none is a whole trajectory, such as an inference
// frame.
func namesParents(d transcript.Delivery) bool {
	if len(d.Links) > 0 {
		return true
	}
	for _, e := range d.Entries {
		if e.Parent != nil || e.Continues != nil {
			return true
		}
	}
	return false
}

// loadWholeTree loads every stored entry of the chat and every natural ID.
// Claude transcripts are uploaded when a session stops, so this runs once per
// stop, and reads one skeleton row (two keys, no text) per stored entry: a
// few thousand rows for a long session.
func loadWholeTree(ctx context.Context, queries *repo.Queries, target DeliveryTarget) ([]treeNode, []storedNaturalID, error) {
	rows, err := queries.ListChatTranscriptTree(ctx, repo.ListChatTranscriptTreeParams{ChatID: target.ChatID, ProjectID: target.ProjectID})
	if err != nil {
		return nil, nil, fmt.Errorf("load chat transcript tree: %w", err)
	}
	nodes := make([]treeNode, 0, len(rows))
	for _, r := range rows {
		nodes = append(nodes, treeNode{id: r.ID, parentID: r.ParentMessageID, continuesID: r.ContinuesMessageID, role: r.Role, synthetic: r.Synthetic, contentKey: r.ContentKey, pathKey: r.PathKey})
	}
	idRows, err := queries.ListChatNaturalIDs(ctx, repo.ListChatNaturalIDsParams{ChatID: target.ChatID, ProjectID: target.ProjectID})
	if err != nil {
		return nil, nil, fmt.Errorf("load chat natural ids: %w", err)
	}
	ids := make([]storedNaturalID, 0, len(idRows))
	for _, r := range idRows {
		ids = append(ids, storedNaturalID{messageID: r.ChatMessageID, id: transcript.NaturalID{Kind: transcript.Kind(r.Kind), Value: r.Value, Block: int(r.BlockIndex)}})
	}
	return nodes, ids, nil
}

// idOwner is a stored entry that owns a natural ID a delivered entry carries.
type idOwner struct {
	id      uuid.UUID
	pathKey []byte
}

// trajectorySegment is a run of delivered entries that the merge matches by
// content, one step down the tree per entry, from one starting point.
type trajectorySegment struct {
	// from is the stored entry the run continues after. Invalid means the
	// start of the chat.
	from uuid.NullUUID
	// keys are the path keys the run's entries have if they are stored.
	keys [][]byte
}

// loadTrajectory loads what a whole-trajectory delivery needs: the deepest
// stored entry its trajectory reaches, with its ancestors and children, and
// every entry that owns a natural ID the delivery carries.
//
// An entry with a stored natural ID is matched by that ID wherever it sits,
// so the entries after it continue from that stored entry, whose path key can
// differ from the trajectory's (a Claude transcript chains parallel tool calls
// one after another). The trajectory is cut into a segment per such anchor,
// and each segment finds its own deepest stored entry.
func loadTrajectory(ctx context.Context, queries *repo.Queries, target DeliveryTarget, entries []transcript.Entry) ([]treeNode, []storedNaturalID, error) {
	owners, start, err := findIDOwners(ctx, queries, target, entries)
	if err != nil {
		return nil, nil, err
	}
	segments := trajectorySegments(entries, owners)

	var keys [][]byte
	seenKey := make(map[string]struct{})
	for _, s := range segments {
		for _, k := range s.keys {
			if _, ok := seenKey[string(k)]; !ok {
				seenKey[string(k)] = struct{}{}
				keys = append(keys, k)
			}
		}
	}
	matches := make(map[string][]uuid.UUID)
	if len(keys) > 0 {
		rows, err := queries.ListChatMessageIDsByPathKey(ctx, repo.ListChatMessageIDsByPathKeyParams{ChatID: target.ChatID, ProjectID: target.ProjectID, PathKeys: keys})
		if err != nil {
			return nil, nil, fmt.Errorf("find chat messages by path key: %w", err)
		}
		for _, r := range rows {
			matches[string(r.PathKey)] = append(matches[string(r.PathKey)], r.ID)
		}
	}

	var seeds, parents []uuid.UUID
	includeRoots := false
	for _, s := range segments {
		var tips []uuid.UUID
		for k := len(s.keys) - 1; k >= 0 && tips == nil; k-- {
			tips = matches[string(s.keys[k])]
		}
		switch {
		case tips != nil:
			seeds = append(seeds, tips...)
			parents = append(parents, tips...)
		case s.from.Valid:
			parents = append(parents, s.from.UUID)
		default:
			includeRoots = true
		}
	}
	for _, list := range owners {
		for _, o := range list {
			seeds = append(seeds, o.id)
		}
	}

	rows, err := queries.ListChatTranscriptNodes(ctx, repo.ListChatTranscriptNodesParams{
		ChatID:       target.ChatID,
		ProjectID:    target.ProjectID,
		Ids:          conv.DefaultSlice(seeds, []uuid.UUID{}),
		ParentIds:    conv.DefaultSlice(parents, []uuid.UUID{}),
		IncludeRoots: includeRoots,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("load chat transcript nodes: %w", err)
	}
	nodes := make([]treeNode, 0, len(rows))
	nodeIDs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		nodes = append(nodes, treeNode{id: r.ID, parentID: r.ParentMessageID, continuesID: r.ContinuesMessageID, role: r.Role, synthetic: r.Synthetic, contentKey: r.ContentKey, pathKey: r.PathKey})
		nodeIDs = append(nodeIDs, r.ID)
	}
	ids := start
	if len(nodeIDs) > 0 {
		idRows, err := queries.ListChatNaturalIDsForMessages(ctx, repo.ListChatNaturalIDsForMessagesParams{ChatID: target.ChatID, ProjectID: target.ProjectID, ChatMessageIds: nodeIDs})
		if err != nil {
			return nil, nil, fmt.Errorf("load natural ids of chat transcript nodes: %w", err)
		}
		for _, r := range idRows {
			ids = append(ids, storedNaturalID{messageID: r.ChatMessageID, id: transcript.NaturalID{Kind: transcript.Kind(r.Kind), Value: r.Value, Block: int(r.BlockIndex)}})
		}
	}
	return nodes, ids, nil
}

// findIDOwners resolves the natural IDs the delivered entries carry to the
// stored entries of this chat that own them, keyed by entry index. IDs that
// stand for the start of the chat come back separately.
//
// The owners include the copies a partial compaction made of an entry: an
// inference frame after the compaction carries the plain IDs of the kept
// messages, and without their copies loaded the merge would match the
// originals in the old tree.
func findIDOwners(ctx context.Context, queries *repo.Queries, target DeliveryTarget, entries []transcript.Entry) (map[int][]idOwner, []storedNaturalID, error) {
	entriesByID := make(map[transcript.NaturalID][]int)
	params := repo.FindChatNaturalIDOwnersParams{ChatID: target.ChatID, ProjectID: target.ProjectID, Kinds: nil, NaturalValues: nil, BlockIndexes: nil}
	for i, e := range entries {
		for _, id := range e.IDs {
			if _, ok := entriesByID[id]; !ok {
				params.Kinds = append(params.Kinds, string(id.Kind))
				params.NaturalValues = append(params.NaturalValues, id.Value)
				params.BlockIndexes = append(params.BlockIndexes, int16(id.Block)) //nolint:gosec // validateNaturalID bounds the block
			}
			entriesByID[id] = append(entriesByID[id], i)
		}
	}
	if len(entriesByID) == 0 {
		return nil, nil, nil
	}
	rows, err := queries.FindChatNaturalIDOwners(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("find owners of natural ids: %w", err)
	}
	owners := make(map[int][]idOwner)
	var start []storedNaturalID
	for _, r := range rows {
		if !r.ChatMessageID.Valid {
			start = append(start, storedNaturalID{messageID: r.ChatMessageID, id: transcript.NaturalID{Kind: transcript.Kind(r.Kind), Value: r.Value, Block: int(r.BlockIndex)}})
			continue
		}
		// A copy that a partial compaction made answers for the ID it copies.
		requested := transcript.NaturalID{Kind: transcript.Kind(r.Kind), Value: r.RequestedValue, Block: int(r.BlockIndex)}
		for _, i := range entriesByID[requested] {
			if !slices.ContainsFunc(owners[i], func(o idOwner) bool { return o.id == r.ChatMessageID.UUID }) {
				owners[i] = append(owners[i], idOwner{id: r.ChatMessageID.UUID, pathKey: r.PathKey})
			}
		}
	}
	return owners, start, nil
}

// trajectorySegments cuts a trajectory into the runs the merge matches by
// content. The first run starts at the start of the chat; every entry whose
// natural ID is stored starts a new run after the entry that owns it.
func trajectorySegments(entries []transcript.Entry, owners map[int][]idOwner) []trajectorySegment {
	segments := []trajectorySegment{{from: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, keys: nil}}
	open := []int{0}
	parents := [][]byte{nil}
	for i, e := range entries {
		if anchors := owners[i]; len(anchors) > 0 {
			open, parents = open[:0], parents[:0]
			for _, a := range anchors {
				segments = append(segments, trajectorySegment{from: uuid.NullUUID{UUID: a.id, Valid: true}, keys: nil})
				open = append(open, len(segments)-1)
				parents = append(parents, a.pathKey)
			}
			continue
		}
		key := transcript.ContentKey(e)
		for j, s := range open {
			parents[j] = transcript.PathKey(parents[j], key)
			segments[s].keys = append(segments[s].keys, parents[j])
		}
	}
	return segments
}

// naturalIDBatch is a set of natural ID rows to upsert. A zero message id
// stands for the start of the chat.
type naturalIDBatch struct {
	messageIDs []uuid.UUID
	kinds      []string
	values     []string
	blocks     []int16
}

func (b *naturalIDBatch) add(messageID uuid.UUID, id transcript.NaturalID) {
	b.messageIDs = append(b.messageIDs, messageID)
	b.kinds = append(b.kinds, string(id.Kind))
	b.values = append(b.values, id.Value)
	b.blocks = append(b.blocks, int16(id.Block)) //nolint:gosec // validateNaturalID bounds the block
}

func (b *naturalIDBatch) params(target DeliveryTarget, source transcript.Source) repo.UpsertChatNaturalIDsParams {
	return repo.UpsertChatNaturalIDsParams{
		ProjectID:      target.ProjectID,
		ChatID:         target.ChatID,
		Source:         string(source),
		ChatMessageIds: b.messageIDs,
		Kinds:          b.kinds,
		NaturalValues:  b.values,
		BlockIndexes:   b.blocks,
	}
}

// deliveryPlan is everything a merge result writes.
type deliveryPlan struct {
	result DeliveryResult
	rows   []repo.CreateTranscriptChatMessageParams
	parts  []repo.CreateChatContentPartParams
	// writes are the new rows that are not synthetic: the ones that are
	// metered, announced and left for risk analysis.
	writes          []MessageWrite
	ids             naturalIDBatch
	aliases         naturalIDBatch
	cleared         []uuid.UUID
	continued       []uuid.UUID
	continuedLeaves []uuid.UUID
	// synthetic are stored rows the delivery showed to be generated history.
	synthetic []uuid.UUID
}

// planDelivery turns a merge result into rows and updates, after checking
// that every link it writes stays inside the loaded and added entries.
func planDelivery(target DeliveryTarget, write DeliveryWrite, merged transcript.Result, loaded map[uuid.UUID]struct{}, occurredAt time.Time) (deliveryPlan, error) {
	plan := deliveryPlan{
		result:          DeliveryResult{Added: nil, Seen: nil},
		rows:            nil,
		parts:           nil,
		writes:          nil,
		ids:             naturalIDBatch{messageIDs: nil, kinds: nil, values: nil, blocks: nil},
		aliases:         naturalIDBatch{messageIDs: nil, kinds: nil, values: nil, blocks: nil},
		cleared:         nil,
		continued:       nil,
		continuedLeaves: nil,
		synthetic:       nil,
	}

	known := make(map[uuid.UUID]struct{}, len(loaded)+len(merged.Added))
	for id := range loaded {
		known[id] = struct{}{}
	}
	added := make(map[uuid.UUID]struct{}, len(merged.Added))
	for _, n := range merged.Added {
		known[n.ID] = struct{}{}
		added[n.ID] = struct{}{}
	}
	inChat := func(id uuid.NullUUID) bool {
		if !id.Valid {
			return true
		}
		_, ok := known[id.UUID]
		return ok
	}

	seenAt := make(map[int]transcript.Seen, len(merged.Seen))
	for _, s := range merged.Seen {
		seenAt[s.Index] = s
	}
	next := 0
	for i := range write.Delivery.Entries {
		if s, ok := seenAt[i]; ok {
			plan.result.Seen = append(plan.result.Seen, DeliveryEntry{Index: i, MessageID: s.Node.ID})
			continue
		}
		if next >= len(merged.Added) {
			return plan, fmt.Errorf("transcript merge placed no node for entry %d", i)
		}
		n := merged.Added[next]
		next++
		if !inChat(n.ParentID()) || !inChat(n.ContinuesID()) {
			return plan, fmt.Errorf("transcript merge linked entry %d outside the loaded chat", i)
		}
		var row DeliveryRow
		if len(write.Rows) > 0 {
			row = write.Rows[i]
		}
		if err := plan.addRow(target, write.Template, row, i, n, occurredAt); err != nil {
			return plan, err
		}
		plan.result.Added = append(plan.result.Added, DeliveryEntry{Index: i, MessageID: n.ID})
	}
	if next != len(merged.Added) {
		return plan, fmt.Errorf("transcript merge added %d nodes for %d new entries", len(merged.Added), next)
	}

	for _, s := range merged.Seen {
		// A node added by this merge is written with everything it gained,
		// including a synthetic mark.
		if _, ok := added[s.Node.ID]; ok {
			continue
		}
		if s.Synthetic {
			plan.synthetic = append(plan.synthetic, s.Node.ID)
		}
		for _, id := range s.NewIDs {
			plan.ids.add(s.Node.ID, id)
		}
		if len(s.NewIDs) > 0 {
			plan.cleared = append(plan.cleared, s.Node.ID)
		}
		if s.Continues != nil {
			if _, ok := known[s.Continues.ID]; !ok {
				return plan, fmt.Errorf("transcript merge continued entry %d from outside the loaded chat", s.Index)
			}
			plan.continued = append(plan.continued, s.Node.ID)
			plan.continuedLeaves = append(plan.continuedLeaves, s.Continues.ID)
		}
	}

	// The last alias for an ID wins, so one upsert never touches a row twice.
	aliasAt := make(map[transcript.NaturalID]int, len(merged.Aliases))
	var aliasIDs []transcript.NaturalID
	var aliasTargets []uuid.UUID
	for _, a := range merged.Aliases {
		targetID := uuid.Nil
		if a.Node != nil {
			if _, ok := known[a.Node.ID]; !ok {
				return plan, fmt.Errorf("transcript merge aliased a line to an entry outside the loaded chat")
			}
			targetID = a.Node.ID
		}
		if k, ok := aliasAt[a.ID]; ok {
			aliasTargets[k] = targetID
			continue
		}
		aliasAt[a.ID] = len(aliasIDs)
		aliasIDs = append(aliasIDs, a.ID)
		aliasTargets = append(aliasTargets, targetID)
	}
	for k, id := range aliasIDs {
		plan.aliases.add(aliasTargets[k], id)
	}
	return plan, nil
}

// addRow plans the row of a new node at entry index i.
func (p *deliveryPlan) addRow(target DeliveryTarget, tmpl ExternalMessageWrite, row DeliveryRow, i int, n *transcript.Node, occurredAt time.Time) error {
	e := n.Entry
	params := tmpl.Params

	var toolCalls []byte
	if e.Call != nil {
		calls, err := json.Marshal([]transcriptToolCall{{
			ID:   conv.StripNUL(e.Call.ID),
			Type: "function",
			Function: transcriptToolFunction{
				Name:      conv.StripNUL(e.Call.Name),
				Arguments: conv.StripNUL(string(conv.DefaultSlice(e.Call.Input, []byte("{}")))),
			},
		}})
		if err != nil {
			return fmt.Errorf("encode transcript tool call: %w", err)
		}
		toolCalls = calls
	}
	toolCallID := ""
	if e.Result != nil {
		toolCallID = conv.StripNUL(e.Result.CallID)
	}
	createdAt := e.At.UTC()
	if e.At.IsZero() {
		// Keep delivery order for entries without a source time.
		createdAt = occurredAt.Add(time.Duration(i) * time.Microsecond)
	}
	model := params.Model
	if row.Model != "" {
		model = conv.ToPGText(conv.StripNUL(row.Model))
	}
	finishReason := params.FinishReason
	if row.FinishReason != "" {
		finishReason = conv.ToPGText(conv.StripNUL(row.FinishReason))
	}
	contentRaw := row.ContentRaw
	if bytes.Contains(contentRaw, jsonNULEscape) || bytes.IndexByte(contentRaw, 0) >= 0 {
		contentRaw = nil
	}
	// The sibling index deduplicates only entries that have no better key.
	var siblingKey []byte
	if len(e.IDs) == 0 {
		siblingKey = n.ContentKey
	}
	captureKind := pgtype.Text{String: "", Valid: false}
	riskAnalyzedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if e.Synthetic {
		// Generated history repeats what was scanned when it was said.
		captureKind = conv.ToPGText(CaptureSynthetic)
		riskAnalyzedAt = conv.ToPGTimestamptz(occurredAt)
	}

	stored := repo.CreateTranscriptChatMessageParams{
		ID:                 n.ID,
		ChatID:             target.ChatID,
		Role:               string(e.Role),
		ProjectID:          target.ProjectID,
		Content:            conv.StripNUL(e.Text),
		ContentRaw:         contentRaw,
		Model:              model,
		MessageID:          conv.ToPGTextEmpty(conv.StripNUL(e.MessageID)),
		ToolCallID:         conv.ToPGTextEmpty(toolCallID),
		UserID:             params.UserID,
		ExternalUserID:     pgtype.Text{String: conv.StripNUL(params.ExternalUserID.String), Valid: params.ExternalUserID.Valid},
		FinishReason:       finishReason,
		ToolCalls:          toolCalls,
		Origin:             params.Origin,
		UserAgent:          pgtype.Text{String: conv.StripNUL(params.UserAgent.String), Valid: params.UserAgent.Valid},
		IpAddress:          params.IpAddress,
		Source:             params.Source,
		CaptureKind:        captureKind,
		RiskAnalyzedAt:     riskAnalyzedAt,
		CreatedAt:          conv.ToPGTimestamptz(createdAt),
		ParentMessageID:    n.ParentID(),
		ContinuesMessageID: n.ContinuesID(),
		ContentKey:         n.ContentKey,
		ContentKeyVersion:  pgtype.Int2{Int16: transcript.ContentKeyVersion, Valid: true},
		PathKey:            n.PathKey,
		SiblingKey:         siblingKey,
	}
	p.rows = append(p.rows, stored)
	for _, id := range e.IDs {
		p.ids.add(n.ID, id)
	}
	for _, part := range row.Parts {
		part.ChatID = target.ChatID
		part.ProjectID = target.ProjectID
		part.ParentChatMessageID = uuid.NullUUID{UUID: n.ID, Valid: true}
		if !part.CreatedAt.Valid {
			part.CreatedAt = stored.CreatedAt
		}
		if e.Synthetic {
			part.RiskAnalyzedAt = riskAnalyzedAt
		}
		p.parts = append(p.parts, part)
	}
	if e.Synthetic {
		return nil
	}

	p.writes = append(p.writes, MessageWrite{
		Params: repo.CreateChatMessageParams{
			ID:               stored.ID,
			ChatID:           stored.ChatID,
			Role:             stored.Role,
			ProjectID:        stored.ProjectID,
			Content:          stored.Content,
			ContentRaw:       stored.ContentRaw,
			ContentAssetUrl:  pgtype.Text{String: "", Valid: false},
			StorageError:     pgtype.Text{String: "", Valid: false},
			Model:            stored.Model,
			MessageID:        stored.MessageID,
			ToolCallID:       stored.ToolCallID,
			UserID:           stored.UserID,
			ExternalUserID:   stored.ExternalUserID,
			FinishReason:     stored.FinishReason,
			ToolCalls:        stored.ToolCalls,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			Origin:           stored.Origin,
			UserAgent:        stored.UserAgent,
			IpAddress:        stored.IpAddress,
			Source:           stored.Source,
			ContentHash:      nil,
			Generation:       0,
			Replayed:         false,
			CreatedAt:        stored.CreatedAt,
		},
		BillingUserID:     tmpl.BillingUserID,
		AssistantID:       uuid.Nil,
		WorkloadSource:    tmpl.WorkloadSource,
		UserEmail:         tmpl.UserEmail,
		Provider:          tmpl.Provider,
		InferenceProvider: tmpl.InferenceProvider,
		Lineage:           nil,
		HookHostname:      tmpl.HookHostname,
		AccountType:       tmpl.AccountType,
		BillingMode:       tmpl.BillingMode,
	})
	return nil
}

// transcriptToolCall is the stored shape of a tool call, as the other capture
// paths write it: the input travels as a JSON string in arguments.
type transcriptToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function transcriptToolFunction `json:"function"`
}

type transcriptToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// storeDelivery writes a plan inside the delivery transaction.
func (w *ChatMessageWriter) storeDelivery(ctx context.Context, tx pgx.Tx, organizationID string, target DeliveryTarget, write DeliveryWrite, plan deliveryPlan, occurredAt time.Time) error {
	queries := repo.New(tx)
	source := write.Delivery.Source

	if len(plan.rows) > 0 {
		if _, err := queries.CreateTranscriptChatMessage(ctx, plan.rows); err != nil {
			return fmt.Errorf("create transcript chat messages: %w", err)
		}
	}
	if n := len(plan.ids.messageIDs); n > 0 {
		stored, err := queries.UpsertChatNaturalIDs(ctx, plan.ids.params(target, source))
		if err != nil {
			return fmt.Errorf("store chat natural ids: %w", err)
		}
		if len(stored) != n {
			return errNaturalIDTaken
		}
	}
	if len(plan.cleared) > 0 {
		if err := queries.ClearChatMessageSiblingKeys(ctx, repo.ClearChatMessageSiblingKeysParams{ChatID: target.ChatID, ProjectID: target.ProjectID, Ids: plan.cleared}); err != nil {
			return fmt.Errorf("clear chat message sibling keys: %w", err)
		}
	}
	if len(plan.continued) > 0 {
		if err := queries.SetChatMessageContinues(ctx, repo.SetChatMessageContinuesParams{ChatID: target.ChatID, ProjectID: target.ProjectID, Ids: plan.continued, ContinuesMessageIds: plan.continuedLeaves}); err != nil {
			return fmt.Errorf("link compaction roots: %w", err)
		}
	}
	// A row marked synthetic now was metered and announced when it was
	// stored; marking it changes nothing downstream.
	if len(plan.synthetic) > 0 {
		if err := queries.MarkChatMessagesSynthetic(ctx, repo.MarkChatMessagesSyntheticParams{ChatID: target.ChatID, ProjectID: target.ProjectID, Ids: plan.synthetic}); err != nil {
			return fmt.Errorf("mark chat messages synthetic: %w", err)
		}
	}
	// An alias never takes over an ID that names an entry; the upsert keeps
	// that row and returns nothing for it.
	if len(plan.aliases.messageIDs) > 0 {
		if _, err := queries.UpsertChatNaturalIDs(ctx, plan.aliases.params(target, source)); err != nil {
			return fmt.Errorf("store transcript aliases: %w", err)
		}
	}
	if len(plan.parts) > 0 {
		if _, err := queries.CreateChatContentPart(ctx, plan.parts); err != nil {
			return fmt.Errorf("create transcript content parts: %w", err)
		}
	}
	if len(plan.writes) == 0 {
		return nil
	}

	// Claude Tag metadata can take the project advisory lock. It runs after
	// every write above, so a merge never holds that lock while it waits on
	// another merge's rows.
	for _, mw := range plan.writes {
		if err := persistClaudeTagMetadata(ctx, tx, mw.Params); err != nil {
			return err
		}
	}
	readings, err := w.meterMessages(ctx, w.logger, organizationID, target.ProjectID, plan.writes, occurredAt)
	if err != nil {
		return err
	}
	if err := metering.Enqueue(ctx, tx, readings); err != nil {
		return fmt.Errorf("enqueue transcript chat message readings: %w", err)
	}
	observations := make([]outbox.Message, 0, len(plan.writes))
	for _, mw := range plan.writes {
		observations = append(observations, sessionObservation(target.ProjectID, mw.Params.ID, write.Template))
	}
	if _, err := outbox.PublishBatch(ctx, tx, organizationID, observations); err != nil {
		return fmt.Errorf("enqueue transcript session observations: %w", err)
	}
	return w.enqueueMessageEvents(ctx, tx, organizationID, target.ProjectID, plan.writes, occurredAt)
}
