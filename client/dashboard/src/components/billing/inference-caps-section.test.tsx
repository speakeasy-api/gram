import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useNavigate } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InferenceSpendCap } from "@gram/client/models/components/inferencespendcap.js";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  productTier: vi.fn(),
  session: vi.fn(),
  query: vi.fn(),
  refetch: vi.fn(),
  mutation: vi.fn(),
  mutate: vi.fn(),
  reset: vi.fn(),
  hasAnyScope: vi.fn(),
  invalidate: vi.fn(),
  subscription: vi.fn(),
  subscriptionRefetch: vi.fn(),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier() as ProductTier,
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => mocks.session(),
}));

vi.mock("@gram/client/react-query/getInferenceSpendCaps.js", () => ({
  // The hook options are part of what's under test — the section has to opt out
  // of the shared throwOnError — so the arguments are forwarded, not dropped.
  useGetInferenceSpendCaps: (...args: unknown[]) => mocks.query(...args),
  invalidateAllGetInferenceSpendCaps: mocks.invalidate,
}));

vi.mock("@gram/client/react-query/setSpendCap.js", () => ({
  useSetSpendCapMutation: (options?: { onSuccess?: () => void }) =>
    mocks.mutation(options),
}));

// The shared subscription read is mocked at the wrapper: what the caps do with
// the live Stripe state is this file's subject, and the wrapper's own query
// options are covered by `use-stripe-subscription.test.ts`.
vi.mock("@/components/billing/use-stripe-subscription", () => ({
  useStripeSubscription: () => mocks.subscription(),
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

import { INFERENCE_CAPS_ANCHOR, inferenceCapAnchor } from "./inference-caps";
import {
  CAP_ANCHOR_WAIT_MS,
  InferenceCapsSection,
} from "./inference-caps-section";

const DAY = 24 * 60 * 60 * 1000;

const OTHER_LABEL = "Customer-facing inference cap";
const SECURITY_LABEL = "Security inference cap";

function cap(overrides: Partial<InferenceSpendCap> = {}): InferenceSpendCap {
  return {
    keyType: "chat",
    creditsUsed: 12,
    monthlyCredits: 250,
    disabled: false,
    ...overrides,
  };
}

/** The customer-facing inference key as the API reports it. */
function otherCap(overrides: Partial<InferenceSpendCap> = {}) {
  return cap({ keyType: "chat", monthlyCredits: 250, ...overrides });
}

function securityCap(overrides: Partial<InferenceSpendCap> = {}) {
  return cap({
    keyType: "internal",
    creditsUsed: 30,
    monthlyCredits: 500,
    ...overrides,
  });
}

type QueryState = {
  data?: InferenceSpendCap[] | undefined;
  isError?: boolean;
  isFetching?: boolean;
};

/**
 * The inference cap list the section renders from. No `data` is a query that
 * never resolved — the loading state, or a load that failed outright.
 */
function queryState({
  data,
  isError = false,
  isFetching = false,
}: QueryState = {}) {
  mocks.query.mockReturnValue({
    data,
    isError,
    isFetching,
    refetch: mocks.refetch,
  });
}

/** The organization the API answers for by default: both platform keys. */
function loadedCaps(caps: InferenceSpendCap[] = [otherCap(), securityCap()]) {
  queryState({ data: caps });
}

// Midday UTC so the formatted day can't slide either side of the date line in
// whichever time zone the tests happen to run in.
const TRIAL_END = new Date("2026-08-20T12:00:00.000Z");

type Subscription = {
  status: string;
  trialEnd?: Date;
  cancelAtPeriodEnd?: boolean;
};

type SubscriptionState = {
  data?: Subscription | undefined;
  error?: unknown;
  isError?: boolean;
  isFetching?: boolean;
};

/**
 * The live Stripe subscription the caps gate on. No `data` is a read that never
 * resolved — the loading state, or a load that failed outright.
 */
function subscriptionState({
  data,
  error,
  isError = false,
  isFetching = false,
}: SubscriptionState = {}) {
  mocks.subscription.mockReturnValue({
    data,
    error,
    isError,
    isFetching,
    refetch: mocks.subscriptionRefetch,
  });
}

/** Shaped like the SDK's 404 rejection, which is what the branch keys on. */
function notFound(): Error {
  return Object.assign(new Error("subscription not found"), {
    statusCode: 404,
  });
}

/** Stripe has converted the trial and is billing the organization. */
function billingSubscription() {
  subscriptionState({ data: { status: "active" } });
}

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
 * controls link to the in-app sales gate through the router.
 */
function render(ui: ReactNode, { at = "/acme/billing" }: { at?: string } = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrap = (node: ReactNode) => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[at]}>{node}</MemoryRouter>
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

const field = (label: string) =>
  screen.queryByLabelText(label) as HTMLInputElement | null;

const otherField = () => field(OTHER_LABEL);
const securityField = () => field(SECURITY_LABEL);

/** Whether a field is painted as holding an amount that was rejected. */
const isFlagged = (input: HTMLInputElement) =>
  input.parentElement!.className.includes("border-destructive-default");

const saveButton = (label: string) =>
  screen.queryByRole("button", { name: new RegExp(`^save ${label}$`, "i") });

const heading = () =>
  screen.queryByRole("heading", { name: /inference caps/i });

const meters = () => screen.queryAllByRole("progressbar");

// jsdom has no layout, so `scrollIntoView` isn't implemented on the prototype.
// The stub records both the options and which element was scrolled, since
// landing on the right element is the whole point.
const scrollIntoView = vi.fn<(options?: ScrollIntoViewOptions) => void>();
const scrolledElements: HTMLElement[] = [];
const originalScrollIntoView = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  "scrollIntoView",
);

function scrolledElementIds(): string[] {
  return scrolledElements.map((element) => element.id);
}

/**
 * Stands in for a banner's link: a real router navigation, so repeats of the
 * same URL get the fresh location the effect has to notice.
 */
function AnchorNavigator({ to }: { to: string }): JSX.Element {
  const navigate = useNavigate();

  return (
    <button
      onClick={() => {
        void navigate(to);
      }}
    >
      Go to the cap
    </button>
  );
}

/**
 * Waits out the animation frame the scroll is deferred by, so "nothing was
 * scrolled" is a settled answer rather than a race the assertion won.
 */
function settleFrames(): Promise<void> {
  return new Promise((resolve) => {
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        resolve();
      });
    });
  });
}

/**
 * Puts the two clocks the anchor scroll runs on — a frame, then the bounded
 * wait for the control it named — under the test's control. Only those two are
 * faked, so nothing else in the tree changes behavior underneath them.
 */
function fakeAnchorClock(): void {
  vi.useFakeTimers({
    toFake: [
      "setTimeout",
      "clearTimeout",
      "requestAnimationFrame",
      "cancelAnimationFrame",
    ],
  });
}

/** Comfortably past the frame the first scroll is deferred by. */
const A_FRAME = 100;

function advanceClock(ms: number): void {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

/** Mutation records are delivered on a microtask, so a render isn't enough. */
async function settleMutations(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
  });
}

