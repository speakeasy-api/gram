import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Gram } from "@gram/client";
import { HTTPClient, type Fetcher } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TrialStatusCard } from "./trial-status-card";

const mocks = vi.hoisted(() => ({
  session: vi.fn(),
  flag: vi.fn(),
  hasScope: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({ useSession: () => mocks.session() }));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flag(),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: "light" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));
vi.mock("@/pages/demo/components/booking-calendar/BookingCalendarLink", () => ({
  BookingCalendarLink: ({ children }: { children: React.ReactNode }) => (
    <button>{children}</button>
  ),
}));

const DAY = 86_400_000;
const activeTrial = {
  startedAt: new Date(Date.now() - DAY),
  endsAt: new Date(Date.now() + DAY),
};
const expiredTrial = {
  startedAt: new Date(Date.now() - 2 * DAY),
  endsAt: new Date(Date.now() - DAY),
};
const invalidTrial = {
  startedAt: new Date(Date.now() + DAY),
  endsAt: new Date(Date.now() - DAY),
};
const clients: QueryClient[] = [];
function setup() {
  const fetcher = vi.fn<Fetcher>().mockResolvedValue(
    Response.json(
      {
        fault: false,
        id: "test-error",
        message: "No subscription",
        name: "not_found",
        temporary: false,
        timeout: false,
      },
      { status: 404 },
    ),
  );
  const sdk = new Gram({
    serverURL: "https://billing.example",
    httpClient: new HTTPClient({ fetcher }),
    retryConfig: { strategy: "none" },
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  render(
    <QueryClientProvider client={client}>
      <GramProvider client={sdk}>
        <MemoryRouter>
          <TrialStatusCard />
        </MemoryRouter>
      </GramProvider>
    </QueryClientProvider>,
  );
  return fetcher;
}
beforeEach(() => {
  mocks.session.mockReturnValue({
    activeOrganizationId: "org-test",
    organization: { slug: "test-org" },
    rawGramAccountType: "payg",
    whitelisted: true,
    hasActiveSubscription: false,
    trial: activeTrial,
  });
  mocks.flag.mockReturnValue({ status: "enabled" });
  mocks.hasScope.mockReturnValue(true);
});
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
});

it.each([
  ["missing", null],
  ["invalid", invalidTrial],
  ["expired", expiredTrial],
])("does not request a subscription for a %s trial", async (_name, trial) => {
  mocks.session.mockReturnValue({ ...mocks.session(), trial });
  const fetcher = setup();
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
  expect(fetcher).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: /add payment method/i }),
  ).toBeNull();
  if (_name === "expired")
    expect(
      screen.getByRole("button", { name: /talk to sales about upgrading/i }),
    ).toBeTruthy();
  else expect(screen.queryByRole("progressbar")).toBeNull();
});
it.each(["member", "disabled flag"])(
  "does not request a subscription for an active trial with %s",
  async (reason) => {
    if (reason === "member") mocks.hasScope.mockReturnValue(false);
    else mocks.flag.mockReturnValue({ status: "disabled" });
    const fetcher = setup();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });
    expect(fetcher).not.toHaveBeenCalled();
    expect(screen.getByRole("progressbar")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: /add payment method/i }),
    ).toBeNull();
  },
);
it("reads the subscription for an eligible active trial before offering checkout", async () => {
  const fetcher = setup();
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: /add payment method/i }),
    ).toBeTruthy(),
  );
  expect(fetcher).toHaveBeenCalledTimes(1);
});
