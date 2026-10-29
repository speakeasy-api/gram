import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { EmaServiceWorkspace } from "./EmaServiceWorkspace";
import type { ProviderView } from "./emaServiceView";
import type { ServiceRow } from "./useEmaServices";

const mocks = vi.hoisted(() => ({
  rows: [] as unknown[],
  isPending: false,
  isError: false,
  isFetching: false,
  refetch: vi.fn(),
  admin: true,
  flag: "enabled",
  flags: {} as Record<string, string>,
  session: {} as Record<string, unknown>,
  save: vi.fn(),
  saveError: null as unknown,
  invalidate: vi.fn(),
  servicesRefresh: null as Promise<void> | null,
  saveResult: { isDefault: false } as { isDefault: boolean },
  speakeasyIssuer: "https://speakeasy.example.com/issuer" as string | undefined,
  signIns: [] as { projectId: string; trustedRemoteSessionIssuerId?: string }[],
  signInsState: "success" as "success" | "pending" | "error",
  signInsRefetch: vi.fn(),
  issuers: [] as { id: string; name: string; issuer: string }[],
  speakeasyError: false,
  speakeasyRefetch: vi.fn(),
  oktaIssuerId: undefined as string | undefined,
  orgId: "org",
  savePending: false,
  issuersState: "success" as "success" | "loading" | "error",
  issuersRefetch: vi.fn(),
}));
vi.mock("./EmaServiceServers", () => ({
  EmaServiceServers: () => <h3>Affected servers</h3>,
}));
vi.mock("./useEmaServices", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./useEmaServices")>()),
  useEmaServices: () => mocks,
  useOktaIssuerId: () => mocks.oktaIssuerId,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.orgId, slug: "example" }),
  useSession: () => mocks.session,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.admin }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: (key: string) => ({ status: mocks.flags[key] ?? mocks.flag }),
}));
vi.mock("@/lib/featureFlags", () => ({
  FEATURE_FLAGS: {
    emaProviders: "ema-providers",
    oktaConnections: "okta-connections",
  },
}));
vi.mock("../useEnterpriseIssuer", () => ({
  useEnterpriseIssuer: () => ({
    data: mocks.speakeasyError ? undefined : { issuer: mocks.speakeasyIssuer },
    isPending: false,
    isError: mocks.speakeasyError,
    refetch: mocks.speakeasyRefetch,
  }),
}));
vi.mock("@gram/client/react-query/organizationUserSessionIssuers.js", () => ({
  useOrganizationUserSessionIssuersInfinite: () => ({
    data:
      mocks.signInsState === "success"
        ? { pages: [{ result: { items: mocks.signIns } }] }
        : undefined,
    isSuccess: mocks.signInsState === "success",
    hasNextPage: false,
    isFetchingNextPage: false,
    isError: mocks.signInsState === "error",
    refetch: mocks.signInsRefetch,
    fetchNextPage: () => Promise.resolve(),
  }),
}));
vi.mock("@/lib/remote-identity", () => ({
  useRemoteSessionIssuersByIds: () => ({
    items: mocks.issuersState === "success" ? mocks.issuers : [],
    isLoading: mocks.issuersState === "loading",
    isError: mocks.issuersState === "error",
    refetch: mocks.issuersRefetch,
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    invalidateQueries: (filters: unknown) => {
      mocks.invalidate(filters);
      return Promise.resolve();
    },
  }),
}));
vi.mock(
  "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js",
  () => ({
    invalidateAllOrganizationRemoteSessionIssuerEMAServices: () => {
      mocks.invalidate("services");
      return mocks.servicesRefresh ?? Promise.resolve();
    },
  }),
);
vi.mock(
  "@gram/client/react-query/setOrganizationRemoteSessionIssuerEMAProviders.js",
  () => ({
    useSetOrganizationRemoteSessionIssuerEMAProvidersMutation: (options: {
      onSuccess: (result: unknown) => unknown;
      onError: (error: unknown) => unknown;
    }) => ({
      isPending: mocks.savePending,
      error: mocks.saveError,
      reset: () => {
        mocks.saveError = null;
      },
      mutate: (variables: unknown) => {
        mocks.save(variables);
        if (mocks.saveError) {
          void options.onError(mocks.saveError);
          return;
        }
        void options.onSuccess(mocks.saveResult);
      },
    }),
  }),
);
vi.mock("sonner", () => ({ toast: { success: vi.fn(), warning: vi.fn() } }));

