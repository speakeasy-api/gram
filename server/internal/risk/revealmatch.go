package risk

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tidwall/gjson"

	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Surface values read off risk_findings.surface: which stored text the row's
// start_pos/end_pos offsets index. Kept in sync with the column's schema
// comment (server/clickhouse/schema.sql) and the backfill's transform
// (server/cmd/tools/migrations/riskfindings/transform.go). The table is
// truncated and fully re-backfilled before this path serves traffic, so every
// live row carries a stamped surface; empty and unrecognized values are
// refused rather than guessed at.
const (
	surfaceContent        = "content"
	surfaceScanSurface    = "scan_surface"
	surfaceToolArgs       = "tool_args"
	surfaceJSONPath       = "json_path"
	surfaceDerived        = "derived"
	surfaceLegacyPresidio = "legacy_presidio"
	surfaceNone           = "none"
)

// maxUnmaskAssetReadSize caps content-part asset reads for reveal, mirroring
// the batch analysis activity's cap (risk_analysis.maxContentPartAssetReadSize)
// so reveal can reconstruct from exactly the text the scanner saw.
const maxUnmaskAssetReadSize = 20 * 1024 * 1024 // 20 MiB

// RevealMatcher reconstructs a ClickHouse finding's raw match text from the
// original chat data (Postgres chat rows plus content-part assets), guided by
// the row's surface metadata. ClickHouse never stores plaintext, so this is
// the only way to recover a match after the fact. Shared by the audited
// unmask endpoint and the retroactive exclusion reconcile's regex evaluation
// — the latter keeps the plaintext strictly in-process, mirroring scan-time
// matching, so no unmask audit entry is written there.
type RevealMatcher struct {
	logger *slog.Logger
	repo   *repo.Queries
	// assetStorage may be nil in hosts that cannot read content-part assets;
	// findings anchored to a content part are then unreconstructable and are
	// refused by the match-length gate.
	assetStorage blobio.Reader
}

// NewRevealMatcher assembles a matcher from its dependencies.
func NewRevealMatcher(logger *slog.Logger, repoQueries *repo.Queries, assetStorage blobio.Reader) *RevealMatcher {
	return &RevealMatcher{
		logger:       logger,
		repo:         repoQueries,
		assetStorage: assetStorage,
	}
}

// unmaskToolCall mirrors the recorded tool_calls JSON shape batch analysis
// parses (risk_analysis.recordedToolCall / batch_messages.go).
type unmaskToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// RevealAnchor is the Postgres source material behind one ClickHouse finding:
// the anchored chat message's recorded content and tool calls, or a content
// part's asset location, plus the chat id used for authorization. All loads
// are best-effort — a deleted anchor leaves the relevant fields zeroed and the
// reveal refuses later for lack of candidates.
type RevealAnchor struct {
	// ChatID is the anchored Postgres row's chat, exported so the unmask
	// handler can authorize against it and detect divergence from the chat id
	// stamped on the ClickHouse row.
	ChatID uuid.NullUUID

	// contentOK reports content carries the scanned text: set for a resolved
	// message row immediately, and for a content part only once its asset has
	// been hydrated.
	content   string
	contentOK bool

	// isToolRequest mirrors the batch role→type mapping: only an assistant
	// message with recorded tool calls composes arguments into its scan
	// surface (risk_analysis.messageTypeForRole).
	isToolRequest bool
	toolCalls     []unmaskToolCall

	partAssetURL string
}

