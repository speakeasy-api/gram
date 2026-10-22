import { useState } from "react";
import { ArrowLeft } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router";

import { Button } from "@/components/ui/Button";
import { InsightsConfig } from "@/components/insights-dock";
import { FormPage } from "@/components/page-templates";
import { useRoutes } from "@/routes";
import { encodeIdentityUrn } from "@/lib/identity-urn";

import { CredentialsWizard } from "./credentials/CredentialsWizard";

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
  // Opened from a link that names a platform, such as the Claude Tag button
  // on the agents list. The page then wears that platform's name.
  const [params] = useSearchParams();
  const platformKey = params.get("platform") ?? undefined;
  const claudeTag = platformKey === "claude-tag";
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
      title={
        claudeTag ? (
          <span className="flex items-center gap-3">
            <img
              src="/access-hub/claude-tag.svg"
              alt=""
              className="size-8 shrink-0"
            />
            Set up Claude Tag
          </span>
        ) : (
          "New agent identity"
        )
      }
      description={
        claudeTag
          ? "Choose the servers Claude can reach, then connect it to your Anthropic organization."
          : "Name it, choose the servers it can reach, then connect it to its runtime."
      }
      primaryAction={
        <Button variant="secondary" disabled={busy} onClick={() => done()}>
          <Button.LeftIcon>
            <ArrowLeft className="size-4" />
          </Button.LeftIcon>
          <Button.Text>All agents</Button.Text>
        </Button>
      }
    >
      {/* The dock floats over the bottom of the content, which on this page
          is the wizard's own footer and its Continue button. */}
      <InsightsConfig hideTrigger />
      <CredentialsWizard
        initialPlatformKey={platformKey}
        onDone={done}
        onBusy={setBusy}
      />
    </FormPage>
  );
}
