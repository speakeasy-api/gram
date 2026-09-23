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

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: { domainVerified: false, verifiedDomains: [] as string[] },
    isLoading: false,
    refetch: vi.fn(),
  },
}));
const portal = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }));
const safeUrl = vi.hoisted(() => ({ open: vi.fn(() => true) }));

vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => onboardingStatus.current,
}));
vi.mock("@gram/client/react-query/generateWorkOSAdminPortalLink.js", () => ({
  useGenerateWorkOSAdminPortalLinkMutation: () => portal,
}));
vi.mock("@/lib/safe-external-url", () => ({
  openSafeExternalUrl: safeUrl.open,
}));

afterEach(cleanup);
beforeEach(() => {
  onboardingStatus.current = {
    data: { domainVerified: false, verifiedDomains: [] as string[] },
    isLoading: false,
    refetch: vi.fn(),
  };
  portal.mutate.mockReset();
  safeUrl.open.mockClear();
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

  it("refetches status when the admin checks verification", async () => {
    const refetch = vi.fn(async () => ({ data: { domainVerified: true } }));
    onboardingStatus.current = { ...onboardingStatus.current, refetch };
    portal.mutate.mockImplementation((_vars, opts) =>
      opts.onSuccess({ url: "https://workos.test/portal" }),
    );

    render(<DomainVerificationStep onComplete={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Verify domain" }));
    fireEvent.click(screen.getByRole("button", { name: "Check verification" }));

    await waitFor(() => expect(refetch).toHaveBeenCalledOnce());
  });

  it("shows the verified state once the server says so", () => {
    onboardingStatus.current = {
      data: { domainVerified: true, verifiedDomains: ["example.com"] },
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
});