const view = (overrides: Partial<ProviderView>): ProviderView => ({
  key: "speakeasy",
  kind: "speakeasy",
  name: "Speakeasy",
  placeholder: false,
  usable: true,
  unavailable: "",
  unusedBySignIn: false,
  ...overrides,
});
const speakeasy = view({ issuerUrl: "https://speakeasy.example.com/issuer" });
const placeholder = view({
  key: "external-default",
  kind: "external",
  name: "Your identity providers",
  placeholder: true,
});
const okta = view({
  key: "external-okta",
  kind: "external",
  name: "Okta",
  remoteSessionIssuerId: "okta",
  issuerUrl: "https://okta.example.com",
  lastAcceptedAt: new Date(Date.now() - 60 * 60 * 1000),
});
const linear: ServiceRow = {
  key: "https://linear.example.com",
  vendorIssuer: "https://linear.example.com",
  name: "Linear",
  remoteSessionIssuerId: "vendor",
  isDefault: true,
  providers: [placeholder, speakeasy],
  serverCount: 2,
  readyServerCount: 2,
  servers: [],
  hiddenServerCount: 0,
};
const notion: ServiceRow = {
  ...linear,
  key: "https://notion.example.com",
  vendorIssuer: "https://notion.example.com",
  name: "Notion",
  remoteSessionIssuerId: "notion-vendor",
};

function tree(vendorIssuer = "https://linear.example.com") {
  return (
    <MemoryRouter>
      <TooltipProvider>
        <EmaServiceWorkspace vendorIssuer={vendorIssuer} />
      </TooltipProvider>
    </MemoryRouter>
  );
}
const show = (vendorIssuer?: string) => render(tree(vendorIssuer));
const trustRows = () =>
  within(screen.getByRole("list", { name: "Linear trusts" })).getAllByRole(
    "listitem",
  );
const checkboxes = () =>
  within(screen.getByRole("list", { name: "Identity providers to enable" }))
    .getAllByRole("checkbox")
    .map((box) => ({
      name: box.closest("li")?.querySelector("label")?.textContent,
      checked: box.getAttribute("aria-checked") === "true",
    }));
const accepted = () =>
  within(screen.getByRole("list", { name: "Enabled identity providers" }))
    .getAllByRole("listitem")
    .map((item) => item.textContent);
const box = (name: string) => screen.getByRole("checkbox", { name });
const editButton = () =>
  screen.getByRole("button", { name: "Edit" }) as HTMLButtonElement;
const saveButton = () =>
  screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
const conflict = () =>
  new GramError("conflict", {
    response: new Response("{}", { status: 409 }),
    request: new Request("https://example.com"),
    body: "{}",
  });
const strandedNote = /nobody to vouch/;
const oktaServer = {
  id: "server-okta",
  name: "Linear (Okta team)",
  slug: "linear-okta",
  projectId: "project",
  projectSlug: "default",
  projectName: "Default",
  status: "working" as const,
  signInIssuerId: "okta",
  signInIssuerName: "Okta",
};
const speakeasyServer = {
  ...oktaServer,
  id: "server-speakeasy",
  name: "Linear (Speakeasy)",
  slug: "linear-speakeasy",
  signInIssuerId: undefined,
  signInIssuerName: undefined,
};

beforeEach(() => {
  mocks.rows = [linear, notion];
  mocks.isPending = false;
  mocks.isError = false;
  mocks.isFetching = false;
  mocks.refetch.mockReset();
  mocks.admin = true;
  mocks.flag = "enabled";
  mocks.flags = {};
  mocks.session = {};
  mocks.save.mockReset();
  mocks.saveError = null;
  mocks.invalidate.mockReset();
  mocks.servicesRefresh = null;
  mocks.saveResult = { isDefault: false };
  mocks.speakeasyIssuer = "https://speakeasy.example.com/issuer";
  mocks.signIns = [{ projectId: "", trustedRemoteSessionIssuerId: "okta" }];
  mocks.signInsState = "success";
  mocks.signInsRefetch.mockReset();
  mocks.issuers = [
    { id: "okta", name: "Okta", issuer: "https://okta.example.com" },
  ];
  mocks.speakeasyError = false;
  mocks.speakeasyRefetch.mockReset();
  mocks.oktaIssuerId = "okta";
  mocks.orgId = "org";
  mocks.savePending = false;
  mocks.issuersState = "success";
  mocks.issuersRefetch.mockReset();
});
afterEach(cleanup);

