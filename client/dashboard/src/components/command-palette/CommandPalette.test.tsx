import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LauncherCandidate } from "./candidates/types";
import type { JudgeState } from "./useLauncherJudge";

const palette = vi.hoisted(() => ({
  isOpen: true,
  close: vi.fn(),
  actions: [] as unknown[],
  contextBadge: null,
}));

const mocks = vi.hoisted(() => ({
  candidates: [] as unknown[],
  judgeState: {
    judgment: null,
    fresh: false,
    inFlight: false,
    latencyMs: null,
    disabled: false,
  } as unknown,
  judge: vi.fn(),
  reset: vi.fn(),
}));

vi.mock("react-router", () => ({
  useLocation: () => ({ pathname: "/acme/mcp" }),
}));
vi.mock("@/contexts/CommandPalette", () => ({
  useCommandPalette: () => palette,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "widgets" }),
}));
vi.mock("./recentlyVisited", () => ({
  useRecentsUserId: () => "user_1",
}));
vi.mock("./candidates", () => ({
  useLauncherCandidates: () => mocks.candidates,
}));
vi.mock("./useLauncherJudge", () => ({
  useLauncherJudge: () => ({
    state: mocks.judgeState,
    judge: mocks.judge,
    reset: mocks.reset,
  }),
}));

import { CommandPalette } from "./CommandPalette";

const IDLE_JUDGE: JudgeState = {
  judgment: null,
  fresh: false,
  inFlight: false,
  latencyMs: null,
  disabled: false,
};

const runSettings = vi.fn();
const runSlack = vi.fn();

function candidate(
  overrides: Partial<LauncherCandidate> & Pick<LauncherCandidate, "id">,
): LauncherCandidate {
  return {
    kind: "page",
    title: overrides.id,
    detail: "Page",
    keywords: [],
    verbs: ["open"],
    group: "Pages",
    run: () => {},
    ...overrides,
  };
}

const SETTINGS = candidate({
  id: "action:settings",
  title: "Settings",
  group: "Pages",
  run: runSettings,
});

const SLACK = candidate({
  id: "mcp:slack",
  kind: "mcp_server",
  title: "Slack",
  detail: "MCP server · enabled",
  keywords: ["mcp", "slack"],
  verbs: ["open", "disable"],
  group: "MCP Servers",
  run: runSlack,
});

/** A settled judgment that picks the Slack row and the disable verb. */
const DISABLE_SLACK: JudgeState = {
  judgment: {
    target: { "mcp:slack": 0.95, "action:settings": 0.05 },
    action: { open: 0.05, disable: 0.9, unclear: 0.05 },
    ready: 0.9,
  },
  fresh: true,
  inFlight: false,
  latencyMs: 123,
  disabled: false,
};

const input = () =>
  screen.getByPlaceholderText("Ask AI or search resources and pages…");

beforeEach(() => {
  mocks.candidates = [SETTINGS, SLACK];
  mocks.judgeState = IDLE_JUDGE;
  mocks.judge.mockReset();
  mocks.reset.mockReset();
  palette.close.mockReset();
  runSettings.mockReset();
  runSlack.mockReset().mockResolvedValue(undefined);
});

afterEach(cleanup);

