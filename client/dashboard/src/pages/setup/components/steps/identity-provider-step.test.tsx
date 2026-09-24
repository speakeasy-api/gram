import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProviderStep } from "./identity-provider-step";

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: {
      ssoConfigured: false,
      dsyncConfigured: false,
      domainVerified: true,
    },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  },
}));

type PortalCall = {
  intent: string;
  onSuccess?: (data: { url: string }) => void;
  onError?: (error: unknown) => void;
};

// Captures the hook-level onError alongside the per-call onSuccess, so a test
// answers the exact section that fired the mutation without depending on which
// section rendered first.
const portal = vi.hoisted(() => ({
  isPending: false,
  calls: [] as PortalCall[],
  respond: undefined as ((call: PortalCall) => void) | undefined,
}));

const external = vi.hoisted(() => ({ open: vi.fn((_url: string) => true) }));
const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }));

vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => onboardingStatus.current,
}));
vi.mock("@gram/client/react-query/generateWorkOSAdminPortalLink.js", () => ({
  useGenerateWorkOSAdminPortalLinkMutation: (hookOptions?: {
    onError?: (error: unknown) => void;
  }) => ({
    isPending: portal.isPending,
    mutate: (
      variables: {
        request: {
          generateWorkOSAdminPortalLinkRequestBody: { intent: string };
        };
      },
      callbacks?: { onSuccess?: (data: { url: string }) => void },
    ) => {
      const call: PortalCall = {
        intent:
          variables.request.generateWorkOSAdminPortalLinkRequestBody.intent,
        onSuccess: callbacks?.onSuccess,
        onError: hookOptions?.onError,
      };
      portal.calls.push(call);
      portal.respond?.(call);
    },
  }),
}));
vi.mock("sonner", () => ({ toast }));
vi.mock("@/lib/safe-external-url", () => ({
  openSafeExternalUrl: (url: string) => external.open(url),
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
    isError: false,
    refetch: vi.fn(),
  };
  portal.calls = [];
  portal.respond = undefined;
  portal.isPending = false;
  external.open.mockReset();
  external.open.mockReturnValue(true);
  toast.error.mockReset();
  toast.success.mockReset();
});

function connectDirectory() {
  fireEvent.click(screen.getByRole("button", { name: "Connect directory" }));
}

function openDirectoryPortal() {
  portal.respond = (call) =>
    call.onSuccess?.({ url: "https://id.workos.com/portal/launch?x=1" });
  connectDirectory();
}

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
    expect(portal.calls).toHaveLength(1);
    expect(portal.calls[0]?.intent).toBe("sso");

    connectDirectory();
    expect(portal.calls[1]?.intent).toBe("dsync");

    fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("shows both halves as connected once the server says so", () => {
    onboardingStatus.current = {
      ...onboardingStatus.current,
      data: {
        ssoConfigured: true,
        dsyncConfigured: true,
        domainVerified: true,
      },
    };

    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.getByText("Single sign-on is connected")).toBeTruthy();
    expect(screen.getByText("Directory sync is connected")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Connect directory" }),
    ).toBeNull();
  });

  it("does not block setup when the status request fails", () => {
    onboardingStatus.current = {
      ...onboardingStatus.current,
      data: undefined,
    } as unknown as typeof onboardingStatus.current;

    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.queryByText(/Verify a domain first/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Connect" })
        .disabled,
    ).toBe(false);
  });
});

// WorkOS refuses either connection for an organization with no verified
// domain, so the wizard names that next step rather than letting the admin
// bounce off a server-side rejection.
describe("verified domain gate", () => {
  beforeEach(() => {
    onboardingStatus.current = {
      ...onboardingStatus.current,
      data: {
        ssoConfigured: false,
        dsyncConfigured: false,
        domainVerified: false,
      },
    };
  });

  it("points both sub-steps at domain verification", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.getAllByText(/Verify a domain first/)).toHaveLength(2);
    expect(
      screen
        .getAllByRole("link", { name: "Go to domain verification" })[0]
        ?.getAttribute("href"),
    ).toBe("/acme/setup/domain");
  });

  it("blocks the SSO portal", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);

    fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Connect" })
        .disabled,
    ).toBe(true);
    expect(portal.calls).toHaveLength(0);
  });

  it("blocks the directory portal", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(
      screen.getByRole<HTMLButtonElement>("button", {
        name: "Connect directory",
      }).disabled,
    ).toBe(true);
    expect(portal.calls).toHaveLength(0);
  });
});

