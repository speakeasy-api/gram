import { useSlugs } from "@/contexts/Sdk";
import { createPersistedFlagStore } from "@/hooks/usePersistedFlagStore";
import { useProductTier } from "@/hooks/useProductTier";
import { useRBAC } from "@/hooks/useRBAC";
import { withViewTransition } from "@/lib/view-transition";

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

// The banner lives in the page header and the resume button lives in the
// sidebar footer — different parts of the tree — so they sync off this
// localStorage-backed store instead of a shared React context.
const store = createPersistedFlagStore("gram-onboarding-banner-dismissed");

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

  const dismissed = store.useFlag(orgSlug);

  const eligible =
    Boolean(orgSlug) && productTier === "enterprise" && hasScope("org:admin");

  const dismiss = () => {
    if (orgSlug) withViewTransition(() => store.write(orgSlug, true));
  };

  const resume = () => {
    if (orgSlug) withViewTransition(() => store.write(orgSlug, false));
  };

  return { eligible, dismissed, dismiss, resume };
}
