import { createHash } from "node:crypto";

import type { Answer } from "./jev.ts";
import {
  QUESTION_BROKEN,
  QUESTION_STATE,
  type ScreenState,
} from "./questions.ts";

// Thresholds are NOT calibrated yet. They are the prompt-injection prefilter's
// 0.50 for a noul, and a margin above it for a choice so a near-tie between
// two states reads as uncertain rather than as a regression. Re-derive both
// from a golden run once there is one; see README.md.
export const CHOICE_CONFIDENCE = 0.6;
export const BROKEN_THRESHOLD = 0.5;

export type Verdict = "pass" | "fail" | "uncertain" | "skipped";

export type CaseRecord = {
  run_id: string;
  commit: string;
  at: string;
  screen: string;
  mutation: string;
  expected: ScreenState[];
  verdict: Verdict;
  reason: string;
  state_choice?: string;
  state_probabilities?: Record<string, number>;
  broken?: number;
  model?: string;
  input_tokens?: number;
  output_tokens?: number;
  latency_ms?: number;
  snapshot_chars?: number;
  snapshot_sha256?: string;
  snapshot_truncated?: boolean;
};

export type Judgement = {
  verdict: Verdict;
  reason: string;
};

export function judge(
  expected: ReadonlySet<ScreenState>,
  answers: Record<string, Answer>,
): Judgement {
  const state = answers[QUESTION_STATE];
  const broken = answers[QUESTION_BROKEN]?.noul;
  const choice = state?.choice;
  const p = choice ? (state?.probabilities?.[choice] ?? 0) : 0;
  if (!choice || broken === undefined) {
    return { verdict: "uncertain", reason: "incomplete answer" };
  }
  const brokenUnexpected =
    broken >= BROKEN_THRESHOLD && !expected.has("broken");
  if (brokenUnexpected) {
    return {
      verdict: "fail",
      reason: `broken=${broken.toFixed(2)} on a case expected ${[...expected].join("|")}`,
    };
  }
  if (expected.has(choice as ScreenState)) {
    if (p >= CHOICE_CONFIDENCE)
      return { verdict: "pass", reason: `${choice}@${p.toFixed(2)}` };
    return {
      verdict: "uncertain",
      reason: `${choice}@${p.toFixed(2)} below ${CHOICE_CONFIDENCE}`,
    };
  }
  if (p >= CHOICE_CONFIDENCE) {
    return {
      verdict: "fail",
      reason: `saw ${choice}@${p.toFixed(2)}, expected ${[...expected].join("|")}`,
    };
  }
  return {
    verdict: "uncertain",
    reason: `saw ${choice}@${p.toFixed(2)} (low confidence), expected ${[...expected].join("|")}`,
  };
}

export function sha256(text: string): string {
  return createHash("sha256").update(text).digest("hex");
}

export function toLine(record: CaseRecord): string {
  return JSON.stringify(record) + "\n";
}

export function summarize(records: CaseRecord[]): Record<Verdict, number> {
  const out: Record<Verdict, number> = {
    pass: 0,
    fail: 0,
    uncertain: 0,
    skipped: 0,
  };
  for (const r of records) out[r.verdict] += 1;
  return out;
}
