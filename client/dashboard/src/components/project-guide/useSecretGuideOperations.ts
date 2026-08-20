import type {
  ProjectGuideEventCard,
  ProjectGuideOperationReport,
  ProjectGuideOperationScope,
  ProjectGuideOperationSignal,
} from "@/components/project-guide/projectGuideMachine";
import { hasBlockingSecretsPolicy } from "@/components/project-guide/journeyStatus";
import { useOrganization } from "@/contexts/Auth";
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

export type SecretGuideClient = "claude" | "cursor" | "codex" | "opencode";

export const SECRET_GUIDE_CLIENTS: Record<
  SecretGuideClient,
  { hookSource: string; installDirectory: string; label: string }
> = {
  claude: {
    hookSource: "claude-code",
    installDirectory: "gram-observability-claude",
    label: "Claude Code",
  },
  cursor: {
    hookSource: "cursor",
    installDirectory: "gram-observability-cursor",
    label: "Cursor",
  },
  codex: {
    hookSource: "codex",
    installDirectory: "gram-observability-codex",
    label: "Codex",
  },
  opencode: {
    hookSource: "opencode",
    installDirectory: ".opencode",
    label: "OpenCode",
  },
};

const SECRET_GUIDE_SYNTHETIC_SECRET = "ghp_R2D2C3POLuk3Skywalker1234567890ab";
const SECRET_GUIDE_PROMPT =
  "Security test with a synthetic, inert dummy secret: use your local shell tool exactly once to run the command below. Do not use the network or contact any service. Submit the command as a tool request exactly as written:\n\nprintf '%s\\n' 'GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab' >/dev/null";

const SECRET_GUIDE_CORRELATION_WINDOW_MS = 60_000;

type ActiveOperation = {
  scope: ProjectGuideOperationScope;
  report: (report: ProjectGuideOperationReport) => void;
  paused: boolean;
};

