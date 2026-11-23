import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProblemDetailSheet } from "./ProblemDetailSheet";
import { SuspectedProblems } from "./SuspectedProblems";
import type { SuspectedProblem, SuspectedProblemDetail } from "./types";

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
    app: { family: "claude_code", name: "Claude Code", version: "2.1.4" },
    server: { id: "srv_1", kind: "mcp" as const, name: "Linear" },
    attempts: 3,
    firstFailedAt: "2026-11-20T10:00:00Z",
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
  people: Array.from({ length: 50 }, (_, i) => person(`p${i}`)),
  peopleTotal: 51,
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

  it("lists API problems with the rule and no sample badge", () => {
    render(<SuspectedProblems problems={[PROBLEM]} now={NOW} onOpen={noop} />);
    expect(
      screen.getByText(/2 people can't connect to Linear since/),
    ).toBeTruthy();
    expect(screen.getByText(/2 or more people/)).toBeTruthy();
    expect(screen.queryByText("Sample data")).toBeNull();
  });

  it("splits the people on this server from all of them", () => {
    render(
      <SuspectedProblems
        problems={[{ ...PROBLEM, peopleOnServerCount: 1 }]}
        now={NOW}
        onOpen={noop}
      />,
    );
    expect(screen.getByText(/1 on this server · 2 in all/)).toBeTruthy();
  });

  it("says there is nothing only when asked to", () => {
    const { container } = render(
      <SuspectedProblems problems={[]} now={NOW} onOpen={noop} />,
    );
    expect(container.textContent).toBe("");
    cleanup();
    render(
      <SuspectedProblems
        problems={[]}
        now={NOW}
        onOpen={noop}
        empty="No suspected problems signing in right now."
      />,
    );
    expect(
      screen.getByText("No suspected problems signing in right now."),
    ).toBeTruthy();
  });
});

describe("ProblemDetailSheet", () => {
  afterEach(() => {
    cleanup();
    state.query = {};
  });

  it("renders the detail and links to exactly its rows", () => {
    state.query = {
      data: { loggingEnabled: true, found: true, problem: DETAIL },
    };
    renderSheet();
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

  it("says a stale problem has cleared", () => {
    state.query = { data: { loggingEnabled: true, found: false } };
    renderSheet();
    expect(screen.getByText("This problem has cleared")).toBeTruthy();
  });

  it("says when it could not load", () => {
    state.query = { isError: true };
    renderSheet();
    expect(screen.getByText("Couldn't load this problem")).toBeTruthy();
  });
});
