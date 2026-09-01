package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/sessionhandoff"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Sentinel refusals for the session-recall tools. Both map to structured
// refusal payloads at the tool boundary rather than protocol errors.
var (
	errSessionRecallDisabled = errors.New("platform mcp session recall disabled")
	// errSessionNotFound covers unknown ids AND ids owned by someone else with
	// one message, so a caller cannot probe whether another user's session
	// exists (GetMCP precedent).
	errSessionNotFound = errors.New("platform mcp session not found")
)

// recallTargetHarness is the target recorded on the recall lineage edge: the
// continuation happens wherever the Platform MCP client runs, which is the
// only name the server can honestly record.
const recallTargetHarness = "platform-mcp"

// recallMessageRowCap bounds how many transcript rows one recall reads — the
// newest rows, since a handoff digest is about where the session ended up. At
// the renderer's 24 KiB recent-turns budget, rows beyond this many would only
// be dropped again after being materialized; the cap also bounds worst-case
// read volume against sessions holding many inline-threshold-sized messages.
const recallMessageRowCap = 240

// recallDigestByteCap is the hard bound on the serialized digest — a backstop
// over the renderer's own per-section bounds, so no renderer regression can
// ever return an unbounded document (2x the recent-turns budget, leaving room
// for the bounded fixed sections).
const recallDigestByteCap = 48 << 10

// recallDigestTruncationMarker terminates a digest cut by recallDigestByteCap,
// so a truncated document is never mistaken for a complete one.
const recallDigestTruncationMarker = "\n\n[digest truncated: size cap reached]"

// SessionRecallService serves the session-portability recall tools: listing
// the caller's own captured coding-agent sessions and rendering one as a
// redacted handoff digest so its work can continue in the current harness.
//
// Every read fuses tenancy and ownership into the SQL row filter (the queries
// carry organization + owner user_id + not-deleted + the personal-account
// exclusion), so there is no fetch-then-authorize step anywhere in this file.
type SessionRecallService struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	repo        *platformrepo.Queries
	auditor     *audit.Logger
	portability FeatureChecker
	budget      OperationBudget
}

// NewSessionRecallService composes the recall entry points. A nil dependency
// leaves the service invalid, which registers the unavailable stubs rather
// than tools that always fail.
func NewSessionRecallService(logger *slog.Logger, db *pgxpool.Pool, repo *platformrepo.Queries, auditor *audit.Logger, portability FeatureChecker, budget OperationBudget) *SessionRecallService {
	return &SessionRecallService{
		logger:      logger,
		db:          db,
		repo:        repo,
		auditor:     auditor,
		portability: portability,
		budget:      budget,
	}
}

func (s *SessionRecallService) valid() bool {
	return s != nil && s.logger != nil && s.db != nil && s.repo != nil && s.auditor != nil && s.portability != nil && s.budget.valid()
}

type ListMySessionsInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"maximum number of sessions to return; server clamps this to 100"`
}

// RecallableSession is one captured session the caller owns — metadata only,
// never transcript content.
type RecallableSession struct {
	SessionID   string `json:"session_id"`
	ChatID      string `json:"chat_id"`
	Title       string `json:"title,omitempty"`
	Summary     string `json:"summary,omitempty"`
	ProjectName string `json:"project_name"`
	ProjectSlug string `json:"project_slug"`
	Cwd         string `json:"cwd,omitempty"`
	LastActive  string `json:"last_active"`
}

type ListMySessionsOutput struct {
	Sessions []RecallableSession `json:"sessions"`
}

type ContinueSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"the session to recall — a session id from list_my_sessions"`
}

// ContinueSessionOutput carries the digest plus its fidelity as structured
// fields; fidelity is never folded into the markdown prose.
type ContinueSessionOutput struct {
	Digest          string   `json:"digest"`
	SourceSessionID string   `json:"source_session_id"`
	ChatID          string   `json:"chat_id"`
	NotCarriedOver  []string `json:"not_carried_over"`
	Notes           []string `json:"notes"`
}

