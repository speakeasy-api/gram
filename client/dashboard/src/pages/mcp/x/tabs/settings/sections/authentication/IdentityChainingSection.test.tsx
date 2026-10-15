import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityChainingSection } from "./IdentityChainingSection";

const state = vi.hoisted(() => ({
  issuer: undefined as Record<string, unknown> | undefined,
  speakeasy: undefined as Record<string, unknown> | undefined,
  flags: { oktaConnections: true, emaProviders: true } as Record<
    string,
    boolean
  >,
  clientsLoading: false,
  url: "https://mcp.vendor.example.com/mcp" as string | undefined,
}));

vi.mock("./authTarget", () => ({
  useMcpServerAuthTarget: (server: McpServer) => ({
    remoteMcpServerId: server.remoteMcpServerId,
    userSessionIssuerId: server.userSessionIssuerId,
  }),
}));
vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: () => ({ data: state.issuer }),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer.js", () => ({
  useGetRemoteMcpServer: () => ({
    data: state.url ? { url: state.url } : undefined,
  }),
}));
vi.mock("@/lib/remote-identity", () => ({
  useAllRemoteSessionClients: () => ({
    items: [{ id: "client" }],
    isLoading: state.clientsLoading,
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: (flag: string) => ({
    status: state.flags[flag] ? "enabled" : "disabled",
  }),
}));
vi.mock("@/lib/featureFlags", () => ({
  FEATURE_FLAGS: {
    oktaConnections: "oktaConnections",
    emaProviders: "emaProviders",
  },
}));
vi.mock("./useSpeakeasyIssuerStatus", () => ({
  useSpeakeasyIssuerStatus: () => ({ data: state.speakeasy }),
}));
vi.mock("./IdentityChainingField", () => ({
  IdentityChainingField: (props: {
    providers: { external: boolean; speakeasy: boolean };
    speakeasyEnabled?: boolean;
    speakeasyIssuerUrl?: string;
    resource: string;
  }) => (
    <div data-testid="identity-chaining">
      {JSON.stringify({
        providers: props.providers,
        speakeasyEnabled: props.speakeasyEnabled,
        speakeasyIssuerUrl: props.speakeasyIssuerUrl,
        resource: props.resource,
      })}
    </div>
  ),
}));

const server = {
  id: "mcp-server",
  remoteMcpServerId: "remote",
  userSessionIssuerId: "issuer",
} as McpServer;

beforeEach(() => {
  state.issuer = { id: "issuer", projectId: "" };
  state.speakeasy = {
    state: "enabled",
    issuer: "https://speakeasy.example.com/idp",
  };
  state.flags = { oktaConnections: true, emaProviders: true };
  state.clientsLoading = false;
  state.url = "https://mcp.vendor.example.com/mcp";
});

afterEach(cleanup);

describe("IdentityChainingSection", () => {
  it("is its own section on an organization issuer Speakeasy can vouch for", () => {
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByRole("heading", { name: "Identity chaining" }),
    ).toBeTruthy();
    expect(
      screen.getByText(/from your identity provider or Speakeasy/),
    ).toBeTruthy();
    expect(
      JSON.parse(screen.getByTestId("identity-chaining").textContent!),
    ).toEqual({
      providers: { external: false, speakeasy: true },
      speakeasyEnabled: true,
      speakeasyIssuerUrl: "https://speakeasy.example.com/idp",
      resource: "https://mcp.vendor.example.com/mcp",
    });
  });

  it.each([
    [
      "nothing can vouch",
      () => {
        state.speakeasy = undefined;
      },
    ],
    [
      "the issuer belongs to a project",
      () => {
        state.issuer = { id: "issuer", projectId: "project" };
      },
    ],
    [
      "the rollouts are off",
      () => {
        state.flags = { oktaConnections: false, emaProviders: false };
      },
    ],
    [
      "the remote URL hasn't loaded",
      () => {
        state.url = undefined;
      },
    ],
    [
      "linked clients are still loading",
      () => {
        state.clientsLoading = true;
      },
    ],
  ])("is hidden when %s", (_name, arrange) => {
    arrange();
    render(<IdentityChainingSection mcpServer={server} />);
    expect(screen.queryByRole("heading", { name: "Identity chaining" })).toBe(
      null,
    );
  });
});
