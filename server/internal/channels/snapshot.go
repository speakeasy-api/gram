package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
)

// snapshot is the bounded bootstrap payload: one page of conversations ordered
// by activity, a recent tail of readable messages, live presence, and the
// caller's durable preferences. It is deliberately not the message archive —
// clients page older history with channels_history.
func (h *Handler) snapshot(ctx context.Context, q *repo.Queries, p Principal, acc access) (snapshotView, error) {
	page, err := h.conversationPage(ctx, q, p, acc, conversationCursor{Seq: 0, ID: uuid.Nil}, h.limits.ConversationPage)
	if err != nil {
		return snapshotView{}, err
	}
	conversations := page.rows
	conversationIDs := make([]uuid.UUID, 0, len(conversations))
	for _, row := range conversations {
		conversationIDs = append(conversationIDs, row.ChannelsConversation.ID)
	}
	byConversation, endpointsByConversation, err := h.conversationRestrictions(ctx, q, p, conversationIDs)
	if err != nil {
		return snapshotView{}, err
	}
	attribution := make([]uuid.UUID, 0, len(conversations))
	for _, endpoints := range endpointsByConversation {
		for _, endpoint := range endpoints {
			parsed, parseErr := uuid.Parse(endpoint)
			if parseErr == nil {
				attribution = append(attribution, parsed)
			}
		}
	}

	tail, hasMore, err := h.recentMessages(ctx, q, p, acc)
	if err != nil {
		return snapshotView{}, err
	}
	for _, row := range tail {
		attribution = append(attribution, row.InstanceID)
	}

	threads := make(map[string]threadView, len(conversations))
	for _, row := range conversations {
		conversation := row.ChannelsConversation
		last := lastMessageView(row)
		if last != nil {
			attribution = append(attribution, row.LastMessageInstanceID)
		}
		// Public threads carry no participant list: an absent list is what
		// marks a conversation as organization-wide.
		var threadParticipants, threadEndpoints []string
		if conversation.Private {
			threadParticipants = byConversation[conversation.ID]
			threadEndpoints = endpointsByConversation[conversation.ID]
		}
		threads[conversation.ID.String()] = newThreadView(conversation, threadParticipants, threadEndpoints, last, conversation.MessageCount)
	}

	live, err := q.ListLiveInstances(ctx, repo.ListLiveInstancesParams{
		OrganizationID: p.OrganizationID,
		ResultLimit:    int32(h.limits.PresencePage), //nolint:gosec // bounded by DefaultLimits.
	})
	if err != nil {
		return snapshotView{}, fmt.Errorf("list live channels instances: %w", err)
	}
	instances := make(map[string]instanceView, len(live))
	known := make(map[string]participantView, len(live))
	for _, row := range live {
		instances[row.ID.String()] = newInstanceView(row)
		known[row.ID.String()] = newParticipantView(row)
	}
	// Presence is live instances only, but a conversation still has to name
	// whoever spoke in it after their lease ended.
	named, err := q.ListInstancesByIDs(ctx, repo.ListInstancesByIDsParams{
		OrganizationID: p.OrganizationID,
		Ids:            uniqueIDs(attribution),
	})
	if err != nil {
		return snapshotView{}, fmt.Errorf("list channels message authors: %w", err)
	}
	for _, row := range named {
		known[row.ID.String()] = newParticipantView(row)
	}

	preferences, err := h.preferences(ctx, q, p)
	if err != nil {
		return snapshotView{}, err
	}
	// The cursor floors at the retention floor, not at zero. An organization
	// whose readable tail is empty but whose history has been trimmed would
	// otherwise hand out cursor 0, which the next forward read rejects as
	// stale — and a client that reloads the snapshot on stale would bounce
	// between the two forever.
	cursor := acc.settings.RetentionFloorSeq
	if len(tail) > 0 {
		cursor = max(cursor, tail[len(tail)-1].Seq)
	}
	view := snapshotView{
		Enabled:          acc.settings.Enabled,
		Instances:        instances,
		Participants:     known,
		Threads:          threads,
		Messages:         newMessageViews(tail),
		Preferences:      preferences,
		Cursor:           cursor,
		HasMore:          hasMore,
		ThreadsHasMore:   page.hasMore,
		ThreadsNextAfter: page.nextCursor.encode(),
		Limits:           h.limitsSummary(),
		Grants:           nil,
		Viewer:           acc.effective(p),
	}
	if p.CanAdmin {
		grants, err := h.grants(ctx, q, p)
		if err != nil {
			return snapshotView{}, err
		}
		view.Grants = grants
	}
	return view, nil
}

