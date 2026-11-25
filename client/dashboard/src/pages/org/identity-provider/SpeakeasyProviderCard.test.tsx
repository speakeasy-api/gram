import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import { SpeakeasyProviderCard } from "./SpeakeasyProviderCard";

const mocks = vi.hoisted(() => ({
  issuer: undefined as InternalSessionIssuer | undefined,
  loading: false,
  error: false,
  refetch: vi.fn(),
}));
vi.mock("./useEnterpriseIssuer", () => ({
  useEnterpriseIssuer: () => ({
    data: mocks.issuer,
    isLoading: mocks.loading,
    isError: mocks.error,
    isFetching: false,
    refetch: mocks.refetch,
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
        <SpeakeasyProviderCard />
      </TooltipProvider>
    </MemoryRouter>,
  );
}
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.issuer = undefined;
  mocks.loading = false;
  mocks.error = false;
});

describe("Speakeasy identity provider card", () => {
  it("summarizes an enabled issuer and links to its page", () => {
    mocks.issuer = enabledIssuer;
    show();
    expect(screen.getByRole("heading", { name: "Speakeasy" })).toBeTruthy();
    expect(screen.getByText("Enabled")).toBeTruthy();
    expect(screen.getByText(enabledIssuer.issuer!)).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Copy issuer URI" }),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Manage Speakeasy" })
        .getAttribute("href"),
    ).toBe("/?tab=identity-providers&provider=speakeasy");
  });

  it("offers setup before an issuer exists", () => {
    mocks.issuer = {
      keys: [],
      kmsCleanupPending: 0,
      state: "setup_required",
      canEnable: true,
    };
    show();
    expect(screen.getByRole("link", { name: "Set up Speakeasy" })).toBeTruthy();
    expect(screen.queryByText(/Issuer URI/)).toBeNull();
  });

  it("flags an issuer that needs attention", () => {
    mocks.issuer = { ...enabledIssuer, keys: [], state: "keyless" };
    show();
    expect(screen.getByText("Needs attention")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Manage Speakeasy" })).toBeTruthy();
  });

  it("keeps destructive actions off the card", () => {
    mocks.issuer = enabledIssuer;
    show();
    expect(screen.queryByRole("button", { name: "Disable" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Replace compromised key" }),
    ).toBeNull();
  });

  it("shows a loading state and retry after failure", () => {
    mocks.loading = true;
    const view = show();
    expect(screen.getByRole("status").textContent).toBe("Loading issuer…");
    expect(screen.queryByRole("link")).toBeNull();
    mocks.loading = false;
    mocks.error = true;
    view.rerender(
      <MemoryRouter>
        <TooltipProvider>
          <SpeakeasyProviderCard />
        </TooltipProvider>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetch).toHaveBeenCalledOnce();
  });
});
