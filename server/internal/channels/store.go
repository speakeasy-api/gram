package channels

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// arguments is the union of every operation's input. Surface is stamped by the
// handler from the receiving transport and is deliberately not decodable from
// a request body.
type arguments struct {
	Surface             string  `json:"-"`
	InstanceID          string  `json:"instanceId"`
	ResumeToken         string  `json:"resumeToken"`
	Label               string  `json:"label"`
	Note                *string `json:"note"`
	LeaseSeconds        int     `json:"leaseSeconds"`
	SubjectID           string  `json:"subjectId"`
	RecipientInstanceID string  `json:"recipientInstanceId"`
	ThreadID            string  `json:"threadId"`
	ParentID            string  `json:"parentId"`
	Title               string  `json:"title"`
	Text                string  `json:"text"`
	IdempotencyKey      string  `json:"idempotencyKey"`
	Cursor              int64   `json:"cursor"`
	// Cursor2 is the opaque conversation-discovery cursor. It is a separate
	// field from Cursor because that one is a message sequence and these two
	// cursors are not interchangeable.
	Cursor2    string          `json:"after"`
	Before     int64           `json:"before"`
	Limit      int             `json:"limit"`
	Follow     *bool           `json:"follow"`
	Muted      json.RawMessage `json:"muted"`
	Followed   []string        `json:"followed"`
	Enabled    *bool           `json:"enabled"`
	CanConnect *bool           `json:"canConnect"`
	CanReadAll *bool           `json:"canReadAll"`
	Remove     bool            `json:"remove"`
}

// noArguments is the input of an operation that takes none.
var noArguments arguments

// access is the caller's effective Channels position: the organization's
// settings plus the permissions actually in force, after an explicit access
// exception overrides the organization's native defaults.
type access struct {
	settings   repo.ChannelsSetting
	canConnect bool
	canReadAll bool
}

func (a access) permitted() bool { return a.settings.Enabled && a.canConnect }

// writeOperations are the operations a write burst limit applies to. Reads are
// bounded by page size instead.
var writeOperations = map[string]bool{
	"channels_connect":       true,
	"channels_dm":            true,
	"channels_create_thread": true,
	"channels_post":          true,
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func noteValue(note *string) string {
	if note == nil {
		return ""
	}
	return *note
}

// dashboardLabel names a browser session after the account that holds it. The
// name comes from the authenticated session, not from the request body, so one
// member cannot present themselves to the organization as another.
func dashboardLabel(p Principal) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, p.SubjectName)
	if at := strings.IndexByte(name, '@'); at > 0 {
		name = name[:at]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "member"
	}
	if limit := 128 - len(dashboardSuffix); len(name) > limit {
		name = strings.ToValidUTF8(name[:limit], "")
	}
	return name + dashboardSuffix
}

func (h *Handler) leaseUntil(seconds int) pgtype.Timestamptz {
	window := h.limits.LeaseDefault
	if seconds > 0 {
		// Clamp in seconds before converting, so an oversized request cannot
		// overflow the duration into an already-expired lease.
		window = time.Duration(min(int64(seconds), int64(h.limits.LeaseMax/time.Second))) * time.Second
	}
	window = min(window, h.limits.LeaseMax)
	return conv.ToPGTimestamptz(time.Now().UTC().Add(window))
}

func (h *Handler) pageSize(requested, fallback int) int {
	if requested <= 0 || requested > h.limits.HistoryLimit {
		return fallback
	}
	return requested
}

// operate runs one operation in its own transaction. Authorization is loaded
// inside that transaction from live storage, never from an admission-time copy.
func (h *Handler) operate(ctx context.Context, p Principal, op string, a arguments) (any, error) {
	if h.db == nil {
		return nil, fmt.Errorf("%w: no database", ErrUnavailable)
	}
	if writeOperations[op] && !h.writes.allow(p.OrganizationID+"\x00"+p.SubjectID, time.Now()) {
		return nil, fmt.Errorf("%w: slow down and retry with the same idempotency key", ErrLimited)
	}
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin channels transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := h.repo.WithTx(tx)
	acc, err := loadAccess(ctx, q, p)
	if err != nil {
		return nil, err
	}
	result, err := h.dispatch(ctx, tx, q, p, acc, op, a)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit channels transaction: %w", err)
	}
	return result, nil
}

