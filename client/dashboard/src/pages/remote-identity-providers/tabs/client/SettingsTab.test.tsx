import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat as AuthAudienceFormat } from "@gram/client/models/components/updateremotesessionclientform.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsTab } from "./SettingsTab";

const mutation = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@/routes", () => ({
  useRoutes: () => ({ remoteIdentityProviders: { issuerDetail: {} } }),
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
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuer.js", () => ({
  useOrganizationRemoteSessionIssuer: () => ({ data: undefined }),
}));
vi.mock("./KeySetField", () => ({ KeySetField: () => null }));
vi.mock("../../clientDialogs", () => ({ DeleteClientDialog: () => null }));
vi.mock(
  "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields",
  () => ({
    ClientAssertionAudienceField: ({
      value,
      onChange,
    }: {
      value: AuthAudienceFormat;
      onChange: (value: AuthAudienceFormat) => void;
    }) => (
      <select
        aria-label="Client assertion audience"
        value={value}
        onChange={(event) => onChange(event.target.value as AuthAudienceFormat)}
      >
        <option value={AuthAudienceFormat.Issuer}>Issuer URL (default)</option>
        <option value={AuthAudienceFormat.TokenEndpoint}>
          Token endpoint URL
        </option>
      </select>
    ),
    TokenEndpointAuthMethodField: ({
      value,
      onChange,
      allowPrivateKeyJwt,
    }: {
      value: AuthMethod | "";
      onChange: (method: AuthMethod | "") => void;
      allowPrivateKeyJwt?: boolean;
    }) => (
      <select
        aria-label="Authentication method"
        value={value}
        onChange={(event) => onChange(event.target.value as AuthMethod)}
      >
        <option value={AuthMethod.ClientSecretBasic}>
          client_secret_basic
        </option>
        {(allowPrivateKeyJwt || value === AuthMethod.PrivateKeyJwt) && (
          <option value={AuthMethod.PrivateKeyJwt}>private_key_jwt</option>
        )}
      </select>
    ),
  }),
);

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function client(
  method: AuthMethod,
  jsonWebKeySetId: string | null = "set-1",
): RemoteSessionClient {
  return {
    id: "client-1",
    tokenEndpointAuthMethod: method,
    jsonWebKeySetId,
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
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateRemoteSessionClientForm: expect.objectContaining({
            id: "client-1",
            tokenEndpointAuthMethod: AuthMethod.PrivateKeyJwt,
            clientSecret: undefined,
            tokenEndpointAuthAudienceFormat: undefined,
          }),
        }),
      }),
    );

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

  it("hides private_key_jwt when no signing key set is attached", () => {
    render(
      <SettingsTab
        client={client(AuthMethod.ClientSecretBasic, null)}
        issuerId="issuer-1"
      />,
    );

    expect(
      screen.queryByRole("option", { name: AuthMethod.PrivateKeyJwt }),
    ).toBeNull();
  });

  it("discards an unsaved private_key_jwt selection when the key set is detached", () => {
    const view = render(
      <SettingsTab
        client={client(AuthMethod.ClientSecretBasic)}
        issuerId="issuer-1"
      />,
    );
    fireEvent.change(
      screen.getByRole("combobox", { name: "Authentication method" }),
      { target: { value: AuthMethod.PrivateKeyJwt } },
    );

    view.rerender(
      <SettingsTab
        client={client(AuthMethod.ClientSecretBasic, null)}
        issuerId="issuer-1"
      />,
    );

    expect(
      (
        screen.getByRole("combobox", {
          name: "Authentication method",
        }) as HTMLSelectElement
      ).value,
    ).toBe(AuthMethod.ClientSecretBasic);
  });

  it("preserves an unset assertion audience on an unrelated save", () => {
    render(
      <SettingsTab
        client={client(AuthMethod.ClientSecretBasic)}
        issuerId="issuer-1"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateRemoteSessionClientForm: expect.objectContaining({
            tokenEndpointAuthAudienceFormat: undefined,
          }),
        }),
      }),
    );
  });

  it("saves an explicitly selected token endpoint audience", () => {
    render(
      <SettingsTab
        client={client(AuthMethod.PrivateKeyJwt)}
        issuerId="issuer-1"
      />,
    );

    fireEvent.change(
      screen.getByRole("combobox", { name: "Client assertion audience" }),
      { target: { value: AuthAudienceFormat.TokenEndpoint } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateRemoteSessionClientForm: expect.objectContaining({
            tokenEndpointAuthAudienceFormat: AuthAudienceFormat.TokenEndpoint,
          }),
        }),
      }),
    );
  });
});
