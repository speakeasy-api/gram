import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { AgentAPIKeys } from "@/pages/agents/AgentAPIKeys";
import {
  AgentIdentityPanel,
  AgentLifecycle,
  RenameAgentButton,
} from "@/pages/agents/agent-admin";
import { AgentFederatedAccess } from "@/pages/agents/AgentFederatedAccess";
import { useAgentAdmissions } from "@/pages/agents/agentAdmissions";
import { AgentGatewayInstall } from "@/pages/agents/AgentGatewayInstall";
import { ManagedAgentSessions } from "@/pages/agents/ManagedAgentSessions";
import { AgentIdentityPermissions } from "./AgentIdentityAccess";
import {
  AgentIdentityAudit,
  AgentIdentityChallenges,
} from "./AgentIdentityActivity";
import { IdentityPanel } from "./IdentityPanel";
import { IdentitySection } from "./IdentitySection";

/** Agent data is keyed by its principal, never by its human owner's identifiers. */
export function AgentIdentityProfile({
  agent,
  section,
  refresh,
}: {
  agent: ManagedAgent;
  section: string;
  /** Re-reads the agent after it is renamed or its lifecycle changes. */
  refresh: () => void;
}): JSX.Element {
  const navigate = useNavigate();
  const routes = useRoutes();
  // `?credential=new` opens issuance on this section. It is a query rather
  // than a segment of its own because issuing is a state of the provisioning
  // section, and because the links that used to name the retired
  // agent-management page carry it.
  const [params, setParams] = useSearchParams();
  // A federated agent authenticates by exchanging its platform's token, so
  // the page must not lead with, or insist on, a key it will never hold.
  const admissions = useAgentAdmissions(agent.id);
  // Which way in the agent uses is a fact about it, not a default: while the
  // read is in flight, and after it fails, "not federated" would be a guess,
  // and the guess is the one that tells a federated agent it cannot
  // authenticate until a key is issued.
  const credentialKnown = !admissions.isPending && !admissions.isError;
  // A wildcard rule the platform has not turned on admits nothing, so it is
  // not a way in: counting it would tell an agent that holds only a dormant
  // rule that it is already signed in and needs no key.
  const federated =
    credentialKnown &&
    admissions.admissions.some(
      (admission) =>
        admission.matchKind !== "wildcard" || admission.wildcardActive,
    );
  const issuing = params.get("credential") === "new";
  const setIssuing = (open: boolean) => {
    const next = new URLSearchParams(params);
    if (open) next.set("credential", "new");
    else next.delete("credential");
    setParams(next, { replace: true });
  };
  switch (section) {
    case "permissions":
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
    case "controls":
      return (
        <IdentitySection
          title="Controls & Safety"
          meta="Stop this agent acting, for a while or for good"
        >
          <AgentLifecycle
            agent={agent}
            refresh={refresh}
            onDeleted={() => void navigate(routes.identities.agents.href())}
          />
        </IdentitySection>
      );
    case "sessions":
      return (
        <IdentitySection
          title="Sessions"
          meta="Current agent sessions across the organization"
        >
          <ManagedAgentSessions agent={agent} variant="bare" />
        </IdentitySection>
      );
    case "provisioning":
      if (issuing) {
        return (
          <IdentitySection
            title="Issue a key"
            meta="Choose what this key reaches, then install it where the agent runs"
          >
            <AgentAPIKeys
              agent={agent}
              creation
              onDone={() => setIssuing(false)}
            />
          </IdentitySection>
        );
      }
      // An agent holds keys, not provider logins and not machines. The
      // managed-device panel that sat here could only ever say so.
      //
      // The endpoint and the keys, under one heading. Both come bare: the
      // keys panel's own "Provision" header would repeat the section title
      // the rail just sent the reader to.
      return (
        <IdentitySection
          title="Provisioning"
          meta={
            !credentialKnown
              ? "Point the agent's runtime here"
              : federated
                ? "Point the agent's runtime here; its platform signs it in"
                : "Point the agent's runtime here, then issue it a key"
          }
        >
          <IdentityPanel title="Endpoint" contentClassName="p-4">
            <AgentGatewayInstall agentID={agent.id} secret={null} />
          </IdentityPanel>
          {/* Issuing is a flow of its own, so the button puts this section
              into it. Without this the control is enabled and does nothing:
              the panel only opens its own wizard in creation mode. */}
          {/* Whichever way in the agent actually uses comes first: a
              federated agent never holds a key, and leading with the key
              panel told it that it could not authenticate. Only the order and
              the wording wait on the read — issuing and revoking keys is
              managed under a scope of its own, so someone who may do it must
              not lose the panel because this read was refused. */}
          {credentialKnown && federated && (
            <AgentFederatedAccess agent={agent} />
          )}
          <AgentAPIKeys
            agent={agent}
            variant="bare"
            federated={credentialKnown ? federated : "unknown"}
            onCreate={() => setIssuing(true)}
          />
          {!credentialKnown ? (
            admissions.isError ? (
              <div role="alert" className="space-y-2">
                <Text>Could not check how this agent signs in.</Text>
                <Button variant="secondary" onClick={admissions.refetch}>
                  Try again
                </Button>
              </div>
            ) : (
              <Text muted>Checking how this agent signs in…</Text>
            )
          ) : (
            !federated && <AgentFederatedAccess agent={agent} />
          )}
        </IdentitySection>
      );
    default:
      // Agents have no usage, cost, risk or findings view, and the rail does
      // not offer them. A link or bookmark to one still resolved and silently
      // rendered this overview, so the address said one thing and the page
      // showed another.
      // The sections an agent holds moved to addresses that match their
      // tabs. Links and bookmarks to the person-shaped ones still work.
      const renamed: Record<string, string> = {
        access: "permissions",
        connections: "sessions",
        devices: "provisioning",
      };
      const moved = renamed[section];
      if (moved) {
        return <Navigate to={`../${moved}${window.location.search}`} replace />;
      }
      if (section !== "overview") {
        return <Navigate to={`../overview${window.location.search}`} replace />;
      }
      return (
        <IdentitySection
          title="Overview"
          meta="What this identity is, and who answers for it"
          action={
            <div className="flex items-center gap-2">
              {/* Changing what the agent reaches is the same set of
                  questions it was registered with, so it is the same flow. */}
              <Button asChild variant="secondary">
                <Link to={routes.identities.agents.edit.href(agent.id)}>
                  Edit
                </Link>
              </Button>
              <RenameAgentButton agent={agent} refresh={refresh} />
            </div>
          }
        >
          {/* Name, principal, owner, scope and lifecycle — the durable
              facts, from the one component that states them. */}
          <AgentIdentityPanel agent={agent} />
          <AgentIdentityPermissions agent={agent} />
          <AgentIdentityChallenges agent={agent} />
          <AgentIdentityAudit agent={agent} subject />
        </IdentitySection>
      );
  }
}
