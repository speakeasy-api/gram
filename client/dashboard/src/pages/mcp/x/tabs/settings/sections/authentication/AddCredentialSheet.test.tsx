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
  createSignIn: vi.fn(),
  createDocumentSignIn: vi.fn(),
  deleteSignIn: vi.fn(),
  pickableIssuers: [] as unknown[],
  createOptions: null as { onError?: (error: Error) => void } | null,
  clientPages: [] as unknown[][],
  registrationsFailed: false,
  registrations: [] as unknown[],
  issuers: [] as unknown[],
  issuersLoading: false,
  issuersFailed: false,
  authorizationServers: undefined as string[] | undefined,
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
  useProjectSlugForRequests: () => "project",
  useSdkClient: () => ({
    remoteSessionClients: {
      create: mocks.createSignIn,
      createCimd: mocks.createDocumentSignIn,
      delete: mocks.deleteSignIn,
    },
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

vi.mock("@/contexts/Auth", () => ({ useIsPlatformAdmin: () => false }));
vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: vi.fn() }),
}));

vi.mock(
  "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js",
  () => ({
    useNewRemoteSessionClientCallbackUrl: () => ({
      data: { callbackUrl: "https://app.example.com/oauth/callback" },
      isError: false,
    }),
  }),
);

vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  invalidateAllRemoteSessionClients: () => Promise.resolve(),
}));

vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuersInfinite: () => ({
    data: { pages: [{ result: { items: mocks.pickableIssuers } }] },
    isFetching: false,
    isError: false,
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(),
  }),
}));

