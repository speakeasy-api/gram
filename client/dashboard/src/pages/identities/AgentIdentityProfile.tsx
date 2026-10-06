import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { HumanizeDateTime } from "@/lib/dates";
import { Navigate, useNavigate } from "react-router";
import { useRoutes } from "@/routes";
import { registeredAgentHref } from "./identityRoster";
import { AgentAPIKeys } from "@/pages/agents/AgentAPIKeys";
import { AgentGatewayInstall } from "@/pages/agents/AgentGatewayInstall";
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
  const navigate = useNavigate();
  const routes = useRoutes();
  switch (section) {
    case "access":
      return (
        <IdentitySection
          title="Permissions"
          meta="What this agent's keys may be narrowed to"
        >
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
          <ManagedAgentSessions agent={agent} variant="bare" />
        </IdentitySection>
      );
    case "devices":
      // An agent holds keys, not provider logins and not machines. The
      // managed-device panel that sat here could only ever say so.
      //
      // The endpoint and the keys, under one heading. Both come bare: the
      // keys panel's own "Provision" header would repeat the section title
      // the rail just sent the reader to.
      return (
        <IdentitySection
          title="Provisioning"
          meta="Point the agent's runtime here, then issue it a key"
        >
          <IdentityPanel title="Endpoint" contentClassName="p-4">
            <AgentGatewayInstall agentID={agent.id} secret={null} />
          </IdentityPanel>
          {/* Issuing is a flow of its own, so the button hands off to the
              page that runs it. Without this the control is enabled and does
              nothing: the panel only opens its own wizard in creation mode. */}
          <AgentAPIKeys
            agent={agent}
            variant="bare"
            onCreate={() => {
              void navigate(
                `${registeredAgentHref(routes.agents.href(), "", agent.id)}&credential=new`,
              );
            }}
          />
        </IdentitySection>
      );
    default:
      // Agents have no usage, cost, risk or findings view, and the rail does
      // not offer them. A link or bookmark to one still resolved and silently
      // rendered this overview, so the address said one thing and the page
      // showed another.
      if (section !== "overview") {
        return <Navigate to={`../overview${window.location.search}`} replace />;
      }
      return (
        <IdentitySection
          title="Overview"
          meta="What this identity is, and who answers for it"
        >
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