// LoadAnchor resolves the finding's Postgres anchor row (chat message or
// content part). Content-part asset bytes are NOT read here — that happens
// after the caller's authorization check, in HydratePartContent.
func (m *RevealMatcher) LoadAnchor(ctx context.Context, projectID uuid.UUID, row *chrepo.RiskFindingUnmaskRow) RevealAnchor {
	var anchor RevealAnchor

	if msgID, err := uuid.Parse(row.ChatMessageID); err == nil {
		msg, err := m.repo.GetChatMessageForUnmask(ctx, repo.GetChatMessageForUnmaskParams{
			ID:        msgID,
			ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				m.logger.WarnContext(ctx, "load chat message for unmask", attr.SlogError(err), attr.SlogMessageID(row.ChatMessageID))
			}
			return anchor
		}
		anchor.ChatID = uuid.NullUUID{UUID: msg.ChatID, Valid: true}
		anchor.content = msg.Content
		anchor.contentOK = true
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			anchor.isToolRequest = true
			anchor.toolCalls = parseUnmaskToolCalls(ctx, m.logger, msg.ToolCalls)
		}
		return anchor
	}

	if partID, err := uuid.Parse(row.ContentPartID); err == nil {
		part, err := m.repo.GetChatContentPartForUnmask(ctx, repo.GetChatContentPartForUnmaskParams{
			ID:        partID,
			ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				m.logger.WarnContext(ctx, "load chat content part for unmask", attr.SlogError(err), attr.SlogValueString(row.ContentPartID))
			}
			return anchor
		}
		anchor.ChatID = uuid.NullUUID{UUID: part.ChatID, Valid: true}
		anchor.partAssetURL = part.ContentAssetUrl
	}

	return anchor
}

// HydratePartContent reads a content-part anchor's asset into the anchor,
// size-capped like the batch scanner's hydration. Best-effort: a missing or
// unreadable asset (or a nil asset reader) just leaves the anchor without
// content and the reveal refuses for lack of candidates.
func (m *RevealMatcher) HydratePartContent(ctx context.Context, anchor *RevealAnchor) {
	if anchor.contentOK || anchor.partAssetURL == "" || m.assetStorage == nil {
		return
	}
	content, err := blobio.ReadAllString(ctx, m.assetStorage, anchor.partAssetURL, maxUnmaskAssetReadSize)
	if err != nil {
		m.logger.WarnContext(ctx, "read content part asset for unmask", attr.SlogError(err))
		return
	}
	anchor.content = content
	anchor.contentOK = true
}

// parseUnmaskToolCalls mirrors risk_analysis.parseRecordedToolCalls, including
// its raw-JSON fallback: scanners composed unparseable tool_calls into the
// scan surface as a single pseudo-call carrying the raw bytes, so reveal must
// recompose the identical text.
func parseUnmaskToolCalls(ctx context.Context, logger *slog.Logger, raw []byte) []unmaskToolCall {
	var calls []unmaskToolCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		logger.WarnContext(ctx, "unmask: failed to parse tool_calls", attr.SlogError(err))
		var fallback unmaskToolCall
		fallback.Function.Name = "tool_calls"
		fallback.Function.Arguments = string(raw)
		return []unmaskToolCall{fallback}
	}
	return calls
}

// scanSurfaceText recomposes the sync-era batch scan surface — message content
// plus each tool call's non-empty arguments, newline-joined — mirroring
// risk_analysis.batchMessage.scanSurface byte for byte (separators only
// between kept members).
func (a RevealAnchor) scanSurfaceText() string {
	if !a.isToolRequest || len(a.toolCalls) == 0 {
		return a.content
	}
	var b strings.Builder
	b.WriteString(a.content)
	for _, call := range a.toolCalls {
		if call.Function.Arguments == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(call.Function.Arguments)
	}
	return b.String()
}

// argsCandidates returns the recorded tool-call argument strings the row's
// offsets may index. A recorded tool call id narrows to its call when it
// resolves; it is matched against call NAMES too because custom-rule spans
// attribute by call name, not recorded id. Rows whose write path decided by
// name alone carry no tool_call_id at all — permanently, not just for old data
// — so the fallback tries every call and the first slice consistent with the
// recorded match length wins. Two calls whose arguments both cover the offsets
// with the same length are then indistinguishable; that ambiguity is an
// accepted tradeoff (offsets plus exact length make collisions unlikely).
func (a RevealAnchor) argsCandidates(toolCallID string) []string {
	if toolCallID != "" {
		var narrowed []string
		for _, call := range a.toolCalls {
			if call.ID == toolCallID || call.Function.Name == toolCallID {
				narrowed = append(narrowed, call.Function.Arguments)
			}
		}
		if len(narrowed) > 0 {
			return narrowed
		}
	}
	all := make([]string, 0, len(a.toolCalls))
	for _, call := range a.toolCalls {
		if call.Function.Arguments == "" {
			continue
		}
		all = append(all, call.Function.Arguments)
	}
	return all
}

