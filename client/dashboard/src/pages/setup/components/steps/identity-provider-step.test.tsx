import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IdentityProviderSetupStep } from "@gram/client/models/components/identityprovidersetupstep.js";
import { IdentityProviderStep } from "./identity-provider-step";

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: { ssoConfigured: false, dsyncConfigured: false },
    isLoading: false,
    refetch: vi.fn(),
  },
}));
const portal = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }));
const applications = vi.hoisted(() => ({
  current: {
    data: undefined,
    isPending: false,
    isFetching: false,
    error: null,
    refetch: vi.fn(),
  } as {
    data: { applications: unknown[]; applicationCount: number } | undefined;
    isPending: boolean;
    isFetching: boolean;
    error: unknown;
    refetch: () => unknown;
  },
}));
const readiness = vi.hoisted(() => ({
  current: { data: undefined, isPending: false } as {
    data: { eligible: boolean } | undefined;
    isPending: boolean;
  },
}));
const identityProvider = vi.hoisted(() => ({
  current: { data: { connection: undefined }, isPending: false } as {
    data: {
      connection:
        | { status: string; directoryState?: string | undefined }
        | undefined;
    };
    isPending: boolean;
  },
}));
const identityProviderSetup = vi.hoisted(() => ({
  current: { data: undefined, isPending: false } as {
    data:
      | { connectionId: string; steps: IdentityProviderSetupStep[] }
      | undefined;
    isPending: boolean;
  },
}));

vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => onboardingStatus.current,
}));
vi.mock("@gram/client/react-query/generateWorkOSAdminPortalLink.js", () => ({
  useGenerateWorkOSAdminPortalLinkMutation: () => portal,
}));
vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: "light" }),
}));

