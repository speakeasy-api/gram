import type { ReactNode } from "react";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityChainingField } from "./IdentityChainingField";
import type { ChainingProviders } from "./identityChainingGate";

const pending = vi.hoisted(() => ({ prepare: false, unlink: false }));
const unlinked = { state: "unlinked", scopes: [] };
const read = vi.hoisted(() => ({ data: {} as Record<string, unknown> }));
// Reads of bindings on clients the metadata filter dropped, by issuer.
const staleRead = vi.hoisted(() => ({
  byIssuer: {} as Record<string, Record<string, unknown>>,
}));
const scopeChecks = vi.hoisted(() => [] as unknown[]);
const issuerNames = vi.hoisted(() => ({
  items: [] as { id: string; name: string; issuer?: string }[],
}));
const state = vi.hoisted(() => ({
  orgAdmin: true,
  calls: [] as string[],
  prepare: vi.fn(),
  unlink: vi.fn(),
  prepareResult: undefined as Record<string, unknown> | undefined,
  oktaIssuerId: undefined as string | undefined,
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: unknown[] }) =>
    queryKey[3] === "stale"
      ? {
          isFetching: false,
          data: (queryKey[6] as string[]).map((id) => staleRead.byIssuer[id]),
        }
      : { isFetching: false, data: read.data },
  useQueryClient: () => ({
    setQueryData: (
      _key: unknown,
      next: (prev: Record<string, unknown>) => Record<string, unknown>,
    ) => {
      read.data = next(read.data);
    },
    invalidateQueries: () => Promise.resolve(),
  }),
}));
vi.mock("@/pages/org/identity-provider/services/useEmaServices", () => ({
  useOktaIssuerId: () => state.oktaIssuerId,
}));
vi.mock(
  "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js",
  () => ({
    invalidateAllOrganizationRemoteSessionIssuerEMAServices: () =>
      Promise.resolve(),
  }),
);
vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  invalidateAllRemoteSessionClients: () => Promise.resolve(),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), warning: vi.fn() } }));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useProject: () => ({ id: "project-id" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope !== "org:admin" || state.orgAdmin,
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
vi.mock("react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock("@/lib/remote-identity", () => ({
  useRemoteSessionIssuersByIds: () => issuerNames,
}));
vi.mock("../RemoteMcpSessionsSection", () => ({
  MCP_SESSIONS_SECTION_ID: "sessions",
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    ...props
  }: {
    children: ReactNode;
    scope: string;
    resourceId: string;
  }) => {
    scopeChecks.push(props);
    return children;
  },
}));
vi.mock("@/components/setup-steps/StepBlocks", () => ({
  CopyableValue: ({ value }: { value?: string }) => <code>{value}</code>,
}));
const prmMock = vi.hoisted(() => ({
  value: {} as { metadata?: { authorizationServers?: string[] } },
}));
vi.mock("@/lib/remote-identity/queries/useProtectedResourceMetadata", () => ({
  useProtectedResourceMetadata: () => prmMock.value,
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/remoteSessionIssuer.js", () => ({
  useRemoteSessionIssuer: () => ({}),
}));
vi.mock("@gram/client/react-query/remoteSessionClientsPrepareEMA.js", () => ({
  useRemoteSessionClientsPrepareEMAMutation: (options: {
    onSuccess: (result: unknown) => void;
  }) => ({
    isPending: pending.prepare,
    mutate: (variables: unknown) => {
      state.calls.push("prepare");
      state.prepare(variables);
      if (state.prepareResult) options.onSuccess(state.prepareResult);
    },
  }),
}));
vi.mock("@gram/client/react-query/remoteSessionClientsUnlinkEMA.js", () => ({
  useRemoteSessionClientsUnlinkEMAMutation: () => ({
    isPending: pending.unlink,
    mutate: (variables: unknown) => {
      state.calls.push("unlink");
      state.unlink(variables);
    },
  }),
}));

const clients = ["first", "second"].map((id) => ({
  id,
  clientId: id,
  remoteSessionIssuerId: "issuer",
  grantTypes: ["urn:ietf:params:oauth:grant-type:jwt-bearer"],
})) as RemoteSessionClient[];