describe("one service", () => {
  it("offers a retry when the services can't load", () => {
    mocks.isError = true;
    mocks.rows = [];
    const { rerender } = show();
    expect(
      screen.getByText("Couldn’t load this service. Try again."),
    ).toBeTruthy();
    const retry = () =>
      screen.getByRole<HTMLButtonElement>("button", { name: "Retry" });
    expect(retry().disabled).toBe(false);
    fireEvent.click(retry());
    expect(mocks.refetch).toHaveBeenCalledTimes(1);
    mocks.isFetching = true;
    rerender(tree());
    expect(retry().disabled).toBe(true);
  });

  it("names the service and the way back, leaving the count to the server list", () => {
    show();
    expect(screen.getByRole("heading", { name: "Linear" })).toBeTruthy();
    expect(screen.queryByText(/reach Linear through/)).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "All identity providers" })
        .getAttribute("href"),
    ).toBe("/?tab=identity-providers");
  });

  it("shows a placeholder heading, not the raw issuer URL, while loading", () => {
    mocks.isPending = true;
    mocks.rows = [];
    show();
    expect(screen.queryByRole("heading")).toBeNull();
    expect(screen.queryByText(/linear\.example\.com/)).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("Loading service…");
  });

  it("gives the issuer URL to paste once per identity provider, and says who the default entries cover", () => {
    show();
    expect(
      screen.getByText(/Set this up once in Linear for each identity provider/),
    ).toBeTruthy();
    const rows = trustRows();
    expect(rows[0]?.textContent).toContain("Your identity providers");
    expect(rows[0]?.textContent).toContain(
      "Set this up in that identity provider’s Cross App Access.",
    );
    expect(rows[1]?.textContent).toContain("Speakeasy");
    expect(rows[1]?.textContent).toContain(
      "for servers with Speakeasy sign-in",
    );
    expect(rows[1]?.querySelector("code")?.textContent).toBe(
      "https://speakeasy.example.com/issuer",
    );
  });

  it("shows the default as every identity provider accepted, with no warnings", () => {
    mocks.rows = [{ ...linear, servers: [oktaServer, speakeasyServer] }];
    show();
    expect(
      screen.getByRole("heading", {
        name: "Enabled identity providers for Linear",
      }),
    ).toBeTruthy();
    expect(
      screen.getByText("Default: all identity providers enabled"),
    ).toBeTruthy();
    expect(accepted()).toEqual(["OktaEnabled", "SpeakeasyEnabled"]);
    expect(screen.queryByText(strandedNote)).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Move/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Use default" })).toBeNull();
    expect(document.querySelector("#accepted-providers .skeleton")).toBeNull();
  });

  it("edits the default as checkboxes and saves the checked ones in candidate order", () => {
    show();
    fireEvent.click(editButton());
    expect(checkboxes()).toEqual([
      { name: "Okta", checked: true },
      { name: "Speakeasy", checked: true },
    ]);
    expect(screen.queryByText(/^\d+$/)).toBeNull();
    fireEvent.click(box("Okta"));
    fireEvent.click(box("Okta"));
    expect(saveButton().disabled).toBe(false);
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "vendor",
          providers: [
            { kind: "external", remoteSessionIssuerId: "okta" },
            { kind: "speakeasy" },
          ],
          expectedProviders: [],
        },
      },
    });
  });

  it("refreshes the services and every server's reading after a save", async () => {
    show();
    fireEvent.click(editButton());
    fireEvent.click(saveButton());
    await waitFor(() =>
      expect(mocks.invalidate).toHaveBeenCalledWith({
        queryKey: ["identityChaining", "readEMA"],
      }),
    );
    expect(mocks.invalidate).toHaveBeenCalledWith("services");
  });

  it("names the servers an unchecked provider leaves with nobody to vouch, without blocking Save", () => {
    mocks.rows = [{ ...linear, servers: [oktaServer, speakeasyServer] }];
    show();
    fireEvent.click(editButton());
    expect(screen.queryByText(strandedNote)).toBeNull();
    fireEvent.click(box("Okta"));
    expect(
      screen.getByText(
        "Linear (Okta team) would lose Enterprise Managed Authorization, because it uses Okta sign-in.",
      ),
    ).toBeTruthy();
    expect(saveButton().disabled).toBe(false);
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          setEMAProvidersRequestBody: expect.objectContaining({
            providers: [{ kind: "speakeasy" }],
          }),
        }),
      }),
    );
  });

  it("disables Save with nothing checked", () => {
    mocks.rows = [{ ...linear, servers: [oktaServer, speakeasyServer] }];
    show();
    fireEvent.click(editButton());
    fireEvent.click(box("Okta"));
    fireEvent.click(box("Speakeasy"));
    expect(saveButton().disabled).toBe(true);
    expect(
      screen.getByText("Enable at least one identity provider."),
    ).toBeTruthy();
  });

  it("names the servers a saved choice leaves with nobody to vouch", () => {
    mocks.rows = [
      {
        ...linear,
        isDefault: false,
        providers: [okta],
        servers: [oktaServer, speakeasyServer],
      },
    ];
    show();
    expect(accepted()).toEqual(["OktaEnabled", "SpeakeasyNot enabled"]);
    expect(
      screen.getByText(
        "Linear (Speakeasy) can’t use Enterprise Managed Authorization, because it uses Speakeasy sign-in. Enable Speakeasy, or go back to the default.",
      ),
    ).toBeTruthy();
  });

  it("restores the default from a saved choice", () => {
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(screen.getByText("Last worked 1h ago")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Use default" }));
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "vendor",
          providers: [],
          expectedProviders: [
            { kind: "external", remoteSessionIssuerId: "okta" },
            { kind: "speakeasy" },
          ],
        },
      },
    });
  });

  it("keeps the saved positions as the expected list, whatever order it was saved in", () => {
    mocks.rows = [
      { ...linear, isDefault: false, providers: [speakeasy, okta] },
    ];
    show();
    fireEvent.click(editButton());
    expect(saveButton().disabled).toBe(true);
    fireEvent.click(box("Speakeasy"));
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "vendor",
          providers: [{ kind: "external", remoteSessionIssuerId: "okta" }],
          expectedProviders: [
            { kind: "speakeasy" },
            { kind: "external", remoteSessionIssuerId: "okta" },
          ],
        },
      },
    });
  });

  it("still lets an admin restore the default when choosing is turned off", () => {
    mocks.flag = "disabled";
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(screen.getByRole("button", { name: "Use default" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(
      screen.getByText(
        /Choosing enabled identity providers isn’t available for your organization yet/,
      ),
    ).toBeTruthy();
  });

  it("caps the accepted providers at ten", () => {
    const ids = Array.from({ length: 11 }, (_, i) => `idp-${i}`);
    mocks.signIns = ids.map((id) => ({
      projectId: "",
      trustedRemoteSessionIssuerId: id,
    }));
    mocks.issuers = ids.map((id) => ({
      id,
      name: `IdP ${id}`,
      issuer: `https://${id}.example.com`,
    }));
    show();
    fireEvent.click(editButton());
    expect(checkboxes()).toHaveLength(12);
    expect(saveButton().disabled).toBe(true);
    expect(
      screen.getByText("You can enable up to 10 identity providers."),
    ).toBeTruthy();
    fireEvent.click(box("IdP idp-0"));
    fireEvent.click(box("Speakeasy"));
    expect(saveButton().disabled).toBe(false);
  });

  it("keeps a draft to its own service", () => {
    const { rerender } = show();
    fireEvent.click(editButton());
    expect(saveButton()).toBeTruthy();
    rerender(tree("https://notion.example.com"));
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(editButton()).toBeTruthy();
  });

  it("asks to uncheck a saved provider that's gone before saving", () => {
    const gone = view({
      key: "external-gone",
      kind: "external",
      name: "Old IdP",
      remoteSessionIssuerId: "gone",
      usable: false,
      unavailable: "No longer available",
      unavailableReason: "deleted",
    });
    mocks.rows = [
      { ...linear, isDefault: false, providers: [gone, speakeasy] },
    ];
    show();
    expect(
      screen.getByText("No longer available. Uncheck it to save."),
    ).toBeTruthy();
    fireEvent.click(editButton());
    expect(checkboxes()).toEqual([
      { name: "Okta", checked: false },
      { name: "Speakeasy", checked: true },
      { name: "Old IdP", checked: true },
    ]);
    fireEvent.click(box("Okta"));
    expect(saveButton().disabled).toBe(true);
    fireEvent.click(box("Old IdP"));
    expect(saveButton().disabled).toBe(false);
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          setEMAProvidersRequestBody: expect.objectContaining({
            providers: [
              { kind: "external", remoteSessionIssuerId: "okta" },
              { kind: "speakeasy" },
            ],
          }),
        }),
      }),
    );
  });

  it("keeps the choice read-only for people who can't change it", () => {
    mocks.admin = false;
    show();
    expect(
      screen.getByText(
        "Only organization admins can change which identity providers are enabled for a service.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(accepted()).toEqual([
      "Your identity providersEnabled",
      "SpeakeasyEnabled",
    ]);
  });

  it("says why a support session can't change it", () => {
    mocks.session = { impersonatorEmail: "staff@example.com" };
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(
      screen.getByText(
        /can’t change this from a support or impersonated session/,
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Use default" })).toBeNull();
  });

  it("says when no server reaches the service any more", () => {
    mocks.rows = [];
    show();
    expect(
      screen.getByText(/No servers connect to this service anymore/),
    ).toBeTruthy();
  });
  it("stops a stale draft when the saved choice changes underneath it", () => {
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    const { rerender } = show();
    fireEvent.click(editButton());
    fireEvent.click(box("Speakeasy"));
    expect(saveButton().disabled).toBe(false);

    // Another admin unchecks Speakeasy.
    mocks.rows = [{ ...linear, isDefault: false, providers: [okta] }];
    rerender(tree());
    expect(
      screen.getByText(
        /The enabled identity providers for Linear changed since you started editing/,
      ),
    ).toBeTruthy();
    expect(screen.getByText(/Now enabled: Okta\./)).toBeTruthy();
    expect(saveButton().disabled).toBe(true);

    fireEvent.click(
      screen.getByRole("button", { name: "Review and keep editing" }),
    );
    fireEvent.click(box("Speakeasy"));
    expect(saveButton().disabled).toBe(false);
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "vendor",
          providers: [
            { kind: "external", remoteSessionIssuerId: "okta" },
            { kind: "speakeasy" },
          ],
          expectedProviders: [
            { kind: "external", remoteSessionIssuerId: "okta" },
          ],
        },
      },
    });
  });

  it("discards a stale draft on request", () => {
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    const { rerender } = show();
    fireEvent.click(editButton());
    mocks.rows = [{ ...linear, isDefault: false, providers: [okta] }];
    rerender(tree());
    fireEvent.click(screen.getByRole("button", { name: "Discard my changes" }));
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(editButton()).toBeTruthy();
  });

  it("says the choice changed when the server refuses a stale save", () => {
    show();
    fireEvent.click(editButton());
    mocks.saveError = conflict();
    fireEvent.click(saveButton());
    expect(mocks.invalidate).toHaveBeenCalledWith("services");
    // A provider the other admin just set up must be a candidate to keep it.
    expect(mocks.signInsRefetch).toHaveBeenCalled();
    expect(mocks.speakeasyRefetch).toHaveBeenCalled();
    expect(
      screen.getByText(
        /The enabled identity providers for Linear changed since you started editing/,
      ),
    ).toBeTruthy();
    expect(saveButton().disabled).toBe(true);
  });

  it("waits for the current choice before rebasing a refused save", async () => {
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    let refreshed = () => {};
    mocks.servicesRefresh = new Promise<void>((resolve) => {
      refreshed = resolve;
    });
    const { rerender } = show();
    fireEvent.click(editButton());
    fireEvent.click(box("Okta"));
    mocks.saveError = conflict();
    fireEvent.click(saveButton());
    expect(screen.getByText(/Loading the current choice…/)).toBeTruthy();
    const review = () =>
      screen.getByRole<HTMLButtonElement>("button", {
        name: "Review and keep editing",
      });
    expect(review().disabled).toBe(true);

    // Another admin unchecked Speakeasy; the refetch brings that in.
    mocks.rows = [{ ...linear, isDefault: false, providers: [okta] }];
    rerender(tree());
    refreshed();
    await waitFor(() => expect(review().disabled).toBe(false));
    expect(screen.getByText(/Now enabled: Okta\./)).toBeTruthy();
    fireEvent.click(review());
    mocks.saveError = null;
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenLastCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          setEMAProvidersRequestBody: expect.objectContaining({
            providers: [{ kind: "speakeasy" }],
            expectedProviders: [
              { kind: "external", remoteSessionIssuerId: "okta" },
            ],
          }),
        }),
      }),
    );
  });

  it("keeps a temporarily disabled Speakeasy saveable, with a note", () => {
    const paused = view({
      usable: false,
      unavailable: "Not set up",
      unavailableReason: "disabled",
    });
    mocks.rows = [{ ...linear, isDefault: false, providers: [paused] }];
    show();
    expect(
      screen.queryByText("No longer available. Uncheck it to save."),
    ).toBeNull();
    expect(
      screen.getByText(/Not set up\. It’s skipped until it works again\./),
    ).toBeTruthy();
    fireEvent.click(editButton());
    fireEvent.click(box("Okta"));
    expect(saveButton().disabled).toBe(false);
  });

  it("keeps a draft to its own organization", () => {
    const { rerender } = show();
    fireEvent.click(editButton());
    expect(saveButton()).toBeTruthy();
    mocks.orgId = "other-org";
    rerender(tree());
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(editButton()).toBeTruthy();
  });

  it("holds Save while the identity providers it could add reload", () => {
    const { rerender } = show();
    fireEvent.click(editButton());
    fireEvent.click(box("Okta"));
    expect(saveButton().disabled).toBe(false);
    mocks.issuersState = "loading";
    rerender(tree());
    expect(saveButton().disabled).toBe(true);
  });

  it("waits for the identity provider names before editing", () => {
    mocks.issuersState = "loading";
    show();
    expect(editButton().disabled).toBe(true);
    expect(
      screen.getByText(
        "Checking which identity providers you can choose from…",
      ),
    ).toBeTruthy();
  });

  it("offers a retry when the identity provider names fail to load", () => {
    mocks.issuersState = "error";
    show();
    expect(
      screen.getByText(
        /Couldn’t load the identity providers you can choose from/,
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.issuersRefetch).toHaveBeenCalled();
    expect(mocks.signInsRefetch).not.toHaveBeenCalled();
  });

  it("holds every control while a save is in flight", () => {
    const { rerender } = show();
    fireEvent.click(editButton());
    fireEvent.click(box("Okta"));
    mocks.savePending = true;
    rerender(tree());
    const saving = screen.getByRole("button", {
      name: "Saving…",
    }) as HTMLButtonElement;
    expect(saving.disabled).toBe(true);
    expect(
      (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      within(screen.getByRole("list", { name: "Identity providers to enable" }))
        .getAllByRole("checkbox")
        .every((checkbox) => checkbox.hasAttribute("disabled")),
    ).toBe(true);
  });

  it("waits for every identity provider it could add before editing", () => {
    mocks.signInsState = "pending";
    show();
    expect(editButton().disabled).toBe(true);
    expect(
      screen.getByText(
        "Checking which identity providers you can choose from…",
      ),
    ).toBeTruthy();
  });

  it("offers a retry when the identity providers it could add fail to load", () => {
    mocks.signInsState = "error";
    show();
    expect(
      screen.getByText(
        /Couldn’t load the identity providers you can choose from/,
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.signInsRefetch).toHaveBeenCalled();
  });

  it("waits for Speakeasy instead of dropping it when it fails to load", () => {
    mocks.speakeasyError = true;
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(
      screen.queryByText("No longer available. Uncheck it to save."),
    ).toBeNull();
    expect(accepted()).toContain("SpeakeasyEnabled");
    expect(editButton().disabled).toBe(true);
    expect(
      screen.getByText(
        /Couldn’t load the identity providers you can choose from/,
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.speakeasyRefetch).toHaveBeenCalled();
  });

  it("links the Cross App Access step to Okta only for the Okta connection", () => {
    const entra = view({
      key: "external-entra",
      kind: "external",
      name: "Entra",
      remoteSessionIssuerId: "entra",
      issuerUrl: "https://entra.example.com",
    });
    mocks.rows = [
      {
        ...linear,
        isDefault: false,
        providers: [okta, entra, speakeasy, placeholder],
      },
    ];
    show();
    const [oktaRow, entraRow, speakeasyRow, placeholderRow] = trustRows();
    expect(
      within(oktaRow!)
        .getByRole("link", {
          name: "Set up Linear in Okta’s Cross App Access",
        })
        .getAttribute("href"),
    ).toBe("/?tab=identity-providers&provider=okta&view=cross-app-access");
    expect(within(entraRow!).queryByRole("link")).toBeNull();
    expect(
      within(entraRow!).getByText("Set up Linear in Entra’s Cross App Access"),
    ).toBeTruthy();
    expect(within(speakeasyRow!).queryByText(/Cross App Access/)).toBeNull();
    expect(within(placeholderRow!).queryByRole("link")).toBeNull();
  });

  it("shows the fix, not the Cross App Access step, for a provider that can't vouch", () => {
    mocks.rows = [
      {
        ...linear,
        isDefault: false,
        providers: [
          {
            ...okta,
            usable: false,
            unavailable: "No longer available",
            unavailableReason: "deleted",
            fix: "okta",
          },
        ],
      },
    ];
    show();
    const [oktaRow] = trustRows();
    expect(within(oktaRow!).queryByText(/Cross App Access/)).toBeNull();
    expect(
      within(oktaRow!).getByRole("link", { name: "Fix in Okta" }),
    ).toBeTruthy();
  });

  it("doesn't link to Okta when its view isn't available", () => {
    mocks.flags = { "okta-connections": "disabled" };
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(within(trustRows()[0]!).queryByRole("link")).toBeNull();
  });

  it.each([
    [
      "the Okta view",
      "enabled",
      "Fix in Okta",
      "/?tab=identity-providers&provider=okta&view=setup",
    ],
    [
      "identity providers without the Okta view",
      "disabled",
      "Identity providers",
      "/?tab=identity-providers",
    ],
  ])("links a broken Okta provider's fix to %s", (_name, flag, label, href) => {
    mocks.flags = { "okta-connections": flag };
    mocks.rows = [
      {
        ...linear,
        isDefault: false,
        providers: [
          {
            ...okta,
            usable: false,
            unavailable: "No longer available",
            fix: "okta",
          },
          speakeasy,
        ],
      },
    ];
    show();
    expect(
      within(trustRows()[0]!)
        .getByRole("link", { name: label })
        .getAttribute("href"),
    ).toBe(href);
  });

  it("says another admin changed the list when Use default is refused", async () => {
    mocks.rows = [
      { ...linear, isDefault: false, providers: [okta, speakeasy] },
    ];
    show();
    mocks.saveError = conflict();
    fireEvent.click(screen.getByRole("button", { name: "Use default" }));
    expect(mocks.invalidate).toHaveBeenCalledWith("services");
    expect(
      screen.getByText(
        /Another admin changed the enabled identity providers for Linear, so it wasn’t reset to the default\./,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Discard my changes" }),
    ).toBeNull();
    const review = () =>
      screen.getByRole<HTMLButtonElement>("button", { name: "Review" });
    await waitFor(() => expect(review().disabled).toBe(false));
    fireEvent.click(review());
    expect(screen.queryByText(/Another admin changed/)).toBeNull();
  });

  it("says a removed provider cuts off people already connected", () => {
    show();
    fireEvent.click(editButton());
    expect(
      screen.getByText(
        "Applies to each person’s next request. People who log in with a provider you remove will need to sign in to their own Linear account.",
      ),
    ).toBeTruthy();
  });

  it("keeps a saved Speakeasy whose issuer was set up again, and saves it onto the new one", () => {
    const old = view({
      usable: false,
      unavailable: "Not set up",
      unavailableReason: "deleted",
    });
    mocks.rows = [{ ...linear, isDefault: false, providers: [okta, old] }];
    show();
    expect(
      screen.queryByText("No longer available. Uncheck it to save."),
    ).toBeNull();
    expect(
      screen.getByText(
        "Speakeasy was set up again. Save this list again to use it.",
      ),
    ).toBeTruthy();
    fireEvent.click(editButton());
    expect(checkboxes()).toEqual([
      { name: "Okta", checked: true },
      { name: "Speakeasy", checked: true },
    ]);
    expect(saveButton().disabled).toBe(false);
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          setEMAProvidersRequestBody: expect.objectContaining({
            providers: [
              { kind: "external", remoteSessionIssuerId: "okta" },
              { kind: "speakeasy" },
            ],
          }),
        }),
      }),
    );
  });

  it("asks an admin to save a recreated Speakeasy for someone who can't edit", () => {
    const old = view({
      usable: false,
      unavailable: "Not set up",
      unavailableReason: "deleted",
    });
    mocks.admin = false;
    mocks.rows = [{ ...linear, isDefault: false, providers: [okta, old] }];
    show();
    expect(
      screen.getByText(
        "Speakeasy was set up again. An organization admin needs to save this list again to use it.",
      ),
    ).toBeTruthy();
  });
});

