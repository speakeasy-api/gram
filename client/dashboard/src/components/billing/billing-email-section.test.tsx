import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Scope } from "@gram/client/models/components/rolegrant.js";

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  mutation: vi.fn(),
  mutate: vi.fn(),
  reset: vi.fn(),
  hasAnyScope: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock("@gram/client/react-query/getBillingEmail.js", () => ({
  useGetBillingEmail: () => mocks.query(),
  invalidateAllGetBillingEmail: mocks.invalidate,
}));

vi.mock("@gram/client/react-query/setBillingEmail.js", () => ({
  useSetBillingEmailMutation: (options?: { onSuccess?: () => void }) =>
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

import { BillingEmailSection } from "./billing-email-section";

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

/** The section reaches for the query client to invalidate after a save. */
function render(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrap = (node: ReactNode) => (
    <QueryClientProvider client={client}>{node}</QueryClientProvider>
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
  screen.queryByLabelText(
    /billing notification email/i,
  ) as HTMLInputElement | null;

const saveButton = () =>
  screen.queryByRole("button", { name: /save billing email/i });

/** The body the save handler sent, unwrapped from the request envelope. */
function sentBody() {
  const variables = mocks.mutate.mock.calls.at(-1)?.[0] as {
    request: { billingEmail: { email?: string } };
  };
  return variables.request.billingEmail;
}

describe("BillingEmailSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.hasAnyScope.mockReturnValue(true);
    mocks.query.mockReturnValue({ data: { email: undefined }, isError: false });
    mutationState();
  });

  afterEach(cleanup);

  it("renders the field for an org admin", () => {
    render(<BillingEmailSection />);

    expect(field()).not.toBeNull();
    expect(mocks.hasAnyScope).toHaveBeenCalledWith(["org:admin"]);
  });

  // The query is admin-only; gating outside the component that owns it keeps
  // a member from firing a request they can't make.
  it("renders nothing — and fetches nothing — for a member", () => {
    mocks.hasAnyScope.mockReturnValue(false);

    render(<BillingEmailSection />);

    expect(field()).toBeNull();
    expect(mocks.query).not.toHaveBeenCalled();
  });

  it("explains a failed load instead of showing an empty field", () => {
    mocks.query.mockReturnValue({ data: undefined, isError: true });

    render(<BillingEmailSection />);

    expect(field()).toBeNull();
    expect(
      screen.getByText(/couldn't load the billing notification email/i),
    ).toBeTruthy();
  });

  it("seeds the field from the configured address", () => {
    mocks.query.mockReturnValue({
      data: { email: "billing@example.test" },
      isError: false,
    });

    render(<BillingEmailSection />);

    expect(field()!.value).toBe("billing@example.test");
  });

  // Blank means "all organization admins", which the API expresses by omitting
  // the field — sending an empty string would store one instead.
  it("clears the address by omitting it", () => {
    mocks.query.mockReturnValue({
      data: { email: "billing@example.test" },
      isError: false,
    });
    render(<BillingEmailSection />);

    fireEvent.change(field()!, { target: { value: "   " } });
    fireEvent.click(saveButton()!);

    const body = sentBody();
    expect(body.email).toBeUndefined();
    expect("email" in body).toBe(true);
  });

  it("sends the trimmed address", () => {
    render(<BillingEmailSection />);

    fireEvent.change(field()!, {
      target: { value: "  billing@example.test  " },
    });
    fireEvent.click(saveButton()!);

    expect(sentBody().email).toBe("billing@example.test");
  });

  // A malformed address has to be caught by the field, where it can be
  // corrected, rather than sent and bounced back as a transient-looking API
  // failure the admin is invited to retry.
  it("submits a valid address but not a malformed one", () => {
    render(<BillingEmailSection />);

    fireEvent.change(field()!, { target: { value: "not-an-email" } });
    fireEvent.click(saveButton()!);

    expect(mocks.mutate).not.toHaveBeenCalled();

    fireEvent.change(field()!, { target: { value: "billing@example.test" } });
    fireEvent.click(saveButton()!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(sentBody().email).toBe("billing@example.test");
  });

  it("disables saving while the save is in flight", () => {
    mutationState({ isPending: true });

    render(<BillingEmailSection />);

    // The button renames itself while the request is in flight, so the idle
    // name is gone by the time this queries for it.
    expect(saveButton()).toBeNull();
    const button = screen.getByRole("button", { name: /saving/i });
    expect(button.hasAttribute("disabled")).toBe(true);
  });

  it("confirms a save and refreshes the cached value", () => {
    mutationState({ isSuccess: true });

    render(<BillingEmailSection />);

    expect(screen.getByRole("status").textContent).toMatch(/saved/i);

    // The section reads its value back from the query, so the cache has to be
    // invalidated or the field would seed from the pre-save address on remount.
    const options = mocks.mutation.mock.calls.at(-1)?.[0] as {
      onSuccess: () => void;
    };
    options.onSuccess();
    expect(mocks.invalidate).toHaveBeenCalled();
  });

  it("reports a failed save", () => {
    mutationState({ isError: true });

    render(<BillingEmailSection />);

    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't save the billing notification email/i,
    );
  });

  // A refetch (the mutation's own invalidation, a window refocus) resolving
  // mid-edit would otherwise replace what the admin is typing with the value
  // already on the server.
  it("keeps an in-progress edit through a background refetch", () => {
    const { rerender } = render(<BillingEmailSection />);

    fireEvent.change(field()!, { target: { value: "new@example.test" } });

    mocks.query.mockReturnValue({
      data: { email: "stale@example.test" },
      isError: false,
    });
    rerender(<BillingEmailSection />);

    expect(field()!.value).toBe("new@example.test");
  });
});