// fieldTexts resolves a recorded scanner field to the stored text(s) it
// evaluated, using the same aliases as celenv: every message body field reads
// the content; tool.args reads a call's arguments. Unknown fields resolve to
// nothing so the reveal refuses instead of slicing a guessed text.
func (a RevealAnchor) fieldTexts(field, toolCallID string) []string {
	switch field {
	case "", "content", "prompt", "assistant", "tool_result":
		if !a.contentOK {
			return nil
		}
		return []string{a.content}
	case "tool.args":
		return a.argsCandidates(toolCallID)
	default:
		return nil
	}
}

// sliceCandidate returns text[start:end) when the byte offsets are in bounds.
func sliceCandidate(text string, start, end int32) ([]byte, bool) {
	if start < 0 || end < start || int(end) > len(text) {
		return nil, false
	}
	return []byte(text)[start:end], true
}

// Candidates assembles the ordered reconstruction candidates for a finding
// per its surface metadata. Every candidate still has to pass the
// match-length gate (MatchingReconstruction) before it is used.
func (m *RevealMatcher) Candidates(ctx context.Context, chatID uuid.UUID, row *chrepo.RiskFindingUnmaskRow, anchor RevealAnchor) [][]byte {
	switch row.Surface {
	case surfaceContent:
		if !anchor.contentOK {
			return nil
		}
		if cand, ok := sliceCandidate(anchor.content, row.StartPos, row.EndPos); ok {
			return [][]byte{cand}
		}
		return nil

	case surfaceScanSurface:
		if !anchor.contentOK {
			return nil
		}
		if cand, ok := sliceCandidate(anchor.scanSurfaceText(), row.StartPos, row.EndPos); ok {
			return [][]byte{cand}
		}
		return nil

	case surfaceToolArgs:
		var out [][]byte
		for _, args := range anchor.argsCandidates(row.ToolCallID) {
			if cand, ok := sliceCandidate(args, row.StartPos, row.EndPos); ok {
				out = append(out, cand)
			}
		}
		return out

	case surfaceJSONPath:
		var out [][]byte
		for _, text := range anchor.fieldTexts(row.Field, row.ToolCallID) {
			base := text
			if row.Path != "" {
				res := gjson.Get(text, row.Path)
				if !res.Exists() {
					continue
				}
				// Offsets index the DECODED extracted string, exactly what the
				// scanner matched against (celenv fieldVal.get).
				base = res.String()
			}
			if cand, ok := sliceCandidate(base, row.StartPos, row.EndPos); ok {
				out = append(out, cand)
			}
		}
		return out

	case surfaceDerived:
		return m.derivedCandidates(ctx, chatID, row, anchor)

	case surfaceNone, surfaceLegacyPresidio:
		// Batch-era judge artifacts and legacy presidio transforms index no
		// stored text; there is nothing faithful to reconstruct.
		return nil

	default:
		// Empty and unrecognized surface values are refused the same way: the
		// table is truncated and fully re-backfilled with stamped surfaces
		// before this path serves traffic, so nothing legitimate lands here,
		// and an unknown value must not be guessed at.
		return nil
	}
}

