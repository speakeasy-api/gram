import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Watchdog from "./Watchdog";

const mocks = vi.hoisted(() => ({
  selectedServerId: "11111111-1111-4111-8111-111111111111",
  nextServerId: "22222222-2222-4222-8222-222222222222",
  useRiskSignals: vi.fn(),
}));

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
  function Toolbar({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }
  Toolbar.Filters = ({
    onChange,
  }: {
    onChange: (id: string, value: string) => void;
  }) => (
    <button
      type="button"
      onClick={() => onChange("mcp_server_id", mocks.nextServerId)}
    >
      Filter server
    </button>
  );
  Toolbar.Leading = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  return { Page: Object.assign(Page, { Header, Body, Section, Toolbar }) };
});

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("./SuppressedFindings", () => ({
  SuppressedFindings: () => <div>Suppressed section</div>,
}));

// All three reach the SDK provider, which this test has no use for.
vi.mock("./SignalDrawer", () => ({ SignalDrawer: () => null }));
vi.mock("./AnalysisStatusBadge", () => ({ AnalysisStatusBadge: () => null }));

vi.mock("../useDismissFinding", () => ({
  useDismissFinding: () => ({
    dismiss: vi.fn(),
    restore: vi.fn(),
    isOptimisticallyDismissed: () => false,
    optimisticallyRestoredIds: new Set<string>(),
  }),
}));

vi.mock("@gram/client/react-query/riskSignals.js", () => ({
  useRiskSignals: mocks.useRiskSignals,
  invalidateAllRiskSignals: vi.fn(),
}));

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: {
      mcpServers: [
        {
          id: mocks.selectedServerId,
          name: "Selected server",
          slug: "selected",
        },
        { id: mocks.nextServerId, name: "Next server", slug: "next" },
      ],
    },
  }),
}));

vi.mock("@gram/client/react-query/riskCreateExclusion.js", () => ({
  useRiskCreateExclusionMutation: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    isPending: false,
    isError: false,
    data: { logsEnabled: true },
  }),
}));

// Only the client accessor needs standing in for — the rest of the module
// (slug helpers used by routing) works fine as-is.
vi.mock("@/contexts/Sdk", async (importOriginal) => {
  const original = await importOriginal<Record<string, unknown>>();
  return {
    ...original,
    useProjectSlugForRequests: () => "default",
    useSdkClient: () => ({}),
  };
});

afterEach(cleanup);

beforeEach(() => {
  mocks.useRiskSignals.mockReset();
  mocks.useRiskSignals.mockReturnValue({
    data: undefined,
    error: new Error("signals are down"),
    isLoading: false,
    refetch: vi.fn(),
  });
});

function LocationSearch(): JSX.Element {
  return <output>{useLocation().search}</output>;
}

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

  it("round-trips the MCP server filter through the URL and signals request", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter
          initialEntries={[
            `/acme/projects/default/watchdog?mcp_server_id=${mocks.selectedServerId}`,
          ]}
        >
          <Watchdog />
          <LocationSearch />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(mocks.useRiskSignals).toHaveBeenCalledWith(
      expect.objectContaining({ mcpServerId: mocks.selectedServerId }),
      undefined,
      expect.any(Object),
    );

    fireEvent.click(screen.getByRole("button", { name: "Filter server" }));

    await waitFor(() => {
      expect(
        screen.getByText(`?mcp_server_id=${mocks.nextServerId}`),
      ).toBeTruthy();
      expect(mocks.useRiskSignals).toHaveBeenCalledWith(
        expect.objectContaining({ mcpServerId: mocks.nextServerId }),
        undefined,
        expect.any(Object),
      );
    });
  });
});
