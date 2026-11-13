import { describe, expect, it } from "vitest";

import {
  accuracyLabel,
  citationRate,
  comparisonRows,
  isActive,
  MAX_ANSWER_MODELS,
  memoryWriteCost,
  parseReport,
  runLabel,
  runTiming,
  shortRunLabel,
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
    // A report missing fields the page reads is unreadable, not a crash.
    expect(
      parseReport({
        ...run("r4"),
        report_json: JSON.stringify({
          scores: [{ model: "m", arm: "a", accuracy: 1 }],
        }),
      }),
    ).toBeNull();
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
      judge_model: "j/other",
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
    // The leaking row scored higher but is disqualified, so the clean row is best.
    expect(memory[0]?.best).toBe(false);
    expect(memory[0]?.unsafe).toBe(true);
    expect(memory[1]?.best).toBe(true);
    expect(memory[0]?.summaryModel).toBe("s/other");
    expect(memory[1]?.summaryModel).toBe("s/sum");
    expect(memory[0]?.judgeModel).toBe("j/other");
    expect(memory[1]?.judgeModel).toBe("j");
    expect(rows.find((r) => r.score.arm === "A: no memory")?.best).toBe(true);
  });

  it("prints a dash for an arm with nothing graded and sorts it last", () => {
    const report: MemoryEvalReport = {
      judge_model: "j",
      runs: 1,
      questions: 40,
      judge_cost_usd: 0,
      scores: [
        score({
          model: "a/broken",
          accuracy: 0,
          accuracy_by_run: null,
          errors: 40,
        }),
        score({ model: "a/one", accuracy: 0.1 }),
      ],
    };
    const rows = comparisonRows([{ run: run("r1", report), report }]);
    expect(rows.map((r) => r.score.model)).toEqual(["a/one", "a/broken"]);
    expect(accuracyLabel(rows[1]!.score)).toBe("—");
    expect(rows[1]?.best).toBe(false);
    expect(accuracyLabel(rows[0]!.score)).toBe("10%");
  });

  it("marks memory write cost as a lower bound when calls were unpriced", () => {
    const spend = {
      model: "m",
      calls: 1,
      prompt_tokens: 1,
      completion_tokens: 1,
      cost_usd: 0.5,
    };
    const prep = {
      models: { summary_model: "s", digest_model: "d" },
      summaries: 1,
      failed_summaries: 0,
      digests: 1,
      duration_ms: 1,
      summary_spend: spend,
      digest_spend: spend,
    };
    expect(memoryWriteCost(prep)).toBe("$1.00");
    expect(
      memoryWriteCost({
        ...prep,
        digest_spend: { ...spend, unpriced_calls: 2 },
      }),
    ).toBe("≥$1.00");
  });

  it("labels runs in UTC with the random tail of the ID", () => {
    const r = {
      id: "019a0000-0000-7000-8000-00000000abcd",
      created_at: "2026-11-02T05:00:00-05:00",
    };
    expect(runLabel(r)).toBe("2026-11-02 10:00 · 0000abcd");
    expect(shortRunLabel(r)).toBe("11-02 10:00");
  });

  it("shows elapsed time for active runs and duration for finished ones", () => {
    const now = Date.parse("2026-11-02T16:05:00Z");
    expect(
      runTiming(
        {
          status: "running",
          created_at: "2026-11-02T09:00:00Z",
          started_at: "2026-11-02T10:00:00Z",
        },
        now,
      ),
    ).toBe("for 6h 05m");
    expect(
      runTiming(
        {
          status: "succeeded",
          created_at: "2026-11-02T09:00:00Z",
          started_at: "2026-11-02T10:00:00Z",
          finished_at: "2026-11-02T10:42:00Z",
        },
        now,
      ),
    ).toBe("took 42m");
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
    expect(
      validateConfig(
        { ...config, answer_models: ["a/one", "j/judge"] },
        allowed,
      ),
    ).toMatch(/judge must not also be an answer model/);
    const many = ["a/one", "a/two", "a/three", "a/four", "a/five"];
    expect(
      validateConfig(
        { ...config, answer_models: many.slice(0, MAX_ANSWER_MODELS + 1) },
        [...allowed, ...many],
      ),
    ).toMatch(/at most/);
    expect(
      validateConfig({ ...config, answer_models: ["a/one", "a/one"] }, allowed),
    ).toMatch(/once/);
  });
});
