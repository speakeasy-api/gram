package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/channels/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// legacyNamespace derives stable identifiers for converted rows. The same
// document always converts to the same identifiers, which is what makes a
// repeated conversion a no-op rather than a duplicate.
var legacyNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("speakeasy/channels/legacy-state"))

// Kinds of document-local identifier a conversion rewrites.
const (
	LegacyKindInstance     = "instance"
	LegacyKindConversation = "thread"
	legacyKindMessage      = "message"
)

// LegacyIdentifier is the documented mapping from a proof-of-concept
// document-local identifier to the UUID the conversion gives it. It is a pure
// function of the organization, the kind and the old identifier, so the
// mapping can always be recomputed and never has to be stored or looked up.
//
// Kind is LegacyKindInstance or LegacyKindConversation.
func LegacyIdentifier(organizationID, kind, legacyID string) uuid.UUID {
	return uuid.NewSHA1(legacyNamespace, []byte(organizationID+"/"+kind+"/"+legacyID))
}

func legacyID(organizationID, kind, key string) uuid.UUID {
	return LegacyIdentifier(organizationID, kind, key)
}

// The shapes below mirror the superseded channels_state document exactly. They
// exist only to read it.
type legacyInstance struct {
	ID         string    `json:"id"`
	SubjectID  string    `json:"subjectId"`
	ResumeHash string    `json:"resumeHash"`
	Label      string    `json:"label"`
	Note       string    `json:"note"`
	Origin     string    `json:"origin"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type legacyThread struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	ParentID     string   `json:"parentId"`
	Participants []string `json:"participants"`
	InstanceIDs  []string `json:"instanceIds"`
}

type legacyMessage struct {
	ID             int64     `json:"id"`
	ThreadID       string    `json:"threadId"`
	InstanceID     string    `json:"instanceId"`
	SubjectID      string    `json:"subjectId"`
	Text           string    `json:"text"`
	IdempotencyKey string    `json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
}

type legacyPreferences struct {
	Followed []string `json:"followed"`
	Muted    []string `json:"muted"`
}

type legacyGrant struct {
	CanConnect bool `json:"canConnect"`
	CanReadAll bool `json:"canReadAll"`
}

type legacyState struct {
	Enabled     bool                         `json:"enabled"`
	Grants      map[string]legacyGrant       `json:"grants"`
	Instances   map[string]legacyInstance    `json:"instances"`
	Threads     map[string]legacyThread      `json:"threads"`
	Messages    []legacyMessage              `json:"messages"`
	Preferences map[string]legacyPreferences `json:"preferences"`
	Cursor      int64                        `json:"cursor"`
}

// ConversionMapping records one identifier a conversion rewrote, so an
// operator can tell an agent or a saved link what its old identifier became.
// It is reportable rather than authoritative: LegacyIdentifier recomputes it.
type ConversionMapping struct {
	OrganizationID string
	// Kind is LegacyKindInstance or LegacyKindConversation.
	Kind     string
	LegacyID string
	ID       uuid.UUID
	Label    string
}

// ConversionResult reports what a conversion imported.
type ConversionResult struct {
	Organizations int
	Instances     int
	Conversations int
	Messages      int
	// SkippedMessages counts messages whose author instance is missing from
	// the document, which cannot be attributed and are therefore not imported.
	SkippedMessages int
	// Mappings lists every rewritten instance and conversation identifier.
	Mappings []ConversionMapping
	// Refused lists organizations the conversion declined to touch, with the
	// reason. A refusal is also returned as an error: it is a result an
	// operator has to act on, not a line in a log.
	Refused []ConversionRefusal
}

// ConversionRefusal records an organization a conversion would not touch.
type ConversionRefusal struct {
	OrganizationID string
	Reason         string
}

