import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { HumanizeDateTime } from "@/lib/dates";
import { AgentAPIKeys } from "@/pages/agents/AgentAPIKeys";
import { ManagedAgentSessions } from "@/pages/agents/ManagedAgentSessions";
import { AgentIdentityPermissions } from "./AgentIdentityAccess";
import {
  AgentIdentityAudit,
  AgentIdentityChallenges,
} from "./AgentIdentityActivity";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
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
        <IdentitySection title="Access">
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
          title="Connections"
          meta="Current agent sessions across the organization"
        >
          <ManagedAgentSessions agent={agent} />
        </IdentitySection>
      );
    case "devices":
      return (
        <IdentitySection title="Accounts & devices">
          <AgentAPIKeys agent={agent} />
          <IdentityPanel title="Managed devices">
            <IdentityPanelEmpty>
              Device assignments are recorded for people. No device inventory is
              attributed to registered agents.
            </IdentityPanelEmpty>
          </IdentityPanel>
        </IdentitySection>
      );
    case "security":
      return (
        <IdentitySection title="Security">
          <AgentIdentityChallenges agent={agent} />
          <AgentTelemetryUnavailable section="Risk findings" />
        </IdentitySection>
      );
    case "usage":
      return (
        <IdentitySection title="Usage">
          <AgentTelemetryUnavailable section="Usage" />
        </IdentitySection>
      );
    case "cost":
      return (
        <IdentitySection title="Cost">
          <AgentTelemetryUnavailable section="Cost" />
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

function AgentTelemetryUnavailable({
  section,
}: {
  section: string;
}): JSX.Element {
  return (
    <IdentityPanel title={section}>
      <IdentityPanelEmpty>
        {section} cannot currently be queried by registered agent identity.
        These figures are unavailable for this agent.
      </IdentityPanelEmpty>
    </IdentityPanel>
  );
}
