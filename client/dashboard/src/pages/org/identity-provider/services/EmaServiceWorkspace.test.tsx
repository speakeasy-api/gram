import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { EmaServiceWorkspace } from "./EmaServiceWorkspace";
import type { ServiceRow } from "./useEmaServices";

const mocks = vi.hoisted(() => ({
  rows: [] as unknown[],
  isPending: false,
  isError: false,
  isFetching: false,
  refetch: vi.fn(),
  admin: true,
  flag: "enabled",
  save: vi.fn(),
  saveResult: { isDefault: false } as { isDefault: boolean },
  speakeasyIssuer: "https://speakeasy.example.com/issuer" as string | undefined,
  signIns: [] as { projectId: string; trustedRemoteSessionIssuerId?: string }[],
  issuers: [] as { id: string; name: string; issuer: string }[],
}));
vi.mock("./useEmaServices", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./useEmaServices")>()),
  useEmaServices: () => mocks,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org", slug: "example" }),
  useSession: () => ({}),
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
  useEnterpriseIssuer: () => ({ data: { issuer: mocks.speakeasyIssuer } }),
}));
vi.mock("@gram/client/react-query/organizationUserSessionIssuers.js", () => ({
  useOrganizationUserSessionIssuers: () => ({
    data: { result: { items: mocks.signIns } },
  }),
}));
vi.mock("@/lib/remote-identity", () => ({
  useRemoteSessionIssuersByIds: () => ({ items: mocks.issuers }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));
vi.mock(
  "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js",
  () => ({
    invalidateAllOrganizationRemoteSessionIssuerEMAServices: () =>
      Promise.resolve(),
  }),
);
vi.mock(
  "@gram/client/react-query/setOrganizationRemoteSessionIssuerEMAProviders.js",
  () => ({
    useSetOrganizationRemoteSessionIssuerEMAProvidersMutation: (options: {
      onSuccess: (result: unknown) => void;
    }) => ({
      isPending: false,
      error: null,
      mutate: (variables: unknown) => {
        mocks.save(variables);
        options.onSuccess(mocks.saveResult);
      },
    }),
  }),
);
vi.mock("sonner", () => ({ toast: { success: vi.fn(), warning: vi.fn() } }));

const speakeasy = {
  key: "speakeasy",
  kind: "speakeasy" as const,
  name: "Speakeasy",
  issuerUrl: "https://speakeasy.example.com/issuer",
  usable: true,
  unavailable: "",
  unusedBySignIn: false,
};
const okta = {
  key: "external-okta",
  kind: "external" as const,
  name: "Okta",
  issuerUrl: "https://okta.example.com",
  usable: true,
  unavailable: "",
  lastAcceptedAt: new Date(Date.now() - 60 * 60 * 1000),
  unusedBySignIn: false,
};
const linear: ServiceRow = {
  key: "https://linear.example.com",
  vendorIssuer: "https://linear.example.com",
  name: "Linear",
  remoteSessionIssuerId: "vendor",
  isDefaultOrder: true,
  providers: [
    {
      key: "external-trusted",
      kind: "external",
      name: "Identity provider",
      usable: true,
      unavailable: "",
      unusedBySignIn: false,
    },
    speakeasy,
  ],
  serverCount: 2,
  readyServerCount: 2,
};

function show() {
  render(
    <MemoryRouter>
      <TooltipProvider>
        <EmaServiceWorkspace vendorIssuer="https://linear.example.com" />
      </TooltipProvider>
    </MemoryRouter>,
  );
}
const trustRows = () =>
  within(screen.getByRole("list", { name: "Linear trusts" })).getAllByRole(
    "listitem",
  );

beforeEach(() => {
  mocks.rows = [linear];
  mocks.isPending = false;
  mocks.isError = false;
  mocks.admin = true;
  mocks.flag = "enabled";
  mocks.save.mockReset();
  mocks.saveResult = { isDefault: false };
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

  it("gives the issuer URL to paste once per identity provider, and explains the default placeholder", () => {
    show();
    expect(screen.getByText(/In Linear’s admin settings/)).toBeTruthy();
    const rows = trustRows();
    expect(rows[0]?.textContent).toContain(
      "Each server’s own identity provider",
    );
    expect(rows[0]?.textContent).toContain(
      "Its issuer URL is on that server’s Enterprise Managed Authorization settings.",
    );
    expect(rows[1]?.textContent).toContain("SpeakeasyNot accepted yet");
    expect(rows[1]?.querySelector("code")?.textContent).toBe(
      "https://speakeasy.example.com/issuer",
    );
  });

  it("describes the default order and lets an admin save their own", () => {
    show();
    expect(
      screen.getByText(/Default: each server’s own identity provider first/),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save order" })).toBeNull();
    fireEvent.click(
      screen.getByRole("combobox", { name: "Identity provider to add" }),
    );
    fireEvent.click(screen.getByRole("option", { name: "Okta" }));
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    fireEvent.click(
      screen.getByRole("combobox", { name: "Identity provider to add" }),
    );
    fireEvent.click(screen.getByRole("option", { name: "Speakeasy" }));
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    const order = () =>
      within(screen.getByRole("list", { name: "Provider order" }))
        .getAllByRole("listitem")
        .map((item) => item.textContent?.replace(/\d/, "").trim());
    expect(order()[0]).toContain("Okta");
    expect(order()[1]).toContain("Speakeasy");
    fireEvent.click(screen.getByRole("button", { name: "Move Speakeasy up" }));
    expect(order()[0]).toContain("Speakeasy");
    fireEvent.click(screen.getByRole("button", { name: "Save order" }));
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "vendor",
          providers: [
            { kind: "speakeasy" },
            { kind: "external", remoteSessionIssuerId: "okta" },
          ],
        },
      },
    });
  });

  it("restores the default from a saved order", () => {
    mocks.rows = [
      { ...linear, isDefaultOrder: false, providers: [okta, speakeasy] },
    ];
    show();
    expect(screen.getByText("Last worked 1h ago")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Use default" }));
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: "vendor",
          providers: [],
        },
      },
    });
  });

  it("keeps the order read-only for people who can't change it", () => {
    mocks.admin = false;
    show();
    expect(
      screen.getByText(/Only organization admins signed in directly/),
    ).toBeTruthy();
    expect(screen.queryByRole("combobox")).toBeNull();
  });

  it("says when no server reaches the service any more", () => {
    mocks.rows = [];
    show();
    expect(
      screen.getByText(/No server reaches this service any more/),
    ).toBeTruthy();
  });
});
