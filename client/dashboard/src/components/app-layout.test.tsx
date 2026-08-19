import {
  cleanup,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  logout: vi.fn(),
  switchScopes: vi.fn(),
  useOrganization: vi.fn(),
  useSession: vi.fn(),
}));

vi.mock("@/contexts/Auth.tsx", () => ({
  useOrganization: () => mocks.useOrganization(),
  useSession: () => mocks.useSession(),
}));

vi.mock("@/contexts/Sdk.tsx", () => ({
  useSdkClient: () => ({
    auth: { logout: mocks.logout, switchScopes: mocks.switchScopes },
  }),
}));

import { ImpersonationBanner } from "./app-layout";
import { useShowsImpersonationBanner } from "./impersonation-banner-state";

let originalLocation: Location | undefined;

const baseSession = {
  impersonatorEmail: undefined,
  organizationOverride: false,
  organizations: [{ id: "org-owned", name: "Owned", slug: "owned" }],
  user: { email: "support@example.test" },
};

beforeEach(() => {
  mocks.logout.mockReset().mockResolvedValue(undefined);
  mocks.switchScopes.mockReset().mockResolvedValue(undefined);
  mocks.useOrganization.mockReturnValue({
    id: "org-target",
    name: "Target organization",
    slug: "target",
  });
  mocks.useSession.mockReturnValue(baseSession);
  document.cookie = "gram_admin_override=; path=/; max-age=0";
  localStorage.clear();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  if (originalLocation) {
    Object.defineProperty(window, "location", {
      configurable: true,
      value: originalLocation,
    });
    originalLocation = undefined;
  }
});

describe("trusted support banner", () => {
  it("shows trusted organization support state", () => {
    mocks.useSession.mockReturnValue({
      ...baseSession,
      organizationOverride: true,
    });

    render(<ImpersonationBanner />);

    expect(
      screen.getByText("Support access active for Target organization"),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Exit support access" }),
    ).toBeTruthy();
  });

  it("does not trust a stale legacy override cookie", () => {
    document.cookie = "gram_admin_override=stale; path=/";

    const { result } = renderHook(() => useShowsImpersonationBanner());

    expect(result.current).toBe(false);
  });

  it("logs out to exit support access", async () => {
    mocks.useSession.mockReturnValue({
      ...baseSession,
      organizationOverride: true,
    });
    originalLocation = window.location;
    const hrefSetter = vi.fn();
    // @ts-expect-error happy-dom-compatible location replacement for redirect assertion
    delete window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: {
        // oxlint-disable-next-line typescript/no-misused-spread -- happy-dom Location is plain enough for tests
        ...originalLocation,
        set href(value: string) {
          hrefSetter(value);
        },
      },
    });

    render(<ImpersonationBanner />);
    await userEvent.click(
      screen.getByRole("button", { name: "Exit support access" }),
    );

    await waitFor(() => {
      expect(mocks.logout).toHaveBeenCalledOnce();
      expect(hrefSetter).toHaveBeenCalledWith("/login");
    });
  });

  it("preserves demo exit by switching to an owned organization", async () => {
    mocks.useOrganization.mockReturnValue({
      id: "org-demo",
      name: "Demo",
      slug: "acme-demo",
    });
    vi.spyOn(window.location, "replace").mockImplementation(() => {});

    render(<ImpersonationBanner />);
    await userEvent.click(screen.getByRole("button", { name: "Exit demo" }));

    await waitFor(() => {
      expect(mocks.switchScopes).toHaveBeenCalledWith({
        organizationId: "org-owned",
      });
      expect(mocks.logout).not.toHaveBeenCalled();
    });
  });

  it("preserves WorkOS impersonation messaging", () => {
    mocks.useSession.mockReturnValue({
      ...baseSession,
      impersonatorEmail: "initiator@example.test",
    });

    render(<ImpersonationBanner />);

    expect(
      screen.getByText(
        "WorkOS impersonation active — signed in as support@example.test in Target organization",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Stop impersonating" }),
    ).toBeTruthy();
  });
});