// loadAccess reads the organization's settings, creating them under the
// canonical default project on first use, and resolves the caller's effective
// permissions.
func loadAccess(ctx context.Context, q *repo.Queries, p Principal) (access, error) {
	settings, err := q.GetChannelsSettings(ctx, p.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		// First use in this organization. Storage ownership is resolved here,
		// once, to the organization's canonical default project. The insert
		// returns no row both when the organization has no project and when a
		// concurrent first use won the insert, so the row is read again by a
		// fresh statement that sees the winner's commit.
		if _, err := q.EnsureChannelsSettings(ctx, p.OrganizationID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return access{}, fmt.Errorf("create channels settings: %w", err)
		}
		settings, err = q.GetChannelsSettings(ctx, p.OrganizationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return access{}, fmt.Errorf("%w: organization has no project to own channels data", ErrUnavailable)
		}
	}
	if err != nil {
		return access{}, fmt.Errorf("load channels settings: %w", err)
	}
	acc := access{settings: settings, canConnect: p.CanConnect, canReadAll: p.CanReadAll}
	exception, err := q.GetChannelsAccessException(ctx, repo.GetChannelsAccessExceptionParams{
		OrganizationID: p.OrganizationID,
		SubjectID:      p.SubjectID,
	})
	switch {
	case err == nil:
		acc.canConnect = exception.CanConnect
		acc.canReadAll = exception.CanReadAll
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return access{}, fmt.Errorf("load channels access exception: %w", err)
	}
	return acc, nil
}

func (h *Handler) dispatch(ctx context.Context, tx pgx.Tx, q *repo.Queries, p Principal, acc access, op string, a arguments) (any, error) {
	if op == "channels_admin" {
		return h.administer(ctx, tx, q, p, acc, a)
	}
	if !acc.permitted() {
		// An administrator may still inspect configuration while Channels is
		// disabled or their own connect permission is withheld. They receive no
		// presence and no messages in that state.
		if op == "snapshot" && p.CanAdmin {
			return h.administrativeSnapshot(ctx, q, p, acc)
		}
		return nil, fmt.Errorf("%w: channels is disabled or connect permission is withheld", ErrDenied)
	}
	if len(a.Label) > h.limits.LabelBytes || len(noteValue(a.Note)) > h.limits.NoteBytes || len(a.Title) > h.limits.TitleBytes {
		return nil, fmt.Errorf("%w: label, note or title exceeds its size limit", ErrInvalid)
	}
	switch op {
	case "snapshot":
		return h.snapshot(ctx, q, p, acc)
	case "channels_connect":
		return h.connect(ctx, q, p, acc, a)
	case "channels_dashboard_session":
		return h.dashboardSession(ctx, q, p, acc, a)
	case "channels_presence":
		return h.presence(ctx, q, p, acc, a)
	case "channels_dm":
		return h.directConversation(ctx, q, p, acc, a)
	case "channels_create_thread":
		return h.createThread(ctx, q, p, acc, a)
	case "channels_post":
		return h.post(ctx, q, p, acc, a)
	case "channels_conversations":
		return h.listConversations(ctx, q, p, acc, a)
	case "channels_read":
		return h.read(ctx, q, p, acc, a)
	case "channels_history":
		return h.history(ctx, q, p, acc, a)
	case "channels_follow":
		return h.follow(ctx, q, p, acc, a)
	case "channels_preferences":
		return h.replacePreferences(ctx, q, p, acc, a)
	default:
		return nil, fmt.Errorf("%w: unknown operation %q", ErrInvalid, op)
	}
}

func publishHint(ctx context.Context, q *repo.Queries, organizationID, kind string, revision int64) error {
	payload, err := json.Marshal(hint{Organization: organizationID, Kind: kind, Revision: revision})
	if err != nil {
		return fmt.Errorf("encode channels delivery hint: %w", err)
	}
	if err := q.PublishChannelsEvent(ctx, string(payload)); err != nil {
		return fmt.Errorf("publish channels delivery hint: %w", err)
	}
	return nil
}

// ownedInstance resolves an active instance belonging to the caller's identity.
func (h *Handler) ownedInstance(ctx context.Context, q *repo.Queries, p Principal, id string) (repo.ChannelsInstance, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return repo.ChannelsInstance{}, fmt.Errorf("%w: active owned instance required", ErrDenied)
	}
	row, err := q.GetInstance(ctx, repo.GetInstanceParams{OrganizationID: p.OrganizationID, ID: parsed})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.ChannelsInstance{}, fmt.Errorf("%w: active owned instance required", ErrDenied)
	case err != nil:
		return repo.ChannelsInstance{}, fmt.Errorf("load channels instance: %w", err)
	}
	if row.SubjectID != p.SubjectID || !row.ExpiresAt.Time.After(time.Now().UTC()) {
		return repo.ChannelsInstance{}, fmt.Errorf("%w: active owned instance required", ErrDenied)
	}
	return row, nil
}