vi.mock("@gram/client/react-query/remoteSessionIssuer.js", () => ({
  useRemoteSessionIssuer: ({ id }: { id: string }) => ({
    data: (mocks.pickableIssuers as { id: string }[]).find(
      (issuer) => issuer.id === id,
    ),
    isError: false,
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
  useCreateAuthClientMutation: (options: {
    onError?: (error: Error) => void;
  }) => {
    mocks.createOptions = options;
    return { mutate: mocks.create, isPending: false };
  },
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
    isError: mocks.issuersFailed,
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
  mocks.issuersFailed = false;
  mocks.authorizationServers = undefined;
  mocks.pickableIssuers = [];
});

describe("AddCredentialSheet", () => {
  it("marks the name as required and explains why an empty form cannot be added", () => {
    mocks.clientPages = [];
    renderSheet();
    fireEvent.click(screen.getByRole("radio", { name: /^Static credential/ }));
    expect(
      screen.getByLabelText("Name (required)").hasAttribute("required"),
    ).toBe(true);
    expect(
      screen.getByText("Give the credential a name people will recognize."),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Add credential" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.change(screen.getByLabelText("Name (required)"), {
      target: { value: "   " },
    });
    expect(
      screen
        .getByRole("button", { name: "Add credential" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByText("Give the credential a name people will recognize."),
    ).toBeTruthy();
  });

  it("waits for reusable credentials before asking for a selection", async () => {
    mocks.clientPages = [[client({ id: "shared", name: "Shared token" })]];
    renderSheet();

    expect(screen.getByText("Loading credentials…")).toBeTruthy();
    expect(screen.queryByText("Choose a credential.")).toBeNull();
    expect(await screen.findByText("Choose a credential.")).toBeTruthy();
    expect(screen.getByLabelText("Credential (required)")).toBeTruthy();
  });

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
      expect(screen.queryByLabelText("Name (required)")).toBeNull();
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
    fireEvent.change(screen.getByLabelText("Name (required)"), {
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
    const picker = () => screen.getByLabelText("Start from (required)");
    const chooseOwnAccount = () => {
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );
      fireEvent.click(
        screen.getByRole("radio", { name: /^New from existing/ }),
      );
    };
    const openPicker = () => {
      chooseOwnAccount();
      fireEvent.click(picker());
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
      chooseOwnAccount();
      fireEvent.click(screen.getByRole("button", { name: "Show all" }));
      fireEvent.click(picker());

      expect(screen.getByRole("option", { name: /Notion/ })).toBeTruthy();
    });

    it("says what to register when no sign-in matches", () => {
      signInMocks();
      mocks.authorizationServers = ["https://auth.example.com"];
      renderSheet();
      chooseOwnAccount();

      expect(
        screen.getByText(
          /with auth\.example\.com can be used as a starting point yet/,
        ),
      ).toBeTruthy();
      expect(screen.queryByLabelText("Start from (required)")).toBeNull();
      expect(screen.queryByText(/Showing sign-ins for/)).toBeNull();
      expect(screen.getByRole("button", { name: "Show all" })).toBeTruthy();
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

    it("shows matches after a failed lookup, the rest behind Show all", () => {
      signInMocks();
      mocks.issuers = [mocks.issuers[0]];
      mocks.issuersFailed = true;
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      openPicker();

      expect(screen.queryByText(/Sign-ins could not be loaded/)).toBeNull();
      expect(screen.queryByRole("option", { name: /oacli_notion/ })).toBeNull();
      fireEvent.click(screen.getByRole("option", { name: /Linear/ }));

      fireEvent.click(screen.getByRole("button", { name: "Show all" }));
      fireEvent.click(picker());
      expect(screen.getByRole("option", { name: /oacli_notion/ })).toBeTruthy();
    });

    it("says a failed lookup failed when nothing matches", () => {
      signInMocks();
      mocks.issuers = [];
      mocks.issuersFailed = true;
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      chooseOwnAccount();

      expect(screen.getByText(/Sign-ins could not be loaded/)).toBeTruthy();
      expect(screen.queryByText(/can be used as a starting point/)).toBeNull();
      expect(screen.queryByRole("button", { name: "Show all" })).toBeNull();
    });

    it("lets no hidden selection through when a lookup then fails", () => {
      signInMocks();
      mocks.authorizationServers = ["https://auth.example.com"];
      renderSheet();
      chooseOwnAccount();
      fireEvent.click(screen.getByRole("button", { name: "Show all" }));
      fireEvent.click(picker());
      fireEvent.click(screen.getByRole("option", { name: /Linear/ }));
      fireEvent.change(screen.getByLabelText("Name (required)"), {
        target: { value: "Team sign-in" },
      });
      expect(
        screen
          .getByRole("button", { name: "Add credential" })
          .hasAttribute("disabled"),
      ).toBe(false);

      mocks.issuersFailed = true;
      fireEvent.change(screen.getByLabelText("Name (required)"), {
        target: { value: "Team sign-ins" },
      });

      expect(screen.getByText(/Sign-ins could not be loaded/)).toBeTruthy();
      expect(
        screen
          .getByRole("button", { name: "Add credential" })
          .hasAttribute("disabled"),
      ).toBe(true);
    });

    it("lists everything after a failed lookup when the upstream names none", () => {
      signInMocks();
      mocks.issuers = [];
      mocks.issuersFailed = true;
      renderSheet();
      openPicker();

      expect(screen.queryByText(/Sign-ins could not be loaded/)).toBeNull();
      expect(screen.getByRole("option", { name: /01a1a312/ })).toBeTruthy();
      expect(screen.getByRole("option", { name: /oacli_notion/ })).toBeTruthy();
    });

    it("hides the narrowing note until the sign-ins are shown", () => {
      signInMocks();
      mocks.issuersLoading = true;
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      chooseOwnAccount();

      expect(screen.getByText("Loading sign-ins…")).toBeTruthy();
      expect(screen.queryByText(/Showing sign-ins for/)).toBeNull();
    });

    it("names the registration on the closed picker", () => {
      signInMocks();
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      openPicker();
      fireEvent.click(screen.getByRole("option", { name: /Linear/ }));

      expect(picker().textContent).toContain("mcp.linear.app");
    });

    describe("Show only", () => {
      const pickFromAll = (option: RegExp) => {
        signInMocks();
        mocks.authorizationServers = ["https://mcp.linear.app"];
        renderSheet();
        chooseOwnAccount();
        fireEvent.click(screen.getByRole("button", { name: "Show all" }));
        fireEvent.click(picker());
        fireEvent.click(screen.getByRole("option", { name: option }));
        fireEvent.click(
          screen.getByRole("button", { name: "Show only mcp.linear.app" }),
        );
      };

      it("keeps a selection in the narrowed list", () => {
        pickFromAll(/Linear/);

        expect(picker().textContent).toContain("Linear");
      });

      it("clears a selection outside the narrowed list for good", () => {
        pickFromAll(/Notion/);
        expect(picker().textContent).toContain("Choose a sign-in");

        fireEvent.click(screen.getByRole("button", { name: "Show all" }));
        expect(picker().textContent).toContain("Choose a sign-in");
      });
    });

    it("offers no toggle once the upstream names nothing", () => {
      signInMocks();
      mocks.authorizationServers = ["https://mcp.linear.app"];
      renderSheet();
      chooseOwnAccount();
      fireEvent.click(screen.getByRole("button", { name: "Show all" }));

      mocks.authorizationServers = undefined;
      fireEvent.change(screen.getByLabelText("Name (required)"), {
        target: { value: "Rerender" },
      });

      expect(picker()).toBeTruthy();
      expect(screen.queryByText(/Showing/)).toBeNull();
      expect(screen.queryByRole("button", { name: /^Show / })).toBeNull();
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

  describe("personal sign-ins are never shared", () => {
    const existingSignIn = {
      id: "stored-sign-in",
      clientId: "linear-app",
      credentialOwner: "subject",
      remoteSessionIssuerId: "linear",
      tokenEndpointAuthMethod: "client_secret_post",
      scope: ["read", "write"],
      audience: "https://api.linear.app",
      projectId: "project-1",
    };
    const made = {
      id: "new-sign-in",
      clientId: "linear-app",
      projectId: "project-1",
    };
    const setup = () => {
      mocks.clientPages = [];
      mocks.issuers = [
        { id: "linear", issuer: "https://mcp.linear.app/", name: "Linear" },
      ];
      mocks.registrations = [existingSignIn];
      mocks.createSignIn.mockResolvedValue(made);
      mocks.createDocumentSignIn.mockResolvedValue(made);
      mocks.deleteSignIn.mockResolvedValue(undefined);
    };
    const startFromExisting = () => {
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );
      fireEvent.click(
        screen.getByRole("radio", { name: /^New from existing/ }),
      );
      fireEvent.click(screen.getByLabelText("Start from (required)"));
      fireEvent.click(screen.getByRole("option", { name: /Linear/ }));
      fireEvent.change(screen.getByLabelText("Name (required)"), {
        target: { value: "Linear for engineering" },
      });
    };
    const add = () =>
      fireEvent.click(screen.getByRole("button", { name: "Add credential" }));

    it("leaves a personal sign-in out of the credentials to share, and keeps the rest", async () => {
      mocks.clientPages = [
        [
          client({
            id: "personal",
            name: "Linear accounts",
            clientType: "remote_session",
          }),
          client({
            id: "service",
            name: "Robot account",
            clientType: "service_account",
          }),
          client({ id: "static", name: "Static token" }),
        ],
      ];
      renderSheet();

      fireEvent.click(await screen.findByRole("combobox"));
      expect(
        screen.queryByRole("option", { name: /Linear accounts/ }),
      ).toBeNull();
      expect(
        screen.getByRole("option", { name: /Robot account/ }),
      ).toBeTruthy();
      expect(screen.getByRole("option", { name: /Static token/ })).toBeTruthy();
    });

    it("says a sign-in started from another is an independent copy", () => {
      setup();
      renderSheet();
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );
      expect(
        screen.getByText(
          "Copy a stored sign-in's settings into an independent one.",
        ),
      ).toBeTruthy();
      fireEvent.click(
        screen.getByRole("radio", { name: /^New from existing/ }),
      );

      expect(
        screen.getByText(
          /independent copy of its settings, not a shared sign-in/,
        ),
      ).toBeTruthy();
    });

    it("fills the form from the stored sign-in and creates a new one for the credential", async () => {
      setup();
      renderSheet();
      startFromExisting();

      expect(
        (screen.getByPlaceholderText("client_abc123") as HTMLInputElement)
          .value,
      ).toBe("linear-app");
      // The stored secret is never fetched or shown: the field starts empty.
      expect(
        (
          screen.getByPlaceholderText(
            "Leave blank to copy the stored secret",
          ) as HTMLInputElement
        ).value,
      ).toBe("");
      expect(
        (screen.getByPlaceholderText("read, write, openid") as HTMLInputElement)
          .value,
      ).toBe("read, write");
      expect(
        (
          screen.getByPlaceholderText(
            "https://api.example.com",
          ) as HTMLInputElement
        ).value,
      ).toBe("https://api.linear.app");

      add();
      await waitFor(() => expect(mocks.create).toHaveBeenCalled());
      expect(mocks.createSignIn).toHaveBeenCalledWith({
        createRemoteSessionClientForm: {
          remoteSessionIssuerId: "linear",
          clientId: "linear-app",
          clientSecret: undefined,
          templateClientId: "stored-sign-in",
          tokenEndpointAuthMethod: "client_secret_post",
          scope: ["read", "write"],
          audience: "https://api.linear.app",
        },
      });
      expect(mocks.create.mock.calls[0]![0]).toEqual({
        request: {
          createAuthClientRequestBody: {
            clientType: "remote_session",
            name: "Linear for engineering",
            projectId: "project-1",
            remoteSessionClientId: "new-sign-in",
            headers: [],
            mcpServerIds: ["this-server"],
          },
        },
      });
      expect(mocks.attach).not.toHaveBeenCalled();
    });

    it("sends reviewed edits and copies no secret for a different client ID", async () => {
      setup();
      renderSheet();
      startFromExisting();
      fireEvent.change(screen.getByPlaceholderText("client_abc123"), {
        target: { value: "another-app" },
      });
      fireEvent.change(
        screen.getByPlaceholderText("Secret for this client ID"),
        {
          target: { value: "typed-secret" },
        },
      );
      fireEvent.change(screen.getByPlaceholderText("read, write, openid"), {
        target: { value: "read" },
      });
      expect(screen.getByText(/no secret is copied/)).toBeTruthy();

      add();
      await waitFor(() => expect(mocks.create).toHaveBeenCalled());
      expect(mocks.createSignIn).toHaveBeenCalledWith({
        createRemoteSessionClientForm: expect.objectContaining({
          clientId: "another-app",
          clientSecret: "typed-secret",
          templateClientId: undefined,
          scope: ["read"],
        }),
      });
    });

    it("registers a new hosted document rather than copying one", async () => {
      setup();
      mocks.issuers = [
        {
          id: "linear",
          issuer: "https://mcp.linear.app/",
          name: "Linear",
          clientIdMetadataDocumentSupported: true,
        },
      ];
      mocks.registrations = [
        {
          ...existingSignIn,
          clientId: "https://app.example.com/oauth/client-metadata/stored",
          clientIdMetadataUri:
            "https://app.example.com/oauth/client-metadata/stored",
          tokenEndpointAuthMethod: "none",
        },
      ];
      renderSheet();
      startFromExisting();
      expect(screen.queryByPlaceholderText("client_abc123")).toBeNull();

      add();
      await waitFor(() => expect(mocks.create).toHaveBeenCalled());
      expect(mocks.createSignIn).not.toHaveBeenCalled();
      expect(mocks.createDocumentSignIn).toHaveBeenCalledWith({
        createCimdForm: {
          remoteSessionIssuerId: "linear",
          scope: ["read", "write"],
          audience: "https://api.linear.app",
        },
      });
    });

    it.each([
      ["a shared credential", { credentialOwner: "self" }],
      ["one the provider rejected", { upstreamRejectedAt: new Date() }],
      [
        "one an identity provider connection manages",
        { identityProviderConnectionId: "connection-1" },
      ],
      [
        "one that signs with a key set",
        { tokenEndpointAuthMethod: "private_key_jwt" },
      ],
      [
        "a hosted document its provider no longer supports",
        {
          clientIdMetadataUri:
            "https://app.example.com/oauth/client-metadata/x",
        },
      ],
    ])("does not offer %s as a starting point", (_name, overrides) => {
      setup();
      mocks.registrations = [{ ...existingSignIn, ...overrides }];
      renderSheet();
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );
      fireEvent.click(
        screen.getByRole("radio", { name: /^New from existing/ }),
      );

      expect(screen.queryByLabelText("Start from (required)")).toBeNull();
      expect(
        screen.getByText(/can be used as a starting point yet/),
      ).toBeTruthy();
    });

    it("reuses the sign-in it made when the credential is saved again, and removes it on cancel", async () => {
      setup();
      renderSheet();
      startFromExisting();

      add();
      await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
      act(() => mocks.createOptions?.onError?.(new Error("save refused")));
      add();
      await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(2));
      expect(mocks.createSignIn).toHaveBeenCalledTimes(1);
      expect(mocks.attach).not.toHaveBeenCalled();

      fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      expect(mocks.deleteSignIn).toHaveBeenCalledWith({ id: "new-sign-in" });
      expect(mocks.deleteSignIn).not.toHaveBeenCalledWith({
        id: "stored-sign-in",
      });
    });

    it("asks for a secret when a method that sends one has none to copy", () => {
      setup();
      renderSheet();
      startFromExisting();
      const submit = () =>
        screen.getByRole("button", { name: "Add credential" });
      expect(submit().hasAttribute("disabled")).toBe(false);

      fireEvent.change(screen.getByPlaceholderText("client_abc123"), {
        target: { value: "another-app" },
      });

      expect(submit().hasAttribute("disabled")).toBe(true);
      expect(screen.getByText(/Enter the client secret/)).toBeTruthy();
    });

    it("creates nothing when the sign-in cannot be made", async () => {
      setup();
      mocks.createSignIn.mockRejectedValue(new Error("template refused"));
      renderSheet();
      startFromExisting();

      add();
      await waitFor(() => expect(mocks.createSignIn).toHaveBeenCalled());
      await waitFor(() =>
        expect(
          screen
            .getByRole("button", { name: "Add credential" })
            .hasAttribute("disabled"),
        ).toBe(false),
      );
      expect(mocks.create).not.toHaveBeenCalled();
      expect(mocks.attach).not.toHaveBeenCalled();
      expect(mocks.deleteSignIn).not.toHaveBeenCalled();
    });

    it("sets up a sign-in from scratch with a chosen authorization server", async () => {
      setup();
      mocks.registrations = [];
      mocks.pickableIssuers = [
        {
          id: "github",
          slug: "github",
          name: "GitHub",
          issuer: "https://github.com",
          projectId: "project-1",
          organizationId: "org-1",
        },
      ];
      renderSheet();
      fireEvent.click(
        screen.getByRole("radio", { name: /^Each person's own account/ }),
      );
      expect(
        screen
          .getByRole("radio", { name: /^New from scratch/ })
          .getAttribute("aria-checked"),
      ).toBe("true");
      fireEvent.change(screen.getByLabelText("Name (required)"), {
        target: { value: "GitHub accounts" },
      });
      expect(
        screen
          .getByRole("button", { name: "Add credential" })
          .hasAttribute("disabled"),
      ).toBe(true);

      fireEvent.click(screen.getByText("Choose an authorization server…"));
      fireEvent.click(screen.getByRole("option", { name: /GitHub/ }));
      fireEvent.change(screen.getByPlaceholderText("client_abc123"), {
        target: { value: "gh-app" },
      });
      add();

      await waitFor(() => expect(mocks.create).toHaveBeenCalled());
      expect(mocks.createSignIn).toHaveBeenCalledWith({
        createRemoteSessionClientForm: expect.objectContaining({
          remoteSessionIssuerId: "github",
          clientId: "gh-app",
        }),
      });
      expect(
        mocks.createSignIn.mock.calls[0]![0].createRemoteSessionClientForm,
      ).not.toHaveProperty("templateClientId");
    });
  });
});
