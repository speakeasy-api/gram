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
const setProviders = vi.hoisted(() => vi.fn());
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
  useRBAC: () => ({ hasScope: () => true }),
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
vi.mock("@/components/vendor-ema-trust-notice", () => ({
  VendorEmaTrustNotice: () => null,
}));
vi.mock("./IdentityChainingScopesField", () => ({
  IdentityChainingScopesField: () => null,
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
  }),
}));
vi.mock(
  "@gram/client/react-query/setOrganizationRemoteSessionIssuerEMAProviders.js",
  () => ({
    useSetOrganizationRemoteSessionIssuerEMAProvidersMutation: () => ({
      isPending: false,
      mutate: setProviders,
    }),
  }),
);
vi.mock("@gram/client/react-query/remoteSessionClientsUnlinkEMA.js", () => ({
  useRemoteSessionClientsUnlinkEMAMutation: () => ({
    isPending: pending.unlink,
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
  setProviders.mockReset();
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

  it("makes replacing the bound client explicit", () => {
    read.data = bound("elsewhere");
    render(field());
    expect(screen.getByText("Use this client instead")).toBeTruthy();
    expect(screen.queryByText("Update scopes")).toBeNull();
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

  it("offers every linked client when no sign-in client is trusted", () => {
    render(field(speakeasyOnly, plainIssuer));
    expect(screen.getByRole("combobox")).toBeTruthy();
    expect(
      screen.getByText(/Speakeasy vouches for the person’s Speakeasy sign-in/),
    ).toBeTruthy();
  });

  it("keeps the identity provider's sign-in client out of the picker", () => {
    const trusting = {
      id: "session-issuer",
      trustedRemoteSessionIssuerId: "idp",
      trustedRemoteSessionClientId: "second",
    } as UserSessionIssuer;
    render(field({ external: true, speakeasy: false }, trusting));
    expect(screen.queryByRole("combobox")).toBeNull();
  });

  it("adds Speakeasy ahead of the saved external providers", () => {
    read.data = {
      ...unlinked,
      emaProviders: {
        isDefault: true,
        vendorIssuer: "https://vendor.example.com",
        providers: [{ kind: "external" }],
      },
    };
    render(field(speakeasyOnly, plainIssuer));
    fireEvent.click(screen.getByText("Use Speakeasy sign-in"));
    expect(setProviders).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "issuer",
          providers: [{ kind: "speakeasy" }],
        },
      },
    });
  });

  it("restores the default when Speakeasy was the only provider", () => {
    read.data = {
      ...unlinked,
      emaProviders: {
        isDefault: false,
        vendorIssuer: "https://vendor.example.com",
        providers: [{ kind: "speakeasy", internalSessionIssuerId: "internal" }],
      },
    };
    render(field(speakeasyOnly, plainIssuer));
    fireEvent.click(screen.getByText("Stop using Speakeasy"));
    expect(setProviders).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "issuer",
          providers: [],
        },
      },
    });
  });
});
