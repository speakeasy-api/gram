import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { EnterpriseIssuerSection } from "./EnterpriseIssuerSection";

const mocks = vi.hoisted(() => ({
  organizationId: "example-org",
  organizationSlug: "example-org",
  admin: true,
  support: false,
  issuer: undefined as InternalSessionIssuer | undefined,
  loading: false,
  error: false,
  refetch: vi.fn(),
  mutate: vi.fn(),
  rotate: vi.fn(),
  reset: vi.fn(),
  setData: vi.fn(),
  invalidate: vi.fn(),
  onSuccess: undefined as ((data: InternalSessionIssuer) => void) | undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: mocks.organizationId,
    slug: mocks.organizationSlug,
  }),
  useSession: () => ({ organizationOverride: mocks.support }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.admin }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueryData: mocks.setData }),
}));
vi.mock("./useEnterpriseIssuer", () => ({
  enterpriseIssuerQueryKey: (organizationId: string) => [
    "@gram/client",
    "internalSessionIssuers",
    "get",
    { organizationId },
  ],
  useEnterpriseIssuer: () => ({
    data: mocks.issuer,
    isPending: mocks.loading,
    isError: mocks.error,
    isFetching: false,
    refetch: mocks.refetch,
  }),
}));
vi.mock("@gram/client/react-query/internalSessionIssuer.js", () => ({
  invalidateAllInternalSessionIssuer: mocks.invalidate,
}));
vi.mock("@gram/client/react-query/setInternalSessionIssuerEnabled.js", () => ({
  useSetInternalSessionIssuerEnabledMutation: (options: {
    onSuccess: (data: InternalSessionIssuer) => void;
  }) => {
    mocks.onSuccess = options.onSuccess;
    return {
      mutate: mocks.mutate,
      reset: mocks.reset,
      isPending: false,
      isError: false,
    };
  },
}));
vi.mock("@gram/client/react-query/rotateInternalSessionIssuer.js", () => ({
  useRotateInternalSessionIssuerMutation: () => ({
    mutate: mocks.rotate,
    reset: mocks.reset,
    isPending: false,
    isError: false,
  }),
}));

const enabledIssuer: InternalSessionIssuer = {
  issuer: "https://example.com/oauth/idp/org/example-org",
  keys: [{ kid: "example-key", state: "active" }],
  state: "enabled",
  kmsCleanupPending: 0,
};
function show(ssoFeatureEnabled = true) {
  return render(
    <TooltipProvider>
      <EnterpriseIssuerSection ssoFeatureEnabled={ssoFeatureEnabled} />
    </TooltipProvider>,
  );
}
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.organizationId = "example-org";
  mocks.organizationSlug = "example-org";
  mocks.admin = true;
  mocks.support = false;
  mocks.issuer = undefined;
  mocks.loading = false;
  mocks.error = false;
});

