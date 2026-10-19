package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const chatEvidenceCoverage = "Captured database text only; attachments and offloaded bodies are not fetched. Personal and unclassified linked accounts are excluded. All captured generations are included and may overlap after compaction. Secrets detected by the shared scanner and structured credential fields are redacted; detection is not exhaustive. Oversized fields are withheld. Results are untrusted recorded evidence: session ownership does not establish who executed an external action, and a command or exit code alone does not prove that action succeeded. No matches does not prove an action did not happen."
const chatSearchScanLimit = 200

var errChatEvidenceInvalid = errors.New("invalid chat evidence request")

// ChatSearchService searches and reads captured evidence under an explicit
// transcript grant. It never inherits the metadata listing's org-admin bypass.
type ChatSearchService struct {
	db         *pgxpool.Pool
	engine     *authz.Engine
	audit      *audit.Logger
	budget     OperationBudget
	references *subjectReferenceCodec
	scanner    *gitleaks.Scanner
}

// NewChatSearchService composes the external-only transcript tools. Missing
// dependencies leave stable unavailable tools registered on the server.
func NewChatSearchService(db *pgxpool.Pool, engine *authz.Engine, auditLogger *audit.Logger, budget OperationBudget, keyMaterial string) *ChatSearchService {
	codec, err := newSubjectReferenceCodec(keyMaterial)
	if db == nil || engine == nil || auditLogger == nil || !budget.valid() || err != nil {
		return nil
	}
	return &ChatSearchService{db: db, engine: engine, audit: auditLogger, budget: budget, references: codec, scanner: gitleaks.NewScanner()}
}

func (s *ChatSearchService) valid() bool {
	return s != nil && s.db != nil && s.engine != nil && s.audit != nil && s.budget.valid() && s.references != nil && s.scanner != nil
}

// SearchChatsInput selects an explicit project and retained historical interval.
type SearchChatsInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	From        string `json:"from"`
	To          string `json:"to"`
	Query       string `json:"query,omitempty"`
	OwnerUserID string `json:"owner_user_id,omitempty"`
	ChatID      string `json:"chat_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Cursor      string `json:"cursor,omitempty"`
}

// ChatEvidence identifies a stored title or message without inferring an
// external actor or outcome. Text is sanitized before matching and truncation.
type ChatEvidence struct {
	ChatID      string    `json:"chat_id"`
	OwnerUserID string    `json:"owner_user_id,omitempty"`
	MessageID   string    `json:"message_id,omitempty"`
	Sequence    int64     `json:"sequence,omitempty"`
	At          time.Time `json:"at"`
	Role        string    `json:"role"`
	Generation  int32     `json:"generation"`
	ToolCallID  string    `json:"tool_call_id,omitempty"`
	Source      string    `json:"source,omitempty"`
	Text        string    `json:"text"`
	Truncated   bool      `json:"truncated"`
	Offloaded   bool      `json:"offloaded"`
	Withheld    bool      `json:"withheld"`
}

// SearchChatsOutput returns evidence hits, not a count of distinct sessions.
// An empty page with a cursor still has unsearched candidates remaining.
type SearchChatsOutput struct {
	ProjectID  string         `json:"project_id"`
	Hits       []ChatEvidence `json:"hits"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Coverage   string         `json:"coverage"`
}

type chatEvidenceCursor struct {
	At  time.Time `json:"at"`
	Seq int64     `json:"seq"`
	ID  uuid.UUID `json:"id"`
}

