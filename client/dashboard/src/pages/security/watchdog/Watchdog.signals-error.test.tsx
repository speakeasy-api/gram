import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import Watchdog from "./Watchdog";

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }
  function Header({ children }: { children?: ReactNode }) {
    return <div>{children}</div>;
  }
  Header.Breadcrumbs = () => <nav>Breadcrumbs</nav>;
  function Body({ children }: { children: ReactNode }) {
    return <main>{children}</main>;
  }
  function Section({ children }: { children: ReactNode }) {
    return <section>{children}</section>;
  }
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h1>{children}</h1>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.CTA = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  Section.Body = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  return { Page: Object.assign(Page, { Header, Body, Section }) };
});

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("./SuppressedFindings", () => ({
  SuppressedFindings: () => <div>Suppressed section</div>,
}));

// Both reach the SDK provider, which this test has no use for.
vi.mock("./SignalDrawer", () => ({ SignalDrawer: () => null }));

vi.mock("../useDismissFinding", () => ({
  useDismissFinding: () => ({
    dismiss: vi.fn(),
    restore: vi.fn(),
    isOptimisticallyDismissed: () => false,
    optimisticallyRestoredIds: new Set<string>(),
  }),
}));

vi.mock("@gram/client/react-query/riskSignals.js", () => ({
  useRiskSignals: () => ({
    data: undefined,
    error: new Error("signals are down"),
    isLoading: false,
  }),
  invalidateAllRiskSignals: vi.fn(),
}));

vi.mock("@gram/client/react-query/riskCreateExclusion.js", () => ({
  useRiskCreateExclusionMutation: () => ({ mutateAsync: vi.fn() }),
}));

// Only the client accessor needs standing in for — the rest of the module
// (slug helpers used by routing) works fine as-is.
vi.mock("@/contexts/Sdk", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Sdk")>()),
  useSdkClient: () => ({}),
}));

afterEach(cleanup);

describe("Watchdog with a failing signals query", () => {
  it("keeps the suppressed section on the page", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/acme/projects/default/watchdog"]}>
          <Watchdog />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(screen.getByText("Error loading watchdog signals")).toBeTruthy();
    // The suppressed listing is a different endpoint with its own error
    // handling, so a signals outage must not take the audit trail down too.
    expect(screen.getByText("Suppressed section")).toBeTruthy();
  });
});
