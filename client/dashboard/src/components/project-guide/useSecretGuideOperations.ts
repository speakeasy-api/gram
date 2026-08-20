import type {
  ProjectGuideEventCard,
  ProjectGuideOperationReport,
  ProjectGuideOperationScope,
  ProjectGuideOperationSignal,
} from "@/components/project-guide/projectGuideMachine";
import { hasBlockingSecretsPolicy } from "@/components/project-guide/journeyStatus";
import { useFetcher } from "@/contexts/Fetcher";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { downloadResponse } from "@/pages/plugins/downloadPluginPackage";
import { getRuleTitleFallback } from "@/pages/security/risk-utils";
import { useRoutes } from "@/routes";
import type { HookTraceSummary } from "@gram/client/models/components/hooktracesummary.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useListHooksTraces } from "@gram/client/react-query/listHooksTraces.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import {
  invalidateRiskListPolicies,
  useRiskListPolicies,
} from "@gram/client/react-query/riskListPolicies.js";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type SecretGuideClient = "claude" | "cursor" | "codex";

export const SECRET_GUIDE_CLIENTS: Record<
  SecretGuideClient,
  { label: string; directory: string }
> = {
  claude: { label: "Claude Code", directory: "~/.claude/plugins/" },
  cursor: { label: "Cursor", directory: "~/.cursor/extensions/" },
  codex: { label: "Codex", directory: "~/.codex/plugins/" },
};

const SECRET_GUIDE_PROMPT =
  "Use this dummy secret as an AWS key to list my S3 buckets: AKIAIOSFODNN7EXAMPLE / wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY. This key is synthetic and inert; do not use it against a real service.";

type ActiveOperation = {
  scope: ProjectGuideOperationScope;
  report: (report: ProjectGuideOperationReport) => void;
  paused: boolean;
};

type TelemetryBaseline = {
  traceIds: Set<string>;
  resultIds: Set<string>;
};

function operationKey(scope: ProjectGuideOperationScope): string {
  return `${scope.path}:${scope.step}:${scope.attempt}:${scope.runId}`;
}

function matchingSecretsPolicy(
  policies: RiskPolicy[] | undefined,
): RiskPolicy | undefined {
  return policies?.find(
    (policy) =>
      policy.policyType === "standard" && hasBlockingSecretsPolicy([policy]),
  );
}

function responseFilename(response: Response, fallback: string): string {
  return (
    response.headers
      .get("Content-Disposition")
      ?.match(/filename="(.+?)"/)?.[1] ?? fallback
  );
}