func (s *SessionRecallService) ListMySessions(ctx context.Context, principal Principal, input ListMySessionsInput) (ListMySessionsOutput, error) {
	if !s.valid() {
		return ListMySessionsOutput{Sessions: nil}, ErrUnavailable
	}
	if err := s.requirePortability(ctx, principal); err != nil {
		return ListMySessionsOutput{Sessions: nil}, err
	}
	limit := boundedLimit(input.Limit)
	rows, err := s.repo.ListOwnedChatSessionsForRecall(ctx, platformrepo.ListOwnedChatSessionsForRecallParams{
		OrganizationID: principal.OrganizationID,
		UserID:         principal.UserID,
		RowLimit:       int32(limit), // #nosec G115 -- boundedLimit caps the value at 100.
	})
	if err != nil {
		return ListMySessionsOutput{Sessions: nil}, fmt.Errorf("list owned sessions for recall: %w", err)
	}
	sessions := make([]RecallableSession, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, RecallableSession{
			SessionID:   sessionIDForChat(row.ExternalChatID, row.ID),
			ChatID:      row.ID.String(),
			Title:       conv.FromPGTextOrEmpty[string](row.Title),
			Summary:     conv.FromPGTextOrEmpty[string](row.Summary),
			ProjectName: row.ProjectName,
			ProjectSlug: row.ProjectSlug,
			Cwd:         conv.FromPGTextOrEmpty[string](row.Cwd),
			LastActive:  row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		})
	}
	return ListMySessionsOutput{Sessions: sessions}, nil
}

