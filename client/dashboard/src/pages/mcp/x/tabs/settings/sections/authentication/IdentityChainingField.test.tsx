import type { ReactNode } from "react";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityChainingField } from "./IdentityChainingField";
import type { ChainingProviders } from "./identityChainingGate";

const pending = vi.hoisted(() => ({ prepare: false, unlink: false }));
const unlinked = { state: "unlinked", scopes: [] };
const read = vi.hoisted(() => ({ data: {} as Record<string, unknown> }));
const scopeChecks = vi.hoisted(() => [] as unknown[]);
const issuerNames = vi.hoisted(() => ({
  items: [] as { id: string; name: string }[],
}));
const state = vi.hoisted(() => ({
  orgAdmin: true,
  session: { organizationOverride: false, impersonatorEmail: "" },
  check: {} as Record<string, unknown>,
  checkEnabled: [] as boolean[],
  addSucceeds: true,
  calls: [] as string[],
  prepare: vi.fn(),
  add: vi.fn(),
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
  useSession: () => state.session,
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
vi.mock("./useSpeakeasyAppendCheck", () => ({
  useSpeakeasyAppendCheck: (_target: unknown, enabled: boolean) => {
    state.checkEnabled.push(enabled);
    return state.check;
  },
}));
vi.mock(
  "@gram/client/react-query/addOrganizationRemoteSessionIssuerSpeakeasyEMAProvider.js",
  () => ({
    useAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation: () => ({
      isPending: false,
      error: null,
      mutate: (variables: unknown, options?: { onSuccess?: () => void }) => {
        state.calls.push("add");
        state.add(variables);
        if (state.addSucceeds) options?.onSuccess?.();
      },
    }),
  }),
);
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

const openClient = () =>
  fireEvent.click(screen.getByRole("button", { name: /^Client/ }));

beforeEach(() => {
  read.data = unlinked;
  scopeChecks.length = 0;
  issuerNames.items = [];
  state.orgAdmin = true;
  state.session = { organizationOverride: false, impersonatorEmail: "" };
  state.check = { isSuccess: true, isFetching: false, isError: false };
  state.checkEnabled.length = 0;
  state.addSucceeds = true;
  state.calls.length = 0;
  state.prepare.mockReset();
  state.add.mockReset();
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
      openClient();
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
    openClient();
    expect(screen.getByRole("combobox").textContent).toBe("second");
    expect(screen.queryByText("Use this client instead")).toBeNull();
  });

  it("says whether chaining is on, next to the action that changes it", () => {
    read.data = bound("second");
    const { rerender } = render(field());
    expect(screen.getByText("On")).toBeTruthy();
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
  const plainIssuer = { id: "session-issuer" } as UserSessionIssuer;
  const oktaOrder = {
    isDefault: false,
    vendorIssuer: "https://vendor.example.com",
    providers: [{ kind: "external", remoteSessionIssuerId: "okta" }],
  };
  const withSpeakeasy = {
    ...oktaOrder,
    providers: [
      ...oktaOrder.providers,
      { kind: "speakeasy", internalSessionIssuerId: "internal" },
    ],
  };
  const trusting = {
    id: "session-issuer",
    trustedRemoteSessionIssuerId: "idp",
    trustedRemoteSessionClientId: "second",
  } as UserSessionIssuer;

  function speakeasyField(
    overrides: Partial<Parameters<typeof IdentityChainingField>[0]> = {},
  ) {
    return (
      <IdentityChainingField
        userSessionIssuer={plainIssuer}
        providers={speakeasyOnly}
        speakeasyEnabled
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

  it("offers every linked client when no sign-in client is trusted", () => {
    render(field(speakeasyOnly, plainIssuer));
    openClient();
    expect(screen.getByRole("combobox")).toBeTruthy();
  });

  it("keeps the identity provider's sign-in client out of the picker", () => {
    render(field({ external: true, speakeasy: false }, trusting));
    expect(screen.queryByRole("button", { name: /^Client/ })).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
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

  it("adds Speakeasy to the order, then prepares the binding", () => {
    read.data = { ...unlinked, generation: 0, emaProviders: oktaOrder };
    render(speakeasyField());
    fireEvent.click(enableButton());
    expect(state.calls).toEqual(["add", "prepare"]);
    expect(state.add).toHaveBeenCalledWith({
      request: {
        addSpeakeasyEMAProviderRequestBody: {
          remoteSessionIssuerId: "issuer",
          userSessionIssuerId: "session-issuer",
          dryRun: false,
        },
      },
    });
  });

  it("doesn't prepare when adding Speakeasy fails", () => {
    state.addSucceeds = false;
    read.data = { ...unlinked, generation: 0, emaProviders: oktaOrder };
    render(speakeasyField());
    fireEvent.click(enableButton());
    expect(state.calls).toEqual(["add"]);
  });

  it("skips the add when Speakeasy is already in the order", () => {
    state.orgAdmin = false;
    read.data = { ...unlinked, generation: 0, emaProviders: withSpeakeasy };
    render(speakeasyField());
    fireEvent.click(enableButton());
    expect(state.calls).toEqual(["prepare"]);
    expect(state.checkEnabled).not.toContain(true);
  });

  it("never writes the order for a server that trusts an external identity provider", () => {
    read.data = { ...unlinked, generation: 0, emaProviders: oktaOrder };
    render(
      speakeasyField({
        userSessionIssuer: trusting,
        providers: { external: true, speakeasy: true },
      }),
    );
    fireEvent.click(enableButton());
    expect(state.calls).toEqual(["prepare"]);
    expect(state.checkEnabled).not.toContain(true);
  });

  it.each([
    [
      "the server refuses the add",
      (): void => {
        state.check = {
          isSuccess: false,
          isFetching: false,
          isError: true,
          error: new Error("other servers chain to this vendor"),
        };
      },
      {},
      "Other servers chain to this vendor.",
    ],
    [
      "Speakeasy's issuer is off",
      (): void => undefined,
      { speakeasyEnabled: false },
      "Turn on Speakeasy’s issuer first.",
    ],
    [
      "the person isn't an organization admin",
      (): void => {
        state.orgAdmin = false;
      },
      {},
      "An organization admin needs to add Speakeasy for this vendor first.",
    ],
    [
      "the session is a support session",
      (): void => {
        state.session = { organizationOverride: true, impersonatorEmail: "" };
      },
      {},
      "The provider order can’t be changed from a support or impersonated session.",
    ],
  ] as const)(
    "disables Enable with a reason when %s",
    (_name, arrange, overrides, reason) => {
      arrange();
      read.data = { ...unlinked, generation: 0, emaProviders: oktaOrder };
      render(speakeasyField(overrides));
      expect(enableButton().disabled).toBe(true);
      expect(screen.getByText(reason, { exact: false })).toBeTruthy();
      fireEvent.click(enableButton());
      expect(state.calls).toEqual([]);
    },
  );

  it("links a refusal for an off issuer to the identity settings", () => {
    read.data = { ...unlinked, generation: 0, emaProviders: oktaOrder };
    render(speakeasyField({ speakeasyEnabled: false }));
    expect(
      screen
        .getByRole("link", { name: "Speakeasy identity provider" })
        .getAttribute("href"),
    ).toBe("/org/identity?tab=identity-providers");
  });

  it("offers to add Speakeasy for a ready server whose order lacks it", () => {
    read.data = { ...bound("first"), emaProviders: oktaOrder };
    render(speakeasyField());
    expect(
      screen.getByText(/Speakeasy isn’t in this vendor’s provider order/),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add Speakeasy" }));
    expect(state.calls).toEqual(["add"]);
  });

  it("leaves the recovery action to organization admins", () => {
    state.orgAdmin = false;
    read.data = { ...bound("first"), emaProviders: oktaOrder };
    render(speakeasyField());
    expect(
      screen.getByText(/Speakeasy isn’t in this vendor’s provider order/),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add Speakeasy" })).toBeNull();
  });

  it("disables without touching the order", () => {
    read.data = { ...bound("first"), emaProviders: withSpeakeasy };
    render(speakeasyField());
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    fireEvent.click(
      screen.getAllByRole("button", { name: "Turn off" }).at(-1)!,
    );
    expect(state.calls).toEqual(["unlink"]);
  });

  it("names only the sign-ins this server can use, in order", () => {
    issuerNames.items = [{ id: "okta", name: "Okta" }];
    read.data = {
      ...bound("first"),
      emaProviders: {
        ...withSpeakeasy,
        providers: [
          { kind: "external", remoteSessionIssuerId: "okta" },
          { kind: "speakeasy", internalSessionIssuerId: "internal" },
        ],
      },
    };
    const { rerender } = render(speakeasyField());
    expect(
      screen.getByText(
        "People can authenticate to example.com with these identity providers:",
      ),
    ).toBeTruthy();
    const providers = () =>
      screen.getAllByRole("listitem").map((item) => item.textContent);
    expect(providers()).toEqual(["Okta", "thenSpeakeasy"]);
    read.data = {
      ...bound("first"),
      emaProviders: {
        ...withSpeakeasy,
        providers: [
          {
            kind: "external",
            remoteSessionIssuerId: "okta",
            unavailableReason: "not_trusted_by_endpoint",
          },
          { kind: "speakeasy", internalSessionIssuerId: "internal" },
        ],
      },
    };
    rerender(speakeasyField());
    expect(providers()).toEqual(["Speakeasy"]);
    expect(screen.queryByText(/Okta/)).toBeNull();
  });

  it("says when no sign-in in the order can serve this server", () => {
    read.data = {
      ...bound("first"),
      emaProviders: {
        ...withSpeakeasy,
        providers: [
          {
            kind: "speakeasy",
            internalSessionIssuerId: "internal",
            unavailableReason: "signing_paused",
          },
        ],
      },
    };
    render(speakeasyField());
    expect(screen.getByText("On, but not running")).toBeTruthy();
  });

  it("has no chaining scope list of its own", () => {
    read.data = { ...bound("first"), emaProviders: withSpeakeasy };
    render(speakeasyField());
    expect(screen.queryByText("Requested scopes")).toBeNull();
    expect(screen.queryByText("Save scopes")).toBeNull();
  });

  it("shows the vendor setup step with Speakeasy's issuer until ready", () => {
    read.data = { ...unlinked, generation: 0, emaProviders: withSpeakeasy };
    const { rerender } = render(speakeasyField());
    expect(screen.getByText(/^Set up .+ first$/)).toBeTruthy();
    expect(
      screen.getByText("https://speakeasy.example.com/issuer"),
    ).toBeTruthy();
    read.data = { ...bound("first"), emaProviders: withSpeakeasy };
    rerender(speakeasyField());
    expect(screen.queryByText(/^Set up .+ first$/)).toBeNull();
  });

  it("names the vendor from its host when its issuer has no name", () => {
    read.data = { ...unlinked, generation: 0, emaProviders: withSpeakeasy };
    render(speakeasyField());
    expect(screen.getByText("Set up example.com first")).toBeTruthy();
  });
});
