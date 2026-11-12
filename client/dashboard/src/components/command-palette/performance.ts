import type { Telemetry } from "@/contexts/Telemetry";

export const COMMAND_PALETTE_PERFORMANCE_EVENT =
  "command_palette_open_performance";

const RESOURCE_QUIET_WINDOW_MS = 300;

export interface CommandPalettePerformancePayload {
  schema_version: 1;
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
}

interface Session {
  startedAt: number;
  lastCandidateChangeAt: number;
  candidateCountAtOpen: number;
  candidateCount: number;
  pageActionCountAtOpen: number;
  firstResourceAt: number | null;
  rankingInvocationCount: number;
  rankingDuration: number;
  rankingMaxDuration: number;
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
      firstResourceAt: candidates.resources > 0 ? now : null,
      rankingInvocationCount: 0,
      rankingDuration: 0,
      rankingMaxDuration: 0,
    };
  }

  candidates(now: number, total: number, resources: number): void {
    const session = this.session;
    if (!session) return;
    if (total !== session.candidateCount) {
      session.candidateCount = total;
      session.lastCandidateChangeAt = now;
    }
    if (resources > 0 && session.firstResourceAt === null) {
      session.firstResourceAt = now;
    }
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
      schema_version: 1,
      scope,
      cache_state:
        session.firstResourceAt === session.startedAt ? "warm" : "cold",
      candidate_count_at_open: session.candidateCountAtOpen,
      candidate_count_final: session.candidateCount,
      page_action_count_at_open: session.pageActionCountAtOpen,
      time_to_page_actions_ms: session.pageActionCountAtOpen > 0 ? 0 : null,
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
    };
  }
}

export function captureCommandPalettePerformance(
  telemetry: Pick<Telemetry, "capture">,
  payload: CommandPalettePerformancePayload,
): void {
  telemetry.capture(COMMAND_PALETTE_PERFORMANCE_EVENT, payload);
}
