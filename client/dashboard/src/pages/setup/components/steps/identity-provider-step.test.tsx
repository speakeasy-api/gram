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

// The guided path reads the identity provider connection. This file covers the
// fork and the grid; okta-connect-section.test.tsx covers the exchange itself,
// so here there is never a connection.
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/identityProvider.js", () => ({
  useIdentityProvider: () => ({
    data: { connection: undefined },
    isPending: false,
  }),
  invalidateAllIdentityProvider: vi.fn(),
}));
vi.mock("@gram/client/react-query/identityProviderSetup.js", () => ({
  useIdentityProviderSetup: () => ({ data: undefined, isPending: false }),
  invalidateAllIdentityProviderSetup: vi.fn(),
}));
vi.mock("@gram/client/react-query/createIdentityProvider.js", () => ({
  useCreateIdentityProviderMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/submitIdentityProviderSetupStep.js", () => ({
  useSubmitIdentityProviderSetupStepMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/verifyIdentityProviderSetupStep.js", () => ({
  useVerifyIdentityProviderSetupStepMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/deleteIdentityProvider.js", () => ({
  useDeleteIdentityProviderMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

/** The Okta entry Speakeasy walks itself. */
const guidedOkta = () => screen.getByRole("button", { name: /Okta Guided/ });
/** The Okta entry that still hands off to the WorkOS portal. */
const portalOkta = () => screen.getByRole("button", { name: /Okta SAML/ });

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
    fireEvent.click(portalOkta());
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

  it("offers Okta over OIDC ahead of its SAML entry, marked as guided", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(guidedOkta()).toBeTruthy();
    expect(portalOkta()).toBeTruthy();
    expect(screen.getByText("Recommended over SAML")).toBeTruthy();
  });

  it("forks the card into the guided journey when Okta over OIDC is picked", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(guidedOkta());

    for (const title of [
      "Connect Okta",
      "Single sign-on",
      "Directory",
      "Applications and access",
      "Token exchange with Okta",
    ]) {
      expect(screen.getByRole("heading", { name: title })).toBeTruthy();
    }

    // With nothing connected the step asks for the tenant first.
    expect(screen.getByLabelText("Okta organization URL")).toBeTruthy();

    // The portal round trip is gone: this provider is walked in-product.
    expect(
      screen.queryByText(/WorkOS portal opens in a new browser tab/),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Connect directory" }),
    ).toBeNull();
  });

  it("names the rest of the journey without opening it", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(guidedOkta());

    const locked = screen
      .getAllByRole("region", { hidden: true })
      .filter((section) => section.getAttribute("aria-disabled") === "true");
    expect(locked).toHaveLength(4);
    // A locked step names its outcome and stops there — no body, no controls.
    for (const section of locked) {
      expect(section.querySelectorAll("button, a, input")).toHaveLength(0);
    }

    expect(screen.getAllByText("Waiting")).toHaveLength(3);
    expect(screen.getByText("Later")).toBeTruthy();
    expect(screen.getByText(/Setup finishes without this/)).toBeTruthy();
  });

  it("lets the administrator back out to the grid before anything is submitted", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(guidedOkta());
    expect(screen.getByRole("heading", { name: "Connect Okta" })).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Choose a different provider" }),
    );

    expect(screen.queryByRole("heading", { name: "Connect Okta" })).toBeNull();
    expect(screen.getByText("Directory sync")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Connect" }).hasAttribute("disabled"),
    ).toBe(true);
  });
});
