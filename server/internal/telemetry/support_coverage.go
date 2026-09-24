package telemetry

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/agentsurface"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const defaultSupportCoverageWindowDays = 30

// coverageCapabilities are the matrix rows, in render order.
var coverageCapabilities = []string{"session", "blocking", "identity", "cost", "shadow"}

// claudeProviderSurfaces are the surfaces a bare "claude" provider could mean.
// The Claude hook path records one provider for three products, so a block it
// wrote cannot be pinned to a single column.
var claudeProviderSurfaces = []agentsurface.Surface{
	agentsurface.SurfaceClaudeCode,
	agentsurface.SurfaceCowork,
	agentsurface.SurfaceClaudeChat,
}

// blockProviderSurfaces returns the surfaces a tool_call_blocks.provider could
// have been returned to. The unified ingest path writes the sender's adapter
// slug into the column, so a provider is a hook_source and folds the same way.
func blockProviderSurfaces(provider string) []agentsurface.Surface {
	if agentsurface.Normalize(provider) == "claude" {
		return claudeProviderSurfaces
	}
	if surface, _ := agentsurface.ForHookSource(provider, ""); surface != agentsurface.SurfaceUnknown {
		return []agentsurface.Surface{surface}
	}
	return nil
}

// surfaceEvidence is the assembled per-surface evidence behind one column.
type surfaceEvidence struct {
	sessions           int64
	tokens             int64
	attributedSessions int64
	deviceOnlySessions int64
	lastSeen           time.Time

	blocks             int64
	blocksLastSeen     time.Time
	blocksProviderOnly bool

	shadowServers  int64
	shadowLastSeen time.Time
}

// GetSupportCoverage assembles the org's observed coverage matrix. Every
// (capability, surface) pair is always returned with an explicit status, so
// callers must never infer meaning from an absent cell.
func (s *Service) GetSupportCoverage(ctx context.Context, payload *telem_gen.GetSupportCoveragePayload) (*telem_gen.SupportCoverageResult, error) {
	windowDays := defaultSupportCoverageWindowDays
	if payload != nil && payload.WindowDays > 0 {
		windowDays = payload.WindowDays
	}
	// Platform-admin, not just org membership: this is a support view, and the
	// dashboard's PlatformAdminGate is presentation only.
	if _, _, err := auth.RequirePlatformAdmin(ctx, s.logger); err != nil {
		return nil, err
	}

	to := time.Now().UTC()
	from := to.AddDate(0, 0, -windowDays)

	scope, err := s.resolveOrgQueryScope(ctx, from.Format(time.RFC3339), to.Format(time.RFC3339), nil)
	if err != nil {
		return nil, err
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	evidence := make(map[agentsurface.Surface]*surfaceEvidence, len(agentsurface.All))
	for _, surface := range agentsurface.All {
		evidence[surface] = &surfaceEvidence{} //exhaustruct:ignore
	}
	unmapped := make(map[string]int64)

	if err := s.collectSessionEvidence(ctx, scope, from, to, evidence, unmapped); err != nil {
		return nil, err
	}
	if err := s.collectShadowEvidence(ctx, scope, from, to, evidence, unmapped); err != nil {
		return nil, err
	}
	if err := s.collectBlockEvidence(ctx, scope, authCtx.ActiveOrganizationID, from, to, evidence, unmapped); err != nil {
		return nil, err
	}

	return &telem_gen.SupportCoverageResult{
		Cells:      buildCoverageCells(evidence),
		Unmapped:   buildUnmappedList(unmapped),
		WindowDays: windowDays,
		From:       from.Format(time.RFC3339),
		To:         to.Format(time.RFC3339),
	}, nil
}

func (s *Service) collectSessionEvidence(ctx context.Context, scope orgQueryScope, from, to time.Time, evidence map[agentsurface.Surface]*surfaceEvidence, unmapped map[string]int64) error {
	rows, err := s.chRepo.ListSurfaceEvidence(ctx, repoSurfaceEvidenceParams(scope, from, to))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read surface activity evidence")
	}
	for _, row := range rows {
		surface, known := agentsurface.ForHookSource(row.HookSource, "")
		if surface == agentsurface.SurfaceUnknown {
			// Unrecognized and recognized-but-ambiguous sources are both
			// reported: the activity is real and absent from the matrix.
			_ = known
			unmapped[row.HookSource] += clampCount(row.Sessions)
			continue
		}
		item := evidence[surface]
		item.sessions += clampCount(row.Sessions)
		item.tokens += row.Tokens
		item.attributedSessions += clampCount(row.AttributedSessions)
		item.deviceOnlySessions += clampCount(row.DeviceOnlySessions)
		if row.LastSeen.After(item.lastSeen) {
			item.lastSeen = row.LastSeen
		}
	}
	return nil
}

