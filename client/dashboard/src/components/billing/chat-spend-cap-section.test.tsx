import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  productTier: vi.fn(),
  session: vi.fn(),
  query: vi.fn(),
  mutation: vi.fn(),
  mutate: vi.fn(),
  reset: vi.fn(),
  hasAnyScope: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier() as ProductTier,
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => mocks.session(),
}));

vi.mock("@gram/client/react-query/getCreditUsage.js", () => ({
  useGetCreditUsage: () => mocks.query(),
  invalidateAllGetCreditUsage: mocks.invalidate,
}));

vi.mock("@gram/client/react-query/setSpendCap.js", () => ({
  useSetSpendCapMutation: (options?: { onSuccess?: () => void }) =>
    mocks.mutation(options),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: Scope) => mocks.hasAnyScope([scope]) as boolean,
    hasAnyScope: (scopes: Scope[]) => mocks.hasAnyScope(scopes) as boolean,
    hasAllScopes: (scopes: Scope[]) => mocks.hasAnyScope(scopes) as boolean,
    isLoading: false,
  }),
}));

// Page chrome isn't what's under test; render it as plain boxes so a failure
// here can only mean the section.
vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h2>{children}</h2>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { Page: { Section } };
});

import { ChatSpendCapSection } from "./chat-spend-cap-section";

const DAY = 24 * 60 * 60 * 1000;

type MutationState = {
  isPending?: boolean;
  isSuccess?: boolean;
  isError?: boolean;
};

function mutationState({
  isPending = false,
  isSuccess = false,
  isError = false,
}: MutationState = {}) {
  mocks.mutation.mockImplementation(() => ({
    mutate: mocks.mutate,
    reset: mocks.reset,
    isPending,
    isSuccess,
    isError,
  }));
}

/**
 * The section reaches for the query client to invalidate after a save, and the
 * form links to the in-app sales gate through the router.
 */
function render(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrap = (node: ReactNode) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>
  );
  const result = rtlRender(wrap(ui));
  // Re-wrapping keeps the provider at the root, so a rerender updates the
  // mounted section instead of remounting it under a new tree.
  return {
    ...result,
    rerender: (node: ReactNode) => result.rerender(wrap(node)),
  };
}

const field = () =>
  screen.queryByLabelText(/monthly chat spend cap/i) as HTMLInputElement | null;

const saveButton = () =>
  screen.queryByRole("button", { name: /save spend cap/i });

const heading = () =>
  screen.queryByRole("heading", { name: /chat spend cap/i });

/** The cap the save handler sent, unwrapped from the request envelope. */
function sentCap() {
  const variables = mocks.mutate.mock.calls.at(-1)?.[0] as {
    request: { spendCap: { monthlyCredits: number } };
  };
  return variables.request.spendCap.monthlyCredits;
}