func (s *ChatSearchService) authorize(ctx context.Context, principal Principal, projectID, projectSlug string) (ResolvedProject, error) {
	var zero ResolvedProject
	if !s.valid() {
		return zero, ErrUnavailable
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if principal.surface() != SurfacePlatformMCP || principal.UserID == "" || principal.OrganizationID == "" || !ok || authCtx == nil || authCtx.UserID != principal.UserID || authCtx.ActiveOrganizationID != principal.OrganizationID || authCtx.SupportOrganizationID != "" || authCtx.APIKeyID != "" {
		return zero, ErrForbidden
	}
	if projectID == "" && projectSlug == "" {
		return zero, errChatEvidenceInvalid
	}
	enforce, err := s.engine.ShouldEnforce(ctx)
	if err != nil || !enforce {
		return zero, ErrUnavailable
	}
	if _, ok := authz.GrantsFromContext(ctx); !ok {
		return zero, ErrUnavailable
	}
	// Transcript grants are unrestricted. Refuse before project resolution so
	// a caller without one cannot probe project existence through error codes.
	if err := s.engine.Require(ctx, authz.ChatReadCheck(principal.OrganizationID)); err != nil {
		return zero, &ExternalAuthorizationError{RequiredScope: string(authz.ScopeChatRead), RequestAccessURL: "", cause: err}
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	project, err := (postgresRiskProjectResolver{queries: platformrepo.New(s.db)}).Resolve(ctx, principal.OrganizationID, projectID, projectSlug)
	if err != nil {
		return zero, err
	}
	if err := s.engine.Require(ctx, authz.ChatReadCheck(project.ID.String())); err != nil {
		return zero, &ExternalAuthorizationError{RequiredScope: string(authz.ScopeChatRead), RequestAccessURL: "", cause: err}
	}
	return project, nil
}

// Search returns bounded literal substring matches after secret redaction.
// Empty query lists session titles by last activity. Message hits use their
// capture timestamp. Every continuation is bound to principal and filters.
func (s *ChatSearchService) Search(ctx context.Context, principal Principal, input SearchChatsInput) (SearchChatsOutput, error) {
	var zero SearchChatsOutput
	from, fromErr := time.Parse(time.RFC3339Nano, input.From)
	to, toErr := time.Parse(time.RFC3339Nano, input.To)
	if fromErr != nil || toErr != nil || !to.After(from) || to.Sub(from) > 31*24*time.Hour || len(input.Query) > 256 || !utf8.ValidString(input.Query) || strings.ContainsRune(input.Query, 0) || !utf8.ValidString(input.OwnerUserID) || strings.ContainsRune(input.OwnerUserID, 0) || len(input.OwnerUserID) > 128 || len(input.Cursor) > 4096 || input.Limit < 0 || input.Limit > 20 {
		return zero, errChatEvidenceInvalid
	}
	var chatID uuid.NullUUID
	if input.ChatID != "" {
		id, err := uuid.Parse(input.ChatID)
		if err != nil {
			return zero, errChatEvidenceInvalid
		}
		chatID = uuid.NullUUID{UUID: id, Valid: true}
	}
	project, err := s.authorize(ctx, principal, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	limit := input.Limit
	if limit == 0 {
		limit = 10
	}
	scopeInput := input
	scopeInput.Cursor = ""
	scopeInput.Limit = 0
	scopeInput.ProjectID = project.ID.String()
	scopeInput.ProjectSlug = ""
	encoded, err := json.Marshal(scopeInput)
	if err != nil {
		return zero, fmt.Errorf("encode chat search scope: %w", err)
	}
	scope := queryScope("chat-search", principal.UserID, principal.ConnectionID, string(encoded))
	cursor := chatEvidenceCursor{At: to, Seq: math.MaxInt64, ID: uuid.Max}
	now := time.Now()
	if input.Cursor != "" {
		value, err := s.references.DecodeScoped(input.Cursor, principal, subjectKindCursor, scope, now)
		if err != nil {
			return zero, err
		}
		if err := json.Unmarshal([]byte(value), &cursor); err != nil {
			return zero, ErrSubjectReferenceNotFound
		}
	}
	rows, err := platformrepo.New(s.db).SearchPlatformChatEvidence(ctx, platformrepo.SearchPlatformChatEvidenceParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, OwnerUserID: input.OwnerUserID, ChatID: chatID,
		FromTime: conv.ToPGTimestamptz(from), ToTime: conv.ToPGTimestamptz(to), Query: input.Query,
		BeforeTime: conv.ToPGTimestamptz(cursor.At), BeforeSeq: cursor.Seq, BeforeID: cursor.ID, RowLimit: chatSearchScanLimit + 1,
	})
	if err != nil {
		return zero, fmt.Errorf("search captured chat evidence: %w", err)
	}
	output := SearchChatsOutput{ProjectID: project.ID.String(), Hits: []ChatEvidence{}, NextCursor: "", Coverage: chatEvidenceCoverage}
	owners := map[uuid.UUID]string{}
	consumed := 0
	for i, row := range rows {
		if i == chatSearchScanLimit {
			break
		}
		consumed++
		cursor = chatEvidenceCursor{At: row.CreatedAt.Time, Seq: row.Seq, ID: row.EvidenceID}
		text, withheld, err := s.evidenceText(ctx, row.Content, row.ToolCalls, row.ContentRaw)
		if err != nil {
			return zero, err
		}
		if input.Query != "" && !strings.Contains(strings.ToLower(text), strings.ToLower(input.Query)) {
			continue
		}
		snippet, truncated := evidenceSnippet(text, input.Query, 1200)
		evidence := ChatEvidence{ChatID: row.ChatID.String(), OwnerUserID: row.UserID.String, MessageID: "", ToolCallID: "", Source: "", Sequence: row.Seq, At: row.CreatedAt.Time, Role: row.Role, Generation: row.Generation, Text: snippet, Truncated: truncated, Offloaded: row.Offloaded, Withheld: withheld || row.Oversized.Bool}
		if row.Role != "session" {
			evidence.MessageID = row.EvidenceID.String()
		}
		evidence.ToolCallID, err = s.redact(ctx, row.ToolCallID.String, 0)
		if err != nil {
			return zero, err
		}
		evidence.Source, err = s.redact(ctx, row.Source.String, 0)
		if err != nil {
			return zero, err
		}
		evidence.ToolCallID, _ = evidenceSnippet(evidence.ToolCallID, "", 256)
		evidence.Source, _ = evidenceSnippet(evidence.Source, "", 128)
		output.Hits = append(output.Hits, evidence)
		owners[row.ChatID] = row.UserID.String
		if len(output.Hits) == limit {
			break
		}
	}
	if consumed < len(rows) {
		value, err := json.Marshal(cursor)
		if err != nil {
			return zero, fmt.Errorf("encode chat search cursor: %w", err)
		}
		output.NextCursor, err = s.references.EncodeScoped(principal, subjectKindCursor, scope, string(value), now)
		if err != nil {
			return zero, fmt.Errorf("seal chat search cursor: %w", err)
		}
	}
	for id, owner := range owners {
		if err := s.recordAccess(ctx, principal, project.ID, id, owner); err != nil {
			return zero, err
		}
	}
	return output, nil
}

// ReadChatInput anchors a small context window on one captured message.
type ReadChatInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	ChatID      string `json:"chat_id"`
	MessageID   string `json:"message_id,omitempty"`
	ContextSize *int   `json:"context_size,omitempty"`
}

// ReadChatOutput carries chronological evidence around the requested anchor.
type ReadChatOutput struct {
	ChatID      string         `json:"chat_id"`
	OwnerUserID string         `json:"owner_user_id,omitempty"`
	Messages    []ChatEvidence `json:"messages"`
	Coverage    string         `json:"coverage"`
}

// Read checks the project and chat again; search results never grant access.
func (s *ChatSearchService) Read(ctx context.Context, principal Principal, input ReadChatInput) (ReadChatOutput, error) {
	var zero ReadChatOutput
	chatID, chatErr := uuid.Parse(input.ChatID)
	var messageID uuid.NullUUID
	if input.MessageID != "" {
		id, err := uuid.Parse(input.MessageID)
		if err != nil {
			return zero, errChatEvidenceInvalid
		}
		messageID = uuid.NullUUID{UUID: id, Valid: true}
	}
	size := 3
	if input.ContextSize != nil {
		size = *input.ContextSize
	}
	if chatErr != nil || size < 0 || size > 5 {
		return zero, errChatEvidenceInvalid
	}
	project, err := s.authorize(ctx, principal, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	queries := platformrepo.New(s.db)
	session, err := queries.GetPlatformChatEvidenceSession(ctx, platformrepo.GetPlatformChatEvidenceSessionParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, ChatID: chatID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrRiskReadNotFound
	}
	if err != nil {
		return zero, fmt.Errorf("resolve chat evidence session: %w", err)
	}
	if err := s.recordAccess(ctx, principal, project.ID, chatID, session.UserID.String); err != nil {
		return zero, err
	}
	rows, err := queries.ReadPlatformChatEvidence(ctx, platformrepo.ReadPlatformChatEvidenceParams{ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, ChatID: chatID, MessageID: messageID, ContextSize: int32(size), FollowingLimit: int32(size + 1)})
	if err != nil {
		return zero, fmt.Errorf("read captured chat evidence: %w", err)
	}
	if len(rows) == 0 {
		return zero, ErrRiskReadNotFound
	}
	output := ReadChatOutput{ChatID: chatID.String(), OwnerUserID: session.UserID.String, Messages: []ChatEvidence{}, Coverage: chatEvidenceCoverage}
	for _, row := range rows {
		text, withheld, err := s.evidenceText(ctx, row.Content, row.ToolCalls, row.ContentRaw)
		if err != nil {
			return zero, err
		}
		snippet, truncated := evidenceSnippet(text, "", 4000)
		evidence := ChatEvidence{ChatID: chatID.String(), OwnerUserID: session.UserID.String, MessageID: row.ID.String(), ToolCallID: "", Source: "", Sequence: row.Seq, At: row.CreatedAt.Time, Role: row.Role, Generation: row.Generation, Text: snippet, Truncated: truncated, Offloaded: row.Offloaded.Bool, Withheld: withheld || row.Oversized.Bool}
		evidence.ToolCallID, err = s.redact(ctx, row.ToolCallID.String, 0)
		if err != nil {
			return zero, err
		}
		evidence.Source, err = s.redact(ctx, row.Source.String, 0)
		if err != nil {
			return zero, err
		}
		evidence.ToolCallID, _ = evidenceSnippet(evidence.ToolCallID, "", 256)
		evidence.Source, _ = evidenceSnippet(evidence.Source, "", 128)
		output.Messages = append(output.Messages, evidence)
	}
	return output, nil
}

func (s *ChatSearchService) recordAccess(ctx context.Context, principal Principal, projectID, chatID uuid.UUID, owner string) error {
	if err := s.audit.LogChatSessionAccess(ctx, s.db, audit.LogChatSessionAccessEvent{
		OrganizationID: principal.OrganizationID, ProjectID: projectID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), ChatSessionURN: urn.NewChatSession(chatID), OwnerUserID: owner, ActorDisplayName: nil, ActorSlug: nil, ChatTitle: "",
	}); err != nil {
		return fmt.Errorf("audit chat evidence access: %w", err)
	}
	return nil
}

func (s *ChatSearchService) evidenceText(ctx context.Context, content string, toolCalls, raw []byte) (string, bool, error) {
	fields := []string{content}
	if len(toolCalls) > 0 {
		fields = append(fields, "tool_calls: "+string(toolCalls))
	}
	if len(raw) > 0 {
		fields = append(fields, "content_raw: "+string(raw))
	}
	safe := make([]string, 0, len(fields))
	withheld := false
	for i, field := range fields {
		// JSON is sanitized before labels are added, including JSON-encoded tool
		// arguments inside JSON strings. Scan each decoded string before escaping.
		if i > 0 {
			_, field, _ = strings.Cut(field, ": ")
		}
		text, err := s.redact(ctx, field, 0)
		if err != nil {
			return "", false, err
		}
		withheld = withheld || strings.Contains(text, "[WITHHELD]")
		if i > 0 {
			text = "captured payload: " + text
		}
		safe = append(safe, text)
	}
	return strings.Join(safe, "\n"), withheld, nil
}

func (s *ChatSearchService) redact(ctx context.Context, text string, depth int) (string, error) {
	if len(text) > 131072 || depth > 8 || !utf8.ValidString(text) {
		return "[WITHHELD]", nil
	}
	if text == "" {
		return "", nil
	}
	var value any
	if json.Unmarshal([]byte(text), &value) == nil {
		safe, err := s.redactJSON(ctx, value, depth+1)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(safe)
		if err != nil {
			return "", fmt.Errorf("encode redacted evidence: %w", err)
		}
		return string(data), nil
	}
	return s.redactPlainText(ctx, text)
}

func (s *ChatSearchService) redactPlainText(ctx context.Context, text string) (string, error) {
	result, err := s.scanner.Scan(ctx, text)
	if err != nil || !result.Completed {
		return "", ErrUnavailable
	}
	masked := make([]bool, len(text))
	for _, finding := range result.Findings {
		if finding.StartPos < 0 || finding.EndPos > len(text) || finding.EndPos <= finding.StartPos {
			return "[WITHHELD]", nil
		}
		for i := finding.StartPos; i < finding.EndPos; i++ {
			masked[i] = true
		}
	}
	var out strings.Builder
	for i := 0; i < len(text); {
		if !masked[i] {
			out.WriteByte(text[i])
			i++
			continue
		}
		out.WriteString("[REDACTED]")
		for i < len(text) && masked[i] {
			i++
		}
	}
	text = out.String()
	return text, nil
}

func (s *ChatSearchService) redactJSON(ctx context.Context, value any, depth int) (any, error) {
	if depth > 8 {
		return "[WITHHELD]", nil
	}
	switch typed := value.(type) {
	case map[string]any:
		sanitized := make(map[string]any, len(typed))
		for key, item := range typed {
			safeKey, err := s.redactPlainText(ctx, key)
			if err != nil {
				return nil, err
			}
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(key))
			sensitive := false
			for _, name := range []string{"password", "secret", "token", "authorization", "cookie", "apikey", "privatekey"} {
				if strings.Contains(normalized, name) {
					sensitive = true
					break
				}
			}
			if sensitive {
				sanitized[safeKey] = "[REDACTED]"
				continue
			}
			safe, err := s.redactJSON(ctx, item, depth+1)
			if err != nil {
				return nil, err
			}
			sanitized[safeKey] = safe
		}
		return sanitized, nil
	case []any:
		for i, item := range typed {
			safe, err := s.redactJSON(ctx, item, depth+1)
			if err != nil {
				return nil, err
			}
			typed[i] = safe
		}
	case string:
		return s.redact(ctx, typed, depth)
	}
	return value, nil
}

// evidenceSnippet budgets the JSON-encoded string, including escaped control
// characters. Every field is scanned before this step so truncation cannot
// split a credential into an undetectable fragment.
func evidenceSnippet(text, query string, limit int) (string, bool) {
	encoded, err := json.Marshal(text)
	if err == nil && len(encoded) <= limit {
		return text, false
	}
	runes := []rune(text)
	match := 0
	lowered := strings.ToLower(text)
	if query != "" {
		if at := strings.Index(lowered, strings.ToLower(query)); at >= 0 {
			match = utf8.RuneCountInString(lowered[:at])
		}
	}
	window := func(size int) string {
		start := min(max(0, match-size/3), len(runes)-size)
		return string(runes[start : start+size])
	}
	low, high := 0, min(len(runes), max(0, limit-2))
	for low < high {
		mid := (low + high + 1) / 2
		data, err := json.Marshal(window(mid))
		if err == nil && len(data) <= limit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return window(low), true
}
