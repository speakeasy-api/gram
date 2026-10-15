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
  session: {} as Record<string, unknown>,
  save: vi.fn(),
  saveError: null as unknown,
  invalidate: vi.fn(),
  saveResult: { isDefault: false } as { isDefault: boolean },
  speakeasyIssuer: "https://speakeasy.example.com/issuer" as string | undefined,
  signIns: [] as { projectId: string; trustedRemoteSessionIssuerId?: string }[],
  issuers: [] as { id: string; name: string; issuer: string }[],
}));
vi.mock("./EmaServiceServers", () => ({
  EmaServiceServers: () => <h3>Servers</h3>,
}));
vi.mock("./useEmaServices", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./useEmaServices")>()),
  useEmaServices: () => mocks,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org", slug: "example" }),
  useSession: () => mocks.session,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.admin }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: mocks.flag }),
}));
vi.mock("@/lib/featureFlags", () => ({
  FEATURE_FLAGS: { emaProviders: "ema-providers" },
}));
vi.mock("../useEnterpriseIssuer", () => ({
  useEnterpriseIssuer: () => ({
    data: { issuer: mocks.speakeasyIssuer },
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/organizationUserSessionIssuers.js", () => ({
  useOrganizationUserSessionIssuersInfinite: () => ({
    data: { pages: [{ result: { items: mocks.signIns } }] },
    isSuccess: true,
    hasNextPage: false,
    isFetchingNextPage: false,
    isError: false,
    fetchNextPage: () => Promise.resolve(),
  }),
}));
vi.mock("@/lib/remote-identity", () => ({
  useRemoteSessionIssuersByIds: () => ({ items: mocks.issuers }),
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
      return Promise.resolve();
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
      isPending: false,
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
  name: "Each server’s own identity provider",
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
  isDefaultOrder: true,
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
const order = () =>
  within(screen.getByRole("list", { name: "Provider order" }))
    .getAllByRole("listitem")
    .map((item) => item.textContent?.replace(/^\d+/, "").trim());
const saveButton = () =>
  screen.getByRole("button", { name: "Save order" }) as HTMLButtonElement;
const fallbackNote =
  /If Okta can’t or won’t vouch for someone, Speakeasy is tried next/;

beforeEach(() => {
  mocks.rows = [linear, notion];
  mocks.isPending = false;
  mocks.isError = false;
  mocks.admin = true;
  mocks.flag = "enabled";
  mocks.session = {};
  mocks.save.mockReset();
  mocks.saveError = null;
  mocks.invalidate.mockReset();
  mocks.saveResult = { isDefault: false };
  mocks.speakeasyIssuer = "https://speakeasy.example.com/issuer";
  mocks.signIns = [{ projectId: "", trustedRemoteSessionIssuerId: "okta" }];
  mocks.issuers = [
    { id: "okta", name: "Okta", issuer: "https://okta.example.com" },
  ];
});
afterEach(cleanup);

describe("one service", () => {
  it("names the service, how many servers reach it, and the way back", () => {
    show();
    expect(screen.getByRole("heading", { name: "Linear" })).toBeTruthy();
    expect(
      screen.getByText(
        "2 servers reach Linear through Enterprise Managed Authorization.",
      ),
    ).toBeTruthy();
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
    expect(screen.getByText(/In Linear’s admin settings/)).toBeTruthy();
    const rows = trustRows();
    expect(rows[0]?.textContent).toContain(
      "Each server’s own identity provider",
    );
    expect(rows[0]?.textContent).toContain(
      "Its issuer URL is on that server’s Enterprise Managed Authorization settings.",
    );
    expect(rows[1]?.textContent).toContain("Speakeasy");
    expect(rows[1]?.textContent).toContain(
      "for servers whose people sign in with Speakeasy",
    );
    expect(rows[1]?.querySelector("code")?.textContent).toBe(
      "https://speakeasy.example.com/issuer",
    );
  });

  it("explains the default and starts a custom order from it", () => {
    show();
    expect(
      screen.getByText(
        /each server is vouched for by whatever signs its people in/,
        {
          selector: "#order *",
        },
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save order" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Customize order" }));
    expect(order()).toEqual(["Okta", "Speakeasy"]);
    expect(
      screen.getByText(
        /A saved order applies to every server that reaches Linear/,
      ),
    ).toBeTruthy();
    expect(screen.getByText(fallbackNote)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Move Speakeasy up" }));
    expect(order()).toEqual(["Speakeasy", "Okta"]);
    expect(screen.queryByText(fallbackNote)).toBeNull();
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "vendor",
          providers: [
            { kind: "speakeasy" },
            { kind: "external", remoteSessionIssuerId: "okta" },
          ],
          expectedProviders: [],
        },
      },
    });
  });

  it("refreshes the services and every server's reading after a save", async () => {
    show();
    fireEvent.click(screen.getByRole("button", { name: "Customize order" }));
    fireEvent.click(saveButton());
    await waitFor(() =>
      expect(mocks.invalidate).toHaveBeenCalledWith({
        queryKey: ["identityChaining", "readEMA"],
      }),
    );
    expect(mocks.invalidate).toHaveBeenCalledWith("services");
  });

  it("restores the default from a saved order", () => {
    mocks.rows = [
      { ...linear, isDefaultOrder: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(screen.getByText("Last worked 1h ago")).toBeTruthy();
    expect(screen.getByText(fallbackNote)).toBeTruthy();
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

  it("still lets an admin restore the default with custom orders turned off", () => {
    mocks.flag = "disabled";
    mocks.rows = [
      { ...linear, isDefaultOrder: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(screen.getByRole("button", { name: "Use default" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit order" })).toBeNull();
    expect(
      screen.getByText(
        /Custom orders aren’t available for your organization yet/,
      ),
    ).toBeTruthy();
  });

  it("caps an order at ten identity providers", () => {
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
    fireEvent.click(screen.getByRole("button", { name: "Customize order" }));
    expect(order()).toHaveLength(10);
    expect(
      screen.getByText("An order holds at most 10 identity providers."),
    ).toBeTruthy();
    expect(
      screen.queryByRole("combobox", { name: "Identity provider to add" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove IdP idp-0" }));
    expect(
      screen.getByRole("combobox", { name: "Identity provider to add" }),
    ).toBeTruthy();
  });

  it("keeps a draft to its own service", () => {
    const { rerender } = show();
    fireEvent.click(screen.getByRole("button", { name: "Customize order" }));
    expect(screen.getByRole("button", { name: "Save order" })).toBeTruthy();
    rerender(tree("https://notion.example.com"));
    expect(screen.queryByRole("button", { name: "Save order" })).toBeNull();
    expect(
      screen.getByRole("button", { name: "Customize order" }),
    ).toBeTruthy();
  });

  it("asks to remove a saved provider that's gone before saving", () => {
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
      { ...linear, isDefaultOrder: false, providers: [gone, speakeasy] },
    ];
    show();
    expect(
      screen.getByText("No longer available. Remove it to save."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Edit order" }));
    fireEvent.click(screen.getByRole("button", { name: "Move Speakeasy up" }));
    expect(saveButton().disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Remove Old IdP" }));
    expect(saveButton().disabled).toBe(false);
  });

  it("keeps the order read-only for people who can't change it", () => {
    mocks.admin = false;
    show();
    expect(
      screen.getByText("Only organization admins can change the order."),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Customize order" }),
    ).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
  });

  it("says why a support session can't change the order", () => {
    mocks.session = { impersonatorEmail: "staff@example.com" };
    mocks.rows = [
      { ...linear, isDefaultOrder: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(
      screen.getByText(
        /can’t be changed from a support or impersonated session/,
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Use default" })).toBeNull();
  });

  it("says when no server reaches the service any more", () => {
    mocks.rows = [];
    show();
    expect(
      screen.getByText(/No server reaches this service any more/),
    ).toBeTruthy();
  });
  it("stops a stale draft when the saved order changes underneath it", () => {
    mocks.rows = [
      { ...linear, isDefaultOrder: false, providers: [okta, speakeasy] },
    ];
    const { rerender } = show();
    fireEvent.click(screen.getByRole("button", { name: "Edit order" }));
    fireEvent.click(screen.getByRole("button", { name: "Move Speakeasy up" }));
    expect(saveButton().disabled).toBe(false);

    // Another admin removes Speakeasy.
    mocks.rows = [{ ...linear, isDefaultOrder: false, providers: [okta] }];
    rerender(tree());
    expect(
      screen.getByText(/This order changed since you started editing/),
    ).toBeTruthy();
    expect(screen.getByText(/It’s now: Okta\./)).toBeTruthy();
    expect(saveButton().disabled).toBe(true);

    fireEvent.click(
      screen.getByRole("button", { name: "Review and keep editing" }),
    );
    expect(saveButton().disabled).toBe(false);
    fireEvent.click(saveButton());
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "vendor",
          providers: [
            { kind: "speakeasy" },
            { kind: "external", remoteSessionIssuerId: "okta" },
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
      { ...linear, isDefaultOrder: false, providers: [okta, speakeasy] },
    ];
    const { rerender } = show();
    fireEvent.click(screen.getByRole("button", { name: "Edit order" }));
    mocks.rows = [{ ...linear, isDefaultOrder: false, providers: [okta] }];
    rerender(tree());
    fireEvent.click(screen.getByRole("button", { name: "Discard my changes" }));
    expect(screen.queryByRole("button", { name: "Save order" })).toBeNull();
    expect(screen.getByRole("button", { name: "Edit order" })).toBeTruthy();
  });

  it("says the order changed when the server refuses a stale save", () => {
    show();
    fireEvent.click(screen.getByRole("button", { name: "Customize order" }));
    mocks.saveError = new GramError("conflict", {
      response: new Response("{}", { status: 409 }),
      request: new Request("https://example.com"),
      body: "{}",
    });
    fireEvent.click(saveButton());
    expect(mocks.invalidate).toHaveBeenCalledWith("services");
    expect(
      screen.getByText(/This order changed since you started editing/),
    ).toBeTruthy();
    expect(saveButton().disabled).toBe(true);
  });

  it("keeps a temporarily disabled Speakeasy saveable, with a note", () => {
    const paused = view({
      usable: false,
      unavailable: "Not set up",
      unavailableReason: "disabled",
    });
    mocks.rows = [
      { ...linear, isDefaultOrder: false, providers: [paused, okta] },
    ];
    show();
    expect(
      screen.queryByText("No longer available. Remove it to save."),
    ).toBeNull();
    expect(
      screen.getByText(/Not set up\. It’s skipped until it can vouch again\./),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Edit order" }));
    fireEvent.click(screen.getByRole("button", { name: "Move Okta up" }));
    expect(saveButton().disabled).toBe(false);
  });
});
