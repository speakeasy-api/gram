import { PROJECT_GUIDE_OVERVIEW_FROM } from "@/components/project-guide/allTimeOverviewQuery";
import { hasBlockingSecretsPolicy } from "@/components/project-guide/journeyStatus";
import type { JourneyStatus } from "@/components/project-guide/journeys";
import { useFetcher } from "@/contexts/Fetcher";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import {
  invalidateAllRiskListPolicies,
  useRiskListPolicies,
} from "@gram/client/react-query/riskListPolicies.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { useListHooksTraces } from "@gram/client/react-query/listHooksTraces.js";
import { useQueryClient } from "@tanstack/react-query";
import { motion, useReducedMotion } from "motion/react";
import { useEffect, useMemo, useRef, useState } from "react";

type Client = "claude" | "cursor" | "codex";

const CLIENTS: Record<Client, { label: string; directory: string }> = {
  claude: { label: "Claude Code", directory: "~/.claude/plugins/" },
  cursor: { label: "Cursor", directory: "~/.cursor/extensions/" },
  codex: { label: "Codex", directory: "~/.codex/plugins/" },
};

const SYNTHETIC_SECRET_PROMPT =
  "Use this AWS key to list my S3 buckets: AKIAIOSFODNN7EXAMPLE / wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY";

function filename(response: Response, client: Client): string {
  return (
    response.headers
      .get("Content-Disposition")
      ?.match(/filename="(.+)"/)?.[1] ?? `observability-${client}.zip`
  );
}

function Section({
  title,
  children,
  onSwitchJourney,
}: {
  title: string;
  children: React.ReactNode;
  onSwitchJourney: () => void;
}): JSX.Element {
  const reducedMotion = useReducedMotion();

  return (
    <motion.section
      initial={{ opacity: 0, y: reducedMotion ? 0 : 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        duration: reducedMotion ? 0 : 0.34,
        ease: [0.2, 0.7, 0.3, 1],
      }}
      className="border-border grid gap-4 border-l-2 border-l-destructive py-4 pl-4"
    >
      <div className="flex items-center justify-between gap-4">
        <h4 className="text-[19px] leading-[1.2]">{title}</h4>
        <button
          type="button"
          onClick={onSwitchJourney}
          className="text-muted-foreground font-mono text-[10px] tracking-[0.05em] uppercase"
        >
          Switch journey
        </button>
      </div>
      {children}
    </motion.section>
  );
}

