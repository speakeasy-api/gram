import { unrecognized } from "@gram/client/types/unrecognized.js";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ClientCard } from "./ClientCard";

// Unlike OverviewTab.test, this renders the real Radix select fields, which a
// disabled fieldset does not stop from opening.

const testState = vi.hoisted(() => ({ admin: false, mutate: vi.fn() }));
beforeEach(() => {
  testState.admin = false;
  testState.mutate.mockClear();
});

vi.mock("@/lib/remote-identity/queries/useCustomerManagedKeys", () => ({
  useCustomerManagedKeys: () => ({ entitled: false, isLoading: false }),
}));
vi.mock("@/contexts/Auth", () => ({ useIsPlatformAdmin: () => true }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAnyScope: () => testState.admin,
    hasScope: () => false,
    hasAllScopes: () => false,
    isLoading: false,
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock(
  "@gram/client/react-query/updateOrganizationRemoteSessionClient.js",
  () => ({
    useUpdateOrganizationRemoteSessionClientMutation: () => ({
      mutate: testState.mutate,
      isPending: false,
    }),
  }),
);
vi.mock("./KeySetField", () => ({
  KeySetField: ({ disabled }: { disabled?: boolean }) => (
    <div data-testid="key-set" data-disabled={String(disabled)} />
  ),
}));

afterEach(cleanup);

const client = {
  id: "client-1",
  clientId: "upstream-client",
  tokenEndpointAuthMethod: AuthMethod.PrivateKeyJwt,
  jsonWebKeySetId: "set-1",
  legacyCallbackUrl: false,
  audience: "aud",
} as RemoteSessionClient;

const issuer = {
  id: "issuer-1",
  issuer: "https://idp.example.com",
  scopesSupported: ["openid"],
} as RemoteSessionIssuer;

describe("client card without org:admin", () => {
  it("disables the Radix selects and every other control", () => {
    render(<ClientCard client={client} issuer={issuer} issuerId="issuer-1" />);

    const triggers = screen
      .getAllByRole("combobox")
      .filter((el) => el.id !== "client-scopes");
    expect(triggers).toHaveLength(2);
    for (const trigger of triggers) {
      expect((trigger as HTMLButtonElement).disabled).toBe(true);
      fireEvent.pointerDown(trigger, { button: 0, pointerType: "mouse" });
      expect(trigger.getAttribute("aria-expanded")).toBe("false");
    }
    expect(screen.queryByRole("listbox")).toBeNull();

    expect(
      (screen.getByRole("combobox", { name: "Scopes" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      (screen.getByLabelText("Audience") as HTMLInputElement).disabled,
    ).toBe(true);
    expect((screen.getByRole("switch") as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(screen.getByTestId("key-set").getAttribute("data-disabled")).toBe(
      "true",
    );
  });
});

describe("client card with unsupported configuration", () => {
  it.each([
    { tokenEndpointAuthMethod: unrecognized("future_auth") },
    { tokenEndpointAuthAudienceFormat: unrecognized("future_audience") },
  ])("disables editing for an administrator: %j", (unknown) => {
    testState.admin = true;
    render(
      <ClientCard
        client={{ ...client, ...unknown }}
        issuer={issuer}
        issuerId="issuer-1"
      />,
    );
    expect(
      (screen.getByLabelText("Audience") as HTMLInputElement).disabled,
    ).toBe(true);
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    expect(screen.getByText(/unsupported client configuration/)).toBeTruthy();
    expect(screen.getByTestId("key-set").getAttribute("data-disabled")).toBe(
      "true",
    );
  });
});

it("preserves unknown audience configuration for unrelated non-JWT edits and blocks switching to JWT", () => {
  testState.admin = true;
  render(
    <ClientCard
      client={{
        ...client,
        tokenEndpointAuthMethod: AuthMethod.ClientSecretPost,
        tokenEndpointAuthAudienceFormat: unrecognized("future_audience"),
      }}
      issuer={issuer}
      issuerId="issuer-1"
    />,
  );
  const audience = screen.getByLabelText("Audience");
  expect(audience).toHaveProperty("disabled", false);
  fireEvent.change(audience, { target: { value: "new-audience" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(testState.mutate).toHaveBeenCalledWith({
    request: {
      updateRemoteSessionClientForm: expect.objectContaining({
        audience: "new-audience",
        tokenEndpointAuthAudienceFormat: undefined,
      }),
    },
  });
  const auth = screen
    .getAllByRole("combobox")
    .find((el) => el.id !== "client-scopes")!;
  fireEvent.click(auth);
  expect(
    screen
      .getByRole("option", { name: /private_key_jwt/ })
      .getAttribute("aria-disabled"),
  ).toBe("true");
});
