import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RemoteMcpToolsSection } from "./RemoteMcpToolsSection";

const mocks = vi.hoisted(() => ({
  needsAuth: true,
  open: vi.fn(),
}));

vi.mock("@/hooks/useProxiedMcpTools", () => ({
  useProxiedMcpTools: () => ({
    tools: undefined,
    isLoading: false,
    needsAuth: mocks.needsAuth,
    isError: false,
    refetch: vi.fn(),
    error: null,
  }),
}));

vi.mock("@/hooks/useUserSessionToken", () => ({
  useUserSessionToken: () => ({ accessToken: undefined, isLoading: false }),
}));

vi.mock("@/hooks/useToolMetadata", () => ({
  useToolMetadata: () => ({
    metadataByTool: {},
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("./useSyncToolMetadata", () => ({
  useSyncToolMetadata: () => ({ sync: vi.fn(), isSyncing: false }),
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
    </MemoryRouter>,
  );
}

describe("RemoteMcpToolsSection connect prompt", () => {
  afterEach(() => {
    cleanup();
    mocks.needsAuth = true;
    mocks.open.mockReset();
  });

  it("does not open first-party connect when the server has no issuer", () => {
    vi.stubGlobal("open", mocks.open);
    renderSection();

    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(screen.getByText(/no authentication configured yet/i)).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Configure authentication" }),
    ).toBeTruthy();
    expect(mocks.open).not.toHaveBeenCalled();
  });

  it("opens the platform slug connect page for an issuer-gated server", () => {
    vi.stubGlobal("open", mocks.open);
    renderSection({ userSessionIssuerId: "issuer-1" });

    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

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

    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(screen.getByText(/Connect isn't available/i)).toBeTruthy();
  });

  it("does not offer connect when only a custom-domain address exists", () => {
    renderSection({
      userSessionIssuerId: "issuer-1",
      platformSlug: undefined,
    });

    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(screen.getByText(/Connect isn't available/i)).toBeTruthy();
  });
});
