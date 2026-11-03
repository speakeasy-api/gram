import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";

const duplicateClient = vi.fn();
const proxyRegister = vi.fn();
const onClose = vi.fn<() => void>();
const handleError = vi.fn();
const loadCopy = vi.fn();
const navigate = vi.fn();

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => false,
  useOrganization: () => ({
    id: "org-1",
    projects: [{ id: "project-1", name: "Project One", slug: "project-one" }],
  }),
}));
vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: vi.fn() }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
  useSdkClient: () => ({
    organizationRemoteSessionClients: { duplicate: duplicateClient },
  }),
}));
vi.mock("@/lib/proxyRegisterUpstreamClient", () => ({
  proxyRegisterUpstreamClient: (...args: unknown[]) => proxyRegister(...args),
  registrationProvenance: () => ({}),
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionClient.js", () => ({
  useOrganizationRemoteSessionClient: ({ id }: { id: string }) => loadCopy(id),
}));
vi.mock("@/lib/errors", () => ({
  handleError: (...args: unknown[]) => handleError(...args),
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => navigate,
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionClients.js", () => ({
  invalidateAllOrganizationRemoteSessionClients: vi.fn(() => Promise.resolve()),
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuers.js", () => ({
  invalidateAllOrganizationRemoteSessionIssuers: vi.fn(() => Promise.resolve()),
}));
vi.mock("./tabs/client/ClientCard", () => ({
  ClientCard: ({
    client,
    onDirtyChange,
  }: {
    client: RemoteSessionClient;
    onDirtyChange?: (dirty: boolean) => void;
  }) => (
    <div>
      <output data-testid="client-card">{client.id}</output>
      <button type="button" onClick={() => onDirtyChange?.(true)}>
        Make an edit
      </button>
    </div>
  ),
}));
vi.mock("@/components/ui/Sheet", () => ({
  Sheet: ({ open, children }: { open: boolean; children: ReactNode }) =>
    open ? children : null,
  SheetContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  SheetDescription: ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  ),
  SheetFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), warning: vi.fn() },
}));

import { DuplicateClientFlow } from "./DuplicateClient";

function source(overrides: Partial<RemoteSessionClient> = {}) {
  return {
    id: "source-id",
    clientId: "source-client",
    remoteSessionIssuerId: "issuer-1",
    projectId: "project-1",
    organizationId: "org-1",
    userSessionIssuerIds: [],
    legacyCallbackUrl: false,
    credentialOwner: "subject",
    tokenEndpointAuthMethod: "client_secret_basic",
    scope: ["read", "write"],
    createdAt: new Date(),
    updatedAt: new Date(),
    ...overrides,
  } as RemoteSessionClient;
}

function issuer(overrides: Partial<RemoteSessionIssuer> = {}) {
  return {
    id: "issuer-1",
    name: "Example IdP",
    projectId: "project-1",
    ...overrides,
  } as RemoteSessionIssuer;
}