func (s *Service) collectShadowEvidence(ctx context.Context, scope orgQueryScope, from, to time.Time, evidence map[agentsurface.Surface]*surfaceEvidence, unmapped map[string]int64) error {
	rows, err := s.chRepo.ListSurfaceShadowExposure(ctx, repoSurfaceEvidenceParams(scope, from, to))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read surface shadow exposure")
	}
	// Counted over distinct (surface, server) pairs: one server reached under
	// two aliases of the same surface arrives as two rows.
	serversBySurface := make(map[agentsurface.Surface]map[string]bool, len(agentsurface.All))
	for _, row := range rows {
		surface, _ := agentsurface.ForHookSource(row.HookSource, "")
		if surface == agentsurface.SurfaceUnknown {
			// Zero delta: shadow rows carry no session count, but the source
			// still needs reporting.
			unmapped[row.HookSource] += 0
			continue
		}
		servers := serversBySurface[surface]
		if servers == nil {
			servers = make(map[string]bool)
			serversBySurface[surface] = servers
		}
		servers[row.CanonicalServerURL] = true
		item := evidence[surface]
		if row.LastSeen.After(item.shadowLastSeen) {
			item.shadowLastSeen = row.LastSeen
		}
	}
	for surface, servers := range serversBySurface {
		evidence[surface].shadowServers = int64(len(servers))
	}
	return nil
}

// collectBlockEvidence attributes synchronous policy decisions to surfaces.
func (s *Service) collectBlockEvidence(ctx context.Context, scope orgQueryScope, orgID string, from, to time.Time, evidence map[agentsurface.Surface]*surfaceEvidence, unmapped map[string]int64) error {
	blocks, err := s.hooksRepo.ListToolCallBlockSurfaceEvidence(ctx, hooksRepo.ListToolCallBlockSurfaceEvidenceParams{
		OrganizationID: orgID,
		ProjectIds:     scope.projectUUIDs,
		FromTime:       pgtype.Timestamptz{Time: from, Valid: true, InfinityModifier: pgtype.Finite},
		ToTime:         pgtype.Timestamptz{Time: to, Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read policy enforcement evidence")
	}

	chatIDs := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.ChatID.Valid {
			chatIDs = append(chatIDs, block.ChatID.UUID.String())
		}
	}
	hookSources, err := s.chRepo.MapChatHookSources(ctx, scope.projectIDs, chatIDs)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to attribute policy decisions to surfaces")
	}

	for _, block := range blocks {
		lastSeen := time.Time{}
		if block.LastBlockAt.Valid {
			lastSeen = block.LastBlockAt.Time.UTC()
		}

		surface := agentsurface.SurfaceUnknown
		if block.ChatID.Valid {
			if hookSource, ok := hookSources[block.ChatID.UUID.String()]; ok {
				surface, _ = agentsurface.ForHookSource(hookSource, "")
			}
		}
		if surface != agentsurface.SurfaceUnknown {
			item := evidence[surface]
			item.blocks += block.BlockCount
			if lastSeen.After(item.blocksLastSeen) {
				item.blocksLastSeen = lastSeen
			}
			continue
		}

		// Known only at provider granularity. Counting it per candidate would
		// multiply the org's block total, so it is flagged rather than added.
		candidates := blockProviderSurfaces(block.Provider)
		if len(candidates) == 0 {
			// Unrecognized provider with no chat to fall back on: report it
			// rather than lose the enforcement evidence.
			if provider := strings.TrimSpace(block.Provider); provider != "" {
				unmapped[provider] += 0
			}
			continue
		}
		for _, candidate := range candidates {
			item := evidence[candidate]
			item.blocksProviderOnly = true
			if lastSeen.After(item.blocksLastSeen) {
				item.blocksLastSeen = lastSeen
			}
		}
	}

	return nil
}

