import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { RemoteMcpToolsSection } from "./RemoteMcpToolsSection";

const mocks = vi.hoisted(() => ({
  needsAuth: true,
  open: vi.fn(),
  tools: undefined as
    | undefined
    | Record<string, { unavailable: boolean; unavailableReason?: string }>,
  stored: {} as Record<string, { toolName: string }>,
  canWrite: true,
  chatError: null as Error | null,
  chatRefetch: vi.fn(),
}));

vi.mock("@/hooks/useProxiedMcpTools", () => ({
  useProxiedMcpTools: () => ({
    tools: mocks.tools,
    isLoading: false,
    needsAuth: mocks.needsAuth,
    isError: false,
    refetch: vi.fn(),
    error: null,
  }),
}));

vi.mock("../../inspect/useToolHealth", async () => {
  const actual = await vi.importActual<
    typeof import("../../inspect/useToolHealth")
  >("../../inspect/useToolHealth");
  return {
    ...actual,
    useToolHealth: () => ({ healthFor: () => undefined, isLoading: false }),
  };
});

vi.mock("../../inspect/useChatSessionHeaders", () => ({
  useChatSessionHeaders: () => ({
    headers: undefined,
    isLoading: false,
    error: mocks.chatError,
    refetch: mocks.chatRefetch,
  }),
}));

vi.mock("@/pages/playground/PlaygroundChat", () => ({
  PlaygroundChat: ({ mcpUrl }: { mcpUrl: string }) => (
    <div data-testid="chat" data-url={mcpUrl} />
  ),
}));

vi.mock("@/hooks/useUserSessionToken", () => ({
  useUserSessionToken: () => ({ accessToken: undefined, isLoading: false }),
}));

vi.mock("@/hooks/useToolMetadata", () => ({
  useToolMetadata: () => ({
    metadataByTool: mocks.stored,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("./useSyncToolMetadata", () => ({
  useSyncToolMetadata: () => ({
    sync: vi.fn(),
    isSyncing: false,
    canWrite: mocks.canWrite,
  }),
}));

vi.mock("@/lib/utils", async () => {
  const actual =
    await vi.importActual<typeof import("@/lib/utils")>("@/lib/utils");
  return {
    ...actual,
    mcpConnectionUrl: (url: string | undefined) => url,
    getServerURL: () => "https://gram.example",
    firstPartyConnectUrl: (url: string | undefined) =>
      url ? `${url}/connect/first-party` : undefined,
  };
});

function renderSection(
  props: Partial<ComponentProps<typeof RemoteMcpToolsSection>> = {},
): void {
  render(
    <MemoryRouter>
      <TooltipProvider>
        <RemoteMcpToolsSection
          mcpUrl="https://mcp.example/mcp/custom-slug"
          isResolvingUrl={false}
          mcpServerId="mcp-server-1"
          userSessionIssuerId={undefined}
          isDisabled={false}
          authSettingsHref="/settings#authentication"
          platformSlug="server"
          {...props}
        />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

const connectButton = { name: "Connect account" };

afterEach(() => {
  cleanup();
  mocks.needsAuth = true;
  mocks.open.mockReset();
  mocks.tools = undefined;
  mocks.stored = {};
  mocks.canWrite = true;
  mocks.chatError = null;
  mocks.chatRefetch.mockReset();
});

describe("RemoteMcpToolsSection chat session", () => {
  it("shows a retryable error when the chat session can't be created", () => {
    mocks.needsAuth = false;
    mocks.chatError = new Error("boom");
    renderSection();

    expect(
      screen.getByText("Couldn't connect to this server to list its tools."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.chatRefetch).toHaveBeenCalled();
  });
});

describe("RemoteMcpToolsSection tool list", () => {
  const remote = { remoteMcpServerId: "remote-1" };

  it("separates tools this viewer can't use", () => {
    mocks.needsAuth = false;
    mocks.tools = {
      search: { unavailable: false },
      delete_all: { unavailable: true, unavailableReason: "blocked" },
    };
    renderSection(remote);

    expect(screen.getByText("Not available to you · 1")).toBeTruthy();
    expect(screen.getByText("delete_all")).toBeTruthy();
    expect(screen.getByText("Blocked by a role rule")).toBeTruthy();
    expect(screen.getByText(/1 of 2 tools/)).toBeTruthy();
  });

  it("does not separate the list when every tool is available", () => {
    mocks.needsAuth = false;
    mocks.tools = { search: { unavailable: false } };
    renderSection(remote);

    expect(screen.getByText("search")).toBeTruthy();
    expect(screen.queryByText(/^Not available to you/)).toBeNull();
  });

  it("offers sync to someone who can write once tools are stored", () => {
    mocks.needsAuth = false;
    mocks.tools = { search: { unavailable: false } };
    mocks.stored = { old_tool: { toolName: "old_tool" } };
    renderSection(remote);

    // The notice is one line; the diff and Sync open from Review.
    fireEvent.click(screen.getByRole("button", { name: /review/i }));
    expect(screen.getByRole("button", { name: /sync/i })).toBeTruthy();
  });

  it("does not offer sync before any tools are stored", () => {
    mocks.needsAuth = false;
    mocks.tools = { search: { unavailable: false } };
    renderSection(remote);

    expect(screen.queryByRole("button", { name: /sync/i })).toBeNull();
  });

  it("does not offer sync to someone who can't write", () => {
    mocks.needsAuth = false;
    mocks.canWrite = false;
    mocks.tools = { search: { unavailable: false } };
    mocks.stored = { old_tool: { toolName: "old_tool" } };
    renderSection(remote);

    expect(screen.queryByRole("button", { name: /sync/i })).toBeNull();
  });
});

describe("RemoteMcpToolsSection connect prompt", () => {
  it("does not open first-party connect when the server has no issuer", () => {
    vi.stubGlobal("open", mocks.open);
    renderSection();

    expect(screen.queryByRole("button", connectButton)).toBeNull();
    expect(screen.getByText(/no authentication configured yet/i)).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Configure authentication" }),
    ).toBeTruthy();
    expect(mocks.open).not.toHaveBeenCalled();
  });

  it("opens the platform slug connect page for an issuer-gated server", () => {
    vi.stubGlobal("open", mocks.open);
    renderSection({ userSessionIssuerId: "issuer-1" });

    fireEvent.click(screen.getByRole("button", connectButton));

    expect(mocks.open).toHaveBeenCalledWith(
      "https://gram.example/mcp/server/connect/first-party",
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("does not offer connect for a public tunnel", () => {
    renderSection({
      userSessionIssuerId: "issuer-1",
      tunneledMcpServerId: "tunnel-1",
      visibility: "public",
    });

    expect(screen.queryByRole("button", connectButton)).toBeNull();
    expect(screen.getByText(/Connect isn't available/i)).toBeTruthy();
  });

  it("does not offer connect when only a custom-domain address exists", () => {
    renderSection({
      userSessionIssuerId: "issuer-1",
      platformSlug: undefined,
    });

    expect(screen.queryByRole("button", connectButton)).toBeNull();
    expect(screen.getByText(/Connect isn't available/i)).toBeTruthy();
  });
});