export function SecretBlockJourney({
  status,
  onComplete,
  onSwitchJourney,
}: {
  status: JourneyStatus;
  onComplete: () => void;
  onSwitchJourney: () => void;
}): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const queryClient = useQueryClient();
  const { fetch: authFetch } = useFetcher();
  const completionNotified = useRef(false);
  const [client, setClient] = useState<Client>("claude");
  const [isCreating, setIsCreating] = useState(false);
  const [isDownloading, setIsDownloading] = useState(false);
  const [policyError, setPolicyError] = useState(false);
  const [downloadError, setDownloadError] = useState(false);
  const [downloadedClient, setDownloadedClient] = useState<Client>();
  const traceWindow = useMemo(
    () => ({ from: new Date(PROJECT_GUIDE_OVERVIEW_FROM), to: new Date() }),
    [],
  );
  const policiesQuery = useRiskListPolicies({ gramProject }, undefined, {
    throwOnError: false,
  });
  const hasPolicy =
    !policiesQuery.isError &&
    hasBlockingSecretsPolicy(policiesQuery.data?.policies);
  const tracesQuery = useListHooksTraces(
    {
      gramProject,
      listHooksTracesPayload: { ...traceWindow, limit: 1 },
    },
    undefined,
    {
      enabled: hasPolicy,
      refetchInterval: hasPolicy ? 2_000 : false,
      throwOnError: false,
    },
  );
  const hasHookTrace =
    !tracesQuery.isError && Boolean(tracesQuery.data?.traces.length);
  const createPolicy = useRiskCreatePolicyMutation();

  useEffect(() => {
    if (status !== "done" || completionNotified.current) return;
    completionNotified.current = true;
    onComplete();
  }, [onComplete, status]);

  const publishPolicy = async () => {
    if (policiesQuery.isError || policiesQuery.isPending || hasPolicy) return;
    setPolicyError(false);
    setIsCreating(true);
    try {
      await createPolicy.mutateAsync({
        request: {
          gramProject,
          createRiskPolicyRequestBody: {
            enabled: true,
            action: "block",
            sources: ["gitleaks"],
            messageTypes: ["tool_request", "tool_response"],
            audienceType: "everyone",
            policyType: "standard",
            autoName: true,
          },
        },
      });
      await invalidateAllRiskListPolicies(queryClient);
    } catch {
      setPolicyError(true);
    } finally {
      setIsCreating(false);
    }
  };

  const downloadPlugin = async () => {
    setDownloadError(false);
    setIsDownloading(true);
    try {
      const response = await authFetch(
        `/rpc/plugins.downloadObservabilityPlugin?platform=${client}`,
        {},
      );
      if (!response.ok) {
        setDownloadError(true);
        return;
      }
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement("a");
      link.href = url;
      link.download = filename(response, client);
      link.click();
      URL.revokeObjectURL(url);
      setDownloadedClient(client);
    } catch {
      setDownloadError(true);
    } finally {
      setIsDownloading(false);
    }
  };

  if (status === "done") {
    return (
      <Section title="The prompt was denied" onSwitchJourney={onSwitchJourney}>
        <p className="text-muted-foreground max-w-[52ch] text-[13px] leading-[1.6]">
          The secrets policy rejected the prompt before the model answered.
          Review the finding in Risk Events.
        </p>
      </Section>
    );
  }

  if (!hasPolicy) {
    if (policiesQuery.isError) {
      return (
        <Section
          title="Create a secrets policy"
          onSwitchJourney={onSwitchJourney}
        >
          <p className="text-destructive text-[13px] leading-[1.6]">
            Could not check for an existing secrets policy.
          </p>
          <button
            type="button"
            onClick={() => void policiesQuery.refetch()}
            className="border-border w-fit border px-3 py-2 font-mono text-[11px] uppercase"
          >
            Retry policy check
          </button>
        </Section>
      );
    }

    return (
      <Section
        title="Create a secrets policy"
        onSwitchJourney={onSwitchJourney}
      >
        <p className="text-muted-foreground max-w-[52ch] text-[13px] leading-[1.6]">
          Deny secrets in tool requests and responses for everyone in this
          project.
        </p>
        {policyError && (
          <p className="text-destructive text-[13px] leading-[1.6]">
            Could not publish the secrets policy.
          </p>
        )}
        <button
          type="button"
          disabled={policiesQuery.isPending || isCreating}
          onClick={() => void publishPolicy()}
          className="bg-foreground text-background disabled:bg-muted disabled:text-muted-foreground w-fit px-3 py-2 font-mono text-[11px] uppercase"
        >
          {isCreating ? "Publishing policy" : "Publish policy"}
        </button>
      </Section>
    );
  }

  if (hasHookTrace) {
    return (
      <Section title="Trigger the policy" onSwitchJourney={onSwitchJourney}>
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          Plugin connected. Send the synthetic secret prompt.
        </p>
        <pre className="border-border bg-muted/30 max-w-[52ch] overflow-x-auto border-l-2 border-l-destructive p-3 font-mono text-[11px] leading-[1.55] whitespace-pre-wrap">
          {SYNTHETIC_SECRET_PROMPT}
        </pre>
      </Section>
    );
  }

  const currentClient = CLIENTS[client];
  return (
    <Section
      title="Install the observability plugin"
      onSwitchJourney={onSwitchJourney}
    >
      <p className="text-muted-foreground max-w-[52ch] text-[13px] leading-[1.6]">
        Download the project plugin, then add it to the agent you use.
      </p>
      <div
        role="tablist"
        aria-label="Agent installation instructions"
        className="border-border flex border-b"
      >
        {(Object.keys(CLIENTS) as Client[]).map((platform) => (
          <button
            key={platform}
            type="button"
            role="tab"
            aria-selected={client === platform}
            onClick={() => setClient(platform)}
            className={
              client === platform
                ? "border-foreground border-b-2 px-3 py-2 font-mono text-[10.5px] uppercase"
                : "text-muted-foreground px-3 py-2 font-mono text-[10.5px] uppercase"
            }
          >
            {CLIENTS[platform].label}
          </button>
        ))}
      </div>
      <pre className="border-border bg-muted/30 max-w-[52ch] overflow-x-auto border p-3 font-mono text-[11px] leading-[1.55]">
        {`unzip observability-${client}.zip -d ${currentClient.directory}`}
      </pre>
      <p className="text-muted-foreground text-[13px] leading-[1.6]">
        Extract the downloaded ZIP into {currentClient.directory} and restart{" "}
        {currentClient.label}. A download does not confirm installation; Gram
        waits for the first hook event.
      </p>
      {downloadError && (
        <p className="text-destructive text-[13px] leading-[1.6]">
          Failed to download observability plugin.
        </p>
      )}
      {downloadedClient === client && (
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          ZIP downloaded. Waiting for the first hook event.
        </p>
      )}
      <button
        type="button"
        disabled={isDownloading}
        onClick={() => void downloadPlugin()}
        className="bg-foreground text-background disabled:bg-muted disabled:text-muted-foreground w-fit px-3 py-2 font-mono text-[11px] uppercase"
      >
        {isDownloading ? "Downloading ZIP" : "Download ZIP"}
      </button>
      {tracesQuery.isError ? (
        <p className="text-destructive text-[13px] leading-[1.6]">
          Could not check for hook events. Retry after installing the plugin.
        </p>
      ) : (
        <p className="text-muted-foreground font-mono text-[11px]">
          Waiting for the first hook event
        </p>
      )}
    </Section>
  );
}
