import { TooltipProvider } from "@/components/ui/Tooltip";
import type { HeaderDraftsState } from "@/lib/remote-identity";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TunneledHeadersSection } from "./TunneledHeadersSection";

const PROJECT = "project-1";

const mocks = vi.hoisted(() => ({
  // Whether the caller holds mcp:write for (resourceId, projectId).
  canWrite: vi.fn<(resourceId?: string, projectId?: string) => boolean>(),
  draftsArgs: vi.fn(),
  state: undefined as unknown as HeaderDraftsState,
  siblings: [] as Array<{
    id: string;
    name: string;
    tunneledMcpServerId: string;
  }>,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (_scope: string, resourceId?: string, projectId?: string) =>
      mocks.canWrite(resourceId, projectId),
    hasAnyScope: (_scopes: string[], resourceId?: string, projectId?: string) =>
      mocks.canWrite(resourceId, projectId),
    hasAllScopes: (
      _scopes: string[],
      resourceId?: string,
      projectId?: string,
    ) => mocks.canWrite(resourceId, projectId),
    isLoading: false,
  }),
}));

vi.mock("@/lib/remote-identity", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/remote-identity")>()),
  useTunneledHeaderDrafts: (args: unknown) => {
    mocks.draftsArgs(args);
    return mocks.state;
  },
}));

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({ data: { mcpServers: mocks.siblings } }),
}));

function state(overrides: Partial<HeaderDraftsState> = {}): HeaderDraftsState {
  return {
    drafts: [],
    authorization: { unknown: false },
    readOnly: false,
    isLoading: false,
    isDirty: true,
    validationError: null,
    fieldErrors: new Map(),
    reportErrors: false,
    saving: false,
    error: null,
    loadError: false,
    addHeader: vi.fn(() => {}),
    replaceHeader: vi.fn(() => {}),
    removeHeader: vi.fn(() => {}),
    save: vi.fn(async () => true),
    discard: vi.fn(() => {}),
    ...overrides,
  };
}

const tunnel = {
  id: "tunnel-1",
  projectId: PROJECT,
  name: "jamf",
} as TunneledMcpServer;

function renderSection(): void {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <TooltipProvider>
          <TunneledHeadersSection
            tunneledMcpServer={tunnel}
            mcpServerId="server-1"
          />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function saveButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: /save/i }) as HTMLButtonElement;
}

beforeEach(() => {
  mocks.state = state();
  mocks.siblings = [];
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("TunneledHeadersSection", () => {
  it("lets a writer for the tunnel's project edit and save", () => {
    mocks.canWrite.mockImplementation(
      (resourceId, projectId) =>
        resourceId === PROJECT && projectId === PROJECT,
    );
    renderSection();

    expect(mocks.draftsArgs).toHaveBeenLastCalledWith(
      expect.objectContaining({
        tunneledMcpServerId: "tunnel-1",
        readOnly: false,
      }),
    );
    expect(saveButton().disabled).toBe(false);
  });

  it("checks the project dimension, not only the resource", () => {
    // A project-wide grant for another project matches when the project
    // dimension is left out of the check.
    mocks.canWrite.mockImplementation(
      (_resourceId, projectId) => projectId === undefined,
    );
    renderSection();

    expect(mocks.draftsArgs).toHaveBeenLastCalledWith(
      expect.objectContaining({ readOnly: true }),
    );
    expect(
      screen
        .queryByRole("button", { name: /save/i })
        ?.hasAttribute("disabled") ?? true,
    ).toBe(true);
  });

  it("locks a principal who can write only this MCP server", () => {
    mocks.canWrite.mockImplementation(
      (resourceId) => resourceId === "server-1",
    );
    renderSection();

    expect(mocks.draftsArgs).toHaveBeenLastCalledWith(
      expect.objectContaining({ readOnly: true }),
    );
  });

  it("refuses to save when the headers could not be loaded", () => {
    mocks.canWrite.mockReturnValue(true);
    mocks.state = state({ loadError: true, readOnly: true });
    renderSection();

    expect(
      screen.getByText(/could not load this tunnel's headers/i),
    ).toBeTruthy();
    expect(saveButton().disabled).toBe(true);
  });

  it("always says the headers are shared across the tunnel", () => {
    mocks.canWrite.mockReturnValue(true);
    mocks.siblings = [
      { id: "server-1", name: "This server", tunneledMcpServerId: "tunnel-1" },
      { id: "server-2", name: "Sandbox", tunneledMcpServerId: "tunnel-1" },
    ];
    renderSection();

    expect(screen.getByText(/apply to every MCP server on it/i)).toBeTruthy();
    expect(screen.getByText("Sandbox")).toBeTruthy();
    expect(screen.queryByText("This server")).toBeNull();
    expect(
      screen.getByText(/cannot\s+pass them to that process/i),
    ).toBeTruthy();
  });
});