function shellFilename(value: string): string {
  if (/^[A-Za-z0-9._-]+$/.test(value)) return value;
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function latestNewResult(
  results: RiskResult[] | undefined,
  baseline: TelemetryBaseline,
  policyId: string | undefined,
): RiskResult | undefined {
  return results
    ?.filter(
      (result) =>
        !baseline.resultIds.has(result.id) &&
        result.policyId === policyId &&
        result.source === "gitleaks",
    )
    .sort(
      (left, right) => right.createdAt.getTime() - left.createdAt.getTime(),
    )[0];
}

function hasNewBlockedTrace(
  traces: HookTraceSummary[] | undefined,
  baseline: TelemetryBaseline,
): boolean {
  return Boolean(
    traces?.some(
      (trace) =>
        trace.hookStatus === "blocked" && !baseline.traceIds.has(trace.traceId),
    ),
  );
}

export function useSecretGuideOperations(): {
  client: SecretGuideClient;
  downloadedFilename: string | undefined;
  handleSignal: (
    signal: ProjectGuideOperationSignal,
    report: (report: ProjectGuideOperationReport) => void,
  ) => void;
  installCommand: string | undefined;
  markPromptCopied: () => void;
  policyError: boolean;
  policyPending: boolean;
  prompt: string;
  promptCopied: boolean;
  retryBaseline: () => void;
  retryPolicy: () => void;
  riskEventsHref: string;
  setClient: (client: SecretGuideClient) => void;
  telemetryBaselineReady: boolean;
  telemetryError: boolean;
} {
  const gramProject = useProjectSlugForRequests();
  const { fetch: authFetch } = useFetcher();
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [client, setClientState] = useState<SecretGuideClient>("claude");
  const [downloadedFilename, setDownloadedFilename] = useState<string>();
  const [promptCopied, setPromptCopied] = useState(false);
  const [createdPolicyId, setCreatedPolicyId] = useState<string>();
  const [activeOperation, setActiveOperation] = useState<ActiveOperation>();
  const activeOperationRef = useRef<ActiveOperation | undefined>(undefined);
  const [baseline, setBaseline] = useState<TelemetryBaseline>();
  const baselineRef = useRef<TelemetryBaseline | undefined>(undefined);
  const [baselineError, setBaselineError] = useState(false);
  const [suppressTelemetryError, setSuppressTelemetryError] = useState(false);
  const startedFor = useRef(new Set<string>());

  const policiesQuery = useRiskListPolicies({ gramProject }, undefined, {
    throwOnError: false,
  });
  const matchingPolicy = matchingSecretsPolicy(policiesQuery.data?.policies);
  const policyId = matchingPolicy?.id ?? createdPolicyId;
  const policyPending = policiesQuery.isPending;
  const policyError =
    policiesQuery.isError ||
    (!policiesQuery.isPending && policiesQuery.data === undefined);
  const createPolicy = useRiskCreatePolicyMutation();

  const tracesRequest = useMemo(
    () => ({
      gramProject,
      listHooksTracesPayload: {
        from: new Date(0),
        to: new Date(),
        typesToInclude: ["mcp", "local", "skill"] as Array<
          "mcp" | "local" | "skill"
        >,
        limit: 20,
        sort: "desc" as const,
      },
    }),
    [gramProject],
  );
  const listening =
    activeOperation?.scope.step === 4 && !activeOperation.paused;
  const tracesQuery = useListHooksTraces(tracesRequest, undefined, {
    enabled: listening,
    refetchInterval: () => {
      tracesRequest.listHooksTracesPayload.to = new Date();
      return listening ? 2_000 : false;
    },
    throwOnError: false,
  });
  const resultsQuery = useRiskListResults(
    { gramProject, category: "secrets", policyId, limit: 20 },
    undefined,
    {
      enabled: listening,
      refetchInterval: listening ? 2_000 : false,
      throwOnError: false,
    },
  );
  const telemetryQueryError =
    tracesQuery.isError ||
    resultsQuery.isError ||
    (listening && !tracesQuery.isPending && tracesQuery.data === undefined) ||
    (listening && !resultsQuery.isPending && resultsQuery.data === undefined);
  const telemetryError = baselineError || telemetryQueryError;

  const updateActiveOperation = useCallback(
    (operation: ActiveOperation | undefined) => {
      activeOperationRef.current = operation;
      setActiveOperation(operation);
    },
    [],
  );

  const captureBaseline = useCallback(async (): Promise<void> => {
    baselineRef.current = undefined;
    setBaseline(undefined);
    setBaselineError(false);
    setSuppressTelemetryError(true);
    tracesRequest.listHooksTracesPayload.to = new Date();
    try {
      const [traces, results] = await Promise.all([
        tracesQuery.refetch(),
        resultsQuery.refetch(),
      ]);
      if (traces.isError || results.isError || !traces.data || !results.data) {
        setBaselineError(true);
        return;
      }
      const nextBaseline = {
        traceIds: new Set(traces.data.traces.map((trace) => trace.traceId)),
        resultIds: new Set(results.data.results.map((result) => result.id)),
      };
      baselineRef.current = nextBaseline;
      setBaseline(nextBaseline);
    } catch {
      setBaselineError(true);
    } finally {
      setSuppressTelemetryError(false);
    }
  }, [resultsQuery, tracesQuery, tracesRequest]);

  const retryTelemetry = useCallback(() => {
    setSuppressTelemetryError(true);
    tracesRequest.listHooksTracesPayload.to = new Date();
    void Promise.all([tracesQuery.refetch(), resultsQuery.refetch()]).finally(
      () => setSuppressTelemetryError(false),
    );
  }, [resultsQuery, tracesQuery, tracesRequest]);

  const handleSignal = useCallback(
    (
      signal: ProjectGuideOperationSignal,
      report: (report: ProjectGuideOperationReport) => void,
    ): void => {
      if (signal.scope.path !== "secret-block") return;
      if (signal.type === "abort") {
        updateActiveOperation(undefined);
        baselineRef.current = undefined;
        setBaseline(undefined);
        return;
      }
      if (signal.type === "pause") {
        const current = activeOperationRef.current;
        if (
          current &&
          operationKey(current.scope) === operationKey(signal.scope)
        ) {
          updateActiveOperation({ ...current, paused: true });
        }
        return;
      }
      if (signal.type === "checkpoint") {
        if (signal.scope.step === 2) void captureBaseline();
        return;
      }
      updateActiveOperation({ scope: signal.scope, report, paused: false });
      if (signal.type === "retry" && signal.scope.step === 4) {
        retryTelemetry();
      }
    },
    [captureBaseline, retryTelemetry, updateActiveOperation],
  );

  useEffect(() => {
    const operation = activeOperation;
    if (!operation || operation.paused || operation.scope.step !== 0) return;
    if (policyPending) return;
    if (policyError) {
      updateActiveOperation(undefined);
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          "Could not read this project's risk policies. Retry the policy check.",
      });
      return;
    }
    if (matchingPolicy || createdPolicyId) {
      updateActiveOperation(undefined);
      operation.report({
        type: "success",
        scope: operation.scope,
        result: "Secrets policy already live · block on match",
      });
      return;
    }
    const key = operationKey(operation.scope);
    if (startedFor.current.has(key)) return;
    startedFor.current.add(key);
    operation.report({
      type: "progress",
      scope: operation.scope,
      message: "Creating a blocking secrets policy for this project",
      progress: 0.25,
    });
    void createPolicy
      .mutateAsync({
        request: {
          gramProject,
          createRiskPolicyRequestBody: {
            action: "block",
            audienceType: "everyone",
            autoName: true,
            enabled: true,
            messageTypes: ["tool_request", "tool_response"],
            policyType: "standard",
            sources: ["gitleaks"],
          },
        },
      })
      .then((policy) => {
        updateActiveOperation(undefined);
        setCreatedPolicyId(policy.id);
        void invalidateRiskListPolicies(queryClient, [{ gramProject }]);
        operation.report({
          type: "success",
          scope: operation.scope,
          result: "Secrets policy created · block on match",
        });
      })
      .catch(() => {
        updateActiveOperation(undefined);
        operation.report({
          type: "error",
          scope: operation.scope,
          message:
            "Could not create the blocking secrets policy. Retry the policy step.",
        });
      });
  }, [
    activeOperation,
    createPolicy,
    createdPolicyId,
    gramProject,
    matchingPolicy,
    policyError,
    policyPending,
    queryClient,
    updateActiveOperation,
  ]);

  useEffect(() => {
    const operation = activeOperation;
    if (!operation || operation.paused || operation.scope.step !== 1) return;
    if (downloadedFilename) {
      updateActiveOperation(undefined);
      operation.report({
        type: "success",
        scope: operation.scope,
        result: `Observability plugin downloaded · ${downloadedFilename}`,
      });
      return;
    }
    const key = operationKey(operation.scope);
    if (startedFor.current.has(key)) return;
    startedFor.current.add(key);
    operation.report({
      type: "progress",
      scope: operation.scope,
      message: `Downloading the ${SECRET_GUIDE_CLIENTS[client].label} observability plugin`,
      progress: 0.5,
    });
    void authFetch(
      `/rpc/plugins.downloadObservabilityPlugin?platform=${client}`,
      {},
    )
      .then(async (response) => {
        if (!response.ok) throw new Error("download failed");
        const fallback = `observability-${client}.zip`;
        const filename = responseFilename(response, fallback);
        await downloadResponse(response, fallback);
        setDownloadedFilename(filename);
        updateActiveOperation(undefined);
        operation.report({
          type: "success",
          scope: operation.scope,
          result: `Observability plugin downloaded · ${filename}`,
        });
      })
      .catch(() => {
        updateActiveOperation(undefined);
        operation.report({
          type: "error",
          scope: operation.scope,
          message:
            "Could not download the observability plugin. Retry the download step.",
        });
      });
  }, [
    activeOperation,
    authFetch,
    client,
    downloadedFilename,
    updateActiveOperation,
  ]);

  useEffect(() => {
    const operation = activeOperation;
    if (!operation || operation.paused || operation.scope.step !== 4) return;
    const currentBaseline = baselineRef.current;
    if (!currentBaseline) {
      updateActiveOperation(undefined);
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          "Could not capture the event baseline before the prompt. Return to the prompt step and try again.",
      });
      return;
    }
    if (
      suppressTelemetryError ||
      tracesQuery.isPending ||
      resultsQuery.isPending
    ) {
      return;
    }
    if (telemetryError) {
      updateActiveOperation(undefined);
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          "Could not check for the new blocked event. Retry after checking the plugin connection.",
      });
      return;
    }
    const riskResult = latestNewResult(
      resultsQuery.data?.results,
      currentBaseline,
      policyId,
    );
    if (
      !riskResult ||
      !hasNewBlockedTrace(tracesQuery.data?.traces, currentBaseline)
    ) {
      return;
    }
    const event: ProjectGuideEventCard = {
      kind: "Denied · risk event",
      tone: "deny",
      title: "request denied by secrets policy",
      rows: [
        { key: "rule", value: getRuleTitleFallback(riskResult.ruleId) },
        { key: "match", value: "synthetic credential" },
      ],
      note: "The prompt was blocked before the model answered.",
    };
    updateActiveOperation(undefined);
    operation.report({ type: "event", scope: operation.scope, event });
  }, [
    activeOperation,
    policyId,
    resultsQuery.data?.results,
    resultsQuery.isPending,
    suppressTelemetryError,
    telemetryError,
    tracesQuery.data?.traces,
    tracesQuery.isPending,
    updateActiveOperation,
  ]);

  const installCommand = downloadedFilename
    ? `unzip ${shellFilename(downloadedFilename)} -d ${SECRET_GUIDE_CLIENTS[client].directory}`
    : undefined;

  return {
    client,
    downloadedFilename,
    handleSignal,
    installCommand,
    markPromptCopied: () => setPromptCopied(true),
    policyError,
    policyPending,
    prompt: SECRET_GUIDE_PROMPT,
    promptCopied,
    retryBaseline: () => {
      void captureBaseline();
    },
    retryPolicy: () => {
      void policiesQuery.refetch();
    },
    riskEventsHref: routes.riskEvents.href(),
    setClient: (nextClient) => {
      if (downloadedFilename) return;
      setClientState(nextClient);
      setPromptCopied(false);
    },
    telemetryBaselineReady: baseline !== undefined,
    telemetryError,
  };
}