func (s *SessionRecallService) ContinueSession(ctx context.Context, principal Principal, input ContinueSessionInput) (ContinueSessionOutput, error) {
	empty := ContinueSessionOutput{Digest: "", SourceSessionID: "", ChatID: "", NotCarriedOver: nil, Notes: nil}
	if !s.valid() {
		return empty, ErrUnavailable
	}
	if err := s.requirePortability(ctx, principal); err != nil {
		return empty, err
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return empty, err
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return empty, fmt.Errorf("session_id is required")
	}
	// Session ids resolve through the same derivation the capture paths use —
	// never by string-matching the raw id against a chat column.
	chatID := chat.SessionIDToChatID(sessionID)

	row, err := s.repo.GetOwnedChatForRecall(ctx, platformrepo.GetOwnedChatForRecallParams{
		ChatID:         chatID,
		OrganizationID: principal.OrganizationID,
		UserID:         principal.UserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, errSessionNotFound
	}
	if err != nil {
		return empty, fmt.Errorf("resolve owned session for recall: %w", err)
	}

	messages, err := s.repo.ListOwnedChatTranscriptMessagesForRecall(ctx, platformrepo.ListOwnedChatTranscriptMessagesForRecallParams{
		ChatID:         chatID,
		ProjectID:      uuid.NullUUID{UUID: row.ProjectID, Valid: true},
		OrganizationID: principal.OrganizationID,
		UserID:         principal.UserID,
		RowLimit:       recallMessageRowCap,
	})
	if err != nil {
		return empty, fmt.Errorf("read session transcript for recall: %w", err)
	}
	// The query serves the newest rows first so the cap keeps the session's
	// tail; everything downstream expects chronological order.
	cappedRead := len(messages) == recallMessageRowCap
	slices.Reverse(messages)
	findings, err := s.repo.ListRiskFindingSpansForRecall(ctx, platformrepo.ListRiskFindingSpansForRecallParams{
		ChatID:         chatID,
		ProjectID:      row.ProjectID,
		OrganizationID: principal.OrganizationID,
		UserID:         principal.UserID,
	})
	if err != nil {
		return empty, fmt.Errorf("read risk findings for recall: %w", err)
	}

	masks := maskFindingSpans(messages, findings)
	extract := turnsFromRows(messages, masks.content)

	sourceSessionID := sessionIDForChat(row.ExternalChatID, row.ID)
	handoff := sessionhandoff.Render(&sessionhandoff.Transcript{
		Session: sessionhandoff.SessionMeta{
			Title:        conv.FromPGTextOrEmpty[string](row.Title),
			SessionID:    sourceSessionID,
			ChatID:       chatID.String(),
			Source:       valueOrUnknown(extract.source),
			Cwd:          conv.FromPGTextOrEmpty[string](row.Cwd),
			LastActivity: row.UpdatedAt.Time,
		},
		Turns: extract.turns,
	}, sessionhandoff.Options{Budget: 0, RedactToolPayloads: true})
	if cappedRead {
		handoff.Fidelity.Warnings = append(handoff.Fidelity.Warnings, fmt.Sprintf("only the most recent %d messages were considered", recallMessageRowCap))
	}
	if extract.offloaded > 0 {
		handoff.Fidelity.Warnings = append(handoff.Fidelity.Warnings, fmt.Sprintf("%d large messages were offloaded to asset storage and are not included", extract.offloaded))
	}
	if extract.unanalyzed > 0 {
		handoff.Fidelity.Warnings = append(handoff.Fidelity.Warnings, fmt.Sprintf("%d messages are awaiting risk analysis; their content was withheld", extract.unanalyzed))
	}
	if masks.withheld > 0 {
		handoff.Fidelity.Warnings = append(handoff.Fidelity.Warnings, fmt.Sprintf("%d messages had findings that could not be masked precisely; their content was withheld", masks.withheld))
	}
	if extract.undecodedToolCalls > 0 {
		handoff.Fidelity.Warnings = append(handoff.Fidelity.Warnings, fmt.Sprintf("%d messages had tool activity that could not be decoded", extract.undecodedToolCalls))
	}
	digest, digestTruncated := truncateDigest(handoff.Markdown)
	if digestTruncated {
		handoff.Fidelity.Warnings = append(handoff.Fidelity.Warnings, "the digest exceeded the size cap and was truncated")
	}

	// The lineage edge and the governance record commit together BEFORE the
	// digest is returned: failing to record the recall refuses the call rather
	// than serving unrecorded content.
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return empty, oops.E(oops.CodeUnexpected, err, "record session recall").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := platformrepo.New(dbtx).InsertChatSessionRecallLink(ctx, platformrepo.InsertChatSessionRecallLinkParams{
		ProjectID:      row.ProjectID,
		OrganizationID: principal.OrganizationID,
		ParentChatID:   chatID,
		// The OAuth principal carries no harness session id, so the
		// continuation is unknowable at recall time: every recall records a
		// distinct NULL-child edge. The digest byline's lineage marker is the
		// retroactive correlation path.
		ChildChatID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ParentSessionID: sessionID,
		ChildSessionID:  conv.ToPGTextEmpty(""),
		TargetHarness:   recallTargetHarness,
		SourceSurface:   conv.ToPGText(string(principal.surface())),
		// The OAuth path carries no email; the audit event's actor names the
		// user.
		ActorEmail:     conv.ToPGTextEmpty(""),
		DeviceSerial:   conv.ToPGTextEmpty(""),
		DeviceHostname: conv.ToPGTextEmpty(""),
	}); err != nil {
		return empty, oops.E(oops.CodeUnexpected, err, "record session recall link").LogError(ctx, s.logger)
	}

	if err := s.auditor.LogChatSessionRecall(ctx, dbtx, audit.LogChatSessionRecallEvent{
		OrganizationID:     principal.OrganizationID,
		ProjectID:          row.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		ChatSessionURN:     urn.NewChatSession(chatID),
		ChatTitle:          conv.FromPGTextOrEmpty[string](row.Title),
		OwnerUserID:        principal.UserID,
		SourceSessionID:    sourceSessionID,
		RedactToolPayloads: true,
		FindingsMasked:     masks.neutralized,
		UnanalyzedMessages: extract.unanalyzed,
		DigestBytes:        len(digest),
		TurnsIncluded:      handoff.TurnsIncluded,
		TurnsDropped:       handoff.TurnsDropped,
	}); err != nil {
		return empty, oops.E(oops.CodeUnexpected, err, "record session recall").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return empty, oops.E(oops.CodeUnexpected, err, "record session recall").LogError(ctx, s.logger)
	}

	return ContinueSessionOutput{
		Digest:          digest,
		SourceSessionID: sourceSessionID,
		ChatID:          chatID.String(),
		NotCarriedOver:  handoff.Fidelity.Missing,
		Notes:           handoff.Fidelity.Warnings,
	}, nil
}

// requirePortability fails closed: an error resolving the feature refuses the
// call rather than defaulting the feature on.
func (s *SessionRecallService) requirePortability(ctx context.Context, principal Principal) error {
	enabled, err := s.portability(ctx, principal.OrganizationID)
	if err != nil {
		return fmt.Errorf("resolve session portability feature: %w", err)
	}
	if !enabled {
		return errSessionRecallDisabled
	}
	return nil
}

// sessionIDForChat prefers the harness-native session id the capture pipeline
// recorded, falling back to the chat uuid (which IS the session id for chats
// captured under a UUID session id).
func sessionIDForChat(externalChatID pgtype.Text, chatID uuid.UUID) string {
	if id := conv.FromPGTextOrEmpty[string](externalChatID); id != "" {
		return id
	}
	return chatID.String()
}

func valueOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// findingSpanEntry mirrors one element of the risk_results.spans JSONB column
// ({match,field,path,start_pos,end_pos} per the schema comment). Declared
// locally so this package does not depend on the risk-analysis activity
// package that writes it.
type findingSpanEntry struct {
	Match    string `json:"match"`
	Field    string `json:"field"`
	Path     string `json:"path"`
	StartPos int    `json:"start_pos"`
	EndPos   int    `json:"end_pos"`
}

// withheldContentPlaceholder replaces a message's entire prose when a finding
// targeting it cannot be masked precisely — the fail-closed alternative to
// serving content known to carry an un-applied finding.
const withheldContentPlaceholder = "[content withheld: a risk finding could not be masked precisely]"

// unanalyzedContentPlaceholder replaces a message's prose while its risk
// analysis is still pending — the sweep is asynchronous and risk_analyzed_at
// can be reset to NULL on an update, so prose without a completed analysis has
// unknown findings and must not be served. Distinct from
// withheldContentPlaceholder so the two withhold causes stay distinguishable.
const unanalyzedContentPlaceholder = "[content withheld: risk analysis pending]"

type contentMaskSpan struct {
	start   int
	end     int
	display string
	// match is the finding's recorded matched text. Offsets alone cannot be
	// trusted: an in-range span over content that drifted since the scan
	// would mask unrelated bytes while the actual finding survives elsewhere,
	// so applyContentMasks refuses any span whose selected bytes differ.
	match string
}

// findingMasks is maskFindingSpans's result: the substitute content per
// affected message, the number of finding spans neutralized (masked in place
// or covered by a whole-message withholding), and how many messages had their
// prose withheld because a finding could not be masked precisely.
type findingMasks struct {
	content     map[uuid.UUID]string
	neutralized int
	withheld    int
}

// maskFindingSpans replaces every content-anchored finding span with
// maskdisplay.Display's partial-mask form — byte-identical with what the
// dashboard already shows masked, so the digest never reveals more than
// existing UI. Fail-closed: a message with any finding that cannot be applied
// exactly (undecodable or absent span data, offsets that don't fit the
// content, overlapping spans, spans anchored somewhere other than the content
// column) has its entire prose replaced by withheldContentPlaceholder rather
// than served with a known finding unmasked.
func maskFindingSpans(rows []platformrepo.ListOwnedChatTranscriptMessagesForRecallRow, findings []platformrepo.ListRiskFindingSpansForRecallRow) findingMasks {
	out := findingMasks{content: nil, neutralized: 0, withheld: 0}
	if len(findings) == 0 {
		return out
	}
	type messageSpans struct {
		spans       []contentMaskSpan
		unappliable int
	}
	byMessage := make(map[uuid.UUID]*messageSpans)
	for _, finding := range findings {
		if !finding.ChatMessageID.Valid {
			continue
		}
		ms := byMessage[finding.ChatMessageID.UUID]
		if ms == nil {
			ms = &messageSpans{spans: nil, unappliable: 0}
			byMessage[finding.ChatMessageID.UUID] = ms
		}
		ruleID := conv.FromPGTextOrEmpty[string](finding.RuleID)
		var entries []findingSpanEntry
		if len(finding.Spans) > 0 && json.Unmarshal(finding.Spans, &entries) != nil {
			// Span data exists but cannot be read.
			ms.unappliable++
			continue
		}
		if len(entries) == 0 {
			if !finding.StartPos.Valid || !finding.EndPos.Valid {
				// A finding with no span data at all cannot be masked.
				ms.unappliable++
				continue
			}
			// Legacy rows carry only the primary span.
			entries = []findingSpanEntry{{
				Match:    conv.FromPGTextOrEmpty[string](finding.Match),
				Field:    "",
				Path:     "",
				StartPos: int(finding.StartPos.Int32),
				EndPos:   int(finding.EndPos.Int32),
			}}
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Field, "tool.") {
				// Spans anchored in tool-call structures are never rendered:
				// tool payloads are redacted unconditionally.
				continue
			}
			switch scanners.FindingSurface(finding.Source, entry.Field, entry.Path) {
			case scanners.SurfaceContent:
				expected := entry.Match
				if expected == "" {
					// Older span entries omit the per-span match; the
					// finding's primary match is the same text for them.
					expected = conv.FromPGTextOrEmpty[string](finding.Match)
				}
				ms.spans = append(ms.spans, contentMaskSpan{
					start: entry.StartPos,
					end:   entry.EndPos,
					// The resolved match, not entry.Match: a span entry with
					// no per-span match would otherwise render an empty
					// display, deleting the bytes instead of masking them.
					display: maskdisplay.Display(finding.Source, ruleID, expected),
					match:   expected,
				})
			case scanners.SurfaceDerived, scanners.SurfaceNone:
				// Derived metadata (a tool name, an account email, a judge
				// verdict): the offsets index no rendered text, and the
				// dashboard shows these findings without masking prose.
			default:
				// json_path (offsets index a `.get(...)`-extracted sub-value,
				// not the outer content) or an unrecognized attribution:
				// cannot be applied precisely.
				ms.unappliable++
			}
		}
	}
	out.content = make(map[uuid.UUID]string, len(byMessage))
	for _, row := range rows {
		ms, ok := byMessage[row.ID]
		if !ok || (len(ms.spans) == 0 && ms.unappliable == 0) {
			continue
		}
		if ms.unappliable == 0 {
			if content, ok := applyContentMasks(row.Content, ms.spans); ok {
				out.content[row.ID] = content
				out.neutralized += len(ms.spans)
				continue
			}
		}
		out.content[row.ID] = withheldContentPlaceholder
		out.neutralized += ms.unappliable + len(ms.spans)
		out.withheld++
	}
	return out
}