// recentMessages returns the newest readable messages in ascending order,
// plus whether older readable messages exist beyond them.
func (h *Handler) recentMessages(ctx context.Context, q *repo.Queries, p Principal, acc access) ([]repo.ChannelsMessage, bool, error) {
	limit := h.limits.SnapshotMessages
	rows, err := q.ListRecentVisibleMessages(ctx, repo.ListRecentVisibleMessagesParams{
		OrganizationID: p.OrganizationID,
		ReadAll:        acc.canReadAll,
		SubjectID:      p.SubjectID,
		ResultLimit:    int32(limit + 1), //nolint:gosec // bounded by DefaultLimits.
	})
	if err != nil {
		return nil, false, fmt.Errorf("list recent channels messages: %w", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	// The query returns newest first so the tail is cheap to take; clients
	// always receive ascending messages.
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, hasMore, nil
}

func (h *Handler) grants(ctx context.Context, q *repo.Queries, p Principal) (map[string]grantView, error) {
	rows, err := q.ListChannelsAccessExceptions(ctx, repo.ListChannelsAccessExceptionsParams{
		OrganizationID: p.OrganizationID,
		ResultLimit:    int32(h.limits.ExceptionPage), //nolint:gosec // bounded by DefaultLimits.
	})
	if err != nil {
		return nil, fmt.Errorf("list channels access exceptions: %w", err)
	}
	grants := make(map[string]grantView, len(rows))
	for _, row := range rows {
		grants[row.SubjectID] = grantView{CanConnect: row.CanConnect, CanReadAll: row.CanReadAll}
	}
	return grants, nil
}

// administrativeSnapshot lets an administrator inspect configuration while
// Channels is disabled or their own connect permission is withheld. It carries
// configuration only: no presence, no conversations, no messages.
func (h *Handler) administrativeSnapshot(ctx context.Context, q *repo.Queries, p Principal, acc access) (snapshotView, error) {
	grants, err := h.grants(ctx, q, p)
	if err != nil {
		return snapshotView{}, err
	}
	return snapshotView{
		Enabled:          acc.settings.Enabled,
		Instances:        map[string]instanceView{},
		Participants:     map[string]participantView{},
		Threads:          map[string]threadView{},
		Messages:         []messageView{},
		Preferences:      preferencesView{Followed: []string{}, Muted: []string{}},
		Cursor:           0,
		HasMore:          false,
		ThreadsHasMore:   false,
		ThreadsNextAfter: "",
		Limits:           h.limitsSummary(),
		Grants:           grants,
		Viewer:           acc.effective(p),
	}, nil
}

// administer applies an organization-level Channels control change. It is the
// only operation an administrator may run while Channels is disabled, and it
// never grants administration or private-conversation participation.
func (h *Handler) administer(ctx context.Context, tx pgx.Tx, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	if !p.CanAdmin {
		return nil, fmt.Errorf("%w: channels administration permission required", ErrDenied)
	}
	event := AdminAuditEvent{
		OrganizationID:  p.OrganizationID,
		ActorSubjectID:  p.SubjectID,
		TargetSubjectID: a.SubjectID,
		EnabledBefore:   nil,
		EnabledAfter:    nil,
		CanConnect:      nil,
		CanReadAll:      nil,
		Removed:         false,
	}
	changed := false
	if a.Enabled != nil {
		before := acc.settings.Enabled
		updated, err := q.SetChannelsEnabled(ctx, repo.SetChannelsEnabledParams{
			OrganizationID: p.OrganizationID,
			Enabled:        *a.Enabled,
		})
		if err != nil {
			return nil, fmt.Errorf("set channels availability: %w", err)
		}
		acc.settings = updated
		event.EnabledBefore, event.EnabledAfter = &before, &updated.Enabled
		changed = true
	}
	if a.SubjectID != "" {
		transition, err := h.applyAccessException(ctx, q, p, acc, a)
		if err != nil {
			return nil, err
		}
		event.CanConnect, event.CanReadAll, event.Removed = transition.canConnect, transition.canReadAll, a.Remove
		changed = true
		// The returned snapshot describes the caller, so a change to the
		// caller's own exception takes effect in it at once.
		if a.SubjectID == p.SubjectID {
			acc.canConnect, acc.canReadAll = transition.canConnect.After, transition.canReadAll.After
			if a.Remove {
				acc.canConnect, acc.canReadAll = p.CanConnect, p.CanReadAll
			}
		}
	}
	if changed && h.adminAudit != nil {
		if err := h.adminAudit(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("record channels administration audit: %w", err)
		}
	}
	if acc.permitted() {
		return h.snapshot(ctx, q, p, acc)
	}
	return h.administrativeSnapshot(ctx, q, p, acc)
}

type accessTransition struct {
	canConnect *AdminAuditChange
	canReadAll *AdminAuditChange
}

func (h *Handler) applyAccessException(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (accessTransition, error) {
	existing, err := q.GetChannelsAccessException(ctx, repo.GetChannelsAccessExceptionParams{
		OrganizationID: p.OrganizationID,
		SubjectID:      a.SubjectID,
	})
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return accessTransition{}, fmt.Errorf("load channels access exception: %w", err)
	}
	before := grantView{CanConnect: false, CanReadAll: false}
	switch {
	case found:
		before = grantView{CanConnect: existing.CanConnect, CanReadAll: existing.CanReadAll}
	case a.SubjectID == p.SubjectID:
		// Writing the first exception for yourself preserves the permissions
		// you already hold natively, so an administrator cannot lock
		// themselves out by toggling one field.
		before = grantView{CanConnect: acc.canConnect, CanReadAll: acc.canReadAll}
	}
	if a.Remove {
		if err := q.DeleteChannelsAccessException(ctx, repo.DeleteChannelsAccessExceptionParams{
			OrganizationID: p.OrganizationID,
			SubjectID:      a.SubjectID,
		}); err != nil {
			return accessTransition{}, fmt.Errorf("remove channels access exception: %w", err)
		}
		return accessTransition{
			canConnect: &AdminAuditChange{Before: before.CanConnect, After: false},
			canReadAll: &AdminAuditChange{Before: before.CanReadAll, After: false},
		}, nil
	}
	after := before
	if a.CanConnect != nil {
		after.CanConnect = *a.CanConnect
	}
	if a.CanReadAll != nil {
		after.CanReadAll = *a.CanReadAll
	}
	if _, err := q.UpsertChannelsAccessException(ctx, repo.UpsertChannelsAccessExceptionParams{
		OrganizationID: p.OrganizationID,
		ProjectID:      acc.settings.ProjectID,
		SubjectID:      a.SubjectID,
		CanConnect:     after.CanConnect,
		CanReadAll:     after.CanReadAll,
	}); err != nil {
		return accessTransition{}, fmt.Errorf("save channels access exception: %w", err)
	}
	// The snapshot lists exceptions in one bounded page, so the cap is what
	// keeps that list complete. It is checked after the write so updating an
	// existing exception never counts twice; the refusal rolls it back.
	held, err := q.CountChannelsAccessExceptions(ctx, p.OrganizationID)
	if err != nil {
		return accessTransition{}, fmt.Errorf("count channels access exceptions: %w", err)
	}
	if held > int64(h.limits.ExceptionPage) {
		return accessTransition{}, fmt.Errorf("%w: at most %d Channels access exceptions are supported", ErrInvalid, h.limits.ExceptionPage)
	}
	return accessTransition{
		canConnect: &AdminAuditChange{Before: before.CanConnect, After: after.CanConnect},
		canReadAll: &AdminAuditChange{Before: before.CanReadAll, After: after.CanReadAll},
	}, nil
}

// conversationCursor is an opaque keyset position in the conversation list.
// It is the activity sequence and identifier of the last row a caller saw,
// which is exactly the ordering channels_conversations_activity_idx provides.
type conversationCursor struct {
	Seq int64     `json:"seq"`
	ID  uuid.UUID `json:"id"`
}

func (c conversationCursor) zero() bool { return c.ID == uuid.Nil && c.Seq == 0 }

func (c conversationCursor) encode() string {
	if c.zero() {
		return ""
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeConversationCursor(value string) (conversationCursor, error) {
	if value == "" {
		return conversationCursor{Seq: 0, ID: uuid.Nil}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return conversationCursor{}, fmt.Errorf("%w: invalid conversation cursor", ErrInvalid)
	}
	var decoded conversationCursor
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return conversationCursor{}, fmt.Errorf("%w: invalid conversation cursor", ErrInvalid)
	}
	return decoded, nil
}

type conversationPage struct {
	rows       []repo.ListVisibleConversationsRow
	hasMore    bool
	nextCursor conversationCursor
}

// conversationPage reads one bounded keyset page of readable conversations.
func (h *Handler) conversationPage(ctx context.Context, q *repo.Queries, p Principal, acc access, after conversationCursor, limit int) (conversationPage, error) {
	rows, err := q.ListVisibleConversations(ctx, repo.ListVisibleConversationsParams{
		OrganizationID: p.OrganizationID,
		FromStart:      after.zero(),
		AfterSeq:       after.Seq,
		AfterID:        after.ID,
		ReadAll:        acc.canReadAll,
		SubjectID:      p.SubjectID,
		ResultLimit:    int32(limit + 1), //nolint:gosec // bounded by DefaultLimits.
	})
	if err != nil {
		return conversationPage{}, fmt.Errorf("list channels conversations: %w", err)
	}
	page := conversationPage{rows: rows, hasMore: len(rows) > limit, nextCursor: conversationCursor{Seq: 0, ID: uuid.Nil}}
	if page.hasMore {
		page.rows = rows[:limit]
	}
	if page.hasMore && len(page.rows) > 0 {
		last := page.rows[len(page.rows)-1].ChannelsConversation
		page.nextCursor = conversationCursor{Seq: last.LastMessageSeq, ID: last.ID}
	}
	return page, nil
}

// listConversations is the discovery operation. The snapshot carries only the
// first page, so a client with more conversations than fit in one page reaches
// the rest here instead of silently never seeing them.
func (h *Handler) listConversations(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	after, err := decodeConversationCursor(a.Cursor2)
	if err != nil {
		return nil, err
	}
	limit := h.pageSize(a.Limit, h.limits.ConversationPage)
	page, err := h.conversationPage(ctx, q, p, acc, after, limit)
	if err != nil {
		return nil, err
	}
	conversationIDs := make([]uuid.UUID, 0, len(page.rows))
	for _, row := range page.rows {
		conversationIDs = append(conversationIDs, row.ChannelsConversation.ID)
	}
	participants, endpoints, err := h.conversationRestrictions(ctx, q, p, conversationIDs)
	if err != nil {
		return nil, err
	}
	threads := make(map[string]threadView, len(page.rows))
	for _, row := range page.rows {
		conversation := row.ChannelsConversation
		var threadParticipants, threadEndpoints []string
		if conversation.Private {
			threadParticipants = participants[conversation.ID]
			threadEndpoints = endpoints[conversation.ID]
		}
		threads[conversation.ID.String()] = newThreadView(conversation, threadParticipants, threadEndpoints, lastMessageView(row), conversation.MessageCount)
	}
	return conversationsView{
		Threads:   threads,
		HasMore:   page.hasMore,
		NextAfter: page.nextCursor.encode(),
	}, nil
}

// conversationRestrictions loads the participants and addressed endpoints of a
// page of conversations in two queries rather than two per conversation.
func (h *Handler) conversationRestrictions(ctx context.Context, q *repo.Queries, p Principal, ids []uuid.UUID) (map[uuid.UUID][]string, map[uuid.UUID][]string, error) {
	participants, err := q.ListParticipantsForConversations(ctx, repo.ListParticipantsForConversationsParams{
		OrganizationID:  p.OrganizationID,
		ConversationIds: ids,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list channels participants: %w", err)
	}
	endpoints, err := q.ListEndpointsForConversations(ctx, repo.ListEndpointsForConversationsParams{
		OrganizationID:  p.OrganizationID,
		ConversationIds: ids,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list channels endpoints: %w", err)
	}
	byConversation := map[uuid.UUID][]string{}
	for _, row := range participants {
		byConversation[row.ConversationID] = append(byConversation[row.ConversationID], row.SubjectID)
	}
	endpointsByConversation := map[uuid.UUID][]string{}
	for _, row := range endpoints {
		endpointsByConversation[row.ConversationID] = append(endpointsByConversation[row.ConversationID], row.InstanceID.String())
	}
	return byConversation, endpointsByConversation, nil
}

func lastMessageView(row repo.ListVisibleConversationsRow) *messageView {
	if row.LastMessageSeqValue == 0 {
		return nil
	}
	return &messageView{
		ID:             row.LastMessageSeqValue,
		ThreadID:       row.ChannelsConversation.ID.String(),
		InstanceID:     row.LastMessageInstanceID.String(),
		SubjectID:      row.LastMessageSubjectID,
		Text:           row.LastMessageBody,
		IdempotencyKey: row.LastMessageIdempotencyKey,
		CreatedAt:      row.LastMessageCreatedAt.Time.UTC(),
	}
}
