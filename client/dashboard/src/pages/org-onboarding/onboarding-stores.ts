import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";

/**
 * The admin left the wizard before saving answers. Org home then offers the
 * banner instead of sending them straight back into the wizard.
 */
export const wizardSkippedStore = createDismissedCtaStore(
  "gram-onboarding-wizard-skipped",
);

/** The "complete setup" banner on org home was dismissed. */
export const bannerDismissedStore = createDismissedCtaStore(
  "gram-onboarding-banner-dismissed",
);
