import { InferenceCapControl } from "@/components/billing/inference-cap-control";
import { useInferenceCapsMode } from "@/components/billing/inference-caps-mode";
import {
  INFERENCE_CAPS_ANCHOR,
  isInferenceCapAnchor,
  sortInferenceCaps,
} from "@/components/billing/inference-caps";
import {
  formatBillingDate,
  isStripeBilling,
  isStripeTrialing,
} from "@/components/billing/payg-plan-state";
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { isNotFoundError } from "@/lib/route-errors";
import { useGetInferenceSpendCaps } from "@gram/client/react-query/getInferenceSpendCaps.js";
import { type RefObject, useEffect, useRef } from "react";
import { useLocation } from "react-router";

/**
 * What the inference cap section does for this organization.
 *
 * The caps belong to the pay-as-you-go surfaces, so tiers with no PAYG state
 * see nothing at all. An active product trial keeps the section — the keys are
 * live and their caps are enforced — but the amounts can't be changed until
 * pay-as-you-go billing starts, which is what `product-trial` carries.
 *
 * Once checkout converts the product trial, the session stops carrying it while
 * Stripe can still be trialing the subscription for days, so from there the
 * live Stripe status decides what is editable.
 */
/**
 * Why the caps can be read but not changed, when that is the case.
 *
 * `null` is an editable section. A lock with no note is one whose reason is
 * already on screen — the failed subscription read says it in its own alert.
 */
type CapsLock = { note: string | null };

const PRODUCT_TRIAL_NOTE =
  "These caps are enforced during your trial. You can change them once pay as you go begins.";

const NO_SUBSCRIPTION_NOTE =
  "Changing a cap needs pay-as-you-go billing through Stripe. This organization has no Stripe subscription, so the caps below are read-only.";

const NOT_BILLING_NOTE =
  "Changing a cap needs pay-as-you-go billing through Stripe. This organization's subscription isn't billing, so the caps below are read-only.";

// The endpoint answers with the Gram-managed inference keys that have been
// materialized for this organization, which can be none of them.
const NO_CAPS_NOTE =
  "No Speakeasy-managed inference keys are available to configure yet.";

/** The same note for a Stripe trial, which knows when billing takes over. */
function stripeTrialNote(convertsOn: Date | null | undefined): string {
  const on = formatBillingDate(convertsOn);
  if (on === null) {
    return "These caps are enforced during your trial. You can change them once your trial converts.";
  }
  return `These caps are enforced during your trial. You can change them when pay as you go begins on ${on}.`;
}

/**
 * How long a link keeps waiting for the control it named.
 *
 * The wait exists for the cap list's fetch, so it is sized for one: long
 * enough for a slow response to still land the reader on the cap they were
 * sent to, short enough that a key materialized long afterwards — by another
 * admin, or by a background refresh — can't yank the page out from under
 * someone who has been reading it since.
 */
export const CAP_ANCHOR_WAIT_MS = 10_000;

/**
 * Scrolls the section into view when the URL is pointing at it.
 *
 * The dashboard's router doesn't scroll to fragments, so an anchor link into
 * this page lands at the top of the billing page with the caps still below the
 * fold — which is the entire point of the banner's link.
 *
 * A banner links at one cap's own anchor, so that element is what the scroll
 * aims for, falling back to the section when the control isn't on screen yet.
 *
 * That fallback is a first answer rather than the last one. The cap list is
 * fetched, so a banner's link routinely arrives while the section is still a
 * skeleton and the named control is a network round trip away from existing —
 * which would strand the reader at the top of a section with the cap they were
 * sent to still below the fold. So the section is watched until the control
 * mounts, and the scroll lands on it the moment it does.
 *
 * Both bounds on that watch matter. The controls can only ever appear inside
 * this section, so that element is what is observed rather than the document,
 * which would wake this hook for every unrelated insertion on the page. And a
 * link can name a cap this organization has never materialized, which no
 * amount of waiting will produce, so the watch expires on its own.
 *
 * `mounted` waits for the render that puts the section in the document: the
 * anchor arrives with the navigation, but the section appears only once the
 * tier and the trial clock have resolved. The location key re-runs the scroll
 * for a repeat navigation to the same URL, which the hash alone can't see.
 */
