// Policy Evals (session replay) — AGE-2704
//
// Local, hand-authored types and mock data for the Evals tab scaffold. These
// SHAPES intentionally mirror the not-yet-generated eval API so that swapping
// the mock store for the generated `@gram/client` hooks is a near-mechanical
// change.
//
// TODO(AGE-2704): delete this file's mock data once the generated eval API
// client exists, and replace the local types with the generated models
// (e.g. `@gram/client/models/components/policyevalrun.js`).

/** Lifecycle of a single eval (replay) run over historical chat messages. */
export type PolicyEvalRunStatus =
  | "pending"
  | "running"
  | "completed"
  | "cancelled"
  | "failed";

/**
 * One eval run: replays a candidate policy across a sample of historical
 * messages and records what it would have flagged, plus cost/latency telemetry.
 */
export type PolicyEvalRun = {
  id: string;
  status: PolicyEvalRunStatus;
  /** The risk policy this run evaluates. Optional while a draft policy is being tuned. */
  riskPolicyId?: string;
  createdAt: Date;
  startedAt?: Date;
  completedAt?: Date;
  messagesScanned: number;
  findingsCount: number;
  totalCostUsd: number;
  inputTokens: number;
  outputTokens: number;
  /** Judge call latency percentiles (ms). Undefined until the run produces samples. */
  judgeLatencyP50Ms?: number;
  judgeLatencyP95Ms?: number;
};

/** A single match the policy would have produced against a replayed message. */
export type PolicyEvalFinding = {
  id: string;
  runId: string;
  /** The historical chat message this finding was produced against. */
  chatMessageId: string;
  /** Detector source, e.g. "presidio", "gitleaks", "llm_judge". */
  source: string;
  ruleId?: string;
  description?: string;
  /** Redacted match context. Never render raw matches verbatim. */
  match?: string;
  /** Judge confidence in [0,1] when produced by an LLM judge. */
  confidence?: number;
  createdAt: Date;
  /** Denormalized sample-message context for the findings table. */
  chatTitle?: string;
  chatUserId?: string;
};

// ---------------------------------------------------------------------------
// Mock data — placeholder only. Replace with generated-API-backed queries.
// ---------------------------------------------------------------------------

/**
 * TODO(AGE-2704): replace with generated eval API client.
 * Stand-in for `useListPolicyEvalRuns(riskPolicyId)`.
 */
export function getMockEvalRuns(riskPolicyId: string): PolicyEvalRun[] {
  const now = Date.now();
  const min = 60_000;
  return [
    {
      id: "run_completed_01",
      status: "completed",
      riskPolicyId,
      createdAt: new Date(now - 120 * min),
      startedAt: new Date(now - 119 * min),
      completedAt: new Date(now - 112 * min),
      messagesScanned: 4820,
      findingsCount: 37,
      totalCostUsd: 1.84,
      inputTokens: 612_400,
      outputTokens: 48_900,
      judgeLatencyP50Ms: 410,
      judgeLatencyP95Ms: 1_180,
    },
    {
      id: "run_running_01",
      status: "running",
      riskPolicyId,
      createdAt: new Date(now - 6 * min),
      startedAt: new Date(now - 5 * min),
      messagesScanned: 1430,
      findingsCount: 9,
      totalCostUsd: 0.52,
      inputTokens: 180_200,
      outputTokens: 12_100,
      judgeLatencyP50Ms: 395,
      judgeLatencyP95Ms: 1_050,
    },
    {
      id: "run_failed_01",
      status: "failed",
      riskPolicyId,
      createdAt: new Date(now - 26 * 60 * min),
      startedAt: new Date(now - 26 * 60 * min),
      completedAt: new Date(now - 25 * 60 * min),
      messagesScanned: 210,
      findingsCount: 0,
      totalCostUsd: 0.03,
      inputTokens: 28_000,
      outputTokens: 1_900,
    },
  ];
}

/**
 * TODO(AGE-2704): replace with generated eval API client.
 * Stand-in for `useListPolicyEvalFindings(runId)`.
 */
export function getMockEvalFindings(runId: string): PolicyEvalFinding[] {
  const now = Date.now();
  const min = 60_000;
  return [
    {
      id: "finding_01",
      runId,
      chatMessageId: "msg_7f1a",
      source: "presidio",
      ruleId: "pii.us_ssn",
      description: "Possible US Social Security Number in tool response",
      match: "•••-••-1234",
      confidence: 0.91,
      createdAt: new Date(now - 113 * min),
      chatTitle: "Onboarding a new vendor",
      chatUserId: "user_alice",
    },
    {
      id: "finding_02",
      runId,
      chatMessageId: "msg_3c92",
      source: "gitleaks",
      ruleId: "secret.aws_access_token",
      description: "AWS access key id pasted into prompt",
      match: "AKIA••••••••EXAMPLE",
      confidence: undefined,
      createdAt: new Date(now - 116 * min),
      chatTitle: "Deploy script help",
      chatUserId: "user_bob",
    },
    {
      id: "finding_03",
      runId,
      chatMessageId: "msg_aa01",
      source: "llm_judge",
      ruleId: undefined,
      description: "Assistant disclosed internal-only roadmap details",
      match: "…the Q3 launch is internally code-named …",
      confidence: 0.74,
      createdAt: new Date(now - 118 * min),
      chatTitle: "Roadmap questions",
      chatUserId: "user_carol",
    },
  ];
}
