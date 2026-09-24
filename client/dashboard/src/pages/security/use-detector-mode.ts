import { useFeatureFlagVariant } from "@/hooks/useFeatureFlagVariant";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { DetectorMode } from "./policy-data";

/** The detector mode the policy surfaces should render for this organization.
 *
 *  `gram-risk-llm-analyzer` is a multivariate flag (`off`, `shadow`, `llm`):
 *  only the `llm` variant switches the editor to the analyzer's collapsed
 *  personal-data category. `shadow` runs the analyzer server-side for
 *  comparison but still enforces with the legacy engine, so it renders the
 *  `presidio` editor like `off`, loading, missing and errored do (failing
 *  closed to the legacy engine like the server).
 *
 *  Transition rule: until the flag is converted from boolean to multivariate
 *  in PostHog, a boolean flag has no variant and its enabled read still means
 *  `llm`. PostHog's boolean read is true for every variant, `off` included,
 *  so it is only consulted while no variant is present. */
export function useDetectorMode(): DetectorMode {
  const flag = useFeatureFlagVariant(FEATURE_FLAGS.riskLlmAnalyzer);
  if (flag.status !== "resolved") return "presidio";
  if (flag.variant === "llm") return "llm";
  const booleanOnly = flag.variant === undefined || flag.variant === "true";
  return booleanOnly && flag.enabled ? "llm" : "presidio";
}
