import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DomainVerificationStep } from "./domain-verification-step";

function status(
  overrides: Partial<{
    domainVerified: boolean;
    ssoConfigured: boolean;
    verifiedDomains: string[];
  }> = {},
) {
  return {
    domainVerified: false,
    ssoConfigured: false,
    verifiedDomains: [] as string[],
    ...overrides,
  };
}

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: {
      domainVerified: false,
      ssoConfigured: false,
      verifiedDomains: [] as string[],
    },
    isLoading: false,
    refetch: vi.fn(),
  },
}));
const portal = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }));
const safeUrl = vi.hoisted(() => ({ open: vi.fn(() => true) }));
const toasts = vi.hoisted(() => ({ error: vi.fn() }));

vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => onboardingStatus.current,
}));
vi.mock("@gram/client/react-query/generateWorkOSAdminPortalLink.js", () => ({
  useGenerateWorkOSAdminPortalLinkMutation: () => portal,
}));
vi.mock("sonner", () => ({ toast: toasts }));
vi.mock("@/lib/safe-external-url", () => ({
  openSafeExternalUrl: safeUrl.open,
}));

afterEach(cleanup);
beforeEach(() => {
  onboardingStatus.current = {
    data: status(),
    isLoading: false,
    refetch: vi.fn(),
  };
  portal.mutate.mockReset();
  safeUrl.open.mockClear();
  toasts.error.mockClear();
});

describe("DomainVerificationStep", () => {
  it("opens the WorkOS portal with the domain verification intent", () => {
    render(<DomainVerificationStep onComplete={() => {}} />);

    expect(screen.getByText("Verify your domain")).toBeTruthy();
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
    const verifiedData = status({
      domainVerified: true,
      verifiedDomains: ["example.com"],
    });
    const refetch = vi.fn(async () => {
      onboardingStatus.current = {
        ...onboardingStatus.current,
        data: verifiedData,
      };
      return { data: verifiedData };
    });
    onboardingStatus.current = { ...onboardingStatus.current, refetch };
    portal.mutate.mockImplementation((_vars, opts) =>
      opts.onSuccess({ url: "https://workos.test/portal" }),
    );

    render(<DomainVerificationStep onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));
    fireEvent.click(screen.getByRole("button", { name: "Check verification" }));

    await waitFor(() =>
      expect(screen.getByText("Your domain is verified")).toBeTruthy(),
    );
    expect(refetch).toHaveBeenCalledOnce();
    expect(
      within(screen.getByRole("list", { name: "Verified domains" })).getByText(
        "example.com",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Verify domain" })).toBeNull();
    expect(toasts.error).not.toHaveBeenCalled();
  });

  it("reports a portal link the browser cannot open", () => {
    safeUrl.open.mockReturnValueOnce(false);
    portal.mutate.mockImplementation((_vars, opts) =>
      opts.onSuccess({ url: "javascript:alert(1)" }),
    );

    render(<DomainVerificationStep onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));

    expect(toasts.error).toHaveBeenCalledWith(
      "Unable to open the WorkOS portal",
    );
    expect(
      screen.queryByRole("button", { name: "Check verification" }),
    ).toBeNull();
  });

  it("shows the verified state once the server says so", () => {
    onboardingStatus.current = {
      data: status({ domainVerified: true, verifiedDomains: ["example.com"] }),
      isLoading: false,
      refetch: vi.fn(),
    };
    const onComplete = vi.fn();

    render(<DomainVerificationStep onComplete={() => void onComplete()} />);

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

  // Matches the server: active SSO completes the task even when no verified
  // domain was tracked for the org.
  it("treats an active SSO connection as verified", () => {
    onboardingStatus.current = {
      data: status({ ssoConfigured: true }),
      isLoading: false,
      refetch: vi.fn(),
    };

    render(<DomainVerificationStep onComplete={() => {}} />);

    expect(screen.getByText("Your domain is verified")).toBeTruthy();
    expect(screen.getByText("Verified")).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Verified domains" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Verify domain" })).toBeNull();
  });
});