function field(
  providers: ChainingProviders = { external: true },
  userSessionIssuer = {
    id: "session-issuer",
    trustedRemoteSessionIssuerId: "idp",
    trustedRemoteSessionClientId: "idp-client",
  } as UserSessionIssuer,
) {
  return (
    <IdentityChainingField
      userSessionIssuer={userSessionIssuer}
      providers={providers}
      linkedClients={clients}
      resource="https://example.com/mcp"
    />
  );
}

const bound = (clientId: string) => ({
  state: "ready",
  bindingId: "binding",
  clientId,
  externalClientId: clientId,
  generation: 1,
  scopes: [],
});

beforeEach(() => {
  read.data = unlinked;
  staleRead.byIssuer = {};
  scopeChecks.length = 0;
  issuerNames.items = [];
  state.orgAdmin = true;
  state.calls.length = 0;
  state.prepare.mockReset();
  state.unlink.mockReset();
  state.prepareResult = undefined;
  state.oktaIssuerId = undefined;
  prmMock.value = {};
});

afterEach(() => {
  cleanup();
  pending.prepare = false;
  pending.unlink = false;
});

describe("identity chaining client selection", () => {
  it.each(["prepare", "unlink"] as const)(
    "keeps the target fixed while %s is pending",
    (operation) => {
      const { rerender } = render(field());
      expect((screen.getByRole("combobox") as HTMLButtonElement).disabled).toBe(
        false,
      );
      pending[operation] = true;
      rerender(field());
      expect((screen.getByRole("combobox") as HTMLButtonElement).disabled).toBe(
        true,
      );
      pending[operation] = false;
      rerender(field());
      expect((screen.getByRole("combobox") as HTMLButtonElement).disabled).toBe(
        false,
      );
    },
  );
});

describe("identity chaining client for this service", () => {
  it("only offers clients registered with this server's authorization server", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com/"] },
    };
    issuerNames.items = [
      { id: "tracker", name: "Tracker", issuer: "https://tracker.example.com" },
      { id: "wiki", name: "Wiki", issuer: "https://wiki.example.com" },
    ];
    render(
      <IdentityChainingField
        userSessionIssuer={{ id: "session-issuer" } as UserSessionIssuer}
        providers={{ external: false }}
        linkedClients={
          [
            {
              id: "wiki-client",
              clientId: "wiki-client",
              remoteSessionIssuerId: "wiki",
            },
            {
              id: "tracker-client",
              clientId: "tracker-client",
              remoteSessionIssuerId: "tracker",
            },
          ] as RemoteSessionClient[]
        }
        resource="https://example.com/mcp"
      />,
    );
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(
      screen.getByRole("link", { name: "tracker-client" }).getAttribute("href"),
    ).toBe("/rip/tracker/clients/tracker-client/overview");
    expect(screen.queryByText("wiki-client")).toBeNull();
  });

  it("matches the authorization server as the server does: case, default port and trailing slash aside", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://TRACKER.example.com:443/"] },
    };
    issuerNames.items = [
      { id: "tracker", name: "Tracker", issuer: "https://tracker.example.com" },
    ];
    render(
      <IdentityChainingField
        userSessionIssuer={{ id: "session-issuer" } as UserSessionIssuer}
        providers={{ external: false }}
        linkedClients={
          [
            {
              id: "tracker-client",
              clientId: "tracker-client",
              remoteSessionIssuerId: "tracker",
            },
          ] as RemoteSessionClient[]
        }
        resource="https://example.com/mcp"
      />,
    );
    expect(screen.getByText("tracker-client")).toBeTruthy();
  });

  it("keeps Turn off for a binding on a client the metadata no longer names", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com/"] },
    };
    issuerNames.items = [
      { id: "wiki", name: "Wiki", issuer: "https://wiki.example.com" },
    ];
    staleRead.byIssuer = { wiki: { ...bound("wiki-client"), generation: 4 } };
    render(
      <IdentityChainingField
        userSessionIssuer={{ id: "session-issuer" } as UserSessionIssuer}
        providers={{ external: false }}
        linkedClients={
          [
            {
              id: "wiki-client",
              clientId: "wiki-client",
              remoteSessionIssuerId: "wiki",
            },
          ] as RemoteSessionClient[]
        }
        resource="https://example.com/mcp"
      />,
    );
    expect(
      screen.getByText(/still on through client wiki-client/),
    ).toBeTruthy();
    expect(
      screen.getByText(
        /Link a client registered with this server’s authorization server/,
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Turn off" }));
    expect(state.unlink).toHaveBeenCalledWith({
      request: {
        gramProject: "project",
        unlinkEMARequestBody: {
          userSessionIssuerId: "session-issuer",
          remoteSessionIssuerId: "wiki",
          resource: "https://example.com/mcp",
          expectedGeneration: 4,
        },
      },
    });
  });

  it("says nothing about the metadata filter when no dropped client is bound", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com/"] },
    };
    issuerNames.items = [
      { id: "wiki", name: "Wiki", issuer: "https://wiki.example.com" },
    ];
    staleRead.byIssuer = { wiki: unlinked };
    render(
      <IdentityChainingField
        userSessionIssuer={{ id: "session-issuer" } as UserSessionIssuer}
        providers={{ external: false }}
        linkedClients={
          [
            {
              id: "wiki-client",
              clientId: "wiki-client",
              remoteSessionIssuerId: "wiki",
            },
          ] as RemoteSessionClient[]
        }
        resource="https://example.com/mcp"
      />,
    );
    expect(screen.queryByText(/still on through client/)).toBeNull();
  });
});

