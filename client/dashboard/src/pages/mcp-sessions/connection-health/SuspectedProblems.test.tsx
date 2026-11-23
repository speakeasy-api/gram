import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProblemDetailSheet } from "./ProblemDetailSheet";
import { SuspectedProblems } from "./SuspectedProblems";
import type {
  ListSuspectedProblemsResponse,
  SuspectedProblem,
  SuspectedProblemDetail,
} from "./types";

const state = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}));

vi.mock("./api", async (original) => ({
  ...(await original<object>()),
  useSuspectedProblem: () => state.query,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcpSessions: {
      provider: {
        connectionLogs: { href: (id: string) => `/providers/${id}/logs` },
      },
    },
  }),
}));

vi.mock("./AskAiButton", () => ({ AskAiButton: () => null }));
vi.mock("./HealthBarChart", () => ({ HealthBarChart: () => null }));

const NOW = new Date("2026-11-20T12:00:00Z");
const noop = (): void => {};

const PROBLEM: SuspectedProblem = {
  id: "abc",
  headline: "2 people can't connect to Linear",
  fact: "Signing in to the service fails.",
  since: "2026-11-20T10:00:00Z",
  lastFailedAt: "2026-11-20T11:50:00Z",
  peopleCount: 2,
  scope: { kind: "server", id: "srv_1", name: "Linear" },
  rule: "same_server",
  part: "service",
  filters: {
    from: "2026-11-20T09:45:00Z",
    outcomes: ["failed"],
    serverIds: ["srv_1", "gw_1"],
    people: ["user:a", "user:b"],
    reasons: ["static_key_rejected"],
  },
};

function person(id: string) {
  return {
    person: { subjectUrn: `user:${id}`, kind: "user" as const, name: id },
    apps: [{ family: "claude_code", name: "Claude Code", version: "2.1.4" }],
    servers: [
      { server: { id: "srv_1", kind: "mcp" as const, name: "Linear" } },
    ],
    attempts: 3,
    firstFailedAt: "2026-11-20T10:00:00Z",
  };
}

function listed(
  problems: SuspectedProblem[],
  more: Partial<ListSuspectedProblemsResponse> = {},
) {
  return {
    data: {
      loggingEnabled: true,
      viewerScope: "all" as const,
      problems,
      problemsTotal: problems.length,
      ...more,
    },
    isPending: false,
    isError: false,
    error: null,
  };
}

const DETAIL: SuspectedProblemDetail = {
  ...PROBLEM,
  failedPer10Minutes: [{ start: "2026-11-20T11:50:00Z", attempts: 6 }],
  otherFailuresOnService: 1,
  messages: [
    {
      reason: "static_key_rejected",
      message: "The server rejected the saved key.",
      adminMessage: "The server rejected the credential saved for it.",
      attempts: 6,
    },
  ],
  people: [
    {
      ...person("ana"),
      apps: [
        { family: "claude_code", name: "Claude Code", version: "2.1.4" },
        { family: "cursor", name: "Cursor", version: "2.0.1" },
      ],
      servers: [
        { server: { id: "srv_1", kind: "mcp", name: "Linear" } },
        { server: { id: "srv_2", kind: "mcp", name: "GitHub" } },
      ],
    },
    ...Array.from({ length: 49 }, (_, i) => person(`p${i}`)),
  ],
  peopleTotal: 51,
  countsWidened: false,
  servers: [
    { server: { id: "srv_1", kind: "mcp", name: "Linear" }, attempts: 6 },
  ],
};

function renderSheet() {
  render(
    <MemoryRouter>
      <ProblemDetailSheet
        id="abc"
        providerId="prov_1"
        now={NOW}
        onClose={noop}
      />
    </MemoryRouter>,
  );
}

