import { scanTargetAccessOf } from "@/components/shadow-ai/access";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";

export type AccessDecision = "approved" | "blocked" | "unreviewed";

/** One row of the Access step: a target of the organization's scan target
 *  library, with the device agent's evidence for it when there is any. The
 *  decision and whether it can be enforced come from the target itself; the
 *  detection only adds evidence. */
export interface AccessRow {
  target: AiScanTarget;
  /** The detection for this target, when the device agent has seen it. */
  detection: AIDetection | undefined;
  /** Whether a decision about the target has anything to enforce. */
  enforceable: boolean;
  /** The decision on file. */
  stored: AccessDecision;
  rationale: string | undefined;
}

/** The whole library, each target joined with its detection. Library order
 *  is kept; grouping is the step's job. */
export function buildAccessRows(
  targets: readonly AiScanTarget[],
  detections: readonly AIDetection[],
): AccessRow[] {
  const byTarget = new Map(
    detections.map((detection) => [detection.targetId, detection]),
  );
  return targets.map((target) => {
    const access = scanTargetAccessOf(target);
    return {
      target,
      detection: byTarget.get(target.id),
      enforceable: access.enforceable,
      stored: access.decision,
      rationale: access.rationale,
    };
  });
}

/** What the row shows: the draft's decision when one was made, else the one
 *  on file. */
export function effectiveDecision(
  row: AccessRow,
  draft: ReadonlyMap<string, AccessDecision>,
): AccessDecision {
  return draft.get(row.target.id) ?? row.stored;
}

export interface DecisionChange {
  targetId: string;
  displayName: string;
  decision: AccessDecision;
  /** An omitted rationale is stored as empty, so the one on file rides along. */
  rationale: string | undefined;
}

/** The decisions that differ from what is on file: what Create or Save
 *  writes. A draft entry equal to the stored decision is not a change, so a
 *  withdrawn press costs nothing, and nothing is written for a target no
 *  decision could be enforced for. */
export function changedDecisions(
  rows: readonly AccessRow[],
  draft: ReadonlyMap<string, AccessDecision>,
): DecisionChange[] {
  return rows.flatMap((row) => {
    const decision = draft.get(row.target.id);
    if (!row.enforceable || decision === undefined || decision === row.stored) {
      return [];
    }
    return [
      {
        targetId: row.target.id,
        displayName: row.target.displayName,
        decision,
        rationale: row.rationale,
      },
    ];
  });
}

export interface DecisionCounts {
  approved: number;
  blocked: number;
  unreviewed: number;
  inventoryOnly: number;
}

export function countDecisions(
  rows: readonly AccessRow[],
  draft: ReadonlyMap<string, AccessDecision>,
): DecisionCounts {
  const counts: DecisionCounts = {
    approved: 0,
    blocked: 0,
    unreviewed: 0,
    inventoryOnly: 0,
  };
  for (const row of rows) {
    if (!row.enforceable) {
      counts.inventoryOnly += 1;
      continue;
    }
    counts[effectiveDecision(row, draft)] += 1;
  }
  return counts;
}