// The guided path reads the identity provider connection, and which steps open
// follows from it. This file covers the fork, the grid and what each step does
// with the connection; okta-connect-section.test.tsx covers the exchange itself.
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("react-router", () => ({
  useSearchParams: () => [new URLSearchParams(), vi.fn()],
}));
vi.mock("@gram/client/react-query/listIdentityProviderApplications.js", () => ({
  useListIdentityProviderApplications: () => applications.current,
}));
vi.mock("@/components/guided-readiness/use-guided-readiness", () => ({
  useGuidedReadiness: () => ({
    ...readiness.current,
    isFetching: false,
    error: null,
    refetch: () => undefined,
  }),
}));
vi.mock("@gram/client/react-query/identityProvider.js", () => ({
  useIdentityProvider: () => identityProvider.current,
  invalidateAllIdentityProvider: vi.fn(),
}));
vi.mock("@gram/client/react-query/identityProviderSetup.js", () => ({
  useIdentityProviderSetup: () => identityProviderSetup.current,
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

/** The Okta entry Speakeasy walks itself, once the flow is offered. */
const guidedOkta = () => screen.getByRole("button", { name: /Okta Guided/ });
/** The same entry where the advanced flow is not on offer: no badge. */
const oidcOkta = () => screen.getByRole("button", { name: /Okta OIDC/ });
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
  identityProvider.current = {
    data: { connection: undefined },
    isPending: false,
  };
  // The organization has passed the checks unless a test says otherwise.
  readiness.current = { data: { eligible: true }, isPending: false };
  identityProviderSetup.current = { data: undefined, isPending: false };
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

  it("keeps the same five steps and fills them in when Okta is picked", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);

    const steps = [
      "Select IDP",
      "Single sign-on",
      "Directory sync",
      "Applications and access",
      "Enterprise managed auth setup",
    ];
    // The list is the same before and after a provider is chosen; picking one
    // fills the steps in rather than swapping them for a different set.
    for (const title of steps) {
      expect(screen.getByRole("heading", { name: title })).toBeTruthy();
    }

    fireEvent.click(guidedOkta());

    for (const title of steps) {
      expect(screen.getByRole("heading", { name: title })).toBeTruthy();
    }
    // Step one carries the exchange, and the portal round trip is gone: Okta's
    // sign-on and directory are Speakeasy's own work, not the portal's.
    expect(screen.getByLabelText("Okta organization URL")).toBeTruthy();
    expect(
      screen.queryByText(/sign-in provider portal opens in a new browser tab/),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Connect directory" }),
    ).toBeNull();
  });

  it("offers guided directory setup once Okta is connected", () => {
    identityProvider.current = {
      data: {
        connection: { status: "active", directoryState: "not_started" },
      },
      isPending: false,
    };
    identityProviderSetup.current = {
      data: {
        connectionId: "conn-1",
        steps: [
          {
            key: "directory",
            title: "Directory sync",
            where: "our_page",
            instructions: [
              "Speakeasy creates the directory application in Okta.",
            ],
            printedValues: [],
            expectedValues: [],
            state: "not_started",
          },
        ],
      },
      isPending: false,
    };
    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(
      screen.getByRole("button", { name: "Set up directory sync" }),
    ).toBeTruthy();
    expect(
      screen.queryByText(/WorkOS portal opens in a new browser tab/),
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

  it("never names the sign-in provider vendor to the customer", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(portalOkta());

    // The portal is somebody else's product and the customer is not buying it
    // from them. Every step that sends them there says whose job it does, not
    // whose name is over the door.
    expect(document.body.textContent).not.toMatch(/workos/i);
    // Both round trips: sign-on and the directory.
    expect(screen.getAllByText(/sign-in provider portal opens/)).toHaveLength(
      2,
    );
  });

  describe("when the advanced flow is not offered", () => {
    const NOT_AVAILABLE =
      "Guided setup for Okta is not available for this organization yet.";

    it("keeps Okta in the grid but takes the portal path, saying so once", () => {
      readiness.current = { data: { eligible: false }, isPending: false };
      render(<IdentityProviderStep onComplete={() => {}} />);

      // The entry is still there and still pickable; what is gone is the
      // badge, the fork behind it, and nothing else.
      expect(oidcOkta()).toBeTruthy();
      expect(screen.queryByRole("button", { name: /Okta Guided/ })).toBeNull();
      expect(screen.queryByText(NOT_AVAILABLE)).toBeNull();

      fireEvent.click(oidcOkta());

      expect(screen.getByText(NOT_AVAILABLE)).toBeTruthy();
      expect(screen.queryByLabelText("Okta organization URL")).toBeNull();
      const connect = screen.getByRole("button", { name: "Connect" });
      expect(connect.hasAttribute("disabled")).toBe(false);
      fireEvent.click(connect);
      expect(portal.mutate).toHaveBeenCalledOnce();
    });

    it("tells the customer nothing about why", () => {
      readiness.current = { data: { eligible: false }, isPending: false };
      render(<IdentityProviderStep onComplete={() => {}} />);
      fireEvent.click(oidcOkta());

      // One sentence, and no trace of the checks behind it: not the verdict,
      // not a check key, not who has to act on one.
      expect(screen.getAllByText(NOT_AVAILABLE)).toHaveLength(1);
      expect(screen.queryByText(/readiness/i)).toBeNull();
      expect(screen.queryByText(/eligible/i)).toBeNull();
      expect(screen.queryByText(/workos_/)).toBeNull();
      expect(screen.queryByText(/Platform admin/)).toBeNull();
    });

    it("says nothing at all until the check comes back", () => {
      readiness.current = { data: undefined, isPending: true };
      render(<IdentityProviderStep onComplete={() => {}} />);

      // Neither answer is in yet, so the card claims neither: no badge to
      // take away again, and no sentence to retract.
      expect(screen.queryByRole("button", { name: /Okta Guided/ })).toBeNull();
      expect(screen.queryByText(NOT_AVAILABLE)).toBeNull();

      fireEvent.click(oidcOkta());

      expect(screen.queryByText(NOT_AVAILABLE)).toBeNull();
      // Pickable meanwhile, on the path every other provider takes.
      expect(screen.queryByLabelText("Okta organization URL")).toBeNull();
      expect(screen.getByRole("button", { name: "Connect" })).toBeTruthy();
    });

    it("keeps the advanced flow for an organization already connected", () => {
      readiness.current = { data: { eligible: false }, isPending: false };
      identityProvider.current = {
        data: { connection: { status: "active" } },
        isPending: false,
      };
      render(<IdentityProviderStep onComplete={() => {}} />);

      // The connection is the proof the pre-work was done once, so a check
      // that says otherwise now does not strand the organization mid-setup.
      expect(guidedOkta()).toBeTruthy();
      expect(screen.queryByText(NOT_AVAILABLE)).toBeNull();
      expect(
        screen.getByText("This is the one step that leaves Speakeasy"),
      ).toBeTruthy();
    });
  });

  it("keeps the provider grid on screen once a provider is picked", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(guidedOkta());

    // The selector stays at the top of the step: picking again is how you
    // change your mind, so the step needs no escape hatch of its own.
    expect(guidedOkta()).toBeTruthy();
    expect(portalOkta()).toBeTruthy();
    expect(screen.getByLabelText("Okta organization URL")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Choose a different provider" }),
    ).toBeNull();

    fireEvent.click(portalOkta());
    expect(screen.queryByLabelText("Okta organization URL")).toBeNull();
  });
});