func repoSurfaceEvidenceParams(scope orgQueryScope, from, to time.Time) repo.SurfaceEvidenceParams {
	return repo.SurfaceEvidenceParams{GramProjectIDs: scope.projectIDs, From: from, To: to}
}

func buildCoverageCells(evidence map[agentsurface.Surface]*surfaceEvidence) []*telem_gen.SupportCoverageCell {
	cells := make([]*telem_gen.SupportCoverageCell, 0, len(coverageCapabilities)*len(agentsurface.All))
	for _, capability := range coverageCapabilities {
		for _, surface := range agentsurface.All {
			cells = append(cells, buildCoverageCell(capability, surface, evidence[surface]))
		}
	}
	return cells
}

func buildCoverageCell(capability string, surface agentsurface.Surface, item *surfaceEvidence) *telem_gen.SupportCoverageCell {
	cell := &telem_gen.SupportCoverageCell{
		Capability: capability,
		Surface:    string(surface),
		Status:     "none",
		Value:      0,
		Detail:     "",
		LastSeen:   "",
	}

	switch capability {
	case "session":
		cell.Value = item.sessions
		cell.LastSeen = stamp(item.lastSeen)
	case "cost":
		cell.Value = item.tokens
		cell.LastSeen = stamp(item.lastSeen)
	case "identity":
		cell.Value = item.attributedSessions
		cell.LastSeen = stamp(item.lastSeen)
		// A session bound only to a device is not bound to a person, so the
		// split is stated rather than summed.
		switch {
		case item.attributedSessions > 0 && item.deviceOnlySessions > 0:
			cell.Detail = fmt.Sprintf("%d device-only", item.deviceOnlySessions)
		case item.attributedSessions == 0 && item.deviceOnlySessions > 0:
			cell.Status = "none"
			cell.Detail = fmt.Sprintf("%d sessions bound to a device only", item.deviceOnlySessions)
		}
	case "blocking":
		cell.Value = item.blocks
		cell.LastSeen = stamp(item.blocksLastSeen)
		if item.blocks == 0 && item.blocksProviderOnly {
			// Blocks happened for this provider but none could be pinned to
			// this surface's sessions.
			cell.Status = "pending"
			cell.Detail = "blocks recorded at provider granularity only"
			return cell
		}
		if item.blocksProviderOnly {
			cell.Detail = "plus provider-level blocks with no resolvable session"
		}
	case "shadow":
		cell.Value = item.shadowServers
		cell.LastSeen = stamp(item.shadowLastSeen)
	}

	if cell.Value > 0 {
		cell.Status = "observed"
	} else {
		cell.LastSeen = ""
	}
	return cell
}

func buildUnmappedList(unmapped map[string]int64) []*telem_gen.SupportCoverageUnmapped {
	list := make([]*telem_gen.SupportCoverageUnmapped, 0, len(unmapped))
	for hookSource, sessions := range unmapped {
		list = append(list, &telem_gen.SupportCoverageUnmapped{HookSource: hookSource, Sessions: sessions})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Sessions != list[j].Sessions {
			return list[i].Sessions > list[j].Sessions
		}
		return list[i].HookSource < list[j].HookSource
	})
	return list
}

// clampCount narrows a ClickHouse UInt64 to the int64 the API exposes. The
// ceiling is unreachable in practice; this keeps the conversion total.
func clampCount(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

func stamp(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}