// ConversionOptions controls one conversion run.
type ConversionOptions struct {
	// DryRun performs the whole conversion and then rolls it back, so an
	// operator can see exactly what would be imported, including the
	// identifier mapping, without writing anything.
	DryRun bool
	// OrganizationID bounds the conversion to one tenant. Empty means every
	// organization with a legacy document in the database the caller pointed
	// at, which is a wide blast radius and should be asked for explicitly.
	OrganizationID string
}

// legacyDocumentPage is how many legacy documents one listing query reads and
// converts before reading the next. Each document holds an organization's
// whole proof-of-concept state, so a small page bounds memory while paging
// keeps an unscoped run complete however many documents there are.
const legacyDocumentPage = 50

// ConvertLegacyState copies the superseded channels_state document into the
// normalized tables.
//
// It is explicitly a development-only conversion: it is not wired into server
// startup and it is not a migration, because migrations in this repository
// carry no data manipulation. It never deletes the legacy document, so the
// original remains readable and the conversion can be repeated. Identifiers
// are derived from the document, so repeating it imports nothing new.
//
// Identifiers change shape: a document-local instance or thread ID becomes a
// UUID. Agents holding an old instance ID must connect again. Resume secrets
// are carried over as their stored hashes and are never re-issued.
func ConvertLegacyState(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, opts ConversionOptions) (ConversionResult, error) {
	result := ConversionResult{Organizations: 0, Instances: 0, Conversations: 0, Messages: 0, SkippedMessages: 0, Mappings: nil, Refused: nil}
	if db == nil {
		return result, fmt.Errorf("%w: no database", ErrUnavailable)
	}
	queries := repo.New(db)
	var failures []error
	after := ""
	for {
		// Each page is converted before the next is read, so memory holds at
		// most one page of documents however many there are.
		page, err := queries.ListLegacyChannelsStates(ctx, repo.ListLegacyChannelsStatesParams{
			OrganizationID:      opts.OrganizationID,
			AfterOrganizationID: after,
			ResultLimit:         legacyDocumentPage,
		})
		if err != nil {
			return result, fmt.Errorf("list legacy channels documents: %w", err)
		}
		for _, document := range page {
			imported, err := convertOne(ctx, db, queries, document, opts)
			if err != nil {
				// A failure is reported, never swallowed: the whole point of
				// this command is that an operator knows what happened to
				// their data.
				logger.ErrorContext(ctx, "channels legacy conversion failed for an organization",
					attr.SlogOrganizationID(document.OrganizationID), attr.SlogError(err))
				result.Refused = append(result.Refused, ConversionRefusal{
					OrganizationID: document.OrganizationID,
					Reason:         err.Error(),
				})
				failures = append(failures, fmt.Errorf("organization %s: %w", document.OrganizationID, err))
				continue
			}
			result.Organizations++
			result.Instances += imported.Instances
			result.Conversations += imported.Conversations
			result.Messages += imported.Messages
			result.SkippedMessages += imported.SkippedMessages
			result.Mappings = append(result.Mappings, imported.Mappings...)
		}
		if len(page) < legacyDocumentPage {
			break
		}
		after = page[len(page)-1].OrganizationID
	}
	if len(failures) > 0 {
		return result, fmt.Errorf("convert channels documents: %w", errors.Join(failures...))
	}
	return result, nil
}

