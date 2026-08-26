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
  PROJECT_GUIDE_MICRO_STEP_DELAY_MS,
  type ProjectGuideDisplayState,
  type ProjectGuideEvent,
  type ProjectGuideOperationReport,
  type ProjectGuideOutputEntry,
} from "@/components/project-guide/projectGuideMachine";
import { useSlugs } from "@/contexts/Sdk";
import { useHideInsightsDock } from "@/components/insights-context";
import { useRoutes } from "@/routes";
import { Icon } from "@/components/ui/Icon";
import type { IconName } from "@/components/ui/Icon/names";
import { cn } from "@/lib/utils";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { Button } from "@/components/ui/Button";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { useMachine } from "@xstate/react";
import { motion, useReducedMotion } from "motion/react";
import { useCallback, useEffect, useRef } from "react";
import { useNavigate } from "react-router";

type McpGuideOperations = ReturnType<typeof useMcpGuideOperations>;
type SecretGuideOperations = ReturnType<typeof useSecretGuideOperations>;

export function ProjectGuide(): JSX.Element {
  const { orgSlug, projectSlug } = useSlugs();

  return (
    <ProjectGuideContent
      key={`${orgSlug ?? ""}:${projectSlug ?? ""}`}
      orgSlug={orgSlug}
      projectSlug={projectSlug}
    />
  );
}