describe("unsupported enabled identity providers", () => {
  it("keeps known missing-provider warnings without offering removal in a mixed unsupported list", () => {
    const missing = view({
      key: "external-missing",
      kind: "external",
      name: "Missing provider",
      remoteSessionIssuerId: "missing",
    });
    const unsupported = view({
      key: "unknown-future-provider",
      kind: "future_provider" as ProviderView["kind"],
      name: "Unknown identity provider",
      usable: false,
      unavailable:
        "Unknown availability. Refresh the page to check for an update.",
    });
    mocks.rows = [
      { ...linear, isDefault: false, providers: [missing, unsupported] },
    ];
    show();
    const rows = within(
      screen.getByRole("list", { name: "Enabled identity providers" }),
    );
    expect(rows.getByText("No longer available.")).toBeTruthy();
    expect(rows.getByText(/Unknown availability/)).toBeTruthy();
    expect(rows.queryByText(/Uncheck it to save/)).toBeNull();
    expect(editButton().disabled).toBe(true);
    const restore = screen.getByRole("button", {
      name: "Use default",
    }) as HTMLButtonElement;
    expect(restore.disabled).toBe(true);
    fireEvent.click(editButton());
    fireEvent.click(restore);
    expect(mocks.save).not.toHaveBeenCalled();
  });
  it.each([
    {
      key: "unknown-future-provider",
      kind: "future_provider" as ProviderView["kind"],
    },
    { unavailableReason: "future_reason" as ProviderView["unavailableReason"] },
  ])(
    "blocks edits and restoring defaults for unsupported settings: %j",
    (settings) => {
      mocks.rows = [
        { ...linear, isDefault: false, providers: [view(settings)] },
      ];
      show();
      expect(editButton().disabled).toBe(true);
      const restore = screen.getByRole("button", {
        name: "Use default",
      }) as HTMLButtonElement;
      expect(restore.disabled).toBe(true);
      fireEvent.click(editButton());
      fireEvent.click(restore);
      expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
      expect(
        screen.getByText(/unsupported identity provider configuration/),
      ).toBeTruthy();
      expect(screen.queryByText(/Uncheck it to save/)).toBeNull();
      expect(mocks.save).not.toHaveBeenCalled();
    },
  );

  it("stops a dirty draft when an unsupported provider arrives in a refetch", () => {
    mocks.rows = [{ ...linear, isDefault: false, providers: [speakeasy] }];
    const rendered = show();
    fireEvent.click(editButton());
    fireEvent.click(box("Speakeasy"));
    mocks.rows = [
      {
        ...linear,
        isDefault: false,
        providers: [
          speakeasy,
          view({
            key: "future",
            kind: "future_provider" as ProviderView["kind"],
            name: "Unknown identity provider",
          }),
        ],
      },
    ];
    rendered.rerender(tree());
    expect(saveButton().disabled).toBe(true);
    fireEvent.click(saveButton());
    fireEvent.click(screen.getByRole("button", { name: "Use default" }));
    expect(mocks.save).not.toHaveBeenCalled();
  });

  it("does not let an unsupported default be replaced by the current candidate list", () => {
    mocks.rows = [
      {
        ...linear,
        providers: [
          view({
            kind: "future_provider" as ProviderView["kind"],
            name: "Unknown identity provider",
          }),
        ],
      },
    ];
    show();
    expect(editButton().disabled).toBe(true);
    expect(accepted().join(" ")).toContain("Unknown identity provider");
    expect(mocks.save).not.toHaveBeenCalled();
  });
});
