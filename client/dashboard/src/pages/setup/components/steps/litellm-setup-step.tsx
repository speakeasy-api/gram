import { useMemo, useState } from "react";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Button } from "@/components/ui/Button";
import { useOrganization } from "@/contexts/Auth";
import { CreateInstanceDialog } from "@/pages/org/litellm-integration-row";
import { useOrgRoutes } from "@/routes";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { isLiteLLMSource } from "../hook-event-sources";
import { PlatformSetupFlow } from "../platform-setup-flow";
import { platformStatusBadge } from "../platform-status-badge";
import type { PlatformSetupStatus } from "../../types";

interface LiteLLMSetupStepProps {
  onComplete: () => void;
}

// LiteLLM is a proxy, not a developer machine: nothing the device agent
// enrolls or a marketplace publishes reaches it. So this card skips the
// logging and marketplace sections its siblings open with and goes straight
// to the instance whose key the proxy authenticates with.
export function LiteLLMSetupStep({
  onComplete,
}: LiteLLMSetupStepProps): JSX.Element {
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const [createOpen, setCreateOpen] = useState(false);
  const [instancesCreated, setInstancesCreated] = useState(0);
  const [proxyStatus, setProxyStatus] =
    useState<PlatformSetupStatus>("not_started");

  const projects = useMemo(
    () =>
      [...organization.projects].sort((a, b) => a.name.localeCompare(b.name)),
    [organization.projects],
  );
  const defaultProject =
    projects.find((project) => project.slug === "default") ?? projects[0];

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <AgentProviderIcon source="litellm" className="h-6 w-6" />
        </div>
      }
      title="Set up LiteLLM"
      description="Point your LiteLLM proxy at Speakeasy so every request through it is scanned by your risk policies and its usage lands in observability. Create an instance to get an ingestion key, configure the proxy with it, and confirm events arrive."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <StepSection
          index={1}
          slug="create-instance"
          title="Create a LiteLLM instance"
          description="Each proxy gets its own instance with a dedicated, project-bound ingestion key. The key is shown once when the instance is created, so copy it before closing the dialog."
          complete={instancesCreated > 0}
        >
          <div className="space-y-3">
            <Button onClick={() => setCreateOpen(true)}>New instance</Button>
            <p className="text-muted-foreground text-sm leading-relaxed">
              Existing instances, key rotation, and connection diagnostics live
              on the{" "}
              <orgRoutes.aiIntegrations.Link className="text-foreground underline underline-offset-2">
                AI Integrations
              </orgRoutes.aiIntegrations.Link>{" "}
              page.
            </p>
          </div>
          <CreateInstanceDialog
            open={createOpen}
            onOpenChange={setCreateOpen}
            projects={projects}
            initialProjectSlug={defaultProject?.slug ?? ""}
            onProjectCreated={() => setInstancesCreated((count) => count + 1)}
          />
        </StepSection>

        <StepSection
          index={2}
          slug="configure-proxy"
          title="Configure the proxy"
          description="Set the environment variables and merge the guardrail fragment into the proxy's config, then restart it. The snippets match the ones shown when the instance was created."
          complete={proxyStatus === "complete"}
          aside={platformStatusBadge(proxyStatus)}
        >
          <PlatformSetupFlow
            platformId="litellm"
            status={proxyStatus}
            onStatusChange={setProxyStatus}
          />
        </StepSection>

        <ConfirmTrafficSection
          index={3}
          description="Send a chat completion through the proxy with any virtual key. The guardrail reports it here as soon as the proxy can reach Speakeasy."
          matchesSource={isLiteLLMSource}
        />
      </div>
    </StepContainer>
  );
}
