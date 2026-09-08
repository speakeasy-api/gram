import type { JSX } from "react";
import { QueryClient, useQuery } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock,
} from "vitest";

import {
  organizationQuery,
  organizationsListQuery,
  paygBillingSummaryQuery,
  stripeSubscriptionQuery,
} from "@/lib/adminQueries";
import { GramAdminError, type AdminOrganization } from "@/lib/gramAdminApi";
import { organizationActivityQuery } from "@/lib/gramAdminClient";
import { WriteReportContext } from "@/pages/organizations/writeReport";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { SetStripeCustomer } from "./SetStripeCustomer";

const mocks = vi.hoisted(() => ({
  getStripeCustomer: vi.fn(),
  getOrganization: vi.fn(),
  setStripeCustomer: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  return {
    ...actual,
    getStripeCustomer: mocks.getStripeCustomer,
    getOrganization: mocks.getOrganization,
    setStripeCustomer: mocks.setStripeCustomer,
  };
});

const ORG = anOrganization({
  id: "org_placeholder_one",
  name: "Example Org",
  slug: "example-org",
});

const STRIPE_CUSTOMER = {
  id: "cus_placeholder_1",
  name: "Example Billing Customer",
  email: "billing@example.test",
  description: "Example Org billing",
  livemode: true,
};

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function queryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
}

function CachedEditor(): JSX.Element | null {
  const { data } = useQuery(organizationQuery(ORG.slug));
  return data ? <SetStripeCustomer org={data} /> : null;
}

async function renderEditor(org: AdminOrganization = ORG): Promise<{
  qc: QueryClient;
  announce: Mock;
  showFailure: Mock;
  unmount: () => void;
}> {
  const qc = queryClient();
  qc.setQueryData(organizationQuery(org.id).queryKey, org);
  qc.setQueryData(organizationQuery(org.slug).queryKey, org);
  qc.setQueryData(organizationsListQuery().queryKey, {
    organizations: [org],
  });
  const announce = vi.fn<(message: string) => void>();
  const showFailure = vi.fn<(message: string | null) => void>();
  mocks.getOrganization.mockResolvedValue(org);
  const mounted = await renderWithApp(
    <WriteReportContext.Provider value={{ announce, showFailure }}>
      <CachedEditor />
    </WriteReportContext.Provider>,
    { queryClient: qc },
  );
  return { qc, announce, showFailure, unmount: mounted.unmount };
}

async function enterCustomerID(value: string): Promise<HTMLInputElement> {
  fireEvent.click(screen.getByRole("button", { name: "Set customer ID" }));
  const input = await screen.findByRole("textbox", {
    name: "Stripe customer ID",
  });
  if (!(input instanceof HTMLInputElement)) {
    throw new Error("customer ID control is not an input");
  }
  fireEvent.change(input, { target: { value } });
  return input;
}

async function reviewCustomerID(): Promise<HTMLElement> {
  await waitFor(() => {
    expect(
      screen
        .getByRole("button", { name: "Review and set" })
        .hasAttribute("disabled"),
    ).toBe(false);
  });
  fireEvent.click(screen.getByRole("button", { name: "Review and set" }));
  const heading = await screen.findByRole("heading", {
    name: `Set Stripe customer for ${ORG.name}?`,
  });
  const dialog = heading.closest('[role="dialog"]');
  if (!(dialog instanceof HTMLElement)) {
    throw new Error("confirmation heading is not in a dialog");
  }
  return dialog;
}

function confirmationValue(dialog: HTMLElement, label: string): string | null {
  return (
    within(dialog).getByText(label).nextElementSibling?.textContent ?? null
  );
}

beforeEach(() => {
  mocks.getStripeCustomer.mockReset();
  mocks.getOrganization.mockReset();
  mocks.setStripeCustomer.mockReset();
  mocks.getStripeCustomer.mockImplementation(
    (_organizationID: string, stripeCustomerID: string) =>
      Promise.resolve({ ...STRIPE_CUSTOMER, id: stripeCustomerID }),
  );
});

afterEach(cleanup);