// readableConversation resolves a conversation the caller may read. Read-all
// observers can reach a private conversation here but not through
// writableConversation: observation never becomes participation.
func (h *Handler) readableConversation(ctx context.Context, q *repo.Queries, p Principal, acc access, id string) (repo.ChannelsConversation, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return repo.ChannelsConversation{}, fmt.Errorf("%w: conversation not found", ErrDenied)
	}
	row, err := q.GetConversation(ctx, repo.GetConversationParams{OrganizationID: p.OrganizationID, ID: parsed})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.ChannelsConversation{}, fmt.Errorf("%w: conversation not found", ErrDenied)
	case err != nil:
		return repo.ChannelsConversation{}, fmt.Errorf("load channels conversation: %w", err)
	}
	if !row.Private || acc.canReadAll {
		return row, nil
	}
	member, err := q.IsConversationParticipant(ctx, repo.IsConversationParticipantParams{
		OrganizationID: p.OrganizationID,
		ConversationID: parsed,
		SubjectID:      p.SubjectID,
	})
	if err != nil {
		return repo.ChannelsConversation{}, fmt.Errorf("check channels conversation participation: %w", err)
	}
	if !member {
		return repo.ChannelsConversation{}, fmt.Errorf("%w: conversation not found", ErrDenied)
	}
	return row, nil
}

func (h *Handler) writableConversation(ctx context.Context, q *repo.Queries, p Principal, acc access, id string) (repo.ChannelsConversation, error) {
	row, err := h.readableConversation(ctx, q, p, acc, id)
	if err != nil {
		return row, err
	}
	if !row.Private {
		return row, nil
	}
	member, err := q.IsConversationParticipant(ctx, repo.IsConversationParticipantParams{
		OrganizationID: p.OrganizationID,
		ConversationID: row.ID,
		SubjectID:      p.SubjectID,
	})
	if err != nil {
		return repo.ChannelsConversation{}, fmt.Errorf("check channels conversation participation: %w", err)
	}
	if !member {
		return repo.ChannelsConversation{}, fmt.Errorf("%w: writing to this conversation requires participating in it", ErrDenied)
	}
	return row, nil
}

func (h *Handler) connect(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	if a.InstanceID != "" {
		return h.resume(ctx, q, p, a)
	}
	if err := h.guardInstanceCount(ctx, q, p); err != nil {
		return nil, err
	}
	announce, err := q.AllocateAnnounceSeq(ctx, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("allocate channels announcement: %w", err)
	}
	token := identifier()
	row, err := q.CreateInstance(ctx, repo.CreateInstanceParams{
		OrganizationID: p.OrganizationID,
		ProjectID:      acc.settings.ProjectID,
		SubjectID:      p.SubjectID,
		Label:          a.Label,
		Note:           noteValue(a.Note),
		Origin:         "",
		ResumeHash:     conv.ToPGText(hashToken(token)),
		AnnounceSeq:    announce,
		ExpiresAt:      h.leaseUntil(a.LeaseSeconds),
	})
	if err != nil {
		return nil, fmt.Errorf("create channels instance: %w", err)
	}
	if err := publishHint(ctx, q, p.OrganizationID, hintPresence, announce); err != nil {
		return nil, err
	}
	return newConnectView(row, token), nil
}

// resume renews an owned instance against its stored secret hash. A dashboard
// session stores no hash, so no token can ever resume one.
func (h *Handler) resume(ctx context.Context, q *repo.Queries, p Principal, a arguments) (any, error) {
	denied := fmt.Errorf("%w: invalid instance resume", ErrDenied)
	parsed, err := uuid.Parse(a.InstanceID)
	if err != nil {
		return nil, denied
	}
	row, err := q.GetInstance(ctx, repo.GetInstanceParams{OrganizationID: p.OrganizationID, ID: parsed})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, denied
	case err != nil:
		return nil, fmt.Errorf("load channels instance: %w", err)
	}
	if row.SubjectID != p.SubjectID || !row.ResumeHash.Valid || a.ResumeToken == "" {
		return nil, denied
	}
	if subtle.ConstantTimeCompare([]byte(row.ResumeHash.String), []byte(hashToken(a.ResumeToken))) != 1 {
		return nil, denied
	}
	// An expired instance is outside the live set, so bringing it back adds a
	// live lease exactly as a new connection does and is held to the same cap.
	if !row.ExpiresAt.Time.After(time.Now().UTC()) {
		if err := h.guardInstanceCount(ctx, q, p); err != nil {
			return nil, err
		}
	}
	// Renewal keeps the announcement sequence, so resuming never repeats an
	// introduction, and no replacement secret is ever issued.
	renewed, err := q.RenewInstance(ctx, repo.RenewInstanceParams{
		OrganizationID: p.OrganizationID,
		ID:             row.ID,
		SubjectID:      p.SubjectID,
		ExpiresAt:      h.leaseUntil(a.LeaseSeconds),
		ReplaceNote:    a.Note != nil,
		Note:           noteValue(a.Note),
	})
	if err != nil {
		return nil, fmt.Errorf("renew channels instance: %w", err)
	}
	return newConnectView(renewed, ""), nil
}

