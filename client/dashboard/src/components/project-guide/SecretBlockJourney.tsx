import { hasBlockingSecretsPolicy } from "@/components/project-guide/journeyStatus";
import { JourneyRun } from "@/components/project-guide/JourneyRun";
import {
  SECRET_BLOCK_STEPS,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import { getRuleTitleFallback } from "@/pages/security/risk-utils";
import { useRoutes } from "@/routes";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { useFetcher } from "@/contexts/Fetcher";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import {
  invalidateRiskListPolicies,
  useRiskListPolicies,
} from "@gram/client/react-query/riskListPolicies.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { useListHooksTraces } from "@gram/client/react-query/listHooksTraces.js";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useQueryClient } from "@tanstack/react-query";
import { motion, useReducedMotion } from "motion/react";
import { useEffect, useMemo, useRef, useState } from "react";

type Client = "claude" | "cursor" | "codex";
type PluginStage = "download" | "install";

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
      ?.match(/filename="(.+?)"/)?.[1] ?? `observability-${client}.zip`
  );
}

function shellFilename(value: string): string {
  if (/^[A-Za-z0-9._-]+$/.test(value)) return value;
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function pluginStageTitle(stage: PluginStage): string {
  if (stage === "install") return "Add it to your agent";
  return "Download the observability plugin";
}

function Section({
  title,
  children,
  currentStep,
  status,
  error,
  complete,
  paused,
  onPause,
  onResume,
  onSwitchJourney,
}: {
  title: string;
  children: React.ReactNode;
  currentStep: number;
  status: JourneyStatus;
  error?: boolean;
  complete?: boolean;
  paused?: boolean;
  onPause?: () => void;
  onResume?: () => void;
  onSwitchJourney: () => void;
}): JSX.Element {
  return (
    <JourneyRun
      journeyId="secret-block"
      steps={SECRET_BLOCK_STEPS}
      currentStep={currentStep}
      status={status}
      title={title}
      tone="destructive"
      error={error}
      complete={complete}
      paused={paused}
      onPause={onPause}
      onResume={onResume}
      onSwitchJourney={onSwitchJourney}
    >
      {children}
    </JourneyRun>
  );
}

function ClientTabs({
  client,
  onClientChange,
  children,
}: {
  client: Client;
  onClientChange: (client: Client) => void;
  children: (client: Client) => React.ReactNode;
}): JSX.Element {
  return (
    <Tabs
      value={client}
      onValueChange={(value) => onClientChange(value as Client)}
    >
      <PageTabsList aria-label="Agent installation instructions">
        {(Object.keys(CLIENTS) as Client[]).map((platform) => (
          <PageTabsTrigger key={platform} value={platform}>
            {CLIENTS[platform].label}
          </PageTabsTrigger>
        ))}
      </PageTabsList>
      {(Object.keys(CLIENTS) as Client[]).map((platform) => (
        <TabsContent key={platform} value={platform}>
          {children(platform)}
        </TabsContent>
      ))}
    </Tabs>
  );
}

function WaitingForHookEvent(): JSX.Element {
  const reducedMotion = useReducedMotion();

  return (
    <div role="status" className="border-border bg-muted/30 border">
      <div className="border-border flex items-center gap-2 border-b px-3 py-2">
        <motion.span
          aria-hidden="true"
          animate={reducedMotion ? undefined : { opacity: [0.35, 1, 0.35] }}
          transition={{ duration: 1.1, repeat: Infinity }}
          className="bg-foreground size-1.5"
        />
        <span className="font-mono text-[10px] tracking-[0.05em] uppercase">
          Live hook telemetry
        </span>
        <span className="text-muted-foreground ml-auto font-mono text-[10px] uppercase">
          Waiting
        </span>
      </div>
      <div className="grid gap-1 px-3 py-3">
        <p className="font-mono text-[11px]">
          Waiting for the first hook event
        </p>
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          Restart your agent after installing the ZIP. This updates only when a
          hook event reaches this project.
        </p>
      </div>
    </div>
  );
}

function WaitingForRiskEvent(): JSX.Element {
  const reducedMotion = useReducedMotion();

  return (
    <div role="status" className="border-border bg-muted/30 border">
      <div className="border-border flex items-center gap-2 border-b px-3 py-2">
        <motion.span
          aria-hidden="true"
          animate={reducedMotion ? undefined : { opacity: [0.35, 1, 0.35] }}
          transition={{ duration: 1.1, repeat: Infinity }}
          className="bg-foreground size-1.5"
        />
        <span className="font-mono text-[10px] tracking-[0.05em] uppercase">
          Live risk events
        </span>
        <span className="text-muted-foreground ml-auto font-mono text-[10px] uppercase">
          Listening
        </span>
      </div>
      <div className="grid gap-1 px-3 py-3">
        <p className="font-mono text-[11px]">
          Waiting for a blocked risk event
        </p>
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          Listening for risk events on this project. The policy should deny the
          prompt before the model answers.
        </p>
      </div>
    </div>
  );
}

function DeniedRiskEventCard({ result }: { result: RiskResult }): JSX.Element {
  const reducedMotion = useReducedMotion();

  return (
    <motion.div
      initial={reducedMotion ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        duration: reducedMotion ? 0 : 0.34,
        ease: [0.2, 0.7, 0.3, 1],
      }}
      className="border-border grid gap-3 border-l-2 border-l-destructive bg-muted/30 p-3"
    >
      <div className="grid gap-1">
        <span className="font-mono text-[10px] tracking-[0.05em] uppercase text-destructive">
          Denied · risk event
        </span>
        <p className="font-mono text-[11px]">Blocked by secrets policy</p>
      </div>
      <dl className="border-border grid gap-2 border-y py-2 text-[13px]">
        <div className="grid grid-cols-[64px_1fr] gap-2">
          <dt className="text-muted-foreground font-mono text-[10px] uppercase">
            Rule
          </dt>
          <dd>{getRuleTitleFallback(result.ruleId)}</dd>
        </div>
        {result.description && (
          <div className="grid grid-cols-[64px_1fr] gap-2">
            <dt className="text-muted-foreground font-mono text-[10px] uppercase">
              Result
            </dt>
            <dd className="text-muted-foreground">{result.description}</dd>
          </div>
        )}
      </dl>
      <p className="text-muted-foreground text-[13px] leading-[1.6]">
        The prompt was rejected before the model saw it.
      </p>
    </motion.div>
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
  const routes = useRoutes();
  const completionNotified = useRef(false);
  const [client, setClient] = useState<Client>("claude");
  const [pluginStage, setPluginStage] = useState<PluginStage>("download");
  const [isCreating, setIsCreating] = useState(false);
  const [isDownloading, setIsDownloading] = useState(false);
  const [policyError, setPolicyError] = useState(false);
  const [downloadError, setDownloadError] = useState(false);
  const [checksPaused, setChecksPaused] = useState(false);
  const [promptCopied, setPromptCopied] = useState(false);
  const [downloadedFilenames, setDownloadedFilenames] = useState<
    Partial<Record<Client, string>>
  >({});
  const policiesQuery = useRiskListPolicies({ gramProject }, undefined, {
    throwOnError: false,
  });
  const hasPolicy =
    !policiesQuery.isError &&
    hasBlockingSecretsPolicy(policiesQuery.data?.policies);
  const tracesRequest = useMemo(
    () => ({
      gramProject,
      listHooksTracesPayload: {
        from: new Date(0),
        to: new Date(),
        typesToInclude: ["mcp", "local", "skill"] as Array<
          "mcp" | "local" | "skill"
        >,
        limit: 10,
        sort: "desc" as const,
      },
    }),
    [gramProject],
  );
  const tracesQuery = useListHooksTraces(tracesRequest, undefined, {
    enabled: hasPolicy && !checksPaused,
    refetchInterval: (query) => {
      // The generated query key omits this payload. Keep the request object
      // stable for its query function, and advance its next upper bound here.
      tracesRequest.listHooksTracesPayload.to = new Date();
      if (query.state.data?.traces.length) return false;
      return hasPolicy && !checksPaused ? 2_000 : false;
    },
    throwOnError: false,
  });
  const hasHookTrace =
    !tracesQuery.isError && Boolean(tracesQuery.data?.traces.length);
  const resultsQuery = useRiskListResults(
    { gramProject, category: "secrets", limit: 10 },
    undefined,
    {
      enabled: hasHookTrace && !checksPaused,
      refetchInterval: (query) => {
        if (query.state.data?.results.length) return false;
        return hasHookTrace && !checksPaused ? 2_000 : false;
      },
      throwOnError: false,
    },
  );
  const riskResults = resultsQuery.data?.results;
  let latestRiskResult;
  if (hasHookTrace && !resultsQuery.isError && riskResults?.length) {
    latestRiskResult = riskResults.reduce((latest, result) =>
      result.createdAt > latest.createdAt ? result : latest,
    );
  }
  const hasRiskResult = Boolean(latestRiskResult);
  const createPolicy = useRiskCreatePolicyMutation();

  useEffect(() => {
    if (!hasRiskResult || completionNotified.current) return;
    completionNotified.current = true;
    onComplete();
  }, [hasRiskResult, onComplete]);

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
      await invalidateRiskListPolicies(queryClient, [{ gramProject }]);
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
      const downloadedFilename = filename(response, client);
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement("a");
      link.href = url;
      link.download = downloadedFilename;
      link.click();
      URL.revokeObjectURL(url);
      setDownloadedFilenames((current) => ({
        ...current,
        [client]: downloadedFilename,
      }));
      setPluginStage("install");
    } catch {
      setDownloadError(true);
    } finally {
      setIsDownloading(false);
    }
  };

  if (latestRiskResult) {
    return (
      <Section
        complete
        currentStep={4}
        status={status}
        title="The prompt was denied"
        onSwitchJourney={onSwitchJourney}
      >
        <div className="grid gap-4">
          <div className="grid gap-2">
            <span className="font-mono text-[10px] tracking-[0.05em] uppercase text-destructive">
              Journey B complete
            </span>
            <p className="text-muted-foreground max-w-[52ch] text-[13px] leading-[1.6]">
              The prompt matched the secrets policy and was rejected before the
              model answered.
            </p>
          </div>
          <div className="grid gap-2">
            <span className="font-mono text-[10px] tracking-[0.05em] uppercase text-muted-foreground">
              The event you watched
            </span>
            <DeniedRiskEventCard result={latestRiskResult} />
          </div>
          <routes.riskEvents.Link className="w-fit font-mono text-[11px] uppercase">
            Open Risk Events
          </routes.riskEvents.Link>
        </div>
      </Section>
    );
  }

  if (!hasPolicy) {
    if (policiesQuery.isError) {
      return (
        <Section
          currentStep={0}
          error
          status={status}
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
        currentStep={0}
        error={policyError}
        status={status}
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
      <Section
        currentStep={promptCopied ? 4 : 3}
        error={resultsQuery.isError}
        paused={checksPaused}
        status={status}
        title="Watch the block land"
        onPause={() => setChecksPaused(true)}
        onResume={() => setChecksPaused(false)}
        onSwitchJourney={onSwitchJourney}
      >
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          Plugin connected. Send the synthetic secret prompt.
        </p>
        <p className="font-mono text-[10px] tracking-[0.05em] uppercase text-muted-foreground">
          Copy this into {CLIENTS[client].label}
        </p>
        <pre className="border-border bg-muted/30 max-w-[52ch] overflow-x-auto border-l-2 border-l-destructive p-3 font-mono text-[11px] leading-[1.55] whitespace-pre-wrap">
          {SYNTHETIC_SECRET_PROMPT}
        </pre>
        <button
          type="button"
          onClick={() => {
            setPromptCopied(true);
            void navigator.clipboard?.writeText(SYNTHETIC_SECRET_PROMPT);
          }}
          className="border-border w-fit border px-3 py-2 font-mono text-[11px] uppercase"
        >
          Copy prompt
        </button>
        <p className="font-mono text-[10px] tracking-[0.05em] uppercase text-muted-foreground">
          Expected result: blocked
        </p>
        {resultsQuery.isError ? (
          <div className="grid gap-3">
            <p className="text-destructive text-[13px] leading-[1.6]">
              Could not check for blocked risk events.
            </p>
            <button
              type="button"
              onClick={() => void resultsQuery.refetch()}
              className="border-border w-fit border px-3 py-2 font-mono text-[11px] uppercase"
            >
              Retry risk events
            </button>
          </div>
        ) : (
          <WaitingForRiskEvent />
        )}
      </Section>
    );
  }

  const installationStage = pluginStage === "install";
  return (
    <Section
      currentStep={installationStage ? 2 : 1}
      error={downloadError || tracesQuery.isError}
      paused={checksPaused}
      status={status}
      title={pluginStageTitle(pluginStage)}
      onPause={installationStage ? () => setChecksPaused(true) : undefined}
      onResume={installationStage ? () => setChecksPaused(false) : undefined}
      onSwitchJourney={onSwitchJourney}
    >
      {installationStage ? (
        <>
          <p className="text-muted-foreground max-w-[52ch] text-[13px] leading-[1.6]">
            Extract the ZIP in your agent, then restart it. A download does not
            confirm installation.
          </p>
          <ClientTabs client={client} onClientChange={setClient}>
            {(platform) => {
              const currentClient = CLIENTS[platform];
              const downloadedFilename = downloadedFilenames[platform];
              return (
                <div className="grid gap-2">
                  {downloadedFilename ? (
                    <pre className="border-border bg-muted/30 max-w-[52ch] overflow-x-auto border p-3 font-mono text-[11px] leading-[1.55]">
                      {`unzip ${shellFilename(downloadedFilename)} -d ${currentClient.directory}`}
                    </pre>
                  ) : (
                    <p className="text-muted-foreground text-[13px] leading-[1.6]">
                      Download this client's ZIP before extracting it.
                    </p>
                  )}
                </div>
              );
            }}
          </ClientTabs>
          <WaitingForHookEvent />
          <button
            type="button"
            onClick={() => setPluginStage("download")}
            className="border-border w-fit border px-3 py-2 font-mono text-[11px] uppercase"
          >
            Download another ZIP
          </button>
        </>
      ) : (
        <>
          <p className="text-muted-foreground max-w-[52ch] text-[13px] leading-[1.6]">
            Download the project plugin, then add it to the agent you use.
          </p>
          <ClientTabs client={client} onClientChange={setClient}>
            {(platform) => (
              <div className="grid gap-3">
                <p className="text-muted-foreground text-[13px] leading-[1.6]">
                  Download a ZIP for {CLIENTS[platform].label}.
                </p>
                <button
                  type="button"
                  disabled={isDownloading}
                  onClick={() => void downloadPlugin()}
                  className="bg-foreground text-background disabled:bg-muted disabled:text-muted-foreground w-fit px-3 py-2 font-mono text-[11px] uppercase"
                >
                  {isDownloading ? "Downloading ZIP" : "Download ZIP"}
                </button>
              </div>
            )}
          </ClientTabs>
          {downloadError && (
            <p className="text-destructive text-[13px] leading-[1.6]">
              Failed to download observability plugin.
            </p>
          )}
        </>
      )}
      {tracesQuery.isError && (
        <div className="grid gap-2">
          <p className="text-destructive text-[13px] leading-[1.6]">
            Could not check for hook events. Retry after installing the plugin.
          </p>
          <button
            type="button"
            onClick={() => {
              tracesRequest.listHooksTracesPayload.to = new Date();
              void tracesQuery.refetch();
            }}
            className="border-border w-fit border px-3 py-2 font-mono text-[11px] uppercase"
          >
            Retry hook events
          </button>
        </div>
      )}
    </Section>
  );
}
