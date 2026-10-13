import { RequireScope } from "@/components/require-scope";
import { AiScanTargetsSection } from "@/pages/device-agent/ai-scan-targets";

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
      <AiScanTargetsSection title="Shadow AI targets" />
    </RequireScope>
  );
}