function renderFlow(client: RemoteSessionClient, idp: RemoteSessionIssuer) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <DuplicateClientFlow
          client={client}
          issuer={idp}
          issuerId={idp.id}
          onClose={onClose}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function conflict(message: string) {
  return new GramError(message, {
    response: new Response(null, { status: 409 }),
    request: new Request("https://app.example.com/rpc"),
    body: "",
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  loadCopy.mockImplementation((id: string) => ({
    data: { id, clientId: "copied-client" },
    isError: false,
    refetch: vi.fn(),
  }));
  duplicateClient.mockResolvedValue({ id: "copy-id" });
  proxyRegister.mockResolvedValue({
    clientId: "registered-client",
    clientSecret: "registered-secret",
    tokenEndpointAuthMethod: "client_secret_post",
  });
});

afterEach(() => {
  cleanup();
});

describe("DuplicateClientFlow", () => {
  it("copies the credentials and opens the copy for editing", async () => {
    renderFlow(source(), issuer());

    expect(screen.queryByText("Register a new client")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));

    await waitFor(() =>
      expect(screen.getByTestId("client-card").textContent).toBe("copy-id"),
    );
    expect(duplicateClient).toHaveBeenCalledWith({
      duplicateOrganizationRemoteSessionClientForm: { id: "source-id" },
    });
    expect(proxyRegister).not.toHaveBeenCalled();
    expect(screen.getByText("Edit the copy")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "View client" }).getAttribute("href"),
    ).toBe(
      "/org/projects/project/remote-identity-providers/issuer-1/clients/copy-id",
    );
  });

  it("registers a new client when chosen", async () => {
    renderFlow(
      source(),
      issuer({ registrationEndpoint: "https://idp.example.com/register" }),
    );

    fireEvent.click(screen.getByText("Register a new client"));
    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));

    await waitFor(() => expect(duplicateClient).toHaveBeenCalled());
    expect(proxyRegister).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({
        registrationEndpoint: "https://idp.example.com/register",
        scope: "read write",
        tokenEndpointAuthMethod: "client_secret_basic",
        projectSlug: "project-one",
      }),
    );
    expect(duplicateClient).toHaveBeenCalledWith({
      duplicateOrganizationRemoteSessionClientForm: {
        id: "source-id",
        clientId: "registered-client",
        clientSecret: "registered-secret",
        tokenEndpointAuthMethod: "client_secret_post",
      },
    });
  });

  it("does not offer a new registration for a client metadata document client", () => {
    renderFlow(
      source({
        clientIdMetadataUri: "https://app.example.com/doc",
        tokenEndpointAuthMethod: "none",
      }),
      issuer({ registrationEndpoint: "https://idp.example.com/register" }),
    );

    expect(screen.queryByText("Register a new client")).toBeNull();
    expect(screen.getByText(/its own client metadata document/)).toBeTruthy();
  });

  it("blocks copying credentials the provider rejected when it cannot register a new client", () => {
    renderFlow(source({ upstreamRejectedAt: new Date() }), issuer());

    const confirm = screen.getByRole("button", {
      name: "Duplicate",
    }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    expect(
      screen.getByText(/no longer accepts this client's credentials/),
    ).toBeTruthy();
  });

  it("refuses a private_key_jwt client the provider knows by its key set address", () => {
    renderFlow(
      source({
        tokenEndpointAuthMethod:
          "private_key_jwt" as RemoteSessionClient["tokenEndpointAuthMethod"],
        jsonWebKeySetId: "set-1",
      }),
      issuer({ registrationEndpoint: "https://idp.example.com/register" }),
    );

    expect(screen.queryByText("Register a new client")).toBeNull();
    const confirm = screen.getByRole("button", {
      name: "Duplicate",
    }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    expect(screen.getByText(/knows by this client's own address/)).toBeTruthy();
  });

  it("does not offer a new registration through a tunnel it cannot route", () => {
    renderFlow(
      source(),
      issuer({
        registrationEndpoint: "https://idp.example.com/register",
        tunneledMcpServerId: "tunneled-server",
        projectId: "project-not-in-org",
      }),
    );

    expect(screen.queryByText("Register a new client")).toBeNull();
  });

  it("reuses a registration when retrying a failed duplicate", async () => {
    duplicateClient
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce({ id: "copy-id" });
    renderFlow(
      source(),
      issuer({ registrationEndpoint: "https://idp.example.com/register" }),
    );

    fireEvent.click(screen.getByText("Register a new client"));
    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));
    await waitFor(() => expect(handleError).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));
    await waitFor(() => expect(screen.getByTestId("client-card")).toBeTruthy());
    expect(proxyRegister).toHaveBeenCalledTimes(1);
    expect(duplicateClient).toHaveBeenCalledTimes(2);
  });

  it("explains a managed client refusal inline", async () => {
    duplicateClient.mockRejectedValueOnce(
      conflict(
        "this remote session client is managed by an identity provider connection",
      ),
    );
    renderFlow(source(), issuer());

    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));

    await waitFor(() =>
      expect(
        screen.getByText(/managed by an identity provider connection, so it/),
      ).toBeTruthy(),
    );
    expect(handleError).not.toHaveBeenCalled();
  });

  it("asks before discarding unsaved edits to the copy", async () => {
    renderFlow(source(), issuer());
    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));
    await waitFor(() => expect(screen.getByTestId("client-card")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Make an edit" }));
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByText("Discard unsaved changes?")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(navigate).not.toHaveBeenCalled();
  });

  it("offers a retry when the copy cannot be loaded", async () => {
    loadCopy.mockImplementation(() => ({
      data: undefined,
      isError: true,
      refetch: vi.fn(),
    }));
    renderFlow(source(), issuer());
    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));

    await waitFor(() =>
      expect(screen.getByText("Couldn't load the copy")).toBeTruthy(),
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
  });
});
