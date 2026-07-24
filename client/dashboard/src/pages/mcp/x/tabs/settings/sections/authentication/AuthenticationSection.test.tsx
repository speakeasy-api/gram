import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthenticationSectionBody } from "./AuthenticationSection";
import type { AuthTarget } from "./authTarget";

const { useProtectedResourceMetadata, useAllRemoteSessionClients } = vi.hoisted(
  () => ({
    useProtectedResourceMetadata: vi.fn(),
    useAllRemoteSessionClients: vi.fn(),
  }),
);

vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: () => ({
    data: { id: "user-session-issuer" },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuers: () => ({
    data: { result: { items: [] } },
    isLoading: false,
  }),
}));

vi.mock("./useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: (...args: unknown[]) =>
    useAllRemoteSessionClients(...args),
}));

vi.mock("./useProtectedResourceMetadata", () => ({
  useProtectedResourceMetadata: (...args: unknown[]) =>
    useProtectedResourceMetadata(...args),
}));

vi.mock("./AttachRemoteIdentityProviderSheet", () => ({
  AttachRemoteIdentityProviderSheet: ({
    open,
    initialIssuerUrl,
    initialScopes,
  }: {
    open: boolean;
    initialIssuerUrl?: string;
    initialScopes?: string[];
  }) =>
    open ? (
      <output>
        {initialIssuerUrl}|{initialScopes?.join(" ")}
      </output>
    ) : null,
}));

vi.mock("./RemoteIdentityProvidersField", () => ({
  RemoteIdentityProvidersField: ({ onAdd }: { onAdd: () => void }) => (
    <button onClick={onAdd}>Add provider</button>
  ),
}));

vi.mock("./UserSessionDurationField", () => ({
  UserSessionDurationField: () => null,
}));

vi.mock("./McpServerSessionsPanel", () => ({
  McpServerSessionsPanel: () => null,
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("AuthenticationSectionBody", () => {
  it("preserves protected-resource scopes when recovering an unconfigured remote server", () => {
    useAllRemoteSessionClients.mockReturnValue({
      items: [],
      isLoading: false,
    });
    useProtectedResourceMetadata.mockReturnValue({
      status: "available",
      metadata: {
        authorizationServers: ["https://auth.example.com"],
        scopesSupported: ["resource.read", "resource.write"],
      },
    });

    render(<AuthenticationSectionBody target={remoteTarget} />);

    expect(useProtectedResourceMetadata).toHaveBeenCalledWith(
      "remote-mcp-server",
      true,
    );

    fireEvent.click(screen.getByText("Add provider"));

    expect(
      screen.getByText("https://auth.example.com|resource.read resource.write"),
    ).toBeDefined();
  });

  it("does not probe configured remote servers that already have a client", () => {
    useAllRemoteSessionClients.mockReturnValue({
      items: [{ id: "remote-session-client" }],
      isLoading: false,
    });
    useProtectedResourceMetadata.mockReturnValue({
      status: "idle",
      metadata: null,
    });

    render(<AuthenticationSectionBody target={remoteTarget} />);

    expect(useProtectedResourceMetadata).toHaveBeenCalledWith(
      "remote-mcp-server",
      false,
    );
  });
});

const remoteTarget: AuthTarget = {
  slug: "remote-server",
  userSessionIssuerId: "user-session-issuer",
  remoteMcpServerId: "remote-mcp-server",
  invalidate: vi.fn(),
};
