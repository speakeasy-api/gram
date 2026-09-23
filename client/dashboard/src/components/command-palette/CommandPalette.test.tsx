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

/** Which shell the palette opens in; reset to the project shell per test. */
const slugs = vi.hoisted(() => ({
  orgSlug: "acme" as string | undefined,
  projectSlug: "widgets" as string | undefined,
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
  useSlugs: () => slugs,
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
const runSessions = vi.fn();

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

const SESSIONS = candidate({
  id: "action:sessions",
  title: "Sessions",
  group: "Pages",
  run: runSessions,
});

// The candidate hook yields projects in both shells; which shell shows them
// while idle is the palette's call, so the tests here are about that.
const WIDGETS = candidate({
  id: "project:project-widgets",
  kind: "project",
  title: "Widgets",
  detail: "Project",
  keywords: ["project", "Widgets", "widgets", "project-widgets"],
  icon: "folder",
  group: "Projects",
});

/**
 * A settled open judgment that orders the rows as `ids` lists them. Halving
 * masses (0.5, 0.25, 0.125, …) keep the total under 1, as a distribution the
 * judge can actually return, while keeping the order strict.
 */
function ordering(ids: string[]): JudgeState {
  const target: Record<string, number> = {};
  ids.forEach((id, index) => {
    target[id] = 0.5 / 2 ** index;
  });
  return {
    judgment: { target, action: { open: 1 }, ready: 0 },
    fresh: true,
    inFlight: false,
    latencyMs: 10,
    disabled: false,
  };
}

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
  slugs.orgSlug = "acme";
  slugs.projectSlug = "widgets";
  mocks.candidates = [SETTINGS, SLACK];
  mocks.judgeState = IDLE_JUDGE;
  mocks.judge.mockReset();
  mocks.reset.mockReset();
  palette.close.mockReset();
  runSettings.mockReset();
  runSlack.mockReset().mockResolvedValue(undefined);
  runSessions.mockReset();
});

afterEach(cleanup);

describe("CommandPalette", () => {
  it("offers the Project Assistant for a query that matches nothing", async () => {
    render(<CommandPalette />);

    await userEvent.type(input(), "zzzzznomatch");

    const row = screen.getByRole("option", { name: /Ask Project Assistant/ });
    expect(row.textContent).toContain("“zzzzznomatch”");
  });

  // The palette used to fall back to page navigation alone at the org level,
  // leaving no way to reach a project from it (S-1028).
  it("offers projects at the organization level", () => {
    slugs.projectSlug = undefined;
    mocks.candidates = [SETTINGS, WIDGETS];
    render(<CommandPalette />);

    // The row's accessible name carries its detail line too ("Widgets Project").
    expect(screen.getByRole("option", { name: /Widgets/ })).toBeTruthy();
    expect(
      screen.getByPlaceholderText("Search projects and pages…"),
    ).toBeTruthy();
    const headings = Array.from(
      document.body.querySelectorAll("[cmdk-group-heading]"),
    ).map((h) => h.textContent);
    expect(headings).toEqual(["Projects", "Pages"]);
  });

  // Inside a project the projects are a switcher, not the palette's main
  // job: they wait for a query so the idle list isn't headed by the projects
  // you aren't in. The judge is mocked idle here, so fuzzy order alone has
  // to surface the row.
  it("offers projects inside a project only once there is a query", async () => {
    mocks.candidates = [SETTINGS, WIDGETS];
    render(<CommandPalette />);

    expect(screen.queryByRole("option", { name: /Widgets/ })).toBeNull();

    await userEvent.type(input(), "widgets");

    expect(screen.getByRole("option", { name: /Widgets/ })).toBeTruthy();
  });

  it("clears the query on the first Escape instead of closing", async () => {
    render(<CommandPalette />);

    await userEvent.type(input(), "sett");
    await userEvent.keyboard("{Escape}");

    expect((input() as HTMLInputElement).value).toBe("");
    expect(palette.close).not.toHaveBeenCalled();
  });

  // Rendering treats whitespace-only input as no query, so Escape must too:
  // otherwise the first press is swallowed clearing spaces nobody can see.
  it("closes on the first Escape when the query is only whitespace", async () => {
    render(<CommandPalette />);

    await userEvent.type(input(), "   ");
    await userEvent.keyboard("{Escape}");

    expect(palette.close).toHaveBeenCalled();
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

  // A judgment landing after the user moved the highlight must not yank it
  // to the new top row: Enter runs the row they were looking at.
  it("keeps the highlight on the row the user moved to when a judgment reorders", async () => {
    mocks.candidates = [SETTINGS, SLACK, SESSIONS];
    mocks.judgeState = ordering([
      "action:settings",
      "mcp:slack",
      "action:sessions",
    ]);
    const { rerender } = render(<CommandPalette />);

    await userEvent.type(input(), "s");
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(
      expect.arrayContaining([
        expect.stringContaining("Settings"),
        expect.stringContaining("Slack"),
        expect.stringContaining("Sessions"),
      ]),
    );
    await userEvent.keyboard("{ArrowDown}");
    expect(
      screen.getByRole("option", { selected: true }).textContent,
    ).toContain("Slack");

    mocks.judgeState = ordering([
      "action:sessions",
      "action:settings",
      "mcp:slack",
    ]);
    rerender(<CommandPalette />);
    expect(
      screen.getAllByRole("option").map((o) => o.textContent)[0],
    ).toContain("Sessions");
    expect(
      screen.getByRole("option", { selected: true }).textContent,
    ).toContain("Slack");

    await userEvent.keyboard("{Enter}");
    expect(runSlack).toHaveBeenCalledWith("open");
    expect(runSessions).not.toHaveBeenCalled();
  });

  // Moving away and back leaves the highlight on the top row, but the user
  // chose that row: a reorder must not take it from under an Enter.
  it("keeps the highlight where the user returned it when a judgment reorders", async () => {
    mocks.candidates = [SETTINGS, SLACK, SESSIONS];
    mocks.judgeState = ordering([
      "action:settings",
      "mcp:slack",
      "action:sessions",
    ]);
    const { rerender } = render(<CommandPalette />);

    await userEvent.type(input(), "s");
    await userEvent.keyboard("{ArrowDown}");
    await userEvent.keyboard("{ArrowUp}");
    expect(
      screen.getByRole("option", { selected: true }).textContent,
    ).toContain("Settings");

    mocks.judgeState = ordering([
      "action:sessions",
      "action:settings",
      "mcp:slack",
    ]);
    rerender(<CommandPalette />);
    expect(
      screen.getAllByRole("option").map((o) => o.textContent)[0],
    ).toContain("Sessions");
    expect(
      screen.getByRole("option", { selected: true }).textContent,
    ).toContain("Settings");

    await userEvent.keyboard("{Enter}");
    expect(runSettings).toHaveBeenCalledWith("open");
    expect(runSessions).not.toHaveBeenCalled();
  });

  it("snaps the highlight to the new top row when the user had not moved it", async () => {
    mocks.candidates = [SETTINGS, SLACK, SESSIONS];
    mocks.judgeState = ordering([
      "action:settings",
      "mcp:slack",
      "action:sessions",
    ]);
    const { rerender } = render(<CommandPalette />);

    await userEvent.type(input(), "s");
    expect(
      screen.getByRole("option", { selected: true }).textContent,
    ).toContain("Settings");

    mocks.judgeState = ordering([
      "action:sessions",
      "action:settings",
      "mcp:slack",
    ]);
    rerender(<CommandPalette />);
    expect(
      screen.getByRole("option", { selected: true }).textContent,
    ).toContain("Sessions");

    await userEvent.keyboard("{Enter}");
    expect(runSessions).toHaveBeenCalledWith("open");
  });

  // A background refetch rebuilds the candidate array with identical content;
  // re-asking then would abort a good request in flight for nothing.
  it("does not ask the judge again when the candidates are rebuilt unchanged", async () => {
    const { rerender } = render(<CommandPalette />);

    await userEvent.type(input(), "sl");
    const asked = mocks.judge.mock.calls.length;
    expect(asked).toBeGreaterThan(0);

    mocks.candidates = (mocks.candidates as LauncherCandidate[]).map((c) => ({
      ...c,
      keywords: [...c.keywords],
    }));
    rerender(<CommandPalette />);
    expect(mocks.judge.mock.calls.length).toBe(asked);

    // Content that changes what Jev would see does re-ask.
    mocks.candidates = (mocks.candidates as LauncherCandidate[]).map((c) =>
      c.id === "mcp:slack" ? { ...c, detail: "MCP server · disabled" } : c,
    );
    rerender(<CommandPalette />);
    expect(mocks.judge.mock.calls.length).toBe(asked + 1);
  });

  // cmdk walks ↑/↓ through the DOM, so interleaved groups must not pull a
  // later row of the first group ahead of a better-ranked row.
  it("renders ranked rows flat, in ranked order, without group headings", async () => {
    mocks.candidates = [SETTINGS, SLACK, SESSIONS];
    mocks.judgeState = ordering([
      "action:settings",
      "mcp:slack",
      "action:sessions",
    ]);
    render(<CommandPalette />);

    await userEvent.type(input(), "s");

    const titles = screen
      .getAllByRole("option")
      .map((o) => o.textContent ?? "")
      .filter((text) => !text.includes("Ask Project Assistant"));
    expect(titles[0]).toContain("Settings");
    expect(titles[1]).toContain("Slack");
    expect(titles[2]).toContain("Sessions");
    const headings = Array.from(
      document.body.querySelectorAll("[cmdk-group-heading]"),
    ).map((h) => h.textContent);
    expect(headings).not.toContain("Pages");
    expect(headings).not.toContain("MCP Servers");

    await userEvent.keyboard("{ArrowDown}");
    expect(
      screen.getByRole("option", { selected: true }).textContent,
    ).toContain("Slack");
  });

  it("keeps group headings for the idle list", () => {
    mocks.candidates = [SETTINGS, SLACK, SESSIONS];
    render(<CommandPalette />);

    const headings = Array.from(
      document.body.querySelectorAll("[cmdk-group-heading]"),
    ).map((h) => h.textContent);
    expect(headings).toContain("Pages");
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