describe("ChatSpendCapSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.session.mockReturnValue({ trial: null });
    mocks.hasAnyScope.mockReturnValue(true);
    mocks.query.mockReturnValue({
      data: { creditsUsed: 12, monthlyCredits: 250 },
      isError: false,
    });
    mutationState();
  });

  afterEach(cleanup);

  it("seeds the field from the cap the usage meters read", () => {
    render(<ChatSpendCapSection />);

    expect(field()!.value).toBe("250");
    expect(saveButton()).not.toBeNull();
  });

  it("sends the cap the admin entered", () => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "1500" } });
    fireEvent.click(saveButton()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(sentCap()).toBe(1500);
  });

  it("lowers the cap as readily as it raises it", () => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "1" } });
    fireEvent.click(saveButton()!);

    expect(sentCap()).toBe(1);
  });

  it("confirms a save and refreshes the cached credit usage", () => {
    mutationState({ isSuccess: true });

    render(<ChatSpendCapSection />);

    expect(screen.getByRole("status").textContent).toMatch(/saved/i);

    // The section — and the usage meter above it — read the cap back from the
    // credit usage query, so that key has to be invalidated or both would keep
    // showing the pre-save amount.
    const options = mocks.mutation.mock.calls.at(-1)?.[0] as {
      onSuccess: () => void;
    };
    options.onSuccess();
    expect(mocks.invalidate).toHaveBeenCalled();
  });

  it("disables saving while the save is in flight", () => {
    mutationState({ isPending: true });

    render(<ChatSpendCapSection />);

    // The button renames itself while the request is in flight, so the idle
    // name is gone by the time this queries for it.
    expect(saveButton()).toBeNull();
    const button = screen.getByRole("button", { name: /saving/i });
    expect(button.hasAttribute("disabled")).toBe(true);
  });

  it("reports a failed save and lets the admin retry it", () => {
    mutationState({ isError: true });

    render(<ChatSpendCapSection />);

    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't save the chat spend cap/i,
    );

    fireEvent.click(saveButton()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(sentCap()).toBe(250);
  });

  it("clears stale save feedback once the amount is edited", () => {
    mutationState({ isError: true });

    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "300" } });

    expect(mocks.reset).toHaveBeenCalled();
  });

  // An amount the API would reject has to be caught in the field, where it can
  // be corrected, rather than sent and bounced back as a transient-looking
  // failure the admin is invited to retry.
  it.each(["0", "10001", "12.5", ""])("refuses to send %s", (value) => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value } });
    fireEvent.click(saveButton()!);

    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toMatch(
      /between \$1 and \$10,000/i,
    );
  });

  it.each(["1", "10000"])("accepts the boundary amount %s", (value) => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value } });
    fireEvent.click(saveButton()!);

    expect(sentCap()).toBe(Number(value));
  });

  it("recovers once an out-of-range amount is corrected", () => {
    render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "20000" } });
    fireEvent.click(saveButton()!);
    expect(mocks.mutate).not.toHaveBeenCalled();

    fireEvent.change(field()!, { target: { value: "2000" } });
    fireEvent.click(saveButton()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(sentCap()).toBe(2000);
  });

  // $10,000 is the ceiling the endpoint enforces, so an admin who needs more
  // has to be pointed at a conversation rather than left retrying an amount
  // that will never be accepted. The gate is the in-app booking route, which
  // prefills from the session — not the marketing site.
  it("points an admin needing a larger cap at the in-app sales gate", () => {
    render(<ChatSpendCapSection />);

    expect(screen.getByText(/need a cap above \$10,000/i)).toBeTruthy();
    const salesLink = screen.getByRole("link", { name: "Talk to us" });
    expect(salesLink.getAttribute("href")).toBe("/talk-to-us");
    expect(salesLink.getAttribute("target")).toBeNull();
  });

  // The endpoint is admin-only, so a member gets the amount they're spending
  // under and no control that would fire a request bound to be refused.
  it("shows a member the cap without a way to change it", () => {
    mocks.hasAnyScope.mockReturnValue(false);

    render(<ChatSpendCapSection />);

    expect(screen.getByText("$250")).toBeTruthy();
    expect(field()).toBeNull();
    expect(saveButton()).toBeNull();
    // The form owns the mutation, so a member never even mounts it.
    expect(mocks.mutation).not.toHaveBeenCalled();
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(mocks.hasAnyScope).toHaveBeenCalledWith(["org:admin"]);
  });

  it("explains a failed load instead of showing an empty field", () => {
    mocks.query.mockReturnValue({ data: undefined, isError: true });

    render(<ChatSpendCapSection />);

    expect(field()).toBeNull();
    expect(screen.getByText(/couldn't load the chat spend cap/i)).toBeTruthy();
  });

  // A refetch that fails leaves the last successful value in the cache, so the
  // query reports data and an error together. The form stays — taking it away
  // would discard an in-progress edit — and the stale amount is called out.
  it("keeps the form and reports the failure when a cached cap is held", () => {
    mocks.query.mockReturnValue({
      data: { creditsUsed: 12, monthlyCredits: 250 },
      isError: true,
    });

    render(<ChatSpendCapSection />);

    expect(field()!.value).toBe("250");
    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't refresh the chat spend cap/i,
    );
  });

  // A refetch (the mutation's own invalidation, a window refocus) resolving
  // mid-edit would otherwise replace what the admin is typing.
  it("keeps an in-progress edit through a background refetch", () => {
    const { rerender } = render(<ChatSpendCapSection />);

    fireEvent.change(field()!, { target: { value: "900" } });

    mocks.query.mockReturnValue({
      data: { creditsUsed: 12, monthlyCredits: 250 },
      isError: false,
    });
    rerender(<ChatSpendCapSection />);

    expect(field()!.value).toBe("900");
  });

  // Trials run on the enterprise tier, but the account type can already read as
  // PAYG while one is still running. Either way there is no pay-as-you-go bill
  // yet, so an active trial outranks the tier: the cap is locked rather than
  // editable, and nothing about it can reach the endpoint.
  describe.each<ProductTier>(["enterprise", "payg"])(
    "during an active trial on the %s tier",
    (tier) => {
      beforeEach(() => {
        mocks.productTier.mockReturnValue(tier);
        mocks.session.mockReturnValue({
          trial: {
            startedAt: new Date(Date.now() - 2 * DAY),
            endsAt: new Date(Date.now() + 12 * DAY),
          },
        });
      });

      it("locks the cap and explains when it starts", () => {
        render(<ChatSpendCapSection />);

        expect(heading()).not.toBeNull();
        expect(field()!.disabled).toBe(true);
        expect(
          screen.getByText(/starts when pay as you go begins/i),
        ).toBeTruthy();
      });

      it("never reaches the cap endpoint", () => {
        render(<ChatSpendCapSection />);

        expect(saveButton()).toBeNull();
        expect(mocks.mutation).not.toHaveBeenCalled();
        expect(mocks.mutate).not.toHaveBeenCalled();
        // The locked state is self-contained copy — no cap to read yet.
        expect(mocks.query).not.toHaveBeenCalled();
      });
    },
  );

  // Only an *active* trial locks the cap. Once it is over, a PAYG org has the
  // bill the cap applies to and gets the form back.
  it("returns the form to pay as you go once the trial has ended", () => {
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date(Date.now() - 20 * DAY),
        endsAt: new Date(Date.now() - 6 * DAY),
      },
    });

    render(<ChatSpendCapSection />);

    expect(field()!.disabled).toBe(false);
    expect(field()!.value).toBe("250");
    expect(saveButton()).not.toBeNull();
  });

  // Every other tier has no pay-as-you-go bill for a cap to apply to.
  it.each<ProductTier>([
    "enterprise",
    "base",
    "base_PAID",
    "__deprecated__pro",
  ])("renders nothing on the %s view without an active trial", (tier) => {
    mocks.productTier.mockReturnValue(tier);

    const { container } = render(<ChatSpendCapSection />);

    expect(container.innerHTML).toBe("");
    expect(mocks.query).not.toHaveBeenCalled();
  });

  it("renders nothing for enterprise once the trial has ended", () => {
    mocks.productTier.mockReturnValue("enterprise");
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date(Date.now() - 20 * DAY),
        endsAt: new Date(Date.now() - 6 * DAY),
      },
    });

    const { container } = render(<ChatSpendCapSection />);

    expect(container.innerHTML).toBe("");
  });
});
