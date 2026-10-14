import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { JourneyStepsProvider } from "../journey-steps-provider";
import { PrivateNetworkSetupStep } from "./private-network-setup-step";

const state = vi.hoisted(() => ({
  enabled: false,
  pending: false,
  error: false,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-test" }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { networkIngressEnabled: state.enabled },
    isPending: state.pending,
    isError: state.error,
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ domains: { href: () => "/org-test/domains" } }),
}));
function renderStep(onComplete: () => void = vi.fn<() => void>()) {
  return render(
    <MemoryRouter>
      <JourneyStepsProvider>
        <PrivateNetworkSetupStep onComplete={onComplete} />
      </JourneyStepsProvider>
    </MemoryRouter>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  state.enabled = false;
  state.pending = false;
  state.error = false;
});

describe("private network setup handoff", () => {
  it("hands entitled admins to existing settings without collecting credentials", async () => {
    const user = userEvent.setup();
    state.enabled = true;
    const onComplete = vi.fn<() => void>();
    const { container } = renderStep(onComplete);
    expect(
      screen
        .getByRole("link", { name: "Open private network settings" })
        .getAttribute("href"),
    ).toBe("/org-test/domains");
    expect(screen.getByText(/enabled for this organization/)).toBeTruthy();
    expect(screen.getByText(/Never paste/)).toBeTruthy();
    expect(container.querySelector("input")).toBeNull();
    expect(onComplete).not.toHaveBeenCalled();
    const done = screen.getByRole("button", { name: "Mark done" });
    expect(done.hasAttribute("disabled")).toBe(false);
    await user.click(done);
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("offers support when the entitlement is missing", async () => {
    const onComplete = vi.fn<() => void>();
    renderStep(onComplete);
    const done = screen.getByRole("button", { name: "Mark done" });
    expect(done.hasAttribute("disabled")).toBe(true);
    await userEvent.setup().click(done);
    expect(onComplete).not.toHaveBeenCalled();
    expect(screen.getByText(/not enabled for this organization/)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Contact support" })
        .getAttribute("href"),
    ).toMatch(/^mailto:support@speakeasy.com/);
  });

  it.each(["pending", "error"] as const)(
    "does not claim entitlement while %s",
    async (status) => {
      state[status] = true;
      state.enabled = true;
      const onComplete = vi.fn<() => void>();
      renderStep(onComplete);
      const done = screen.getByRole("button", { name: "Mark done" });
      expect(done.hasAttribute("disabled")).toBe(true);
      await userEvent.setup().click(done);
      expect(onComplete).not.toHaveBeenCalled();
      expect(screen.queryByText(/is enabled for this organization/)).toBeNull();
      expect(
        screen.getByRole("link", { name: "Open private network settings" }),
      ).toBeTruthy();
    },
  );
});
