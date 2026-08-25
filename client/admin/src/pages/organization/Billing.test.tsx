import { QueryClient } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GramAdminError } from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  getInferenceKeys: vi.fn(),
  getInferenceSpendHistory: vi.fn(),
  listOrganizationProjects: vi.fn(),
  getPaygBillingSummary: vi.fn(),
  getStripeSubscription: vi.fn(),
  cancelStripeSubscription: vi.fn(),
  resumeStripeSubscription: vi.fn(),
  setInferenceKeyMonthlyLimit: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return { ...actual, ...mocks };
});

const ORG = anOrganization({ account_type: "payg" });
const HISTORY = [
  {
    period_start: "2026-06-01",
    period_end: "2026-07-01",
    spend_usd: "3.250000",
  },
  {
    period_start: "2026-07-01",
    period_end: "2026-08-01",
    spend_usd: "4.750000",
  },
];

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
      credits_used: 42.75,
      monthly_credits: 100,
      disabled: false,
    },
    {
      key_type: "internal",
      credits_used: 12.5,
      monthly_credits: 0,
      disabled: true,
    },
    {
      key_type: "future-purpose",
      credits_used: 1.25,
      monthly_credits: 25,
      disabled: false,
    },
  ]);
  mocks.getInferenceSpendHistory.mockImplementation((organizationID: string) =>
    organizationID === ORG.id
      ? Promise.resolve(HISTORY)
      : Promise.reject(new Error(`unexpected organization ${organizationID}`)),
  );
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
  mocks.setInferenceKeyMonthlyLimit.mockImplementation(
    ({
      keyType,
      monthlyCredits,
    }: {
      keyType: string;
      monthlyCredits: number;
    }) =>
      Promise.resolve({
        key_type: keyType,
        credits_used: 0,
        monthly_credits: monthlyCredits,
        disabled: false,
      }),
  );
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
  it("shows an empty state when the organization has no subscription", async () => {
    mocks.getStripeSubscription.mockRejectedValue(
      new GramAdminError(404, null, "gram admin 404"),
    );

    await renderBilling();

    expect(
      await screen.findByText("This organization has no Stripe subscription."),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(mocks.getPaygBillingSummary).not.toHaveBeenCalled();
  });

  it("renders exact current-cycle usage and payment state", async () => {
    await renderBilling();

    expect(await screen.findByText("1,234,567")).toBeTruthy();
    expect(screen.getByText("$0.00000035 per token")).toBeTruthy();
    expect(screen.getByText("$2.53209845")).toBeTruthy();
    expect(screen.getByText("Inference spend")).toBeTruthy();
    expect(screen.getByText("$2.10")).toBeTruthy();
    expect(screen.getByText("No payment failure reported")).toBeTruthy();
    expect(screen.getByText("Other inference")).toBeTruthy();
    expect(screen.getByText("Security and internal inference")).toBeTruthy();
    expect(screen.getByText("Platform-managed inference")).toBeTruthy();
    expect(screen.getByText("$42.75 of $100.00")).toBeTruthy();
    expect(screen.getByText("$12.50 (unlimited limit)")).toBeTruthy();
    expect(screen.getByText("$1.25 of $25.00")).toBeTruthy();
    expect(screen.getByText("$100.00")).toBeTruthy();
    expect(screen.getByText("$25.00")).toBeTruthy();
    expect(screen.getByText("Unlimited")).toBeTruthy();
    expect(screen.getAllByText("Current monthly usage")).toHaveLength(3);
    expect(screen.getAllByText("Configured monthly credit limit")).toHaveLength(
      3,
    );
    expect(screen.getAllByText("Enabled")).toHaveLength(2);
    expect(screen.getByText("Disabled")).toBeTruthy();
    expect(screen.getByText(/This is an estimate, not a bill/)).toBeTruthy();
    expect(screen.getByLabelText("Monthly inference spend graph")).toBeTruthy();
    expect(screen.getByText("$3.25")).toBeTruthy();
    expect(screen.getByText("$4.75")).toBeTruthy();
    expect(mocks.getInferenceKeys).toHaveBeenCalledWith(ORG.id);
    expect(mocks.getInferenceSpendHistory).toHaveBeenCalledWith(ORG.id);
    expect(mocks.getStripeSubscription).toHaveBeenCalledWith(ORG.id);
    expect(mocks.getPaygBillingSummary).toHaveBeenCalledWith(ORG.id);
  });

  it("hides the graph until two consecutive complete months are available", async () => {
    mocks.getInferenceSpendHistory.mockResolvedValue(HISTORY.slice(0, 1));

    await renderBilling();

    expect(await screen.findByText("$3.25")).toBeTruthy();
    expect(screen.queryByLabelText("Monthly inference spend graph")).toBeNull();
  });

  it("shows the graph when the latest two months are consecutive after a gap", async () => {
    mocks.getInferenceSpendHistory.mockResolvedValue([
      {
        period_start: "2026-04-01",
        period_end: "2026-05-01",
        spend_usd: "1.000000",
      },
      ...HISTORY,
    ]);

    await renderBilling();

    expect(
      await screen.findByLabelText("Monthly inference spend graph"),
    ).toBeTruthy();
  });

  it("shows a loaded unlimited key without an error but rejects zero as an edit", async () => {
    mocks.getInferenceKeys.mockResolvedValue([
      {
        key_type: "chat",
        credits_used: 4.25,
        monthly_credits: 0,
        disabled: false,
      },
    ]);

    await renderBilling();

    expect(await screen.findByText("Unlimited")).toBeTruthy();
    const input = screen.getByRole("spinbutton", {
      name: "chat monthly limit in USD",
    });
    const button = within(input.closest("form")!).getByRole("button");
    expect((input as HTMLInputElement).value).toBe("0");
    expect(input.getAttribute("aria-invalid")).toBe("false");
    expect(screen.queryByText(/Enter a whole-dollar limit/)).toBeNull();
    expect((button as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(input, { target: { value: "" } });
    fireEvent.change(input, { target: { value: "0" } });

    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByText(/Enter a whole-dollar limit/)).toBeTruthy();
    fireEvent.submit(input.closest("form")!);
    expect(mocks.setInferenceKeyMonthlyLimit).not.toHaveBeenCalled();
  });

  it("updates one materialized key with the canonical organization id and refreshes the keys", async () => {
    await renderBilling();
    const input = await screen.findByRole("spinbutton", {
      name: "chat monthly limit in USD",
    });
    fireEvent.change(input, { target: { value: "750" } });
    fireEvent.click(within(input.closest("form")!).getByRole("button"));

    await vi.waitFor(() => {
      expect(mocks.setInferenceKeyMonthlyLimit).toHaveBeenCalledWith({
        organizationID: ORG.id,
        keyType: "chat",
        monthlyCredits: 750,
      });
      expect(mocks.getInferenceKeys).toHaveBeenCalledTimes(2);
    });
  });

  it("validates limits and keeps disabled or unsupported keys read-only", async () => {
    let resolveUpdate: (() => void) | undefined;
    mocks.setInferenceKeyMonthlyLimit.mockReturnValue(
      new Promise((resolve) => {
        resolveUpdate = () =>
          resolve({
            key_type: "chat",
            credits_used: 42.75,
            monthly_credits: 750,
            disabled: false,
          });
      }),
    );
    await renderBilling();

    const chatInput = await screen.findByRole("spinbutton", {
      name: "chat monthly limit in USD",
    });
    const internalInput = screen.getByRole("spinbutton", {
      name: "internal monthly limit in USD",
    });
    expect((internalInput as HTMLInputElement).disabled).toBe(true);
    expect(
      screen.queryByRole("spinbutton", {
        name: "future-purpose monthly limit in USD",
      }),
    ).toBeNull();

    fireEvent.change(chatInput, { target: { value: "10001" } });
    expect(chatInput.getAttribute("aria-invalid")).toBe("true");
    expect(
      (
        within(chatInput.closest("form")!).getByRole(
          "button",
        ) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(mocks.setInferenceKeyMonthlyLimit).not.toHaveBeenCalled();

    fireEvent.change(chatInput, { target: { value: "750" } });
    const chatButton = within(chatInput.closest("form")!).getByRole("button");
    fireEvent.click(chatButton);
    await vi.waitFor(() => expect(chatButton.textContent).toBe("Saving…"));
    expect((chatButton as HTMLButtonElement).disabled).toBe(true);

    resolveUpdate?.();
    await vi.waitFor(() => expect(chatButton.textContent).toBe("Save limit"));
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
    expect(screen.getByLabelText("Monthly inference spend graph")).toBeTruthy();
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
