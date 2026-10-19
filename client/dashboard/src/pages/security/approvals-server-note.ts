import type { RiskPolicyApprovalsServerSetup } from "@gram/client/models/components/riskpolicyapprovalsserversetup.js";

/** What the toast says when saving a policy set up the project's approvals
 *  server: the server agents call request_approval on, now in the Default
 *  plugin so agents that use the plugin get it once it republishes. */
export function approvalsServerSetupNote(
  setup: RiskPolicyApprovalsServerSetup | undefined,
): string | undefined {
  if (!setup) return undefined;
  if (!setup.addedToDefaultPlugin) {
    return `Created the ${setup.pluginServerName} server, which agents call to request approval. Add it to the plugins your agents use.`;
  }
  return `Added ${setup.pluginServerName} to the Default plugin, which republishes so agents can request approval.`;
}
