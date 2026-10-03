import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { TooltipProvider } from "@/components/ui/Tooltip";

import { OktaConnectionTab } from "./OktaConnectionTab";
import { makeConnection } from "./testFixtures";

const idleMutation = vi.hoisted(() => () => ({
  mutate: vi.fn(),
  isPending: false,
  error: null,
}));
vi.mock("@gram/client/react-query/createIdentityProviderConnection.js", () => ({
  useCreateIdentityProviderConnectionMutation: idleMutation,
}));
vi.mock("@gram/client/react-query/verifyIdentityProviderConnection.js", () => ({
  useVerifyIdentityProviderConnectionMutation: idleMutation,
}));
vi.mock("@gram/client/react-query/revokeIdentityProviderConnection.js", () => ({
  useRevokeIdentityProviderConnectionMutation: idleMutation,
}));
vi.mock(
  "@gram/client/react-query/submitIdentityProviderConnectionClientId.js",
  () => ({
    useSubmitIdentityProviderConnectionClientIdMutation: idleMutation,
  }),
);
vi.mock(
  "@gram/client/react-query/replaceIdentityProviderConnectionClientSecret.js",
  () => ({
    useReplaceIdentityProviderConnectionClientSecretMutation: idleMutation,
  }),
);
vi.mock(
  "@gram/client/react-query/recordIdentityProviderConnectionAgent.js",
  () => ({
    useRecordIdentityProviderConnectionAgentMutation: idleMutation,
  }),
);

afterEach(cleanup);

function renderTab(connection: OktaIdentityProviderConnection | undefined) {
  return render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <OktaConnectionTab connection={connection} />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

describe("OktaConnectionTab", () => {
  it.each([undefined, "revoked"] as const)(
    "shows only the organization form for %s",
    (status) => {
      renderTab(
        status ? ({ status } as OktaIdentityProviderConnection) : undefined,
      );
      expect(screen.getByLabelText("Okta organization URL")).toBeTruthy();
      expect(screen.getAllByRole("textbox")).toHaveLength(1);
      expect(screen.queryByText("Connection")).toBeNull();
      expect(screen.queryByText("Okta setup checklist")).toBeNull();
      expect(screen.queryByRole("navigation")).toBeNull();
    },
  );

  it("describes a public-key connection by its JWKS", () => {
    renderTab(makeConnection({ status: "pending" }));
    expect(
      screen.getByText(/public keys from the public key URL \(JWKS\)/),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Verify once the app is set up to use the public key URL (JWKS).",
      ),
    ).toBeTruthy();
  });

  it("describes an OIN connection by its client secret, not a JWKS", () => {
    renderTab(
      makeConnection({
        status: "pending",
        listingMode: "oin",
        jwksUrl: undefined,
      }),
    );
    expect(screen.getByText(/client ID and client secret/)).toBeTruthy();
    expect(
      screen.getByText(
        "Verify once the app’s client secret is submitted and its permissions are granted.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/JWKS/)).toBeNull();
  });
});
