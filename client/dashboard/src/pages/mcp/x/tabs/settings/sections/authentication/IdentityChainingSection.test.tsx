import type { ReactNode } from "react";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
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
  sourceError: false,
  hash: "",
  refetch: vi.fn(),
}));

vi.mock("./authTarget", () => ({
  useMcpServerAuthTarget: (server: McpServer) => ({
    remoteMcpServerId: server.remoteMcpServerId,
    userSessionIssuerId: server.userSessionIssuerId,
  }),
}));
vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: () => ({
    data: state.issuer,
    isLoading: false,
    isError: false,
    refetch: state.refetch,
  }),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer.js", () => ({
  useGetRemoteMcpServer: () => ({
    data: state.url ? { url: state.url } : undefined,
    isLoading: !state.url && !state.sourceError,
    isError: state.sourceError,
    refetch: state.refetch,
  }),
}));
vi.mock("@/lib/remote-identity", () => ({
  useAllRemoteSessionClients: () => ({
    items: [{ id: "client" }],
    isLoading: state.clientsLoading,
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
}));
vi.mock("@/pages/org/identity-provider/tabs", () => ({
  identityProvidersHref: () => "?tab=identity-providers",
}));
vi.mock("react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
  useLocation: () => ({ hash: state.hash }),
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
  useSpeakeasyIssuerStatus: () => ({ data: state.speakeasy, isLoading: false }),
}));
vi.mock("./IdentityChainingField", () => ({
  IdentityChainingField: (props: {
    providers: { external: boolean };
    speakeasyIssuerUrl?: string;
    resource: string;
  }) => (
    <div data-testid="identity-chaining">
      {JSON.stringify({
        providers: props.providers,
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
  state.sourceError = false;
  state.hash = "";
  state.refetch.mockReset();
});

afterEach(cleanup);

describe("IdentityChainingSection", () => {
  it("is its own section on an organization issuer Speakeasy can vouch for", () => {
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Authorization" }),
    ).toBeTruthy();
    expect(
      screen.getByText(/from your identity provider or Speakeasy/),
    ).toBeTruthy();
    expect(
      JSON.parse(screen.getByTestId("identity-chaining").textContent!),
    ).toEqual({
      providers: { external: false },
      speakeasyIssuerUrl: "https://speakeasy.example.com/idp",
      resource: "https://mcp.vendor.example.com/mcp",
    });
  });

  it("says a project sign-in can't chain, and where to change it", () => {
    state.issuer = { id: "issuer", projectId: "project", slug: "team-only" };
    render(<IdentityChainingSection mcpServer={server} />);
    expect(screen.queryByTestId("identity-chaining")).toBeNull();
    expect(
      screen.getByText(/needs an organization-level sign-in/).textContent,
    ).toContain("This server’s sign-in belongs to this project.");
    expect(screen.queryByText(/team-only/)).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Change in Sessions" })
        .getAttribute("href"),
    ).toBe("#sessions");
  });

  it("points at setting Speakeasy up when nothing can vouch yet", () => {
    state.speakeasy = { state: "setup_required" };
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Authorization" }),
    ).toBeTruthy();
    expect(screen.queryByTestId("identity-chaining")).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Set up Speakeasy as an identity provider" })
        .getAttribute("href"),
    ).toBe("/org/identity?tab=identity-providers");
  });

  it.each([
    [
      "nothing can vouch",
      () => {
        state.speakeasy = undefined;
      },
    ],
    [
      "Speakeasy isn't rolled out and nothing else can vouch",
      () => {
        state.speakeasy = { state: "setup_required" };
        state.flags = { oktaConnections: true, emaProviders: false };
      },
    ],
    [
      "the issuer belongs to a project and nothing could vouch",
      () => {
        state.issuer = { id: "issuer", projectId: "project" };
        state.speakeasy = undefined;
      },
    ],
    [
      "the rollouts are off",
      () => {
        state.flags = { oktaConnections: false, emaProviders: false };
      },
    ],
  ])("is hidden when %s", (_name, arrange) => {
    arrange();
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.queryByRole("heading", {
        name: "Enterprise Managed Authorization",
      }),
    ).toBe(null);
  });

  it.each([
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
  ])("holds its place while %s", (_name, arrange) => {
    arrange();
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Authorization" }),
    ).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe(
      "Loading Enterprise Managed Authorization…",
    );
    expect(screen.queryByTestId("identity-chaining")).toBeNull();
  });

  it("says when it couldn't load, with a retry", () => {
    state.url = undefined;
    state.sourceError = true;
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByText(
        "Couldn’t load Enterprise Managed Authorization for this server.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetch).toHaveBeenCalled();
  });

  it("scrolls to itself once it has loaded on a deep link", () => {
    state.hash = "#identity-chaining";
    state.clientsLoading = true;
    const scroll = vi.fn<(arg?: boolean | ScrollIntoViewOptions) => void>();
    Element.prototype.scrollIntoView = scroll;
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => {
      cb(0);
      return 1;
    });
    const { rerender } = render(<IdentityChainingSection mcpServer={server} />);
    expect(scroll).not.toHaveBeenCalled();
    state.clientsLoading = false;
    rerender(<IdentityChainingSection mcpServer={server} />);
    expect(scroll).toHaveBeenCalledTimes(1);
    rerender(<IdentityChainingSection mcpServer={server} />);
    expect(scroll).toHaveBeenCalledTimes(1);
  });
});