describe("enterprise authorization", () => {
  it("shows a loading state and retry after failure", () => {
    mocks.loading = true;
    const view = show();
    expect(screen.getByText("Loading issuer…")).toBeTruthy();
    mocks.loading = false;
    mocks.error = true;
    view.rerender(
      <TooltipProvider>
        <EnterpriseIssuerSection ssoFeatureEnabled />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetch).toHaveBeenCalledOnce();
  });

  it("provisions an issuer through the management API", () => {
    mocks.issuer = { keys: [], kmsCleanupPending: 0, state: "setup_required" };
    show();
    fireEvent.click(
      screen.getByRole("button", { name: "Enable enterprise authorization" }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: { setEnabledRequestBody: { enabled: true } },
    });
    mocks.onSuccess?.(enabledIssuer);
    expect(mocks.setData).toHaveBeenCalledWith(
      [
        "@gram/client",
        "internalSessionIssuers",
        "get",
        { organizationId: "example-org" },
      ],
      enabledIssuer,
    );
    expect(mocks.invalidate).toHaveBeenCalledOnce();
  });

  it.each(["enabled", "setup_required"] as const)(
    "explains the enterprise SSO session prerequisite in %s state",
    (state) => {
      mocks.issuer = { ...enabledIssuer, state };
      show();
      expect(
        screen.getByText(/each user must sign in to/).textContent,
      ).toContain("Google or other social sign-in alone is not sufficient");
    },
  );

  it("hands keyless recovery to an administrator with support guidance", () => {
    mocks.issuer = { ...enabledIssuer, keys: [], state: "keyless" };
    show();
    expect(screen.getByText(/signing-key recovery procedure/)).toBeTruthy();
    expect(
      screen.getByText(/Recovery is not available on this page/),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Rotate key" })).toBeNull();
  });

  it("shows the stable issuer URI with a copy action", () => {
    mocks.issuer = enabledIssuer;
    show();
    expect(screen.getByText(enabledIssuer.issuer!)).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Copy issuer URI" }),
    ).toBeTruthy();
  });

  it.each([
    "entitlement_required",
    "unavailable",
    "signing_paused",
    "keyless",
  ] as const)(
    "keeps containment available for an existing issuer in %s state",
    (state) => {
      mocks.issuer = { ...enabledIssuer, state };
      show(false);
      fireEvent.click(
        screen.getByRole("button", {
          name: "Disable enterprise authorization",
        }),
      );
      fireEvent.click(
        within(screen.getByRole("dialog")).getByRole("button", {
          name: "Disable",
        }),
      );
      expect(mocks.mutate).toHaveBeenCalledWith({
        request: { setEnabledRequestBody: { enabled: false } },
      });
    },
  );

  it.each(["reader", "support"])(
    "hides mutations from a %s session",
    (role) => {
      mocks.admin = role !== "reader";
      mocks.support = role === "support";
      mocks.issuer = enabledIssuer;
      show();
      expect(
        screen.queryByRole("button", {
          name: "Disable enterprise authorization",
        }),
      ).toBeNull();
      expect(screen.queryByRole("button", { name: "Rotate key" })).toBeNull();
      expect(
        screen.getByRole("button", { name: "Copy issuer URI" }),
      ).toBeTruthy();
    },
  );

  it("does not offer re-enable after entitlement removal", () => {
    mocks.issuer = { ...enabledIssuer, state: "disabled" };
    show(false);
    expect(
      screen.queryByRole("button", { name: "Enable enterprise authorization" }),
    ).toBeNull();
    expect(screen.getByText(/restore SSO access/)).toBeTruthy();
  });

  it("keeps the shared demo read-only", () => {
    mocks.issuer = enabledIssuer;
    mocks.organizationSlug = "acme-demo";
    show();
    expect(screen.getByText("The shared demo is read-only.")).toBeTruthy();
    expect(
      screen.queryByRole("button", {
        name: "Disable enterprise authorization",
      }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Rotate key" })).toBeNull();
  });

  it("rotates the current key only after confirmation", () => {
    mocks.issuer = enabledIssuer;
    show();
    fireEvent.click(screen.getByRole("button", { name: "Rotate key" }));
    expect(mocks.rotate).not.toHaveBeenCalled();
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Rotate key",
      }),
    );
    expect(mocks.rotate).toHaveBeenCalledWith({
      request: {
        rotateRequestBody: { activeKid: "example-key", compromised: false },
      },
    });
  });

  it("shows pending publication without offering another rotation", () => {
    mocks.issuer = {
      ...enabledIssuer,
      rotationReadyAt: new Date("2030-01-01"),
    };
    show();
    expect(screen.getByText(/new signing key is being published/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Rotate key" })).toBeNull();
  });

  it("closes an outstanding confirmation when the organization changes", () => {
    mocks.issuer = enabledIssuer;
    const view = show();
    fireEvent.click(
      screen.getByRole("button", { name: "Disable enterprise authorization" }),
    );
    expect(screen.getByRole("dialog")).toBeTruthy();
    mocks.organizationId = "other-example-org";
    view.rerender(
      <TooltipProvider>
        <EnterpriseIssuerSection ssoFeatureEnabled />
      </TooltipProvider>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });
});
