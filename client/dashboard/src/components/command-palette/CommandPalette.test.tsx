import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LauncherCandidate } from "./candidates/types";

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
  capture: vi.fn(),
  prefilter: vi.fn(),
  rank: vi.fn(),
  launcherOptions: [] as Array<{
    query: string;
    enabled: boolean;
    inProject: boolean;
  }>,
}));

vi.mock("@/contexts/CommandPalette", () => ({
  useCommandPalette: () => palette,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => slugs,
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_1" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));
vi.mock("./recentlyVisited", () => ({
  useRecentsUserId: () => "user_1",
}));
vi.mock("./candidates", () => ({
  useLauncherCandidates: (options: {
    query: string;
    enabled: boolean;
    inProject: boolean;
  }) => {
    mocks.launcherOptions.push(options);
    return mocks.candidates;
  },
}));
vi.mock("./ranker", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./ranker")>();
  return {
    ...actual,
    prefilter: (...args: Parameters<typeof actual.prefilter>) => {
      mocks.prefilter(...args);
      return actual.prefilter(...args);
    },
    rank: (...args: Parameters<typeof actual.rank>) => {
      mocks.rank(...args);
      return actual.rank(...args);
    },
  };
});
import { CommandPalette } from "./CommandPalette";

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

const input = () =>
  screen.getByPlaceholderText("Ask AI or search resources and pages…");

beforeEach(() => {
  slugs.orgSlug = "acme";
  slugs.projectSlug = "widgets";
  mocks.candidates = [SETTINGS, SLACK];
  mocks.capture.mockReset();
  mocks.prefilter.mockReset();
  mocks.rank.mockReset();
  mocks.launcherOptions = [];
  palette.isOpen = true;
  palette.close.mockReset();
  runSettings.mockReset();
  runSlack.mockReset().mockResolvedValue(undefined);
  runSessions.mockReset();
});

afterEach(cleanup);

describe("CommandPalette", () => {
  it("debounces two-character resource queries for 200ms", async () => {
    vi.useFakeTimers();
    try {
      render(<CommandPalette />);
      fireEvent.change(input(), { target: { value: "ab" } });
      expect(mocks.launcherOptions.at(-1)?.query).toBe("");
      await act(async () => {
        vi.advanceTimersByTime(199);
      });
      expect(mocks.launcherOptions.at(-1)?.query).toBe("");
      await act(async () => {
        vi.advanceTimersByTime(1);
      });
      expect(mocks.launcherOptions.at(-1)?.query).toBe("ab");
    } finally {
      vi.useRealTimers();
    }
  });

  it("captures one performance event per open under StrictMode", () => {
    const view = render(
      <StrictMode>
        <CommandPalette />
      </StrictMode>,
    );

    palette.isOpen = false;
    view.rerender(
      <StrictMode>
        <CommandPalette />
      </StrictMode>,
    );
    view.rerender(
      <StrictMode>
        <CommandPalette />
      </StrictMode>,
    );

    expect(mocks.capture).toHaveBeenCalledOnce();
    expect(mocks.capture).toHaveBeenCalledWith(
      "command_palette_open_performance",
      expect.objectContaining({
        cache_state: "warm",
        ranking_invocation_count: 0,
        scope: "project",
      }),
    );

    palette.isOpen = true;
    view.rerender(
      <StrictMode>
        <CommandPalette />
      </StrictMode>,
    );
    palette.isOpen = false;
    view.rerender(
      <StrictMode>
        <CommandPalette />
      </StrictMode>,
    );

    expect(mocks.capture).toHaveBeenCalledTimes(2);
  });

  it("only ranks meaningful queries across updates and reopen", async () => {
    const view = render(<CommandPalette />);

    expect(mocks.prefilter).not.toHaveBeenCalled();
    expect(mocks.rank).not.toHaveBeenCalled();

    mocks.candidates = [SETTINGS, SLACK, SESSIONS];
    view.rerender(<CommandPalette />);
    await userEvent.type(input(), "   ");
    mocks.candidates = [SETTINGS, SLACK];
    view.rerender(<CommandPalette />);

    expect(mocks.prefilter).not.toHaveBeenCalled();
    expect(mocks.rank).not.toHaveBeenCalled();

    fireEvent.change(input(), { target: { value: "   s" } });

    expect(mocks.prefilter).toHaveBeenCalledOnce();
    expect(mocks.prefilter).toHaveBeenLastCalledWith("s", mocks.candidates);
    expect(mocks.rank).toHaveBeenCalledOnce();

    await userEvent.clear(input());
    mocks.candidates = [SETTINGS, SLACK];
    view.rerender(<CommandPalette />);

    expect(mocks.prefilter).toHaveBeenCalledOnce();
    expect(mocks.rank).toHaveBeenCalledOnce();

    palette.isOpen = false;
    view.rerender(<CommandPalette />);
    palette.isOpen = true;
    view.rerender(<CommandPalette />);

    expect(mocks.prefilter).toHaveBeenCalledOnce();
    expect(mocks.rank).toHaveBeenCalledOnce();

    fireEvent.change(input(), { target: { value: "slack" } });

    expect(mocks.prefilter).toHaveBeenCalledTimes(2);
    expect(mocks.prefilter).toHaveBeenLastCalledWith("slack", mocks.candidates);
    expect(mocks.rank).toHaveBeenCalledTimes(2);
  });

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
  // you aren't in.
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

  // Search results stay flat so fuzzy order is not disturbed by display groups.
  it("renders fuzzy-ranked rows flat without group headings", async () => {
    mocks.candidates = [SETTINGS, SLACK, SESSIONS];
    render(<CommandPalette />);

    await userEvent.type(input(), "s");

    const titles = screen
      .getAllByRole("option")
      .map((o) => o.textContent ?? "")
      .filter((text) => !text.includes("Ask Project Assistant"));
    expect(titles[0]).toContain("Slack");
    expect(titles[1]).toContain("Sessions");
    expect(titles[2]).toContain("Settings");
    const headings = Array.from(
      document.body.querySelectorAll("[cmdk-group-heading]"),
    ).map((h) => h.textContent);
    expect(headings).not.toContain("Pages");
    expect(headings).not.toContain("MCP Servers");

    await userEvent.keyboard("{ArrowDown}");
    expect(
      screen.getByRole("option", { selected: true }).textContent,
    ).toContain("Sessions");
  });

  it("keeps group headings for the idle list", () => {
    mocks.candidates = [SETTINGS, SLACK, SESSIONS];
    render(<CommandPalette />);

    const headings = Array.from(
      document.body.querySelectorAll("[cmdk-group-heading]"),
    ).map((h) => h.textContent);
    expect(headings).toContain("Pages");
  });
});