describe("directory sync failure feedback", () => {
  it("reports the server's explanation when the portal link is refused", () => {
    const message =
      "WorkOS rejected the Directory Sync setup request. Verify a domain for this organization, then try again.";
    portal.respond = (call) => call.onError?.(new Error(message));

    render(<IdentityProviderStep onComplete={() => {}} />);
    connectDirectory();

    expect(toast.error).toHaveBeenCalledWith(message);
  });

  it("falls back to its own copy when the failure carries no message", () => {
    portal.respond = (call) => call.onError?.(new Error("   "));

    render(<IdentityProviderStep onComplete={() => {}} />);
    connectDirectory();

    expect(toast.error).toHaveBeenCalledWith(
      "Could not start directory sync setup. Try again, and contact support if it keeps failing.",
    );
  });

  it("reports a blocked pop-up instead of leaving the click silent", () => {
    external.open.mockReturnValue(false);

    render(<IdentityProviderStep onComplete={() => {}} />);
    openDirectoryPortal();

    expect(toast.error).toHaveBeenCalledWith(
      "Could not open the WorkOS portal for directory sync. Allow pop-ups for this site, then try again.",
    );
    // A portal that never opened must not swap Connect for Verify.
    expect(
      screen.getByRole("button", { name: "Connect directory" }),
    ).toBeTruthy();
  });

  it("swaps to verify once the portal opens", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);
    openDirectoryPortal();

    expect(toast.error).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Verify connection" }),
    ).toBeTruthy();
  });

  it("separates a failed status check from a directory WorkOS has not linked", async () => {
    const refetch = vi
      .fn()
      .mockResolvedValueOnce({ data: undefined })
      .mockResolvedValueOnce({
        data: { ssoConfigured: false, dsyncConfigured: false },
      });
    onboardingStatus.current = { ...onboardingStatus.current, refetch };

    render(<IdentityProviderStep onComplete={() => {}} />);
    openDirectoryPortal();

    const verify = screen.getByRole("button", { name: "Verify connection" });
    fireEvent.click(verify);
    await vi.waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        "Could not check the directory sync connection. Try again in a moment.",
      ),
    );

    fireEvent.click(verify);
    await vi.waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        "No directory sync connection detected yet. Finish setup in the WorkOS tab, then try again.",
      ),
    );
  });

  it("stays quiet when verification finds the directory", async () => {
    const refetch = vi.fn().mockResolvedValue({
      data: { ssoConfigured: false, dsyncConfigured: true },
    });
    onboardingStatus.current = { ...onboardingStatus.current, refetch };

    render(<IdentityProviderStep onComplete={() => {}} />);
    openDirectoryPortal();
    fireEvent.click(screen.getByRole("button", { name: "Verify connection" }));

    await vi.waitFor(() => expect(refetch).toHaveBeenCalledOnce());
    expect(toast.error).not.toHaveBeenCalled();
  });
});

// Connected state for both sub-steps comes from one call that reaches WorkOS
// for the SSO connections and the directory, so a directory lookup failure
// would otherwise render a confident, wrong "not connected".
describe("unreadable setup status", () => {
  it("says the state is unknown and offers a retry", () => {
    const refetch = vi.fn();
    onboardingStatus.current = {
      ...onboardingStatus.current,
      data: undefined,
      isError: true,
      refetch,
    } as unknown as typeof onboardingStatus.current;

    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.getByText("Setup status unavailable")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(refetch).toHaveBeenCalledOnce();
  });

  it("stays out of the way when the status loads", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.queryByText("Setup status unavailable")).toBeNull();
  });
});