function ProjectGuideContent({
  orgSlug,
  projectSlug,
}: {
  orgSlug: string | undefined;
  projectSlug: string | undefined;
}): JSX.Element {
  useHideInsightsDock();
  const routes = useRoutes();
  const navigate = useNavigate();
  const { statusByJourney, isPending: progressPending } =
    useProjectGuideProgress();
  const mcpOperations = useMcpGuideOperations();
  const secretOperations = useSecretGuideOperations();
  const mcpOperationSignalRef = useRef(mcpOperations.handleSignal);
  const secretOperationSignalRef = useRef(secretOperations.handleSignal);
  const sendRef = useRef<(event: ProjectGuideEvent) => void>(() => undefined);
  const reportRef = useRef<(report: ProjectGuideOperationReport) => void>(
    () => undefined,
  );
  const reportTimersRef = useRef(new Set<number>());
  const nextReportAtRef = useRef(0);
  mcpOperationSignalRef.current = mcpOperations.handleSignal;
  secretOperationSignalRef.current = secretOperations.handleSignal;
  const [snapshot, send] = useMachine(projectGuideMachine, {
    input: {
      onSignal: (signal) => {
        if (signal.type === "abort") {
          nextReportAtRef.current = 0;
        }
        const report =
          signal.type === "prepare"
            ? (result: ProjectGuideOperationReport) =>
                sendRef.current({ type: "ADAPTER_REPORT", report: result })
            : reportRef.current;
        mcpOperationSignalRef.current(signal, report);
        secretOperationSignalRef.current(signal, report);
      },
    },
  });
  sendRef.current = send;
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
    void navigate(routes.home.href(), { replace: true });
  }, [navigate, routes.home]);

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

  const openJourney = (journey: JourneyMeta): void => {
    if (orgSlug && projectSlug) markProjectGuideStarted(orgSlug, projectSlug);
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
        statusByJourney[selectedJourney.id],
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
      <section className="border-border bg-card mx-auto flex w-full max-w-[1200px] flex-col overflow-hidden border shadow-sm">
        <header className="border-border flex items-end gap-3.5 border-b px-6 py-4">
          <div>
            <h5 className="text-eyebrow">Guided Journey</h5>
            <h2 className="text-display-xs">
              {selectedJourney?.title ?? "Put your agent traffic under control"}
            </h2>
          </div>
          {selected && (
            <div className="ml-auto flex items-center gap-3 mb-1">
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
        <div className="flex flex-col md:flex-row">
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
                          if (
                            journey.id === "third-party-mcp" &&
                            currentStep === 2 &&
                            displayState === "checkpoint"
                          ) {
                            mcpOperations.markPromptCopied();
                          }
                        }}
                        onMcpServerSelected={(name) =>
                          send({ type: "SELECT_MCP_SERVER", name })
                        }
                        onSecretPromptCopied={() => {
                          if (
                            journey.id === "secret-block" &&
                            currentStep === 3 &&
                            displayState === "checkpoint"
                          ) {
                            secretOperations.markPromptCopied();
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
                        isProcessing={
                          displayState === "running" ||
                          displayState === "preparing" ||
                          displayState === "waiting"
                        }
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
                          href={
                            journey.id === "secret-block"
                              ? secretOperations.riskEventsHref
                              : undefined
                          }
                        />
                      ) : null
                    }
                    primaryAction={primaryAction ?? null}
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
  journeyStatus: JourneyStatus,
  send: (event: ProjectGuideEvent) => void,
  mcpOperations: McpGuideOperations,
  secretOperations: SecretGuideOperations,
): ProjectGuideRunAction | null {
  switch (displayState) {
    case "ready":
      if (
        journey.id === "third-party-mcp" &&
        currentStep === 0 &&
        mcpOperations.catalogError
      ) {
        return {
          label: "Retry catalog",
          onClick: mcpOperations.retryCatalog,
        };
      }
      if (
        journey.id === "secret-block" &&
        currentStep === 0 &&
        secretOperations.policyError
      ) {
        return {
          label: "Retry policy check",
          onClick: secretOperations.retryPolicy,
        };
      }
      return {
        label:
          journeyStatus === "in-progress" ? "Continue" : "Start the journey",
        icon: "play",
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
        icon: "pause",
        onClick: () => send({ type: "PAUSE" }),
      };
    case "preparing":
      return { label: "Preparing the next step…", disabled: true };
    case "checkpoint":
      if (journey.id === "third-party-mcp" && currentStep === 1) {
        return {
          label: "Server is installed",
          icon: "play",
          disabled:
            !mcpOperations.connectionPrompts ||
            !mcpOperations.connectionPromptCopied,
          onClick: () =>
            send({
              type: "USER_CHECKPOINT_COMPLETE",
              result: "Server added to client",
            }),
        };
      }
      if (journey.id === "third-party-mcp" && currentStep === 2) {
        return {
          label: "Prompt run",
          icon: "play",
          disabled: !mcpOperations.promptCopied,
          onClick: () =>
            send({
              type: "USER_CHECKPOINT_COMPLETE",
              result: "Prompt copied · listening for the governed call",
            }),
        };
      }
      if (journey.id === "secret-block" && currentStep === 2) {
        return {
          label: "Plugin is installed",
          icon: "play",
          disabled:
            !secretOperations.installCommand ||
            secretOperations.baselinePending,
          onClick: () => {
            void (async () => {
              if (!(await secretOperations.prepareTelemetryBaseline())) return;
              send({
                type: "USER_CHECKPOINT_COMPLETE",
                result: "Observability plugin installed and agent restarted",
              });
            })();
          },
        };
      }
      if (journey.id === "secret-block" && currentStep === 1) {
        return {
          label: "Start the journey",
          icon: "play",
          disabled: !secretOperations.clientSelected,
          onClick: () => {
            send({ type: "SELECT_AGENT", client: secretOperations.client });
            send({ type: "START" });
          },
        };
      }
      if (journey.id === "secret-block" && currentStep === 3) {
        return {
          label: "Prompt run",
          icon: "play",
          disabled: !secretOperations.promptCopied,
          onClick: () =>
            send({
              type: "USER_CHECKPOINT_COMPLETE",
              result: "Prompt copied · listening for the blocked event",
            }),
        };
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
        icon: "pause",
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
      return { label: "Start the journey", icon: "play", disabled: true };
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
  onMcpServerSelected,
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
  onMcpServerSelected: (name: string) => void;
  onSecretPromptCopied: () => void;
  onSelectAgent: (client: SecretGuideClient) => void;
}): JSX.Element {
  if (journey.id === "third-party-mcp") {
    return (
      <ProjectGuideMcpStepContent
        journey={journey}
        step={step}
        displayState={displayState}
        operationProgress={operationProgress}
        error={error}
        operations={mcpOperations}
        onMcpPromptCopied={onMcpPromptCopied}
        onMcpServerSelected={onMcpServerSelected}
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

const MCP_CATALOG_PHASES = [
  "Read the server's tool list",
  "Install it into this project",
] as const;

const SECRET_POLICY_PHASES = [
  "Enable the Secrets category",
  "Scope user prompts",
  "Deny the request",
] as const;

const SECRET_PLUGIN_PHASES = [
  "Build the plugin for this project",
  "Sign the bundle",
] as const;

type ProjectGuidePhaseStatus =
  | "not run"
  | "queued"
  | "running"
  | "ok"
  | "failed";

function phaseStatuses(
  labels: readonly string[],
  displayState: ProjectGuideDisplayState,
  operationProgress: number | null,
  hasError: boolean,
  runningAt = labels.map((_, index) => index / labels.length),
): ProjectGuidePhaseStatus[] {
  if (hasError) {
    return labels.map((_, index) => (index === 0 ? "failed" : "queued"));
  }
  if (displayState !== "running" && displayState !== "paused") {
    return labels.map(() => "not run");
  }
  if (operationProgress === null) {
    return labels.map(() => "not run");
  }

  const currentPhase = runningAt.reduce(
    (phase, threshold, index) =>
      operationProgress >= threshold ? index : phase,
    0,
  );
  return labels.map((_, index) => {
    if (operationProgress >= 1 || index < currentPhase) return "ok";
    if (index === currentPhase) return "running";
    return "queued";
  });
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
  const statuses = phaseStatuses(
    SECRET_POLICY_PHASES,
    displayState,
    operationProgress,
    hasError,
  );

  return (
    <ProjectGuidePhaseChecklist
      labels={SECRET_POLICY_PHASES}
      statuses={statuses}
    />
  );
}

function McpCatalogPhases({
  displayState,
  operationProgress,
  hasError,
}: {
  displayState: ProjectGuideDisplayState;
  operationProgress: number | null;
  hasError: boolean;
}): JSX.Element {
  return (
    <ProjectGuidePhaseChecklist
      labels={MCP_CATALOG_PHASES}
      statuses={phaseStatuses(
        MCP_CATALOG_PHASES,
        displayState,
        operationProgress,
        hasError,
        [0, 0.5],
      )}
    />
  );
}

function ProjectGuidePhaseChecklist({
  labels,
  statuses,
}: {
  labels: readonly string[];
  statuses: readonly ProjectGuidePhaseStatus[];
}): JSX.Element {
  return (
    <div className="grid gap-2 pt-2">
      {labels.map((label, index) => {
        const status = statuses[index] ?? "not run";
        const indicatorClassName = {
          "not run": "border-neutral-default",
          queued: "border-neutral-default",
          running: "animate-pulse border-foreground",
          ok: "border-success-default bg-success-default",
          failed: "border-destructive-default",
        }[status];
        return (
          <div key={label} className="flex items-center gap-3 py-1">
            <span
              aria-hidden="true"
              className={cn("size-3 shrink-0 border", indicatorClassName)}
            />
            <span className="text-muted-foreground min-w-0 text-sm">
              {label}
            </span>
            <span className="text-eyebrow text-disabled ml-auto shrink-0">
              {status}
            </span>
          </div>
        );
      })}
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
  const statuses = phaseStatuses(
    SECRET_PLUGIN_PHASES,
    displayState,
    operationProgress,
    hasError,
    [0, 0.75],
  );

  return (
    <ProjectGuidePhaseChecklist
      title="What you get"
      labels={SECRET_PLUGIN_PHASES}
      statuses={statuses}
    />
  );
}

function ProjectGuideMcpStepContent({
  journey,
  step,
  displayState,
  operationProgress,
  error,
  operations,
  onMcpPromptCopied,
  onMcpServerSelected,
}: {
  journey: JourneyMeta;
  step: number;
  displayState: ProjectGuideDisplayState;
  operationProgress: number | null;
  error: string | null;
  operations: McpGuideOperations;
  onMcpPromptCopied: () => void;
  onMcpServerSelected: (name: string) => void;
}): JSX.Element {
  return (
    <div className="grid gap-3 pt-3">
      <p className="text-muted-foreground max-w-md text-body-sm">
        {journey.stepBlurbs[step]}
      </p>
      <McpStepBody
        step={step}
        displayState={displayState}
        operationProgress={operationProgress}
        error={error}
        operations={operations}
        onMcpPromptCopied={onMcpPromptCopied}
        onMcpServerSelected={onMcpServerSelected}
      />
    </div>
  );
}

function McpStepBody({
  step,
  displayState,
  operationProgress,
  error,
  operations,
  onMcpPromptCopied,
  onMcpServerSelected,
}: {
  step: number;
  displayState: ProjectGuideDisplayState;
  operationProgress: number | null;
  error: string | null;
  operations: McpGuideOperations;
  onMcpPromptCopied: () => void;
  onMcpServerSelected: (name: string) => void;
}): JSX.Element | null {
  switch (step) {
    case 0:
      return (
        <McpCatalogSelection
          displayState={displayState}
          operationProgress={operationProgress}
          error={error}
          operations={operations}
          onMcpServerSelected={onMcpServerSelected}
        />
      );
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
  displayState,
  operationProgress,
  error,
  operations,
  onMcpServerSelected,
}: {
  displayState: ProjectGuideDisplayState;
  operationProgress: number | null;
  error: string | null;
  operations: McpGuideOperations;
  onMcpServerSelected: (name: string) => void;
}): JSX.Element {
  if (operations.catalogPending) {
    return (
      <span className="text-eyebrow text-muted-foreground">
        Loading automatic servers
      </span>
    );
  }
  if (operations.catalogError || !operations.catalogServers?.length) {
    return (
      <div className="grid gap-2">
        {operations.catalogServers && (
          <p className="text-muted-foreground text-sm">
            No curated hosted servers are available right now.
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="grid gap-3">
      <div className="grid gap-2 sm:grid-cols-3">
        {operations.catalogServers.map((server) => {
          const name = server.title ?? server.registrySpecifier;
          const selected = operations.selectedServer === server;
          return (
            <button
              key={server.registrySpecifier}
              type="button"
              aria-pressed={selected}
              disabled={displayState !== "ready" && displayState !== "error"}
              onClick={() => {
                operations.selectServer(server);
                onMcpServerSelected(name);
              }}
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
      <McpCatalogPhases
        displayState={displayState}
        operationProgress={operationProgress}
        hasError={Boolean(error) || operations.catalogError}
      />
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
          role={entry.kind === "error" ? "alert" : undefined}
          working={
            isProcessing &&
            !error &&
            entry.kind === "working" &&
            index === entries.length - 1
          }
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
  working: {
    icon: "info",
    iconClass: "text-muted-foreground",
    message: "text-muted-foreground",
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
    if (step === 0 && mcpOperations.activityBaselineError) {
      return "We couldn't prepare the connection yet. Try again.";
    }
    return null;
  }

  if (step === 0 && secretOperations.policyError) {
    return "Could not read this project's risk policies.";
  }
  if (step === 2 && secretOperations.baselineError) {
    return "Could not capture the hook and risk-event baseline. Retry before opening the prompt.";
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
                  "bg-surface-tertiary-default",
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
            key={plate.zone}
            accent={fixture.accent}
            active={active}
            animated={animated}
            index={index}
            plate={plate}
          />
          {index < 2 && (
            <JourneyGraphicPipe
              key={`${plate.zone}-pipe`}
              accent={fixture.accent}
              active={active}
              animated={animated}
              label={
                isMcp
                  ? index === 0
                    ? "request path"
                    : "governed route"
                  : index === 0
                    ? "prompt flow"
                    : "policy boundary"
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

const graphicLiveOpacity = [0, 0, 0, 0, 1, 1, 0, 0, 0];
const graphicOffOpacity = [1, 1, 0, 0, 0, 0, 0, 0, 1];
const graphicLoopTimes = [0, 0.46, 0.52, 0.94, 0.99];
const graphicTextLoopTimes = [0, 0.4, 0.46, 0.52, 0.58, 0.9, 0.94, 0.96, 1];
const graphicStateTransition = {
  duration: 0.16,
  ease: [0.2, 0.7, 0.3, 1] as const,
};

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
        ease: "easeInOut" as const,
        repeat: Infinity,
        times: graphicTextLoopTimes,
      }
    : {
        ...graphicStateTransition,
        delay: active ? graphicStateTransition.duration : 0,
      };
  const offTransition = animated
    ? {
        duration: 9,
        ease: "easeInOut" as const,
        repeat: Infinity,
        times: graphicTextLoopTimes,
      }
    : {
        ...graphicStateTransition,
        delay: active ? 0 : graphicStateTransition.duration,
      };

  return (
    <span
      data-testid={isCenter ? "project-guide-graphic-center-plate" : undefined}
      className={cn(
        "border-border bg-background relative flex w-4/5 flex-col gap-2 border p-4",
        isCenter && "w-full bg-muted p-5",
      )}
    >
      {isCenter && (
        <motion.span
          aria-hidden="true"
          data-testid="project-guide-graphic-center-glow"
          data-glow-state={active ? "on" : "off"}
          animate={animated ? liveOpacity : { opacity: active ? 1 : 0 }}
          transition={
            animated
              ? liveTransition
              : {
                  ...graphicStateTransition,
                  delay: active ? graphicStateTransition.duration : 0,
                }
          }
          className="pointer-events-none absolute inset-0"
          style={{
            boxShadow: `inset 0 0 0 1px ${accent}, 0 0 18px ${accent}66`,
          }}
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
              "relative grid min-w-0 text-sm",
              isCenter && "text-xl",
            )}
          >
            <motion.span
              initial={false}
              animate={offOpacity}
              transition={offTransition}
              className="col-start-1 row-start-1 block truncate"
            >
              {offName}
            </motion.span>
            <motion.span
              initial={false}
              animate={liveOpacity}
              transition={liveTransition}
              className="col-start-1 row-start-1 block truncate"
            >
              {plate.name}
            </motion.span>
          </span>
        </span>
        <span className="text-eyebrow relative mt-0 mb-auto grid min-w-0 flex-[0_1_35%] text-right">
          <motion.span
            initial={false}
            animate={offOpacity}
            transition={offTransition}
            className="text-disabled col-start-1 row-start-1 block min-w-0 wrap-break-words whitespace-normal"
          >
            {plate.off}
          </motion.span>
          <motion.span
            initial={false}
            animate={liveOpacity}
            transition={liveTransition}
            className="col-start-1 row-start-1 block min-w-0 wrap-break-words whitespace-normal"
            style={{ color: accent }}
          >
            {plate.on}
          </motion.span>
        </span>
      </span>
      {isCenter && (
        <span className="relative grid">
          <span
            data-testid="project-guide-signal-track"
            aria-hidden="true"
            className="col-start-1 row-start-1 flex gap-1"
          >
            {Array.from({ length: 12 }, (_, tick) => (
              <span key={tick} className="bg-border h-1 flex-1" />
            ))}
          </span>
          <motion.span
            initial={false}
            animate={liveOpacity}
            transition={liveTransition}
            aria-hidden="true"
            className="col-start-1 row-start-1 flex gap-1"
          >
            {Array.from({ length: 12 }, (_, tick) => (
              <motion.span
                key={tick}
                animate={
                  animated
                    ? { opacity: [0.2, 1, 0.2] }
                    : { opacity: active ? 1 : 0.2 }
                }
                transition={
                  animated
                    ? {
                        duration: 2.4,
                        ease: "easeInOut",
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
        </span>
      )}
    </span>
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
                ease: "easeInOut",
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
                ease: "easeInOut",
                repeat: Infinity,
                times: [0, 0.3, 0.36, 0.96, 1],
              }
            : { duration: 0 }
        }
        className="bg-foreground/60 absolute inset-0 overflow-hidden"
      >
        <GraphicPipeStripes animated={animated} />
      </motion.span>
      <motion.span
        animate={{ opacity: liveOpacity }}
        transition={
          animated
            ? {
                duration: 9,
                ease: "easeInOut",
                repeat: Infinity,
                times: graphicLoopTimes,
              }
            : { duration: 0 }
        }
        className="absolute inset-0 overflow-hidden"
        style={{ backgroundColor: `${accent}70` }}
      >
        <GraphicPipeStripes animated={animated} />
      </motion.span>
      <span className="text-eyebrow text-disabled absolute top-1/2 left-6 -translate-y-1/2 whitespace-nowrap">
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
      className="flex h-full w-full flex-col items-center gap-3.5 py-4 hover:bg-card/60"
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
      className="border-border bg-card grid w-full max-w-[1200px] gap-4 border p-6"
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
        <Button type="button" onClick={onReturnToProjectHome} variant="primary">
          {PROJECT_GUIDE_COMPLETE.primaryAction}
        </Button>
      </div>
    </motion.section>
  );
}
