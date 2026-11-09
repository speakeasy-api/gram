import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import {
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { AgentAPIKeys } from "@/pages/agents/AgentAPIKeys";
import { AgentOwnership } from "@/pages/agents/AgentOwnership";
import { AgentIdentityFacts, AgentLifecycle } from "@/pages/agents/agent-admin";
import { AgentMcpSignIn } from "./AgentMcpSignIn";
import { AgentFederatedAccess } from "@/pages/agents/AgentFederatedAccess";
import { useAgentAdmissions } from "@/pages/agents/agentAdmissions";
import { ManagedAgentSessions } from "@/pages/agents/ManagedAgentSessions";
import { SignInPath } from "@/pages/agents/credentials/SignInPath";
import { AgentIdentityPermissions } from "./AgentIdentityAccess";
import { AgentCredentialRestrictions } from "./AgentCredentialRestrictions";
import {
  AgentIdentityAudit,
  AgentIdentityChallenges,
} from "./AgentIdentityActivity";
import { AgentChatSessionsPanel } from "./AgentChatSessions";
import { AgentIssueKey } from "./AgentIssueKey";
import { AgentIdentityUsage } from "./AgentIdentityUsage";
import { IdentityPanel } from "./IdentityPanel";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { IdentitySection } from "./IdentitySection";
import { IdentityToolLogsPanel } from "./IdentityToolLogs";
import { useShowLocalTools } from "./identityLocalTools";
import { LocalToolsSwitch } from "./LocalToolsSwitch";

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
  const location = useLocation();
  const routes = useRoutes();
  // `?credential=new` opens issuance on this section. It is a query rather
  // than a segment of its own because issuing is a state of the Agent sign-in
  // section, and because the links that used to name the retired
  // agent-management page carry it.
  const [params, setParams] = useSearchParams();
  // One choice for the overview's Tool calls card and its Tool logs, so the
  // chart and the rows under it always count the same calls.
  const [showLocalTools, setShowLocalTools] = useShowLocalTools();
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
  // Keys and federated access are revoked where they are issued, so their
  // connection rows link there.
  const credentialsHref = routes.identities.detail.credentials.href(
    encodeIdentityUrn(`agent:${agent.id}`),
  );
  const setIssuing = (open: boolean) => {
    const next = new URLSearchParams(params);
    if (open) next.set("credential", "new");
    else next.delete("credential");
    // The issuing steps keep their place in `step`; a closed flow has none.
    next.delete("step");
    setParams(next, { replace: true });
  };
  switch (section) {
    case "permissions":
      return (
        <IdentitySection
          title="Permissions"
          timeRange
          meta="What this agent's keys may be narrowed to"
        >
          <AgentIdentityPermissions agent={agent} />
          <AgentCredentialRestrictions agent={agent} />
          <AgentIdentityChallenges agent={agent} />
        </IdentitySection>
      );
    case "mcp-sign-in":
      return (
        <IdentitySection
          title="MCP sign-in"
          meta="Which MCP servers this agent calls, and how Speakeasy signs in to each"
        >
          <AgentMcpSignIn agent={agent} refresh={refresh} />
        </IdentitySection>
      );
    case "audit-trail":
      return (
        <IdentitySection title="Audit trail" timeRange>
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
          <AgentOwnership agent={agent} />
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
          timeRange
          meta="What this agent did, and the clients signed in as it"
        >
          <AgentChatSessionsPanel agent={agent} limit={10} />
          {/* Padded rather than full-bleed: the full list carries a pager and
              permission notes beneath its table, which need an inset. */}
          <IdentityPanel title="MCP connections" contentClassName="p-4">
            <ManagedAgentSessions
              agent={agent}
              variant="bare"
              credentialsHref={credentialsHref}
            />
          </IdentityPanel>
        </IdentitySection>
      );
    case "sign-in":
      if (issuing) {
        // The same steps that issued the agent's first key. The servers it
        // reaches and how it signs in to them are the agent's, set on its
        // own page, so a new key is not asked about them again.
        return (
          <IdentitySection
            title="Issue a key"
            meta="Issue another key the way the first one was issued, then install it where the agent runs"
          >
            <AgentIssueKey agent={agent} onDone={() => setIssuing(false)} />
          </IdentitySection>
        );
      }
      // An agent holds keys, not provider logins and not machines. The
      // managed-device panel that sat here could only ever say so.
      //
      // The gateway endpoint is not repeated here: the wizard hands it over
      // while connecting the runtime, and a second copy under the keys read
      // as a second thing to configure.
      return (
        <IdentitySection
          title="Agent sign-in"
          meta={
            !credentialKnown
              ? "How this agent proves who it is to Speakeasy"
              : federated
                ? "The platforms that sign in as this agent, and the keys it holds"
                : "The keys this agent holds, and the platforms that sign in as it"
          }
        >
          {/* Issuing is a flow of its own, so the button puts this section
              into it. Without this the control is enabled and does nothing:
              the panel only opens its own wizard in creation mode. */}
          {/* Whichever way in the agent actually uses comes first: a
              federated agent never holds a key, and leading with the key
              panel told it that it could not authenticate. Only the order and
              the wording wait on the read — issuing and revoking keys is
              managed under a scope of its own, so someone who may do it must
              not lose the panel because this read was refused. */}
          {/* The same picture the wizard shows on this step: this is the
              agent's own sign-in, not its sign-in to each MCP server. */}
          <SignInPath hop="agent" />
          {credentialKnown && federated && (
            <AgentFederatedAccess agent={agent} />
          )}
          <AgentAPIKeys
            agent={agent}
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
        devices: "sign-in",
        provisioning: "sign-in",
        credentials: "sign-in",
        activity: "audit-trail",
      };
      // The section's own segment is swapped for the new one. A relative `..`
      // does not work here: this renders in the identity route's place, so
      // it climbed past the identity and landed on "Identity not found".
      const sectionHref = (next: string) =>
        `${location.pathname.replace(/\/[^/]*\/?$/, "")}/${next}${location.search}`;
      const moved = renamed[section];
      if (moved) {
        return <Navigate to={sectionHref(moved)} replace />;
      }
      if (section !== "overview") {
        return <Navigate to={sectionHref("overview")} replace />;
      }
      return (
        <IdentitySection
          title="Overview"
          timeRange
          meta="What this agent has been doing, and who answers for it"
          action={
            <LocalToolsSwitch
              checked={showLocalTools}
              onCheckedChange={setShowLocalTools}
            />
          }
        >
          {/* Who answers for it and whether it may act, as one line. The
              rest of what this card used to hold is said by the header above
              or by the Permissions section, and was read twice. */}
          <AgentIdentityFacts agent={agent} />
          {/* What the agent has actually been doing, before what it is
              allowed to do: an overview is opened to find out whether
              something is wrong, and the policy is where you look next. */}
          <AgentIdentityUsage
            agent={agent}
            includeLocalTools={showLocalTools}
          />
          {/* The sessions the agent's hooks recorded. Usage above can read
              empty for an agent that is plainly working, so this is the
              surest sign of what it has been doing. */}
          <AgentChatSessionsPanel agent={agent} limit={3} />
          {/* Who is signed in as it right now, capped. The Sessions section
              owns the full list and the revoking; this says whether anything
              is live without making the reader go and look. */}
          <IdentityPanel
            title="MCP connections"
            handoffLabel="all sessions"
            // Carries the query across: the date range lives in the URL, so a
            // bare path silently put the reader back on the default window
            // when they came back to the overview.
            handoffHref={`${routes.identities.detail.sessions.href(
              encodeIdentityUrn(`agent:${agent.id}`),
            )}${params.toString() ? `?${params}` : ""}`}
          >
            <ManagedAgentSessions
              agent={agent}
              variant="panel"
              limit={3}
              credentialsHref={credentialsHref}
            />
          </IdentityPanel>
          <AgentIdentityAudit agent={agent} subject />
          {/* Every call it made, under the usage summary's numbers. */}
          <IdentityToolLogsPanel
            agentId={agent.id}
            includeLocalTools={showLocalTools}
          />
        </IdentitySection>
      );
  }
}
