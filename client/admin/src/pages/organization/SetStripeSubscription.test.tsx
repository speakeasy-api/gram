import { useRef, type JSX } from "react";
import { QueryClient, useQuery } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
import { type AdminOrganization } from "@/lib/gramAdminApi";
import { WriteReportContext } from "@/pages/organizations/writeReport";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { SetStripeSubscription } from "./SetStripeSubscription";

const mocks = vi.hoisted(() => ({
  getStripeSubscriptionCandidate: vi.fn(),
  setStripeSubscription: vi.fn(),
  getOrganization: vi.fn(),
}));

vi.mock("@/lib/gramAdminClient", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  return {
    ...actual,
    getStripeSubscriptionCandidate: mocks.getStripeSubscriptionCandidate,
    setStripeSubscription: mocks.setStripeSubscription,
  };
});

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  return { ...actual, getOrganization: mocks.getOrganization };
});

const ORG = anOrganization({
  id: "org_placeholder_one",
  name: "Example Org",
  slug: "example-org",
  account_type: "payg",
  stripe_customer_id: "cus_placeholder_1",
});

const CANDIDATE = {
  id: "sub_placeholder_1",
  customerId: "cus_placeholder_1",
  status: "active",
};

function queryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
}

function CachedEditor({ org }: { org: AdminOrganization }): JSX.Element | null {
  const heading = useRef<HTMLHeadingElement>(null);
  const { data } = useQuery(organizationQuery(org.slug));
  return data ? (
    <>
      <h2 ref={heading} tabIndex={-1}>
        Details
      </h2>
      <SetStripeSubscription org={data} focusFallbackRef={heading} />
    </>
  ) : null;
}

async function renderEditor(org: AdminOrganization = ORG) {
  const qc = queryClient();
  mocks.getOrganization.mockResolvedValue(org);
  qc.setQueryData(organizationQuery(org.slug).queryKey, org);
  const announce = vi.fn<(message: string) => void>();
  const showFailure = vi.fn<(message: string | null) => void>();
  const mounted = await renderWithApp(
    <WriteReportContext.Provider value={{ announce, showFailure }}>
      <CachedEditor org={org} />
    </WriteReportContext.Provider>,
    { queryClient: qc },
  );
  return { qc, announce, showFailure, unmount: mounted.unmount };
}

async function enterSubscriptionID(value: string): Promise<HTMLInputElement> {
  fireEvent.click(screen.getByRole("button", { name: "Set subscription ID" }));
  const input = await screen.findByRole("textbox", {
    name: "Stripe subscription ID",
  });
  if (!(input instanceof HTMLInputElement)) {
    throw new Error("subscription ID control is not an input");
  }
  fireEvent.change(input, { target: { value } });
  return input;
}

async function validateSubscription(): Promise<void> {
  fireEvent.click(screen.getByRole("button", { name: "Validate" }));
  await screen.findByRole("status");
}

