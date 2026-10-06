import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { HumanizeDateTime } from "@/lib/dates";
import { AgentAPIKeys } from "@/pages/agents/AgentAPIKeys";
import { ManagedAgentSessions } from "@/pages/agents/ManagedAgentSessions";
import { AgentIdentityPermissions } from "./AgentIdentityAccess";
import {
  AgentIdentityAudit,
  AgentIdentityChallenges,
} from "./AgentIdentityActivity";
import { IdentityPanel, IdentityPanelRow } from "./IdentityPanel";
import { IdentitySection } from "./IdentitySection";

/** Agent data is keyed by its principal, never by its human owner's identifiers. */
export function AgentIdentityProfile({
  agent,
  section,
}: {
  agent: ManagedAgent;
  section: string;
}): JSX.Element {
  switch (section) {
    case "access":
      return (
        <IdentitySection title="Permissions">
          <AgentIdentityPermissions agent={agent} />
          <AgentIdentityChallenges agent={agent} />
        </IdentitySection>
      );
    case "activity":
      return (
        <IdentitySection title="Activity">
          <AgentIdentityAudit agent={agent} subject />
          <AgentIdentityAudit agent={agent} />
        </IdentitySection>
      );
    case "connections":
      return (
        <IdentitySection
          title="Sessions"
          meta="Current agent sessions across the organization"
        >
          <ManagedAgentSessions agent={agent} />
        </IdentitySection>
      );
    case "devices":
      // An agent holds keys, not provider logins and not machines. The
      // managed-device panel that sat here could only ever say so.
      return (
        <IdentitySection title="Keys">
          <AgentAPIKeys agent={agent} />
        </IdentitySection>
      );
    default:
      return (
        <IdentitySection>
          <IdentityPanel title="Agent status">
            <IdentityPanelRow title="Status" trailing={agent.lifecycle} />
            <IdentityPanelRow
              title="Owner"
              trailing={agent.ownerProfile?.displayName || agent.ownerUserId}
            />
            {agent.ownerReassignmentRequiredAt && (
              <IdentityPanelRow
                title="Owner reassignment required"
                detail={agent.ownerReassignmentReason}
                accent="destructive"
              />
            )}
            <IdentityPanelRow
              title="Created"
              trailing={<HumanizeDateTime date={agent.createdAt} />}
            />
          </IdentityPanel>
          <AgentIdentityPermissions agent={agent} />
          <AgentIdentityChallenges agent={agent} />
          <AgentIdentityAudit agent={agent} subject />
        </IdentitySection>
      );
  }
}
