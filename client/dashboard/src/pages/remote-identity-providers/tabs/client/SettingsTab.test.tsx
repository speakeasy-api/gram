import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsTab } from "./SettingsTab";

const mutation = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ remoteIdentityProviders: { issuerDetail: {} } }),
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
      mutate: mutation.mutate,
      isPending: false,
    }),
  }),
);
vi.mock("./KeySetField", () => ({ KeySetField: () => null }));
vi.mock("../../clientDialogs", () => ({ DeleteClientDialog: () => null }));
vi.mock(
  "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields",
  () => ({
    TokenEndpointAuthMethodField: ({
      value,
      onChange,
    }: {
      value: AuthMethod | "";
      onChange: (method: AuthMethod) => void;
    }) => (
      <select
        aria-label="Authentication method"
        value={value}
        onChange={(event) => onChange(event.target.value as AuthMethod)}
      >
        <option value={AuthMethod.ClientSecretBasic}>
          client_secret_basic
        </option>
        <option value={AuthMethod.PrivateKeyJwt}>private_key_jwt</option>
      </select>
    ),
  }),
);

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function client(method: AuthMethod): RemoteSessionClient {
  return {
    id: "client-1",
    tokenEndpointAuthMethod: method,
    jsonWebKeySetId: "set-1",
  } as RemoteSessionClient;
}

describe("organization client settings", () => {
  it("hides secret rotation when private_key_jwt is selected", () => {
    render(
      <SettingsTab
        client={client(AuthMethod.PrivateKeyJwt)}
        issuerId="issuer-1"
      />,
    );

    expect(screen.queryByText("Rotate client secret")).toBeNull();
    expect(
      screen.getByText(/existing client secret is retained but not used/),
    ).toBeTruthy();
  });

  it("clears and omits an unsaved secret after switching to private_key_jwt", () => {
    render(
      <SettingsTab
        client={client(AuthMethod.ClientSecretBasic)}
        issuerId="issuer-1"
      />,
    );

    fireEvent.change(
      screen.getByPlaceholderText(/Enter a new secret to rotate/),
      { target: { value: "staged-secret" } },
    );
    fireEvent.change(
      screen.getByRole("combobox", { name: "Authentication method" }),
      {
        target: { value: AuthMethod.PrivateKeyJwt },
      },
    );

    expect(screen.queryByText("Rotate client secret")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(
      mutation.mutate.mock.calls[0]?.[0]?.request?.updateRemoteSessionClientForm
        ?.clientSecret,
    ).toBeUndefined();

    fireEvent.change(
      screen.getByRole("combobox", { name: "Authentication method" }),
      {
        target: { value: AuthMethod.ClientSecretBasic },
      },
    );
    expect(
      (
        screen.getByPlaceholderText(
          /Enter a new secret to rotate/,
        ) as HTMLInputElement
      ).value,
    ).toBe("");
  });
});