function useScrollToInferenceCapHash(
  mounted: boolean,
): RefObject<HTMLDivElement | null> {
  const section = useRef<HTMLDivElement>(null);
  const { hash, key } = useLocation();

  useEffect(() => {
    if (!mounted) return;
    const target = hash.replace("#", "");
    if (!isInferenceCapAnchor(target)) return;
    const container = section.current;
    if (container === null) return;

    let watcher: MutationObserver | null = null;
    let expiry: number | undefined;

    function stopWatching(): void {
      watcher?.disconnect();
      watcher = null;
      window.clearTimeout(expiry);
    }

    /** Scrolls to the best element available, and whether that was the named one. */
    function scrollToTarget(): boolean {
      const named = document.getElementById(target);
      const element = named ?? document.getElementById(INFERENCE_CAPS_ANCHOR);
      element?.scrollIntoView({ behavior: "smooth", block: "start" });
      return named !== null;
    }

    // A frame's grace so the section's own body — a skeleton on the first
    // paint — has been laid out and the scroll lands on its settled position.
    const frame = window.requestAnimationFrame(() => {
      if (scrollToTarget()) return;

      // The named control isn't in the document yet. It arrives with the cap
      // list, whose render this hook has no other way to hear about, so the
      // section it renders into is what gets watched — until the control shows
      // up, until the wait runs out, or until this navigation stops being the
      // one being answered.
      watcher = new MutationObserver(() => {
        if (document.getElementById(target) === null) return;
        stopWatching();
        scrollToTarget();
      });
      watcher.observe(container, { childList: true, subtree: true });
      expiry = window.setTimeout(stopWatching, CAP_ANCHOR_WAIT_MS);
    });

    return () => {
      window.cancelAnimationFrame(frame);
      stopWatching();
    };
    // `key` is a trigger rather than an input: it identifies the navigation
    // being answered, so a repeat of the same one re-runs the scroll.
  }, [hash, key, mounted]);

  return section;
}

/**
 * The monthly ceilings on the inference Gram runs for this organization — one
 * independent control per Gram-managed key it has.
 *
 * The tier rule lives here rather than at the call site so the billing page can
 * place the section in both of its branches without either one re-deriving when
 * the caps apply.
 */
export function InferenceCapsSection(): JSX.Element | null {
  const mode = useInferenceCapsMode();
  const section = useScrollToInferenceCapHash(mode !== "hidden");

  if (mode === "hidden") return null;

  return (
    // A banner links at the control for the cap it names, and at this section
    // when that control isn't there to land on. `scroll-mt` clears the sticky
    // page header, which would otherwise cover what was jumped to.
    <div ref={section} id={INFERENCE_CAPS_ANCHOR} className="scroll-mt-24">
      <Page.Section>
        {/* Secondary section below Usage: suppress the area eyebrow. */}
        <Page.Section.Title area="">Inference caps</Page.Section.Title>
        <Page.Section.Description>
          Limit what this organization can spend each month on the inference
          Speakeasy runs for it. Each cap is enforced on its own.
        </Page.Section.Description>
        <Page.Section.Body>
          {mode === "product-trial" ? (
            <InferenceCaps lock={{ note: PRODUCT_TRIAL_NOTE }} />
          ) : (
            <PaygInferenceCapsGate />
          )}
        </Page.Section.Body>
      </Page.Section>
    </div>
  );
}

/**
 * The live Stripe subscription decides whether the caps can be changed, and it
 * fails closed: nothing reaches the write endpoint unless the most recent read
 * confirmed Stripe is billing.
 *
 * Every other answer still shows the caps. They exist and are enforced whatever
 * Stripe is doing — a trial has defaults, a canceled subscription leaves the
 * last amounts in place — so the state that can't be confirmed locks the
 * controls rather than hiding what this organization is running under.
 *
 * A refetch that is merely in flight is different again. Unmounting the forms
 * over one would throw away amounts an admin has typed but not saved, and
 * refetches arrive for reasons that have nothing to do with them — a window
 * refocus is enough. So a cached billing subscription keeps the forms mounted
 * and locks them: the drafts survive, and the endpoint stays out of reach until
 * the fresh state confirms the bill is still there.
 */
