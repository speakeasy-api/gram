/**
 * Work-units analysis helpers. The report shapes mirror the verdict JSON
 * published by the work_units judge
 * (server/internal/chat/analysis/workunits.go): per-task work units plus
 * session-level flags, with `units = base_units × modifier × completion`.
 */

export interface WorkUnitsTask {
  id?: number;
  request?: string;
  band?: string;
  base_units?: number;
  modifier?: number;
  completion?: number;
  units?: number;
  nearest_exemplar?: string;
  rationale?: string;
}

export interface WorkUnitsVerdict {
  tasks?: WorkUnitsTask[];
  session_units?: number;
  flags?: string[];
}

/** Chat fields the work-units displays read; both ChatOverview (list rows)
 * and Chat (loaded session) satisfy it. `workUnitsReport` only arrives on the
 * loaded session. */
export interface WorkUnitsChatFields {
  workUnits?: number | undefined;
  workUnitsReport?: string | undefined;
  totalCost?: number | undefined;
  totalTokens?: number | undefined;
  totalInputTokens?: number | undefined;
  totalOutputTokens?: number | undefined;
}

// Absent covers null too: the server marshals empty Go slices as JSON null.
function isOptionalArrayOf(
  value: unknown,
  ok: (item: unknown) => boolean,
): boolean {
  return value == null || (Array.isArray(value) && value.every(ok));
}

export function parseWorkUnitsReport(json: string): WorkUnitsVerdict | null {
  try {
    const parsed = JSON.parse(json) as unknown;
    if (parsed === null || typeof parsed !== "object") return null;
    const candidate = parsed as Record<string, unknown>;
    // Lightweight shape check so malformed-but-valid JSON falls back to the
    // raw-text display instead of crashing the consumers that .map() these.
    if (
      !isOptionalArrayOf(
        candidate["tasks"],
        (task) =>
          task !== null && typeof task === "object" && !Array.isArray(task),
      ) ||
      !isOptionalArrayOf(candidate["flags"], (flag) => typeof flag === "string")
    ) {
      return null;
    }
    return parsed as WorkUnitsVerdict;
  } catch {
    return null;
  }
}

export function formatWorkUnits(units: number): string {
  return new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 1,
  }).format(units);
}

/** Token/cost efficiency is only meaningful for sessions judged to have
 * delivered positive work; a zero or negative denominator yields nothing. */
export function workUnitsEfficiency(chat: WorkUnitsChatFields): {
  costPerUnit: number | null;
  tokensPerUnit: number | null;
} {
  const units = chat.workUnits;
  if (units === undefined || units <= 0) {
    return { costPerUnit: null, tokensPerUnit: null };
  }
  const tokens =
    chat.totalTokens && chat.totalTokens > 0
      ? chat.totalTokens
      : (chat.totalInputTokens || 0) + (chat.totalOutputTokens || 0);
  return {
    costPerUnit:
      chat.totalCost && chat.totalCost > 0 ? chat.totalCost / units : null,
    tokensPerUnit: tokens > 0 ? tokens / units : null,
  };
}
