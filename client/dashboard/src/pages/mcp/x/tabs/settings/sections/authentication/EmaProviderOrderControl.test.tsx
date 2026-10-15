import type { ReactNode } from "react";
import type { EMAProviderOrder } from "@gram/client/models/components/emaproviderorder.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EmaProviderOrderControl } from "./EmaProviderOrderControl";

const state = vi.hoisted(() => ({
  admin: true,
  session: { organizationOverride: false, impersonatorEmail: "" },
  check: {} as Record<string, unknown>,
  checkEnabled: [] as boolean[],
  save: {} as Record<string, unknown>,
  mutate: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useSession: () => state.session,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => state.admin }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
}));
vi.mock("react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));
vi.mock("@/components/api-error-alert", () => ({
  ApiErrorAlert: ({ error }: { error: Error | null }) =>
    error ? <div role="alert">{error.message}</div> : null,
}));
vi.mock("@/components/vendor-ema-trust-notice", () => ({
  VendorEmaTrustNotice: ({ issuerUrl }: { issuerUrl?: string }) => (
    <div data-testid="trust-notice">{issuerUrl}</div>
  ),
}));
vi.mock("./useSpeakeasyAppendCheck", () => ({
  useSpeakeasyAppendCheck: (_target: unknown, enabled: boolean) => {
    state.checkEnabled.push(enabled);
    return state.check;
  },
}));
vi.mock(
  "@gram/client/react-query/addOrganizationRemoteSessionIssuerSpeakeasyEMAProvider.js",
  () => ({
    useAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation: () => ({
      ...state.save,
      mutate: state.mutate,
    }),
  }),
);

const defaultOrder: EMAProviderOrder = {
  isDefault: true,
  vendorIssuer: "https://vendor.example.com",
  providers: [{ kind: "external" }],
};

function control(
  overrides: Partial<Parameters<typeof EmaProviderOrderControl>[0]> = {},
) {
  return (
    <EmaProviderOrderControl
      order={defaultOrder}
      vendorRemoteSessionIssuerId="vendor"
      userSessionIssuerId="usi"
      trustsExternalIdp={false}
      speakeasyEnabled
      speakeasyIssuerUrl="https://speakeasy.example.com/issuer"
      chainingReady={false}
      refreshing={false}
      onSaved={() => undefined}
      {...overrides}
    />
  );
}

const addButton = () =>
  screen.queryByRole("button", {
    name: "Add Speakeasy",
  }) as HTMLButtonElement | null;

beforeEach(() => {
  state.admin = true;
  state.session = { organizationOverride: false, impersonatorEmail: "" };
  state.check = { isSuccess: true, isFetching: false, isError: false };
  state.checkEnabled.length = 0;
  state.save = { isPending: false, error: null };
  state.mutate.mockReset();
});

afterEach(cleanup);

describe("EmaProviderOrderControl", () => {
  it("appends Speakeasy for a sign-in that trusts no external identity provider", () => {
    render(control());
    expect(
      screen.getByText(/each server uses its own identity provider/),
    ).toBeTruthy();
    expect(screen.getByTestId("trust-notice").textContent).toBe(
      "https://speakeasy.example.com/issuer",
    );
    fireEvent.click(addButton()!);
    expect(state.mutate).toHaveBeenCalledWith({
      request: {
        addSpeakeasyEMAProviderRequestBody: {
          remoteSessionIssuerId: "vendor",
          userSessionIssuerId: "usi",
          dryRun: false,
        },
      },
    });
  });

  it("never offers to add Speakeasy for a sign-in that trusts an external identity provider", () => {
    render(control({ trustsExternalIdp: true }));
    expect(addButton()).toBeNull();
    expect(
      screen.getByText(/trusts an external identity provider/),
    ).toBeTruthy();
    expect(state.checkEnabled).not.toContain(true);
  });

  it("offers no removal once Speakeasy is in the order", () => {
    render(
      control({
        chainingReady: true,
        order: {
          isDefault: false,
          vendorIssuer: "https://vendor.example.com",
          providers: [
            { kind: "external", remoteSessionIssuerId: "okta" },
            { kind: "speakeasy", internalSessionIssuerId: "internal" },
          ],
        },
      }),
    );
    expect(document.body.textContent).toContain("1. Identity provider");
    expect(document.body.textContent).toContain("2. Speakeasy");
    expect(screen.queryAllByRole("button")).toHaveLength(0);
    expect(screen.queryByTestId("trust-notice")).toBeNull();
  });

  it("explains a full order", () => {
    render(
      control({
        order: {
          isDefault: false,
          vendorIssuer: "https://vendor.example.com",
          providers: Array.from({ length: 10 }, (_, index) => ({
            kind: "external" as const,
            remoteSessionIssuerId: `idp-${index}`,
          })),
        },
      }),
    );
    expect(addButton()).toBeNull();
    expect(screen.getByText(/maximum of 10 providers/)).toBeTruthy();
  });

  it("asks for Speakeasy's issuer to be turned on first", () => {
    render(control({ speakeasyEnabled: false }));
    expect(addButton()).toBeNull();
    expect(screen.getByText(/Turn on Speakeasy's issuer/)).toBeTruthy();
  });

  it("is read-only for people who aren't organization admins", () => {
    state.admin = false;
    render(control());
    expect(addButton()).toBeNull();
    expect(screen.getByText(/Only organization admins/)).toBeTruthy();
    expect(state.checkEnabled).not.toContain(true);
  });

  it.each([
    ["support", { organizationOverride: true, impersonatorEmail: "" }],
    [
      "impersonated",
      { organizationOverride: false, impersonatorEmail: "staff@example.com" },
    ],
  ])("is read-only in a %s session", (_name, session) => {
    state.session = session;
    render(control());
    expect(addButton()).toBeNull();
    expect(screen.getByText(/support or impersonated session/)).toBeTruthy();
  });

  it("blocks adding when the server's check refuses, and links to identity settings", () => {
    state.check = {
      isSuccess: false,
      isFetching: false,
      isError: true,
      error: new Error("other servers chain to this vendor"),
    };
    render(control());
    expect(addButton()!.disabled).toBe(true);
    expect(screen.getByText(/other servers chain to this vendor/)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Organization identity settings" })
        .getAttribute("href"),
    ).toBe("/org/identity?tab=identity-providers");
  });

  it("waits for a refresh before allowing another add", () => {
    render(control({ refreshing: true }));
    expect(addButton()!.disabled).toBe(true);
  });

  it("shows a failed save", () => {
    state.save = { isPending: false, error: new Error("save failed") };
    render(control());
    expect(screen.getByRole("alert").textContent).toBe("save failed");
  });
});
