import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlatformMcpFeedbackPage } from "./PlatformMcpFeedback";
import { renderWithApp } from "@/test/harness";

const api = vi.hoisted(() => ({ fetch: vi.fn() }));
vi.mock("@/lib/gramAdminApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/gramAdminApi")>()),
  gramAdminFetch: api.fetch,
}));

const report = {
  id: "00000000-0000-4000-8000-000000000001",
  organization_id: "org-fixture",
  organization_name: "Fixture Org",
  subject_urn: "user:user-fixture",
  user_email: "sender@example.test",
  category: "missing_capability",
  rating: 2,
  tool_name: "find_mcp",
  note: "Could not find the server I wanted",
  delivery_state: "queued",
  created_at: "2026-01-01T00:00:00Z",
  expires_at: "2026-06-30T00:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(cleanup);

describe("Platform MCP feedback", () => {
  it("lists each report with its details", async () => {
    api.fetch.mockResolvedValue({ feedback: [report] });
    await renderWithApp(<PlatformMcpFeedbackPage />);
    expect(await screen.findByText("Fixture Org")).toBeTruthy();
    expect(screen.getByText("sender@example.test")).toBeTruthy();
    expect(screen.getByText("missing capability")).toBeTruthy();
    expect(screen.getByText("2/5")).toBeTruthy();
    expect(screen.getByText("find_mcp")).toBeTruthy();
    expect(screen.getByText("Could not find the server I wanted")).toBeTruthy();
    expect(api.fetch).toHaveBeenCalledWith("/admin/platformMcpFeedback.list");
  });

  it("falls back to ids when the organization or user is gone", async () => {
    api.fetch.mockResolvedValue({
      feedback: [
        {
          ...report,
          organization_name: undefined,
          user_email: undefined,
        },
      ],
    });
    await renderWithApp(<PlatformMcpFeedbackPage />);
    expect(await screen.findByText("org-fixture")).toBeTruthy();
    expect(screen.getByText("user:user-fixture")).toBeTruthy();
  });

  it("shows an empty state", async () => {
    api.fetch.mockResolvedValue({ feedback: [] });
    await renderWithApp(<PlatformMcpFeedbackPage />);
    expect(
      await screen.findByText("No feedback has been sent yet."),
    ).toBeTruthy();
  });

  it("shows the error and retries", async () => {
    api.fetch.mockRejectedValueOnce(new Error("Feedback read failed"));
    await renderWithApp(<PlatformMcpFeedbackPage />);
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Feedback read failed",
    );
    api.fetch.mockResolvedValue({ feedback: [report] });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getByText("Fixture Org")).toBeTruthy());
  });
});