// dashboardSession reclaims this account's browser session or opens one. The
// partial unique index resolves simultaneous first sends and reloads, so the
// organization is never locked to keep one session per account.
func (h *Handler) dashboardSession(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	if a.Surface != surfaceDashboard {
		return nil, fmt.Errorf("%w: dashboard sessions belong to the dashboard", ErrDenied)
	}
	announce := int64(0)
	existing, err := q.GetDashboardInstance(ctx, repo.GetDashboardInstanceParams{
		OrganizationID: p.OrganizationID,
		SubjectID:      p.SubjectID,
	})
	switch {
	case err == nil:
		announce = existing.AnnounceSeq
		// Reclaiming an expired session adds a live lease, so it is held to
		// the same cap as opening one.
		if !existing.ExpiresAt.Time.After(time.Now().UTC()) {
			if err := h.guardInstanceCount(ctx, q, p); err != nil {
				return nil, err
			}
		}
	case errors.Is(err, pgx.ErrNoRows):
		if err := h.guardInstanceCount(ctx, q, p); err != nil {
			return nil, err
		}
		announce, err = q.AllocateAnnounceSeq(ctx, p.OrganizationID)
		if err != nil {
			return nil, fmt.Errorf("allocate channels announcement: %w", err)
		}
	default:
		return nil, fmt.Errorf("load channels dashboard session: %w", err)
	}
	row, err := q.UpsertDashboardInstance(ctx, repo.UpsertDashboardInstanceParams{
		OrganizationID: p.OrganizationID,
		ProjectID:      acc.settings.ProjectID,
		SubjectID:      p.SubjectID,
		Label:          dashboardLabel(p),
		Note:           noteValue(a.Note),
		AnnounceSeq:    announce,
		ExpiresAt:      h.leaseUntil(a.LeaseSeconds),
		ReplaceNote:    a.Note != nil,
	})
	if err != nil {
		return nil, fmt.Errorf("open channels dashboard session: %w", err)
	}
	// Only a session that did not exist yet is an announcement. Re-entering
	// the dashboard renews the same session silently.
	if existing.ID == uuid.Nil {
		if err := publishHint(ctx, q, p.OrganizationID, hintPresence, row.AnnounceSeq); err != nil {
			return nil, err
		}
	}
	return newConnectView(row, ""), nil
}

// guardInstanceCount refuses a new live lease once the organization holds its
// cap. It locks the organization's settings row first, so concurrent connects
// count and create one at a time and cannot overshoot the cap together; the
// lock is held until the calling transaction ends.
func (h *Handler) guardInstanceCount(ctx context.Context, q *repo.Queries, p Principal) error {
	if _, err := q.LockChannelsSettings(ctx, p.OrganizationID); err != nil {
		return fmt.Errorf("lock channels settings: %w", err)
	}
	live, err := q.CountLiveInstances(ctx, p.OrganizationID)
	if err != nil {
		return fmt.Errorf("count live channels instances: %w", err)
	}
	if live >= int64(h.limits.ActiveInstances) {
		return fmt.Errorf("%w: this organization already holds %d active channels instances", ErrLimited, live)
	}
	return nil
}

func (h *Handler) presence(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	if a.InstanceID != "" {
		row, err := h.ownedInstance(ctx, q, p, a.InstanceID)
		if err != nil {
			return nil, err
		}
		if _, err := q.RenewInstance(ctx, repo.RenewInstanceParams{
			OrganizationID: p.OrganizationID,
			ID:             row.ID,
			SubjectID:      p.SubjectID,
			ExpiresAt:      h.leaseUntil(a.LeaseSeconds),
			ReplaceNote:    a.Note != nil,
			Note:           noteValue(a.Note),
		}); err != nil {
			return nil, fmt.Errorf("renew channels instance: %w", err)
		}
	}
	return h.snapshot(ctx, q, p, acc)
}

// endpointPairKey identifies a direct conversation by its two endpoints,
// ordered so either sender reuses the same conversation.
func endpointPairKey(left, right uuid.UUID) string {
	first, second := left.String(), right.String()
	if first > second {
		first, second = second, first
	}
	return first + ":" + second
}

