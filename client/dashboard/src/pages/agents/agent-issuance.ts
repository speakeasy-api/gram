import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

/**
 * Why this agent cannot be issued a key, or null when it can.
 *
 * One rule, because more than one surface offers the action: the keys panel
 * and the provisioning review both put an "Issue a key" button on screen, and
 * a button enabled by a weaker test than the one that runs on submit is a
 * button that fails after the click.
 *
 * It names the condition that actually blocks rather than listing all four:
 * three of them are not things the reader can act on.
 */
export function agentIssuanceBlocked(
  agent: ManagedAgent,
  credentialsEnabled: boolean,
): string | null {
  if (!credentialsEnabled) {
    return "Issuing agent keys is not enabled for this organization.";
  }
  if (!agent.permissions.authorize) {
    return "You do not have permission to issue keys for this agent.";
  }
  if (agent.lifecycle !== "active") {
    return `This agent is ${agent.lifecycle}, so it cannot be issued new keys.`;
  }
  if (agent.ownerReassignmentRequiredAt) {
    return "This agent needs a new owner before it can be issued keys.";
  }
  return null;
}
