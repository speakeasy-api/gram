package channels

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
)

// The views below are the wire contract. Field names and meanings are stable:
// clients persist `cursor` and message `id` across reconnects.

type instanceView struct {
	ID        string    `json:"id"`
	SubjectID string    `json:"subjectId"`
	Label     string    `json:"label"`
	Note      string    `json:"note"`
	Origin    string    `json:"origin,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// participantView names whoever spoke in a conversation, including after their
// lease ended. It is display identity only: no note, no secret, and no claim
// of being connected.
type participantView struct {
	ID        string `json:"id"`
	SubjectID string `json:"subjectId"`
	Label     string `json:"label"`
	Origin    string `json:"origin,omitempty"`
}

type messageView struct {
	ID             int64     `json:"id"`
	ThreadID       string    `json:"threadId"`
	InstanceID     string    `json:"instanceId"`
	SubjectID      string    `json:"subjectId"`
	Text           string    `json:"text"`
	IdempotencyKey string    `json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
}

type threadView struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	ParentID     string       `json:"parentId,omitempty"`
	Participants []string     `json:"participants,omitempty"`
	InstanceIDs  []string     `json:"instanceIds,omitempty"`
	LastMessage  *messageView `json:"lastMessage,omitempty"`
	MessageCount int64        `json:"messageCount"`
}

type preferencesView struct {
	Followed []string `json:"followed"`
	Muted    []string `json:"muted"`
}

type grantView struct {
	CanConnect bool `json:"canConnect"`
	CanReadAll bool `json:"canReadAll"`
}

// limitsView publishes the client-visible bounds so no client hardcodes them.
type limitsView struct {
	MessageBytes     int `json:"messageBytes"`
	TitleBytes       int `json:"titleBytes"`
	NoteBytes        int `json:"noteBytes"`
	LabelBytes       int `json:"labelBytes"`
	HistoryLimit     int `json:"historyLimit"`
	SnapshotMessages int `json:"snapshotMessages"`
}

type snapshotView struct {
	Enabled      bool                       `json:"enabled"`
	Instances    map[string]instanceView    `json:"instances"`
	Participants map[string]participantView `json:"participants"`
	Threads      map[string]threadView      `json:"threads"`
	Messages     []messageView              `json:"messages"`
	Preferences  preferencesView            `json:"preferences"`
	Cursor       int64                      `json:"cursor"`
	HasMore      bool                       `json:"hasMore"`
	// ThreadsHasMore reports that the organization has more conversations
	// than this payload carries. ThreadsNextAfter is the opaque cursor to
	// pass to channels_conversations to reach them.
	ThreadsHasMore   bool       `json:"threadsHasMore"`
	ThreadsNextAfter string     `json:"threadsNextAfter,omitempty"`
	Limits           limitsView `json:"limits"`
	// Grants is present only for an administrator, and present even when
	// empty, so a client can tell "no exceptions" from "not an administrator".
	Grants map[string]grantView `json:"grants,omitzero"`

	// Viewer is the requesting identity's own effective Channels access.
	Viewer EffectiveAccess `json:"viewer"`
}

// conversationsView is one keyset page of conversation discovery.
type conversationsView struct {
	Threads   map[string]threadView `json:"threads"`
	HasMore   bool                  `json:"hasMore"`
	NextAfter string                `json:"nextAfter,omitempty"`
}

type readView struct {
	Messages []messageView `json:"messages"`
	Cursor   int64         `json:"cursor"`
	HasMore  bool          `json:"hasMore"`
	Stale    bool          `json:"stale"`
}

type historyView struct {
	Messages   []messageView `json:"messages"`
	Cursor     int64         `json:"cursor"`
	HasMore    bool          `json:"hasMore"`
	NextBefore int64         `json:"nextBefore,omitempty"`
	Stale      bool          `json:"stale"`
}

// connectView is the result of opening, resuming or reclaiming an instance.
// resumeToken is present exactly once, when a new agent instance is created.
type connectView struct {
	InstanceID  string    `json:"instanceId"`
	ID          string    `json:"id"`
	SubjectID   string    `json:"subjectId"`
	Label       string    `json:"label"`
	Note        string    `json:"note"`
	Origin      string    `json:"origin,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	ResumeToken string    `json:"resumeToken,omitempty"`
}

func newInstanceView(row repo.ChannelsInstance) instanceView {
	return instanceView{
		ID:        row.ID.String(),
		SubjectID: row.SubjectID,
		Label:     row.Label,
		Note:      row.Note,
		Origin:    row.Origin,
		ExpiresAt: row.ExpiresAt.Time.UTC(),
	}
}

func newParticipantView(row repo.ChannelsInstance) participantView {
	return participantView{
		ID:        row.ID.String(),
		SubjectID: row.SubjectID,
		Label:     row.Label,
		Origin:    row.Origin,
	}
}

func newMessageView(row repo.ChannelsMessage) messageView {
	return messageView{
		ID:             row.Seq,
		ThreadID:       row.ConversationID.String(),
		InstanceID:     row.InstanceID.String(),
		SubjectID:      row.SubjectID,
		Text:           row.Body,
		IdempotencyKey: row.IdempotencyKey,
		CreatedAt:      row.CreatedAt.Time.UTC(),
	}
}

func newMessageViews(rows []repo.ChannelsMessage) []messageView {
	out := make([]messageView, 0, len(rows))
	for _, row := range rows {
		out = append(out, newMessageView(row))
	}
	return out
}

func newConnectView(row repo.ChannelsInstance, resumeToken string) connectView {
	return connectView{
		InstanceID:  row.ID.String(),
		ID:          row.ID.String(),
		SubjectID:   row.SubjectID,
		Label:       row.Label,
		Note:        row.Note,
		Origin:      row.Origin,
		ExpiresAt:   row.ExpiresAt.Time.UTC(),
		ResumeToken: resumeToken,
	}
}

func newThreadView(row repo.ChannelsConversation, participants, endpoints []string, last *messageView, messages int64) threadView {
	parent := ""
	if row.ParentID.Valid {
		parent = row.ParentID.UUID.String()
	}
	return threadView{
		ID:           row.ID.String(),
		Title:        row.Title,
		ParentID:     parent,
		Participants: participants,
		InstanceIDs:  endpoints,
		LastMessage:  last,
		MessageCount: messages,
	}
}

func (h *Handler) limitsSummary() limitsView {
	return limitsView{
		MessageBytes:     h.limits.MessageBytes,
		TitleBytes:       h.limits.TitleBytes,
		NoteBytes:        h.limits.NoteBytes,
		LabelBytes:       h.limits.LabelBytes,
		HistoryLimit:     h.limits.HistoryLimit,
		SnapshotMessages: h.limits.SnapshotMessages,
	}
}

// uniqueIDs collects identifiers in first-seen order, which keeps the instance
// fetch for attribution bounded by the page it serves.
func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
