import type { Telemetry } from "@/contexts/Telemetry";
import type { ResourceSearchMetrics } from "./candidates/useResourceSearchCandidates";

export const COMMAND_PALETTE_PERFORMANCE_EVENT =
  "command_palette_open_performance";

const RESOURCE_QUIET_WINDOW_MS = 300;

export interface CommandPalettePerformancePayload {
  schema_version: 4;
  scope: "organization" | "project";
  cache_state: "cold" | "warm";
  candidate_count_at_open: number;
  candidate_count_final: number;
  page_action_count_at_open: number;
  time_to_page_actions_ms: number | null;
  time_to_first_resource_ms: number | null;
  time_to_resource_quiet_ms: number | null;
  resource_quiet_before_close: boolean;
  open_duration_ms: number;
  ranking_invocation_count: number;
  ranking_duration_ms: number;
  ranking_max_duration_ms: number;
  inventory_call_baseline: 17;
  searched_resource_kind_count: number;
  net_eliminated_inventory_call_baseline: number;
  remaining_inventory_call_baseline: number;
  search_request_count: number;
  modeled_no_fallback_call_count: number;
  search_candidate_count: number;
  search_complete: boolean;
  search_truncated: boolean;
  search_truncated_kind_count: number;
  search_payload_byte_bucket: ResourceSearchMetrics["payloadByteBucket"];
  time_to_search_ready_ms: number | null;
  parse_search_duration_ms: number | null;
  fallback_used: boolean;
}

interface Session {
  startedAt: number;
  lastCandidateChangeAt: number;
  candidateCountAtOpen: number;
  candidateCount: number;
  pageActionCountAtOpen: number;
  firstPageActionAt: number | null;
  firstResourceAt: number | null;
  warm: boolean;
  rankingInvocationCount: number;
  rankingDuration: number;
  rankingMaxDuration: number;
  searchRequestCount: number;
  searchCandidateCount: number;
  searchComplete: boolean;
  searchTruncated: boolean;
  searchTruncatedKindCount: number;
  searchPayloadByteBucket: ResourceSearchMetrics["payloadByteBucket"];
  searchReadyAt: number | null;
  parseSearchDuration: number | null;
  fallbackUsed: boolean;
}

/**
 * One low-cardinality measurement is emitted when each committed palette open
 * closes. "Warm" means resource candidates were synchronously available on the
 * first committed open render; "cold" means they were not. Resource quiet is
 * 300ms without a candidate-count change. It is deliberately not called
 * source-settled: candidate hooks do not expose request/page lifecycle, and
 * inferring that from shared React Query traffic would be unreliable.
 *
 * The payload contains only fixed enums, counts, and durations. Candidate
 * names/IDs, raw queries, routes, tenant identifiers, and source names are
 * intentionally impossible to pass to this API. The event is emitted on close
 * rather than open so ranking and async candidate timings fit in one event.
 */
export class CommandPalettePerformanceTracker {
  private session: Session | null = null;

  open(
    now: number,
    candidates: { total: number; pages: number; resources: number },
  ): void {
    if (this.session) return;
    this.session = {
      startedAt: now,
      lastCandidateChangeAt: now,
      candidateCountAtOpen: candidates.total,
      candidateCount: candidates.total,
      pageActionCountAtOpen: candidates.pages,
      firstPageActionAt: candidates.pages > 0 ? now : null,
      firstResourceAt: candidates.resources > 0 ? now : null,
      warm: candidates.resources > 0,
      rankingInvocationCount: 0,
      rankingDuration: 0,
      rankingMaxDuration: 0,
      searchRequestCount: 0,
      searchCandidateCount: 0,
      searchComplete: false,
      searchTruncated: false,
      searchTruncatedKindCount: 0,
      searchPayloadByteBucket: null,
      searchReadyAt: null,
      parseSearchDuration: null,
      fallbackUsed: false,
    };
  }

  candidates(
    now: number,
    total: number,
    pages: number,
    resources: number,
  ): void {
    const session = this.session;
    if (!session) return;
    if (total !== session.candidateCount) {
      session.candidateCount = total;
      session.lastCandidateChangeAt = now;
    }
    if (pages > 0 && session.firstPageActionAt === null) {
      session.firstPageActionAt = now;
    }
    if (resources > 0 && session.firstResourceAt === null) {
      session.firstResourceAt = now;
    }
  }

