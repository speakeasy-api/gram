import { markProjectGuideStarted } from "@/components/project-guide/projectGuideStores";
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import {
  ProjectGuideObservedEvent,
  ProjectGuideRun,
  type ProjectGuideRunAction,
} from "@/components/project-guide/ProjectGuideRun";
import {
  JOURNEY_STATUS_LABELS,
  PROJECT_GUIDE_COMPLETE,
  PROJECT_GUIDE_FIXTURES,
  PROJECT_GUIDE_JOURNEYS,
  otherProjectGuideJourney,
  type JourneyId,
  type JourneyMeta,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import { useProjectGuideProgress } from "@/components/project-guide/useProjectGuideProgress";
import { RainbowSpinner } from "@/components/ui/Spinner";
import {
  MCP_GUIDE_CLIENTS,
  type McpGuideClient,
  useMcpGuideOperations,
} from "@/components/project-guide/useMcpGuideOperations";
import {
  SECRET_GUIDE_CLIENTS,
  type SecretGuideClient,
  useSecretGuideOperations,
} from "@/components/project-guide/useSecretGuideOperations";
import { firstIncompleteStepIndex } from "@/components/project-guide/journeyStatus";
import {
  getProjectGuideCurrentStep,
  projectGuideMachine,
  type ProjectGuideDisplayState,
  type ProjectGuideEvent,
  type ProjectGuideOperationReport,
  type ProjectGuideOperationSignal,
  type ProjectGuideOutputEntry,
} from "@/components/project-guide/projectGuideMachine";
import { useSlugs } from "@/contexts/Sdk";
import { Icon } from "@/components/ui/Icon";
import type { IconName } from "@/components/ui/Icon/names";
import { cn } from "@/lib/utils";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { useMachine } from "@xstate/react";
import { motion, useReducedMotion } from "motion/react";
import { useCallback, useEffect, useRef } from "react";
import { useSearchParams } from "react-router";

type McpGuideOperations = ReturnType<typeof useMcpGuideOperations>;
type SecretGuideOperations = ReturnType<typeof useSecretGuideOperations>;

const PROJECT_GUIDE_MICRO_STEP_DELAY_MS = 2000;

export function ProjectGuide({
  onOperationSignal,
}: {
  onOperationSignal?: (
    signal: ProjectGuideOperationSignal,
    report: (report: ProjectGuideOperationReport) => void,
  ) => void;
} = {}): JSX.Element {
  const { projectSlug } = useSlugs();
  const { statusByJourney, isPending: progressPending } =
    useProjectGuideProgress();
  const mcpOperations = useMcpGuideOperations();
  const secretOperations = useSecretGuideOperations();
  const operationSignalRef = useRef(onOperationSignal);
  const mcpOperationSignalRef = useRef(mcpOperations.handleSignal);
  const secretOperationSignalRef = useRef(secretOperations.handleSignal);
  const reportRef = useRef<(report: ProjectGuideOperationReport) => void>(
    () => undefined,
  );
  const reportTimersRef = useRef(new Set<number>());
  const nextReportAtRef = useRef(0);
  operationSignalRef.current = onOperationSignal;
  mcpOperationSignalRef.current = mcpOperations.handleSignal;
  secretOperationSignalRef.current = secretOperations.handleSignal;
  const [snapshot, send] = useMachine(projectGuideMachine, {
    input: {
      onSignal: (signal) => {
        if (signal.type === "abort") {
          nextReportAtRef.current = 0;
        }
        if (signal.type === "start") {
          nextReportAtRef.current = Math.max(
            nextReportAtRef.current,
            Date.now() + PROJECT_GUIDE_MICRO_STEP_DELAY_MS,
          );
        }
        mcpOperationSignalRef.current(signal, reportRef.current);
        secretOperationSignalRef.current(signal, reportRef.current);
        operationSignalRef.current?.(signal, reportRef.current);
      },
    },
  });
  const reportOperation = useCallback(
    (report: ProjectGuideOperationReport) => {
      const now = Date.now();
      const dispatchAt = Math.max(now, nextReportAtRef.current);
      nextReportAtRef.current = dispatchAt + PROJECT_GUIDE_MICRO_STEP_DELAY_MS;
      const timer = window.setTimeout(() => {
        reportTimersRef.current.delete(timer);
        send({ type: "ADAPTER_REPORT", report });
      }, dispatchAt - now);
      reportTimersRef.current.add(timer);
    },
    [send],
  );
  reportRef.current = reportOperation;
  useEffect(
    () => () => {
      for (const timer of reportTimersRef.current) {
        window.clearTimeout(timer);
      }
    },
    [],
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const reducedMotion = useReducedMotion();
  const selected = snapshot.context.activePath;
  const displayState = snapshot.value as ProjectGuideDisplayState;
  const selectedJourney = PROJECT_GUIDE_JOURNEYS.find(
    (journey) => journey.id === selected,
  );
  const selectedContentId = selected
    ? projectGuideContentId(selected)
    : undefined;
  const isComplete =
    statusByJourney["third-party-mcp"] === "done" &&
    statusByJourney["secret-block"] === "done";

  const returnToProjectHome = useCallback(() => {
    setSearchParams(searchParams, { replace: true });
  }, [searchParams, setSearchParams]);

  useEffect(() => {
    if (displayState !== "waiting") return;
    const startedAt =
      Date.now() - snapshot.context.elapsedListeningSeconds * 1000;
    const interval = window.setInterval(() => {
      send({
        type: "LISTEN_TICK",
        elapsedSeconds: Math.floor((Date.now() - startedAt) / 1000),
      });
    }, 1000);
    return () => window.clearInterval(interval);
  }, [displayState, send, snapshot.context.elapsedListeningSeconds]);

  useEffect(() => {
    if (displayState === "exited") returnToProjectHome();
  }, [displayState, returnToProjectHome]);

  const openJourney = (journey: JourneyMeta): void => {
    if (projectSlug) markProjectGuideStarted(projectSlug);
    send({
      type: "OPEN",
      path: journey.id,
      resumeStep: firstIncompleteStepIndex(
        statusByJourney[journey.id],
        journey.steps.length,
      ),
    });
  };

  const switchJourney = (journey: JourneyMeta): void => {
    send({
      type: "SWITCH",
      path: journey.id,
      resumeStep: firstIncompleteStepIndex(
        statusByJourney[journey.id],
        journey.steps.length,
      ),
    });
  };

  const currentStep = getProjectGuideCurrentStep(snapshot.context);
  const completedSteps = selected
    ? snapshot.context.completedByPath[selected]
    : [];
  const primaryAction = selectedJourney
    ? primaryActionFor(
        displayState,
        selectedJourney,
        currentStep,
        send,
        mcpOperations,
        secretOperations,
      )
    : undefined;
  if (isComplete) {
    return (
      <GuideCanvas>
        <ProjectGuideComplete
          reducedMotion={reducedMotion}
          onReturnToProjectHome={returnToProjectHome}
        />
      </GuideCanvas>
    );
  }

  return (
    <GuideCanvas>
      <section className="border-border bg-card mx-auto flex min-h-96 w-full max-w-6xl flex-col overflow-hidden border shadow-sm">
        <header className="border-border flex items-baseline gap-3.5 border-b px-6 py-5 pb-3">
          <h2 className="text-display-xs">
            {selectedJourney?.title ?? "Put your agent traffic under control"}
          </h2>
          {selected && (
            <div className="ml-auto flex items-center gap-3">
              <button
                type="button"
                onClick={() => send({ type: "BACK" })}
                aria-controls={selectedContentId}
                aria-expanded="true"
                className="text-eyebrow text-disabled hover:text-foreground"
              >
                ← Back to start
              </button>
            </div>
          )}
        </header>
        <div className="flex min-h-96 flex-col md:flex-row">
          {PROJECT_GUIDE_JOURNEYS.map((journey) => {
            const status = statusByJourney[journey.id];
            const isSelected = selected === journey.id;
            const isSpine = selected !== null && !isSelected;
            return (
              <section
                key={journey.id}
                data-testid={`project-guide-${journey.id}-card`}
                data-state={isSelected ? "open" : isSpine ? "spine" : "closed"}
                className={cn(
                  "min-w-0 overflow-hidden",
                  isSpine ? "bg-background md:w-14 md:flex-none" : "md:flex-1",
                  journey.id === "third-party-mcp" && "border-border border-l",
                )}
              >
                {isSpine ? (
                  <JourneySpine
                    journey={journey}
                    status={status}
                    controlsId={projectGuideContentId(journey.id)}
                    onSelect={() => switchJourney(journey)}
                  />
                ) : isSelected ? (
                  <ProjectGuideRun
                    journey={journey}
                    status={status}
                    regionId={projectGuideContentId(journey.id)}
                    completionBody={
                      journey.id === "third-party-mcp"
                        ? mcpCompletionBody(mcpOperations.serverName)
                        : undefined
                    }
                    displayState={displayState}
                    completedSteps={completedSteps}
                    currentStep={currentStep}
                    currentContent={
                      <ProjectGuideStepContent
                        journey={journey}
                        step={currentStep}
                        displayState={displayState}
                        operationProgress={snapshot.context.operationProgress}
                        error={snapshot.context.error}
                        mcpOperations={mcpOperations}
                        secretOperations={secretOperations}
                        onMcpPromptCopied={() => {
                          mcpOperations.markPromptCopied();
                          if (
                            journey.id === "third-party-mcp" &&
                            currentStep === 2 &&
                            displayState === "checkpoint"
                          ) {
                            send({
                              type: "USER_CHECKPOINT_COMPLETE",
                              result:
                                "Prompt copied · listening for the governed call",
                            });
                          }
                        }}
                        onSecretPromptCopied={() => {
                          secretOperations.markPromptCopied();
                          if (
                            journey.id === "secret-block" &&
                            currentStep === 3 &&
                            displayState === "checkpoint"
                          ) {
                            send({
                              type: "USER_CHECKPOINT_COMPLETE",
                              result:
                                "Prompt copied · listening for the blocked event",
                            });
                          }
                        }}
                        onSelectAgent={(client) => {
                          secretOperations.setClient(client);
                          send({ type: "SELECT_AGENT", client });
                        }}
                      />
                    }
                    output={
                      <ProjectGuideOutput
                        entries={snapshot.context.output}
                        accent={PROJECT_GUIDE_FIXTURES[journey.id].accent}
                        isProcessing={displayState === "running"}
                        error={guideStepError(
                          journey,
                          currentStep,
                          mcpOperations,
                          secretOperations,
                        )}
                      />
                    }
                    eventCard={
                      snapshot.context.observedEvent ? (
                        <ProjectGuideObservedEvent
                          event={snapshot.context.observedEvent}
                          label={PROJECT_GUIDE_FIXTURES[journey.id].event.label}
                        />
                      ) : null
                    }
                    primaryAction={primaryAction}
                    listeningElapsedSeconds={
                      snapshot.context.elapsedListeningSeconds
                    }
                    onRewind={(step) => send({ type: "REWIND", step })}
                    onSwitchJourney={() => {
                      const otherId = otherProjectGuideJourney(journey.id);
                      const otherJourney = PROJECT_GUIDE_JOURNEYS.find(
                        (candidate) => candidate.id === otherId,
                      );
                      if (otherJourney) switchJourney(otherJourney);
                    }}
                  />
                ) : (
                  <JourneyChoice
                    journey={journey}
                    status={status}
                    statusPending={progressPending}
                    controlsId={projectGuideContentId(journey.id)}
                    onSelect={() => openJourney(journey)}
                  />
                )}
              </section>
            );
          })}
        </div>
      </section>
    </GuideCanvas>
  );
}

function primaryActionFor(
  displayState: ProjectGuideDisplayState,
  journey: JourneyMeta,
  currentStep: number,
  send: (event: ProjectGuideEvent) => void,
  mcpOperations: McpGuideOperations,
  secretOperations: SecretGuideOperations,
): ProjectGuideRunAction | null {
  switch (displayState) {
    case "ready":
      return {
        label: "Start the journey",
        disabled:
          (journey.id === "third-party-mcp" &&
            currentStep === 0 &&
            (!mcpOperations.selectedServer ||
              mcpOperations.projectStatePending ||
              mcpOperations.projectStateError)) ||
          (journey.id === "secret-block" &&
            ((currentStep === 0 &&
              (secretOperations.policyPending ||
                secretOperations.policyError)) ||
              (currentStep === 1 && !secretOperations.clientSelected))),
        onClick: () => send({ type: "START" }),
      };
    case "running":
      return {
        label: "Pause the journey",
        onClick: () => send({ type: "PAUSE" }),
      };
    case "checkpoint":
      if (journey.id === "third-party-mcp" && currentStep === 1) {
        return {
          label: "I've connected it",
          disabled:
            !mcpOperations.connectionPrompts ||
            !mcpOperations.connectionPromptCopied,
          onClick: () =>
            send({
              type: "USER_CHECKPOINT_COMPLETE",
              result: "Client connected to the governed endpoint",
            }),
        };
      }
      if (journey.id === "third-party-mcp" && currentStep === 2) {
        return null;
      }
      if (journey.id === "secret-block" && currentStep === 2) {
        return {
          label: "I've installed and restarted it",
          disabled: !secretOperations.installCommand,
          onClick: () =>
            send({
              type: "USER_CHECKPOINT_COMPLETE",
              result: "Observability plugin installed and agent restarted",
            }),
        };
      }
      if (journey.id === "secret-block" && currentStep === 1) {
        return {
          label: "Start the journey",
          disabled: !secretOperations.clientSelected,
          onClick: () => {
            send({ type: "SELECT_AGENT", client: secretOperations.client });
            send({ type: "START" });
          },
        };
      }
      if (journey.id === "secret-block" && currentStep === 3) {
        return null;
      }
      return {
        label: "I've completed this step",
        onClick: () =>
          send({
            type: "USER_CHECKPOINT_COMPLETE",
            result: `Completed · ${journey.steps[currentStep] ?? "checkpoint"}`,
          }),
      };
    case "waiting":
      return {
        label: "Pause listening",
        onClick: () => send({ type: "PAUSE" }),
      };
    case "paused":
      return { label: "Resume", onClick: () => send({ type: "RESUME" }) };
    case "error":
      return { label: "Retry", onClick: () => send({ type: "RETRY" }) };
    case "complete":
      if (journey.id === "third-party-mcp") {
        return {
          label: journey.completion.primaryAction,
          href: mcpOperations.toolLogsHref,
        };
      }
      return {
        label: journey.completion.primaryAction,
        href: secretOperations.riskEventsHref,
      };
    case "opening":
    case "exited":
      return { label: "Start the journey", disabled: true };
  }
}

function ProjectGuideStepContent({
  journey,
  step,
  displayState,
  operationProgress,
  error,
  mcpOperations,
  secretOperations,
  onMcpPromptCopied,
  onSecretPromptCopied,
  onSelectAgent,
}: {
  journey: JourneyMeta;
  step: number;
  displayState: ProjectGuideDisplayState;
  operationProgress: number | null;
  error: string | null;
  mcpOperations: McpGuideOperations;
  secretOperations: SecretGuideOperations;
  onMcpPromptCopied: () => void;
  onSecretPromptCopied: () => void;
  onSelectAgent: (client: SecretGuideClient) => void;
}): JSX.Element {
  if (journey.id === "third-party-mcp") {
    return (
      <ProjectGuideMcpStepContent
        journey={journey}
        step={step}
        operations={mcpOperations}
        onMcpPromptCopied={onMcpPromptCopied}
      />
    );
  }

  return (
    <div className="grid min-w-0 gap-3 pt-3">
      <p className="text-muted-foreground max-w-md text-body-sm">
        {journey.stepBlurbs[step]}
      </p>
      <SecretStepBody
        step={step}
        displayState={displayState}
        operationProgress={operationProgress}
        error={error}
        operations={secretOperations}
        onSecretPromptCopied={onSecretPromptCopied}
        onSelectAgent={onSelectAgent}
      />
    </div>
  );
}

const SECRET_POLICY_PHASES = [
  "Detect · enable the Secrets category",
  "Scope · user prompts, the recommended surface",
  "Action · deny the request",
] as const;

const SECRET_PLUGIN_PHASES = [
  "Build the plugin for this project",
  "Sign the bundle",
] as const;

type SecretPolicyPhaseStatus =
  | "not run"
  | "queued"
  | "running"
  | "ok"
  | "failed";

function secretPolicyPhaseStatuses(
  displayState: ProjectGuideDisplayState,
  operationProgress: number | null,
  hasError: boolean,
): SecretPolicyPhaseStatus[] {
  if (hasError) return ["failed", "queued", "queued"];
  if (displayState !== "running" && displayState !== "paused") {
    return ["not run", "not run", "not run"];
  }

  const progress = operationProgress ?? 0;
  const currentPhase = Math.min(
    SECRET_POLICY_PHASES.length - 1,
    Math.floor(progress * SECRET_POLICY_PHASES.length),
  );
  return SECRET_POLICY_PHASES.map((_, index) => {
    if (progress >= 1 || index < currentPhase) return "ok";
    if (index === currentPhase) return "running";
    return "queued";
  });
}

function secretPluginPhaseStatuses(
  displayState: ProjectGuideDisplayState,
  operationProgress: number | null,
  hasError: boolean,
): SecretPolicyPhaseStatus[] {
  if (hasError) return ["failed", "queued"];
  if (displayState !== "running" && displayState !== "paused") {
    return ["not run", "not run"];
  }

  const progress = operationProgress ?? 0;
  if (progress >= 1) return ["ok", "ok"];
  return progress >= 0.75 ? ["ok", "running"] : ["running", "queued"];
}

function SecretPolicyPhases({
  displayState,
  operationProgress,
  hasError,
}: {
  displayState: ProjectGuideDisplayState;
  operationProgress: number | null;
  hasError: boolean;
}): JSX.Element {
  const statuses = secretPolicyPhaseStatuses(
    displayState,
    operationProgress,
    hasError,
  );

  return (
    <SecretPhaseChecklist
      title="What the wizard does"
      labels={SECRET_POLICY_PHASES}
      statuses={statuses}
    />
  );
}

function SecretPhaseChecklist({
  title,
  labels,
  statuses,
}: {
  title: string;
  labels: readonly string[];
  statuses: readonly SecretPolicyPhaseStatus[];
}): JSX.Element {
  return (
    <div className="grid gap-2 pt-2">
      <span className="text-eyebrow text-disabled">{title}</span>
      <div>
        {labels.map((label, index) => {
          const status = statuses[index] ?? "not run";
          const statusClassName = {
            "not run": "border-neutral-default text-disabled",
            queued: "border-neutral-default text-disabled",
            running: "animate-pulse border-foreground text-foreground",
            ok: "border-success-default text-default-success",
            failed: "border-destructive-default text-default-destructive",
          }[status];

          return (
            <div
              key={label}
              className="border-border flex items-center gap-2 border-t py-3 last:border-b"
            >
              <span
                aria-hidden="true"
                className={cn("size-3 shrink-0 border", statusClassName)}
              />
              <span className="text-muted-foreground text-sm">{label}</span>
              <span className="border-border flex-1 border-t" />
              <span className={cn("text-eyebrow", statusClassName)}>
                {status}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function SecretStepBody({
  step,
  displayState,
  operationProgress,
  error,
  operations,
  onSecretPromptCopied,
  onSelectAgent,
}: {
  step: number;
  displayState: ProjectGuideDisplayState;
  operationProgress: number | null;
  error: string | null;
  operations: SecretGuideOperations;
  onSecretPromptCopied: () => void;
  onSelectAgent: (client: SecretGuideClient) => void;
}): JSX.Element | null {
  switch (step) {
    case 0:
      return (
        <div className="grid gap-3">
          <SecretPolicyPhases
            displayState={displayState}
            operationProgress={operationProgress}
            hasError={Boolean(error) || operations.policyError}
          />
          {operations.policyError && (
            <button
              type="button"
              onClick={operations.retryPolicy}
              className="border-border w-fit border px-3 py-2 font-mono text-xs uppercase"
            >
              Retry policy check
            </button>
          )}
        </div>
      );
    case 1:
      return (
        <div className="grid gap-3">
          <SecretAgentPicker
            client={operations.clientSelected ? operations.client : undefined}
            disabled={
              (displayState !== "ready" && displayState !== "checkpoint") ||
              Boolean(operations.downloadedFilename)
            }
            onSelect={onSelectAgent}
          />
          <SecretPluginPhases
            displayState={displayState}
            operationProgress={operationProgress}
            hasError={Boolean(error)}
          />
        </div>
      );
    case 2:
      if (!operations.installCommand) {
        return null;
      }
      return (
        <div className="min-w-0">
          <CodeSnippet
            code={operations.installCommand}
            language="bash"
            copyable
            wordWrap
            className="min-w-0 max-w-full"
            snippetClassName="min-w-0 w-full max-w-full"
          />
        </div>
      );
    case 3:
      return (
        <div className="grid min-w-0 gap-2">
          <div className="grid gap-2">
            <span className="font-mono text-xs text-muted-foreground">
              Copy into {SECRET_GUIDE_CLIENTS[operations.client].label}
            </span>
            <div className="min-w-0">
              <CodeSnippet
                code={operations.prompt}
                language="text"
                copyable
                wordWrap
                className="min-w-0 w-full max-w-full"
                snippetClassName="min-w-0 w-full max-w-full"
                onSelectOrCopy={onSecretPromptCopied}
              />
            </div>
          </div>
        </div>
      );
    case 4:
      return null;
    default:
      return null;
  }
}

function SecretAgentPicker({
  client,
  disabled,
  onSelect,
}: {
  client: SecretGuideClient | undefined;
  disabled: boolean;
  onSelect: (client: SecretGuideClient) => void;
}): JSX.Element {
  return (
    <fieldset className="grid gap-2 pt-2">
      <legend className="text-eyebrow text-disabled">Choose your agent</legend>
      <div className="flex min-w-0 gap-2 overflow-x-auto pb-1">
        {(Object.keys(SECRET_GUIDE_CLIENTS) as SecretGuideClient[]).map(
          (option) => {
            const selected = client !== undefined && option === client;
            return (
              <button
                key={option}
                type="button"
                aria-pressed={selected}
                disabled={disabled}
                onClick={() => onSelect(option)}
                className={cn(
                  "shrink-0 border px-3 py-2 text-left text-sm whitespace-nowrap transition-colors",
                  selected
                    ? "border-foreground bg-foreground text-background"
                    : "border-border text-muted-foreground hover:border-foreground hover:text-foreground",
                  disabled && !selected && "cursor-default opacity-50",
                )}
              >
                {SECRET_GUIDE_CLIENTS[option].label}
              </button>
            );
          },
        )}
      </div>
    </fieldset>
  );
}

function SecretPluginPhases({
  displayState,
  operationProgress,
  hasError,
}: {
  displayState: ProjectGuideDisplayState;
  operationProgress: number | null;
  hasError: boolean;
}): JSX.Element {
  const statuses = secretPluginPhaseStatuses(
    displayState,
    operationProgress,
    hasError,
  );

  return (
    <SecretPhaseChecklist
      title="What you get"
      labels={SECRET_PLUGIN_PHASES}
      statuses={statuses}
    />
  );
}

function ProjectGuideMcpStepContent({
  journey,
  step,
  operations,
  onMcpPromptCopied,
}: {
  journey: JourneyMeta;
  step: number;
  operations: McpGuideOperations;
  onMcpPromptCopied: () => void;
}): JSX.Element {
  return (
    <div className="grid gap-3 pt-3">
      <p className="text-muted-foreground max-w-md text-body-sm">
        {journey.stepBlurbs[step]}
      </p>
      <McpStepBody
        step={step}
        operations={operations}
        onMcpPromptCopied={onMcpPromptCopied}
      />
    </div>
  );
}

function McpStepBody({
  step,
  operations,
  onMcpPromptCopied,
}: {
  step: number;
  operations: McpGuideOperations;
  onMcpPromptCopied: () => void;
}): JSX.Element | null {
  switch (step) {
    case 0:
      return <McpCatalogSelection operations={operations} />;
    case 1:
      return <McpClientConnection operations={operations} />;
    case 2:
      return (
        <McpSafePrompt
          operations={operations}
          onMcpPromptCopied={onMcpPromptCopied}
        />
      );
    case 3:
      return null;
    default:
      return null;
  }
}

function McpCatalogSelection({
  operations,
}: {
  operations: McpGuideOperations;
}): JSX.Element {
  if (operations.catalogPending) {
    return (
      <span className="text-eyebrow text-muted-foreground">
        Loading automatic servers
      </span>
    );
  }
  if (operations.catalogError || !operations.catalogServers) {
    return (
      <button
        type="button"
        onClick={operations.retryCatalog}
        className="border-border w-fit border px-3 py-2 font-mono text-xs uppercase"
      >
        Retry catalog
      </button>
    );
  }
  if (operations.catalogServers.length === 0) {
    return (
      <div className="grid gap-2">
        <p className="text-muted-foreground text-sm">
          No curated hosted servers are available right now.
        </p>
        <button
          type="button"
          onClick={operations.retryCatalog}
          className="border-border w-fit border px-3 py-2 font-mono text-xs uppercase"
        >
          Retry catalog
        </button>
      </div>
    );
  }

  return (
    <div className="grid gap-2 sm:grid-cols-3">
      {operations.catalogServers.map((server) => {
        const name = server.title ?? server.registrySpecifier;
        const selected = operations.selectedServer === server;
        return (
          <button
            key={server.registrySpecifier}
            type="button"
            aria-pressed={selected}
            onClick={() => operations.selectServer(server)}
            className={cn(
              "border-border flex items-center gap-2 border px-3 py-2 text-left",
              selected && "border-foreground",
            )}
          >
            <span aria-hidden="true" className="bg-foreground size-1.5" />
            <span className="text-sm">{name}</span>
            <span className="text-muted-foreground ml-auto font-mono text-xs">
              {server.toolCount} tools
            </span>
          </button>
        );
      })}
    </div>
  );
}

function mcpClientLabel(client: McpGuideClient): string {
  switch (client) {
    case "claude":
      return "Claude";
    case "cursor":
      return "Cursor";
    case "codex":
      return "Codex";
  }
}

function McpClientConnection({
  operations,
}: {
  operations: McpGuideOperations;
}): JSX.Element | null {
  if (!operations.endpointUrl || !operations.connectionPrompts) {
    return null;
  }

  return (
    <div className="grid min-w-0 max-w-full gap-3 overflow-hidden">
      <Tabs
        value={operations.client}
        onValueChange={(value) => operations.setClient(value as McpGuideClient)}
        className="min-w-0 max-w-full"
      >
        <PageTabsList aria-label="MCP client">
          {MCP_GUIDE_CLIENTS.map((client) => (
            <PageTabsTrigger key={client} value={client}>
              {mcpClientLabel(client)}
            </PageTabsTrigger>
          ))}
        </PageTabsList>
        {MCP_GUIDE_CLIENTS.map((client) => (
          <TabsContent
            key={client}
            value={client}
            className="min-w-0 max-w-full overflow-hidden"
          >
            <CodeSnippet
              code={operations.connectionPrompts![client]}
              language="text"
              copyable
              wordWrap
              className="min-w-0 w-full max-w-full"
              snippetClassName="min-w-0 w-full max-w-full"
              onSelectOrCopy={operations.markConnectionPromptCopied}
            />
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}

function McpSafePrompt({
  operations,
  onMcpPromptCopied,
}: {
  operations: McpGuideOperations;
  onMcpPromptCopied: () => void;
}): JSX.Element | null {
  if (!operations.prompt) {
    return null;
  }

  return (
    <div className="grid min-w-0 max-w-full gap-2 overflow-hidden">
      <CodeSnippet
        code={operations.prompt}
        language="text"
        copyable
        wordWrap
        className="min-w-0 w-full max-w-full"
        snippetClassName="min-w-0 w-full max-w-full"
        onSelectOrCopy={onMcpPromptCopied}
      />
    </div>
  );
}

function mcpCompletionBody(name: string | undefined): string {
  return `Your client now reaches ${name ?? "the selected server"} through an endpoint you own. Tool lists are filtered to what each caller may use, every call lands in tool logs, and the vendor's server never changed. Remove the server and the path closes.`;
}

function ProjectGuideOutput({
  entries,
  accent,
  isProcessing,
  error,
}: {
  entries: ProjectGuideOutputEntry[];
  accent: string;
  isProcessing: boolean;
  error?: string | null;
}): JSX.Element {
  if (entries.length === 0 && !error) {
    return <span>Nothing has run for this step yet</span>;
  }

  return (
    <ol className="grid gap-1.5">
      {entries.map((entry, index) => (
        <ProjectGuideOutputRow
          key={entry.id}
          accent={accent}
          kind={entry.kind}
          message={entry.message}
          working={isProcessing && !error && index === entries.length - 1}
        />
      ))}
      {error && (
        <ProjectGuideOutputRow
          accent={accent}
          kind="error"
          message={error}
          role="alert"
        />
      )}
    </ol>
  );
}

type ProjectGuideOutputKind = ProjectGuideOutputEntry["kind"] | "error";

const PROJECT_GUIDE_OUTPUT_ENTRY_STYLES: Record<
  ProjectGuideOutputKind,
  { icon: IconName; iconClass: string; message: string; useAccent?: boolean }
> = {
  start: {
    icon: "play",
    iconClass: "text-disabled",
    message: "text-foreground",
  },
  note: {
    icon: "info",
    iconClass: "text-muted-foreground",
    message: "text-muted-foreground",
  },
  next: {
    icon: "arrow-right",
    iconClass: "font-medium",
    message: "font-medium",
    useAccent: true,
  },
  result: {
    icon: "check",
    iconClass: "text-default-success",
    message: "text-default-success font-medium",
  },
  error: {
    icon: "circle-alert",
    iconClass: "text-default-destructive",
    message: "text-default-destructive",
  },
};

function ProjectGuideOutputRow({
  accent,
  kind,
  message,
  role,
  working = false,
}: {
  accent: string;
  kind: ProjectGuideOutputKind;
  message: string;
  role?: "alert";
  working?: boolean;
}): JSX.Element {
  const styles = PROJECT_GUIDE_OUTPUT_ENTRY_STYLES[kind];
  const accentStyle = styles.useAccent ? { color: accent } : undefined;

  return (
    <li role={role} className="flex items-start gap-4">
      <span
        className={cn(
          "flex size-4 shrink-0 items-center justify-center",
          styles.iconClass,
        )}
        style={accentStyle}
      >
        {working ? (
          <RainbowSpinner className="size-3.5" />
        ) : (
          <Icon name={styles.icon} className="size-3.5" aria-hidden="true" />
        )}
        <span className="sr-only">{kind}</span>
      </span>
      <span className={cn("min-w-0", styles.message)} style={accentStyle}>
        {message}
      </span>
    </li>
  );
}

function guideStepError(
  journey: JourneyMeta,
  step: number,
  mcpOperations: McpGuideOperations,
  secretOperations: SecretGuideOperations,
): string | null {
  if (journey.id === "third-party-mcp") {
    if (step === 0 && mcpOperations.catalogError) {
      return "Could not load the automatic catalog servers.";
    }
    if (
      step === 1 &&
      (!mcpOperations.endpointUrl || !mcpOperations.connectionPrompts)
    ) {
      return "The governed endpoint is not ready yet.";
    }
    if (step === 2 && !mcpOperations.prompt) {
      return "The safe prompt is unavailable until the server is ready.";
    }
    return null;
  }

  if (step === 0 && secretOperations.policyError) {
    return "Could not read this project's risk policies.";
  }
  if (step === 2 && !secretOperations.installCommand) {
    return "The observability ZIP is not ready. Retry the download step.";
  }
  return null;
}

function projectGuideContentId(journeyId: JourneyId): string {
  return `project-guide-${journeyId}-content`;
}

function GuideCanvas({ children }: { children: React.ReactNode }): JSX.Element {
  return (
    <div
      className={cn(
        BRAND_MESH_SURFACE_CLASS,
        "relative flex min-h-dvh w-full p-4 sm:p-8",
      )}
    >
      <BrandMeshLayers />
      <div className="relative z-10 flex w-full items-center justify-center">
        {children}
      </div>
    </div>
  );
}

function JourneyChoice({
  journey,
  status,
  statusPending,
  controlsId,
  onSelect,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
  statusPending: boolean;
  controlsId: string;
  onSelect: () => void;
}): JSX.Element {
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const isComplete = status === "done";
  const isInProgress = status === "in-progress";
  const isUnavailable = status === "unreadable";
  const statusLabel = statusPending ? "Loading" : JOURNEY_STATUS_LABELS[status];
  const progressLabel = isUnavailable
    ? "Progress unavailable"
    : isComplete
      ? "Complete"
      : isInProgress
        ? `1 of ${journey.steps.length} done`
        : "Not started";

  return (
    <button
      type="button"
      onClick={onSelect}
      aria-label={journey.title}
      aria-controls={controlsId}
      aria-expanded="false"
      className="flex h-full w-full flex-col text-left"
    >
      <JourneyGraphic journey={journey} status={status} />
      <span className="border-border bg-background flex flex-col gap-2 border-t p-6 transition-all hover:bg-card hover:shadow-inner">
        <span className="flex items-center gap-2.5">
          {journey.steps.map((step, index) => (
            <span
              key={step}
              aria-hidden="true"
              className={cn(
                "h-1 w-4",
                !(isComplete || (isInProgress && index === 0)) &&
                  "bg-neutral-softest",
              )}
              style={
                isComplete || (isInProgress && index === 0)
                  ? { backgroundColor: fixture.accent }
                  : undefined
              }
            />
          ))}
          <span className="text-eyebrow text-disabled">{progressLabel}</span>
        </span>
        <span className="text-xl leading-tight">{journey.title}</span>
        <span className="text-muted-foreground text-sm leading-relaxed">
          {journey.win}
        </span>
        <span className="flex items-center gap-3 pt-1">
          <span className="font-mono text-sm">
            {isComplete
              ? "Review"
              : isInProgress
                ? "Resume the run"
                : "Open the journey"}
          </span>
          <span
            className="h-px flex-1"
            style={{ backgroundColor: fixture.accent }}
          />
          <span className="text-disabled font-mono text-xs">
            {journey.steps.length} steps · ~4 min
          </span>
          <span className="font-mono text-sm" style={{ color: fixture.accent }}>
            →
          </span>
        </span>
        <span className="sr-only">{statusLabel}</span>
      </span>
    </button>
  );
}

function JourneyGraphic({
  journey,
  status,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
}): JSX.Element {
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const reducedMotion = useReducedMotion();
  const isMcp = journey.id === "third-party-mcp";
  const plates = isMcp
    ? [
        {
          zone: "Your client",
          name: "claude code · cursor · codex",
          on: "connected",
          off: "not connected",
        },
        {
          zone: "Your endpoint",
          name: "tool access · tool logs",
          nameOff: "no endpoint yet",
          on: "verified",
          off: "not installed",
        },
        {
          zone: "Upstream",
          name: "linear · vendor server",
          on: "27 tools",
          off: "not picked",
        },
      ]
    : [
        {
          zone: "Your agent",
          name: "claude code + observability plugin",
          nameOff: "claude code · cursor",
          on: "streaming",
          off: "no plugin",
        },
        {
          zone: "Secrets policy",
          name: "deny on match",
          nameOff: "no policy yet",
          on: "enforcing",
          off: "off",
        },
        {
          zone: "Model provider",
          name: "anthropic · openai",
          on: "unsafe prompt not received",
          off: "unproven",
        },
      ];
  const active = status !== "not-started" && status !== "unreadable";
  const animated = !reducedMotion && status === "not-started";

  return (
    <span
      data-testid={`project-guide-graphic-${journey.id}`}
      data-animated={animated}
      className="flex min-h-96 flex-col items-center justify-center px-16 pt-10 pb-8"
    >
      {plates.map((plate, index) => (
        <span key={plate.zone} className="flex w-full flex-col items-center">
          <JourneyGraphicPlate
            key={`${plate.zone}-${animated ? "animated" : "static"}`}
            accent={fixture.accent}
            active={active}
            animated={animated}
            index={index}
            plate={plate}
          />
          {index < 2 && (
            <JourneyGraphicPipe
              key={`${plate.zone}-pipe-${animated ? "animated" : "static"}`}
              accent={fixture.accent}
              active={active}
              animated={animated}
              label={
                isMcp
                  ? index === 0
                    ? "requests"
                    : "governed hop"
                  : index === 0
                    ? "every prompt"
                    : "denied here"
              }
            />
          )}
        </span>
      ))}
    </span>
  );
}

type JourneyGraphicPlateData = {
  zone: string;
  name: string;
  nameOff?: string;
  on: string;
  off: string;
};

const graphicLiveOpacity = [0, 0, 1, 1, 0];
const graphicOffOpacity = [1, 1, 0, 0, 1];
const graphicLoopTimes = [0, 0.46, 0.52, 0.94, 0.99];

function JourneyGraphicPlate({
  accent,
  active,
  animated,
  index,
  plate,
}: {
  accent: string;
  active: boolean;
  animated: boolean;
  index: number;
  plate: JourneyGraphicPlateData;
}): JSX.Element {
  const isCenter = index === 1;
  const offName = plate.nameOff ?? plate.name;
  const liveOpacity = animated
    ? { opacity: graphicLiveOpacity }
    : { opacity: active ? 1 : 0 };
  const offOpacity = animated
    ? { opacity: graphicOffOpacity }
    : { opacity: active ? 0 : 1 };
  const liveTransition = animated
    ? {
        duration: 9,
        ease: "linear" as const,
        repeat: Infinity,
        times: graphicLoopTimes,
      }
    : { duration: 0 };
  const offTransition = animated
    ? {
        duration: 9,
        ease: "linear" as const,
        repeat: Infinity,
        times: [0, 0.4, 0.46, 0.96, 1],
      }
    : { duration: 0 };

  return (
    <motion.span
      animate={animated && isCenter ? { scale: [1, 1.018, 1] } : { scale: 1 }}
      transition={
        animated
          ? { duration: 9, ease: "linear", repeat: Infinity }
          : { duration: 0 }
      }
      className={cn(
        "border-border bg-background relative flex w-4/5 flex-col gap-2 border p-4",
        isCenter && "w-full bg-muted p-5",
      )}
    >
      {isCenter && (
        <motion.span
          aria-hidden="true"
          animate={
            animated
              ? {
                  opacity: [0.35, 0.35, 1, 1, 0.35],
                  boxShadow: [
                    `inset 0 0 0 1px ${accent}`,
                    `inset 0 0 0 1px ${accent}`,
                    `inset 0 0 0 1px ${accent}, 0 0 18px ${accent}66`,
                    `inset 0 0 0 1px ${accent}, 0 0 18px ${accent}66`,
                    `inset 0 0 0 1px ${accent}`,
                  ],
                }
              : { opacity: active ? 1 : 0.35, boxShadow: "none" }
          }
          transition={
            animated
              ? {
                  duration: 2.6,
                  ease: "easeInOut",
                  repeat: Infinity,
                }
              : { duration: 0 }
          }
          className="pointer-events-none absolute inset-0"
        />
      )}
      {isCenter && (
        <span
          aria-hidden="true"
          className="absolute inset-y-0 left-0 w-1 opacity-40"
          style={{
            background:
              "linear-gradient(180deg,#320F1E,#FA873C,#5A8250,#00143C,#9BC3FF)",
          }}
        />
      )}
      <span className="flex items-center gap-3">
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="text-eyebrow text-muted-foreground">
            {plate.zone}
          </span>
          <span
            className={cn(
              "relative min-w-0 truncate text-sm",
              isCenter && "text-xl",
            )}
          >
            <motion.span
              animate={offOpacity}
              transition={offTransition}
              className="block truncate"
            >
              {offName}
            </motion.span>
            <motion.span
              animate={liveOpacity}
              transition={liveTransition}
              className="absolute inset-x-0 top-0 block truncate"
            >
              {plate.name}
            </motion.span>
          </span>
        </span>
        <span className="text-eyebrow relative min-w-24 text-right">
          <motion.span
            animate={offOpacity}
            transition={offTransition}
            className="text-disabled block whitespace-nowrap"
          >
            {plate.off}
          </motion.span>
          <motion.span
            animate={liveOpacity}
            transition={liveTransition}
            className="absolute inset-x-0 top-0 block whitespace-nowrap"
            style={{ color: accent }}
          >
            {plate.on}
          </motion.span>
        </span>
      </span>
      {isCenter && (
        <motion.span
          animate={liveOpacity}
          transition={liveTransition}
          className="flex gap-1 pl-2"
        >
          {Array.from({ length: 12 }, (_, tick) => (
            <motion.span
              key={tick}
              aria-hidden="true"
              animate={
                animated
                  ? { opacity: [0.16, 1, 0.16] }
                  : { opacity: active ? 1 : 0.16 }
              }
              transition={
                animated
                  ? {
                      duration: 2.4,
                      ease: "linear",
                      repeat: Infinity,
                      delay: -2.4 + tick * 0.2,
                    }
                  : { duration: 0 }
              }
              className="h-1 flex-1"
              style={{ backgroundColor: accent }}
            />
          ))}
        </motion.span>
      )}
    </motion.span>
  );
}

function JourneyGraphicPipe({
  accent,
  active,
  animated,
  label,
}: {
  accent: string;
  active: boolean;
  animated: boolean;
  label: string;
}): JSX.Element {
  const offOpacity = animated ? [1, 1, 0, 0, 1] : active ? 0 : 1;
  const liveOpacity = animated ? [0, 0, 1, 1, 0] : active ? 1 : 0;

  return (
    <span className="relative flex h-9 w-3 justify-center">
      <motion.span
        animate={
          animated ? { opacity: [0, 0, 1, 1, 0] } : { opacity: active ? 0 : 1 }
        }
        transition={
          animated
            ? {
                duration: 9,
                ease: "linear",
                repeat: Infinity,
                times: graphicLoopTimes,
              }
            : { duration: 0 }
        }
        className="border-neutral-default absolute inset-0 border-x border-dashed"
      />
      <motion.span
        animate={{ opacity: offOpacity }}
        transition={
          animated
            ? {
                duration: 9,
                ease: "linear",
                repeat: Infinity,
                times: [0, 0.3, 0.36, 0.96, 1],
              }
            : { duration: 0 }
        }
        className="bg-foreground/90 absolute inset-0 overflow-hidden"
      >
        <GraphicPipeStripes animated={animated} />
      </motion.span>
      <motion.span
        animate={{ opacity: liveOpacity }}
        transition={
          animated
            ? {
                duration: 9,
                ease: "linear",
                repeat: Infinity,
                times: graphicLoopTimes,
              }
            : { duration: 0 }
        }
        className="absolute inset-0 overflow-hidden"
        style={{ backgroundColor: `${accent}24` }}
      >
        <GraphicPipeStripes animated={animated} />
      </motion.span>
      <span className="text-eyebrow text-disabled absolute top-1/2 left-5 -translate-y-1/2 whitespace-nowrap">
        {label}
      </span>
    </span>
  );
}

function GraphicPipeStripes({ animated }: { animated: boolean }): JSX.Element {
  return (
    <motion.span
      animate={animated ? { y: [0, 14] } : { y: 0 }}
      transition={
        animated
          ? { duration: 0.5, ease: "linear", repeat: Infinity }
          : { duration: 0 }
      }
      className="absolute inset-x-0 -top-3 -bottom-3 bg-[repeating-linear-gradient(180deg,rgba(255,255,255,.7)_0_4px,rgba(255,255,255,0)_4px_14px)]"
    />
  );
}

function JourneySpine({
  journey,
  status,
  controlsId,
  onSelect,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
  controlsId: string;
  onSelect: () => void;
}): JSX.Element {
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const meta =
    status === "done"
      ? "complete"
      : status === "in-progress"
        ? `1 / ${journey.steps.length}`
        : fixture.meta;
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-label={`Switch to ${journey.title}`}
      aria-controls={controlsId}
      aria-expanded="false"
      className="flex h-full w-full flex-col items-center gap-3.5 py-5 hover:bg-card/60"
    >
      <span
        aria-hidden="true"
        className="size-2"
        style={{ backgroundColor: fixture.accent }}
      />
      <span className="text-eyebrow text-muted-foreground flex-1 [writing-mode:vertical-rl]">
        Journey · {meta}
      </span>
      <span aria-hidden="true" className="text-disabled font-mono text-xs">
        ↔
      </span>
    </button>
  );
}

function ProjectGuideComplete({
  reducedMotion,
  onReturnToProjectHome,
}: {
  reducedMotion: boolean | null;
  onReturnToProjectHome: () => void;
}): JSX.Element {
  return (
    <motion.section
      data-testid="project-guide-complete"
      initial={reducedMotion ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={
        reducedMotion
          ? { duration: 0 }
          : { duration: 0.4, ease: [0.2, 0.7, 0.3, 1] }
      }
      className="border-border bg-card grid w-full max-w-6xl gap-4 border p-6"
    >
      <span className="text-eyebrow text-primary">
        {PROJECT_GUIDE_COMPLETE.eyebrow}
      </span>
      <h2 className="text-display-xs max-w-48">
        {PROJECT_GUIDE_COMPLETE.heading}
      </h2>
      <p className="text-muted-foreground max-w-md text-body-sm">
        {PROJECT_GUIDE_COMPLETE.body}
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={onReturnToProjectHome}
          className="bg-foreground text-background px-4 py-2 font-mono text-xs uppercase"
        >
          {PROJECT_GUIDE_COMPLETE.primaryAction}
        </button>
      </div>
    </motion.section>
  );
}
