import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptySession } from "@/contexts/Auth";
import StartTrial from "./StartTrial";

const mocks = vi.hoisted(() => ({
  session: vi.fn(),
  isAdmin: vi.fn(),
  mutate: vi.fn(),
  refetch: vi.fn(),
  onComplete: undefined as (() => void) | undefined,
}));

vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useSessionData: () => ({ session: mocks.session() as unknown }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => {
    const allowed = mocks.isAdmin() as boolean;
    return {
      isLoading: false,
      can: () => allowed,
      hasAnyScope: () => allowed,
      hasAllScopes: () => allowed,
      hasScope: () => allowed,
    };
  },
}));

vi.mock("@gram/client/react-query/createTrialPaymentMethodCheckout.js", () => ({
  useCreateTrialPaymentMethodCheckoutMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));

vi.mock("@/components/billing/trial-checkout-mount", () => ({
  TrialCheckoutMount: ({ onComplete }: { onComplete: () => void }) => {
    mocks.onComplete = onComplete;
    return <div data-testid="trial-checkout" />;
  },
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ auth: { logout: vi.fn() } }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));

const CHECKOUT = {
  clientSecret: "cs_test_example_secret_example",
  publishableKey: "pk_test_example",
};

function pendingSession(overrides: Record<string, unknown> = {}) {
  return {
    ...emptySession,
    session: "session-token",
    activeOrganizationId: "org-1",
    organization: { id: "org-1", name: "Acme", slug: "acme", projects: [] },
    whitelisted: false,
    trial: null,
    trialPendingPaymentMethod: true,
    refetch: mocks.refetch,
    ...overrides,
  };
}

function renderPage(path = "/start-trial") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/" element={<div>Workspace</div>} />
        <Route path="/start-trial" element={<StartTrial />} />
      </Routes>
    </MemoryRouter>,
  );
}

function statusError(status: number) {
  return Object.assign(new Error(`status ${status}`), { statusCode: status });
}