func (h *Handler) directConversation(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	sender, err := h.ownedInstance(ctx, q, p, a.InstanceID)
	if err != nil {
		return nil, err
	}
	if a.ParentID != "" {
		return nil, fmt.Errorf("%w: a direct conversation cannot specify parentId", ErrInvalid)
	}
	recipientID, err := uuid.Parse(a.RecipientInstanceID)
	if err != nil {
		return nil, fmt.Errorf("%w: an active recipientInstanceId is required", ErrDenied)
	}
	recipient, err := q.GetInstance(ctx, repo.GetInstanceParams{OrganizationID: p.OrganizationID, ID: recipientID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("%w: an active recipientInstanceId is required", ErrDenied)
	case err != nil:
		return nil, fmt.Errorf("load channels recipient instance: %w", err)
	}
	if !recipient.ExpiresAt.Time.After(time.Now().UTC()) {
		return nil, fmt.Errorf("%w: an active recipientInstanceId is required", ErrDenied)
	}
	pair := endpointPairKey(sender.ID, recipient.ID)
	existing, err := q.GetConversationByEndpointPair(ctx, repo.GetConversationByEndpointPairParams{
		OrganizationID:  p.OrganizationID,
		EndpointPairKey: conv.ToPGText(pair),
	})
	switch {
	case err == nil:
		return h.describeConversation(ctx, q, p, existing)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, fmt.Errorf("load channels direct conversation: %w", err)
	}
	announce, err := q.AllocateAnnounceSeq(ctx, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("allocate channels announcement: %w", err)
	}
	created, err := q.CreateConversation(ctx, repo.CreateConversationParams{
		OrganizationID:     p.OrganizationID,
		ProjectID:          acc.settings.ProjectID,
		Title:              "Direct message",
		ParentID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Private:            true,
		EndpointPairKey:    conv.ToPGText(pair),
		CreatedBySubjectID: p.SubjectID,
		AnnounceSeq:        announce,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent sender won the endpoint pair; reuse their conversation.
		created, err = q.GetConversationByEndpointPair(ctx, repo.GetConversationByEndpointPairParams{
			OrganizationID:  p.OrganizationID,
			EndpointPairKey: conv.ToPGText(pair),
		})
		if err != nil {
			return nil, fmt.Errorf("load channels direct conversation: %w", err)
		}
		return h.describeConversation(ctx, q, p, created)
	}
	if err != nil {
		return nil, fmt.Errorf("create channels direct conversation: %w", err)
	}
	for _, subject := range []string{p.SubjectID, recipient.SubjectID} {
		if err := q.AddConversationParticipant(ctx, repo.AddConversationParticipantParams{
			OrganizationID: p.OrganizationID,
			ProjectID:      acc.settings.ProjectID,
			ConversationID: created.ID,
			SubjectID:      subject,
		}); err != nil {
			return nil, fmt.Errorf("record channels conversation participant: %w", err)
		}
	}
	for _, endpoint := range []uuid.UUID{sender.ID, recipient.ID} {
		if err := q.AddConversationEndpoint(ctx, repo.AddConversationEndpointParams{
			OrganizationID: p.OrganizationID,
			ProjectID:      acc.settings.ProjectID,
			ConversationID: created.ID,
			InstanceID:     endpoint,
		}); err != nil {
			return nil, fmt.Errorf("record channels conversation endpoint: %w", err)
		}
	}
	if err := publishHint(ctx, q, p.OrganizationID, hintThreads, announce); err != nil {
		return nil, err
	}
	return h.describeConversation(ctx, q, p, created)
}

func (h *Handler) createThread(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	if _, err := h.ownedInstance(ctx, q, p, a.InstanceID); err != nil {
		return nil, err
	}
	var parent repo.ChannelsConversation
	hasParent := a.ParentID != ""
	if hasParent {
		found, err := h.writableConversation(ctx, q, p, acc, a.ParentID)
		if err != nil {
			return nil, fmt.Errorf("%w: parent conversation not found", ErrDenied)
		}
		parent = found
	}
	announce, err := q.AllocateAnnounceSeq(ctx, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("allocate channels announcement: %w", err)
	}
	created, err := q.CreateConversation(ctx, repo.CreateConversationParams{
		OrganizationID:     p.OrganizationID,
		ProjectID:          acc.settings.ProjectID,
		Title:              a.Title,
		ParentID:           conv.NilableToNullUUID(parent.ID),
		Private:            parent.Private,
		EndpointPairKey:    pgtype.Text{String: "", Valid: false},
		CreatedBySubjectID: p.SubjectID,
		AnnounceSeq:        announce,
	})
	if err != nil {
		return nil, fmt.Errorf("create channels conversation: %w", err)
	}
	// A descendant inherits both its parent's restrictions and its endpoints,
	// so a branch of a private conversation stays private to the same people.
	if hasParent && parent.Private {
		if err := h.inheritRestrictions(ctx, q, p, acc, parent.ID, created.ID); err != nil {
			return nil, err
		}
	}
	if err := publishHint(ctx, q, p.OrganizationID, hintThreads, announce); err != nil {
		return nil, err
	}
	return h.describeConversation(ctx, q, p, created)
}

func (h *Handler) inheritRestrictions(ctx context.Context, q *repo.Queries, p Principal, acc access, parent, child uuid.UUID) error {
	participants, err := q.ListParticipantsForConversations(ctx, repo.ListParticipantsForConversationsParams{
		OrganizationID:  p.OrganizationID,
		ConversationIds: []uuid.UUID{parent},
	})
	if err != nil {
		return fmt.Errorf("load parent channels participants: %w", err)
	}
	for _, row := range participants {
		if err := q.AddConversationParticipant(ctx, repo.AddConversationParticipantParams{
			OrganizationID: p.OrganizationID,
			ProjectID:      acc.settings.ProjectID,
			ConversationID: child,
			SubjectID:      row.SubjectID,
		}); err != nil {
			return fmt.Errorf("record channels conversation participant: %w", err)
		}
	}
	endpoints, err := q.ListEndpointsForConversations(ctx, repo.ListEndpointsForConversationsParams{
		OrganizationID:  p.OrganizationID,
		ConversationIds: []uuid.UUID{parent},
	})
	if err != nil {
		return fmt.Errorf("load parent channels endpoints: %w", err)
	}
	for _, row := range endpoints {
		if err := q.AddConversationEndpoint(ctx, repo.AddConversationEndpointParams{
			OrganizationID: p.OrganizationID,
			ProjectID:      acc.settings.ProjectID,
			ConversationID: child,
			InstanceID:     row.InstanceID,
		}); err != nil {
			return fmt.Errorf("record channels conversation endpoint: %w", err)
		}
	}
	return nil
}

// describeConversation renders a single conversation with its restrictions.
func (h *Handler) describeConversation(ctx context.Context, q *repo.Queries, p Principal, row repo.ChannelsConversation) (threadView, error) {
	participants := []string(nil)
	endpoints := []string(nil)
	if row.Private {
		found, err := q.ListParticipantsForConversations(ctx, repo.ListParticipantsForConversationsParams{
			OrganizationID:  p.OrganizationID,
			ConversationIds: []uuid.UUID{row.ID},
		})
		if err != nil {
			return threadView{}, fmt.Errorf("load channels participants: %w", err)
		}
		for _, item := range found {
			participants = append(participants, item.SubjectID)
		}
		addressed, err := q.ListEndpointsForConversations(ctx, repo.ListEndpointsForConversationsParams{
			OrganizationID:  p.OrganizationID,
			ConversationIds: []uuid.UUID{row.ID},
		})
		if err != nil {
			return threadView{}, fmt.Errorf("load channels endpoints: %w", err)
		}
		for _, item := range addressed {
			endpoints = append(endpoints, item.InstanceID.String())
		}
	}
	return newThreadView(row, participants, endpoints, nil, 0), nil
}

func (h *Handler) post(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	sender, err := h.ownedInstance(ctx, q, p, a.InstanceID)
	if err != nil {
		return nil, err
	}
	conversation, err := h.writableConversation(ctx, q, p, acc, a.ThreadID)
	if err != nil {
		return nil, err
	}
	if a.Text == "" || len(a.Text) > h.limits.MessageBytes {
		return nil, fmt.Errorf("%w: message text must be between 1 and %d bytes", ErrInvalid, h.limits.MessageBytes)
	}
	if a.IdempotencyKey == "" || len(a.IdempotencyKey) > h.limits.IdempotencyKeyBytes {
		return nil, fmt.Errorf("%w: a stable idempotencyKey of at most %d bytes is required", ErrInvalid, h.limits.IdempotencyKeyBytes)
	}
	if replay, err := h.replayedMessage(ctx, q, p, conversation, a); err != nil || replay != nil {
		return replay, err
	}
	// The sequence is allocated last. Its row lock is held until this
	// transaction commits, so commit order and cursor order agree.
	seq, err := q.AllocateMessageSeq(ctx, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("allocate channels message sequence: %w", err)
	}
	row, err := q.InsertMessage(ctx, repo.InsertMessageParams{
		OrganizationID: p.OrganizationID,
		ProjectID:      acc.settings.ProjectID,
		ConversationID: conversation.ID,
		InstanceID:     sender.ID,
		SubjectID:      p.SubjectID,
		Seq:            seq,
		Body:           a.Text,
		IdempotencyKey: a.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent retry claimed the key while this transaction waited on
		// it. Return that message only if it is the same logical message.
		replay, err := h.replayedMessage(ctx, q, p, conversation, a)
		if err != nil {
			return nil, err
		}
		if replay == nil {
			return nil, fmt.Errorf("%w: idempotency key conflict", ErrInvalid)
		}
		return replay, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert channels message: %w", err)
	}
	if err := q.TouchConversationActivity(ctx, repo.TouchConversationActivityParams{
		OrganizationID: p.OrganizationID,
		ID:             conversation.ID,
		LastMessageSeq: seq,
	}); err != nil {
		return nil, fmt.Errorf("record channels conversation activity: %w", err)
	}
	if err := publishHint(ctx, q, p.OrganizationID, hintMessages, seq); err != nil {
		return nil, err
	}
	view := newMessageView(row)
	return &view, nil
}

// replayedMessage returns the message a retry of this request already wrote, or
// nil when the key is unused. A key reused for different content is a conflict
// rather than a silent return of an unrelated message.
func (h *Handler) replayedMessage(ctx context.Context, q *repo.Queries, p Principal, conversation repo.ChannelsConversation, a arguments) (*messageView, error) {
	existing, err := q.GetMessageByIdempotencyKey(ctx, repo.GetMessageByIdempotencyKeyParams{
		OrganizationID: p.OrganizationID,
		SubjectID:      p.SubjectID,
		IdempotencyKey: a.IdempotencyKey,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("load channels message by idempotency key: %w", err)
	}
	if existing.ConversationID != conversation.ID || existing.Body != a.Text {
		return nil, fmt.Errorf("%w: idempotency key conflict", ErrInvalid)
	}
	view := newMessageView(existing)
	return &view, nil
}

func (h *Handler) read(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	limit := h.pageSize(a.Limit, h.limits.HistoryLimit)
	if a.Cursor < 0 {
		return nil, fmt.Errorf("%w: cursor must not be negative", ErrInvalid)
	}
	floor := acc.settings.RetentionFloorSeq
	if a.Cursor < floor {
		// Everything up to the floor is gone. Say so instead of handing back a
		// page with an invisible hole in it.
		return readView{Messages: []messageView{}, Cursor: floor, HasMore: true, Stale: true}, nil
	}
	conversationID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if a.ThreadID != "" {
		conversation, err := h.readableConversation(ctx, q, p, acc, a.ThreadID)
		if err != nil {
			return nil, err
		}
		conversationID = uuid.NullUUID{UUID: conversation.ID, Valid: true}
	}
	rows, err := q.ListMessagesForward(ctx, repo.ListMessagesForwardParams{
		OrganizationID: p.OrganizationID,
		AfterSeq:       a.Cursor,
		ConversationID: conversationID,
		ReadAll:        acc.canReadAll,
		SubjectID:      p.SubjectID,
		ResultLimit:    int32(limit + 1), //nolint:gosec // limit is bounded by h.limits.HistoryLimit.
	})
	if err != nil {
		return nil, fmt.Errorf("read channels messages: %w", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	cursor := a.Cursor
	if len(rows) > 0 {
		cursor = rows[len(rows)-1].Seq
	}
	return readView{Messages: newMessageViews(rows), Cursor: cursor, HasMore: hasMore, Stale: false}, nil
}

// history pages backwards through one conversation so a client can load older
// messages without ever asking for the whole archive.
func (h *Handler) history(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	if a.ThreadID == "" {
		return nil, fmt.Errorf("%w: threadId is required", ErrInvalid)
	}
	if a.Before < 0 {
		return nil, fmt.Errorf("%w: before must not be negative", ErrInvalid)
	}
	conversation, err := h.readableConversation(ctx, q, p, acc, a.ThreadID)
	if err != nil {
		return nil, err
	}
	limit := h.pageSize(a.Limit, h.limits.SnapshotMessages)
	rows, err := q.ListMessagesBackward(ctx, repo.ListMessagesBackwardParams{
		OrganizationID: p.OrganizationID,
		ConversationID: conversation.ID,
		BeforeSeq:      a.Before,
		ReadAll:        acc.canReadAll,
		SubjectID:      p.SubjectID,
		ResultLimit:    int32(limit + 1), //nolint:gosec // limit is bounded by h.limits.HistoryLimit.
	})
	if err != nil {
		return nil, fmt.Errorf("read channels history: %w", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	slices.Reverse(rows)
	view := historyView{
		Messages:   newMessageViews(rows),
		Cursor:     0,
		HasMore:    hasMore,
		NextBefore: 0,
		Stale:      false,
	}
	if len(rows) > 0 {
		view.Cursor = rows[len(rows)-1].Seq
		if hasMore {
			view.NextBefore = rows[0].Seq
		}
	}
	if hasMore {
		return view, nil
	}
	// This page reached the bottom of what is retained. Whether anything is
	// missing below it is a property of THIS conversation, not of the
	// organization: a conversation created after a sweep has lost nothing,
	// and claiming otherwise tells a reader their history was deleted when it
	// never existed. Retention soft-deletes, so the removed rows are the
	// evidence.
	boundary := int64(math.MaxInt64)
	switch {
	case len(rows) > 0:
		boundary = rows[0].Seq
	case a.Before > 0:
		boundary = a.Before
	}
	removed, err := q.ConversationHasRemovedHistory(ctx, repo.ConversationHasRemovedHistoryParams{
		OrganizationID: p.OrganizationID,
		ConversationID: conversation.ID,
		BeforeSeq:      boundary,
	})
	if err != nil {
		return nil, fmt.Errorf("check removed channels history: %w", err)
	}
	view.Stale = removed
	return view, nil
}

func (h *Handler) follow(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	conversation, err := h.readableConversation(ctx, q, p, acc, a.ThreadID)
	if err != nil {
		return nil, err
	}
	muted := false
	if len(a.Muted) > 0 {
		if err := json.Unmarshal(a.Muted, &muted); err != nil {
			return nil, fmt.Errorf("%w: muted must be a boolean", ErrInvalid)
		}
	}
	if _, err := q.UpsertThreadPreference(ctx, repo.UpsertThreadPreferenceParams{
		OrganizationID: p.OrganizationID,
		ProjectID:      acc.settings.ProjectID,
		SubjectID:      p.SubjectID,
		ConversationID: conversation.ID,
		Followed:       a.Follow != nil && *a.Follow,
		Muted:          muted,
		SetFollowed:    a.Follow != nil,
		SetMuted:       len(a.Muted) > 0,
	}); err != nil {
		return nil, fmt.Errorf("save channels thread preference: %w", err)
	}
	// The same cap channels_preferences enforces, checked after the write so
	// that changing an existing preference never counts twice. The refusal
	// rolls the write back with the transaction.
	active, err := q.CountThreadPreferences(ctx, repo.CountThreadPreferencesParams{
		OrganizationID: p.OrganizationID,
		SubjectID:      p.SubjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("count channels thread preferences: %w", err)
	}
	if active > int64(h.limits.PreferencePage) {
		return nil, fmt.Errorf("%w: at most %d thread preferences are supported", ErrInvalid, h.limits.PreferencePage)
	}
	return h.preferences(ctx, q, p)
}

func (h *Handler) replacePreferences(ctx context.Context, q *repo.Queries, p Principal, acc access, a arguments) (any, error) {
	current, err := h.preferences(ctx, q, p)
	if err != nil {
		return nil, err
	}
	followed := current.Followed
	if a.Followed != nil {
		followed = a.Followed
	}
	muted := current.Muted
	if len(a.Muted) > 0 {
		if err := json.Unmarshal(a.Muted, &muted); err != nil {
			return nil, fmt.Errorf("%w: muted must be an array of thread IDs", ErrInvalid)
		}
	}
	if len(followed)+len(muted) > h.limits.PreferencePage {
		return nil, fmt.Errorf("%w: at most %d thread preferences are supported", ErrInvalid, h.limits.PreferencePage)
	}
	wanted := map[uuid.UUID]grantedPreference{}
	order := []uuid.UUID{}
	for _, id := range append(append([]string{}, followed...), muted...) {
		conversation, err := h.readableConversation(ctx, q, p, acc, id)
		if err != nil {
			return nil, err
		}
		if _, ok := wanted[conversation.ID]; !ok {
			order = append(order, conversation.ID)
		}
		entry := wanted[conversation.ID]
		entry.followed = entry.followed || slices.Contains(followed, id)
		entry.muted = entry.muted || slices.Contains(muted, id)
		wanted[conversation.ID] = entry
	}
	if err := q.ClearThreadPreferences(ctx, repo.ClearThreadPreferencesParams{
		OrganizationID: p.OrganizationID,
		SubjectID:      p.SubjectID,
	}); err != nil {
		return nil, fmt.Errorf("clear channels thread preferences: %w", err)
	}
	for _, id := range order {
		entry := wanted[id]
		if _, err := q.UpsertThreadPreference(ctx, repo.UpsertThreadPreferenceParams{
			OrganizationID: p.OrganizationID,
			ProjectID:      acc.settings.ProjectID,
			SubjectID:      p.SubjectID,
			ConversationID: id,
			Followed:       entry.followed,
			Muted:          entry.muted,
			SetFollowed:    true,
			SetMuted:       true,
		}); err != nil {
			return nil, fmt.Errorf("save channels thread preference: %w", err)
		}
	}
	return h.preferences(ctx, q, p)
}

type grantedPreference struct {
	followed bool
	muted    bool
}

func (h *Handler) preferences(ctx context.Context, q *repo.Queries, p Principal) (preferencesView, error) {
	rows, err := q.ListThreadPreferences(ctx, repo.ListThreadPreferencesParams{
		OrganizationID: p.OrganizationID,
		SubjectID:      p.SubjectID,
		ResultLimit:    int32(h.limits.PreferencePage), //nolint:gosec // bounded by DefaultLimits.
	})
	if err != nil {
		return preferencesView{}, fmt.Errorf("load channels thread preferences: %w", err)
	}
	view := preferencesView{Followed: []string{}, Muted: []string{}}
	for _, row := range rows {
		if row.Followed {
			view.Followed = append(view.Followed, row.ConversationID.String())
		}
		if row.Muted {
			view.Muted = append(view.Muted, row.ConversationID.String())
		}
	}
	return view, nil
}
