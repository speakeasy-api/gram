import { QueryClient } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  getInferenceKeys: vi.fn(),
  listOrganizationProjects: vi.fn(),
  getPaygBillingSummary: vi.fn(),
  getStripeSubscription: vi.fn(),
  cancelStripeSubscription: vi.fn(),
  resumeStripeSubscription: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return { ...actual, ...mocks };
});

const ORG = anOrganization({ account_type: "payg" });
const SUBSCRIPTION = {
  status: "active" as const,
  current_period_start: "2026-08-01T00:00:00Z",
  current_period_end: "2026-09-01T00:00:00Z",
  cancel_at_period_end: false,
  payment_failed: false,
};
const SUMMARY = {
  period_start: "2026-08-01T00:00:00Z",
  period_end: "2026-09-01T00:00:00Z",
  tum_tokens: 1_234_567,
  tum_unit_price_usd: "0.00000035",
  tum_cost_usd: "0.43209845",
  other_inference_spend_usd: "2.100000",
  recorded_through: "2026-08-14",
  estimated_total_usd: "2.53209845",
};

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockResolvedValue(ORG);
  mocks.getInferenceKeys.mockResolvedValue([
    {
      key_type: "chat",
      monthly_credits: 100,
      disabled: false,
    },
    {
      key_type: "internal",
      monthly_credits: 0,
      disabled: true,
    },
    {
      key_type: "future-purpose",
      monthly_credits: 25,
      disabled: false,
    },
  ]);
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.getStripeSubscription.mockImplementation((organizationID: string) =>
    organizationID === ORG.id
      ? Promise.resolve(SUBSCRIPTION)
      : Promise.reject(new Error(`unexpected organization ${organizationID}`)),
  );
  mocks.getPaygBillingSummary.mockImplementation((organizationID: string) =>
    organizationID === ORG.id
      ? Promise.resolve(SUMMARY)
      : Promise.reject(new Error(`unexpected organization ${organizationID}`)),
  );
  mocks.cancelStripeSubscription.mockResolvedValue({
    ...SUBSCRIPTION,
    cancel_at_period_end: true,
  });
  mocks.resumeStripeSubscription.mockResolvedValue(SUBSCRIPTION);
});

afterEach(cleanup);

async function renderBilling(): Promise<QueryClient> {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  await renderRouteTree(routeTree, {
    initialPath: `/organizations/${ORG.slug}/billing`,
    queryClient,
  });
  return queryClient;
}

describe("Billing", () => {
  it("renders exact current-cycle usage and payment state", async () => {
    await renderBilling();

    expect(await screen.findByText("1,234,567")).toBeTruthy();
    expect(screen.getByText("$0.00000035 per token")).toBeTruthy();
    expect(screen.getByText("$2.53209845")).toBeTruthy();
    expect(screen.getByText("No payment failure reported")).toBeTruthy();
    expect(screen.getByText("Other inference")).toBeTruthy();
    expect(screen.getByText("Security and internal inference")).toBeTruthy();
    expect(screen.getByText("Platform-managed inference")).toBeTruthy();
    expect(screen.getByText("$100.00")).toBeTruthy();
    expect(screen.getByText("$25.00")).toBeTruthy();
    expect(screen.getByText("Unlimited")).toBeTruthy();
    expect(screen.getAllByText("Configured monthly credit limit")).toHaveLength(
      3,
    );
    expect(screen.getAllByText("Enabled")).toHaveLength(2);
    expect(screen.getByText("Disabled")).toBeTruthy();
    expect(screen.getByText(/This is an estimate, not a bill/)).toBeTruthy();
    expect(mocks.getInferenceKeys).toHaveBeenCalledWith(ORG.id);
    expect(mocks.getStripeSubscription).toHaveBeenCalledWith(ORG.id);
    expect(mocks.getPaygBillingSummary).toHaveBeenCalledWith(ORG.id);
  });

  it.each(["incomplete", "paused"] as const)(
    "reports a %s subscription as not collecting",
    async (status) => {
      mocks.getStripeSubscription.mockResolvedValue({
        ...SUBSCRIPTION,
        status,
      });

      await renderBilling();

      expect(await screen.findByText("Not collecting")).toBeTruthy();
      expect(screen.queryByText("No payment failure reported")).toBeNull();
    },
  );

  it("hides a cached billing summary after the subscription ends", async () => {
    const queryClient = await renderBilling();
    expect(await screen.findByText("$2.53209845")).toBeTruthy();

    act(() => {
      queryClient.setQueryData(["gram-admin-stripe-subscription", ORG.id], {
        ...SUBSCRIPTION,
        status: "canceled",
      });
    });

    await vi.waitFor(() => {
      expect(screen.queryByText("$2.53209845")).toBeNull();
    });
    expect(screen.getByText("canceled")).toBeTruthy();
    expect(mocks.getPaygBillingSummary).toHaveBeenCalledTimes(1);
  });

  it("confirms cancellation and sends the canonical organization id", async () => {
    await renderBilling();
    const cancel = await screen.findByRole("button", {
      name: "Cancel pay as you go",
    });
    fireEvent.click(cancel);
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Cancel pay as you go" }),
    );

    await vi.waitFor(() => {
      expect(mocks.cancelStripeSubscription).toHaveBeenCalledWith(ORG.id);
    });
  });

  it("shows payment failure and resumes a scheduled cancellation", async () => {
    mocks.getStripeSubscription.mockResolvedValue({
      ...SUBSCRIPTION,
      status: "past_due",
      payment_failed: true,
      cancel_at_period_end: true,
      cancel_at: "2026-09-01T00:00:00Z",
    });
    await renderBilling();

    expect((await screen.findByRole("alert")).textContent).toContain(
      "latest invoice has an unpaid balance",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Resume pay as you go" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Resume pay as you go" }),
    );
    await vi.waitFor(() => {
      expect(mocks.resumeStripeSubscription).toHaveBeenCalledWith(ORG.id);
    });
  });
});