describe("StartTrial", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.onComplete = undefined;
    mocks.isAdmin.mockReturnValue(true);
    mocks.session.mockReturnValue(pendingSession());
    mocks.refetch.mockResolvedValue(undefined);
    mocks.mutate.mockImplementation(
      (_vars: unknown, handlers: { onSuccess: (data: unknown) => void }) =>
        handlers.onSuccess(CHECKOUT),
    );
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("shows an admin the card form and says the card is not charged", () => {
    renderPage();

    expect(screen.getByTestId("trial-checkout")).toBeTruthy();
    expect(screen.getByText(/not charged during the trial/i)).toBeTruthy();
    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(mocks.mutate.mock.calls[0]?.[0]).toEqual({
      request: { theme: "light" },
    });
  });

  it("tells a member that an admin needs to add the card", () => {
    mocks.isAdmin.mockReturnValue(false);

    renderPage();

    expect(screen.queryByTestId("trial-checkout")).toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(
      screen.getByText(/starts when an organization admin adds a card/i),
    ).toBeTruthy();
    expect(screen.getByText("Acme")).toBeTruthy();
  });

  it.each([
    [503, /unavailable right now/i, true],
    [409, /can't start a free trial/i, false],
    [500, /couldn't load the card form/i, true],
  ])("explains a %s checkout failure", (status, message, retryable) => {
    mocks.mutate.mockImplementation(
      (_vars: unknown, handlers: { onError: (error: unknown) => void }) =>
        handlers.onError(statusError(status)),
    );

    renderPage();

    expect(screen.getByText(message)).toBeTruthy();
    expect(screen.queryByTestId("trial-checkout")).toBeNull();
    expect(screen.queryByRole("button", { name: "Try again" }) !== null).toBe(
      retryable,
    );
  });

  it("waits for the trial after checkout completes, then enters the app", async () => {
    vi.useFakeTimers();
    const view = renderPage();

    act(() => mocks.onComplete?.());

    expect(screen.getByRole("status").textContent).toMatch(/few seconds/i);
    expect(mocks.refetch).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(mocks.refetch).toHaveBeenCalledTimes(2);

    mocks.session.mockReturnValue(
      pendingSession({
        whitelisted: true,
        trialPendingPaymentMethod: false,
        trial: {
          startedAt: new Date(),
          endsAt: new Date(Date.now() + 14 * 24 * 60 * 60 * 1000),
        },
      }),
    );
    view.rerender(
      <MemoryRouter initialEntries={["/start-trial"]}>
        <Routes>
          <Route path="/" element={<div>Workspace</div>} />
          <Route path="/start-trial" element={<StartTrial />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByText("Workspace")).toBeTruthy();
  });

  it("confirms instead of opening checkout when Stripe returns to the page", () => {
    renderPage("/start-trial?session_id=cs_test_example");

    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByText("Starting your trial.")).toBeTruthy();
    expect(mocks.refetch).toHaveBeenCalled();
  });

  it("stops checking after a while and offers a retry", () => {
    vi.useFakeTimers();
    renderPage("/start-trial?session_id=cs_test_example");

    act(() => {
      vi.advanceTimersByTime(60_000);
    });

    expect(screen.getByText(/taking longer than usual/i)).toBeTruthy();
    const calls = mocks.refetch.mock.calls.length;

    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    expect(mocks.refetch.mock.calls.length).toBe(calls + 1);
    expect(screen.queryByText(/taking longer than usual/i)).toBeNull();

    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    fireEvent.click(screen.getByRole("button", { name: "Add a card again" }));
    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("trial-checkout")).toBeTruthy();
  });

  function rerenderPage(view: ReturnType<typeof renderPage>, path: string) {
    view.rerender(
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<div>Workspace</div>} />
          <Route path="/start-trial" element={<StartTrial />} />
        </Routes>
      </MemoryRouter>,
    );
  }

  function rejectedSession(reason: string, rejectedAt: Date) {
    return pendingSession({ trialCardRejection: { reason, rejectedAt } });
  }

  it.each([
    ["prepaid_card", /prepaid cards can't be used/i],
    ["card_used", /already used to start a free trial/i],
  ])(
    "stops checking and explains a %s rejection after checkout completes",
    async (reason, message) => {
      vi.useFakeTimers();
      const view = renderPage();

      act(() => mocks.onComplete?.());
      expect(mocks.refetch).toHaveBeenCalledTimes(1);

      mocks.session.mockReturnValue(rejectedSession(reason, new Date()));
      rerenderPage(view, "/start-trial");

      expect(screen.getByText("Your card couldn't be used.")).toBeTruthy();
      expect(screen.getByText(message)).toBeTruthy();
      expect(screen.queryByRole("status")).toBeNull();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(mocks.refetch).toHaveBeenCalledTimes(1);
    },
  );

  it("detects a rejection after Stripe returns to the page", () => {
    mocks.session.mockReturnValue(
      rejectedSession("prepaid_card", new Date(Date.now() + 1_000)),
    );

    renderPage("/start-trial?session_id=cs_test_example");

    expect(screen.getByText("Your card couldn't be used.")).toBeTruthy();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("shows the last rejected card above checkout on load", () => {
    mocks.session.mockReturnValue(
      rejectedSession("card_used", new Date(Date.now() - 60 * 60 * 1000)),
    );

    renderPage();

    expect(
      screen.getByText("The last card added couldn't be used."),
    ).toBeTruthy();
    expect(
      screen.getByText(/already used to start a free trial/i),
    ).toBeTruthy();
    expect(screen.getByTestId("trial-checkout")).toBeTruthy();
  });

  it("starts a fresh checkout when the admin adds a different card", () => {
    const view = renderPage();
    act(() => mocks.onComplete?.());

    mocks.session.mockReturnValue(rejectedSession("prepaid_card", new Date()));
    rerenderPage(view, "/start-trial");

    fireEvent.click(
      screen.getByRole("button", { name: "Add a different card" }),
    );

    expect(mocks.mutate).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("trial-checkout")).toBeTruthy();
    expect(
      screen.getByText("The last card added couldn't be used."),
    ).toBeTruthy();
  });

  it("keeps checking when the only rejection predates this attempt", async () => {
    vi.useFakeTimers();
    mocks.session.mockReturnValue(
      rejectedSession("prepaid_card", new Date(Date.now() - 2 * 60 * 1000)),
    );
    renderPage();

    act(() => mocks.onComplete?.());

    expect(screen.getByRole("status").textContent).toMatch(/few seconds/i);
    expect(screen.queryByText("Your card couldn't be used.")).toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(mocks.refetch).toHaveBeenCalledTimes(2);
  });

  it("leaves the gate once the organization no longer waits for a card", () => {
    mocks.session.mockReturnValue(
      pendingSession({ trialPendingPaymentMethod: false }),
    );

    renderPage();

    expect(screen.getByText("Workspace")).toBeTruthy();
  });
});