  search(now: number, metrics: ResourceSearchMetrics): void {
    const session = this.session;
    if (!session) return;
    session.searchRequestCount = Math.max(
      session.searchRequestCount,
      metrics.searchRequestCount,
    );
    session.fallbackUsed ||= metrics.state === "fallback";
    // Cached rows can coexist with an issued refetch. Only a settled response
    // may finalize result metadata, including valid zero-candidate outcomes.
    if (metrics.searchInFlight) return;
    if (metrics.state !== "ready" && metrics.state !== "fallback") return;
    session.searchCandidateCount = metrics.candidateCount;
    session.searchComplete = metrics.complete;
    session.searchTruncated = metrics.truncated;
    session.searchTruncatedKindCount = metrics.truncatedKindCount;
    session.searchPayloadByteBucket = metrics.payloadByteBucket;
    session.parseSearchDuration = metrics.parseSearchDurationMs;
    session.searchReadyAt ??= now;
  }

  ranking(durationMs: number): void {
    const session = this.session;
    if (!session) return;
    const bounded = Math.max(0, durationMs);
    session.rankingInvocationCount += 1;
    session.rankingDuration += bounded;
    session.rankingMaxDuration = Math.max(session.rankingMaxDuration, bounded);
  }

  close(
    now: number,
    scope: "organization" | "project",
  ): CommandPalettePerformancePayload | null {
    const session = this.session;
    if (!session) return null;
    this.session = null;
    const openDuration = Math.max(0, now - session.startedAt);
    const quietAt = session.lastCandidateChangeAt + RESOURCE_QUIET_WINDOW_MS;
    const quiet = quietAt <= now;
    return {
      schema_version: 4,
      scope,
      cache_state: session.warm ? "warm" : "cold",
      candidate_count_at_open: session.candidateCountAtOpen,
      candidate_count_final: session.candidateCount,
      page_action_count_at_open: session.pageActionCountAtOpen,
      time_to_page_actions_ms:
        session.firstPageActionAt === null
          ? null
          : Math.max(0, session.firstPageActionAt - session.startedAt),
      time_to_first_resource_ms:
        session.firstResourceAt === null
          ? null
          : Math.max(0, session.firstResourceAt - session.startedAt),
      time_to_resource_quiet_ms: quiet
        ? Math.max(0, quietAt - session.startedAt)
        : null,
      resource_quiet_before_close: quiet,
      open_duration_ms: openDuration,
      ranking_invocation_count: session.rankingInvocationCount,
      ranking_duration_ms: session.rankingDuration,
      ranking_max_duration_ms: session.rankingMaxDuration,
      // Seven collection-authorized kinds are searched. Live MCP actions still
      // need their shared inventory, so six list requests are net eliminated.
      // This is an explicit no-fallback model, not a measured total.
      inventory_call_baseline: 17,
      searched_resource_kind_count: scope === "project" ? 7 : 0,
      net_eliminated_inventory_call_baseline: scope === "project" ? 6 : 0,
      remaining_inventory_call_baseline: scope === "project" ? 11 : 17,
      search_request_count: session.searchRequestCount,
      modeled_no_fallback_call_count:
        (scope === "project" ? 11 : 17) + session.searchRequestCount,
      search_candidate_count: session.searchCandidateCount,
      search_complete: session.searchComplete,
      search_truncated: session.searchTruncated,
      search_truncated_kind_count: session.searchTruncatedKindCount,
      search_payload_byte_bucket: session.searchPayloadByteBucket,
      time_to_search_ready_ms:
        session.searchReadyAt === null
          ? null
          : Math.max(0, session.searchReadyAt - session.startedAt),
      parse_search_duration_ms: session.parseSearchDuration,
      fallback_used: session.fallbackUsed,
    };
  }
}

export function captureCommandPalettePerformance(
  telemetry: Pick<Telemetry, "capture">,
  payload: CommandPalettePerformancePayload,
): void {
  telemetry.capture(COMMAND_PALETTE_PERFORMANCE_EVENT, payload);
}