describe("identity chaining bound client", () => {
  it("selects the client the binding uses", () => {
    read.data = bound("second");
    render(field());
    expect(screen.getByRole("combobox").textContent).toBe("second");
    expect(screen.queryByText("Use this client instead")).toBeNull();
  });

  it("says whether chaining is on, next to the action that changes it", () => {
    read.data = bound("second");
    const { rerender } = render(field());
    expect(screen.getByText("On, not proven yet")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Turn off" })).toBeTruthy();
    read.data = unlinked;
    rerender(field());
    expect(screen.getByText("Off")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Turn on" })).toBeTruthy();
  });

  it("keeps the badge for states the buttons don't explain", () => {
    read.data = { ...bound("second"), state: "transient_failure" };
    render(field());
    expect(screen.getByText("Temporary failure")).toBeTruthy();
  });

  it("makes replacing the bound client explicit", () => {
    read.data = bound("elsewhere");
    render(field());
    expect(screen.getByText("Use this client instead")).toBeTruthy();
  });

  it("requires the scope the API enforces", () => {
    render(field());
    expect(scopeChecks).toContainEqual(
      expect.objectContaining({
        scope: "project:write",
        resourceId: "project-id",
      }),
    );
  });
});

describe("identity chaining vouched for by Speakeasy", () => {
  const speakeasyOnly = { external: false };
  const plainIssuer = {
    id: "session-issuer",
    slug: "speakeasy-sign-in",
  } as UserSessionIssuer;
  const okta = { kind: "external", remoteSessionIssuerId: "okta" };
  const speakeasy = { kind: "speakeasy", internalSessionIssuerId: "internal" };
  const providersOf = (...providers: Record<string, unknown>[]) => ({
    isDefault: true,
    vendorIssuer: "https://vendor.example.com",
    providers,
  });
  const trusting = {
    id: "session-issuer",
    slug: "okta-sign-in",
    trustedRemoteSessionIssuerId: "okta",
    trustedRemoteSessionClientId: "second",
  } as UserSessionIssuer;

  function speakeasyField(
    overrides: Partial<Parameters<typeof IdentityChainingField>[0]> = {},
  ) {
    return (
      <IdentityChainingField
        userSessionIssuer={plainIssuer}
        providers={speakeasyOnly}
        speakeasyIssuerUrl="https://speakeasy.example.com/issuer"
        linkedClients={clients}
        resource="https://example.com/mcp"
        {...overrides}
      />
    );
  }
  const enableButton = () =>
    screen.getByRole("button", {
      name: "Turn on",
    }) as HTMLButtonElement;
  const chain = () => screen.getByRole("list", { name: "How people get in" });
  const vouchers = () =>
    within(screen.getByRole("list", { name: "Identity providers" }))
      .getAllByRole("listitem")
      .map((item) => item.textContent);
  const trusts = () =>
    within(screen.getByRole("list", { name: "example.com trusts" }))
      .getAllByRole("listitem")
      .map((item) => ({
        name: within(item).getAllByText(/./)[0]?.textContent,
        accepted: item.textContent?.match(
          /Last worked [^I]*|Not accepted yet/,
        )?.[0],
        url: item.querySelector("code")?.textContent,
      }));

  beforeEach(() => {
    issuerNames.items = [
      { id: "okta", name: "Okta", issuer: "https://okta.example.com" },
    ];
  });

  it("offers every linked client when no sign-in client is trusted", () => {
    render(field(speakeasyOnly, plainIssuer));
    expect(screen.getByRole("combobox")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Manage client" }).getAttribute("href"),
    ).toBe("/rip/issuer/clients/first/overview");
  });

  it("keeps the identity provider's sign-in client out of the picker", () => {
    read.data = { ...unlinked, emaProviders: providersOf(okta) };
    render(field({ external: true }, trusting));
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(within(chain()).getByRole("link", { name: "first" })).toBeTruthy();
  });

  it("keeps the trusted sign-in client out even when only Speakeasy applies", () => {
    render(field(speakeasyOnly, trusting));
    expect(screen.queryByRole("combobox")).toBeNull();
  });

  it("asks for an authorization server client, not an identity provider one", () => {
    render(speakeasyField({ linkedClients: [] }));
    expect(
      screen.getByText(/registered with this server’s authorization server/),
    ).toBeTruthy();
  });

  it("only prepares the binding; who vouches is derived, never written", () => {
    read.data = {
      ...unlinked,
      generation: 0,
      emaProviders: providersOf(speakeasy),
    };
    render(speakeasyField());
    fireEvent.click(enableButton());
    expect(state.calls).toEqual(["prepare"]);
  });

  // On a federated sign-in only that identity provider can vouch; Speakeasy
  // comes back as not trusted by the endpoint.
  const speakeasyUnused = {
    ...speakeasy,
    unavailableReason: "not_trusted_by_endpoint",
  };

  it("names the accepted providers as plain text for people who can't open them", () => {
    state.orgAdmin = false;
    read.data = {
      ...bound("first"),
      emaProviders: { ...providersOf(okta, speakeasyUnused), isDefault: false },
    };
    render(speakeasyField({ userSessionIssuer: trusting }));
    expect(
      screen.getByText(/Set by the identity providers example\.com accepts\./),
    ).toBeTruthy();
    expect(
      screen.queryByRole("link", {
        name: "the identity providers example.com accepts",
      }),
    ).toBeNull();
  });

  it("walks the chain: sign-in, who vouches, what the vendor trusts, client", () => {
    read.data = {
      ...bound("first"),
      emaProviders: { ...providersOf(okta, speakeasyUnused), isDefault: false },
    };
    render(speakeasyField({ userSessionIssuer: trusting }));
    expect(
      screen.getByText(
        (_, el) =>
          el?.tagName === "P" &&
          el.textContent ===
            "Set by the identity providers example.com accepts.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", {
          name: "the identity providers example.com accepts",
        })
        .getAttribute("href"),
    ).toBe(
      "/org/identity?tab=identity-providers&service=https%3A%2F%2Fvendor.example.com#accepted-providers",
    );
    const links = within(chain()).getAllByText(
      /^(Sign-in|Vouched for by|example.com trusts|Remote session client)$/,
    );
    expect(links.map((link) => link.textContent)).toEqual([
      "Sign-in",
      "Vouched for by",
      "example.com trusts",
      "Remote session client",
    ]);
    expect(
      within(chain()).getByRole("link", { name: "first" }).getAttribute("href"),
    ).toBe("/rip/issuer/clients/first/overview");
    expect(
      within(chain()).getByText("Okta", { selector: "[title='okta-sign-in']" }),
    ).toBeTruthy();
    expect(screen.queryByText(/okta-sign-in/)).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Change in Sessions" })
        .getAttribute("href"),
    ).toBe("#sessions");
    expect(vouchers()).toEqual(["Oktafor people who signed in with Okta"]);
    expect(trusts()).toEqual([
      {
        name: "Okta",
        accepted: "Not accepted yet",
        url: undefined,
      },
    ]);
    expect(screen.queryByText(/Add this issuer URL/)).toBeNull();
    expect(
      screen
        .getByRole("link", {
          name: "Set up example.com in Okta’s Cross App Access",
        })
        .getAttribute("href"),
    ).toBe(
      "/org/identity?tab=identity-providers&provider=okta&view=cross-app-access",
    );
    expect(
      screen
        .getByRole("link", { name: "Every server that reaches example.com" })
        .getAttribute("href"),
    ).toBe(
      "/org/identity?tab=identity-providers&service=https%3A%2F%2Fvendor.example.com#servers",
    );
  });

  it("folds the issuer URL once the vendor trusts a provider, but says Working only for this server", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    const { rerender } = render(speakeasyField());
    expect(screen.getByText("On, not proven yet")).toBeTruthy();
    expect(trusts()).toEqual([
      {
        name: "Speakeasy",
        accepted: "Not accepted yet",
        url: "https://speakeasy.example.com/issuer",
      },
    ]);
    // Accepted elsewhere in the organization: the paste step is done.
    read.data = {
      ...bound("first"),
      emaProviders: providersOf({
        ...speakeasy,
        lastAcceptedAt: new Date(Date.now() - 2 * 60 * 60 * 1000),
      }),
    };
    rerender(speakeasyField());
    expect(screen.getByText("On, not proven yet")).toBeTruthy();
    expect(screen.getByText(/already trusts Speakeasy/)).toBeTruthy();
    expect(trusts()).toEqual([
      { name: "Speakeasy", accepted: "Last worked 2h ago", url: undefined },
    ]);
    expect(screen.queryByText(/admin settings/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Issuer URL" }));
    expect(trusts()[0]?.url).toBe("https://speakeasy.example.com/issuer");
    // Accepted for this server's own binding.
    read.data = {
      ...(read.data as Record<string, unknown>),
      lastAcceptedAt: new Date(Date.now() - 30 * 60 * 1000),
    };
    rerender(speakeasyField());
    expect(screen.getByText("Working")).toBeTruthy();
    expect(
      screen.getByText("example.com accepted a sign-in here 30m ago"),
    ).toBeTruthy();
  });

  it("keeps the chain after Turn on succeeds", () => {
    read.data = {
      ...unlinked,
      generation: 0,
      emaProviders: providersOf(speakeasy),
    };
    state.prepareResult = {
      state: "ready",
      bindingId: "binding",
      clientId: "first",
      generation: 1,
      scopes: [],
    };
    const { rerender } = render(speakeasyField());
    fireEvent.click(enableButton());
    rerender(speakeasyField());
    expect(screen.getByText("On, not proven yet")).toBeTruthy();
    expect(vouchers()).toEqual([
      "Speakeasyfor everyone who signs in to this server",
    ]);
    expect(trusts().map((row) => row.name)).toEqual(["Speakeasy"]);
  });

  it("explains a blocked state in plain words, with the server's detail tucked away", () => {
    read.data = {
      ...bound("first"),
      state: "unknown_grants",
      remediation: "record grant_types on remote_session_client",
      emaProviders: providersOf(speakeasy),
    };
    render(speakeasyField());
    expect(screen.getByText("Couldn’t check the client")).toBeTruthy();
    expect(screen.queryByText("Grants unknown")).toBeNull();
    expect(screen.queryByText(/remote_session_client/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Technical details" }));
    expect(screen.getByText(/remote_session_client/)).toBeTruthy();
  });

  it("only shows organization admins the link to every server", () => {
    state.orgAdmin = false;
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    render(speakeasyField());
    expect(
      screen.queryByRole("link", {
        name: "Every server that reaches example.com",
      }),
    ).toBeNull();
  });

  it.each([
    [
      "okta",
      "Fix in Okta",
      "/org/identity?tab=identity-providers&provider=okta&view=setup",
    ],
    [undefined, "Identity providers", "/org/identity?tab=identity-providers"],
  ])(
    "links a broken identity provider to where it's fixed (Okta connection %s)",
    (oktaIssuerId, label, href) => {
      state.oktaIssuerId = oktaIssuerId;
      read.data = {
        ...bound("first"),
        emaProviders: providersOf({ ...okta, unavailableReason: "deleted" }),
      };
      render(speakeasyField({ userSessionIssuer: trusting }));
      expect(
        screen.getByRole("link", { name: label }).getAttribute("href"),
      ).toBe(href);
    },
  );

  it("names Speakeasy's own sign-in when no identity provider is trusted", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    render(speakeasyField());
    expect(
      screen.getByText(
        "People sign in to this server with Speakeasy, so Speakeasy vouches for them.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("Speakeasy’s own sign-in").getAttribute("title"),
    ).toBe("speakeasy-sign-in");
  });

  it("leaves out an identity provider this server's sign-in doesn't use", () => {
    read.data = {
      ...bound("first"),
      emaProviders: providersOf(
        { ...okta, unavailableReason: "not_trusted_by_endpoint" },
        speakeasy,
      ),
    };
    render(speakeasyField());
    expect(vouchers()).toEqual([
      "Speakeasyfor everyone who signs in to this server",
    ]);
    expect(screen.queryByText(/Okta/)).toBeNull();
    expect(trusts().map((row) => row.name)).toEqual(["Speakeasy"]);
  });

  it.each([
    ["setup_required", "Not set up"],
    ["disabled", "Not set up"],
    ["signing_paused", "Can’t sign right now"],
    ["keyless", "Can’t sign right now"],
    [
      "sign_in_enforcement_required",
      "Needs sign-in enforcement for the organization",
    ],
  ])("explains why Speakeasy can't vouch when %s", (reason, text) => {
    read.data = {
      ...unlinked,
      generation: 0,
      emaProviders: providersOf({ ...speakeasy, unavailableReason: reason }),
    };
    render(speakeasyField());
    expect(screen.getByText(text)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Fix in Speakeasy" })
        .getAttribute("href"),
    ).toBe("/org/identity?tab=identity-providers&provider=speakeasy");
    expect(
      screen.queryByRole("list", { name: "example.com trusts" }),
    ).toBeNull();
    expect(screen.getByText(/Nothing to set up at example.com/)).toBeTruthy();
  });

  it("blocks Turn on while nobody can vouch", () => {
    read.data = {
      ...unlinked,
      generation: 0,
      emaProviders: providersOf({ ...speakeasy, unavailableReason: "keyless" }),
    };
    render(speakeasyField());
    expect(enableButton().disabled).toBe(true);
    expect(
      screen.getByText(
        "No identity provider can vouch for people on this server.",
      ),
    ).toBeTruthy();
    fireEvent.click(enableButton());
    expect(state.calls).toEqual([]);
  });

  it("shows Speakeasy greyed when the service doesn't accept a federated server's provider", () => {
    const savedSpeakeasy = {
      isDefault: false,
      vendorIssuer: "https://vendor.example.com",
      providers: [
        { ...speakeasy, unavailableReason: "not_trusted_by_endpoint" },
      ],
    };
    read.data = { ...unlinked, generation: 0, emaProviders: savedSpeakeasy };
    render(speakeasyField({ userSessionIssuer: trusting }));
    expect(vouchers()).toEqual([
      "SpeakeasyCan’t vouch for people who sign in through Okta",
    ]);
    expect(screen.queryByRole("link", { name: "Fix in Speakeasy" })).toBeNull();
    expect(enableButton().disabled).toBe(true);
    expect(
      screen.getByText(
        "No identity provider can vouch for people on this server.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", {
          name: "change which identity providers example.com accepts",
        })
        .getAttribute("href"),
    ).toBe(
      "/org/identity?tab=identity-providers&service=https%3A%2F%2Fvendor.example.com#accepted-providers",
    );
    expect(
      screen
        .getByRole("link", { name: "change the sign-in in Sessions" })
        .getAttribute("href"),
    ).toBe("#sessions");
    fireEvent.click(enableButton());
    expect(state.calls).toEqual([]);
  });

  it("asks someone who isn't an org admin to have the accepted providers changed", () => {
    state.orgAdmin = false;
    read.data = {
      ...unlinked,
      generation: 0,
      emaProviders: {
        isDefault: false,
        vendorIssuer: "https://vendor.example.com",
        providers: [
          { ...speakeasy, unavailableReason: "not_trusted_by_endpoint" },
        ],
      },
    };
    render(speakeasyField({ userSessionIssuer: trusting }));
    expect(
      screen.getByText(
        /ask an organization admin to change which identity providers example\.com accepts/,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByRole("link", {
        name: "change which identity providers example.com accepts",
      }),
    ).toBeNull();
  });

  it("says an on server nobody can vouch for isn't running", () => {
    read.data = {
      ...bound("first"),
      emaProviders: {
        isDefault: false,
        vendorIssuer: "https://vendor.example.com",
        providers: [
          { ...speakeasy, unavailableReason: "not_trusted_by_endpoint" },
        ],
      },
    };
    render(speakeasyField({ userSessionIssuer: trusting }));
    expect(screen.getByText("On, but not running")).toBeTruthy();
    expect(
      screen.getByText(
        "No identity provider can vouch for people on this server.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", {
        name: "change which identity providers example.com accepts",
      }),
    ).toBeTruthy();
  });

  it("hides a provider the sign-in doesn't go through while another vouches", () => {
    read.data = {
      ...bound("first"),
      emaProviders: {
        isDefault: false,
        vendorIssuer: "https://vendor.example.com",
        providers: [
          okta,
          { ...speakeasy, unavailableReason: "not_trusted_by_endpoint" },
        ],
      },
    };
    render(speakeasyField({ userSessionIssuer: trusting }));
    expect(vouchers()).toEqual(["Oktafor people who signed in with Okta"]);
    expect(
      screen.queryByRole("link", {
        name: "change which identity providers example.com accepts",
      }),
    ).toBeNull();
  });

  it("says when chaining is on but nobody can vouch right now", () => {
    read.data = {
      ...bound("first"),
      emaProviders: providersOf({
        ...speakeasy,
        unavailableReason: "signing_paused",
      }),
    };
    render(speakeasyField());
    expect(screen.getByText("On, but not running")).toBeTruthy();
  });

  it("points at the organization's identity settings when nobody is listed", () => {
    read.data = { ...unlinked, generation: 0, emaProviders: providersOf() };
    render(speakeasyField());
    expect(
      screen
        .getByRole("link", {
          name: "Set up Speakeasy as an identity provider",
        })
        .getAttribute("href"),
    ).toBe("/org/identity?tab=identity-providers");
  });

  it("shows what Turn on will do before it's on", () => {
    read.data = {
      ...unlinked,
      generation: 0,
      emaProviders: providersOf(speakeasy),
    };
    render(speakeasyField());
    expect(screen.getByText("Off")).toBeTruthy();
    expect(vouchers()).toEqual([
      "Speakeasyfor everyone who signs in to this server",
    ]);
    expect(
      screen.getByText("https://speakeasy.example.com/issuer"),
    ).toBeTruthy();
  });

  it("disables without touching who vouches", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    render(speakeasyField());
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    fireEvent.click(
      screen.getAllByRole("button", { name: "Turn off" }).at(-1)!,
    );
    expect(state.calls).toEqual(["unlink"]);
  });

  it("has no chaining scope list of its own", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    render(speakeasyField());
    expect(screen.queryByText("Requested scopes")).toBeNull();
    expect(screen.queryByText("Save scopes")).toBeNull();
  });
});