//nolint:gocyclo // The conversion is one linear pass over the document's sections.
func convertOne(ctx context.Context, db *pgxpool.Pool, queries *repo.Queries, document repo.ListLegacyChannelsStatesRow, opts ConversionOptions) (ConversionResult, error) {
	result := ConversionResult{Organizations: 1, Instances: 0, Conversations: 0, Messages: 0, SkippedMessages: 0, Mappings: nil, Refused: nil}
	var state legacyState
	if err := json.Unmarshal(document.State, &state); err != nil {
		return result, fmt.Errorf("decode legacy channels document: %w", err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin channels conversion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := queries.WithTx(tx)

	organizationID := document.OrganizationID
	if _, err := q.EnsureChannelsSettings(ctx, organizationID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, fmt.Errorf("create channels settings: %w", err)
	}
	settings, err := q.GetChannelsSettings(ctx, organizationID)
	if err != nil {
		return result, fmt.Errorf("load channels settings: %w", err)
	}
	// Converted rows belong to the organization's canonical Channels project,
	// whatever project the document itself recorded, so they share the
	// lifetime of every row the live service writes.
	projectID := settings.ProjectID

	// A conversion carries the document's own message sequence numbers across,
	// so it can only be applied to an organization whose normalized tables
	// hold nothing but this document's own output. Merging into live data
	// would collide with sequences the service has already handed out, and
	// renumbering would change identifiers clients already hold. Refuse
	// instead, loudly.
	foreign, err := q.CountForeignChannelsRows(ctx, repo.CountForeignChannelsRowsParams{
		OrganizationID:      organizationID,
		MessageIds:          expectedMessageIDs(organizationID, state),
		ConversationIds:     expectedConversationIDs(organizationID, state),
		InstanceIds:         expectedInstanceIDs(organizationID, state),
		ExceptionSubjectIds: sortedKeys(state.Grants),
	})
	if err != nil {
		return result, fmt.Errorf("inspect existing channels rows: %w", err)
	}
	if foreign > 0 {
		return result, fmt.Errorf(
			"%w: this organization already holds %d normalized Channels rows that did not come from this document; "+
				"converting would reuse sequence numbers the live service has already issued", ErrDenied, foreign)
	}
	// The allocators are the other evidence of live use: a sequence the
	// service handed out above what this document converts to was handed out
	// by the service, even if the row it numbered is gone.
	highest, announced := legacyCounters(state)
	if settings.MessageSeq > highest || settings.AnnounceSeq > announced {
		return result, fmt.Errorf(
			"%w: this organization's Channels sequences are already past what this document converts to; "+
				"converting would reuse sequence numbers the live service has already issued", ErrDenied)
	}
	// A settings row with no sequences issued is one this conversion is
	// creating, so it takes the document's availability. On a repeat the
	// organization's current availability is kept: an administrator may have
	// changed it since.
	enabled := settings.Enabled
	if settings.MessageSeq == 0 && settings.AnnounceSeq == 0 {
		enabled = state.Enabled
	}

	announce := int64(0)
	for _, key := range sortedKeys(state.Instances) {
		item := state.Instances[key]
		announce++
		// Only a real SHA-256 hash is carried over. Anything else could never
		// have matched a presented secret, so dropping it loses no ability to
		// resume and keeps malformed material out of the credential column.
		resumeHash := item.ResumeHash
		if len(resumeHash) != 64 {
			resumeHash = ""
		}
		if err := q.ImportInstance(ctx, repo.ImportInstanceParams{
			ID:             legacyID(organizationID, LegacyKindInstance, key),
			OrganizationID: organizationID,
			ProjectID:      projectID,
			SubjectID:      item.SubjectID,
			Label:          item.Label,
			Note:           item.Note,
			Origin:         item.Origin,
			ResumeHash:     conv.PtrToPGText(conv.PtrEmpty(resumeHash)),
			AnnounceSeq:    announce,
			ExpiresAt:      conv.ToPGTimestamptz(item.ExpiresAt),
		}); err != nil {
			return result, fmt.Errorf("import channels instance: %w", err)
		}
		result.Instances++
		result.Mappings = append(result.Mappings, ConversionMapping{
			OrganizationID: organizationID,
			Kind:           LegacyKindInstance,
			LegacyID:       key,
			ID:             legacyID(organizationID, LegacyKindInstance, key),
			Label:          item.Label,
		})
	}

	// Only the first conversation addressing a given pair of endpoints claims
	// the reusable pair key; the document never deduplicated them.
	claimedPairs := map[string]bool{}
	lastSeq := map[string]int64{}
	counts := map[string]int64{}
	for _, message := range state.Messages {
		if _, ok := state.Instances[message.InstanceID]; !ok {
			continue
		}
		lastSeq[message.ThreadID] = max(lastSeq[message.ThreadID], message.ID)
		counts[message.ThreadID]++
	}
	for _, key := range sortedKeys(state.Threads) {
		item := state.Threads[key]
		private := len(item.Participants) > 0
		parent := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		if item.ParentID != "" {
			if _, ok := state.Threads[item.ParentID]; ok {
				parent = uuid.NullUUID{UUID: legacyID(organizationID, LegacyKindConversation, item.ParentID), Valid: true}
			}
		}
		pair := pgtype.Text{String: "", Valid: false}
		if private && len(item.InstanceIDs) == 2 {
			candidate := endpointPairKey(
				legacyID(organizationID, LegacyKindInstance, item.InstanceIDs[0]),
				legacyID(organizationID, LegacyKindInstance, item.InstanceIDs[1]),
			)
			if !claimedPairs[candidate] {
				claimedPairs[candidate] = true
				pair = conv.ToPGText(candidate)
			}
		}
		createdBy := ""
		if len(item.Participants) > 0 {
			createdBy = item.Participants[0]
		}
		if createdBy == "" {
			createdBy = "converted"
		}
		conversationID := legacyID(organizationID, LegacyKindConversation, key)
		announce++
		if err := q.ImportConversation(ctx, repo.ImportConversationParams{
			ID:                 conversationID,
			OrganizationID:     organizationID,
			ProjectID:          projectID,
			Title:              item.Title,
			ParentID:           parent,
			Private:            private,
			EndpointPairKey:    pair,
			CreatedBySubjectID: createdBy,
			AnnounceSeq:        announce,
			LastMessageSeq:     lastSeq[key],
			MessageCount:       counts[key],
		}); err != nil {
			return result, fmt.Errorf("import channels conversation: %w", err)
		}
		result.Conversations++
		result.Mappings = append(result.Mappings, ConversionMapping{
			OrganizationID: organizationID,
			Kind:           LegacyKindConversation,
			LegacyID:       key,
			ID:             conversationID,
			Label:          item.Title,
		})
		for _, subject := range item.Participants {
			if err := q.AddConversationParticipant(ctx, repo.AddConversationParticipantParams{
				OrganizationID: organizationID,
				ProjectID:      projectID,
				ConversationID: conversationID,
				SubjectID:      subject,
			}); err != nil {
				return result, fmt.Errorf("import channels participant: %w", err)
			}
		}
		for _, endpoint := range item.InstanceIDs {
			if _, ok := state.Instances[endpoint]; !ok {
				continue
			}
			if err := q.AddConversationEndpoint(ctx, repo.AddConversationEndpointParams{
				OrganizationID: organizationID,
				ProjectID:      projectID,
				ConversationID: conversationID,
				InstanceID:     legacyID(organizationID, LegacyKindInstance, endpoint),
			}); err != nil {
				return result, fmt.Errorf("import channels endpoint: %w", err)
			}
		}
	}

	for _, message := range state.Messages {
		_, hasThread := state.Threads[message.ThreadID]
		_, hasInstance := state.Instances[message.InstanceID]
		if !hasThread || !hasInstance {
			// A message whose author or conversation is gone cannot be
			// attributed, and attribution is not optional here.
			result.SkippedMessages++
			continue
		}
		if err := q.ImportMessage(ctx, repo.ImportMessageParams{
			ID:             legacyID(organizationID, legacyKindMessage, strconv.FormatInt(message.ID, 10)),
			OrganizationID: organizationID,
			ProjectID:      projectID,
			ConversationID: legacyID(organizationID, LegacyKindConversation, message.ThreadID),
			InstanceID:     legacyID(organizationID, LegacyKindInstance, message.InstanceID),
			SubjectID:      message.SubjectID,
			Seq:            message.ID,
			Body:           message.Text,
			IdempotencyKey: message.IdempotencyKey,
			CreatedAt:      conv.ToPGTimestamptz(message.CreatedAt),
		}); err != nil {
			return result, fmt.Errorf("import channels message: %w", err)
		}
		result.Messages++
	}

	for _, subject := range sortedKeys(state.Preferences) {
		item := state.Preferences[subject]
		for conversationKey, wanted := range legacyPreferenceSet(item, state.Threads) {
			if _, err := q.UpsertThreadPreference(ctx, repo.UpsertThreadPreferenceParams{
				OrganizationID: organizationID,
				ProjectID:      projectID,
				SubjectID:      subject,
				ConversationID: legacyID(organizationID, LegacyKindConversation, conversationKey),
				Followed:       wanted.followed,
				Muted:          wanted.muted,
				// Insert-only: an existing preference was written by an
				// earlier conversion or changed by its owner since.
				SetFollowed: false,
				SetMuted:    false,
			}); err != nil {
				return result, fmt.Errorf("import channels preference: %w", err)
			}
		}
	}

	for _, subject := range sortedKeys(state.Grants) {
		item := state.Grants[subject]
		if err := q.ImportChannelsAccessException(ctx, repo.ImportChannelsAccessExceptionParams{
			OrganizationID: organizationID,
			ProjectID:      projectID,
			SubjectID:      subject,
			CanConnect:     item.CanConnect,
			CanReadAll:     item.CanReadAll,
		}); err != nil {
			return result, fmt.Errorf("import channels access exception: %w", err)
		}
	}

	if err := q.SetChannelsCounters(ctx, repo.SetChannelsCountersParams{
		OrganizationID: organizationID,
		Enabled:        enabled,
		MessageSeq:     highest,
		AnnounceSeq:    announce,
	}); err != nil {
		return result, fmt.Errorf("lift channels counters: %w", err)
	}
	if opts.DryRun {
		// The rollback is the point: the counts and the mapping above are the
		// report, and nothing is written.
		return result, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit channels conversion: %w", err)
	}
	return result, nil
}

// legacyCounters returns the message and announcement sequences a conversion
// of state lifts the organization's allocators to.
func legacyCounters(state legacyState) (int64, int64) {
	highest := state.Cursor
	for _, message := range state.Messages {
		_, hasThread := state.Threads[message.ThreadID]
		_, hasInstance := state.Instances[message.InstanceID]
		if hasThread && hasInstance {
			highest = max(highest, message.ID)
		}
	}
	return highest, int64(len(state.Instances) + len(state.Threads))
}

// The expected*IDs helpers enumerate exactly what this document converts to,
// which is what lets a repeated conversion stay idempotent while a conversion
// into somebody else's data is refused.
func expectedInstanceIDs(organizationID string, state legacyState) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(state.Instances))
	for _, key := range sortedKeys(state.Instances) {
		ids = append(ids, legacyID(organizationID, LegacyKindInstance, key))
	}
	return ids
}

func expectedConversationIDs(organizationID string, state legacyState) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(state.Threads))
	for _, key := range sortedKeys(state.Threads) {
		ids = append(ids, legacyID(organizationID, LegacyKindConversation, key))
	}
	return ids
}

func expectedMessageIDs(organizationID string, state legacyState) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(state.Messages))
	for _, message := range state.Messages {
		ids = append(ids, legacyID(organizationID, legacyKindMessage, strconv.FormatInt(message.ID, 10)))
	}
	return ids
}

func legacyPreferenceSet(item legacyPreferences, threads map[string]legacyThread) map[string]grantedPreference {
	wanted := map[string]grantedPreference{}
	for _, id := range item.Followed {
		if _, ok := threads[id]; !ok {
			continue
		}
		entry := wanted[id]
		entry.followed = true
		wanted[id] = entry
	}
	for _, id := range item.Muted {
		if _, ok := threads[id]; !ok {
			continue
		}
		entry := wanted[id]
		entry.muted = true
		wanted[id] = entry
	}
	return wanted
}

// sortedKeys keeps a conversion deterministic across map iteration order, so
// announcement sequences do not depend on the order Go happened to walk.
func sortedKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
