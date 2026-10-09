import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";

import { OktaSignInSection } from "./OktaSignInSection";
import { makeConnection } from "./testFixtures";

type PageState = {
  pages: unknown[];
  isPending: boolean;
  error: Error | null;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isFetchNextPageError: boolean;
};

const state = vi.hoisted(() => ({
  clients: {} as PageState,
  issuers: {} as PageState,
  fetchClientsPage: vi.fn(() => Promise.resolve()),
  fetchIssuersPage: vi.fn(() => Promise.resolve()),
  setUp: vi.fn(),
  features: { customerManagedEncryptionKeysEnabled: true } as
    | { customerManagedEncryptionKeysEnabled: boolean }
    | undefined,
  keys: [] as unknown[],
  sets: [] as unknown[],
  preflight: vi.fn(),
}));

function infinite(page: PageState, fetchNextPage: () => Promise<unknown>) {
  return {
    data: { pages: page.pages },
    isPending: page.isPending,
    error: page.error,
    hasNextPage: page.hasNextPage,
    isFetchingNextPage: page.isFetchingNextPage,
    isFetchNextPageError: page.isFetchNextPageError,
    fetchNextPage,
  };
}

vi.mock("@gram/client/react-query/organizationRemoteSessionClients.js", () => ({
  invalidateAllOrganizationRemoteSessionClients: vi.fn(),
  useOrganizationRemoteSessionClientsInfinite: () =>
    infinite(state.clients, state.fetchClientsPage),
}));
vi.mock("@gram/client/react-query/organizationUserSessionIssuers.js", () => ({
  invalidateAllOrganizationUserSessionIssuers: vi.fn(),
  useOrganizationUserSessionIssuersInfinite: () =>
    infinite(state.issuers, state.fetchIssuersPage),
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionClient.js", () => ({
  invalidateAllOrganizationRemoteSessionClient: vi.fn(),
}));
vi.mock("@gram/client/react-query/listJsonWebKeySets.js", () => ({
  invalidateAllListJsonWebKeySets: vi.fn(),
  useListJsonWebKeySets: () => ({
    data: { sets: state.sets },
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/listJsonWebKeys.js", () => ({
  invalidateAllListJsonWebKeys: vi.fn(),
  useListJsonWebKeys: () => ({
    data: { keys: state.keys },
    isPending: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({ data: state.features, isLoading: false }),
}));
vi.mock(
  "@gram/client/funcs/organizationUserSessionIssuersGetDeletePreflight.js",
  () => ({
    organizationUserSessionIssuersGetDeletePreflight: state.preflight,
  }),
);
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "organization-id" }),
}));
vi.mock(
  "@gram/client/react-query/createOrganizationUserSessionIssuer.js",
  () => ({
    useCreateOrganizationUserSessionIssuerMutation: () => ({
      mutate: vi.fn(),
      isPending: false,
      error: null,
    }),
  }),
);
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("../../../encryption-keys/jwks/ExternalKeySelect", () => ({
  ExternalKeySelect: () => null,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("./oktaSignIn", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./oktaSignIn")>()),
  setUpOktaSignIn: state.setUp,
}));

function loaded(pages: unknown[]): PageState {
  return {
    pages,
    isPending: false,
    error: null,
    hasNextPage: false,
    isFetchingNextPage: false,
    isFetchNextPageError: false,
  };
}

const issuersPage = { result: { items: [] } };
const clientsPage = { result: { items: [] } };

beforeEach(() => {
  state.clients = loaded([clientsPage]);
  state.issuers = loaded([issuersPage]);
  state.fetchClientsPage.mockClear();
  state.fetchIssuersPage.mockClear();
  state.setUp.mockReset();
  state.setUp.mockResolvedValue({ issuer: { slug: "okta-sign-in" } });
  state.features = { customerManagedEncryptionKeysEnabled: true };
  state.keys = [];
  state.sets = [{ id: "set-1", name: "Set 1", externalKeyId: "key-1" }];
  state.preflight.mockReset();
  state.preflight.mockResolvedValue({
    ok: true,
    value: { mcpServers: [], toolsets: [] },
  });
});
afterEach(cleanup);

const connection = makeConnection({
  status: "verified",
  agentId: "agent-id",
  clientId: "management-client-id",
  remoteSessionIssuerId: "remote-issuer-id",
});

function renderSection() {
  const client = new QueryClient();
  const tree = () => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <OktaSignInSection connection={connection} />
      </TooltipProvider>
    </QueryClientProvider>
  );
  const result = render(tree());
  return { ...result, rerenderSection: () => result.rerender(tree()) };
}

function setUpButton(): HTMLButtonElement {
  return screen.getByRole("button", {
    name: "Set up Okta sign-in",
  }) as HTMLButtonElement;
}

