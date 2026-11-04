import { TooltipProvider } from "@/components/ui/Tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
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
  registrations: [] as unknown[],
  issuers: [] as unknown[],
  issuersLoading: false,
  authorizationServers: undefined as string[] | undefined,
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
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

vi.mock("@/lib/remote-identity", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/remote-identity")>()),
  useAllRemoteSessionClients: () => ({
    items: mocks.registrations,
    isLoading: false,
    isError: mocks.registrationsFailed,
    error: null,
    refetch: vi.fn(),
  }),
  useRemoteSessionIssuersByIds: () => ({
    items: mocks.issuers,
    isLoading: mocks.issuersLoading,
    isError: false,
    refetch: vi.fn(),
  }),
  useProtectedResourceMetadata: () => ({
    status: mocks.authorizationServers ? "available" : "unavailable",
    metadata: mocks.authorizationServers
      ? { authorizationServers: mocks.authorizationServers }
      : null,
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
  const queryClient = new QueryClient();
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <MemoryRouter>
          <AddCredentialSheet
            mcpServerId="this-server"
            projectId="project-1"
            remoteMcpServerId="remote-1"
            tunneled={tunneled}
            onOpenChange={() => undefined}
          />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.registrationsFailed = false;
  mocks.registrations = [];
  mocks.issuers = [];
  mocks.issuersLoading = false;
  mocks.authorizationServers = undefined;
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

  it.each([false, true])(
    "preserves a stale existing selection after refetch (other candidates: %s)",
    async (hasOtherCandidate) => {
      mocks.clientPages = [[client({ id: "selected" })]];
      const queryClient = renderSheet();

      fireEvent.click(await screen.findByRole("combobox"));
      fireEvent.click(screen.getByRole("option", { name: /Shared token/ }));
      expect(
        screen
          .getByRole("button", { name: "Add credential" })
          .hasAttribute("disabled"),
      ).toBe(false);

      mocks.clientPages = hasOtherCandidate
        ? [[client({ id: "other", name: "Other token" })]]
        : [];
      await act(async () => {
        await queryClient.invalidateQueries();
      });

      expect(
        screen
          .getByRole("radio", { name: /Existing credential/ })
          .getAttribute("aria-checked"),
      ).toBe("true");
      expect(screen.queryByLabelText("Name")).toBeNull();
      expect(
        await screen.findByText(/This credential is no longer available/),
      ).toBeTruthy();
      const submit = screen.getByRole("button", { name: "Add credential" });
      expect(submit.hasAttribute("disabled")).toBe(true);
      fireEvent.click(submit);
      expect(mocks.attach).not.toHaveBeenCalled();
      expect(mocks.create).not.toHaveBeenCalled();
    },
  );

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

  describe("sign-ins", () => {
    const signInMocks = () => {
      mocks.clientPages = [];
      mocks.issuers = [
        { id: "linear", issuer: "https://mcp.linear.app/", name: "Linear" },
        { id: "notion", issuer: "https://mcp.notion.com", name: "Notion" },
      ];
      mocks.registrations = [
        {
          id: "01a1a312",
          clientId: "01a1a312",
          credentialOwner: "subject",
          remoteSessionIssuerId: "linear",
        },
        {
          id: "01a19e9a",
          clientId: "oacli_notion",
          credentialOwner: "subject",
          remoteSessionIssuerId: "notion",
        },
      ];
    };
    const openPicker = () => {
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );
      fireEvent.click(screen.getByRole("combobox"));
    };

    it("offers the upstream's authorization server, by name", () => {
      signInMocks();
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      openPicker();

      expect(screen.getByRole("option", { name: /Linear/ })).toBeTruthy();
      expect(screen.queryByRole("option", { name: /Notion/ })).toBeNull();
      expect(screen.queryByText("01a1a312")).toBeNull();
    });

    it("keeps the other sign-ins one click away", () => {
      signInMocks();
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );
      fireEvent.click(screen.getByRole("button", { name: "Show all" }));
      fireEvent.click(screen.getByRole("combobox"));

      expect(screen.getByRole("option", { name: /Notion/ })).toBeTruthy();
    });

    it("says what to register when no sign-in matches", () => {
      signInMocks();
      mocks.authorizationServers = ["https://auth.example.com"];
      renderSheet();
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );

      expect(
        screen.getByText(/registered with auth\.example\.com yet/),
      ).toBeTruthy();
      expect(screen.queryByRole("combobox")).toBeNull();
    });

    it("tells issuers apart by query and fragment", () => {
      signInMocks();
      mocks.issuers = [
        {
          id: "linear",
          issuer: "https://issuer.example/",
          name: "Tenant-less",
        },
        {
          id: "notion",
          issuer: "https://issuer.example/?tenant=a",
          name: "Tenant A",
        },
      ];
      mocks.authorizationServers = ["https://issuer.example?tenant=a"];
      renderSheet();
      openPicker();

      expect(screen.getByRole("option", { name: /Tenant A/ })).toBeTruthy();
      expect(screen.queryByRole("option", { name: /Tenant-less/ })).toBeNull();
    });

    it("describes every server the upstream names", () => {
      signInMocks();
      mocks.issuers = [
        ...mocks.issuers,
        { id: "other", issuer: "https://other.example", name: "Other" },
      ];
      mocks.registrations = [
        ...mocks.registrations,
        {
          id: "01a17e7e",
          clientId: "other",
          credentialOwner: "subject",
          remoteSessionIssuerId: "other",
        },
      ];
      mocks.authorizationServers = [
        "https://mcp.linear.app",
        "https://mcp.linear.app/",
        " ",
        "https://mcp.notion.com",
      ];
      renderSheet();
      openPicker();

      expect(screen.getByRole("option", { name: /Linear/ })).toBeTruthy();
      expect(screen.getByRole("option", { name: /Notion/ })).toBeTruthy();
      expect(screen.queryByRole("option", { name: /Other/ })).toBeNull();
      expect(
        screen.getByText(
          /Showing sign-ins for mcp\.linear\.app and mcp\.notion\.com,/,
        ),
      ).toBeTruthy();
    });

    it("keeps a sign-in whose authorization server could not be looked up", () => {
      signInMocks();
      mocks.issuers = [mocks.issuers[0]];
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      openPicker();

      expect(screen.getByRole("option", { name: /Linear/ })).toBeTruthy();
      expect(screen.getByRole("option", { name: /oacli_notion/ })).toBeTruthy();
      expect(screen.queryByText(/Sign-ins could not be loaded/)).toBeNull();
    });

    it("hides the narrowing note until the sign-ins are shown", () => {
      signInMocks();
      mocks.issuersLoading = true;
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );

      expect(screen.getByText("Loading sign-ins…")).toBeTruthy();
      expect(screen.queryByText(/Showing sign-ins for/)).toBeNull();
    });

    it("names the registration on the closed picker", () => {
      signInMocks();
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      openPicker();
      fireEvent.click(screen.getByRole("option", { name: /Linear/ }));

      expect(screen.getByRole("combobox").textContent).toContain(
        "mcp.linear.app",
      );
    });

    it("drops a choice the narrower list leaves out", () => {
      signInMocks();
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );
      fireEvent.click(screen.getByRole("button", { name: "Show all" }));
      fireEvent.click(screen.getByRole("combobox"));
      fireEvent.click(screen.getByRole("option", { name: /Notion/ }));
      fireEvent.click(
        screen.getByRole("button", { name: "Show only mcp.linear.app" }),
      );
      fireEvent.click(screen.getByRole("button", { name: "Show all" }));

      expect(screen.getByRole("combobox").textContent).toContain(
        "Choose a sign-in",
      );
    });

    it("treats a blank-only list as naming no server", () => {
      signInMocks();
      mocks.authorizationServers = [" "];
      renderSheet();
      openPicker();

      expect(screen.getByRole("option", { name: /Linear/ })).toBeTruthy();
      expect(screen.getByRole("option", { name: /Notion/ })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Show all" })).toBeNull();
    });

    it("offers every sign-in when the upstream names none", () => {
      signInMocks();
      renderSheet();
      openPicker();

      expect(screen.getByRole("option", { name: /Linear/ })).toBeTruthy();
      expect(screen.getByRole("option", { name: /Notion/ })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Show all" })).toBeNull();
    });

    it("tells unnamed registrations of one server apart", () => {
      signInMocks();
      mocks.registrations = [
        ...mocks.registrations,
        {
          id: "01a16996",
          clientId: "second",
          credentialOwner: "subject",
          remoteSessionIssuerId: "linear",
        },
      ];
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      openPicker();

      expect(screen.getByText("mcp.linear.app · 01a1a312")).toBeTruthy();
      expect(screen.getByText("mcp.linear.app · second")).toBeTruthy();
    });
  });
});