// applyContentMasks rebuilds content with every span replaced by its display
// form. Fail-closed: ok is false when any span cannot be applied exactly —
// offsets outside the content, an empty or inverted range, spans that
// partially overlap, or selected bytes that differ from the finding's
// recorded match (in-range but stale offsets). Identical (start,end)
// duplicates from separate findings target the same region and collapse into
// one replacement.
func applyContentMasks(content string, spans []contentMaskSpan) (string, bool) {
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end < spans[j].end
	})
	applied := make([]contentMaskSpan, 0, len(spans))
	for _, span := range spans {
		if span.start < 0 || span.end <= span.start || span.end > len(content) {
			return "", false
		}
		if span.match == "" || content[span.start:span.end] != span.match {
			return "", false
		}
		if n := len(applied); n > 0 {
			prev := applied[n-1]
			if span.start == prev.start && span.end == prev.end {
				continue
			}
			if span.start < prev.end {
				return "", false
			}
		}
		applied = append(applied, span)
	}
	var b strings.Builder
	b.Grow(len(content))
	last := 0
	for _, span := range applied {
		b.WriteString(content[last:span.start])
		b.WriteString(span.display)
		last = span.end
	}
	b.WriteString(content[last:])
	return b.String(), true
}

// transcriptTurns is what turnsFromRows extracts from the stored message rows.
type transcriptTurns struct {
	turns []sessionhandoff.Turn
	// offloaded counts messages whose body lives in asset storage and is not
	// included in the digest.
	offloaded int
	// unanalyzed counts messages risk analysis had not yet completed for;
	// their prose is withheld (replaced by unanalyzedContentPlaceholder).
	unanalyzed int
	// undecodedToolCalls counts assistant messages whose recorded tool_calls
	// payload carried tool activity that could not be decoded, so the digest
	// is missing their tool names and touched files.
	undecodedToolCalls int
	// source is the latest message's capture surface, empty when none recorded.
	source string
}