function deferredCandidate() {
  let resolve!: (value: typeof CANDIDATE) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<typeof CANDIDATE>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function applyDisabled(): boolean {
  const button = screen.getByRole("button", { name: "Apply changes" });
  if (!(button instanceof HTMLButtonElement))
    throw new Error("missing Apply changes button");
  return button.disabled;
}

beforeEach(() => {
  mocks.getStripeSubscriptionCandidate.mockReset();
  mocks.setStripeSubscription.mockReset();
  mocks.getOrganization.mockReset();
  mocks.getStripeSubscriptionCandidate.mockImplementation(
    ({ stripeSubscriptionId }: { stripeSubscriptionId: string }) =>
      Promise.resolve({ ...CANDIDATE, id: stripeSubscriptionId }),
  );
});

afterEach(cleanup);

describe("SetStripeSubscription", () => {
  it("offers entry for free, PAYG, or unconverted enterprise trials with a customer and no subscription", async () => {
    await renderWithApp(
      <div>
        <SetStripeSubscription org={ORG} />
        <SetStripeSubscription
          org={{
            ...ORG,
            id: "subscribed",
            stripe_subscription_id: "sub_existing",
          }}
        />
        <SetStripeSubscription
          org={{ ...ORG, id: "free", account_type: "free" }}
        />
        <SetStripeSubscription
          org={{ ...ORG, id: "enterprise", account_type: "enterprise" }}
        />
        <SetStripeSubscription
          org={{
            ...ORG,
            id: "enterprise-trial",
            whitelisted: true,
            account_type: "enterprise",
            trial_tier: "enterprise",
            trial_state: "running",
          }}
        />
        <SetStripeSubscription
          org={{
            ...ORG,
            id: "enterprise-converted",
            account_type: "enterprise",
            trial_tier: "enterprise",
            trial_state: "converted",
            trial_converted_at: "2026-01-01T00:00:00Z",
          }}
        />
        <SetStripeSubscription
          org={{ ...ORG, id: "pro", account_type: "pro" }}
        />
        <SetStripeSubscription
          org={{
            ...ORG,
            id: "free-no-customer",
            account_type: "free",
            stripe_customer_id: undefined,
          }}
        />
        <SetStripeSubscription
          org={{ ...ORG, id: "nocustomer", stripe_customer_id: undefined }}
        />
        <SetStripeSubscription
          org={{ ...ORG, id: "empty-customer", stripe_customer_id: "" }}
        />
      </div>,
    );

    expect(
      screen.getAllByRole("button", { name: "Set subscription ID" }),
    ).toHaveLength(3);
    expect(
      screen.getByRole("button", { name: "Copy Stripe subscription ID" }),
    ).toBeTruthy();
    expect(
      screen.getAllByText(/Set a Stripe customer ID before recording/),
    ).toHaveLength(3);
  });

  it.each([
    {
      account_type: "payg",
      trial_tier: "enterprise",
      whitelisted: true,
      guidance: "Repair its trial lifecycle",
    },
    {
      account_type: "payg",
      trial_tier: "pro",
      whitelisted: false,
      guidance: "Repair its trial lifecycle",
    },
    {
      account_type: "enterprise",
      trial_tier: "enterprise",
      whitelisted: false,
      guidance: "Review and repair its trial lifecycle and book-a-demo gate",
    },
    {
      account_type: "free",
      trial_tier: "enterprise",
      whitelisted: true,
      guidance: "Review and repair its trial lifecycle and book-a-demo gate",
    },
  ])(
    "requires lifecycle repair for $account_type / $trial_tier / whitelisted=$whitelisted",
    async ({ guidance, ...overrides }) => {
      await renderEditor({ ...ORG, ...overrides });
      expect(
        screen.queryByRole("button", { name: "Set subscription ID" }),
      ).toBeNull();
      expect(screen.getByText(new RegExp(guidance))).toBeTruthy();
      expect(mocks.getStripeSubscriptionCandidate).not.toHaveBeenCalled();
      expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
    },
  );

  it.each([
    {
      account_type: "payg",
      trial_tier: "enterprise",
      whitelisted: true,
      trial_converted_at: "2026-01-01T00:00:00Z",
    },
    { account_type: "free", trial_tier: "enterprise", whitelisted: false },
    { account_type: "enterprise", trial_tier: "enterprise", whitelisted: true },
    { account_type: "free", trial_tier: "pro", whitelisted: true },
  ])(
    "allows compatible lifecycle for $account_type / $trial_tier",
    async (overrides) => {
      await renderEditor({ ...ORG, ...overrides });
      expect(
        screen.getByRole("button", { name: "Set subscription ID" }),
      ).toBeTruthy();
    },
  );

  it("keeps an existing linked subscription visible even when trial lifecycle needs repair", async () => {
    await renderEditor({
      ...ORG,
      trial_tier: "enterprise",
      stripe_subscription_id: "sub_existing",
    });
    expect(
      screen.getByRole("button", { name: "Copy Stripe subscription ID" }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Set subscription ID" }),
    ).toBeNull();
    expect(screen.queryByText(/Repair its trial lifecycle/)).toBeNull();
  });

  it("shows a trailing icon for idle, validating, error, and valid states", async () => {
    const pending = deferredCandidate();
    mocks.getStripeSubscriptionCandidate.mockReturnValueOnce(pending.promise);
    await renderEditor();
    const input = await enterSubscriptionID("sub_placeholder_1");
    const button = screen.getByRole("button", { name: "Validate" });
    const icon = (state: string) =>
      button.querySelector(`[data-slot="validation-${state}"]`);
    expect(icon("idle")).not.toBeNull();
    fireEvent.click(button);
    expect(icon("spinner")).not.toBeNull();
    expect(screen.getByRole("button", { name: "Validate" })).toBe(button);
    expect(button.textContent).toBe("Validate");
    expect(button.getAttribute("aria-busy")).toBe("true");
    expect(button.hasAttribute("disabled")).toBe(true);
    await act(async () =>
      pending.reject(new Error("Subscription is canceled")),
    );
    expect(icon("error")).not.toBeNull();
    expect(button.classList.contains("text-destructive")).toBe(true);
    expect(button.classList.contains("border-destructive")).toBe(true);
    expect(button.getAttribute("aria-busy")).toBe("false");
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Subscription is canceled",
    );
    fireEvent.change(input, { target: { value: "sub_placeholder_2" } });
    expect(icon("idle")).not.toBeNull();
    fireEvent.click(button);
    await waitFor(() => expect(icon("success")).not.toBeNull());
    expect(button.classList.contains("text-success-foreground")).toBe(true);
    expect(button.classList.contains("border-success-border")).toBe(true);
    expect(button.classList.contains("text-destructive")).toBe(false);
    expect(icon("success")?.getAttribute("aria-hidden")).toBe("true");
    fireEvent.change(input, { target: { value: "sub_placeholder_3" } });
    expect(icon("idle")).not.toBeNull();
    expect(button.classList.contains("text-success-foreground")).toBe(false);
    expect(button.classList.contains("text-destructive")).toBe(false);
    expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
  });

  it("requires explicit validation and clears it on any edit or close", async () => {
    await renderEditor();
    const input = await enterSubscriptionID("");
    const validate = screen.getByRole("button", { name: "Validate" });
    expect(validate).toHaveProperty("disabled", true);
    expect(applyDisabled()).toBe(true);
    fireEvent.change(input, { target: { value: "sub_placeholder_1" } });
    expect(validate).toHaveProperty("disabled", false);
    expect(applyDisabled()).toBe(true);
    expect(mocks.getStripeSubscriptionCandidate).not.toHaveBeenCalled();
    fireEvent.click(validate);
    await screen.findByRole("status");
    expect(applyDisabled()).toBe(false);
    fireEvent.change(input, { target: { value: "sub_placeholder_1 " } });
    expect(applyDisabled()).toBe(true);
    expect(screen.queryByRole("status")).toBeNull();
    fireEvent.click(validate);
    await screen.findByRole("status");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await enterSubscriptionID("sub_placeholder_1");
    expect(applyDisabled()).toBe(true);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it.each(["resolve", "reject"] as const)(
    "ignores stale validation %s after A to B to A edits",
    async (outcome) => {
      const stale = deferredCandidate();
      mocks.getStripeSubscriptionCandidate.mockReturnValueOnce(stale.promise);
      await renderEditor();
      const input = await enterSubscriptionID("sub_placeholder_1");
      fireEvent.click(screen.getByRole("button", { name: "Validate" }));
      fireEvent.change(input, { target: { value: "sub_other" } });
      fireEvent.change(input, { target: { value: "sub_placeholder_1" } });
      await act(async () => {
        if (outcome === "resolve") stale.resolve(CANDIDATE);
        else stale.reject(new Error("stale failure"));
      });
      expect(applyDisabled()).toBe(true);
      expect(screen.queryByRole("status")).toBeNull();
      expect(screen.queryByRole("alert")).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Validate" }));
      await screen.findByRole("status");
      expect(applyDisabled()).toBe(false);
    },
  );

  it.each([
    { id: "org_other" },
    { stripe_customer_id: "cus_other" },
    { account_type: "free" },
    { trial_tier: "enterprise", trial_converted_at: "2026-01-01T00:00:00Z" },
    { whitelisted: true },
  ])(
    "discards pending and completed validation when context changes: %j",
    async (change) => {
      const stale = deferredCandidate();
      mocks.getStripeSubscriptionCandidate.mockReturnValueOnce(stale.promise);
      const { qc } = await renderEditor();
      await enterSubscriptionID("sub_placeholder_1");
      fireEvent.click(screen.getByRole("button", { name: "Validate" }));
      await act(async () => {
        qc.setQueryData(organizationQuery(ORG.slug).queryKey, {
          ...ORG,
          ...change,
        });
        await new Promise<void>((resolve) => {
          setTimeout(resolve, 0);
        });
      });
      await act(async () => {
        stale.resolve(CANDIDATE);
      });
      expect(applyDisabled()).toBe(true);
      expect(screen.queryByRole("status")).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Validate" }));
      await screen.findByRole("status");
      await act(async () => {
        qc.setQueryData(organizationQuery(ORG.slug).queryKey, ORG);
        await new Promise<void>((resolve) => {
          setTimeout(resolve, 0);
        });
      });
      expect(applyDisabled()).toBe(true);
      expect(screen.queryByRole("status")).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Apply changes" }));
      expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
    },
  );

  it("ignores validation results after closing or unmounting", async () => {
    const stale = deferredCandidate();
    mocks.getStripeSubscriptionCandidate.mockReturnValueOnce(stale.promise);
    const { unmount, showFailure } = await renderEditor();
    await enterSubscriptionID("sub_placeholder_1");
    fireEvent.click(screen.getByRole("button", { name: "Validate" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await enterSubscriptionID("sub_placeholder_1");
    await act(async () => {
      stale.resolve(CANDIDATE);
    });
    expect(applyDisabled()).toBe(true);
    const afterUnmount = deferredCandidate();
    mocks.getStripeSubscriptionCandidate.mockReturnValueOnce(
      afterUnmount.promise,
    );
    fireEvent.click(screen.getByRole("button", { name: "Validate" }));
    showFailure.mockClear();
    unmount();
    await act(async () => {
      afterUnmount.reject(new Error("late failure"));
    });
    expect(showFailure).not.toHaveBeenCalled();
    expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
  });

  it("rejects an oversized ID locally during explicit validation", async () => {
    await renderEditor();
    await enterSubscriptionID(`sub_${"a".repeat(255)}`);
    fireEvent.click(screen.getByRole("button", { name: "Validate" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "255 characters or fewer",
    );
    expect(applyDisabled()).toBe(true);
    expect(mocks.getStripeSubscriptionCandidate).not.toHaveBeenCalled();
  });

  it("rejects a malformed ID before confirmation or a request", async () => {
    await renderEditor();
    await enterSubscriptionID(" subscription_placeholder ");
    fireEvent.click(screen.getByRole("button", { name: "Validate" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "beginning with sub_",
    );
    expect(mocks.getStripeSubscriptionCandidate).not.toHaveBeenCalled();
    expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
  });

  it.each([
    ["active", "Active"],
    ["trialing", "Trialing"],
    ["past_due", "Past due"],
  ])("shows a compact human-readable %s status", async (status, label) => {
    mocks.getStripeSubscriptionCandidate.mockResolvedValue({
      ...CANDIDATE,
      status,
    });
    await renderEditor();
    await enterSubscriptionID("sub_placeholder_1");
    await validateSubscription();
    expect(screen.getByRole("status").textContent).toContain(
      `Valid subscription · ${label}`,
    );
  });

  it("keeps the independent disabled organization state clear", async () => {
    await renderEditor({
      ...ORG,
      account_type: "free",
      disabled_at: "2026-01-01T00:00:00Z",
    });
    await enterSubscriptionID("sub_placeholder_1");
    await validateSubscription();
    expect(
      screen.getByText("This organization will remain disabled."),
    ).toBeTruthy();
  });

  it("shows the verified customer match in a single modal before applying", async () => {
    const { announce } = await renderEditor();
    await enterSubscriptionID("  sub_placeholder_1  ");
    await validateSubscription();

    const heading = await screen.findByRole("heading", {
      name: "Set Stripe subscription ID",
    });
    const dialog = heading.closest('[role="dialog"]');
    if (!(dialog instanceof HTMLElement)) {
      throw new Error("subscription heading is not in a dialog");
    }
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
    expect(within(dialog).getByRole("status").textContent).toContain(
      "Valid subscription · Active",
    );
    expect(dialog.textContent).toContain(
      "Matches this organization’s Stripe customer.",
    );
    expect(dialog.textContent).toContain(
      "Applying changes will link this subscription. Account type and access stay unchanged.",
    );
    expect(dialog.textContent).not.toContain(ORG.stripe_customer_id);
    expect(dialog.textContent).not.toContain(ORG.id);
    expect(dialog.textContent).not.toContain("sub_placeholder_1");
    expect(dialog.textContent).not.toContain(
      "The current trial will be marked converted.",
    );
    expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
    expect(mocks.getStripeSubscriptionCandidate).toHaveBeenCalledWith({
      organizationId: ORG.id,
      stripeSubscriptionId: "sub_placeholder_1",
    });

    mocks.setStripeSubscription.mockResolvedValue({
      ...ORG,
      stripe_subscription_id: "sub_placeholder_1",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Apply changes" }),
    );
    await waitFor(() => {
      expect(mocks.setStripeSubscription.mock.calls[0]?.[0]).toEqual({
        organizationId: ORG.id,
        stripeSubscriptionId: "sub_placeholder_1",
      });
    });
    await waitFor(() => {
      expect(announce).toHaveBeenCalledWith(
        `Set Stripe subscription ID sub_placeholder_1 for ${ORG.name}.`,
      );
    });
  });

  it.each([
    { ...ORG, account_type: "free", whitelisted: false },
    {
      ...ORG,
      account_type: "enterprise",
      trial_tier: "enterprise",
      whitelisted: true,
    },
  ])(
    "shows PAYG activation and lifecycle effects inline for $account_type",
    async (org) => {
      const { announce, qc } = await renderEditor(org);
      await enterSubscriptionID("sub_placeholder_1");
      await validateSubscription();

      const heading = await screen.findByRole("heading", {
        name: "Set Stripe subscription ID",
      });
      const dialog = heading.closest('[role="dialog"]');
      if (!(dialog instanceof HTMLElement))
        throw new Error("missing subscription dialog");
      expect(screen.getAllByRole("dialog")).toHaveLength(1);
      expect(dialog.textContent).toContain(
        `Link an existing subscription to ${ORG.name}.`,
      );
      expect(dialog.textContent).toContain(
        `Applying changes will switch ${ORG.name} from ${org.account_type === "free" ? "Free" : "Enterprise"} to PAYG.`,
      );
      if (org.trial_tier) {
        expect(dialog.textContent).toContain(
          "The current trial will be marked converted.",
        );
      } else {
        expect(dialog.textContent).not.toContain(
          "The current trial will be marked converted.",
        );
      }
      expect(mocks.setStripeSubscription).not.toHaveBeenCalled();

      const updated = {
        ...ORG,
        stripe_subscription_id: "sub_placeholder_1",
        whitelisted: true,
      };
      mocks.getOrganization.mockResolvedValue(updated);
      mocks.setStripeSubscription.mockResolvedValue(updated);
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Apply changes" }),
      );
      await waitFor(() => {
        expect(announce).toHaveBeenCalledWith(
          `Activated PAYG and linked Stripe subscription ID sub_placeholder_1 for ${ORG.name}.`,
        );
      });
      expect(qc.getQueryData(organizationQuery(ORG.slug).queryKey)).toEqual(
        updated,
      );
      expect(
        screen.queryByRole("button", { name: "Set subscription ID" }),
      ).toBeNull();
    },
  );

  it("refreshes organization state after a write conflict and lets the operator retry verification", async () => {
    const { showFailure } = await renderEditor({
      ...ORG,
      account_type: "free",
    });
    mocks.getOrganization.mockResolvedValue({
      ...ORG,
      name: "Updated Example Org",
    });
    mocks.setStripeSubscription.mockRejectedValueOnce(
      Object.assign(new Error("organization changed; refresh and retry"), {
        statusCode: 409,
      }),
    );
    await enterSubscriptionID("sub_placeholder_1");
    await validateSubscription();
    fireEvent.click(
      await screen.findByRole("button", { name: "Apply changes" }),
    );
    await waitFor(() =>
      expect(showFailure).toHaveBeenCalledWith(
        expect.stringContaining("organization changed"),
      ),
    );
    expect(applyDisabled()).toBe(true);
    await screen.findByText(
      "Link an existing subscription to Updated Example Org.",
    );
    expect((await screen.findByRole("alert")).textContent).toContain(
      "organization changed",
    );
    await validateSubscription();
    expect(applyDisabled()).toBe(false);
    expect(
      screen.getByText(
        "Applying changes will link this subscription. Account type and access stay unchanged.",
      ),
    ).toBeTruthy();
  });

  it("reports a verification failure without saving", async () => {
    mocks.getStripeSubscriptionCandidate.mockRejectedValue(
      Object.assign(
        new Error(
          "Stripe subscription does not belong to the organization's Stripe customer",
        ),
        { statusCode: 409 },
      ),
    );
    const { announce } = await renderEditor();
    await enterSubscriptionID("sub_other");
    fireEvent.click(screen.getByRole("button", { name: "Validate" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "does not belong",
    );
    expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
    expect(announce).toHaveBeenCalledWith(
      expect.stringContaining("does not belong"),
    );
  });
});
