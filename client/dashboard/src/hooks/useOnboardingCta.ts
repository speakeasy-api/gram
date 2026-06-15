import { useSlugs } from "@/contexts/Sdk";
import { useProductTier } from "@/hooks/useProductTier";
import { useRBAC } from "@/hooks/useRBAC";
import { useSyncExternalStore } from "react";
import { flushSync } from "react-dom";

// Shared Tailwind class that tags BOTH the dismissable onboarding banner and the
// sidebar resume button with the same view-transition-name. Because only one of
// the two is rendered at a time (the dismissed flag decides which), the browser
// morphs one into the other — the "genie" effect — when the flag flips.
// Must stay a static string literal: Tailwind's scanner can't evaluate template
// interpolation, so a computed class name would never be generated into the CSS.
// The name must match the ::view-transition-*(onboarding-cta) rules in App.css.
export const ONBOARDING_CTA_VT_CLASS = "[view-transition-name:onboarding-cta]";

// Applied to the content INSIDE each surface. Giving the content its own
// view-transition-name pulls it out of the container's snapshot, so the genie
// animates an empty surface while the text simply fades at the endpoints —
// otherwise the rasterized text visibly warps mid-flight.
export const ONBOARDING_CTA_CONTENT_VT_CLASS =
  "[view-transition-name:onboarding-cta-content]";

function storageKey(orgSlug: string) {
  return `gram-onboarding-banner-dismissed:${orgSlug}`;
}

function readDismissed(orgSlug: string): boolean {
  try {
    return localStorage.getItem(storageKey(orgSlug)) === "true";
  } catch {
    return false;
  }
}

// Module-level pub/sub. The banner lives in the page header and the resume button
// lives in the sidebar footer — different parts of the tree — so they sync off a
// single localStorage-backed source of truth instead of a shared React context.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function writeDismissed(orgSlug: string, value: boolean) {
  try {
    if (value) {
      localStorage.setItem(storageKey(orgSlug), "true");
    } else {
      localStorage.removeItem(storageKey(orgSlug));
    }
  } catch {
    // localStorage unavailable — listeners still fire so the in-memory UI updates
  }
  listeners.forEach((listener) => listener());
}

// Genie the banner into the resume button (or back). flushSync forces React to
// apply the state change synchronously inside the transition callback so the
// browser captures the post-update DOM; without it React 19 batches the update
// until after the snapshot and no transition plays. Falls back to a plain update
// where the View Transitions API is unavailable.
function withCtaViewTransition(update: () => void) {
  if (
    typeof document !== "undefined" &&
    typeof document.startViewTransition === "function"
  ) {
    document.startViewTransition(() => {
      flushSync(update);
    });
    return;
  }
  update();
}

/**
 * Coordinates the enterprise onboarding call-to-action across its two surfaces:
 * the dismissable banner in the page header and the persistent resume button in
 * the sidebar footer. Both gate on the same eligibility (enterprise org admin)
 * and the same dismissed flag, and `dismiss`/`resume` animate the swap.
 */
export function useOnboardingCta(): {
  eligible: boolean;
  dismissed: boolean;
  dismiss: () => void;
  resume: () => void;
} {
  const { orgSlug } = useSlugs();
  const { hasScope } = useRBAC();
  const productTier = useProductTier();

  const dismissed = useSyncExternalStore(
    subscribe,
    () => (orgSlug ? readDismissed(orgSlug) : false),
    () => false,
  );

  const eligible =
    Boolean(orgSlug) && productTier === "enterprise" && hasScope("org:admin");

  const dismiss = () => {
    if (orgSlug) withCtaViewTransition(() => writeDismissed(orgSlug, true));
  };

  const resume = () => {
    if (orgSlug) withCtaViewTransition(() => writeDismissed(orgSlug, false));
  };

  return { eligible, dismissed, dismiss, resume };
}
