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
const read = vi.hoisted(() => ({
  data: {} as Record<string, unknown>,
  error: null as unknown,
}));
// Reads of bindings on clients the metadata filter dropped, by issuer.
const staleRead = vi.hoisted(() => ({
  byIssuer: {} as Record<string, Record<string, unknown>>,
}));
const scopeChecks = vi.hoisted(() => [] as unknown[]);
const issuerNames = vi.hoisted(() => ({
  items: [] as { id: string; name: string; issuer?: string }[],
  isLoading: false,
  isError: false,
}));
const state = vi.hoisted(() => ({
  orgAdmin: true,
  orgRead: true,
  projectWrite: true,
  oktaView: true,
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
      : { isFetching: false, data: read.data, error: read.error },
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
    hasScope: (scope: string) =>
      scope === "org:admin"
        ? state.orgAdmin
        : scope === "org:read"
          ? state.orgRead
          : scope === "project:write"
            ? state.projectWrite
            : true,
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({
    status: state.oktaView ? "enabled" : "disabled",
  }),
}));
vi.mock("@/lib/featureFlags", () => ({
  FEATURE_FLAGS: { oktaConnections: "okta-connections" },
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
  value: {} as {
    status?: string;
    metadata?: { authorizationServers?: string[] };
  },
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
  read.error = null;
  staleRead.byIssuer = {};
  scopeChecks.length = 0;
  issuerNames.items = [];
  issuerNames.isLoading = false;
  issuerNames.isError = false;
  state.orgAdmin = true;
  state.orgRead = true;
  state.projectWrite = true;
  state.oktaView = true;
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

function trackerAndWiki() {
  return (
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
    />
  );
}

describe("identity chaining client for this service", () => {
  it("only offers clients registered with this server's authorization server", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com"] },
    };
    issuerNames.items = [
      { id: "tracker", name: "Tracker", issuer: "https://tracker.example.com" },
      { id: "wiki", name: "Wiki", issuer: "https://wiki.example.com" },
    ];
    render(trackerAndWiki());
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(
      screen.getByRole("link", { name: "tracker-client" }).getAttribute("href"),
    ).toBe("/rip/tracker/clients/tracker-client/overview");
    expect(screen.queryByText("wiki-client")).toBeNull();
  });

  it("matches the authorization server exactly, as Turn on does", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com/"] },
    };
    issuerNames.items = [
      { id: "tracker", name: "Tracker", issuer: "https://tracker.example.com" },
    ];
    render(trackerAndWiki());
    expect(screen.queryByText("tracker-client")).toBeNull();
    expect(
      screen.getByText(
        /Link a client registered with this server’s authorization server/,
      ),
    ).toBeTruthy();
  });

  it.each([
    [
      "the metadata is loading",
      () => {
        prmMock.value = { status: "loading" };
      },
    ],
    [
      "client issuers are loading",
      () => {
        issuerNames.isLoading = true;
      },
    ],
  ])("offers no client while %s", (_name, arrange) => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com"] },
    };
    arrange();
    render(trackerAndWiki());
    expect(screen.queryByText("tracker-client")).toBeNull();
    expect(screen.queryByText("wiki-client")).toBeNull();
    expect(screen.queryByText(/Link a client/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
  });

  it("leaves out a client whose authorization server couldn't be read", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com"] },
    };
    issuerNames.items = [
      { id: "tracker", name: "Tracker", issuer: "https://tracker.example.com" },
    ];
    render(trackerAndWiki());
    expect(screen.getByText("tracker-client")).toBeTruthy();
    expect(screen.queryByText("wiki-client")).toBeNull();
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
    expect(screen.getByText(/still on for client wiki-client/)).toBeTruthy();
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
    expect(screen.queryByText(/still on for client/)).toBeNull();
  });

  it("still offers Turn off, without calling the client old, when its authorization server couldn't be looked up", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com/"] },
    };
    issuerNames.isError = true;
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
    expect(screen.getByText(/still on for client wiki-client/)).toBeTruthy();
    expect(
      screen.getByText(/couldn’t check that client’s authorization server/),
    ).toBeTruthy();
    expect(screen.queryByText(/no longer uses/)).toBeNull();
    expect(screen.getByRole("button", { name: "Turn off" })).toBeTruthy();
    expect(
      screen.getByText(/couldn’t check their authorization server/),
    ).toBeTruthy();
  });

  it("skips the lookup notice when the metadata names no authorization server", () => {
    prmMock.value = { metadata: { authorizationServers: [] } };
    issuerNames.isError = true;
    render(field());
    expect(
      screen.queryByText(/couldn’t check their authorization server/),
    ).toBeNull();
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
    expect(screen.getByText("On, waiting for a first connection")).toBeTruthy();
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
  it("keeps an unknown provider visible and blocks turning on without claiming it can vouch", () => {
    read.data = {
      state: "unlinked",
      generation: 1,
      scopes: [],
      emaProviders: providersOf({ kind: "future_provider" }),
    };
    render(speakeasyField());
    expect(enableButton().disabled).toBe(true);
    fireEvent.click(enableButton());
    expect(screen.getByText("Unknown identity provider")).toBeTruthy();
    expect(screen.getByText(/Unknown availability/)).toBeTruthy();
    expect(
      screen.queryByText(
        "No identity provider can vouch for people on this server.",
      ),
    ).toBeNull();
  });

  it("explains an unknown preparation state without a success or failure label", () => {
    read.data = {
      ...bound("first"),
      state: "future_state",
      emaProviders: providersOf(speakeasy),
    };
    render(speakeasyField());
    expect(screen.getByText("Unknown")).toBeTruthy();
    expect(
      screen.getByText(
        /Refresh the page to check for an update before changing this setup/,
      ),
    ).toBeTruthy();
    expect(enableButton().disabled).toBe(true);
    const disable = screen.getByRole("button", {
      name: "Turn off",
    }) as HTMLButtonElement;
    expect(disable.disabled).toBe(true);
    fireEvent.click(enableButton());
    fireEvent.click(disable);
    expect(state.prepare).not.toHaveBeenCalled();
    expect(state.unlink).not.toHaveBeenCalled();
  });
  it("blocks an unknown state with no binding from turning on", () => {
    read.data = {
      ...unlinked,
      state: "future_state",
      emaProviders: providersOf(speakeasy),
    };
    render(speakeasyField());
    expect(enableButton().disabled).toBe(true);
    fireEvent.click(enableButton());
    expect(state.prepare).not.toHaveBeenCalled();
  });

  it("keeps an unknown provider visible alongside a usable provider despite a known unused reason", () => {
    read.data = {
      ...bound("first"),
      emaProviders: providersOf(speakeasy, {
        kind: "future_provider",
        unavailableReason: "not_trusted_by_endpoint",
      }),
    };
    render(speakeasyField());
    expect(screen.getByText("Unknown identity provider")).toBeTruthy();
    expect(screen.getByText(/Unknown availability/)).toBeTruthy();
  });

  it("blocks a pending turn-off confirmation when a refetch reports an unknown state", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    const rendered = render(speakeasyField());
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    read.data = { ...read.data, state: "future_state" };
    rendered.rerender(speakeasyField());
    const confirmation = within(screen.getByRole("dialog")).getByRole(
      "button",
      { name: "Turn off" },
    ) as HTMLButtonElement;
    expect(confirmation.disabled).toBe(true);
    fireEvent.click(confirmation);
    expect(state.unlink).not.toHaveBeenCalled();
  });
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
          /Last worked [^I]*|Hasn’t worked yet/,
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
    expect(state.prepare).toHaveBeenCalledWith({
      request: {
        gramProject: "project",
        prepareEMARequestBody: {
          userSessionIssuerId: "session-issuer",
          remoteSessionIssuerId: "issuer",
          resource: "https://example.com/mcp",
          resourceMetadata: undefined,
          clientId: "first",
          mechanism: "manual",
          confirmGrants: ["urn:ietf:params:oauth:grant-type:jwt-bearer"],
          scopes: [],
          expectedGeneration: 0,
        },
      },
    });
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
      screen.getByText(
        /Set in the identity providers enabled for example\.com\./,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByRole("link", {
        name: "the identity providers enabled for example.com",
      }),
    ).toBeNull();
  });

  it("walks the chain: sign-in, who vouches, what the vendor trusts, client", () => {
    state.oktaIssuerId = "okta";
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
            "Set in the identity providers enabled for example.com.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", {
          name: "the identity providers enabled for example.com",
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
    expect(vouchers()).toEqual(["Oktafor people who log in with Okta"]);
    expect(trusts()).toEqual([
      {
        name: "Okta",
        accepted: "Hasn’t worked yet",
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
        .getByRole("link", {
          name: "Every server that connects to example.com",
        })
        .getAttribute("href"),
    ).toBe(
      "/org/identity?tab=identity-providers&service=https%3A%2F%2Fvendor.example.com#servers",
    );
  });

  it("folds the issuer URL once the vendor trusts a provider, but says Working only for this server", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    const { rerender } = render(speakeasyField());
    expect(screen.getByText("On, waiting for a first connection")).toBeTruthy();
    expect(trusts()).toEqual([
      {
        name: "Speakeasy",
        accepted: "Hasn’t worked yet",
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
    expect(screen.getByText("On, waiting for a first connection")).toBeTruthy();
    expect(
      screen.getByText(/no one has connected through this server yet/),
    ).toBeTruthy();
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
    expect(screen.getByText("Last worked 30m ago")).toBeTruthy();
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
    expect(screen.getByText("On, waiting for a first connection")).toBeTruthy();
    expect(vouchers()).toEqual([
      "Speakeasyfor everyone who logs in to this server",
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
        name: "Every server that connects to example.com",
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
        "People log in to this server with Speakeasy, so Speakeasy vouches for them.",
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
      "Speakeasyfor everyone who logs in to this server",
    ]);
    expect(screen.queryByText(/Okta/)).toBeNull();
    expect(trusts().map((row) => row.name)).toEqual(["Speakeasy"]);
  });

  it.each([
    ["setup_required", "Not set up"],
    ["disabled", "Not set up"],
    ["signing_paused", "Not working right now"],
    ["keyless", "Not working right now"],
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
      "SpeakeasyCan’t vouch for people who log in with Okta",
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
          name: "change the identity providers enabled for example.com",
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
        /ask an organization admin to change the identity providers enabled for example\.com/,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByRole("link", {
        name: "change the identity providers enabled for example.com",
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
        name: "change the identity providers enabled for example.com",
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
    expect(vouchers()).toEqual(["Oktafor people who log in with Okta"]);
    expect(
      screen.queryByRole("link", {
        name: "change the identity providers enabled for example.com",
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
      "Speakeasyfor everyone who logs in to this server",
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
    expect(state.unlink).toHaveBeenCalledWith({
      request: {
        gramProject: "project",
        unlinkEMARequestBody: {
          userSessionIssuerId: "session-issuer",
          remoteSessionIssuerId: "issuer",
          resource: "https://example.com/mcp",
          expectedGeneration: 1,
        },
      },
    });
  });

  it("has no chaining scope list of its own", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    render(speakeasyField());
    expect(screen.queryByText("Requested scopes")).toBeNull();
    expect(screen.queryByText("Save scopes")).toBeNull();
  });

  it("links Cross App Access to Okta only for the Okta connection", () => {
    state.oktaIssuerId = "another-okta";
    read.data = {
      ...bound("first"),
      emaProviders: { ...providersOf(okta, speakeasyUnused), isDefault: false },
    };
    render(speakeasyField({ userSessionIssuer: trusting }));
    expect(
      screen.queryByRole("link", {
        name: "Set up example.com in Okta’s Cross App Access",
      }),
    ).toBeNull();
    expect(
      screen.getByText("Set up example.com in Okta’s Cross App Access"),
    ).toBeTruthy();
  });

  it("doesn't link Cross App Access when the Okta view isn't available", () => {
    state.oktaIssuerId = "okta";
    state.oktaView = false;
    read.data = {
      ...bound("first"),
      emaProviders: { ...providersOf(okta, speakeasyUnused), isDefault: false },
    };
    render(speakeasyField({ userSessionIssuer: trusting }));
    expect(screen.queryByRole("link", { name: /Cross App Access/ })).toBeNull();
  });

  it("says nothing about acceptance when it's withheld", () => {
    state.orgAdmin = false;
    state.orgRead = false;
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    render(speakeasyField());
    expect(trusts()).toEqual([
      {
        name: "Speakeasy",
        accepted: undefined,
        url: "https://speakeasy.example.com/issuer",
      },
    ]);
  });

  it("names an admin instead of linking fixes for people who can't open them", () => {
    state.orgAdmin = false;
    read.data = {
      ...unlinked,
      generation: 0,
      emaProviders: providersOf({ ...speakeasy, unavailableReason: "keyless" }),
    };
    render(speakeasyField());
    expect(screen.queryByRole("link", { name: "Fix in Speakeasy" })).toBeNull();
    expect(
      screen.getByText("An organization admin can fix this."),
    ).toBeTruthy();
  });

  it("names an admin instead of linking setup when nobody is listed", () => {
    state.orgAdmin = false;
    read.data = { ...unlinked, generation: 0, emaProviders: providersOf() };
    render(speakeasyField());
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

  it("renders nothing while off when only an on binding should show", () => {
    read.data = { ...unlinked, generation: 0 };
    const { container } = render(
      speakeasyField({
        onlyWhenOn: true,
        frame: (content) => <section>{content}</section>,
      }),
    );
    expect(container.querySelector("section")).toBeNull();
    expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
  });

  it("offers only Turn off for a stale binding when only an on binding should show", () => {
    prmMock.value = {
      metadata: { authorizationServers: ["https://tracker.example.com"] },
    };
    issuerNames.items = [
      { id: "tracker", name: "Tracker", issuer: "https://tracker.example.com" },
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
            {
              id: "tracker-client",
              clientId: "tracker-client",
              remoteSessionIssuerId: "tracker",
            },
          ] as RemoteSessionClient[]
        }
        resource="https://example.com/mcp"
        onlyWhenOn
      />,
    );
    expect(screen.getByText(/still on for client wiki-client/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Turn off" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
  });

  it.each([false, true])(
    "blocks an unknown stale binding (confirmation already open: %s)",
    (confirmFirst) => {
      prmMock.value = {
        metadata: { authorizationServers: ["https://tracker.example.com"] },
      };
      issuerNames.items = [
        {
          id: "tracker",
          name: "Tracker",
          issuer: "https://tracker.example.com",
        },
        { id: "wiki", name: "Wiki", issuer: "https://wiki.example.com" },
      ];
      staleRead.byIssuer = {
        wiki: {
          ...bound("wiki-client"),
          state: confirmFirst ? "ready" : "future_state",
          generation: 4,
        },
      };
      const element = () => (
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
          onlyWhenOn
        />
      );
      const rendered = render(element());
      if (confirmFirst) {
        fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
        staleRead.byIssuer.wiki = {
          ...staleRead.byIssuer.wiki,
          state: "future_state",
        };
        rendered.rerender(element());
      }
      expect(
        screen.getByText(/unknown state for client wiki-client/),
      ).toBeTruthy();
      for (const button of screen.getAllByRole("button", {
        name: "Turn off",
      })) {
        expect((button as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(button);
      }
      expect(state.unlink).not.toHaveBeenCalled();
    },
  );

  it("shows a failed read when only an on binding should show", () => {
    read.data = undefined as unknown as Record<string, unknown>;
    read.error = new Error("binding lookup failed");
    const { container } = render(
      speakeasyField({
        onlyWhenOn: true,
        frame: (content) => <section>{content}</section>,
      }),
    );
    expect(container.querySelector("section")).toBeTruthy();
    expect(screen.getByText(/binding lookup failed/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
  });

  it("keeps Turn off for an on binding when only an on binding should show", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    const { container } = render(
      speakeasyField({
        onlyWhenOn: true,
        frame: (content) => <section>{content}</section>,
      }),
    );
    expect(container.querySelector("section")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Turn off" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
  });

  it("still lists Speakeasy as trusted when its issuer URL can't be read", () => {
    read.data = { ...bound("first"), emaProviders: providersOf(speakeasy) };
    render(
      speakeasyField({
        onlyWhenOn: true,
        speakeasyIssuerUrl: undefined,
      }),
    );
    const trusts = screen.getByRole("list", { name: /trusts$/ });
    expect(within(trusts).getByText("Speakeasy")).toBeTruthy();
    expect(
      within(trusts).getByText(
        /An organization admin adds Speakeasy’s issuer URL/,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText(/until an identity provider can vouch/),
    ).toBeNull();
  });
});

describe("identity chaining connection scopes", () => {
  const outOfDate = {
    ...bound("first"),
    generation: 4,
    scopes: ["read"],
    connectionScopes: ["read", "write"],
    scopesOutOfDate: true,
  };
  const applyButton = () =>
    screen.queryByRole("button", { name: "Apply connection scopes" });

  it("says nothing while the recorded scopes match the connection", () => {
    read.data = { ...outOfDate, scopesOutOfDate: false };
    render(field());
    expect(screen.queryByText(/connection now asks for/)).toBeNull();
    expect(applyButton()).toBeNull();
  });

  it("names what changed and offers one action to apply it", () => {
    read.data = outOfDate;
    render(field());
    expect(
      screen.getByText(
        "This server’s connection now asks for read, write. Enterprise Managed Authorization still asks for read.",
      ),
    ).toBeTruthy();
    expect(applyButton()).toBeTruthy();
  });

  it("re-prepares the binding at its generation, without a client, grants or mechanism", () => {
    read.data = outOfDate;
    state.prepareResult = {
      ...outOfDate,
      generation: 5,
      scopes: ["read", "write"],
      scopesOutOfDate: false,
    };
    render(field());
    fireEvent.click(applyButton()!);
    expect(state.prepare).toHaveBeenCalledWith({
      request: {
        gramProject: "project",
        prepareEMARequestBody: {
          userSessionIssuerId: "session-issuer",
          remoteSessionIssuerId: "issuer",
          resource: "https://example.com/mcp",
          expectedGeneration: 4,
          scopes: ["read"],
        },
      },
    });
    expect(
      screen.queryByText("Someone else changed this. Review it and try again."),
    ).toBeNull();
  });

  it("asks for a review when someone else changed the binding first", () => {
    read.data = outOfDate;
    state.prepareResult = { ...outOfDate, state: "configuration_required" };
    render(field());
    fireEvent.click(applyButton()!);
    expect(
      screen.getByText("Someone else changed this. Review it and try again."),
    ).toBeTruthy();
  });

  it("tells someone who can't edit the project who can apply it", () => {
    state.projectWrite = false;
    read.data = outOfDate;
    render(field());
    expect(screen.getByText(/connection now asks for/)).toBeTruthy();
    expect(
      screen.getByText("Someone who can edit this project can apply them."),
    ).toBeTruthy();
    expect(applyButton()).toBeNull();
  });

  it("lets a bound, not-ready binding turn on when the connection records its scopes", () => {
    const scoped = [
      { ...clients[0], scope: ["read"] },
    ] as RemoteSessionClient[];
    read.data = {
      ...bound("first"),
      state: "transient_failure",
      scopes: ["write"],
      connectionScopes: ["read"],
      scopesOutOfDate: true,
    };
    render(
      <IdentityChainingField
        userSessionIssuer={{ id: "session-issuer" } as UserSessionIssuer}
        providers={{ external: false }}
        linkedClients={scoped}
        resource="https://example.com/mcp"
      />,
    );
    expect(
      (screen.getByRole("button", { name: "Turn on" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    expect(applyButton()).toBeNull();
    expect(screen.getByText("Turn it on again to apply them.")).toBeTruthy();
  });

  it("checks the recorded scopes when Turn on can't follow the connection", () => {
    const scoped = [
      { ...clients[0], scope: ["read"] },
    ] as RemoteSessionClient[];
    read.data = {
      ...bound("first"),
      state: "transient_failure",
      scopes: ["write"],
      connectionScopes: ["write"],
      scopesOutOfDate: false,
    };
    render(
      <IdentityChainingField
        userSessionIssuer={{ id: "session-issuer" } as UserSessionIssuer}
        providers={{ external: false }}
        linkedClients={scoped}
        resource="https://example.com/mcp"
      />,
    );
    expect(
      (screen.getByRole("button", { name: "Turn on" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("holds Turn off while the connection's scopes are applying", () => {
    read.data = outOfDate;
    render(field());
    const turnOff = () =>
      screen.getByRole("button", { name: "Turn off" }) as HTMLButtonElement;
    expect(turnOff().disabled).toBe(false);
    fireEvent.click(applyButton()!);
    expect(turnOff().disabled).toBe(true);
  });
});