type TelemetryBaseline = {
  capturedAtMs: number;
  client: SecretGuideClient;
  syntheticSecretRedaction: string;
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

function downloadedArchivePath(filename: string): string {
  return `"$HOME/Downloads"/${shellFilename(filename)}`;
}

function installDetails(
  client: SecretGuideClient,
  filename: string | undefined,
): { command: string; instructions: string } | undefined {
  if (!filename) return undefined;
  const archive = downloadedArchivePath(filename);
  const directoryName = SECRET_GUIDE_CLIENTS[client].installDirectory;
  if (client === "opencode") {
    return {
      command: `mkdir -p ${directoryName} && unzip -oq ${archive} -d ${directoryName}`,
      instructions:
        "Run this from your project directory. OpenCode discovers the extracted plugin in .opencode/ when it starts.",
    };
  }
  const directory = `"$HOME/${directoryName}"`;
  const extract = `mkdir -p ${directory} && unzip -oq ${archive} -d ${directory}`;

  switch (client) {
    case "claude":
      return {
        command: `${extract} && claude --plugin-dir ${directory}`,
        instructions:
          "This launches Claude Code with the extracted plugin active. Keep that session open for the test, then confirm below.",
      };
    case "cursor":
      return {
        command: extract,
        instructions:
          "After extraction, open Cursor Settings → Plugins → Import, select $HOME/gram-observability-cursor, finish the import, and restart Cursor before confirming.",
      };
    case "codex":
      return {
        command: `${extract} && bash "$HOME/${directoryName}/install.sh"`,
        instructions:
          "The bundled installer registers the marketplace, enables the Codex plugin and hook feature flags, and approves the hooks. Restart Codex before confirming.",
      };
  }
}

async function syntheticSecretRedaction(
  organizationId: string,
): Promise<string> {
  const encoder = new TextEncoder();
  const secret = encoder.encode(SECRET_GUIDE_SYNTHETIC_SECRET);
  const saltedSecret = encoder.encode(
    `${organizationId}\0${SECRET_GUIDE_SYNTHETIC_SECRET}`,
  );
  const digest = await globalThis.crypto.subtle.digest("SHA-256", saltedSecret);
  const sha = Array.from(new Uint8Array(digest).slice(0, 4), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  return `<redacted len=${secret.byteLength} sha=${sha}>`;
}

function matchesSyntheticSecret(
  result: RiskResult,
  expectedRedaction: string,
): boolean {
  if (!result.ruleId?.toLowerCase().includes("github")) return false;
  if (result.match !== undefined) {
    return result.match === SECRET_GUIDE_SYNTHETIC_SECRET;
  }
  return result.matchRedacted === expectedRedaction;
}

function newMatchingResults(
  results: RiskResult[] | undefined,
  baseline: TelemetryBaseline,
  policyId: string | undefined,
): RiskResult[] {
  return (
    results
      ?.filter(
        (result) =>
          !baseline.resultIds.has(result.id) &&
          result.createdAt.getTime() > baseline.capturedAtMs &&
          result.policyId === policyId &&
          result.source === "gitleaks" &&
          matchesSyntheticSecret(result, baseline.syntheticSecretRedaction),
      )
      .sort(
        (left, right) => right.createdAt.getTime() - left.createdAt.getTime(),
      ) ?? []
  );
}

function matchingBlockedTrace(
  traces: HookTraceSummary[] | undefined,
  baseline: TelemetryBaseline,
  policyName: string | undefined,
  result: RiskResult,
): HookTraceSummary | undefined {
  if (!policyName) return undefined;
  const expectedPolicyReason = `matched policy ${JSON.stringify(policyName)}`;
  const resultTimeMs = result.createdAt.getTime();
  return traces?.find((trace) => {
    let traceTimeMs: number;
    try {
      traceTimeMs = Number(BigInt(trace.startTimeUnixNano) / 1_000_000n);
    } catch {
      return false;
    }
    return (
      trace.hookStatus === "blocked" &&
      !baseline.traceIds.has(trace.traceId) &&
      traceTimeMs > baseline.capturedAtMs &&
      trace.hookSource === SECRET_GUIDE_CLIENTS[baseline.client].hookSource &&
      trace.blockReason?.includes(expectedPolicyReason) === true &&
      Math.abs(traceTimeMs - resultTimeMs) <= SECRET_GUIDE_CORRELATION_WINDOW_MS
    );
  });
}

function telemetryEvidence(
  traces: HookTraceSummary[] | undefined,
  results: RiskResult[] | undefined,
  baseline: TelemetryBaseline,
  policy: RiskPolicy | undefined,
): RiskResult | undefined {
  return newMatchingResults(results, baseline, policy?.id).find((riskResult) =>
    matchingBlockedTrace(traces, baseline, policy?.name, riskResult),
  );
}

export function useSecretGuideOperations(): {
  client: SecretGuideClient;
  clientSelected: boolean;
  downloadedFilename: string | undefined;
  handleSignal: (
    signal: ProjectGuideOperationSignal,
    report: (report: ProjectGuideOperationReport) => void,
  ) => void;
  installCommand: string | undefined;
  installInstructions: string | undefined;
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
  const organization = useOrganization();
  const { fetch: authFetch } = useFetcher();
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [client, setClientState] = useState<SecretGuideClient>();
  const [downloadedFilename, setDownloadedFilename] = useState<string>();
  const [promptCopied, setPromptCopied] = useState(false);
  const [createdPolicy, setCreatedPolicy] = useState<RiskPolicy>();
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
  const policy = matchingPolicy ?? createdPolicy;
  const policyId = policy?.id;
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
    const capturedAtMs = Date.now();
    baselineRef.current = undefined;
    setBaseline(undefined);
    setBaselineError(false);
    setSuppressTelemetryError(true);
    tracesRequest.listHooksTracesPayload.to = new Date();
    try {
      const [traces, results, expectedRedaction] = await Promise.all([
        tracesQuery.refetch(),
        resultsQuery.refetch(),
        syntheticSecretRedaction(organization.id),
      ]);
      if (traces.isError || results.isError || !traces.data || !results.data) {
        setBaselineError(true);
        return;
      }
      const baselineClient = client ?? "claude";
      const nextBaseline = {
        capturedAtMs,
        client: baselineClient,
        syntheticSecretRedaction: expectedRedaction,
        traceIds: new Set(traces.data.traces.map((trace) => trace.traceId)),
        resultIds: new Set(results.data.results.map((result) => result.id)),
      };
      baselineRef.current = nextBaseline;
      setBaseline(nextBaseline);
      tracesRequest.listHooksTracesPayload.from = new Date(capturedAtMs);
      tracesRequest.listHooksTracesPayload.to = new Date();
    } catch {
      setBaselineError(true);
    } finally {
      setSuppressTelemetryError(false);
    }
  }, [client, organization.id, resultsQuery, tracesQuery, tracesRequest]);

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
    if (matchingPolicy || createdPolicy) {
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
        setCreatedPolicy(policy);
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
    createdPolicy,
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
    if (!client) {
      updateActiveOperation(undefined);
      return;
    }
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
    const riskResult = telemetryEvidence(
      tracesQuery.data?.traces,
      resultsQuery.data?.results,
      currentBaseline,
      policy,
    );
    if (!riskResult) return;
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
    policy,
    resultsQuery.data?.results,
    resultsQuery.isPending,
    suppressTelemetryError,
    telemetryError,
    tracesQuery.data?.traces,
    tracesQuery.isPending,
    updateActiveOperation,
  ]);

  const resolvedClient = client ?? "claude";
  const install = installDetails(resolvedClient, downloadedFilename);

  return {
    client: resolvedClient,
    clientSelected: client !== undefined,
    downloadedFilename,
    handleSignal,
    installCommand: install?.command,
    installInstructions: install?.instructions,
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