// derivedCandidates rebuilds the match for findings whose match is derived
// metadata rather than a slice of any recorded surface.
func (m *RevealMatcher) derivedCandidates(ctx context.Context, chatID uuid.UUID, row *chrepo.RiskFindingUnmaskRow, anchor RevealAnchor) [][]byte {
	switch row.Source {
	case "account_identity":
		// The match is the chat's AI-account email, re-resolved through the
		// same chats.user_account_id link the scanner used.
		if chatID == uuid.Nil {
			return nil
		}
		email, err := m.repo.GetChatUserAccountEmailForUnmask(ctx, repo.GetChatUserAccountEmailForUnmaskParams{
			ChatID:         chatID,
			OrganizationID: row.OrganizationID,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				m.logger.WarnContext(ctx, "resolve account identity email for unmask", attr.SlogError(err), attr.SlogChatID(chatID.String()))
			}
			return nil
		}
		if v := conv.FromPGTextOrEmpty[string](email); v != "" {
			return [][]byte{[]byte(v)}
		}
		return nil

	case "destructive_tool", "cli_destructive":
		// The match is a tool call name from the anchored message:
		// cli_destructive records the full recorded name, destructive_tool the
		// resolved bare function name (toolref.MCPFunctionOf), so both forms
		// are offered and the match-length gate picks whichever the finding
		// stored (an MCP-routed full name is always longer than its bare form).
		var out [][]byte
		for _, call := range anchor.toolCalls {
			name := call.Function.Name
			if name == "" {
				continue
			}
			out = append(out, []byte(name))
			if bare := toolref.MCPFunctionOf(name); bare != "" && bare != name {
				out = append(out, []byte(bare))
			}
		}
		return out

	case "custom":
		// Custom CEL rules can match on the derived tool.server / tool.function
		// strings, which are computed from a recorded call name rather than
		// stored anywhere. The recorded field selects which derivation to
		// offer: emitting both would let an equal-length value from the other
		// field win the length gate and be served as the match.
		wantFunction := row.Field == "tool.function"
		wantServer := row.Field == "tool.server"
		if !wantFunction && !wantServer {
			return nil
		}
		var out [][]byte
		for _, call := range anchor.toolCalls {
			name := call.Function.Name
			if name == "" {
				continue
			}
			if wantFunction {
				if fn := toolref.MCPFunctionOf(name); fn != "" {
					out = append(out, []byte(fn))
				}
				continue
			}
			if srv := toolref.MCPServerOf(name); srv != "" {
				out = append(out, []byte(srv))
			}
		}
		return out

	case "shadow_mcp":
		// Shadow MCP stores the server identifier verbatim in match_redacted —
		// the documented carve-out for a non-secret value listings already show
		// unmasked — so the stored display string IS the match.
		if row.MatchRedacted == "" {
			return nil
		}
		return [][]byte{[]byte(row.MatchRedacted)}

	default:
		return nil
	}
}

// MatchingReconstruction selects the reconstruction to trust: the first
// candidate whose byte length equals the recorded match_len. The length gate
// is a plain integrity check, not cryptography — a candidate of the wrong
// size proves the underlying data no longer lines up with what was scanned
// (edited content, shifted offsets, changed account email) and using it would
// misattribute bytes as the finding's match. A zero match_len means the
// finding never had match content (judge verdicts, dead-lettered scans).
func MatchingReconstruction(matchLen uint32, candidates [][]byte) (string, bool) {
	if matchLen == 0 {
		return "", false
	}
	for _, cand := range candidates {
		if len(cand) == int(matchLen) {
			return string(cand), true
		}
	}
	return "", false
}

// ReconstructMatch is the one-shot reconstruction used by unauthenticated
// in-process consumers (the retroactive exclusion reconcile): anchor load,
// asset hydration, candidate assembly, and the match-length gate in one call.
// The unmask endpoint uses the granular methods instead because it interposes
// authorization between the anchor load and the asset read.
func (m *RevealMatcher) ReconstructMatch(ctx context.Context, projectID uuid.UUID, row *chrepo.RiskFindingUnmaskRow) (string, bool) {
	anchor := m.LoadAnchor(ctx, projectID, row)

	chatID := uuid.Nil
	if parsed, err := uuid.Parse(row.ChatID); err == nil {
		chatID = parsed
	} else if anchor.ChatID.Valid {
		chatID = anchor.ChatID.UUID
	}

	m.HydratePartContent(ctx, &anchor)
	return MatchingReconstruction(row.MatchLen, m.Candidates(ctx, chatID, row, anchor))
}
