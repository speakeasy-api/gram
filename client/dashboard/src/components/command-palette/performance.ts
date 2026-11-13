import type { Telemetry } from "@/contexts/Telemetry";
import type { ProjectResourceIndexMetrics } from "./candidates/useProjectResourceIndexCandidates";

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
  indexed_resource_kind_count: number;
  net_eliminated_inventory_call_baseline: number;
  remaining_inventory_call_baseline: number;
  index_request_count: number;
  modeled_no_fallback_call_count: number;
  index_item_count: number;
  index_complete: boolean;
  index_cache_hit: boolean;
  index_incomplete_kind_count: number;
  index_payload_byte_bucket: ProjectResourceIndexMetrics["payloadByteBucket"];
  time_to_index_ready_ms: number | null;
  index_mapping_duration_ms: number | null;
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
  indexRequestCount: number;
  indexItemCount: number;
  indexComplete: boolean;
  indexCacheHit: boolean;
  indexIncompleteKindCount: number;
  indexPayloadByteBucket: ProjectResourceIndexMetrics["payloadByteBucket"];
  indexReadyAt: number | null;
  mappingDuration: number | null;
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
      indexRequestCount: 0,
      indexItemCount: 0,
      indexComplete: false,
      indexCacheHit: false,
      indexIncompleteKindCount: 0,
      indexPayloadByteBucket: null,
      indexReadyAt: null,
      mappingDuration: null,
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

  index(now: number, metrics: ProjectResourceIndexMetrics): void {
    const session = this.session;
    if (!session) return;
    session.indexRequestCount = Math.max(
      session.indexRequestCount,
      metrics.indexRequestCount,
    );
    session.fallbackUsed ||= metrics.state === "fallback";
    // Cached rows can coexist with an issued refetch. Only a settled response
    // may finalize result metadata, including valid zero-candidate outcomes.
    if (metrics.indexInFlight) return;
    if (metrics.state !== "ready" && metrics.state !== "fallback") return;
    session.indexItemCount = metrics.itemCount;
    session.indexComplete = metrics.complete;
    session.indexCacheHit = metrics.cacheHit;
    session.indexIncompleteKindCount = metrics.incompleteKindCount;
    session.indexPayloadByteBucket = metrics.payloadByteBucket;
    session.mappingDuration = metrics.mappingDurationMs;
    session.indexReadyAt ??= now;
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
      indexed_resource_kind_count: scope === "project" ? 7 : 0,
      net_eliminated_inventory_call_baseline: scope === "project" ? 6 : 0,
      remaining_inventory_call_baseline: scope === "project" ? 11 : 17,
      index_request_count: session.indexRequestCount,
      modeled_no_fallback_call_count:
        (scope === "project" ? 11 : 17) + session.indexRequestCount,
      index_item_count: session.indexItemCount,
      index_complete: session.indexComplete,
      index_cache_hit: session.indexCacheHit,
      index_incomplete_kind_count: session.indexIncompleteKindCount,
      index_payload_byte_bucket: session.indexPayloadByteBucket,
      time_to_index_ready_ms:
        session.indexReadyAt === null
          ? null
          : Math.max(0, session.indexReadyAt - session.startedAt),
      index_mapping_duration_ms: session.mappingDuration,
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
