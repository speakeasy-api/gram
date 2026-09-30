import { useRef, type JSX } from "react";
import { QueryClient, useQuery } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationQuery } from "@/lib/adminQueries";
import { GramAdminError, type AdminOrganization } from "@/lib/gramAdminApi";
import { WriteReportContext } from "@/pages/organizations/writeReport";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { SetStripeSubscription } from "./SetStripeSubscription";

const mocks = vi.hoisted(() => ({
  getStripeSubscriptionCandidate: vi.fn(),
  getOrganization: vi.fn(),
  setStripeSubscription: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  return {
    ...actual,
    getStripeSubscriptionCandidate: mocks.getStripeSubscriptionCandidate,
    getOrganization: mocks.getOrganization,
    setStripeSubscription: mocks.setStripeSubscription,
  };
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
  customer_id: "cus_placeholder_1",
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
  qc.setQueryData(organizationQuery(org.slug).queryKey, org);
  const announce = vi.fn<(message: string) => void>();
  const showFailure = vi.fn<(message: string | null) => void>();
  mocks.getOrganization.mockResolvedValue(org);
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

function confirmationValue(dialog: HTMLElement, label: string): string | null {
  return (
    within(dialog).getByText(label).nextElementSibling?.textContent ?? null
  );
}

beforeEach(() => {
  mocks.getStripeSubscriptionCandidate.mockReset();
  mocks.getOrganization.mockReset();
  mocks.setStripeSubscription.mockReset();
  mocks.getStripeSubscriptionCandidate.mockImplementation(
    (_organizationID: string, stripeSubscriptionID: string) =>
      Promise.resolve({ ...CANDIDATE, id: stripeSubscriptionID }),
  );
});

afterEach(cleanup);

describe("SetStripeSubscription", () => {
  it("offers entry only for a PAYG organization with a customer and no subscription", async () => {
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
          org={{ ...ORG, id: "nocustomer", stripe_customer_id: undefined }}
        />
        <SetStripeSubscription
          org={{ ...ORG, id: "empty-customer", stripe_customer_id: "" }}
        />
      </div>,
    );

    expect(
      screen.getAllByRole("button", { name: "Set subscription ID" }),
    ).toHaveLength(1);
    expect(
      screen.getByRole("button", { name: "Copy Stripe subscription ID" }),
    ).toBeTruthy();
    expect(
      screen.getAllByText(/Set a Stripe customer ID before recording/),
    ).toHaveLength(2);
  });

  it("rejects a malformed ID before confirmation or a request", async () => {
    await renderEditor();
    await enterSubscriptionID(" subscription_placeholder ");
    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "beginning with sub_",
    );
    expect(mocks.getStripeSubscriptionCandidate).not.toHaveBeenCalled();
    expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
  });

  it("confirms the Stripe customer match before saving", async () => {
    const { announce } = await renderEditor();
    await enterSubscriptionID("  sub_placeholder_1  ");
    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));

    const heading = await screen.findByRole("heading", {
      name: `Set Stripe subscription for ${ORG.name}?`,
    });
    const dialog = heading.closest('[role="dialog"]');
    if (!(dialog instanceof HTMLElement)) {
      throw new Error("confirmation heading is not in a dialog");
    }
    expect(confirmationValue(dialog, "Requested ID")).toBe("sub_placeholder_1");
    expect(confirmationValue(dialog, "Stripe returned ID")).toBe(
      "sub_placeholder_1",
    );
    expect(confirmationValue(dialog, "Stripe customer")).toBe(
      "cus_placeholder_1",
    );
    expect(confirmationValue(dialog, "Status")).toBe("active");
    expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
    expect(mocks.getStripeSubscriptionCandidate).toHaveBeenCalledWith(
      ORG.id,
      "sub_placeholder_1",
    );

    mocks.setStripeSubscription.mockResolvedValue({
      ...ORG,
      stripe_subscription_id: "sub_placeholder_1",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Set subscription ID" }),
    );
    await waitFor(() => {
      expect(mocks.setStripeSubscription.mock.calls[0]?.[0]).toEqual({
        organization_id: ORG.id,
        stripe_subscription_id: "sub_placeholder_1",
      });
    });
    await waitFor(() => {
      expect(announce).toHaveBeenCalledWith(
        `Set Stripe subscription ID sub_placeholder_1 for ${ORG.name}.`,
      );
    });
  });

  it("reports a verification failure without saving", async () => {
    mocks.getStripeSubscriptionCandidate.mockRejectedValue(
      new GramAdminError(
        409,
        {
          message:
            "Stripe subscription does not belong to the organization's Stripe customer",
        },
        "gram admin 409 Conflict",
      ),
    );
    const { announce } = await renderEditor();
    await enterSubscriptionID("sub_other");
    fireEvent.click(screen.getByRole("button", { name: "Review and set" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "does not belong",
    );
    expect(mocks.setStripeSubscription).not.toHaveBeenCalled();
    expect(announce).toHaveBeenCalledWith(
      expect.stringContaining("does not belong"),
    );
  });
});
