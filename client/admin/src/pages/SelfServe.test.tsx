import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SelfServePage } from "./SelfServe";
import { renderWithApp } from "@/test/harness";

const api = vi.hoisted(() => ({ fetch: vi.fn(), mutate: vi.fn() }));
vi.mock("@/lib/gramAdminApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/gramAdminApi")>()),
  gramAdminFetch: api.fetch,
  gramAdminMutation: api.mutate,
}));

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(cleanup);

describe("Self-serve signup", () => {
  it("turns self-serve off after confirmation", async () => {
    api.fetch.mockResolvedValue({ disabled: false });
    api.mutate.mockResolvedValue({
      disabled: true,
      updated_by: "operator@example.com",
      updated_at: "2026-01-01T00:00:00Z",
    });
    await renderWithApp(<SelfServePage />);
    expect(await screen.findByText("Self-serve is on")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Turn self-serve off" }),
    );
    expect(api.mutate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    await waitFor(() =>
      expect(api.mutate).toHaveBeenCalledWith(
        "/admin/selfServe.set",
        expect.objectContaining({ body: JSON.stringify({ disabled: true }) }),
      ),
    );
    expect(await screen.findByText("Self-serve is off")).toBeTruthy();
    expect(screen.getByText(/operator@example.com/)).toBeTruthy();
  });

  it("turns self-serve back on", async () => {
    api.fetch.mockResolvedValue({ disabled: true });
    api.mutate.mockResolvedValue({ disabled: false });
    await renderWithApp(<SelfServePage />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Turn self-serve on" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    await waitFor(() =>
      expect(api.mutate).toHaveBeenCalledWith(
        "/admin/selfServe.set",
        expect.objectContaining({ body: JSON.stringify({ disabled: false }) }),
      ),
    );
    expect(await screen.findByText("Self-serve is on")).toBeTruthy();
  });
});
