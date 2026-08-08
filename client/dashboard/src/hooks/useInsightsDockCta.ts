import { useSlugs } from "@/contexts/Sdk";
import { createPersistedFlagStore } from "@/hooks/usePersistedFlagStore";
import { withViewTransition } from "@/lib/view-transition";

// Shared Tailwind classes that tag BOTH the docked Project Assistant composer
// and the sidebar resume button with the same view-transition-name — only one
// of the two is rendered at a time (the dismissed flag decides which), so the
// browser genies one into the other when the flag flips. Must stay static
// string literals: Tailwind's scanner can't evaluate template interpolation.
// The names must match the ::view-transition-*(insights-dock) rules in App.css.
export const INSIGHTS_DOCK_VT_CLASS = "[view-transition-name:insights-dock]";
export const INSIGHTS_DOCK_CONTENT_VT_CLASS =
  "[view-transition-name:insights-dock-content]";

// The dock floats over the page content and the resume button lives in the
// sidebar footer — different parts of the tree — so they sync off this
// localStorage-backed store instead of a shared React context.
const store = createPersistedFlagStore("gram-insights-dock-dismissed");

/**
 * Coordinates the docked Project Assistant composer across its two surfaces:
 * the floating "Ask anything" dock and the sidebar-footer resume button shown
 * once the dock is dismissed. `dismiss`/`resume` animate the swap with the
 * same genie view transition as the onboarding CTA (see App.css).
 */
export function useInsightsDockCta(): {
  dismissed: boolean;
  dismiss: () => void;
  resume: () => void;
} {
  const { orgSlug } = useSlugs();
  const dismissed = store.useFlag(orgSlug);

  const dismiss = () => {
    if (orgSlug) withViewTransition(() => store.write(orgSlug, true));
  };

  const resume = () => {
    if (orgSlug) withViewTransition(() => store.write(orgSlug, false));
  };

  return { dismissed, dismiss, resume };
}
