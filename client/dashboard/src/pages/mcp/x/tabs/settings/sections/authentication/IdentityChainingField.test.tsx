import type { ReactNode } from "react";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IdentityChainingField } from "./IdentityChainingField";

const pending = vi.hoisted(() => ({ prepare: false, unlink: false }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    isFetching: false,
    data: { state: "unlinked", scopes: [] },
  }),
  useQueryClient: () => ({}),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
}));
vi.mock("@/contexts/Auth", () => ({ useOrganization: () => ({ id: "org" }) }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
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
vi.mock("@gram/client/react-query/remoteSessionClientsUnlinkEMA.js", () => ({
  useRemoteSessionClientsUnlinkEMAMutation: () => ({
    isPending: pending.unlink,
  }),
}));

const clients = ["first", "second"].map((id) => ({
  id,
  clientId: id,
  remoteSessionIssuerId: `${id}-issuer`,
  grantTypes: ["urn:ietf:params:oauth:grant-type:jwt-bearer"],
})) as RemoteSessionClient[];

function field() {
  return (
    <IdentityChainingField
      userSessionIssuer={{ id: "session-issuer" } as UserSessionIssuer}
      linkedClients={clients}
      resource="https://example.com/mcp"
      permissionResourceId="server"
    />
  );
}

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