describe("SuspectedProblems", () => {
  afterEach(cleanup);
  const EMPTY = "No suspected problems signing in right now.";

  it("lists API problems with the rule and no sample badge", () => {
    render(
      <SuspectedProblems
        query={listed([PROBLEM])}
        now={NOW}
        onOpen={noop}
        empty={EMPTY}
      />,
    );
    expect(
      screen.getByText(/2 people can't connect to Linear since/),
    ).toBeTruthy();
    expect(screen.getByText(/2 or more people/)).toBeTruthy();
    expect(screen.queryByText("Sample data")).toBeNull();
  });

  it("counts every problem and says when the list is cut", () => {
    render(
      <SuspectedProblems
        query={listed([PROBLEM], { problemsTotal: 140 })}
        now={NOW}
        onOpen={noop}
        empty={EMPTY}
      />,
    );
    expect(screen.getByText("Suspected problems · 140")).toBeTruthy();
    expect(screen.getByText(/Showing the first 1\./)).toBeTruthy();
  });

  it("splits the people on this server from all of them", () => {
    render(
      <SuspectedProblems
        query={listed([{ ...PROBLEM, peopleOnServerCount: 1 }])}
        now={NOW}
        onOpen={noop}
        empty={EMPTY}
      />,
    );
    expect(screen.getByText(/1 on this server · 2 in all/)).toBeTruthy();
  });

  it("says when there is nothing", () => {
    render(
      <SuspectedProblems
        query={listed([])}
        now={NOW}
        onOpen={noop}
        empty={EMPTY}
      />,
    );
    expect(screen.getByText(EMPTY)).toBeTruthy();
  });

  it("shows nothing with logging off or to someone who reads only their own logs", () => {
    for (const more of [
      { loggingEnabled: false },
      { viewerScope: "own" as const },
    ]) {
      const { container } = render(
        <SuspectedProblems
          query={listed([PROBLEM], more)}
          now={NOW}
          onOpen={noop}
          empty={EMPTY}
        />,
      );
      expect(container.textContent).toBe("");
      cleanup();
    }
  });

  it("says when it could not load", () => {
    render(
      <SuspectedProblems
        query={{
          data: undefined,
          isPending: false,
          isError: true,
          error: new Error("x"),
        }}
        now={NOW}
        onOpen={noop}
        empty={EMPTY}
      />,
    );
    expect(screen.getByText("Couldn't load suspected problems")).toBeTruthy();
  });
});

describe("ProblemDetailSheet", () => {
  afterEach(() => {
    cleanup();
    state.query = {};
  });

  it("renders the detail and links to exactly its rows", () => {
    state.query = {
      data: {
        loggingEnabled: true,
        viewerScope: "all",
        found: true,
        problem: DETAIL,
      },
    };
    renderSheet();
    expect(
      screen.getByText("Claude Code 2.1.4, Cursor 2.0.1 · Linear, GitHub"),
    ).toBeTruthy();
    expect(screen.getByText("2 people can't connect to Linear")).toBeTruthy();
    expect(screen.getByText(/Showing the first 50 of 51/)).toBeTruthy();
    expect(
      screen.getByText("The server rejected the credential saved for it."),
    ).toBeTruthy();
    expect(screen.getByText(/1 other person failing on Linear/)).toBeTruthy();

    const link = screen.getByRole("link", { name: "Open in Connection Logs" });
    const url = new URL(link.getAttribute("href") ?? "", "http://x");
    expect(url.pathname).toBe("/providers/prov_1/logs");
    expect(url.searchParams.get("from")).toBe("2026-11-20T09:45:00.000Z");
    expect(url.searchParams.get("outcome")).toBe("failed");
    expect(url.searchParams.get("server")).toBe("srv_1,gw_1");
    expect(url.searchParams.get("person")).toBe("user:a,user:b");
    expect(url.searchParams.get("message")).toBe("static_key_rejected");
  });

  it("says the counts are wider when the filters had to be", () => {
    state.query = {
      data: {
        loggingEnabled: true,
        viewerScope: "all",
        found: true,
        problem: { ...DETAIL, countsWidened: true },
      },
    };
    renderSheet();
    expect(screen.getByText(/too many people or servers/)).toBeTruthy();
    expect(
      screen.getByRole("link", {
        name: "Open matching attempts in Connection Logs",
      }),
    ).toBeTruthy();
  });

  it("says a stale problem is no longer listed", () => {
    state.query = {
      data: { loggingEnabled: true, viewerScope: "all", found: false },
    };
    renderSheet();
    expect(screen.getByText("This problem is no longer listed.")).toBeTruthy();
  });

  it("says logging is off", () => {
    state.query = {
      data: { loggingEnabled: false, viewerScope: "all", found: false },
    };
    renderSheet();
    expect(screen.getByText("Logging is off")).toBeTruthy();
  });

  it("says when it could not load", () => {
    state.query = { isError: true };
    renderSheet();
    expect(screen.getByText("Couldn't load this problem")).toBeTruthy();
  });
});
