import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat as AuthAudienceFormat } from "@gram/client/models/components/updateremotesessionclientform.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ModifyRemoteIdentityProviderSheet } from "./ModifyRemoteIdentityProviderSheet";

const sdk = vi.hoisted(() => ({
  updateIssuer: vi.fn(async () => ({})),
  updateClient: vi.fn(async () => ({})),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    remoteSessionIssuers: { update: sdk.updateIssuer },
    remoteSessionClients: { update: sdk.updateClient },
  }),
}));
vi.mock("@/components/asset-image-upload-field", () => ({
  AssetImageUploadField: () => null,
}));
vi.mock("@/components/ui/Sheet", () => ({
  Sheet: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <>{children}</> : null,
  SheetContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  SheetFooter: ({ children }: { children: ReactNode }) => <>{children}</>,
  SheetHeader: ({ children }: { children: ReactNode }) => <>{children}</>,
  SheetTitle: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({ data: { mcpServers: [] } }),
  invalidateAllMcpServers: vi.fn(async () => undefined),
}));
vi.mock("./useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: () => ({
    items: [
      {
        id: "client-1",
        clientId: "oauth-client",
        userSessionIssuerIds: ["user-issuer-1"],
        tokenEndpointAuthMethod: AuthMethod.PrivateKeyJwt,
        jsonWebKeySetId: "set-1",
      },
    ],
    isLoading: false,
  }),
}));
vi.mock("./useIssuerDuplicatePreflight", () => ({
  useIssuerDuplicatePreflight: () => ({ matches: [] }),
}));
vi.mock("./IssuerDuplicateWarning", () => ({
  IssuerDuplicateWarning: () => null,
}));
vi.mock("./IssuerFormFields", () => ({
  IssuerUrlField: ({
    issuerUrl,
    onIssuerUrlChange,
  }: {
    issuerUrl: string;
    onIssuerUrlChange: (value: string) => void;
  }) => (
    <input
      aria-label="Issuer URL"
      value={issuerUrl}
      onChange={(event) => onIssuerUrlChange(event.target.value)}
    />
  ),
  EndpointsFields: () => null,
  OverridesFields: () => null,
  ClientCredentialsFields: ({
    clientSecret,
    tokenEndpointAuthMethod,
    onClientSecretChange,
    onTokenEndpointAuthMethodChange,
  }: {
    clientSecret: string;
    tokenEndpointAuthMethod: AuthMethod | "";
    onClientSecretChange: (value: string) => void;
    onTokenEndpointAuthMethodChange: (method: AuthMethod) => void;
  }) => (
    <>
      <select
        aria-label="Authentication method"
        value={tokenEndpointAuthMethod}
        onChange={(event) =>
          onTokenEndpointAuthMethodChange(event.target.value as AuthMethod)
        }
      >
        <option value={AuthMethod.ClientSecretBasic}>
          client_secret_basic
        </option>
        <option value={AuthMethod.PrivateKeyJwt}>private_key_jwt</option>
      </select>
      {tokenEndpointAuthMethod !== AuthMethod.PrivateKeyJwt && (
        <input
          aria-label="Rotate client secret"
          value={clientSecret}
          onChange={(event) => onClientSecretChange(event.target.value)}
        />
      )}
    </>
  ),
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
      <option value={AuthAudienceFormat.Issuer}>Issuer URL</option>
      <option value={AuthAudienceFormat.TokenEndpoint}>
        Token endpoint URL
      </option>
    </select>
  ),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ModifyRemoteIdentityProviderSheet", () => {
  it("keeps private_key_jwt and its audience control after an issuer URL edit", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ModifyRemoteIdentityProviderSheet
          open
          onOpenChange={vi.fn<(open: boolean) => void>()}
          userSessionIssuer={{ id: "user-issuer-1" } as UserSessionIssuer}
          issuer={
            {
              id: "issuer-1",
              issuer: "https://idp.example.com",
              slug: "idp",
              authorizationEndpoint: "https://idp.example.com/authorize",
              tokenEndpoint: "https://idp.example.com/token",
              clientIdMetadataDocumentSupported: false,
            } as RemoteSessionIssuer
          }
        />
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(
        (
          screen.getByRole("combobox", {
            name: "Authentication method",
          }) as HTMLSelectElement
        ).value,
      ).toBe(AuthMethod.PrivateKeyJwt),
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Issuer URL" }), {
      target: { value: "https://other.example.com" },
    });

    expect(
      (
        screen.getByRole("combobox", {
          name: "Authentication method",
        }) as HTMLSelectElement
      ).value,
    ).toBe(AuthMethod.PrivateKeyJwt);
    expect(
      screen.queryByRole("textbox", { name: "Rotate client secret" }),
    ).toBeNull();
    fireEvent.change(
      screen.getByRole("combobox", { name: "Client assertion audience" }),
      { target: { value: AuthAudienceFormat.TokenEndpoint } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(sdk.updateClient).toHaveBeenCalledWith({
        updateRemoteSessionClientForm: expect.objectContaining({
          id: "client-1",
          tokenEndpointAuthMethod: AuthMethod.PrivateKeyJwt,
          tokenEndpointAuthAudienceFormat: AuthAudienceFormat.TokenEndpoint,
          clientSecret: undefined,
        }),
      }),
    );
  });

  it("preserves an omitted private_key_jwt audience format on an unrelated save", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ModifyRemoteIdentityProviderSheet
          open
          onOpenChange={vi.fn<(open: boolean) => void>()}
          userSessionIssuer={{ id: "user-issuer-1" } as UserSessionIssuer}
          issuer={
            {
              id: "issuer-1",
              issuer: "https://idp.example.com",
              slug: "idp",
              authorizationEndpoint: "https://idp.example.com/authorize",
              tokenEndpoint: "https://idp.example.com/token",
              clientIdMetadataDocumentSupported: false,
            } as RemoteSessionIssuer
          }
        />
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(
        (
          screen.getByRole("combobox", {
            name: "Client assertion audience",
          }) as HTMLSelectElement
        ).value,
      ).toBe(AuthAudienceFormat.Issuer),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(sdk.updateClient).toHaveBeenCalledWith({
        updateRemoteSessionClientForm: expect.objectContaining({
          id: "client-1",
          tokenEndpointAuthMethod: AuthMethod.PrivateKeyJwt,
          tokenEndpointAuthAudienceFormat: undefined,
        }),
      }),
    );
  });
});