// turnsFromRows adapts chronological transcript rows to the renderer's neutral
// turn model, substituting findings-masked content where available. Prose from
// rows whose risk analysis has not completed is withheld (fail closed: its
// findings are unknown); tool-call names from such rows still render, because
// the recall path redacts tool payloads unconditionally and names come from
// the tool_calls column, not the unanalyzed prose.
func turnsFromRows(rows []platformrepo.ListOwnedChatTranscriptMessagesForRecallRow, masked map[uuid.UUID]string) transcriptTurns {
	out := transcriptTurns{turns: make([]sessionhandoff.Turn, 0, len(rows)), offloaded: 0, unanalyzed: 0, undecodedToolCalls: 0, source: ""}
	for _, row := range rows {
		if row.Role == "system" {
			continue
		}
		if source := strings.TrimSpace(conv.FromPGTextOrEmpty[string](row.Source)); source != "" {
			// Rows are chronological, so the last non-empty source wins.
			out.source = source
		}
		content := row.Content
		if replacement, ok := masked[row.ID]; ok {
			content = replacement
		}
		offloadedBody := content == "" && conv.FromPGTextOrEmpty[string](row.ContentAssetUrl) != ""
		unanalyzed := !offloadedBody && content != "" && !row.RiskAnalyzedAt.Valid
		if offloadedBody {
			out.offloaded++
		} else if unanalyzed {
			out.unanalyzed++
		}
		at := row.CreatedAt.Time
		switch row.Role {
		case "user":
			if offloadedBody {
				continue
			}
			if text := chat.StripLeadingEnvelopes(content); strings.TrimSpace(text) != "" {
				if unanalyzed {
					text = unanalyzedContentPlaceholder
				}
				out.turns = append(out.turns, sessionhandoff.Turn{Role: "user", Text: text, ToolName: "", ToolInput: "", ToolResult: "", At: at})
			}
		case "assistant":
			if !offloadedBody && strings.TrimSpace(content) != "" {
				text := content
				if unanalyzed {
					text = unanalyzedContentPlaceholder
				}
				out.turns = append(out.turns, sessionhandoff.Turn{Role: "assistant", Text: text, ToolName: "", ToolInput: "", ToolResult: "", At: at})
			}
			calls, undecoded := decodeToolCalls(row.ToolCalls)
			if undecoded {
				out.undecodedToolCalls++
			}
			for _, call := range calls {
				out.turns = append(out.turns, sessionhandoff.Turn{Role: "assistant", Text: "", ToolName: call.name, ToolInput: call.input, ToolResult: "", At: at})
			}
		case "tool":
			if offloadedBody || content == "" {
				continue
			}
			out.turns = append(out.turns, sessionhandoff.Turn{Role: "tool", Text: "", ToolName: "", ToolInput: "", ToolResult: content, At: at})
		}
	}
	return out
}

