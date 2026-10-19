import { InsightsConfig } from "@/components/insights-dock";
import { RequireScope } from "@/components/require-scope";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { AiScanTargetsSection } from "@/pages/device-agent/ai-scan-targets";
import { useAiScanTargets } from "@gram/client/react-query/aiScanTargets.js";
import { shadowAITargetsInsightsContext } from "./shadow-ai-targets-insights";

/**
 * The Shadow AI targets tab body: the organization's scan target library,
 * the same list Device Agent → Scan Targets shows. It is duplicated here for
 * now so the Shadow AI story reads end to end from Guardrails; the targets
 * stay organization-scoped until they move to project scope. The RPCs behind
 * the library require org:admin, so the tab gates itself the way the Device
 * Agent page does rather than relying on the page-level gate.
 */
export function ShadowAITargetsTab(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <ShadowAITargetsInsights />
      <AiScanTargetsSection title="Shadow AI targets" />
    </RequireScope>
  );
}

/** The dock follows the tab: target-specific prompts and the library's own
 *  facts, in place of the page's policy insights. Shares the section's query,
 *  so it costs no extra request. */
function ShadowAITargetsInsights() {
  const list = useAiScanTargets(undefined, undefined, { throwOnError: false });
  if (!list.data) return null;
  return (
    <InsightsConfig
      contextInfo={shadowAITargetsInsightsContext(list.data)}
      suggestions={INSIGHTS_SUGGESTIONS["shadow-ai-targets"]}
      title="Shadow AI target insights"
      subtitle="Ask what the library covers and what shadow MCP activity looks like. Match content is redacted before it reaches the assistant."
    />
  );
}
