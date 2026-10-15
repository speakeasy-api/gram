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
const scopeChecks = vi.hoisted(() => [] as unknown[]);
const issuerNames = vi.hoisted(() => ({
  items: [] as { id: string; name: string; issuer?: string }[],
}));
const state = vi.hoisted(() => ({
  orgAdmin: true,
  calls: [] as string[],
  prepare: vi.fn(),
  unlink: vi.fn(),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ isFetching: false, data: read.data }),
  useQueryClient: () => ({}),
}));
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
vi.mock("@/lib/remote-identity/queries/useProtectedResourceMetadata", () => ({
  useProtectedResourceMetadata: () => ({}),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/remoteSessionIssuer.js", () => ({
  useRemoteSessionIssuer: () => ({}),
}));
vi.mock("@gram/client/react-query/remoteSessionClientsPrepareEMA.js", () => ({
  useRemoteSessionClientsPrepareEMAMutation: () => ({
    isPending: pending.prepare,
    mutate: (variables: unknown) => {
      state.calls.push("prepare");
      state.prepare(variables);
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
  providers: ChainingProviders = { external: true, speakeasy: false },
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
  scopeChecks.length = 0;
  issuerNames.items = [];
  state.orgAdmin = true;
  state.calls.length = 0;
  state.prepare.mockReset();
  state.unlink.mockReset();
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
  const speakeasyOnly = { external: false, speakeasy: true };
  const plainIssuer = {
    id: "session-issuer",
    slug: "speakeasy-sign-in",
  } as UserSessionIssuer;
  const okta = { kind: "external", remoteSessionIssuerId: "okta" };
  const speakeasy = { kind: "speakeasy", internalSessionIssuerId: "internal" };
  const order = (...providers: Record<string, unknown>[]) => ({
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
  });

  it("keeps the identity provider's sign-in client out of the picker", () => {
    read.data = { ...unlinked, emaProviders: order(okta) };
    render(field({ external: true, speakeasy: false }, trusting));
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(
      within(chain()).getByText("first", { selector: "p, span" }),
    ).toBeTruthy();
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
    read.data = { ...unlinked, generation: 0, emaProviders: order(speakeasy) };
    render(speakeasyField());
    fireEvent.click(enableButton());
    expect(state.calls).toEqual(["prepare"]);
  });

  it("walks the chain: sign-in, who vouches, what the vendor trusts, client", () => {
    read.data = { ...bound("first"), emaProviders: order(okta, speakeasy) };
    render(speakeasyField({ userSessionIssuer: trusting }));
    const links = within(chain()).getAllByText(
      /^(Sign-in|Vouched for by|example.com trusts|Client)$/,
    );
    expect(links.map((link) => link.textContent)).toEqual([
      "Sign-in",
      "Vouched for by",
      "example.com trusts",
      "Client",
    ]);
    expect(screen.getByText("(okta-sign-in)").parentElement?.textContent).toBe(
      "Okta (okta-sign-in)",
    );
    expect(
      screen
        .getByRole("link", { name: "Change in Sessions" })
        .getAttribute("href"),
    ).toBe("#sessions");
    expect(vouchers()).toEqual([
      "Oktafor people who signed in with Okta · tried first",
      "Speakeasyfor everyone who signs in to this server · then",
    ]);
    expect(trusts()).toEqual([
      {
        name: "Okta",
        accepted: "Not accepted yet",
        url: "https://okta.example.com",
      },
      {
        name: "Speakeasy",
        accepted: "Not accepted yet",
        url: "https://speakeasy.example.com/issuer",
      },
    ]);
    expect(screen.getAllByText(/In example.com’s admin settings/)).toHaveLength(
      1,
    );
    expect(
      screen
        .getByRole("link", { name: "Every server that reaches example.com" })
        .getAttribute("href"),
    ).toBe(
      "/org/identity?tab=identity-providers&service=https%3A%2F%2Fvendor.example.com",
    );
  });

  it("waits for the vendor until an assertion has been accepted", () => {
    read.data = { ...bound("first"), emaProviders: order(speakeasy) };
    const { rerender } = render(speakeasyField());
    expect(screen.getByText("On, not proven yet")).toBeTruthy();
    read.data = {
      ...bound("first"),
      emaProviders: order({
        ...speakeasy,
        lastAcceptedAt: new Date(Date.now() - 2 * 60 * 60 * 1000),
      }),
    };
    rerender(speakeasyField());
    expect(screen.getByText("Working")).toBeTruthy();
    expect(
      screen.getByText("example.com accepted a sign-in 2h ago"),
    ).toBeTruthy();
    expect(trusts()).toEqual([
      {
        name: "Speakeasy",
        accepted: "Last worked 2h ago",
        url: undefined,
      },
    ]);
    expect(screen.queryByText(/admin settings/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Issuer URL" }));
    expect(trusts()[0]?.url).toBe("https://speakeasy.example.com/issuer");
  });

  it("names Speakeasy's own sign-in when no identity provider is trusted", () => {
    read.data = { ...bound("first"), emaProviders: order(speakeasy) };
    render(speakeasyField());
    expect(screen.getByText("Speakeasy’s own sign-in")).toBeTruthy();
    expect(screen.getByText("(speakeasy-sign-in)")).toBeTruthy();
  });

  it("leaves out an identity provider this server's sign-in doesn't use", () => {
    read.data = {
      ...bound("first"),
      emaProviders: order(
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
      emaProviders: order({ ...speakeasy, unavailableReason: reason }),
    };
    render(speakeasyField());
    expect(screen.getByText(text)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Speakeasy identity provider" })
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
      emaProviders: order({ ...speakeasy, unavailableReason: "keyless" }),
    };
    render(speakeasyField());
    expect(enableButton().disabled).toBe(true);
    expect(
      screen.getByText(
        "No identity provider can vouch for people on this server yet.",
      ),
    ).toBeTruthy();
    fireEvent.click(enableButton());
    expect(state.calls).toEqual([]);
  });

  it("says when chaining is on but nobody can vouch right now", () => {
    read.data = {
      ...bound("first"),
      emaProviders: order({
        ...speakeasy,
        unavailableReason: "signing_paused",
      }),
    };
    render(speakeasyField());
    expect(screen.getByText("On, but not running")).toBeTruthy();
  });

  it("points at the organization's identity settings when nobody is listed", () => {
    read.data = { ...unlinked, generation: 0, emaProviders: order() };
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
    read.data = { ...unlinked, generation: 0, emaProviders: order(speakeasy) };
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
    read.data = { ...bound("first"), emaProviders: order(speakeasy) };
    render(speakeasyField());
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    fireEvent.click(
      screen.getAllByRole("button", { name: "Turn off" }).at(-1)!,
    );
    expect(state.calls).toEqual(["unlink"]);
  });

  it("has no chaining scope list of its own", () => {
    read.data = { ...bound("first"), emaProviders: order(speakeasy) };
    render(speakeasyField());
    expect(screen.queryByText("Requested scopes")).toBeNull();
    expect(screen.queryByText("Save scopes")).toBeNull();
  });
});
