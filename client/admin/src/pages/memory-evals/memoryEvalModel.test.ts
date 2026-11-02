import { describe, expect, it } from "vitest";

import {
  citationRate,
  comparisonRows,
  isActive,
  parseReport,
  validateConfig,
  type ArmScore,
  type MemoryEvalReport,
  type MemoryEvalRun,
} from "./memoryEvalModel";

const config = {
  answer_models: ["a/one"],
  judge_model: "j/judge",
  summary_model: "s/sum",
  digest_model: "d/dig",
  runs: 1,
};

function score(over: Partial<ArmScore>): ArmScore {
  return {
    model: "a/one",
    arm: "A: no memory",
    has_tools: false,
    accuracy_by_run: [0.5],
    accuracy: 0.5,
    by_kind: {},
    answerable: 10,
    cited_answers: 0,
    valid_cited: 0,
    leaks: null,
    isolation_violations: null,
    not_found: 0,
    negatives: 0,
    errors: 0,
    answers: 40,
    prompt_tokens: 1,
    completion_tokens: 1,
    cost_usd: 0.1,
    latency_mean_ms: 1,
    latency_p95_ms: 2,
    ...over,
  };
}

function run(id: string, report?: MemoryEvalReport): MemoryEvalRun {
  return {
    id,
    status: report ? "succeeded" : "queued",
    config,
    requested_by: "staff@example.com",
    created_at: "2026-11-02T10:00:00Z",
    report_json: report ? JSON.stringify(report) : undefined,
  };
}

describe("memory eval model", () => {
  it("treats queued and running runs as active", () => {
    expect(isActive({ status: "queued" })).toBe(true);
    expect(isActive({ status: "running" })).toBe(true);
    expect(isActive({ status: "failed" })).toBe(false);
  });

  it("parses reports and rejects unreadable ones", () => {
    const report: MemoryEvalReport = {
      judge_model: "j",
      runs: 1,
      questions: 40,
      scores: [score({})],
      judge_cost_usd: 0,
    };
    expect(parseReport(run("r1", report))?.scores).toHaveLength(1);
    expect(parseReport(run("r2"))).toBeNull();
    expect(parseReport({ ...run("r3"), report_json: "{" })).toBeNull();
  });

  it("marks the best accuracy per arm across runs and flags leaks", () => {
    const first: MemoryEvalReport = {
      judge_model: "j",
      runs: 1,
      questions: 40,
      judge_cost_usd: 0,
      scores: [
        score({ arm: "C: search_team_memory", accuracy: 0.8 }),
        score({ arm: "A: no memory", accuracy: 0.2 }),
      ],
    };
    const second: MemoryEvalReport = {
      ...first,
      scores: [
        score({
          arm: "C: search_team_memory",
          accuracy: 0.9,
          leaks: ["planted-secret"],
        }),
      ],
      preparation: {
        models: { summary_model: "s/other", digest_model: "d/other" },
        summaries: 1,
        failed_summaries: 0,
        digests: 1,
        summary_spend: {
          model: "s/other",
          calls: 1,
          prompt_tokens: 1,
          completion_tokens: 1,
          cost_usd: 0,
        },
        digest_spend: {
          model: "d/other",
          calls: 1,
          prompt_tokens: 1,
          completion_tokens: 1,
          cost_usd: 0,
        },
        duration_ms: 1,
      },
    };
    const rows = comparisonRows([
      { run: run("r1", first), report: first },
      { run: run("r2", second), report: second },
    ]);
    const memory = rows.filter((r) => r.score.arm === "C: search_team_memory");
    expect(memory.map((r) => r.runId)).toEqual(["r2", "r1"]);
    expect(memory[0]?.best).toBe(true);
    expect(memory[0]?.unsafe).toBe(true);
    expect(memory[0]?.summaryModel).toBe("s/other");
    expect(memory[1]?.summaryModel).toBe("s/sum");
    expect(rows.find((r) => r.score.arm === "A: no memory")?.best).toBe(true);
  });

  it("shows citation rate only for arms with tools", () => {
    expect(citationRate(score({}))).toBe("—");
    expect(citationRate(score({ has_tools: true, valid_cited: 5 }))).toBe(
      "50%",
    );
  });

  it("validates configs like the server", () => {
    const allowed = ["a/one", "j/judge", "s/sum", "d/dig"];
    expect(validateConfig(config, allowed)).toBeNull();
    expect(validateConfig({ ...config, runs: 4 }, allowed)).toMatch(/Runs/);
    expect(validateConfig({ ...config, answer_models: [] }, allowed)).toMatch(
      /at least one/,
    );
    expect(
      validateConfig({ ...config, judge_model: "x/unknown" }, allowed),
    ).toMatch(/not an allowed/);
  });
});
