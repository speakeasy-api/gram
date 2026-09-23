import type { OnboardingState } from "@gram/client/models/components/onboardingstate.js";
import { useOnboarding } from "@gram/client/react-query/onboarding.js";
import { useOnboardingUseCaseStatus } from "@gram/client/react-query/onboardingUseCaseStatus.js";
import { useSlugs } from "@/contexts/Sdk";
import { useNewOnboardingEnabled } from "@/hooks/useNewOnboarding";
import { useRBAC } from "@/hooks/useRBAC";
import { bannerDismissedStore, wizardSkippedStore } from "./onboarding-stores";

export type OnboardingEntryMode = "redirect" | "banner" | "none";

/**
 * How org home should surface onboarding for this admin. A brand-new
 * organization, one with no answers and no evidence for any use case, goes
 * straight to the wizard. An organization that already has traffic, or that
 * has started answering, gets a dismissible banner instead.
 */
export function useOnboardingEntry(): {
  mode: OnboardingEntryMode;
  state: OnboardingState | undefined;
} {
  const enabled = useNewOnboardingEnabled();
  const { hasScope } = useRBAC();
  const { orgSlug } = useSlugs();
  const isAdmin = hasScope("org:admin");
  const active = enabled && isAdmin;

  const onboarding = useOnboarding(undefined, undefined, { enabled: active });
  const hasAnswers = Boolean(onboarding.data?.answers);
  const statuses = useOnboardingUseCaseStatus(undefined, undefined, {
    enabled: active && onboarding.isSuccess && !hasAnswers,
  });
  const skipped = wizardSkippedStore.useDismissed(orgSlug);
  const dismissed = bannerDismissedStore.useDismissed(orgSlug);

  if (!active || !onboarding.isSuccess) {
    return { mode: "none", state: onboarding.data };
  }
  const state = onboarding.data;
  if (state.done) {
    return { mode: "none", state };
  }
  if (hasAnswers || skipped) {
    return { mode: dismissed ? "none" : "banner", state };
  }
  if (!statuses.isSuccess) {
    return { mode: "none", state };
  }
  const anyEvidence = statuses.data.statuses.some((status) => status.verified);
  if (anyEvidence) {
    return { mode: dismissed ? "none" : "banner", state };
  }
  return { mode: "redirect", state };
}
