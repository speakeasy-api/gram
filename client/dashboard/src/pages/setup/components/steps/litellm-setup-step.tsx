import { useState } from "react";
import { Check } from "lucide-react";
import { useQueries } from "@tanstack/react-query";
import type { LiteLLMInstance } from "@gram/client/models/components/litellminstance.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildLiteLLMInstancesQuery } from "@gram/client/react-query/liteLLMInstances.js";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import {
  CreateInstanceDialog,
  HealthBadge,
  SetupContent,
} from "@/pages/org/litellm-integration-row";
import { useLiteLLMInstanceProjects } from "@/pages/org/use-litellm-instance-projects";
import { useOrgRoutes } from "@/routes";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { platformStatusBadge } from "../platform-status-badge";
import type { PlatformSetupStatus } from "../../types";

interface LiteLLMSetupStepProps {
  onComplete: () => void;
}

// Diagnostics update as the proxy reports in; the AI Integrations row polls
// at this rate while expanded and this card is the same kind of surface.
const POLL_INTERVAL_MS = 10_000;

// LiteLLM is a proxy, not a developer machine: nothing the device agent
// enrolls or a marketplace publishes reaches it. So this card skips the
// logging and marketplace sections its siblings open with and goes straight
// to the instance whose key the proxy authenticates with. Everything after
// that is read off the instance itself — its project, its failure posture,
// its connection health — so the card cannot disagree with the AI
// Integrations page.
export function LiteLLMSetupStep({
  onComplete,
}: LiteLLMSetupStepProps): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const client = useGramContext();
  const { projects, defaultProject } = useLiteLLMInstanceProjects();
  const [createOpen, setCreateOpen] = useState(false);
  // The instance the dialog just handed back, and when: the list is refetched
  // after creation, and until a refresh newer than that lands the list may
  // simply not have caught up, whereas after it the list is the truth (a
  // revoked instance drops out of it).
  const [created, setCreated] = useState<{
    instance: LiteLLMInstance;
    at: number;
  } | null>(null);
  // Per instance, so a second instance does not inherit the first one's
  // "configured".
  const [proxyStatus, setProxyStatus] = useState<
    Record<string, PlatformSetupStatus>
  >({});

  // An instance created on the AI Integrations page, or on this card before a
  // reload, is as good as one created here; and the list is what carries the
  // diagnostics the last section confirms on. Instances are listed per
  // project, and an admin may have bound theirs to any project, so every
  // project is asked.
  const instanceQueries = useQueries({
    queries: projects.map((project) => ({
      ...buildLiteLLMInstancesQuery(client, { gramProject: project.slug }),
      refetchInterval: POLL_INTERVAL_MS,
      retry: false,
    })),
  });
  const listPending = instanceQueries.some((query) => query.isPending);
  const listError = instanceQueries.some((query) => query.isError);
  const listed = instanceQueries
    .flatMap((query) => query.data?.instances ?? [])
    .filter((instance) => instance.active)
    .sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime());
  const createdQuery = created
    ? instanceQueries[
        projects.findIndex((p) => p.slug === created.instance.project.slug)
      ]
    : undefined;
  const createdStillUnlisted =
    created !== null && (createdQuery?.dataUpdatedAt ?? 0) < created.at;
  // An existing instance is only chosen once every project has answered;
  // choosing from a partial set would show an older instance's setup and
  // then swap it for a newer one as the remaining lists land.
  const instance =
    listed.find((candidate) => candidate.id === created?.instance.id) ??
    (createdStillUnlisted ? created.instance : null) ??
    (listPending ? null : listed[0]) ??
    null;
  const connected = instance?.diagnostics.status === "success";
  const status: PlatformSetupStatus = instance
    ? (proxyStatus[instance.id] ?? "not_started")
    : "not_started";
  const setStatus = (next: PlatformSetupStatus) => {
    if (!instance) return;
    setProxyStatus((prev) => ({ ...prev, [instance.id]: next }));
  };

  // What the later sections say while there is no instance to read from: an
  // unloadable list must not read as an empty one, or an admin creates a
  // duplicate of an instance they cannot see.
  let heldBack: string | undefined;
  if (!instance && listError) {
    heldBack =
      "Existing instances could not be loaded, so there is nothing to show here yet.";
  } else if (!instance && listPending) {
    heldBack = "Loading existing instances…";
  } else if (!instance) {
    heldBack = "Create an instance above first — this is generated from it.";
  }

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
          complete={instance !== null}
        >
          <div className="space-y-3">
            {instance ? (
              <p className="text-foreground text-sm">
                Using <span className="font-medium">{instance.name}</span> in{" "}
                {instance.project.name}.
              </p>
            ) : null}
            {!instance && listError ? (
              <Alert variant="error">
                <div>
                  <AlertTitle>Could not load existing instances</AlertTitle>
                  <AlertDescription>
                    One may already exist. Check the AI Integrations page before
                    creating another.
                  </AlertDescription>
                </div>
              </Alert>
            ) : null}
            <Button
              variant={instance ? "secondary" : "primary"}
              onClick={() => setCreateOpen(true)}
            >
              New instance
            </Button>
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
            onProjectCreated={() => {}}
            onInstanceCreated={(instance) =>
              setCreated({ instance, at: Date.now() })
            }
          />
        </StepSection>

        <StepSection
          index={2}
          slug="configure-proxy"
          title="Configure the proxy"
          description="Set the environment variables and merge the guardrail fragment into the proxy's config, then restart it. These name the instance's project and failure posture, so they are the ones shown when it was created."
          complete={status === "complete"}
          aside={platformStatusBadge(status)}
        >
          {instance ? (
            <div className="space-y-8">
              <SetupContent instance={instance} />
              <ProxyConfiguredToggle
                status={status}
                onStatusChange={setStatus}
              />
            </div>
          ) : (
            <p className="text-muted-foreground text-sm">{heldBack}</p>
          )}
        </StepSection>

        <StepSection
          index={3}
          slug="confirm-traffic"
          title="Confirm traffic"
          description="Send a chat completion through the proxy with any virtual key. The instance reports Connected once the guardrail or the OpenTelemetry exporter reaches Speakeasy, whatever client sent the request."
          complete={connected}
          aside={instance ? <HealthBadge instance={instance} /> : undefined}
        >
          {instance ? (
            <div className="space-y-1">
              <Text muted small>
                Last guardrail event
              </Text>
              <Text small>
                {instance.diagnostics.lastGuardrailEventAt ? (
                  <HumanizeDateTime
                    date={instance.diagnostics.lastGuardrailEventAt}
                  />
                ) : (
                  "Not received"
                )}
              </Text>
            </div>
          ) : (
            <p className="text-muted-foreground text-sm">{heldBack}</p>
          )}
        </StepSection>
      </div>
    </StepContainer>
  );
}

// The same configured/not-yet toggle PlatformSetupFlow ends with, for a
// section whose instructions come from the instance rather than setup-data.
function ProxyConfiguredToggle({
  status,
  onStatusChange,
}: {
  status: PlatformSetupStatus;
  onStatusChange: (status: PlatformSetupStatus) => void;
}): JSX.Element {
  if (status === "complete") {
    return (
      <div className="border-border bg-secondary/20 flex items-center justify-between border p-4">
        <p className="text-foreground flex items-center gap-2 text-sm">
          <Check className="text-default-success h-4 w-4" strokeWidth={3} />
          LiteLLM is configured.
        </p>
        <Button
          variant="tertiary"
          onClick={() => onStatusChange("not_started")}
        >
          Not yet
        </Button>
      </div>
    );
  }
  return (
    <Button variant="secondary" onClick={() => onStatusChange("complete")}>
      Mark LiteLLM as configured
    </Button>
  );
}