describe("OktaSignInSection", () => {
  it("enables setup once clients and issuers load", () => {
    renderSection();
    expect(setUpButton().disabled).toBe(false);
  });

  it.each([
    ["clients", "clients"],
    ["issuers", "issuers"],
  ] as const)(
    "blocks setup and shows the error when %s fail to load",
    (_, which) => {
      state[which] = {
        ...loaded([]),
        error: new Error(`${which} unavailable`),
      };
      renderSection();
      expect(setUpButton().disabled).toBe(true);
      expect(screen.getByText(`${which} unavailable`)).toBeTruthy();
      expect(screen.queryByText("Not set up")).toBeNull();
      fireEvent.click(setUpButton());
      expect(state.setUp).not.toHaveBeenCalled();
    },
  );

  it("walks every page of clients and issuers", () => {
    const issuerPage = (slug: string) => ({
      result: { items: [{ id: slug, slug, projectId: "" }] },
    });
    const pages = [
      issuerPage("first"),
      issuerPage("second"),
      issuerPage("third"),
    ];
    state.clients = { ...loaded([clientsPage]), hasNextPage: true };
    state.issuers = { ...loaded(pages.slice(0, 1)), hasNextPage: true };
    // Each fetch starts a request; the test settles it below.
    state.fetchClientsPage.mockImplementation(() => {
      state.clients = { ...state.clients, isFetchingNextPage: true };
      return Promise.resolve();
    });
    state.fetchIssuersPage.mockImplementation(() => {
      state.issuers = { ...state.issuers, isFetchingNextPage: true };
      return Promise.resolve();
    });
    const { rerenderSection: again } = renderSection();
    expect(state.fetchClientsPage).toHaveBeenCalledTimes(1);
    expect(state.fetchIssuersPage).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Loading sign-in settings…")).toBeTruthy();

    // In flight: no duplicate fetch.
    again();
    expect(state.fetchIssuersPage).toHaveBeenCalledTimes(1);

    state.clients = loaded([clientsPage, clientsPage]);
    state.issuers = { ...loaded(pages.slice(0, 2)), hasNextPage: true };
    again();
    expect(state.fetchClientsPage).toHaveBeenCalledTimes(1);
    expect(state.fetchIssuersPage).toHaveBeenCalledTimes(2);
    expect(screen.getByText("Loading sign-in settings…")).toBeTruthy();

    state.issuers = loaded(pages);
    again();
    expect(state.fetchIssuersPage).toHaveBeenCalledTimes(2);
    expect(screen.queryByText("Loading sign-in settings…")).toBeNull();
    for (const slug of ["first", "second", "third"]) {
      expect(screen.getByText(slug)).toBeTruthy();
    }
    expect(setUpButton().disabled).toBe(false);
  });

  it("keeps the trust dialog open on failure so a retry trusts the same issuer", async () => {
    const corp = {
      id: "corp-id",
      slug: "corp",
      projectId: "",
      trustedRemoteSessionIssuerId: "other-issuer",
      trustedRemoteSessionClientId: "other-client",
    };
    state.issuers = loaded([{ result: { items: [corp] } }]);
    state.setUp.mockRejectedValue(new Error("trust failed"));
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "Trust Okta sign-in" }));
    const dialog = await screen.findByRole("dialog");
    const confirm = () =>
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Trust Okta sign-in" }),
      );
    confirm();
    expect(await within(dialog).findByText("trust failed")).toBeTruthy();
    expect(screen.getByRole("dialog")).toBe(dialog);

    confirm();
    await waitFor(() => expect(state.setUp).toHaveBeenCalledTimes(2));
    for (const [, input] of state.setUp.mock.calls) {
      expect(input.userSessionIssuer).toBe(corp);
    }

    const cancel = within(dialog).getByRole("button", {
      name: "Cancel",
    }) as HTMLButtonElement;
    await waitFor(() => expect(cancel.disabled).toBe(false));
    fireEvent.click(cancel);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByText("trust failed")).toBeTruthy();
  });

  it("stops paging after a failed page and surfaces the error", () => {
    state.issuers = {
      ...loaded([issuersPage]),
      hasNextPage: true,
      isFetchNextPageError: true,
      error: new Error("page two failed"),
    };
    renderSection();
    expect(state.fetchIssuersPage).not.toHaveBeenCalled();
    expect(screen.queryByText("Loading sign-in settings…")).toBeNull();
    expect(screen.getByText("page two failed")).toBeTruthy();
    expect(setUpButton().disabled).toBe(true);
  });

  it("lists the servers and toolsets on the issuer before trusting it", async () => {
    const plain = { id: "plain-id", slug: "plain", projectId: "" };
    state.issuers = loaded([{ result: { items: [plain] } }]);
    state.preflight.mockResolvedValue({
      ok: true,
      value: {
        mcpServers: [],
        toolsets: [
          {
            id: "t",
            name: "Billing tools",
            projectId: "p",
            projectName: "Ops",
          },
        ],
      },
    });
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "Trust Okta sign-in" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText("Toolset: Billing tools (Ops)"),
    ).toBeTruthy();
    expect(state.preflight.mock.calls[0]?.[1]).toEqual({ id: "plain-id" });
    expect(state.setUp).not.toHaveBeenCalled();
  });

  it("asks before trusting when the owner lookup fails", async () => {
    const plain = { id: "plain-id", slug: "plain", projectId: "" };
    state.issuers = loaded([{ result: { items: [plain] } }]);
    state.preflight.mockResolvedValue({ ok: false, error: new Error("nope") });
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "Trust Okta sign-in" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(/Could not check which servers and toolsets/),
    ).toBeTruthy();
  });

  it("blocks setup when several clients claim the agent ID", () => {
    const duplicate = (id: string) => ({
      client: { id, clientId: "agent-id", projectId: "" },
    });
    state.clients = loaded([
      { result: { items: [duplicate("a"), duplicate("b")] } },
    ]);
    renderSection();

    expect(
      screen.getByText(/2 organization sign-in clients use the agent ID/),
    ).toBeTruthy();
    expect(setUpButton().disabled).toBe(true);
  });

  it("lets an administrator select the previous agent's signing key set among available sets", async () => {
    state.sets = [
      {
        id: "other-set",
        name: "Other signing set",
        externalKeyId: "other-key",
      },
      {
        id: "set-1",
        name: "Previous agent signing set",
        externalKeyId: "key-1",
      },
      {
        id: "managed-set",
        name: "Connection-managed set",
        externalKeyId: "managed-key",
      },
    ];
    state.clients = loaded([
      {
        result: {
          items: [
            {
              client: {
                id: "managed",
                clientId: "management-client-id",
                projectId: "",
                jsonWebKeySetId: "managed-set",
              },
            },
            {
              client: {
                id: "previous-sign-in",
                clientId: "previous-agent-id",
                projectId: "",
                jsonWebKeySetId: "set-1",
              },
            },
          ],
        },
      },
    ]);
    renderSection();

    expect(setUpButton().disabled).toBe(false);
    // Reuse is an administrator choice, not an automatic preference for old keys.
    const picker = screen.getByRole("combobox");
    expect(picker.textContent).toContain("Other signing set");
    fireEvent.keyDown(picker, { key: "ArrowDown" });
    expect(
      screen.queryByRole("option", { name: "Connection-managed set" }),
    ).toBeNull();
    fireEvent.click(
      screen.getByRole("option", { name: "Previous agent signing set" }),
    );
    expect(picker.textContent).toContain("Previous agent signing set");
    fireEvent.click(setUpButton());
    await waitFor(() =>
      expect(state.setUp).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({
          agentId: "agent-id",
          keySet: { kind: "existing", setId: "set-1" },
        }),
      ),
    );
  });

  it("blocks setup without customer-managed keys", () => {
    state.features = { customerManagedEncryptionKeysEnabled: false };
    renderSection();

    expect(screen.getByText(/customer-managed encryption keys/)).toBeTruthy();
    expect(screen.getByText(/Google Cloud KMS/)).toBeTruthy();
    expect(setUpButton().disabled).toBe(true);
  });

  it("shows the active public key to paste into Okta", () => {
    state.clients = loaded([
      {
        result: {
          items: [
            {
              client: {
                id: "sign-in",
                clientId: "agent-id",
                projectId: "",
                jsonWebKeySetId: "set-1",
              },
            },
          ],
        },
      },
    ]);
    state.keys = [
      {
        keyState: "active",
        publicJwk: {
          kid: "kid-1",
          kty: "EC",
          crv: "P-256",
          x: "x",
          y: "y",
          d: "secret",
        },
      },
    ];
    renderSection();

    const pre = screen.getByText(/"kid": "kid-1"/);
    expect(pre.textContent).toContain('"use": "sig"');
    expect(pre.textContent).not.toContain("secret");
    expect(
      screen.getByText(/paste this key in the agent's Credentials and click/),
    ).toBeTruthy();
    expect(screen.getByText(/registering the new public key/)).toBeTruthy();
  });

  it("warns about issuers trusting a previous agent's client", () => {
    state.clients = loaded([
      {
        result: {
          items: [
            {
              client: { id: "previous", clientId: "old-agent", projectId: "" },
            },
          ],
        },
      },
    ]);
    state.issuers = loaded([
      {
        result: {
          items: [
            {
              id: "old",
              slug: "okta-sign-in",
              projectId: "",
              trustedRemoteSessionIssuerId: "remote-issuer-id",
              trustedRemoteSessionClientId: "previous",
            },
          ],
        },
      },
    ]);
    renderSection();

    expect(
      screen.getByText(
        /still trusts the sign-in client of a previous agent ID/,
      ),
    ).toBeTruthy();
    expect(screen.getByText("Trusts a previous agent")).toBeTruthy();
  });
});
