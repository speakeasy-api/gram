import type { ReactNode } from "react";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityChainingSection } from "./IdentityChainingSection";

const state = vi.hoisted(() => ({
  issuer: undefined as Record<string, unknown> | undefined,
  speakeasy: undefined as Record<string, unknown> | undefined,
  flags: { emaProviders: true } as Record<string, boolean>,
  clientsLoading: false,
  clientsError: false,
  clientsRefetch: vi.fn(),
  url: "https://mcp.vendor.example.com/mcp" as string | undefined,
  sourceError: false,
  hash: "",
  refetch: vi.fn(),
  speakeasyStatus: "success" as "success" | "loading" | "error" | "disabled",
  speakeasyRefetch: vi.fn(),
  orgAdmin: true,
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
    isError: state.clientsError,
    refetch: state.clientsRefetch,
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
  useRoutes: () => ({
    remoteIdentityProviders: {
      clientDetail: {
        overview: {
          href: (issuerId: string, clientId: string) =>
            `/rip/${issuerId}/clients/${clientId}/overview`,
        },
      },
    },
  }),
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
    emaProviders: "emaProviders",
  },
}));
vi.mock("./useSpeakeasyIssuerStatus", () => ({
  useSpeakeasyIssuerStatus: () => ({
    data: state.speakeasyStatus === "success" ? state.speakeasy : undefined,
    isLoading: state.speakeasyStatus === "loading",
    isPending: state.speakeasyStatus !== "success",
    isError: state.speakeasyStatus === "error",
    fetchStatus: state.speakeasyStatus === "loading" ? "fetching" : "idle",
    refetch: state.speakeasyRefetch,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope !== "org:admin" || state.orgAdmin,
  }),
}));
vi.mock("./IdentityChainingField", () => ({
  IdentityChainingField: (props: {
    providers: { external: boolean };
    speakeasyIssuerUrl?: string;
    resource: string;
    onlyWhenOn?: boolean;
    frame?: (content: ReactNode) => ReactNode;
  }) => {
    const content = (
      <div data-testid="identity-chaining">
        {JSON.stringify({
          providers: props.providers,
          speakeasyIssuerUrl: props.speakeasyIssuerUrl,
          resource: props.resource,
          ...(props.onlyWhenOn ? { onlyWhenOn: true } : {}),
        })}
      </div>
    );
    return props.frame ? props.frame(content) : content;
  },
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
  state.flags = { emaProviders: true };
  state.clientsLoading = false;
  state.clientsError = false;
  state.clientsRefetch.mockReset();
  state.url = "https://mcp.vendor.example.com/mcp";
  state.sourceError = false;
  state.hash = "";
  state.refetch.mockReset();
  state.speakeasyStatus = "success";
  state.speakeasyRefetch.mockReset();
  state.orgAdmin = true;
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("IdentityChainingSection", () => {
  it("is its own section on an organization issuer Speakeasy can vouch for", () => {
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Authorization" }),
    ).toBeTruthy();
    expect(screen.getByText(/using their existing login/)).toBeTruthy();
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

  it("sends a verified-domain gap to the Single sign-on tab", () => {
    state.speakeasy = { state: "verified_domain_required" };
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen
        .getByRole("link", {
          name: "Verify a domain on the Single sign-on tab",
        })
        .getAttribute("href"),
    ).toBe("/org/identity?tab=sso");
    expect(
      screen.queryByRole("link", {
        name: "Set up Speakeasy as an identity provider",
      }),
    ).toBeNull();
  });

  it("asks someone who isn't an org admin to have Speakeasy set up", () => {
    state.orgAdmin = false;
    state.speakeasy = { state: "setup_required" };
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.queryByRole("link", {
        name: "Set up Speakeasy as an identity provider",
      }),
    ).toBeNull();
    expect(
      screen.getByText(
        /An organization admin can set up Speakeasy as an identity provider/,
      ),
    ).toBeTruthy();
  });

  it("holds its place while Speakeasy's issuer loads", () => {
    state.speakeasyStatus = "loading";
    render(<IdentityChainingSection mcpServer={server} />);
    expect(screen.getByRole("status").textContent).toBe(
      "Loading Enterprise Managed Authorization…",
    );
  });

  it("says when Speakeasy's issuer couldn't load, with a retry", () => {
    state.speakeasyStatus = "error";
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByText(
        "Couldn’t load Enterprise Managed Authorization for this server.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.speakeasyRefetch).toHaveBeenCalled();
  });

  it("keeps an on binding manageable for someone who can't read Speakeasy's issuer", () => {
    state.speakeasyStatus = "disabled";
    state.orgAdmin = false;
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Authorization" }),
    ).toBeTruthy();
    expect(
      JSON.parse(screen.getByTestId("identity-chaining").textContent!),
    ).toEqual({
      providers: { external: false },
      resource: "https://mcp.vendor.example.com/mcp",
      onlyWhenOn: true,
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
      "Speakeasy isn't rolled out and nothing else can vouch",
      () => {
        state.speakeasy = { state: "unavailable" };
        state.flags = { emaProviders: false };
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
      "the issuer belongs to a project and Speakeasy isn't set up",
      () => {
        state.issuer = { id: "issuer", projectId: "project" };
        state.speakeasy = { state: "setup_required" };
      },
    ],
    [
      "the issuer belongs to a project whose identity provider has no client",
      () => {
        state.issuer = {
          id: "issuer",
          projectId: "project",
          trustedRemoteSessionIssuerId: "okta",
        };
        state.speakeasy = { state: "verified_domain_required" };
      },
    ],
    [
      "the EMA provider rollout is off",
      () => {
        state.flags = { emaProviders: false };
        state.speakeasy = { state: "unavailable" };
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

  it("stays hidden when Speakeasy's issuer is unavailable even with ema-providers on", () => {
    state.flags = { emaProviders: true };
    state.speakeasy = { state: "unavailable" };
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.queryByRole("heading", {
        name: "Enterprise Managed Authorization",
      }),
    ).toBe(null);
  });

  it("shows when Speakeasy's issuer is offered while ema-providers is off", () => {
    state.flags = { emaProviders: false };
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByRole("heading", {
        name: "Enterprise Managed Authorization",
      }),
    ).toBeTruthy();
  });

  it.each([
    [
      "loading",
      () => {
        state.clientsLoading = true;
      },
    ],
    [
      "failing",
      () => {
        state.url = undefined;
        state.sourceError = true;
      },
    ],
  ])(
    "shows the provider section while %s without a Speakeasy issuer",
    (_name, arrange) => {
      state.flags = { emaProviders: false };
      state.speakeasy = { state: "unavailable" };
      arrange();
      render(<IdentityChainingSection mcpServer={server} />);
      expect(
        screen.queryByRole("heading", {
          name: "Enterprise Managed Authorization",
        }),
      ).toBeTruthy();
    },
  );

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

  it("says when the clients couldn't load, instead of asking for a client", () => {
    state.clientsError = true;
    render(<IdentityChainingSection mcpServer={server} />);
    expect(
      screen.getByText(
        "Couldn’t load Enterprise Managed Authorization for this server.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.clientsRefetch).toHaveBeenCalled();
  });

  it("scrolls to itself once it has loaded on a deep link", () => {
    state.hash = "#identity-chaining";
    state.clientsLoading = true;
    // jsdom has no scrollIntoView to spy on.
    if (!Object.hasOwn(Element.prototype, "scrollIntoView")) {
      Element.prototype.scrollIntoView = () => {};
    }
    const scroll = vi
      .spyOn(Element.prototype, "scrollIntoView")
      .mockImplementation(() => {});
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => {
      cb(0);
      return 1;
    });
    const { rerender } = render(<IdentityChainingSection mcpServer={server} />);
    expect(scroll).not.toHaveBeenCalled();
    state.clientsLoading = false;
    rerender(<IdentityChainingSection mcpServer={server} />);
    expect(scroll).toHaveBeenCalledTimes(1);
    const section = document.getElementById("identity-chaining");
    expect(section).not.toBeNull();
    expect(scroll.mock.contexts[0]).toBe(section);
    rerender(<IdentityChainingSection mcpServer={server} />);
    expect(scroll).toHaveBeenCalledTimes(1);
  });
});
