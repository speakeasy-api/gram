import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { PromotionAccountUsage, PromotionReview } from "./PromotionReview";

const mocks = vi.hoisted(() => ({
  review: vi.fn<() => Promise<unknown>>(),
  start: vi.fn<(input: unknown) => Promise<unknown>>(),
}));

vi.mock("@/lib/gramAdminClient", () => ({
  registryPromotionReviewQuery: (target: unknown) => ({
    queryKey: ["registryPromotionReview", target],
    queryFn: mocks.review,
  }),
  useStartRegistryPromotionReviewMutation: () => ({
    mutateAsync: mocks.start,
    isPending: false,
  }),
}));

const target = {
  organizationId: "org-synthetic",
  projectId: "project-synthetic",
  mcpServerId: "server-synthetic",
};

function renderReview(availability = "available"): void {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PromotionReview target={target} availability={availability} />
    </QueryClientProvider>,
  );
}

async function enabledRunButton(): Promise<HTMLElement> {
  return vi.waitFor(() => {
    const button = screen.getByRole("button", { name: "Run review" });
    expect(button.hasAttribute("disabled")).toBe(false);
    return button;
  });
}

afterEach(cleanup);

beforeEach(() => {
  mocks.review.mockReset();
  mocks.start.mockReset();
});

test("explains why a customer-shaped endpoint cannot be reviewed", () => {
  renderReview("endpoint_not_allowed");
  expect(screen.getByText(/may identify a customer/)).toBeTruthy();
  expect(screen.queryByRole("button")).toBeNull();
  expect(mocks.review).not.toHaveBeenCalled();
});

test("explains when reviews are turned off but keeps existing findings", async () => {
  mocks.review.mockResolvedValue({
    review: {
      requestId: "review",
      research: { status: "completed", report: { summary: "Prior findings." } },
    },
  });
  renderReview("disabled");
  expect(await screen.findByText("Prior findings.")).toBeTruthy();
  expect(screen.getByRole("status").textContent).toContain("turned off");
  expect(
    screen
      .getByRole("button", { name: "Run review again" })
      .hasAttribute("disabled"),
  ).toBe(true);
});

test("starts a review and shows the run", async () => {
  mocks.review.mockResolvedValue({});
  mocks.start.mockResolvedValue({
    review: { requestId: "review", research: { status: "running" } },
  });
  renderReview();
  fireEvent.click(await enabledRunButton());
  expect(mocks.start).toHaveBeenCalledWith({ request: target });
  expect(
    (
      await screen.findByRole("button", { name: "Research running…" })
    ).hasAttribute("disabled"),
  ).toBe(true);
});

test("reports a failed start", async () => {
  mocks.review.mockResolvedValue({});
  mocks.start.mockRejectedValue(new Error("synthetic"));
  renderReview();
  fireEvent.click(await enabledRunButton());
  expect((await screen.findByRole("alert")).textContent).toContain(
    "could not be started",
  );
});

test("shows cited findings and links only https citations", async () => {
  mocks.review.mockResolvedValue({
    review: {
      requestId: "review",
      evidence: {
        authority: { mode: "oauth" },
        gaps: ["domain_lookup_failed"],
      },
      research: {
        status: "completed",
        report: {
          summary: "Little independent coverage.",
          coverage: { level: "thin" },
          claims: [
            {
              tier: "vendor_claim",
              text: "The vendor publishes a security policy.",
              citations: [
                { url: "https://example.com/security", title: "Security" },
                { url: "javascript:alert(1)", title: "Unsafe" },
              ],
            },
          ],
        },
      },
    },
  });
  renderReview();
  expect(await screen.findByText("Little independent coverage.")).toBeTruthy();
  expect(screen.getByText("oauth")).toBeTruthy();
  expect(screen.getByText("domain_lookup_failed")).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "Security" }).getAttribute("href"),
  ).toBe("https://example.com/security");
  expect(screen.queryByText("Unsafe")).toBeNull();
  expect(
    screen
      .getByRole("button", { name: "Run review again" })
      .hasAttribute("disabled"),
  ).toBe(false);
});

test("says when account usage is unavailable", () => {
  render(<PromotionAccountUsage usage={undefined} />);
  expect(screen.getByText(/Usage is unavailable/)).toBeTruthy();
});

test("shows aggregate account usage", () => {
  render(
    <PromotionAccountUsage
      usage={{
        windowDays: 14,
        distinctUsers: 3,
        clients: ["synthetic-client"],
        tools: [{ name: "search", calls: 20, successes: 19 }],
        transport: "streamable-http",
      }}
    />,
  );
  expect(screen.getByText("synthetic-client")).toBeTruthy();
  expect(screen.getByText("19 of 20 succeeded")).toBeTruthy();
});
