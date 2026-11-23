import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SERVICE_DEFINITION } from "./format";
import { ServerConnectionHealth } from "./ServerConnectionHealth";

const state = vi.hoisted(() => ({
  problems: [] as { id: string; part: string }[],
  problemsError: false,
  loggingEnabled: true,
  logsProps: [] as Record<string, unknown>[],
}));

vi.mock("./api", async (original) => ({
  ...(await original<object>()),
  useSuspectedProblems: () => ({
    data: { problems: state.problems },
    isError: state.problemsError,
  }),
  useSignInSummary: () => ({
    data: { loggingEnabled: state.loggingEnabled, viewerScope: "all" },
  }),
}));

vi.mock("./ConnectionLogsView", () => ({
  ConnectionLogsView: (props: Record<string, unknown>) => {
    state.logsProps.push(props);
    return <div data-testid="logs" />;
  },
}));

vi.mock("./SuspectedProblems", () => ({
  SuspectedProblems: ({ problems }: { problems: unknown[] }) =>
    problems.length ? <div data-testid="problems" /> : null,
}));

vi.mock("./ProblemDetailSheet", () => ({ ProblemDetailSheet: () => null }));

function renderTab(url = "/") {
  render(
    <MemoryRouter initialEntries={[url]}>
      <ServerConnectionHealth
        providerId="provider_1"
        serverId="srv_1"
        sessions={<div data-testid="sessions" />}
      />
    </MemoryRouter>,
  );
}

describe("ServerConnectionHealth", () => {
  afterEach(() => {
    cleanup();
    state.problems = [];
    state.problemsError = false;
    state.loggingEnabled = true;
    state.logsProps = [];
  });

  it("scopes the logs to the server with no link to the provider", () => {
    renderTab();
    expect(screen.getByTestId("logs")).toBeTruthy();
    expect(state.logsProps.at(-1)).toEqual({
      providerId: "provider_1",
      serverId: "srv_1",
    });
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.queryByText(/Provider:/)).toBeNull();
  });

  it("shows Active Sessions when asked", () => {
    renderTab("/?view=sessions");
    expect(screen.getByTestId("sessions")).toBeTruthy();
    expect(screen.queryByTestId("logs")).toBeNull();
  });

  it("explains a service only when a service problem is listed", () => {
    state.problems = [{ id: "p1", part: "server" }];
    renderTab();
    expect(screen.queryByText(SERVICE_DEFINITION)).toBeNull();
    cleanup();
    state.problems = [{ id: "p2", part: "service" }];
    renderTab();
    expect(screen.getByText(SERVICE_DEFINITION)).toBeTruthy();
  });

  it("shows no problems while logging is off", () => {
    state.problems = [{ id: "p1", part: "service" }];
    state.loggingEnabled = false;
    renderTab();
    expect(screen.queryByTestId("problems")).toBeNull();
  });

  it("says when problems could not load", () => {
    state.problemsError = true;
    renderTab();
    expect(screen.getByText(/Couldn't load suspected problems/)).toBeTruthy();
  });
});
