import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { ConvergenceSummary } from "./ConvergenceSummary";
const result = vi.hoisted(() => vi.fn());
vi.mock("@/lib/gramAdminClient", () => ({
  adminListGlobalIssuerConvergenceCandidatesQuery: () => ({
    queryKey: ["summary"],
    queryFn: result,
  }),
}));
vi.mock("./Convergence", () => ({ ConvergenceHelp: () => null }));
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => (
    <span>{children}</span>
  ),
}));
afterEach(cleanup);
it("reports a paginated lower bound rather than fabricated total or readiness", async () => {
  result.mockResolvedValue({ result: { items: [{}, {}], nextCursor: "more" } });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ConvergenceSummary issuerId="target" />
    </QueryClientProvider>,
  );
  expect(await screen.findByText(/At least 2 matching issuers/)).toBeTruthy();
  expect(screen.getByText(/do not indicate migration readiness/)).toBeTruthy();
});
