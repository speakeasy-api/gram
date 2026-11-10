import { ArrowLeft } from "lucide-react";
import { useState } from "react";
import {
  Navigate,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router";

import { InsightsConfig } from "@/components/insights-dock";
import { FormPage } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { useOrganization } from "@/contexts/Auth";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { useRoutes } from "@/routes";
import {
  invalidateAgentsListPolicyGrants,
  useAgentsListPolicyGrants,
} from "@gram/client/react-query/agentsListPolicyGrants.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  AGENT_SIGN_IN_STEP,
  CredentialsWizard,
} from "./credentials/CredentialsWizard";

/**
 * Changing how an existing agent signs in to Speakeasy: the setup wizard from
 * its Agent sign-in step, opened from the agent's credentials tab.
 *
 * Its name is renamed in the agent page's header and its servers changed on
 * the MCP sign-in tab, so links to the earlier steps (no step, the name, the
 * servers, their sign-in) land there instead.
 */
export default function EditAgentIdentity(): JSX.Element {
  const routes = useRoutes();
  const { agentId = "" } = useParams<{ agentId: string }>();
  const [params] = useSearchParams();
  const step = Number(params.get("step") ?? "");
  if (!Number.isInteger(step) || step < AGENT_SIGN_IN_STEP) {
    const view = step === 1 ? "?view=servers" : "";
    return (
      <Navigate
        to={`${routes.identities.detail.mcpSignIn.href(
          encodeIdentityUrn(`agent:${agentId}`),
        )}${view}`}
        replace
      />
    );
  }
  return <EditAgentSignIn agentId={agentId} />;
}

function EditAgentSignIn({ agentId }: { agentId: string }): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const sdk = useSdkClient();
  const organization = useOrganization();
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState(false);

  const agentQuery = useQuery({
    queryKey: ["managed-agents", organization.id, "detail", agentId],
    queryFn: () => sdk.agents.get({ id: agentId }),
    retry: false,
    throwOnError: false,
  });
  const grantsQuery = useAgentsListPolicyGrants({ agentId }, undefined, {
    throwOnError: false,
  });

  const done = (id?: string) => {
    // The wizard seeds its server selection from these grants, so a cached
    // pre-save read would reopen it with the servers the save just changed
    // — and the next save, with nothing touched, would take one back out.
    void invalidateAgentsListPolicyGrants(queryClient, [{ agentId }]);
    void navigate(
      id
        ? routes.identities.detail.credentials.href(
            encodeIdentityUrn(`agent:${id}`),
          )
        : routes.identities.agents.href(),
    );
  };

  const agent = agentQuery.data;
  const grants = grantsQuery.data;

  return (
    <FormPage
      width="full"
      // The path carries the agent's id, which names nothing to a reader.
      // Undefined, not "": the header shows its pending placeholder for a
      // name it does not have yet, and a blank crumb for an empty string.
      breadcrumbSubstitutions={{ [agentId]: agent?.name }}
      // The agent's crumb is the agent's own page, not this form's parent.
      breadcrumbUrlSubstitutions={{
        [`/identities/agents/${agentId}`]: `/identities/${encodeIdentityUrn(`agent:${agentId}`)}/overview`,
      }}
      title={agent ? `Edit ${agent.name}` : "Edit agent identity"}
      description="Change how this agent signs in to Speakeasy."
      primaryAction={
        <Button variant="secondary" disabled={busy} onClick={() => done()}>
          <Button.LeftIcon>
            <ArrowLeft className="size-4" />
          </Button.LeftIcon>
          <Button.Text>All agents</Button.Text>
        </Button>
      }
    >
      <InsightsConfig hideTrigger />
      {agentQuery.isError ? (
        <Text role="alert">This agent could not be loaded.</Text>
      ) : grantsQuery.isError ? (
        // Checked before the undefined-data branch: a failed read never
        // produces data, so without this the page waits on it forever.
        <div role="alert" className="space-y-2">
          <Text>
            The servers this agent reaches could not be loaded, so there is
            nothing to edit yet.
          </Text>
          <Button
            variant="secondary"
            onClick={() => void grantsQuery.refetch()}
          >
            Try again
          </Button>
        </div>
      ) : agent === undefined || grants === undefined ? (
        <Text muted>Loading agent…</Text>
      ) : (
        <CredentialsWizard
          agent={agent}
          editing
          grants={grants}
          onDone={done}
          onBusy={setBusy}
        />
      )}
    </FormPage>
  );
}
