import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProviderStep } from "./identity-provider-step";
import { ConnectIdpStep } from "./connect-idp-step";
import { DirectorySyncStep } from "./directory-sync-step";

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: {
      ssoConfigured: false,
      dsyncConfigured: false,
      domainVerified: true,
    },
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
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setupTask: {
      Link: ({
        params,
        children,
      }: {
        params: string[];
        children: ReactNode;
      }) => <a href={`/acme/setup/${params[0]}`}>{children}</a>,
    },
  }),
}));
vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: "light" }),
}));

afterEach(cleanup);
beforeEach(() => {
  onboardingStatus.current = {
    data: {
      ssoConfigured: false,
      dsyncConfigured: false,
      domainVerified: true,
    },
    isLoading: false,
    refetch: vi.fn(),
  };
  portal.mutate.mockReset();
});

describe("IdentityProviderStep", () => {
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
      data: {
        ssoConfigured: true,
        dsyncConfigured: true,
        domainVerified: true,
      },
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

  it("blocks SSO setup until a domain is verified", () => {
    onboardingStatus.current = {
      data: {
        ssoConfigured: false,
        dsyncConfigured: false,
        domainVerified: false,
      },
      isLoading: false,
      refetch: vi.fn(),
    };

    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.getByText(/Verify a domain first/)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Go to domain verification" })
        .getAttribute("href"),
    ).toBe("/acme/setup/domain");

    fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
    const connect = screen.getByRole<HTMLButtonElement>("button", {
      name: "Connect",
    });
    expect(connect.disabled).toBe(true);
  });

  it("does not block SSO setup when the status request fails", () => {
    onboardingStatus.current = {
      data: undefined,
      isLoading: false,
      refetch: vi.fn(),
    } as unknown as typeof onboardingStatus.current;

    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.queryByText(/Verify a domain first/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
    const connect = screen.getByRole<HTMLButtonElement>("button", {
      name: "Connect",
    });
    expect(connect.disabled).toBe(false);
  });
});

describe("split identity prerequisites", () => {
  it.each([
    { domainVerified: false, ssoConfigured: false, blocked: true },
    { domainVerified: false, ssoConfigured: true, blocked: false },
    { domainVerified: true, ssoConfigured: false, blocked: false },
    { domainVerified: undefined, ssoConfigured: false, blocked: false },
  ])(
    "gates SSO only for an explicitly unverified new connection: $domainVerified / $ssoConfigured",
    ({ domainVerified, ssoConfigured, blocked }) => {
      onboardingStatus.current.data = {
        dsyncConfigured: false,
        domainVerified,
        ssoConfigured,
      } as typeof onboardingStatus.current.data;
      render(<ConnectIdpStep onComplete={vi.fn()} onSkip={vi.fn()} />);
      fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
      const connect = screen.getByRole<HTMLButtonElement>("button", {
        name: "Connect",
      });
      expect(connect.disabled).toBe(blocked);
      expect(
        !!screen.queryByRole("link", { name: "Go to domain verification" }),
      ).toBe(blocked);
      fireEvent.click(connect);
      expect(portal.mutate).toHaveBeenCalledTimes(blocked ? 0 : 1);
    },
  );
  it("leaves split SSO ungated when status is unavailable", () => {
    onboardingStatus.current = {
      data: undefined,
      isLoading: false,
      refetch: vi.fn(),
    } as unknown as typeof onboardingStatus.current;
    render(<ConnectIdpStep onComplete={vi.fn()} onSkip={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(portal.mutate).toHaveBeenCalledOnce();
  });
  it("does not gate directory sync on domain verification", () => {
    onboardingStatus.current.data.domainVerified = false;
    render(<DirectorySyncStep onComplete={vi.fn()} onSkip={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Connect directory" }));
    expect(portal.mutate).toHaveBeenCalledOnce();
  });
});
