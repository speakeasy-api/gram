import { useState } from "react";
import { ArrowLeft } from "lucide-react";
import { useNavigate } from "react-router";

import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { FormPage } from "@/components/page-templates";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRoutes } from "@/routes";
import { encodeIdentityUrn } from "@/lib/identity-urn";

import { ProvisionWizard } from "./provision/ProvisionWizard";

/**
 * Registering an agent, which is the one thing about an agent that is a
 * sequence: it is named, given a reach, given a credential, and then that
 * credential is installed where it runs. Each step depends on the one before,
 * so this is a wizard.
 *
 * Reading an agent afterwards is not a sequence, and lives on the agent's own
 * page.
 */
export default function NewAgentIdentity(): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  // The wizard ends in an issued key, so it belongs to the credentials
  // rollout and not to `agent-management`: the roster this page is reached
  // from is on the wider gate, and without this the page would issue a key
  // for an organization that credentials are not enabled for.
  const credentials = useFeatureFlag(FEATURE_FLAGS.agentCredentials);

  const done = (agentID?: string) => {
    void navigate(
      agentID
        ? routes.identities.detail.overview.href(
            encodeIdentityUrn(`agent:${agentID}`),
          )
        : routes.identities.agents.href(),
    );
  };

  return (
    <FormPage
      width="full"
      title="New agent identity"
      description="Name it, choose the servers it can reach, then connect it to its runtime."
      primaryAction={
        <Button variant="secondary" disabled={busy} onClick={() => done()}>
          <Button.LeftIcon>
            <ArrowLeft className="size-4" />
          </Button.LeftIcon>
          <Button.Text>All agents</Button.Text>
        </Button>
      }
    >
      {credentials.status === "loading" ? (
        <Text muted>Checking what this organization can provision…</Text>
      ) : credentials.status === "enabled" ? (
        <ProvisionWizard onDone={done} onBusy={setBusy} />
      ) : (
        <Text muted>
          Issuing agent keys is not enabled for this organization.
        </Text>
      )}
    </FormPage>
  );
}