/** What the save handler sent, unwrapped from the request envelope. */
function sentCaps(): Array<{ keyType: string; monthlyCredits: number }> {
  return mocks.mutate.mock.calls.map(
    (call) =>
      (
        call[0] as {
          request: {
            setSpendCapRequestBody: {
              keyType: string;
              monthlyCredits: number;
            };
          };
        }
      ).request.setSpendCapRequestBody,
  );
}

function lastSentCap() {
  return sentCaps().at(-1);
}

describe("InferenceCapsSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.session.mockReturnValue({ trial: null });
    mocks.hasAnyScope.mockReturnValue(true);
    loadedCaps();
    billingSubscription();
    mutationState();

    scrollIntoView.mockReset();
    scrolledElements.length = 0;
    HTMLElement.prototype.scrollIntoView = function (
      this: HTMLElement,
      options?: boolean | ScrollIntoViewOptions,
    ): void {
      scrolledElements.push(this);
      scrollIntoView(options as ScrollIntoViewOptions | undefined);
    };
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    if (originalScrollIntoView) {
      Object.defineProperty(
        HTMLElement.prototype,
        "scrollIntoView",
        originalScrollIntoView,
      );
    } else {
      Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
    }
  });

  // The endpoint answers with the materialized Gram-managed keys — two, one, or
  // none — so the section renders what it was given rather than a shape it
  // assumed.
  describe("one control per key the API returned", () => {
    it("renders both caps under the labels the product named them", () => {
      render(<InferenceCapsSection />);

      expect(otherField()!.value).toBe("250");
      expect(securityField()!.value).toBe("500");
      expect(saveButton(OTHER_LABEL)).not.toBeNull();
      expect(saveButton(SECURITY_LABEL)).not.toBeNull();
    });

    it("renders the invoiced cap first", () => {
      render(<InferenceCapsSection />);

      // Node.DOCUMENT_POSITION_FOLLOWING — the security cap comes after.
      expect(otherField()!.compareDocumentPosition(securityField()!) & 4).toBe(
        4,
      );
    });

    // The list arrives in whatever order the rows came back; the controls must
    // not swap places under an admin mid-edit.
    it("keeps that order whichever order the list arrives in", () => {
      loadedCaps([securityCap(), otherCap()]);

      render(<InferenceCapsSection />);

      expect(otherField()!.compareDocumentPosition(securityField()!) & 4).toBe(
        4,
      );
    });

    it.each<[string, InferenceSpendCap, string, string]>([
      ["only the invoiced key", otherCap(), OTHER_LABEL, SECURITY_LABEL],
      ["only the security key", securityCap(), SECURITY_LABEL, OTHER_LABEL],
    ])("renders one control for %s", (_name, only, present, absent) => {
      loadedCaps([only]);

      render(<InferenceCapsSection />);

      expect(field(present)).not.toBeNull();
      expect(field(absent)).toBeNull();
      expect(saveButton(absent)).toBeNull();
    });

    // An empty list says only that no Gram-managed key has been materialized
    // for this organization yet. It says nothing about what the organization
    // runs, and the copy must not read as if it did.
    it("states plainly that there is nothing to configure yet", () => {
      loadedCaps([]);

      const { container } = render(<InferenceCapsSection />);

      expect(heading()).not.toBeNull();
      expect(otherField()).toBeNull();
      expect(securityField()).toBeNull();
      expect(
        screen.getByText(
          "No Gram-managed inference keys are available to configure yet.",
        ),
      ).toBeTruthy();
      expect(container.querySelector(".skeleton")).toBeNull();
      expect(mocks.mutation).not.toHaveBeenCalled();
    });

    // An empty list is not evidence of a customer-supplied key, and not
    // evidence that Gram runs no inference for this organization.
    it("draws no conclusion about what the organization runs", () => {
      loadedCaps([]);

      const { container } = render(<InferenceCapsSection />);

      const copy = container.textContent ?? "";
      for (const claim of [
        /bring/i,
        /your own/i,
        /provider key/i,
        /isn't running/i,
        /no inference/i,
      ]) {
        expect(copy).not.toMatch(claim);
      }
    });
  });

  it("gives every control its own meter", () => {
    render(<InferenceCapsSection />);

    const labels = meters().map((meter) => meter.getAttribute("aria-label"));
    expect(labels).toEqual([
      `${OTHER_LABEL}: $12.00 of the $250.00 monthly cap`,
      `${SECURITY_LABEL}: $30.00 of the $500.00 monthly cap`,
    ]);
  });

  // A key with no cap set has nothing for a bar to be a proportion of, but the
  // spend against it still has to be somewhere.
  it("shows the spend for a key with no cap set, without a bar", () => {
    loadedCaps([otherCap({ monthlyCredits: 0, creditsUsed: 4 })]);

    render(<InferenceCapsSection />);

    expect(meters()).toHaveLength(0);
    expect(screen.getByText(/\$4\.00 spent this month/)).toBeTruthy();
    expect(otherField()).not.toBeNull();
  });

  // A banner links straight at the control for the cap it is about, so the
  // anchors have to survive edits to the section that carries them.
  it("carries the section anchor and one anchor per control", () => {
    const { container } = render(<InferenceCapsSection />);

    expect(container.querySelector(`#${INFERENCE_CAPS_ANCHOR}`)).not.toBeNull();
    expect(
      container.querySelector(`#${inferenceCapAnchor("chat")}`),
    ).not.toBeNull();
    expect(
      container.querySelector(`#${inferenceCapAnchor("internal")}`),
    ).not.toBeNull();
  });

  // 0 is how the API reports a key that has no cap on it, not an amount an
  // admin chose — and it is one of the amounts the endpoint refuses. Seeding it
  // would open the page on a field already flagged as wrong.
  describe("a key with no cap set", () => {
    beforeEach(() => {
      loadedCaps([otherCap({ monthlyCredits: 0, creditsUsed: 4 })]);
    });

    it("opens on an empty field with nothing corrected yet", () => {
      render(<InferenceCapsSection />);

      expect(otherField()!.value).toBe("");
      expect(isFlagged(otherField()!)).toBe(false);
      expect(screen.queryAllByRole("alert")).toHaveLength(0);
    });

    it("says no cap is set rather than showing an amount", () => {
      render(<InferenceCapsSection />);

      expect(otherField()!.getAttribute("placeholder")).toBe("No cap set");
    });

    it("leaves the save quiet until the admin touches the control", () => {
      render(<InferenceCapsSection />);

      expect(screen.queryAllByRole("alert")).toHaveLength(0);
      expect(screen.queryAllByRole("status")).toHaveLength(0);
      expect(screen.getByText(/no monthly cap is currently set/i)).toBeTruthy();
    });

    it("refuses a save until an amount is entered", () => {
      render(<InferenceCapsSection />);

      fireEvent.click(saveButton(OTHER_LABEL)!);

      expect(mocks.mutate).not.toHaveBeenCalled();
      expect(screen.getByRole("alert").textContent).toMatch(
        /between \$1 and \$10,000/i,
      );
      expect(isFlagged(otherField()!)).toBe(true);
    });

    it("saves the first cap an admin sets on it", () => {
      render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "250" } });
      fireEvent.click(saveButton(OTHER_LABEL)!);

      expect(lastSentCap()).toEqual({ keyType: "chat", monthlyCredits: 250 });
    });

    // The cap arriving from another admin has to reach the untouched field, an
    // empty one included.
    it("takes up a cap set on it elsewhere", () => {
      const { rerender } = render(<InferenceCapsSection />);

      loadedCaps([otherCap({ monthlyCredits: 400, creditsUsed: 4 })]);
      rerender(<InferenceCapsSection />);

      expect(otherField()!.value).toBe("400");
    });
  });

  it("gives the two controls different ids", () => {
    render(<InferenceCapsSection />);

    expect(otherField()!.id).not.toBe(securityField()!.id);
    expect(document.querySelectorAll(`#${otherField()!.id}`)).toHaveLength(1);
  });

  // The dashboard's router doesn't scroll to fragments, so arriving on an
  // anchor without this leaves the caps below the fold — which is the whole
  // point of the link that sent them.
  describe("arriving on an anchor", () => {
    const anchored = `/acme/billing#${INFERENCE_CAPS_ANCHOR}`;
    const securityAnchored = `/acme/billing#${inferenceCapAnchor("internal")}`;

    it("scrolls the section into view", async () => {
      render(<InferenceCapsSection />, { at: anchored });

      await vi.waitFor(() =>
        expect(scrollIntoView).toHaveBeenCalledWith({
          behavior: "smooth",
          block: "start",
        }),
      );
      expect(scrolledElementIds()).toEqual([INFERENCE_CAPS_ANCHOR]);
    });

    // A banner names one cap, so its link lands on that cap's own control
    // rather than the top of a section the other cap also lives in.
    it("scrolls to the control the link names", async () => {
      render(<InferenceCapsSection />, { at: securityAnchored });

      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));
      expect(scrolledElementIds()).toEqual([inferenceCapAnchor("internal")]);
    });

    // A link can outlive the key it named — the list is what decides what is on
    // screen — and the request still has to be answered somewhere.
    it("falls back to the section when that control isn't on screen", async () => {
      loadedCaps([otherCap()]);

      render(<InferenceCapsSection />, { at: securityAnchored });

      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));
      expect(scrolledElementIds()).toEqual([INFERENCE_CAPS_ANCHOR]);
    });

    // The cap list is fetched, so a banner's link routinely arrives while the
    // section is still a skeleton and the control it names doesn't exist yet.
    // The section is the first answer; the reader still has to end up on the
    // cap they were sent to.
    it("lands on the control once the cap list resolves", async () => {
      queryState();

      const { rerender } = render(<InferenceCapsSection />, {
        at: securityAnchored,
      });

      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));
      expect(scrolledElementIds()).toEqual([INFERENCE_CAPS_ANCHOR]);

      loadedCaps();
      rerender(<InferenceCapsSection />);

      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(2));
      expect(scrolledElementIds()).toEqual([
        INFERENCE_CAPS_ANCHOR,
        inferenceCapAnchor("internal"),
      ]);
    });

    // The link has been answered. A later refresh of the list — this section's
    // own post-save invalidation, or another admin's change — is not a second
    // request to jump, and yanking the page around mid-edit would be one.
    it("stops watching once it has landed on the control", async () => {
      queryState();

      const { rerender } = render(<InferenceCapsSection />, {
        at: securityAnchored,
      });
      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));

      loadedCaps();
      rerender(<InferenceCapsSection />);
      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(2));

      loadedCaps([otherCap(), securityCap({ monthlyCredits: 900 })]);
      rerender(<InferenceCapsSection />);

      await settleFrames();
      expect(scrollIntoView).toHaveBeenCalledTimes(2);
    });

    // A slow list is still the list this link was waiting for, so the wait has
    // to outlast a fetch rather than the frame that starts it.
    it("still lands on a control that arrives late in the wait", async () => {
      fakeAnchorClock();
      queryState();

      const { rerender } = render(<InferenceCapsSection />, {
        at: securityAnchored,
      });

      advanceClock(A_FRAME);
      expect(scrolledElementIds()).toEqual([INFERENCE_CAPS_ANCHOR]);

      advanceClock(CAP_ANCHOR_WAIT_MS - A_FRAME - 1);
      loadedCaps();
      rerender(<InferenceCapsSection />);
      await settleMutations();

      expect(scrolledElementIds()).toEqual([
        INFERENCE_CAPS_ANCHOR,
        inferenceCapAnchor("internal"),
      ]);
    });

    // A link can name a key this organization has never materialized, which no
    // amount of waiting will produce. The wait ends anyway, so the section
    // isn't watched for the rest of the page's life — and a key that shows up
    // much later, from another admin or a background refresh, doesn't yank the
    // page away from whoever has been reading it since.
    it("gives up on a control that never materializes", async () => {
      fakeAnchorClock();
      queryState();

      const { rerender } = render(<InferenceCapsSection />, {
        at: securityAnchored,
      });

      advanceClock(A_FRAME);
      expect(scrolledElementIds()).toEqual([INFERENCE_CAPS_ANCHOR]);

      // The list resolves without the key the link named, and stays that way
      // until well past the wait.
      loadedCaps([otherCap()]);
      rerender(<InferenceCapsSection />);
      await settleMutations();
      advanceClock(CAP_ANCHOR_WAIT_MS);

      loadedCaps();
      rerender(<InferenceCapsSection />);
      await settleMutations();

      expect(scrolledElementIds()).toEqual([INFERENCE_CAPS_ANCHOR]);
    });

    // The controls can only ever appear inside this section, so watching the
    // document would wake this hook for every unrelated insertion the billing
    // page makes while the list is in flight.
    it("watches the caps section rather than the document", async () => {
      const observe = vi.spyOn(MutationObserver.prototype, "observe");

      try {
        queryState();
        const { container } = render(<InferenceCapsSection />, {
          at: securityAnchored,
        });

        await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));

        const section = container.querySelector(`#${INFERENCE_CAPS_ANCHOR}`);
        expect(section).not.toBeNull();
        expect(observe.mock.calls.map(([root]) => root)).toEqual([section]);

        // An unrelated part of the page changing is not this hook's business.
        const unrelated = document.createElement("div");
        document.body.appendChild(unrelated);
        await settleMutations();
        unrelated.remove();

        expect(scrollIntoView).toHaveBeenCalledTimes(1);
      } finally {
        observe.mockRestore();
      }
    });

    // A link at the section itself is answered the moment the section is there.
    // The controls arriving later are not the thing it asked for.
    it("answers a section link once, whatever the list does after", async () => {
      queryState();

      const { rerender } = render(<InferenceCapsSection />, { at: anchored });
      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));

      loadedCaps();
      rerender(<InferenceCapsSection />);

      await settleFrames();
      expect(scrollIntoView).toHaveBeenCalledTimes(1);
      expect(scrolledElementIds()).toEqual([INFERENCE_CAPS_ANCHOR]);
    });

    // A trial locks the controls but still renders them, so a banner's link
    // lands on the cap it named rather than the top of the section.
    it("scrolls to a locked control during a trial", async () => {
      mocks.session.mockReturnValue({
        trial: {
          startedAt: new Date(Date.now() - 2 * DAY),
          endsAt: new Date(Date.now() + 12 * DAY),
        },
      });

      render(<InferenceCapsSection />, { at: securityAnchored });

      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));
      expect(scrolledElementIds()).toEqual([inferenceCapAnchor("internal")]);
    });

    it("leaves the page alone with no hash", async () => {
      render(<InferenceCapsSection />);

      await settleFrames();
      expect(scrollIntoView).not.toHaveBeenCalled();
    });

    it("leaves the page alone for another section's hash", async () => {
      render(<InferenceCapsSection />, { at: "/acme/billing#billing-email" });

      await settleFrames();
      expect(scrollIntoView).not.toHaveBeenCalled();
    });

    // Following the link, scrolling away, then following it again is a fresh
    // navigation to a URL that hasn't changed. Reading the hash alone would
    // report nothing happened and leave the second request unanswered.
    it("scrolls again when the same anchor is followed twice", async () => {
      render(
        <>
          <AnchorNavigator to={securityAnchored} />
          <InferenceCapsSection />
        </>,
      );

      fireEvent.click(screen.getByRole("button", { name: /go to the cap/i }));
      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));

      fireEvent.click(screen.getByRole("button", { name: /go to the cap/i }));
      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(2));

      expect(scrolledElementIds()).toEqual([
        inferenceCapAnchor("internal"),
        inferenceCapAnchor("internal"),
      ]);
    });

    // The anchor arrives with the navigation but the section behind it appears
    // only once the tier resolves, so a scroll fired on arrival would have
    // nothing to land on.
    it("waits for a section that mounts after the navigation", async () => {
      mocks.productTier.mockReturnValue("base");
      const { rerender } = render(<InferenceCapsSection />, { at: anchored });

      await settleFrames();
      expect(scrollIntoView).not.toHaveBeenCalled();

      mocks.productTier.mockReturnValue("payg");
      rerender(<InferenceCapsSection />);

      await vi.waitFor(() => expect(scrollIntoView).toHaveBeenCalledTimes(1));
      expect(scrolledElementIds()).toEqual([INFERENCE_CAPS_ANCHOR]);
    });
  });

  describe("saving one cap", () => {
    it("sends the amount with the key it belongs to", () => {
      render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "1500" } });
      fireEvent.click(saveButton(OTHER_LABEL)!);

      expect(mocks.mutate).toHaveBeenCalledTimes(1);
      expect(lastSentCap()).toEqual({ keyType: "chat", monthlyCredits: 1500 });
    });

    it("sends the security cap under its own key", () => {
      render(<InferenceCapsSection />);

      fireEvent.change(securityField()!, { target: { value: "800" } });
      fireEvent.click(saveButton(SECURITY_LABEL)!);

      expect(lastSentCap()).toEqual({
        keyType: "internal",
        monthlyCredits: 800,
      });
    });

    // The caps are independent limits: an amount typed into one must not reach
    // the other, and saving one must not save both.
    it("keeps the two drafts and the two saves apart", () => {
      render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "900" } });
      expect(securityField()!.value).toBe("500");

      fireEvent.change(securityField()!, { target: { value: "600" } });
      expect(otherField()!.value).toBe("900");

      fireEvent.click(saveButton(OTHER_LABEL)!);
      expect(sentCaps()).toEqual([{ keyType: "chat", monthlyCredits: 900 }]);

      fireEvent.click(saveButton(SECURITY_LABEL)!);
      expect(sentCaps()).toEqual([
        { keyType: "chat", monthlyCredits: 900 },
        { keyType: "internal", monthlyCredits: 600 },
      ]);
    });

    it("lowers a cap as readily as it raises one", () => {
      render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "1" } });
      fireEvent.click(saveButton(OTHER_LABEL)!);

      expect(lastSentCap()?.monthlyCredits).toBe(1);
    });

    it("confirms a save by name and refreshes the cached list", () => {
      mutationState({ isSuccess: true });

      render(<InferenceCapsSection />);

      const statuses = screen
        .getAllByRole("status")
        .map((node) => node.textContent);
      expect(statuses).toEqual([
        `Saved the ${OTHER_LABEL.toLowerCase()}.`,
        `Saved the ${SECURITY_LABEL.toLowerCase()}.`,
      ]);

      // Every meter and field on the page reads this list, so the whole key has
      // to be invalidated or they would all keep showing the pre-save amount.
      const options = mocks.mutation.mock.calls.at(-1)?.[0] as {
        onSuccess: () => void;
      };
      options.onSuccess();
      expect(mocks.invalidate).toHaveBeenCalled();
    });

    it("disables saving while the save is in flight", () => {
      mutationState({ isPending: true });

      render(<InferenceCapsSection />);

      // The button renames itself while the request is in flight, so the idle
      // name is gone by the time this queries for it.
      expect(saveButton(OTHER_LABEL)).toBeNull();
      const buttons = screen.getAllByRole("button", { name: /saving/i });
      expect(buttons).toHaveLength(2);
      expect(buttons[0]!.hasAttribute("disabled")).toBe(true);
    });

    it("reports a failed save by name and lets the admin retry it", () => {
      mutationState({ isError: true });

      render(<InferenceCapsSection />);

      expect(
        screen.getByText(`Couldn't save the ${OTHER_LABEL.toLowerCase()}.`, {
          exact: false,
        }),
      ).toBeTruthy();

      fireEvent.click(saveButton(SECURITY_LABEL)!);

      expect(mocks.mutate).toHaveBeenCalledTimes(1);
      expect(lastSentCap()).toEqual({
        keyType: "internal",
        monthlyCredits: 500,
      });
    });

    it("clears stale save feedback once the amount is edited", () => {
      mutationState({ isError: true });

      render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "300" } });

      expect(mocks.reset).toHaveBeenCalled();
    });
  });

  // An amount the API would reject has to be caught in the field, where it can
  // be corrected, rather than sent and bounced back as a transient-looking
  // failure the admin is invited to retry.
  it.each(["0", "10001", "12.5", ""])("refuses to send %s", (value) => {
    render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value } });
    fireEvent.click(saveButton(OTHER_LABEL)!);

    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toMatch(
      /between \$1 and \$10,000/i,
    );
  });

  // An amount is only wrong once somebody has put it there. A field emptied on
  // the way to a new amount is mid-edit, and a correction announced at that
  // moment is read out over an admin who is still typing.
  it("stays quiet while a field is cleared on the way to a new amount", () => {
    render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "" } });

    expect(screen.queryAllByRole("alert")).toHaveLength(0);
    expect(isFlagged(otherField()!)).toBe(false);
    // A cap is set on this key, so the empty field is mid-edit rather than
    // uncapped, and the hint is the amount to type.
    expect(otherField()!.getAttribute("placeholder")).toBe("$1–$10,000");

    fireEvent.change(otherField()!, { target: { value: "300" } });
    fireEvent.click(saveButton(OTHER_LABEL)!);

    expect(lastSentCap()?.monthlyCredits).toBe(300);
  });

  // An amount that is in the field and out of range is a mistake to point out
  // where it was made, rather than one to hold back until a save.
  it("flags an out-of-range amount as it is typed", () => {
    render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "0" } });

    expect(screen.getByRole("alert").textContent).toMatch(
      /between \$1 and \$10,000/i,
    );
    expect(isFlagged(otherField()!)).toBe(true);
  });

  // A save refused over an empty field still has to say so, or the button
  // reads as broken.
  it("explains an empty field once a save is attempted", () => {
    render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "" } });
    expect(screen.queryAllByRole("alert")).toHaveLength(0);

    fireEvent.click(saveButton(OTHER_LABEL)!);

    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toMatch(
      /between \$1 and \$10,000/i,
    );
  });

  // The refusal belonged to the amount that was saved. An admin who goes back
  // to the field is editing again, and an empty field mid-edit is the one state
  // the control is deliberately quiet about — a save earlier in the session
  // must not turn it into a standing correction.
  describe("after a save was refused", () => {
    it("goes quiet again once the amount is cleared", () => {
      render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "" } });
      fireEvent.click(saveButton(OTHER_LABEL)!);
      expect(screen.getAllByRole("alert")).toHaveLength(1);

      fireEvent.change(otherField()!, { target: { value: "300" } });
      expect(screen.queryAllByRole("alert")).toHaveLength(0);

      fireEvent.change(otherField()!, { target: { value: "" } });

      expect(screen.queryAllByRole("alert")).toHaveLength(0);
      expect(isFlagged(otherField()!)).toBe(false);
    });

    // Quiet is for the admin who is mid-edit, not for the one who pressed save
    // on an empty field: that request has to be refused out loud every time, or
    // the button reads as broken.
    it("refuses the next save on that empty field out loud", () => {
      render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "" } });
      fireEvent.click(saveButton(OTHER_LABEL)!);
      fireEvent.change(otherField()!, { target: { value: "300" } });
      fireEvent.change(otherField()!, { target: { value: "" } });

      fireEvent.click(saveButton(OTHER_LABEL)!);

      expect(mocks.mutate).not.toHaveBeenCalled();
      expect(screen.getByRole("alert").textContent).toMatch(
        /between \$1 and \$10,000/i,
      );
      expect(isFlagged(otherField()!)).toBe(true);
    });

    // Clearing is quiet because it is mid-edit; an amount that is actually in
    // the field is a mistake to point out wherever the control is in its life.
    it("still flags an out-of-range amount typed after it", () => {
      render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "" } });
      fireEvent.click(saveButton(OTHER_LABEL)!);
      fireEvent.change(otherField()!, { target: { value: "20000" } });

      expect(screen.getByRole("alert").textContent).toMatch(
        /between \$1 and \$10,000/i,
      );
      expect(isFlagged(otherField()!)).toBe(true);
    });
  });

  it.each(["1", "10000"])("accepts the boundary amount %s", (value) => {
    render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value } });
    fireEvent.click(saveButton(OTHER_LABEL)!);

    expect(lastSentCap()?.monthlyCredits).toBe(Number(value));
  });

  // One control's out-of-range amount is one control's problem.
  it("keeps a validation failure inside the control that has it", () => {
    render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "20000" } });
    fireEvent.click(saveButton(OTHER_LABEL)!);
    expect(mocks.mutate).not.toHaveBeenCalled();

    // The correction belongs to the field that earned it: the other control is
    // holding a perfectly good amount.
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(isFlagged(securityField()!)).toBe(false);

    fireEvent.click(saveButton(SECURITY_LABEL)!);

    expect(lastSentCap()).toEqual({ keyType: "internal", monthlyCredits: 500 });
  });

  // A refused save belongs to the control that was saved. The other control has
  // not been asked to save anything, so clearing its field afterwards is still
  // an admin mid-edit rather than a mistake to correct.
  it("keeps a refused save from speaking for the other control", () => {
    render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "" } });
    fireEvent.click(saveButton(OTHER_LABEL)!);
    expect(screen.getAllByRole("alert")).toHaveLength(1);

    fireEvent.change(securityField()!, { target: { value: "" } });

    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(isFlagged(securityField()!)).toBe(false);

    fireEvent.change(securityField()!, { target: { value: "700" } });
    fireEvent.click(saveButton(SECURITY_LABEL)!);

    expect(sentCaps()).toEqual([{ keyType: "internal", monthlyCredits: 700 }]);
  });

  it("recovers once an out-of-range amount is corrected", () => {
    render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "20000" } });
    fireEvent.click(saveButton(OTHER_LABEL)!);
    expect(mocks.mutate).not.toHaveBeenCalled();

    fireEvent.change(otherField()!, { target: { value: "2000" } });
    fireEvent.click(saveButton(OTHER_LABEL)!);

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(lastSentCap()?.monthlyCredits).toBe(2000);
  });

  // $10,000 is the ceiling the endpoint enforces, so an admin who needs more
  // has to be pointed at a conversation rather than left retrying an amount
  // that will never be accepted. The gate is the in-app booking route, which
  // prefills from the session — not the marketing site.
  it("points an admin needing a larger cap at the in-app sales gate", () => {
    loadedCaps([otherCap()]);

    render(<InferenceCapsSection />);

    expect(screen.getByText(/need a cap above \$10,000/i)).toBeTruthy();
    const salesLink = screen.getByRole("link", { name: "Talk to us" });
    expect(salesLink.getAttribute("href")).toBe("/talk-to-us");
    expect(salesLink.getAttribute("target")).toBeNull();
  });

  // The endpoint is admin-only, so a member gets the amounts they are spending
  // under and no control that would fire a request bound to be refused.
  it("shows a member every cap without a way to change any of them", () => {
    mocks.hasAnyScope.mockReturnValue(false);

    render(<InferenceCapsSection />);

    expect(screen.getByText(OTHER_LABEL)).toBeTruthy();
    expect(screen.getByText(SECURITY_LABEL)).toBeTruthy();
    expect(meters()).toHaveLength(2);
    expect(otherField()).toBeNull();
    expect(securityField()).toBeNull();
    expect(saveButton(OTHER_LABEL)).toBeNull();
    // The form owns the mutation, so a member never even mounts it.
    expect(mocks.mutation).not.toHaveBeenCalled();
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(mocks.hasAnyScope).toHaveBeenCalledWith(["org:admin"]);
  });

  // A key the platform has turned off is refused at the endpoint, so the field
  // would only invite a request that is going to come back a conflict. The
  // other cap is unaffected.
  it("locks a disabled key and leaves the other cap editable", () => {
    loadedCaps([otherCap(), securityCap({ disabled: true })]);

    render(<InferenceCapsSection />);

    expect(securityField()).toBeNull();
    expect(saveButton(SECURITY_LABEL)).toBeNull();
    expect(screen.getByText(/turned off for this organization/i)).toBeTruthy();
    // Still reported: a disabled key can have spent against its cap.
    expect(meters()).toHaveLength(2);

    expect(otherField()).not.toBeNull();
    fireEvent.click(saveButton(OTHER_LABEL)!);
    expect(lastSentCap()).toEqual({ keyType: "chat", monthlyCredits: 250 });
  });

  it("mounts no mutation when every key is disabled", () => {
    loadedCaps([otherCap({ disabled: true }), securityCap({ disabled: true })]);

    render(<InferenceCapsSection />);

    expect(mocks.mutation).not.toHaveBeenCalled();
  });

  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down over this section
  // and leave the branches below unreachable. Handling the failure inline only
  // works if the query opts out.
  it("keeps a load failure out of the app error boundary", () => {
    render(<InferenceCapsSection />);

    const options = mocks.query.mock.calls.at(-1)?.[2] as {
      throwOnError?: boolean;
    };
    expect(options.throwOnError).toBe(false);
  });

  it("explains a failed load instead of leaving a skeleton up", () => {
    queryState({ isError: true });

    const { container } = render(<InferenceCapsSection />);

    expect(otherField()).toBeNull();
    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't load the inference caps/i,
    );
    expect(container.querySelector(".skeleton")).toBeNull();
  });

  // Nothing was cached, so there is nothing on screen to work from. The failure
  // stays inside the section, so the way out has to be here too.
  it("retries a failed load in place", () => {
    queryState({ isError: true });

    render(<InferenceCapsSection />);

    fireEvent.click(screen.getByRole("button", { name: /^retry$/i }));

    expect(mocks.refetch).toHaveBeenCalledTimes(1);
  });

  it("disables the retry while the reload is in flight", () => {
    queryState({ isError: true, isFetching: true });

    render(<InferenceCapsSection />);

    const button = screen.getByRole("button", { name: /retrying/i });
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(screen.queryByRole("button", { name: /^retry$/i })).toBeNull();
  });

  it("shows the controls once a retried load succeeds", () => {
    queryState({ isError: true });

    const { rerender } = render(<InferenceCapsSection />);

    loadedCaps();
    rerender(<InferenceCapsSection />);

    expect(otherField()!.value).toBe("250");
    expect(securityField()!.value).toBe("500");
  });

  // A refetch that fails leaves the last successful list in the cache, so the
  // query reports data and an error together. The controls stay — taking them
  // away would discard an in-progress edit — and the stale amounts are called
  // out.
  it("keeps the controls and reports the failure when a cached list is held", () => {
    queryState({ data: [otherCap(), securityCap()], isError: true });

    render(<InferenceCapsSection />);

    expect(otherField()!.value).toBe("250");
    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't refresh the inference caps/i,
    );
  });

  // A refetch (a save's own invalidation, a window refocus) resolving mid-edit
  // would otherwise replace what the admin is typing.
  it("keeps in-progress edits through a background refetch", () => {
    const { rerender } = render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "900" } });
    fireEvent.change(securityField()!, { target: { value: "700" } });

    loadedCaps([
      otherCap({ monthlyCredits: 400 }),
      securityCap({ monthlyCredits: 900 }),
    ]);
    rerender(<InferenceCapsSection />);

    expect(otherField()!.value).toBe("900");
    expect(securityField()!.value).toBe("700");
  });

  // A cap can change under an idle page — another admin sets it, or this
  // section's own post-save invalidation lands — and an untouched field left
  // showing the old amount would save that stale amount straight back.
  it("takes up a cap that changed while its field was untouched", () => {
    const { rerender } = render(<InferenceCapsSection />);
    expect(otherField()!.value).toBe("250");

    loadedCaps([otherCap({ monthlyCredits: 400 }), securityCap()]);
    rerender(<InferenceCapsSection />);

    expect(otherField()!.value).toBe("400");
    fireEvent.click(saveButton(OTHER_LABEL)!);
    expect(lastSentCap()?.monthlyCredits).toBe(400);
  });

  // One field being edited must not stop the other from synchronizing.
  it("synchronizes an untouched field beside one being edited", () => {
    const { rerender } = render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "900" } });

    loadedCaps([
      otherCap({ monthlyCredits: 400 }),
      securityCap({ monthlyCredits: 750 }),
    ]);
    rerender(<InferenceCapsSection />);

    expect(otherField()!.value).toBe("900");
    expect(securityField()!.value).toBe("750");
  });

  // Editing back to the amount on the server is not an edit to protect: the
  // field is pristine again and the next cap has to reach it.
  it("resumes synchronizing once an edit is reverted", () => {
    const { rerender } = render(<InferenceCapsSection />);

    fireEvent.change(otherField()!, { target: { value: "900" } });
    fireEvent.change(otherField()!, { target: { value: "250" } });

    loadedCaps([otherCap({ monthlyCredits: 400 }), securityCap()]);
    rerender(<InferenceCapsSection />);

    expect(otherField()!.value).toBe("400");
  });

  // Trials run on the enterprise tier, but the account type can already read as
  // PAYG while one is still running. Either way the keys are live and their
  // caps are enforced on the trial's own defaults — what the trial withholds is
  // the ability to change them, not the caps themselves.
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

      it("shows every materialized cap with its enforced amount, locked", () => {
        render(<InferenceCapsSection />);

        expect(heading()).not.toBeNull();
        expect(otherField()!.value).toBe("250");
        expect(otherField()!.disabled).toBe(true);
        expect(securityField()!.value).toBe("500");
        expect(securityField()!.disabled).toBe(true);
        expect(saveButton(OTHER_LABEL)!.hasAttribute("disabled")).toBe(true);
        expect(saveButton(SECURITY_LABEL)!.hasAttribute("disabled")).toBe(true);
        expect(meters()).toHaveLength(2);
      });

      // The lock shows the amount upstream is holding, and for an uncapped key
      // that is no amount at all — not $0.
      it("shows a key with no cap as uncapped rather than as zero", () => {
        loadedCaps([otherCap({ monthlyCredits: 0 })]);

        render(<InferenceCapsSection />);

        expect(otherField()!.value).toBe("");
        expect(otherField()!.getAttribute("placeholder")).toBe("No cap set");
      });

      it("says the caps are enforced and when they can be changed", () => {
        render(<InferenceCapsSection />);

        expect(
          screen.getByText(/enforced during your trial/i).textContent,
        ).toMatch(/once pay as you go begins/i);
      });

      // Two, one, or none: the section renders the list it was handed, on a
      // trial as anywhere else.
      it.each<[string, InferenceSpendCap[], number]>([
        ["no", [], 0],
        ["one", [securityCap()], 1],
        ["two", [otherCap(), securityCap()], 2],
      ])("renders %s locked controls for that many keys", (_n, caps, count) => {
        loadedCaps(caps);

        render(<InferenceCapsSection />);

        const fields = screen.queryAllByRole("spinbutton");
        expect(fields).toHaveLength(count);
        for (const input of fields) {
          expect(input.hasAttribute("disabled")).toBe(true);
        }
        if (count === 0) {
          expect(
            screen.getByText(
              "No Gram-managed inference keys are available to configure yet.",
            ),
          ).toBeTruthy();
        }
      });

      it("reads the caps without asking about the subscription", () => {
        render(<InferenceCapsSection />);

        expect(mocks.query).toHaveBeenCalled();
        // The trial outranks the tier, so there is no Stripe state to consult.
        expect(mocks.subscription).not.toHaveBeenCalled();
      });

      it("never mounts the write path", () => {
        render(<InferenceCapsSection />);

        fireEvent.click(saveButton(OTHER_LABEL)!);

        expect(mocks.mutation).not.toHaveBeenCalled();
        expect(mocks.mutate).not.toHaveBeenCalled();
      });
    },
  );

  // Checkout marks the product trial converted and drops it from the session
  // while Stripe can keep trialing the subscription for days. So a PAYG
  // organization with no product trial left is not necessarily being billed:
  // from here on the live Stripe status is what decides, and it fails closed.
  describe("once the product trial has been converted", () => {
    it("shows the enforced caps, locked, while Stripe is still trialing", () => {
      subscriptionState({ data: { status: "trialing", trialEnd: TRIAL_END } });

      render(<InferenceCapsSection />);

      expect(otherField()!.value).toBe("250");
      expect(otherField()!.disabled).toBe(true);
      expect(securityField()!.disabled).toBe(true);
      expect(meters()).toHaveLength(2);
      expect(
        screen.getByText(
          /you can change them when pay as you go begins on August 20, 2026/i,
        ),
      ).toBeTruthy();
    });

    it.each<[string, InferenceSpendCap[], number]>([
      ["no", [], 0],
      ["one", [otherCap()], 1],
      ["two", [otherCap(), securityCap()], 2],
    ])(
      "renders %s locked controls for that many keys while trialing",
      (_n, caps, count) => {
        subscriptionState({ data: { status: "trialing" } });
        loadedCaps(caps);

        render(<InferenceCapsSection />);

        const fields = screen.queryAllByRole("spinbutton");
        expect(fields).toHaveLength(count);
        for (const input of fields) {
          expect(input.hasAttribute("disabled")).toBe(true);
        }
        if (count === 0) {
          expect(
            screen.getByText(
              "No Gram-managed inference keys are available to configure yet.",
            ),
          ).toBeTruthy();
        }
      },
    );

    it("never mounts the write path while Stripe is trialing", () => {
      subscriptionState({ data: { status: "trialing" } });

      render(<InferenceCapsSection />);

      fireEvent.click(saveButton(OTHER_LABEL)!);

      expect(mocks.mutation).not.toHaveBeenCalled();
      expect(mocks.mutate).not.toHaveBeenCalled();
      // The caps themselves are still read: they are being enforced.
      expect(mocks.query).toHaveBeenCalled();
    });

    // A trial on its way out is still a trial: the amounts can't be changed
    // until it converts.
    it("keeps a trial that is set to cancel locked", () => {
      subscriptionState({
        data: {
          status: "trialing",
          trialEnd: TRIAL_END,
          cancelAtPeriodEnd: true,
        },
      });

      render(<InferenceCapsSection />);

      expect(otherField()!.disabled).toBe(true);
      expect(mocks.mutation).not.toHaveBeenCalled();
    });

    // Only a subscription Stripe is actually billing has a bill for a cap to
    // apply to. `past_due` counts: the bill exists and Stripe is retrying it.
    it.each(["active", "past_due"])(
      "opens the controls for a %s subscription",
      (status) => {
        subscriptionState({ data: { status } });

        render(<InferenceCapsSection />);

        expect(otherField()!.disabled).toBe(false);
        expect(securityField()!.disabled).toBe(false);
      },
    );

    // Service and billing both run to the end of the period, so a scheduled
    // cancellation leaves the caps editable for as long as they apply.
    it("keeps the controls through a scheduled cancellation", () => {
      subscriptionState({
        data: { status: "active", cancelAtPeriodEnd: true },
      });

      render(<InferenceCapsSection />);

      expect(otherField()!.disabled).toBe(false);
      expect(saveButton(OTHER_LABEL)).not.toBeNull();
    });

    // A subscription with no live bill behind it can't take a changed cap, but
    // the caps it was running under are still there and still enforced.
    it.each([
      "canceled",
      "unpaid",
      "incomplete",
      "incomplete_expired",
      "paused",
    ])("shows the caps read-only for a %s subscription", (status) => {
      subscriptionState({ data: { status } });

      render(<InferenceCapsSection />);

      expect(otherField()!.value).toBe("250");
      expect(otherField()!.disabled).toBe(true);
      expect(securityField()!.disabled).toBe(true);
      expect(meters()).toHaveLength(2);
      expect(screen.getByText(/subscription isn't billing/i)).toBeTruthy();
      // Read, but never written.
      expect(mocks.query).toHaveBeenCalled();
      expect(mocks.mutation).not.toHaveBeenCalled();
      expect(mocks.mutate).not.toHaveBeenCalled();
    });

    // Nothing has resolved yet, so there is nothing to show or lock — just the
    // placeholder, and no reads behind it.
    it("waits on the placeholder while the subscription read is in flight", () => {
      subscriptionState();

      render(<InferenceCapsSection />);

      expect(otherField()).toBeNull();
      expect(mocks.query).not.toHaveBeenCalled();
    });

    // A refetch is in flight precisely when the state might be about to change
    // — after a conversion or a cancellation — but also for reasons that have
    // nothing to do with this admin, like a window refocus. Taking the
    // controls away would discard amounts they typed and haven't saved, so the
    // lock stops the writing without touching what is on screen.
    it("locks the controls rather than closing them during a background refetch", () => {
      subscriptionState({ data: { status: "active" }, isFetching: true });

      render(<InferenceCapsSection />);

      expect(otherField()!.disabled).toBe(true);
      expect(securityField()!.disabled).toBe(true);
      expect(saveButton(OTHER_LABEL)!.hasAttribute("disabled")).toBe(true);
      expect(saveButton(SECURITY_LABEL)!.hasAttribute("disabled")).toBe(true);
    });

    // The lock is one state shared by every control, so it is announced once —
    // two live regions carrying identical text get announced in whichever
    // order the screen reader pleases, or talk over each other.
    it("announces the lock once for the whole section", () => {
      subscriptionState({ data: { status: "active" }, isFetching: true });

      render(<InferenceCapsSection />);

      const statuses = screen.getAllByRole("status");
      expect(statuses).toHaveLength(1);
      expect(statuses[0]!.textContent).toMatch(/checking your subscription/i);
    });

    it("keeps unsaved drafts through a refetch and blocks saving them", () => {
      const { rerender } = render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "900" } });
      fireEvent.change(securityField()!, { target: { value: "700" } });

      subscriptionState({ data: { status: "active" }, isFetching: true });
      rerender(<InferenceCapsSection />);

      // The drafts are still there, still showing the admin's amounts...
      expect(otherField()!.value).toBe("900");
      expect(securityField()!.value).toBe("700");

      // ...and nothing can reach the endpoint until the state is confirmed.
      const save = saveButton(OTHER_LABEL)!;
      fireEvent.click(save);
      fireEvent.submit(save.closest("form")!);
      expect(mocks.mutate).not.toHaveBeenCalled();
    });

    // Saving invalidates the caps and the subscription both, so a confirmation
    // and the lock can land in the same render. The control that would have
    // spoken for the lock says the more important thing instead.
    it("announces one status per control when a save and a refetch overlap", () => {
      mutationState({ isSuccess: true });
      subscriptionState({ data: { status: "active" }, isFetching: true });

      render(<InferenceCapsSection />);

      const statuses = screen.getAllByRole("status");
      expect(statuses).toHaveLength(2);
      expect(statuses[0]!.textContent).toMatch(
        /saved the customer-facing inference cap/i,
      );
      expect(screen.queryByText(/checking your subscription/i)).toBeNull();
      // The lock still holds even though it no longer speaks for itself.
      expect(otherField()!.disabled).toBe(true);
    });

    // A failed save outranks both: the amount didn't land, which is the thing
    // the admin has to hear.
    it("announces the failure when a failed save and a refetch overlap", () => {
      mutationState({ isError: true });
      subscriptionState({ data: { status: "active" }, isFetching: true });

      render(<InferenceCapsSection />);

      expect(screen.queryAllByRole("status")).toHaveLength(0);
      expect(screen.getAllByRole("alert")[0]!.textContent).toMatch(
        /couldn't save the customer-facing inference cap/i,
      );
      expect(screen.queryByText(/checking your subscription/i)).toBeNull();
    });

    it("saves the same draft once the refetch settles", () => {
      const { rerender } = render(<InferenceCapsSection />);

      fireEvent.change(otherField()!, { target: { value: "900" } });
      subscriptionState({ data: { status: "active" }, isFetching: true });
      rerender(<InferenceCapsSection />);

      billingSubscription();
      rerender(<InferenceCapsSection />);

      expect(otherField()!.value).toBe("900");
      expect(otherField()!.disabled).toBe(false);
      expect(screen.queryByRole("status")).toBeNull();

      fireEvent.click(saveButton(OTHER_LABEL)!);

      expect(mocks.mutate).toHaveBeenCalledTimes(1);
      expect(lastSentCap()?.monthlyCredits).toBe(900);
    });

    // A refetch that settles somewhere the caps can no longer be changed drops
    // the draft with the form: there is nothing left to save it against. The
    // enforced amount takes its place rather than an empty field.
    it.each(["trialing", "canceled"])(
      "locks the controls when the refetch settles to %s",
      (status) => {
        const { rerender } = render(<InferenceCapsSection />);

        fireEvent.change(otherField()!, { target: { value: "900" } });

        subscriptionState({ data: { status } });
        rerender(<InferenceCapsSection />);

        expect(otherField()!.value).toBe("250");
        expect(otherField()!.disabled).toBe(true);
        expect(saveButton(OTHER_LABEL)!.hasAttribute("disabled")).toBe(true);
      },
    );

    // The pay-as-you-go tier predates Stripe, so an organization can hold it
    // without a Stripe subscription behind it. Its keys can still be
    // materialized and capped, so the caps show; only the editing goes away,
    // along with a recheck that would find nothing.
    describe("with no Stripe subscription behind the tier", () => {
      beforeEach(() => {
        subscriptionState({ isError: true, error: notFound() });
      });

      it("says why the caps can't be changed", () => {
        render(<InferenceCapsSection />);

        expect(heading()).not.toBeNull();
        expect(
          screen.getByText(/this organization has no stripe subscription/i),
        ).toBeTruthy();
        expect(
          screen.queryByText(/couldn't check your subscription/i),
        ).toBeNull();
      });

      it("still shows every materialized cap, read-only", () => {
        render(<InferenceCapsSection />);

        expect(otherField()!.value).toBe("250");
        expect(otherField()!.disabled).toBe(true);
        expect(securityField()!.disabled).toBe(true);
        expect(meters()).toHaveLength(2);
        expect(mocks.query).toHaveBeenCalled();
        expect(mocks.mutation).not.toHaveBeenCalled();
        expect(mocks.mutate).not.toHaveBeenCalled();
      });

      it("offers nothing to recheck", () => {
        render(<InferenceCapsSection />);

        expect(screen.queryByRole("button", { name: /recheck/i })).toBeNull();
        expect(mocks.subscriptionRefetch).not.toHaveBeenCalled();
      });

      // The answer is definitive, so it outranks whatever the cache still
      // holds — and it stays fail-closed either way.
      it("outranks a cached subscription", () => {
        subscriptionState({
          data: { status: "active" },
          isError: true,
          error: notFound(),
        });

        render(<InferenceCapsSection />);

        expect(otherField()!.disabled).toBe(true);
        expect(
          screen.getByText(/this organization has no stripe subscription/i),
        ).toBeTruthy();
      });
    });

    // The billing state is unknown rather than known-bad. The caps are known,
    // so they stay on screen; what goes away is the ability to change them.
    it("shows the caps locked when the subscription can't be read", () => {
      subscriptionState({
        isError: true,
        error: new Error("stripe unavailable"),
      });

      render(<InferenceCapsSection />);

      expect(otherField()!.disabled).toBe(true);
      expect(securityField()!.disabled).toBe(true);
      expect(mocks.mutation).not.toHaveBeenCalled();
      expect(screen.getByRole("alert").textContent).toMatch(
        /couldn't check your subscription/i,
      );
    });

    // The cached copy goes stale across exactly the moment that matters — a
    // trial converting, or a subscription ending — so a read that is failing
    // right now must not re-open the controls on the strength of it.
    it("keeps the caps locked when a failing read still holds a cached subscription", () => {
      subscriptionState({
        data: { status: "active" },
        isError: true,
        error: new Error("stripe unavailable"),
      });

      render(<InferenceCapsSection />);

      expect(otherField()!.disabled).toBe(true);
      expect(mocks.mutation).not.toHaveBeenCalled();
      expect(screen.getByRole("alert").textContent).toMatch(
        /couldn't check your subscription/i,
      );
    });

    it("rechecks the subscription in place", () => {
      subscriptionState({
        isError: true,
        error: new Error("stripe unavailable"),
      });

      render(<InferenceCapsSection />);

      fireEvent.click(screen.getByRole("button", { name: /^recheck$/i }));

      expect(mocks.subscriptionRefetch).toHaveBeenCalledTimes(1);
    });

    it("disables the recheck while it is in flight", () => {
      subscriptionState({
        isError: true,
        error: new Error("stripe unavailable"),
        isFetching: true,
      });

      render(<InferenceCapsSection />);

      const button = screen.getByRole("button", { name: /rechecking/i });
      expect(button.hasAttribute("disabled")).toBe(true);
    });

    it("returns the controls to editable once a recheck succeeds", () => {
      subscriptionState({
        isError: true,
        error: new Error("stripe unavailable"),
      });

      const { rerender } = render(<InferenceCapsSection />);
      expect(otherField()!.disabled).toBe(true);

      billingSubscription();
      rerender(<InferenceCapsSection />);

      expect(otherField()!.disabled).toBe(false);
      expect(securityField()!.disabled).toBe(false);
    });
  });

  // Only an *active* trial locks the caps. Once it is over, a PAYG org has the
  // bill they apply to and gets the controls back.
  it("returns the controls to pay as you go once the trial has ended", () => {
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date(Date.now() - 20 * DAY),
        endsAt: new Date(Date.now() - 6 * DAY),
      },
    });

    render(<InferenceCapsSection />);

    expect(otherField()!.disabled).toBe(false);
    expect(otherField()!.value).toBe("250");
  });

  // Every other tier has no pay-as-you-go bill for a cap to apply to.
  it.each<ProductTier>([
    "enterprise",
    "base",
    "base_PAID",
    "__deprecated__pro",
  ])("renders nothing on the %s view without an active trial", (tier) => {
    mocks.productTier.mockReturnValue(tier);

    const { container } = render(<InferenceCapsSection />);

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

    const { container } = render(<InferenceCapsSection />);

    expect(container.innerHTML).toBe("");
  });
});
