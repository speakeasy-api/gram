import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProviderStep } from "./identity-provider-step";

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: { ssoConfigured: false, dsyncConfigured: false },
    isLoading: false,
    refetch: vi.fn(),
  },
}));
const portal = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }));

vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => onboardingStatus.current,
}));
vi.mock("@gram/client/react-query/generateWorkOSAdminPortalLink.js", () => ({
  useGenerateWorkOSAdminPortalLinkMutation: () => portal,
}));
vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: "light" }),
}));

afterEach(cleanup);
beforeEach(() => {
  onboardingStatus.current = {
    data: { ssoConfigured: false, dsyncConfigured: false },
    isLoading: false,
    refetch: vi.fn(),
  };
  portal.mutate.mockReset();
});

describe("IdentityProviderStep", () => {
  it("offers SSO and directory sync in one card and continues regardless", () => {
    const onComplete = vi.fn();
    render(<IdentityProviderStep onComplete={() => void onComplete()} />);

    expect(screen.getByText("Set up identity provider")).toBeTruthy();
    expect(screen.getByText("Single sign-on")).toBeTruthy();
    expect(screen.getByText("Directory sync")).toBeTruthy();

    const connect = screen.getByRole("button", { name: "Connect" });
    expect(connect.hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
    expect(connect.hasAttribute("disabled")).toBe(false);
    fireEvent.click(connect);
    expect(portal.mutate).toHaveBeenCalledOnce();

    expect(
      screen.getByRole("button", { name: "Connect directory" }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("shows both halves as connected once the server says so", () => {
    onboardingStatus.current = {
      data: { ssoConfigured: true, dsyncConfigured: true },
      isLoading: false,
      refetch: vi.fn(),
    };

    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.getByText("Single sign-on is connected")).toBeTruthy();
    expect(screen.getByText("Directory sync is connected")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Connect directory" }),
    ).toBeNull();
  });
});