function PaygInferenceCapsGate(): JSX.Element {
  const { data, error, isError, isFetching, refetch } = useStripeSubscription();

  // A 404 is an answer, not an outage: the pay-as-you-go tier predates Stripe,
  // so an organization can be on it without a Stripe subscription behind it.
  // There is nothing a retry would find, so the recheck goes away.
  if (isNotFoundError(error)) {
    return <InferenceCaps lock={{ note: NO_SUBSCRIPTION_NOTE }} />;
  }

  // The billing state is unknown rather than known-bad, so the caps are shown
  // and locked, and the reason carries its own way out.
  if (isError) {
    return (
      <Stack gap={6}>
        <Stack direction="horizontal" align="center" gap={3}>
          <Text muted small role="alert">
            Couldn't check your subscription, so the inference caps can't be
            edited right now.
          </Text>
          <Button
            variant="secondary"
            size="sm"
            disabled={isFetching}
            onClick={() => void refetch()}
          >
            {isFetching ? "RECHECKING..." : "RECHECK"}
          </Button>
        </Stack>
        <InferenceCaps lock={{ note: null }} />
      </Stack>
    );
  }

  // Nothing has ever loaded, so there is no state to lock and no draft to
  // protect — just the placeholder.
  if (data === undefined) return <InferenceCapsSkeleton />;

  if (isStripeTrialing(data)) {
    return <InferenceCaps lock={{ note: stripeTrialNote(data.trialEnd) }} />;
  }

  // Only a subscription Stripe is actually billing has a bill for a changed cap
  // to apply to. Everything else — canceled, unpaid, paused, never completed —
  // shows the enforced amounts without a way to move them.
  if (!isStripeBilling(data)) {
    return <InferenceCaps lock={{ note: NOT_BILLING_NOTE }} />;
  }

  // Same element in the same position whether or not a read is in flight, so
  // React keeps the forms — and the amounts typed into them — across a refetch.
  return <InferenceCaps lock={null} refetching={isFetching} />;
}

function InferenceCapsSkeleton(): JSX.Element {
  return (
    <div className="max-w-md space-y-4">
      <Skeleton className="h-4 w-48" />
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-32" />
    </div>
  );
}

/**
 * One control per Gram-managed inference key this organization has.
 *
 * The list is the whole story: it carries the materialized, undeleted keys Gram
 * manages, and the section renders exactly those — two, one, or none — rather
 * than a shape it assumed.
 */
function InferenceCaps({
  lock,
  refetching = false,
}: {
  lock: CapsLock | null;
  /** The editable path's transient lock while the subscription is re-read. */
  refetching?: boolean;
}): JSX.Element {
  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down over one section.
  // This section handles its own failures, so it opts out and keeps them inline.
  const { data, isError, refetch, isFetching } = useGetInferenceSpendCaps(
    undefined,
    undefined,
    { throwOnError: false },
  );

  // A refetch that fails leaves the last successful list in the cache, so the
  // query reports data and an error together. The controls stay mounted in the
  // same child positions — swapping them for the error message would throw away
  // whatever the admin had typed — and the failure is reported beside them.
  if (data) {
    const caps = sortInferenceCaps(data);
    // The transient lock is one state shared by the whole section, so the first
    // control with a field to lock is the one that explains it.
    const announcer = caps.find((cap) => !cap.disabled)?.keyType;

    return (
      <Stack gap={8}>
        {lock?.note != null && (
          <Text muted small>
            {lock.note}
          </Text>
        )}
        {caps.length === 0 ? (
          <Text muted small>
            {NO_CAPS_NOTE}
          </Text>
        ) : (
          caps.map((cap) => (
            <InferenceCapControl
              key={cap.keyType}
              cap={cap}
              locked={lock !== null}
              refetching={refetching}
              announcesLock={cap.keyType === announcer}
            />
          ))
        )}
        {isError && (
          <Text muted small role="alert">
            Couldn't refresh the inference caps, so the amounts shown may be out
            of date. Saving still works.
          </Text>
        )}
      </Stack>
    );
  }

  // Nothing was ever cached, so there are no caps to show and no controls to
  // keep. The failure never reaches an error boundary, so recovery belongs
  // here: a retry of this one query rather than a reload of the billing page.
  if (isError) {
    return (
      <Stack direction="horizontal" align="center" gap={3}>
        <Text muted small role="alert">
          Couldn't load the inference caps.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          disabled={isFetching}
          onClick={() => void refetch()}
        >
          {isFetching ? "RETRYING..." : "RETRY"}
        </Button>
      </Stack>
    );
  }

  return <InferenceCapsSkeleton />;
}
