import { TooltipProvider } from "@/components/ui/Tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AddCredentialSheet } from "./AddCredentialSheet";

const mocks = vi.hoisted(() => ({
  attach: vi.fn(),
  create: vi.fn(),
  clientPages: [] as unknown[][],
  registrationsFailed: false,
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    authClients: {
      list: () =>
        Promise.resolve(
          (async function* () {
            for (const page of mocks.clientPages)
              yield { result: { items: page } };
          })(),
        ),
    },
  }),
}));

vi.mock("@gram/client/react-query/authClients.js", () => ({
  invalidateAllAuthClients: () => Promise.resolve(),
}));

vi.mock("@gram/client/react-query/attachAuthClientToMcpServer.js", () => ({
  useAttachAuthClientToMcpServerMutation: () => ({
    mutate: mocks.attach,
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/createAuthClient.js", () => ({
  useCreateAuthClientMutation: () => ({
    mutate: mocks.create,
    isPending: false,
  }),
}));

vi.mock("@/lib/remote-identity", () => ({
  useAllRemoteSessionClients: () => ({
    items: [],
    isLoading: false,
    isError: mocks.registrationsFailed,
    error: null,
    refetch: vi.fn(),
  }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    remoteIdentityProviders: { href: () => "/authorization-servers" },
  }),
}));

function client(overrides: Record<string, unknown>) {
  return {
    id: "client-1",
    name: "Shared token",
    clientType: "static_credential",
    authClientSource: "admin",
    enabled: true,
    personal: false,
    headers: [],
    servers: [
      {
        id: "other-server",
        projectId: "project-1",
        public: false,
        name: "GitHub",
      },
    ],
    createdAt: new Date(),
    updatedAt: new Date(),
    ...overrides,
  };
}

function renderSheet(tunneled = false) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <MemoryRouter>
          <AddCredentialSheet
            mcpServerId="this-server"
            projectId="project-1"
            tunneled={tunneled}
            onOpenChange={() => undefined}
          />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.registrationsFailed = false;
});

describe("AddCredentialSheet", () => {
  it("offers only credentials this server can share", async () => {
    mocks.clientPages = [
      [client({ id: "shared", name: "Shared token" })],
      [
        client({
          id: "already-here",
          name: "Already here",
          servers: [
            { id: "this-server", projectId: "project-1", public: false },
          ],
        }),
        client({
          id: "other-project",
          name: "Other project",
          projectId: "project-2",
        }),
        client({ id: "second-page", name: "Second page token" }),
      ],
    ];
    renderSheet();

    fireEvent.click(await screen.findByRole("combobox"));
    expect(
      screen.getByRole("option", { name: /Second page token/ }),
    ).toBeTruthy();
    expect(screen.getByRole("option", { name: /Shared token/ })).toBeTruthy();
    expect(screen.queryByRole("option", { name: /Already here/ })).toBeNull();
    expect(screen.queryByRole("option", { name: /Other project/ })).toBeNull();
  });

  it("creates a static credential offered on this server", () => {
    mocks.clientPages = [];
    renderSheet();

    fireEvent.click(screen.getByRole("radio", { name: /^Static credential/ }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Engineering token" },
    });
    fireEvent.change(screen.getByLabelText("Value for Authorization"), {
      target: { value: "Bearer secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add credential" }));

    expect(mocks.create).toHaveBeenCalledWith({
      request: {
        createAuthClientRequestBody: {
          clientType: "static_credential",
          name: "Engineering token",
          projectId: "project-1",
          remoteSessionClientId: undefined,
          headers: [
            {
              name: "Authorization",
              isSecret: true,
              isRequired: false,
              value: "Bearer secret",
              valueFromRequestHeader: undefined,
            },
          ],
          mcpServerIds: ["this-server"],
        },
      },
    });
  });

  it("offers no header credentials on a tunneled server", async () => {
    mocks.clientPages = [
      [
        client({ id: "static", name: "Static token" }),
        client({
          id: "forwarded",
          name: "Forwarded token",
          clientType: "pass_through",
        }),
        client({
          id: "service",
          name: "Robot account",
          clientType: "service_account",
        }),
      ],
    ];
    renderSheet(true);

    expect(
      screen.queryByRole("radio", { name: /^Static credential/ }),
    ).toBeNull();
    expect(
      screen.queryByRole("radio", { name: /^Caller-supplied header/ }),
    ).toBeNull();
    expect(
      screen.getByRole("radio", { name: /^Service account/ }),
    ).toBeTruthy();

    fireEvent.click(await screen.findByRole("combobox"));
    expect(screen.getByRole("option", { name: /Robot account/ })).toBeTruthy();
    expect(screen.queryByRole("option", { name: /Static token/ })).toBeNull();
    expect(
      screen.queryByRole("option", { name: /Forwarded token/ }),
    ).toBeNull();
  });

  it("leaves out reuse when no other server has a credential to share", async () => {
    mocks.clientPages = [];
    renderSheet();

    await waitFor(() =>
      expect(
        screen.queryByRole("radio", { name: /Existing credential/ }),
      ).toBeNull(),
    );
    expect(
      screen
        .getByRole("radio", { name: /^Service account/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("says when sign-ins could not be loaded", () => {
    mocks.clientPages = [];
    mocks.registrationsFailed = true;
    renderSheet();

    fireEvent.click(screen.getByRole("radio", { name: /^Service account/ }));
    expect(screen.getByText(/Sign-ins could not be loaded/)).toBeTruthy();
    expect(screen.queryByText(/No sign-in that gets its own token/)).toBeNull();
  });
});
