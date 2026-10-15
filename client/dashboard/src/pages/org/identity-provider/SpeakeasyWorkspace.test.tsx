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
import { MemoryRouter } from "react-router";
import { SpeakeasyWorkspace } from "./SpeakeasyWorkspace";

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
  rotateResult: undefined as InternalSessionIssuer | undefined,
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
    isLoading: mocks.loading,
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
  useRotateInternalSessionIssuerMutation: (options: {
    onSuccess: (data: InternalSessionIssuer) => void;
  }) => ({
    mutate: (...args: unknown[]) => {
      mocks.rotate(...args);
      if (mocks.rotateResult) options.onSuccess(mocks.rotateResult);
    },
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
  canEnable: false,
};
function show() {
  return render(
    <MemoryRouter>
      <TooltipProvider>
        <SpeakeasyWorkspace />
      </TooltipProvider>
    </MemoryRouter>,
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
  mocks.rotateResult = undefined;
});

describe("Speakeasy identity provider page", () => {
  it("shows a loading state and retry after failure", () => {
    mocks.loading = true;
    const view = show();
    expect(screen.getByRole("status").textContent).toBe("Loading issuer…");
    mocks.loading = false;
    mocks.error = true;
    view.rerender(
      <MemoryRouter>
        <TooltipProvider>
          <SpeakeasyWorkspace />
        </TooltipProvider>
      </MemoryRouter>,
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

  it("lets an administrator finish setting up a key for a keyless issuer", () => {
    mocks.issuer = { ...enabledIssuer, keys: [], state: "keyless" };
    show();
    expect(screen.getByText(/finish setting up a new key below/)).toBeTruthy();
    expect(screen.queryByText(/Speakeasy support/)).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Replace compromised key" }),
    ).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Finish setting up a new key" }),
    );
    expect(mocks.rotate).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
      request: {
        rotateRequestBody: { activeKid: undefined, compromised: true },
      },
    });
  });

  it("reports a replacement that couldn't be set up instead of success", () => {
    mocks.issuer = { ...enabledIssuer, keys: [], state: "keyless" };
    mocks.rotateResult = {
      ...mocks.issuer,
      replacementError: "a replacement key couldn't be set up yet",
    };
    show();
    fireEvent.click(
      screen.getByRole("button", { name: "Finish setting up a new key" }),
    );
    expect(
      screen.getByText("a replacement key couldn't be set up yet"),
    ).toBeTruthy();
  });

  it("replaces a compromised key only after confirmation", () => {
    mocks.issuer = enabledIssuer;
    show();
    fireEvent.click(
      screen.getByRole("button", { name: "Replace compromised key" }),
    );
    expect(mocks.rotate).not.toHaveBeenCalled();
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Replace key",
      }),
    );
    expect(mocks.rotate).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
      request: {
        rotateRequestBody: { activeKid: "example-key", compromised: true },
      },
    });
  });

  it("shows the stable issuer URI with a copy action", () => {
    mocks.issuer = enabledIssuer;
    show();
    expect(screen.getByText(enabledIssuer.issuer!)).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Speakeasy" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Issuer" })).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "RFC 7523" }).getAttribute("href"),
    ).toBe("https://www.rfc-editor.org/rfc/rfc7523.html");
    expect(screen.queryByText(/Google|social sign-in/)).toBeNull();
    expect(
      screen.getByRole("button", { name: "Copy issuer URI" }),
    ).toBeTruthy();
  });

  it.each([
    "verified_domain_required",
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
      expect(
        screen.queryByRole("button", { name: "Replace compromised key" }),
      ).toBeNull();
      expect(screen.queryByRole("heading", { name: "Danger zone" })).toBeNull();
      expect(
        screen.getByRole("button", { name: "Copy issuer URI" }),
      ).toBeTruthy();
    },
  );

  it("does not offer re-enable when the server says the issuer can't be enabled", () => {
    mocks.issuer = { ...enabledIssuer, state: "disabled" };
    show();
    expect(
      screen.queryByRole("button", {
        name: "Enable Enterprise Managed Authorization",
      }),
    ).toBeNull();
    expect(screen.getByText(/still has a verified domain/)).toBeTruthy();
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
    expect(
      screen.getByText(/cannot be enabled on this deployment right now/),
    ).toBeTruthy();
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

  it("asks for a verified domain and links to where domains are verified", () => {
    mocks.issuer = {
      keys: [],
      kmsCleanupPending: 0,
      state: "verified_domain_required",
      canEnable: false,
    };
    show();
    expect(screen.getByText("Needs a verified domain")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Single sign-on" }).getAttribute("href"),
    ).toBe("?tab=sso");
    expect(screen.queryByText(/your organization's plan/)).toBeNull();
  });

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
    expect(
      screen.queryByRole("button", { name: "Replace compromised key" }),
    ).toBeNull();
  });

  it.each([
    ["an enabled issuer with an active key", enabledIssuer, true],
    [
      "a keyless issuer",
      { ...enabledIssuer, keys: [], state: "keyless" },
      false,
    ],
    ["a disabled issuer", { ...enabledIssuer, state: "disabled" }, false],
    ["a paused issuer", { ...enabledIssuer, state: "signing_paused" }, false],
  ] satisfies [string, InternalSessionIssuer, boolean][])(
    "offers routine rotation only for %s",
    (_, issuer, shown) => {
      mocks.issuer = issuer;
      show();
      expect(!!screen.queryByRole("button", { name: "Rotate key" })).toBe(
        shown,
      );
    },
  );

  it.each(["reader", "support", "demo"])(
    "hides routine rotation from a %s session",
    (role) => {
      mocks.admin = role !== "reader";
      mocks.support = role === "support";
      if (role === "demo") mocks.organizationSlug = "acme-demo";
      mocks.issuer = enabledIssuer;
      show();
      expect(screen.queryByRole("button", { name: "Rotate key" })).toBeNull();
    },
  );

  it("rotates routinely only after confirmation", () => {
    mocks.issuer = enabledIssuer;
    show();
    fireEvent.click(screen.getByRole("button", { name: "Rotate key" }));
    expect(mocks.rotate).not.toHaveBeenCalled();
    const dialog = within(screen.getByRole("dialog"));
    expect(
      dialog.getByText(
        /publishes a new key now.*activates automatically within about an hour/,
      ),
    ).toBeTruthy();
    fireEvent.click(dialog.getByRole("button", { name: "Rotate key" }));
    expect(mocks.rotate).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
      request: {
        rotateRequestBody: { activeKid: "example-key", compromised: false },
      },
    });
  });

  it("shows a publishing key and blocks repeat rotation until it is ready", () => {
    mocks.issuer = {
      ...enabledIssuer,
      keys: [
        { kid: "example-key", state: "active" },
        { kid: "next-key", state: "pending" },
      ],
      rotationReadyAt: new Date(Date.now() + 60_000),
    };
    show();
    expect(
      screen.getByText(
        /New key published\. From .+ it activates automatically within the hour/,
      ),
    ).toBeTruthy();
    const button = screen.getByRole("button", { name: "Rotate key" });
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(
      screen.getByRole("button", { name: "Replace compromised key" }),
    ).toBeTruthy();
  });

  it("activates a published key once it is ready, without a second confirmation", () => {
    mocks.issuer = {
      ...enabledIssuer,
      keys: [
        { kid: "example-key", state: "active" },
        { kid: "next-key", state: "pending" },
      ],
      rotationReadyAt: new Date(Date.now() - 1_000),
    };
    show();
    expect(screen.queryByText(/New key published\./)).toBeNull();
    expect(
      screen.getByText(
        /The new key is published and activates automatically within the hour/,
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Activate new key" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.rotate).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
      request: {
        rotateRequestBody: { activeKid: "example-key", compromised: false },
      },
    });
  });

  it.each([
    ["enabled", "Enabled", /vouches for your team/],
    ["setup_required", "Off", /cannot be enabled on this deployment right now/],
    ["disabled", "Off", /cannot be re-enabled right now/],
    ["signing_paused", "Paused", /temporarily paused/],
    ["keyless", "Needs attention", /signing key was withdrawn/],
    ["unavailable", "Unavailable", /not available for this organization/],
  ] as const)("explains the %s state", (state, label, message) => {
    mocks.issuer = { ...enabledIssuer, state };
    show();
    const status = screen.getByRole("heading", { name: "Status" });
    expect(status.parentElement?.textContent).toContain(label);
    expect(screen.getByText(message)).toBeTruthy();
  });

  it("lists signing keys and withdrawn key cleanup", () => {
    mocks.issuer = {
      ...enabledIssuer,
      keys: [
        { kid: "example-key", state: "active" },
        { kid: "next-key", state: "pending" },
      ],
      kmsCleanupPending: 2,
    };
    show();
    const keys = within(screen.getByRole("list", { name: "Signing keys" }));
    expect(keys.getByText("example-key")).toBeTruthy();
    expect(keys.getByText("Active")).toBeTruthy();
    expect(keys.getByText("Pending")).toBeTruthy();
    expect(screen.getByText(/2 withdrawn keys/)).toBeTruthy();
  });

  it("links back to all identity providers", () => {
    mocks.issuer = enabledIssuer;
    show();
    expect(
      screen
        .getByRole("link", { name: "All identity providers" })
        .getAttribute("href"),
    ).toBe("/?tab=identity-providers");
  });

  it("hides the issuer sections before an issuer exists", () => {
    mocks.issuer = {
      keys: [],
      kmsCleanupPending: 0,
      state: "setup_required",
      canEnable: true,
    };
    show();
    expect(screen.queryByRole("heading", { name: "Issuer" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "Signing keys" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "Danger zone" })).toBeNull();
  });

  it("closes an outstanding confirmation when the organization changes", () => {
    mocks.issuer = enabledIssuer;
    const view = show();
    fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    expect(screen.getByRole("dialog")).toBeTruthy();
    mocks.organizationId = "other-example-org";
    view.rerender(
      <MemoryRouter>
        <TooltipProvider>
          <SpeakeasyWorkspace />
        </TooltipProvider>
      </MemoryRouter>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });
});