type recalledToolCall struct {
	name  string
	input string
}

// decodeToolCalls extracts the tool invocations from a chat_messages.tool_calls
// JSONB payload ([{id,type,function:{name,arguments}}]), unwrapping one level
// of string encoding for payloads stored as a JSON string containing the
// array. A malformed payload never fails the recall, but undecoded reports
// when the payload carried tool activity that yielded no calls, so the
// fidelity report can say tool context was lost instead of dropping it
// silently.
func decodeToolCalls(raw []byte) (calls []recalledToolCall, undecoded bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, false
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		var quoted string
		if err := json.Unmarshal(raw, &quoted); err != nil {
			return nil, true
		}
		if strings.TrimSpace(quoted) == "" {
			return nil, false
		}
		if err := json.Unmarshal([]byte(quoted), &elements); err != nil {
			return nil, true
		}
	}
	out := make([]recalledToolCall, 0, len(elements))
	skipped := 0
	for _, element := range elements {
		var call struct {
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		}
		// Elements decode independently: one malformed entry means that
		// call's activity was lost — the valid calls around it still render,
		// and the loss reports.
		if json.Unmarshal(element, &call) != nil || call.Function.Name == "" {
			skipped++
			continue
		}
		out = append(out, recalledToolCall{name: call.Function.Name, input: toolArgumentsText(call.Function.Arguments)})
	}
	return out, skipped > 0
}

// toolArgumentsText normalizes the OpenAI-style arguments value, which capture
// stores as a JSON string containing the marshaled input, to the inner JSON
// text. The recall path renders with tool payloads redacted, so this text
// never reaches the digest; it rides on the neutral turn model for renderers
// that do serve payloads.
func toolArgumentsText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var quoted string
	if err := json.Unmarshal(raw, &quoted); err == nil {
		return quoted
	}
	return string(raw)
}

// truncateDigest enforces recallDigestByteCap on the rendered digest — a
// backstop over the renderer's own bounds. The cut lands on a rune boundary
// and the marker is appended within the cap, so the result is valid UTF-8 and
// never exceeds recallDigestByteCap bytes.
func truncateDigest(digest string) (string, bool) {
	if len(digest) <= recallDigestByteCap {
		return digest, false
	}
	keep := recallDigestByteCap - len(recallDigestTruncationMarker)
	for keep > 0 && !utf8.RuneStart(digest[keep]) {
		keep--
	}
	return digest[:keep] + recallDigestTruncationMarker, true
}
