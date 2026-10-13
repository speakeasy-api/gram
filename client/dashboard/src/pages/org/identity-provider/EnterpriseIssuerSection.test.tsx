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

const enabledIssuer: InternalSessionIssuer = {
  issuer: "https://example.com/oauth/idp/org/example-org",
  keys: [{ kid: "example-key", state: "active" }],
  state: "enabled",
  kmsCleanupPending: 0,
  canEnable: false,
};
function show() {
  return render(
    <TooltipProvider>
      <EnterpriseIssuerSection />
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

describe("Enterprise Managed Authorization", () => {
  it("shows a loading state and retry after failure", () => {
    mocks.loading = true;
    const view = show();
    expect(screen.getByText("Loading issuer…")).toBeTruthy();
    mocks.loading = false;
    mocks.error = true;
    view.rerender(
      <TooltipProvider>
        <EnterpriseIssuerSection />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetch).toHaveBeenCalledOnce();
  });

  it("provisions an issuer through the management API", () => {
    mocks.issuer = {
      keys: [],
      kmsCleanupPending: 0,
      state: "setup_required",
      canEnable: true,
    };
    show();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Enable Enterprise Managed Authorization",
      }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
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

  it("hands keyless recovery to an administrator with support guidance", () => {
    mocks.issuer = { ...enabledIssuer, keys: [], state: "keyless" };
    show();
    expect(
      screen.getByText(/contact Speakeasy support to restore it/),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Rotate key" })).toBeNull();
  });

  it("shows the stable issuer URI with a copy action", () => {
    mocks.issuer = enabledIssuer;
    show();
    expect(screen.getByText(enabledIssuer.issuer!)).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Authorization" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "RFC 7523" }).getAttribute("href"),
    ).toBe("https://www.rfc-editor.org/rfc/rfc7523.html");
    expect(screen.queryByText(/Google|social sign-in/)).toBeNull();
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
      show();
      fireEvent.click(
        screen.getByRole("button", {
          name: "Disable",
        }),
      );
      fireEvent.click(
        within(screen.getByRole("dialog")).getByRole("button", {
          name: "Disable",
        }),
      );
      expect(mocks.mutate).toHaveBeenCalledWith({
        security: { sessionHeaderGramSession: "" },
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
          name: "Disable",
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
    show();
    expect(
      screen.queryByRole("button", {
        name: "Enable Enterprise Managed Authorization",
      }),
    ).toBeNull();
    expect(screen.getByText(/check availability and SSO access/)).toBeTruthy();
  });

  it.each(["rollout is disabled", "status lookup failed"])(
    "does not offer re-enable when %s despite existing SSO access",
    () => {
      mocks.issuer = { ...enabledIssuer, state: "disabled", canEnable: false };
      show();
      expect(
        screen.queryByRole("button", {
          name: "Enable Enterprise Managed Authorization",
        }),
      ).toBeNull();
      expect(screen.getByText(/cannot be re-enabled right now/)).toBeTruthy();
      expect(screen.getByText(enabledIssuer.issuer!)).toBeTruthy();
    },
  );

  it("offers re-enable when the server confirms eligibility", () => {
    mocks.issuer = { ...enabledIssuer, state: "disabled", canEnable: true };
    show();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Enable Enterprise Managed Authorization",
      }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
      request: { setEnabledRequestBody: { enabled: true } },
    });
  });

  it("does not offer initial enable when the server cannot confirm eligibility", () => {
    mocks.issuer = {
      keys: [],
      kmsCleanupPending: 0,
      state: "setup_required",
      canEnable: false,
    };
    show();
    expect(
      screen.queryByRole("button", {
        name: "Enable Enterprise Managed Authorization",
      }),
    ).toBeNull();
    expect(screen.getByText(/cannot be enabled right now/)).toBeTruthy();
  });

  it.each(["reader", "support"])(
    "does not offer eligible enable to a %s session",
    (role) => {
      mocks.admin = role !== "reader";
      mocks.support = role === "support";
      mocks.issuer = { ...enabledIssuer, state: "disabled", canEnable: true };
      show();
      expect(
        screen.queryByRole("button", {
          name: "Enable Enterprise Managed Authorization",
        }),
      ).toBeNull();
    },
  );

  it("keeps the shared demo read-only", () => {
    mocks.issuer = enabledIssuer;
    mocks.organizationSlug = "acme-demo";
    show();
    expect(screen.getByText("The shared demo is read-only.")).toBeTruthy();
    expect(
      screen.queryByRole("button", {
        name: "Disable",
      }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Rotate key" })).toBeNull();
  });

  it.each([undefined, new Date("2030-01-01"), new Date("2020-01-01")])(
    "leaves signing-key maintenance out of the page (%s)",
    (rotationReadyAt) => {
      mocks.issuer = { ...enabledIssuer, rotationReadyAt };
      show();
      expect(screen.queryByRole("button", { name: /rotat/i })).toBeNull();
      expect(screen.queryByText(/signing key|complete rotation/i)).toBeNull();
    },
  );

  it("closes an outstanding confirmation when the organization changes", () => {
    mocks.issuer = enabledIssuer;
    const view = show();
    fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    expect(screen.getByRole("dialog")).toBeTruthy();
    mocks.organizationId = "other-example-org";
    view.rerender(
      <TooltipProvider>
        <EnterpriseIssuerSection />
      </TooltipProvider>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });
});
