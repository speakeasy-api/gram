import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { DetectorMode } from "./policy-data";

/** The detector mode the policy surfaces should render for this organization:
 *  `llm` while the fine-tuned risk analyzer flag is enabled, `presidio`
 *  otherwise (including while the flag is still loading, missing or errored,
 *  which fails closed to the legacy engine like the server does). */
export function useDetectorMode(): DetectorMode {
  const flag = useFeatureFlag(FEATURE_FLAGS.riskLlmAnalyzer);
  return flag.status === "enabled" ? "llm" : "presidio";
}