describe("CommandPalette", () => {
  it("offers the Project Assistant for a query that matches nothing", async () => {
    render(<CommandPalette />);

    await userEvent.type(input(), "zzzzznomatch");

    const row = screen.getByRole("option", { name: /Ask Project Assistant/ });
    expect(row.textContent).toContain("“zzzzznomatch”");
  });

  it("clears the query on the first Escape instead of closing", async () => {
    render(<CommandPalette />);

    await userEvent.type(input(), "sett");
    await userEvent.keyboard("{Escape}");

    expect((input() as HTMLInputElement).value).toBe("");
    expect(palette.close).not.toHaveBeenCalled();
  });

  it("runs an open row on the first Enter and closes", async () => {
    render(<CommandPalette />);

    await userEvent.type(input(), "sett");
    await userEvent.keyboard("{Enter}");

    expect(runSettings).toHaveBeenCalledWith("open");
    expect(palette.close).toHaveBeenCalled();
    expect(screen.queryByText(/confirm/)).toBeNull();
  });

  it("asks the judge on every query change with the prefiltered rows", async () => {
    render(<CommandPalette />);

    await userEvent.type(input(), "sl");

    const [query, sent] = mocks.judge.mock.calls.at(-1) as [
      string,
      LauncherCandidate[],
    ];
    expect(query).toBe("sl");
    expect(sent.map((c) => c.id)).toContain("mcp:slack");
  });

  it("labels a mutating row with its verb and confirms before running", async () => {
    mocks.judgeState = DISABLE_SLACK;
    render(<CommandPalette />);

    await userEvent.type(input(), "slack");
    expect(
      screen.getByRole("option", { name: /Disable · Slack/ }),
    ).toBeDefined();

    await userEvent.keyboard("{Enter}");

    expect(runSlack).not.toHaveBeenCalled();
    expect(screen.getByText("Disable Slack?")).toBeDefined();
    expect(screen.queryByPlaceholderText(/search resources/)).toBeNull();
    // The list collapses to the row being confirmed.
    expect(screen.getAllByRole("option")).toHaveLength(1);

    await userEvent.keyboard("{Enter}");

    expect(runSlack).toHaveBeenCalledWith("disable");
    await waitFor(() => expect(palette.close).toHaveBeenCalled());
  });

  it("returns from confirm to the list on Escape, keeping the query", async () => {
    mocks.judgeState = DISABLE_SLACK;
    render(<CommandPalette />);

    await userEvent.type(input(), "slack");
    await userEvent.keyboard("{Enter}");
    expect(screen.getByText("Disable Slack?")).toBeDefined();

    await userEvent.keyboard("{Escape}");

    expect(screen.queryByText("Disable Slack?")).toBeNull();
    expect((input() as HTMLInputElement).value).toBe("slack");
    expect(palette.close).not.toHaveBeenCalled();
    expect(runSlack).not.toHaveBeenCalled();
  });

  it("returns to the list when the mutation rejects", async () => {
    mocks.judgeState = DISABLE_SLACK;
    runSlack.mockRejectedValue(new Error("boom"));
    render(<CommandPalette />);

    await userEvent.type(input(), "slack");
    await userEvent.keyboard("{Enter}");
    await userEvent.keyboard("{Enter}");

    await waitFor(() => expect(input()).toBeDefined());
    expect((input() as HTMLInputElement).value).toBe("slack");
    expect(palette.close).not.toHaveBeenCalled();
  });

  it("shows the green ↵ on the top row when ready and hides it once the selection moves", async () => {
    mocks.judgeState = DISABLE_SLACK;
    render(<CommandPalette />);

    await userEvent.type(input(), "s");

    const options = screen.getAllByRole("option");
    expect(options[0]?.textContent).toContain("Slack");
    expect(screen.getByLabelText("Ready")).toBeDefined();

    await userEvent.keyboard("{ArrowDown}");

    expect(screen.queryByLabelText("Ready")).toBeNull();
  });

  it("renders the round-trip latency when the judgment is fresh", async () => {
    mocks.judgeState = DISABLE_SLACK;
    render(<CommandPalette />);

    await userEvent.type(input(), "s");

    expect(screen.getByText("123 ms")).toBeDefined();
  });

  it("keeps fuzzy order and never asks the judge once it is disabled", async () => {
    mocks.judgeState = { ...IDLE_JUDGE, disabled: true };
    render(<CommandPalette />);

    await userEvent.type(input(), "s");

    expect(mocks.judge).not.toHaveBeenCalled();
    expect(screen.getByRole("option", { name: /Slack/ })).toBeDefined();
    expect(screen.getByRole("option", { name: /Settings/ })).toBeDefined();
    expect(screen.queryByLabelText("Ready")).toBeNull();
    expect(screen.queryByText(/ ms$/)).toBeNull();
  });
});
