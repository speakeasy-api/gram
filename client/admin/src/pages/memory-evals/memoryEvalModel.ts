// Types and pure helpers for the team memory eval runs page. The report shape
// mirrors server/internal/platformmcp/memoryeval/bench/report.go, which the
// API returns as report_json.

export type MemoryEvalStatus = "queued" | "running" | "succeeded" | "failed";

export type MemoryEvalConfig = {
  answer_models: string[];
  judge_model: string;
  summary_model: string;
  digest_model: string;
  runs: number;
};

export type MemoryEvalRun = {
  id: string;
  status: MemoryEvalStatus;
  config: MemoryEvalConfig;
  cost_usd?: number;
  error?: string;
  requested_by: string;
  report_json?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
};

export type MemoryEvalOptions = {
  defaults: MemoryEvalConfig;
  allowed_models: string[];
  daily_run_limit: number;
  runs_today: number;
};

export type KindScore = { correct: number; total: number };

export type ArmScore = {
  model: string;
  arm: string;
  has_tools: boolean;
  accuracy_by_run: number[];
  accuracy: number;
  by_kind: Record<string, KindScore>;
  answerable: number;
  cited_answers: number;
  valid_cited: number;
  leaks: string[] | null;
  isolation_violations: string[] | null;
  not_found: number;
  negatives: number;
  errors: number;
  answers: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
  latency_mean_ms: number;
  latency_p95_ms: number;
};

export type ModelSpend = {
  model: string;
  calls: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
};

export type Preparation = {
  models: { summary_model: string; digest_model: string };
  summaries: number;
  failed_summaries: number;
  digests: number;
  summary_spend: ModelSpend;
  digest_spend: ModelSpend;
  duration_ms: number;
};

export type MemoryEvalReport = {
  judge_model: string;
  runs: number;
  questions: number;
  scores: ArmScore[];
  judge_cost_usd: number;
  preparation?: Preparation;
};

/** Runs that may still write team memory; a new run waits for these. */
export function isActive(run: Pick<MemoryEvalRun, "status">): boolean {
  return run.status === "queued" || run.status === "running";
}

/** parseReport returns null for a run without a readable report. */
export function parseReport(run: MemoryEvalRun): MemoryEvalReport | null {
  if (!run.report_json) return null;
  try {
    const report = JSON.parse(run.report_json) as MemoryEvalReport;
    return Array.isArray(report.scores) ? report : null;
  } catch {
    return null;
  }
}

/** One row of the comparison table: a model in an arm, in one run. */
export type ComparisonRow = {
  runId: string;
  runLabel: string;
  summaryModel: string;
  digestModel: string;
  score: ArmScore;
  /** Best accuracy for this arm across every compared run and model. */
  best: boolean;
  /** Any planted secret or cross-project canary reached the answer. */
  unsafe: boolean;
};

export function comparisonRows(
  runs: { run: MemoryEvalRun; report: MemoryEvalReport }[],
): ComparisonRow[] {
  const bestByArm = new Map<string, number>();
  for (const { report } of runs) {
    for (const s of report.scores) {
      bestByArm.set(s.arm, Math.max(bestByArm.get(s.arm) ?? 0, s.accuracy));
    }
  }
  const rows: ComparisonRow[] = [];
  for (const { run, report } of runs) {
    for (const s of report.scores) {
      rows.push({
        runId: run.id,
        runLabel: shortRunLabel(run),
        summaryModel:
          report.preparation?.models.summary_model ?? run.config.summary_model,
        digestModel:
          report.preparation?.models.digest_model ?? run.config.digest_model,
        score: s,
        best: s.accuracy > 0 && s.accuracy === bestByArm.get(s.arm),
        unsafe:
          (s.leaks?.length ?? 0) > 0 ||
          (s.isolation_violations?.length ?? 0) > 0,
      });
    }
  }
  return rows.sort(
    (a, b) =>
      a.score.arm.localeCompare(b.score.arm) ||
      b.score.accuracy - a.score.accuracy,
  );
}

export function runLabel(
  run: Pick<MemoryEvalRun, "id" | "created_at">,
): string {
  const day = run.created_at.slice(0, 16).replace("T", " ");
  return `${day} · ${run.id.slice(0, 8)}`;
}

/** shortRunLabel fits a comparison cell: day and time, no ID. */
export function shortRunLabel(run: Pick<MemoryEvalRun, "created_at">): string {
  return run.created_at.slice(5, 16).replace("T", " ");
}

export function percent(value: number): string {
  return `${Math.round(value * 1000) / 10}%`;
}

export function usd(value: number | undefined): string {
  return value === undefined ? "—" : `$${value.toFixed(2)}`;
}

/** citationRate is valid citations over answerable graded answers. */
export function citationRate(s: ArmScore): string {
  if (!s.has_tools || s.answerable === 0) return "—";
  return percent(s.valid_cited / s.answerable);
}

/** validateConfig mirrors the server's caps so the form says why early. */
export function validateConfig(
  cfg: MemoryEvalConfig,
  allowed: string[],
): string | null {
  if (cfg.answer_models.length === 0) return "Pick at least one answer model.";
  if (cfg.answer_models.length > 4) return "Pick at most 4 answer models.";
  if (new Set(cfg.answer_models).size !== cfg.answer_models.length)
    return "Each answer model can appear once.";
  if (cfg.runs < 1 || cfg.runs > 3) return "Runs must be between 1 and 3.";
  const allowedSet = new Set(allowed);
  for (const model of [
    ...cfg.answer_models,
    cfg.judge_model,
    cfg.summary_model,
    cfg.digest_model,
  ]) {
    if (!allowedSet.has(model)) return `${model} is not an allowed model.`;
  }
  return null;
}