describe("SetStripeCustomer", () => {
  it("offers the setter only when both Stripe identifiers are absent", async () => {
    const qc = queryClient();
    await renderWithApp(
      <div>
        <SetStripeCustomer org={ORG} />
        <SetStripeCustomer
          org={{ ...ORG, id: "customer", stripe_customer_id: "cus_existing" }}
        />
        <SetStripeCustomer
          org={{
            ...ORG,
            id: "subscription",
            stripe_subscription_id: "sub_existing",
          }}
        />
        <SetStripeCustomer
          org={{ ...ORG, id: "empty-customer", stripe_customer_id: "" }}
        />
      </div>,
      { queryClient: qc },
    );

    expect(
      screen.getAllByRole("button", { name: "Set customer ID" }),
    ).toHaveLength(1);
    expect(
      screen.getByRole("button", { name: "Copy Stripe customer ID" }),
    ).toBeTruthy();
  });

  it("rejects malformed IDs before confirmation or a request", async () => {
    await renderEditor();
    await enterCustomerID(" customer_placeholder ");
    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "beginning with cus_",
    );
    expect(
      screen.queryByRole("heading", { name: /Set Stripe customer for/ }),
    ).toBeNull();

    fireEvent.change(
      screen.getByRole("textbox", { name: "Stripe customer ID" }),
      { target: { value: `cus_${"a".repeat(252)}` } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "255 characters or fewer",
    );
    expect(mocks.getStripeCustomer).not.toHaveBeenCalled();
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
  });

  it("waits for a fresh Stripe lookup before offering final confirmation", async () => {
    const lookup = deferred<typeof STRIPE_CUSTOMER>();
    mocks.getStripeCustomer.mockReturnValue(lookup.promise);
    await renderEditor();
    await enterCustomerID("cus_placeholder_1");

    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));
    await waitFor(() => {
      expect(mocks.getStripeCustomer).toHaveBeenCalledWith(
        ORG.id,
        "cus_placeholder_1",
      );
    });
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("heading", { name: /Set Stripe customer for/ }),
    ).toBeNull();
    expect(
      screen
        .getByRole("button", { name: "Review and set" })
        .hasAttribute("disabled"),
    ).toBe(true);

    lookup.resolve(STRIPE_CUSTOMER);
    const confirmation = await screen.findByRole("heading", {
      name: `Set Stripe customer for ${ORG.name}?`,
    });
    const dialog = confirmation.closest('[role="dialog"]');
    if (!(dialog instanceof HTMLElement)) {
      throw new Error("confirmation heading is not in a dialog");
    }
    expect(confirmationValue(dialog, "Stripe returned ID")).toBe(
      "cus_placeholder_1",
    );
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
  });

  it("does not open confirmation or mutate after the reviewed organization unmounts", async () => {
    const lookup = deferred<typeof STRIPE_CUSTOMER>();
    mocks.getStripeCustomer.mockReturnValue(lookup.promise);
    const { unmount } = await renderEditor();
    await enterCustomerID("cus_placeholder_1");
    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));
    await waitFor(() => {
      expect(mocks.getStripeCustomer).toHaveBeenCalled();
    });

    unmount();
    lookup.resolve(STRIPE_CUSTOMER);
    await Promise.resolve();

    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("heading", { name: /Set Stripe customer for/ }),
    ).toBeNull();
  });

  it("reports lookup errors without blocking retries and clears them on edit", async () => {
    mocks.getStripeCustomer.mockRejectedValue(
      new GramAdminError(
        404,
        { message: "Stripe customer was not found" },
        "gram admin 404 Not Found",
      ),
    );
    const { announce, showFailure } = await renderEditor();
    const input = await enterCustomerID("cus_missing");

    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "Stripe customer was not found",
    );
    expect(input.value).toBe("cus_missing");
    expect(
      screen.queryByRole("heading", { name: /Set Stripe customer for/ }),
    ).toBeNull();
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
    const report = `Could not verify Stripe customer ID for ${ORG.name}: Stripe customer was not found`;
    expect(announce).toHaveBeenCalledWith(report);
    expect(showFailure).toHaveBeenCalledWith(report);

    mocks.getStripeCustomer.mockResolvedValue({
      ...STRIPE_CUSTOMER,
      id: "cus_missing",
    });
    const confirmation = await reviewCustomerID();
    expect(mocks.getStripeCustomer).toHaveBeenCalledTimes(2);
    fireEvent.click(
      within(confirmation).getByRole("button", { name: "Cancel" }),
    );
    await waitFor(() => {
      expect(input.disabled).toBe(false);
    });
    expect(screen.queryByRole("alert")).toBeNull();

    mocks.getStripeCustomer.mockRejectedValue(new Error("Stripe unavailable"));
    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));
    await screen.findByRole("alert");
    fireEvent.change(input, { target: { value: "cus_corrected" } });
    await waitFor(() => {
      expect(screen.queryByRole("alert")).toBeNull();
    });
    expect(mocks.getStripeCustomer).toHaveBeenCalledTimes(3);
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
  });

  it("reviews the trimmed ID and preserves the input when canceled", async () => {
    await renderEditor();
    const input = await enterCustomerID("  cus_placeholder_1  ");
    const confirmation = await reviewCustomerID();

    expect(confirmationValue(confirmation, "Organization")).toBe(
      `${ORG.name} (${ORG.id})`,
    );
    expect(confirmationValue(confirmation, "Requested ID")).toBe(
      "cus_placeholder_1",
    );
    expect(confirmationValue(confirmation, "Stripe returned ID")).toBe(
      "cus_placeholder_1",
    );
    expect(confirmationValue(confirmation, "Name")).toBe(
      "Example Billing Customer",
    );
    expect(confirmationValue(confirmation, "Email")).toBe(
      "billing@example.test",
    );
    expect(confirmationValue(confirmation, "Description")).toBe(
      "Example Org billing",
    );
    expect(confirmationValue(confirmation, "Mode")).toBe("Live");
    const cancel = within(confirmation).getByRole("button", { name: "Cancel" });
    fireEvent.pointerDown(cancel, { button: 0, pointerType: "mouse" });
    fireEvent.pointerUp(cancel, { button: 0, pointerType: "mouse" });
    fireEvent.click(cancel);
    await waitFor(() => {
      expect(screen.getByRole("textbox", { name: "Stripe customer ID" })).toBe(
        input,
      );
      expect(input.disabled).toBe(false);
    });
    expect(mocks.getStripeCustomer).toHaveBeenCalledWith(
      ORG.id,
      "cus_placeholder_1",
    );
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
    expect(input.value).toBe("  cus_placeholder_1  ");

    mocks.getStripeCustomer.mockResolvedValue({
      id: "cus_placeholder_2",
      livemode: false,
    });
    fireEvent.change(input, { target: { value: "cus_placeholder_2" } });
    const secondConfirmation = await reviewCustomerID();
    expect(confirmationValue(secondConfirmation, "Stripe returned ID")).toBe(
      "cus_placeholder_2",
    );
    expect(confirmationValue(secondConfirmation, "Mode")).toBe("Test");
    expect(secondConfirmation.textContent).not.toContain(
      "Example Billing Customer",
    );
    expect(mocks.getStripeCustomer).toHaveBeenLastCalledWith(
      ORG.id,
      "cus_placeholder_2",
    );
    fireEvent.click(
      within(secondConfirmation).getByRole("button", { name: "Cancel" }),
    );
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
  });

  it("sends once, updates every organization cache, and invalidates related views", async () => {
    const request = deferred<AdminOrganization>();
    mocks.setStripeCustomer.mockReturnValue(request.promise);
    const { qc, announce, showFailure } = await renderEditor();
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    await enterCustomerID(" cus_placeholder_1 ");
    const confirmation = await reviewCustomerID();
    expect(mocks.setStripeCustomer).not.toHaveBeenCalled();
    fireEvent.click(
      within(confirmation).getByRole("button", { name: "Set customer ID" }),
    );

    await waitFor(() => {
      expect(mocks.setStripeCustomer.mock.calls[0]?.[0]).toEqual({
        organization_id: ORG.id,
        stripe_customer_id: "cus_placeholder_1",
      });
    });
    const setting = screen.getByRole("button", { name: "Setting…" });
    expect(setting.hasAttribute("disabled")).toBe(true);
    fireEvent.click(setting);
    expect(mocks.setStripeCustomer).toHaveBeenCalledTimes(1);

    const updated = { ...ORG, stripe_customer_id: "cus_placeholder_1" };
    mocks.getOrganization.mockResolvedValue(updated);
    request.resolve(updated);

    expect(
      await screen.findByRole("button", { name: "Copy Stripe customer ID" }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Set customer ID" }),
    ).toBeNull();
    expect(qc.getQueryData(organizationQuery(ORG.id).queryKey)).toEqual(
      updated,
    );
    expect(qc.getQueryData(organizationQuery(ORG.slug).queryKey)).toEqual(
      updated,
    );
    expect(
      qc.getQueryData<{ organizations: AdminOrganization[] }>(
        organizationsListQuery().queryKey,
      )?.organizations[0],
    ).toEqual(updated);
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: organizationActivityQuery(ORG.id).queryKey,
      exact: true,
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: paygBillingSummaryQuery(ORG.id).queryKey,
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: stripeSubscriptionQuery(ORG.id).queryKey,
    });
    expect(showFailure).toHaveBeenCalledWith(null);
    expect(announce).toHaveBeenCalledWith(
      `Set Stripe customer ID cus_placeholder_1 for ${ORG.name}.`,
    );
  });

  it("keeps a rejected ID editable and reports the server failure", async () => {
    mocks.setStripeCustomer.mockRejectedValue(
      new GramAdminError(
        400,
        { message: "invalid Stripe customer ID" },
        "gram admin 400 Bad Request",
      ),
    );
    const { announce, showFailure } = await renderEditor();
    const input = await enterCustomerID("cus_rejected");
    const confirmation = await reviewCustomerID();
    fireEvent.click(
      within(confirmation).getByRole("button", { name: "Set customer ID" }),
    );

    expect((await screen.findByRole("alert")).textContent).toContain(
      "invalid Stripe customer ID",
    );
    expect(input.value).toBe("cus_rejected");
    const report = `Could not set Stripe customer ID for ${ORG.name}: invalid Stripe customer ID`;
    expect(announce).toHaveBeenCalledWith(report);
    expect(showFailure).toHaveBeenCalledWith(report);
  });

  it("refreshes canonical organization truth after a conflict", async () => {
    const canonical = { ...ORG, stripe_customer_id: "cus_winner" };
    mocks.setStripeCustomer.mockRejectedValue(
      new GramAdminError(
        409,
        { message: "Stripe identity already set" },
        "409",
      ),
    );
    await renderEditor();
    mocks.getOrganization.mockResolvedValue(canonical);

    await enterCustomerID("cus_loser");
    const confirmation = await reviewCustomerID();
    fireEvent.click(
      within(confirmation).getByRole("button", { name: "Set customer ID" }),
    );

    expect(
      await screen.findByRole("button", { name: "Copy Stripe customer ID" }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Set customer ID" }),
    ).toBeNull();
  });
});
