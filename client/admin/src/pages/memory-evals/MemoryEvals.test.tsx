import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminMemoryEvalOptions } from "@gram/admin-client/models/components/adminmemoryevaloptions";
import type { AdminMemoryEvalRun } from "@gram/admin-client/models/components/adminmemoryevalrun";

import { routeTree } from "@/routeTree.gen";
import { renderRouteTree } from "@/test/harness";

import type { MemoryEvalReport } from "./memoryEvalModel";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  options: vi.fn(),
  runs: vi.fn(),
  run: vi.fn(),
  start: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return { ...actual, getSession: mocks.getSession };
});

vi.mock("./memoryEvalClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./memoryEvalClient")>();
  return {
    ...actual,
    memoryEvalOptionsQuery: () =>
      queryOptions({
        // The real keys, so invalidateMemoryEvals reaches these mocks.
        queryKey: actual.memoryEvalOptionsQuery().queryKey,
        queryFn: () => mocks.options(),
      }),
    memoryEvalRunsQuery: () =>
      infiniteQueryOptions({
        queryKey: actual.memoryEvalRunsQuery().queryKey,
        queryFn: () => mocks.runs(),
        initialPageParam: undefined as undefined | { cursor: string },
        getNextPageParam: () => undefined,
      }),
    memoryEvalRunQuery: (id: string) =>
      queryOptions({
        queryKey: ["@gram/admin-client", "admin", "getMemoryEvalRun", { id }],
        queryFn: () => mocks.run(id),
      }),
    startMemoryEvalRun: mocks.start,
  };
});

// Invented values only: this repository is public.
const defaults = {
  answerModels: ["provider/answer-a"],
  judgeModel: "provider/judge",
  summaryModel: "provider/summary",
  digestModel: "provider/digest",
  runs: 1,
};

function someOptions(
  overrides: Partial<AdminMemoryEvalOptions> = {},
): AdminMemoryEvalOptions {
  return {
    defaults,
    allowedModels: [
      "provider/answer-a",
      "provider/answer-b",
      "provider/judge",
      "provider/summary",
      "provider/digest",
    ],
    dailyRunLimit: 10,
    runsLast24h: 1,
    maxAnswerModels: 4,
    maxRuns: 3,
    ...overrides,
  };
}

const report: MemoryEvalReport = {
  judge_model: "provider/judge",
  runs: 1,
  questions: 40,
  judge_cost_usd: 0.5,
  scores: [
    {
      model: "provider/answer-a",
      arm: "C: search_team_memory",
      has_tools: true,
      accuracy_by_run: [0.875],
      accuracy: 0.875,
      by_kind: {},
      answerable: 40,
      cited_answers: 30,
      valid_cited: 30,
      leaks: null,
      isolation_violations: null,
      not_found: 0,
      negatives: 0,
      errors: 0,
      answers: 40,
      prompt_tokens: 100,
      completion_tokens: 10,
      cost_usd: 1.25,
      latency_mean_ms: 1,
      latency_p95_ms: 900,
    },
  ],
};

function aRun(overrides: Partial<AdminMemoryEvalRun> = {}): AdminMemoryEvalRun {
  return {
    id: "019a0000-0000-7000-8000-00000000abcd",
    status: "succeeded",
    config: defaults,
    costUsd: 2.5,
    requestedBy: "staff@example.test",
    createdAt: new Date("2026-11-02T10:00:00Z"),
    startedAt: new Date("2026-11-02T10:01:00Z"),
    finishedAt: new Date("2026-11-02T10:43:00Z"),
    ...overrides,
  };
}

function runsPage(runs: AdminMemoryEvalRun[]) {
  return { result: { runs } };
}

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.options.mockResolvedValue(someOptions());
  mocks.runs.mockResolvedValue(runsPage([aRun()]));
  mocks.run.mockResolvedValue(aRun({ reportJson: JSON.stringify(report) }));
});

afterEach(() => {
  cleanup();
});

describe("MemoryEvalsPage", () => {
  it("shows the run budget, the caps and the runs", async () => {
    await renderRouteTree(routeTree, { initialPath: "/memory-evals" });

    expect(
      await screen.findByText(/1 of 10 runs used in the last 24 hours/),
    ).toBeTruthy();
    expect(screen.getByText("Answer models (up to 4)")).toBeTruthy();
    expect(
      screen.getByLabelText("Repeats per model and arm").getAttribute("max"),
    ).toBe("3");
    expect(await screen.findByText("staff@example.test")).toBeTruthy();
    expect(screen.getByText("succeeded")).toBeTruthy();
    expect(screen.getByText("$2.50")).toBeTruthy();
    expect(screen.getByText("took 42m")).toBeTruthy();
  });

  it("says so when there are no runs", async () => {
    mocks.runs.mockResolvedValue(runsPage([]));

    await renderRouteTree(routeTree, { initialPath: "/memory-evals" });

    expect(await screen.findByText(/No runs yet/)).toBeTruthy();
  });

  it("blocks a start at the daily limit", async () => {
    mocks.options.mockResolvedValue(someOptions({ runsLast24h: 10 }));

    await renderRouteTree(routeTree, { initialPath: "/memory-evals" });

    expect(
      await screen.findByText("10 runs started in the last 24 hours."),
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Start run" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("blocks a start while a run is active", async () => {
    mocks.runs.mockResolvedValue(
      runsPage([aRun({ status: "running", finishedAt: undefined })]),
    );

    await renderRouteTree(routeTree, { initialPath: "/memory-evals" });

    expect(await screen.findByText(/A run is in progress/)).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Start run" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("starts the default config after confirmation", async () => {
    mocks.start.mockResolvedValue(aRun({ status: "queued" }));
    await renderRouteTree(routeTree, { initialPath: "/memory-evals" });
    await screen.findByText("staff@example.test");

    fireEvent.click(screen.getByRole("button", { name: "Start run" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Start run" }));

    await waitFor(() => expect(mocks.start).toHaveBeenCalled());
    expect(mocks.start.mock.calls[0]?.[0]).toEqual(defaults);
    // The start re-reads the runs and the remaining budget.
    await waitFor(() => expect(mocks.runs).toHaveBeenCalledTimes(2));
    expect(mocks.options).toHaveBeenCalledTimes(2);
  });

  it("compares a succeeded run from its report", async () => {
    await renderRouteTree(routeTree, { initialPath: "/memory-evals" });

    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: "Compare run 2026-11-02 10:00 · 0000abcd",
      }),
    );

    expect(await screen.findByText("Comparison")).toBeTruthy();
    expect(mocks.run).toHaveBeenCalledWith(
      "019a0000-0000-7000-8000-00000000abcd",
    );
    expect(await screen.findByText("87.5%")).toBeTruthy();
    expect(screen.getByText("900 ms")).toBeTruthy();
  });
});
