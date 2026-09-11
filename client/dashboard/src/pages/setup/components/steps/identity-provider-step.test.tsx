import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProviderStep } from "./identity-provider-step";
import { ConnectIdpStep } from "./connect-idp-step";
import { DirectorySyncStep } from "./directory-sync-step";
import { toast } from "sonner";
import { openSafeExternalUrl } from "@/lib/safe-external-url";

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: { ssoConfigured: false, dsyncConfigured: false },
    isLoading: false,
    refetch: vi.fn(),
  },
}));
const portal = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }));
const queryOptions = vi.hoisted(() => vi.fn());
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("@/lib/safe-external-url", () => ({
  openSafeExternalUrl: vi.fn(() => true),
}));

vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: (...args: unknown[]) => {
    queryOptions(...args);
    return onboardingStatus.current;
  },
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
  queryOptions.mockClear();
  vi.mocked(toast.error).mockClear();
  vi.mocked(openSafeExternalUrl).mockReturnValue(true);
});

describe("IdentityProviderStep", () => {
  it("does not reopen the portal when directory sync is connected", () => {
    onboardingStatus.current.data.dsyncConfigured = true;
    const complete = vi.fn<() => void>();
    render(
      <DirectorySyncStep
        onComplete={complete}
        onSkip={() => {}}
        onBack={() => {}}
      />,
    );
    expect(screen.getByText("Directory sync is connected.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(complete).toHaveBeenCalledOnce();
    expect(portal.mutate).not.toHaveBeenCalled();
  });
  it.each([
    { Component: ConnectIdpStep, button: "Connect", enabled: false },
    {
      Component: DirectorySyncStep,
      button: "Connect directory",
      enabled: undefined,
    },
  ])(
    "recovers inline from blocked portals in $button",
    ({ Component, button, enabled }) => {
      vi.mocked(openSafeExternalUrl).mockReturnValue(false);
      render(
        <Component onComplete={() => {}} onSkip={() => {}} onBack={() => {}} />,
      );
      expect(queryOptions).toHaveBeenCalledWith(
        undefined,
        undefined,
        expect.objectContaining({ throwOnError: false }),
      );
      if (enabled === false) {
        expect(queryOptions).toHaveBeenCalledWith(
          undefined,
          undefined,
          expect.objectContaining({ enabled: false }),
        );
        fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
      }
      fireEvent.click(screen.getByRole("button", { name: button }));
      portal.mutate.mock.calls[0]![1].onSuccess({
        url: "https://example.com/portal",
      });
      expect(toast.error).toHaveBeenCalledWith(
        "Unable to open the WorkOS portal. Allow popups and try again.",
      );
      expect(screen.getByRole("button", { name: button })).toBeTruthy();
    },
  );
  it.each([
    {
      Component: ConnectIdpStep,
      intent: "sso",
      task: "connect-idp",
      button: "Connect",
    },
    {
      Component: DirectorySyncStep,
      intent: "dsync",
      task: "directory-sync",
      button: "Connect directory",
    },
  ])(
    "preserves the $task origin through the portal callback",
    ({ Component, intent, task, button }) => {
      render(
        <Component
          onComplete={vi.fn<() => void>()}
          onSkip={vi.fn<() => void>()}
          onBack={vi.fn<() => void>()}
        />,
      );
      if (intent === "sso")
        fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
      fireEvent.click(screen.getByRole("button", { name: button }));
      expect(portal.mutate).toHaveBeenCalledWith(
        expect.objectContaining({
          request: {
            generateWorkOSAdminPortalLinkRequestBody: expect.objectContaining({
              intent,
              successUrl: expect.stringContaining(
                `/v1/setup/callback?intent=${intent}&task=${task}`,
              ),
              returnUrl: window.location.href,
            }),
          },
        }),
        expect.anything(),
      );
    },
  );
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
