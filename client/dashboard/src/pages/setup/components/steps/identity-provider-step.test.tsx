import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProviderStep } from "./identity-provider-step";
import { toast } from "sonner";
import { openSafeExternalUrl } from "@/lib/safe-external-url";

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: {
      ssoConfigured: false,
      dsyncConfigured: false,
      domainVerified: true,
      verifiedDomains: [] as string[],
    },
    isLoading: false,
    refetch: vi.fn(),
  },
}));
const portal = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPending: false,
  // Options of the section whose mutate ran last, so a test can fire its
  // onError. Each section registers its own options.
  options: undefined as { onError?: (error: unknown) => void } | undefined,
}));
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
  useGenerateWorkOSAdminPortalLinkMutation: (
    options: typeof portal.options,
  ) => ({
    isPending: portal.isPending,
    mutate: (...args: unknown[]) => {
      portal.options = options;
      return portal.mutate(...args);
    },
  }),
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
      verifiedDomains: [],
    },
    isLoading: false,
    refetch: vi.fn(),
  };
  portal.mutate.mockReset();
  portal.options = undefined;
  queryOptions.mockClear();
  vi.mocked(toast.error).mockClear();
  vi.mocked(openSafeExternalUrl).mockReturnValue(true);
});

describe("IdentityProviderStep", () => {
  it("does not reopen the portal when directory sync is connected", () => {
    onboardingStatus.current.data.dsyncConfigured = true;
    const complete = vi.fn<() => void>();
    render(<IdentityProviderStep onComplete={complete} />);
    expect(screen.getByText("Directory sync is connected")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Connect directory" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
    expect(complete).toHaveBeenCalledOnce();
    expect(portal.mutate).not.toHaveBeenCalled();
  });
  it.each([{ button: "Connect" }, { button: "Connect directory" }])(
    "recovers inline from blocked portals in $button",
    ({ button }) => {
      vi.mocked(openSafeExternalUrl).mockReturnValue(false);
      render(<IdentityProviderStep onComplete={() => {}} />);
      expect(queryOptions).toHaveBeenCalledWith(
        undefined,
        undefined,
        expect.objectContaining({ throwOnError: false }),
      );
      expect(queryOptions).not.toHaveBeenCalledWith(
        undefined,
        undefined,
        expect.objectContaining({ enabled: false }),
      );
      if (button === "Connect") {
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
    { intent: "sso", button: "Connect" },
    { intent: "dsync", button: "Connect directory" },
  ])(
    "sends the $intent intent through the portal callback",
    ({ intent, button }) => {
      render(<IdentityProviderStep onComplete={vi.fn<() => void>()} />);
      if (intent === "sso")
        fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
      fireEvent.click(screen.getByRole("button", { name: button }));
      expect(portal.mutate).toHaveBeenCalledWith(
        expect.objectContaining({
          request: {
            generateWorkOSAdminPortalLinkRequestBody: expect.objectContaining({
              intent,
              successUrl: expect.stringMatching(
                new RegExp(`/v1/setup/callback\\?intent=${intent}$`),
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
        verifiedDomains: [],
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
        verifiedDomains: [],
      },
      isLoading: false,
      refetch: vi.fn(),
    };

    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.getByText("Verify a domain above first.")).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();

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

    expect(screen.queryByText("Verify a domain above first.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
    const connect = screen.getByRole<HTMLButtonElement>("button", {
      name: "Connect",
    });
    expect(connect.disabled).toBe(false);
  });
});

describe("IdentityProviderStep domain verification", () => {
  function unverified() {
    onboardingStatus.current.data = {
      ssoConfigured: false,
      dsyncConfigured: false,
      domainVerified: false,
      verifiedDomains: [],
    };
  }
  function mockPortalOpens() {
    portal.mutate.mockImplementation((_vars, opts) =>
      opts.onSuccess({ url: "https://workos.test/portal" }),
    );
  }
  it("orders domain verification before single sign-on and directory sync", () => {
    render(<IdentityProviderStep onComplete={() => {}} />);
    expect(
      screen.getAllByRole("region").map((section) => section.textContent),
    ).toEqual([
      expect.stringMatching(/^1?Verify domain/),
      expect.stringMatching(/^2?Single sign-on/),
      expect.stringMatching(/^3?Directory sync/),
    ]);
  });

  it("opens the WorkOS portal with the domain verification intent", () => {
    unverified();
    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.queryByRole("list", { name: "Verified domains" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));

    expect(portal.mutate).toHaveBeenCalledOnce();
    const body =
      portal.mutate.mock.calls[0]?.[0].request
        .generateWorkOSAdminPortalLinkRequestBody;
    expect(body.intent).toBe("domain_verification");
    expect(body.successUrl).toMatch(
      /\/v1\/setup\/callback\?intent=domain_verification$/,
    );
    expect(body.returnUrl).toBe(window.location.href);
  });

  it("shows the verified state once a verification check succeeds", async () => {
    unverified();
    const verifiedData = {
      ssoConfigured: false,
      dsyncConfigured: false,
      domainVerified: true,
      verifiedDomains: ["example.com"],
    };
    const refetch = vi.fn(async () => {
      onboardingStatus.current = {
        ...onboardingStatus.current,
        data: verifiedData,
      };
      return { data: verifiedData };
    });
    onboardingStatus.current = { ...onboardingStatus.current, refetch };
    mockPortalOpens();

    const view = render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));
    fireEvent.click(screen.getByRole("button", { name: "Check verification" }));

    await waitFor(() => expect(refetch).toHaveBeenCalledOnce());
    // The mocked query does not notify subscribers, so re-render to stand in
    // for react-query pushing the refetched status to the parent.
    view.rerender(<IdentityProviderStep onComplete={() => {}} />);

    await waitFor(() =>
      expect(screen.getByText("Your domain is verified")).toBeTruthy(),
    );
    expect(
      within(screen.getByRole("list", { name: "Verified domains" })).getByText(
        "example.com",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Verify domain" })).toBeNull();
    expect(screen.queryByText("Verify a domain above first.")).toBeNull();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("reports a portal link the browser cannot open", () => {
    unverified();
    vi.mocked(openSafeExternalUrl).mockReturnValueOnce(false);
    mockPortalOpens();

    render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));

    expect(toast.error).toHaveBeenCalledWith(
      "Unable to open the WorkOS portal",
    );
    expect(
      screen.queryByRole("button", { name: "Check verification" }),
    ).toBeNull();
  });

  it("keeps the unverified state when a verification check fails", async () => {
    unverified();
    const refetch = vi.fn(async () => ({
      data: { ...onboardingStatus.current.data },
    }));
    onboardingStatus.current = { ...onboardingStatus.current, refetch };
    mockPortalOpens();

    render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));
    fireEvent.click(screen.getByRole("button", { name: "Check verification" }));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        "Domain not verified yet. Finish setup in the WorkOS tab, then try again.",
      ),
    );
    expect(refetch).toHaveBeenCalledOnce();
    expect(screen.queryByText("Your domain is verified")).toBeNull();
    expect(screen.queryByText("Verified")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Check verification" }),
    ).toBeTruthy();
  });

  it("reports a portal link request that fails", () => {
    unverified();
    portal.mutate.mockImplementation(() =>
      portal.options?.onError?.(new Error("Portal unavailable")),
    );

    render(<IdentityProviderStep onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));

    expect(toast.error).toHaveBeenCalledWith("Portal unavailable");
    expect(screen.getByRole("button", { name: "Verify domain" })).toBeTruthy();
  });

  it("shows the verified state once the server says so", () => {
    onboardingStatus.current.data.verifiedDomains = ["example.com"];
    const onComplete = vi.fn();

    render(<IdentityProviderStep onComplete={() => void onComplete()} />);

    expect(screen.getByText("Your domain is verified")).toBeTruthy();
    expect(screen.getByText("Verified")).toBeTruthy();
    expect(
      within(screen.getByRole("list", { name: "Verified domains" })).getByText(
        "example.com",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Verify domain" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });

  // Matches the server: active SSO completes domain verification even when no
  // verified domain was tracked for the org.
  it("treats an active SSO connection as verified", () => {
    onboardingStatus.current.data = {
      ssoConfigured: true,
      dsyncConfigured: false,
      domainVerified: false,
      verifiedDomains: [],
    };

    render(<IdentityProviderStep onComplete={() => {}} />);

    expect(screen.getByText("Your domain is verified")).toBeTruthy();
    expect(screen.getByText("Verified")).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Verified domains" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Verify domain" })).toBeNull();
    expect(screen.queryByText("Verify a domain above first.")).toBeNull();
  });
});

describe("IdentityProviderStep prerequisites", () => {
  it.each([
    { domainVerified: false, blocked: true },
    { domainVerified: true, blocked: false },
    { domainVerified: undefined, blocked: false },
  ])(
    "gates SSO only for an explicitly unverified domain: $domainVerified",
    ({ domainVerified, blocked }) => {
      onboardingStatus.current.data = {
        dsyncConfigured: false,
        domainVerified,
        ssoConfigured: false,
      } as typeof onboardingStatus.current.data;
      render(<IdentityProviderStep onComplete={vi.fn<() => void>()} />);
      fireEvent.click(screen.getByRole("button", { name: /Okta/ }));
      const connect = screen.getByRole<HTMLButtonElement>("button", {
        name: "Connect",
      });
      expect(connect.disabled).toBe(blocked);
      fireEvent.click(connect);
      expect(portal.mutate).toHaveBeenCalledTimes(blocked ? 0 : 1);
    },
  );
  it("does not gate directory sync on domain verification", () => {
    onboardingStatus.current.data.domainVerified = false;
    render(<IdentityProviderStep onComplete={vi.fn<() => void>()} />);
    fireEvent.click(screen.getByRole("button", { name: "Connect directory" }));
    expect(portal.mutate).toHaveBeenCalledOnce();
  });
});
