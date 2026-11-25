import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SignupOffersPage } from "./SignupOffers";
import { renderWithApp } from "@/test/harness";

const api = vi.hoisted(() => ({ fetch: vi.fn(), mutate: vi.fn() }));
vi.mock("@/lib/gramAdminApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/gramAdminApi")>()),
  gramAdminFetch: api.fetch,
  gramAdminMutation: api.mutate,
}));
const offer = {
  id: "00000000-0000-4000-8000-000000000001",
  name: "Fixture offer",
  stripe_promotion_code_id: "promo_fixture",
  signup_flow: "tailscale_partner_gateway",
  enabled: false,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};
beforeEach(() => {
  vi.clearAllMocks();
  api.fetch.mockResolvedValue({ offers: [offer] });
  api.mutate.mockResolvedValue(offer);
});
afterEach(cleanup);

describe("Signup offers", () => {
  it("lists the supported flow without playbook or discount editing", async () => {
    await renderWithApp(<SignupOffersPage />);
    expect(await screen.findByText("Fixture offer")).toBeTruthy();
    expect(screen.getByText("Tailscale Partner Gateway")).toBeTruthy();
    expect(screen.queryByText(/playbook/i)).toBeNull();
    expect(screen.queryByRole("spinbutton")).toBeNull();
  });
  it("creates a disabled offer using the exact Stripe ID", async () => {
    await renderWithApp(<SignupOffersPage />);
    fireEvent.click(screen.getByRole("button", { name: "Create offer" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "New offer" },
    });
    fireEvent.change(screen.getByLabelText("Stripe promotion code ID"), {
      target: { value: "promo_New" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save offer" }));
    await waitFor(() =>
      expect(api.mutate).toHaveBeenCalledWith(
        "/admin/signupOffers.create",
        expect.objectContaining({
          body: JSON.stringify({
            name: "New offer",
            stripe_promotion_code_id: "promo_New",
            signup_flow: "tailscale_partner_gateway",
          }),
        }),
      ),
    );
  });
  it("enables and disables explicitly", async () => {
    await renderWithApp(<SignupOffersPage />);
    const enable = await screen.findByRole("button", {
      name: "Enable Fixture offer",
    });
    api.fetch.mockResolvedValue({ offers: [{ ...offer, enabled: true }] });
    fireEvent.click(enable);
    await waitFor(() =>
      expect(api.mutate).toHaveBeenCalledWith(
        "/admin/signupOffers.update",
        expect.objectContaining({
          body: expect.stringContaining('"enabled":true'),
        }),
      ),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Disable Fixture offer" }),
      ).toBeTruthy(),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Disable Fixture offer" }),
    );
    await waitFor(() =>
      expect(api.mutate).toHaveBeenCalledWith(
        "/admin/signupOffers.update",
        expect.objectContaining({
          body: expect.stringContaining('"enabled":false'),
        }),
      ),
    );
  });
  it("edits mapping and retains enabled state", async () => {
    await renderWithApp(<SignupOffersPage />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Fixture offer" }),
    );
    fireEvent.change(screen.getByLabelText("Stripe promotion code ID"), {
      target: { value: "promo_changed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save offer" }));
    await waitFor(() => {
      expect(api.mutate).toHaveBeenCalledOnce();
      const [path, request] = api.mutate.mock.calls[0]!;
      expect(path).toBe("/admin/signupOffers.update");
      expect(JSON.parse(request.body)).toEqual(
        expect.objectContaining({
          stripe_promotion_code_id: "promo_changed",
          enabled: false,
        }),
      );
    });
  });
});
