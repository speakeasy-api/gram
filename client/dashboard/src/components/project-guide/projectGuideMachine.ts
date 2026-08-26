import {
  PROJECT_GUIDE_JOURNEYS,
  type JourneyId,
} from "@/components/project-guide/journeys";
import { assign, setup } from "xstate";

export type ProjectGuideEventCard = {
  kind: string;
  tone: "allow" | "deny";
  title: string;
  rows: Array<{ key: string; value: string }>;
  note: string;
};

export const PROJECT_GUIDE_OUTPUT_LIMIT = 18;
export const LISTEN_TIMEOUT_SECONDS = 90;
export const PROJECT_GUIDE_MICRO_STEP_DELAY_MS = 3000;

export type ProjectGuideDisplayState =
  | "opening"
  | "ready"
  | "running"
  | "preparing"
  | "checkpoint"
  | "waiting"
  | "paused"
  | "error"
  | "complete";

export type ProjectGuideOutputEntry = {
  id: number;
  kind: "start" | "working" | "note" | "next" | "result" | "error";
  message: string;
};

export type ProjectGuideOperationScope = {
  path: JourneyId;
  step: number;
  attempt: number;
  runId: number;
};

export type ProjectGuideOperationReport =
  | {
      type: "progress";
      scope: ProjectGuideOperationScope;
      message: string;
      progress?: number;
    }
  | {
      type: "success";
      scope: ProjectGuideOperationScope;
      result: string;
    }
  | {
      type: "error";
      scope: ProjectGuideOperationScope;
      message: string;
    }
  | {
      type: "event";
      scope: ProjectGuideOperationScope;
      event: ProjectGuideEventCard;
    };

export type ProjectGuideOperationSignal =
  | { type: "start"; scope: ProjectGuideOperationScope }
  | { type: "prepare"; scope: ProjectGuideOperationScope }
  | { type: "pause"; scope: ProjectGuideOperationScope }
  | { type: "resume"; scope: ProjectGuideOperationScope }
  | { type: "retry"; scope: ProjectGuideOperationScope }
  | { type: "checkpoint"; scope: ProjectGuideOperationScope }
  | {
      type: "abort";
      scope: ProjectGuideOperationScope;
      reason: "switch" | "back" | "rewind";
    };

export type ProjectGuideEvent =
  | { type: "OPEN"; path: JourneyId; resumeStep: number }
  | { type: "SWITCH"; path: JourneyId; resumeStep: number }
  | { type: "BACK" }
  | { type: "START" }
  | { type: "SELECT_MCP_SERVER"; name: string }
  | { type: "SELECT_AGENT"; client: string }
  | { type: "PAUSE" }
  | { type: "RESUME" }
  | { type: "USER_CHECKPOINT_COMPLETE"; result: string }
  | { type: "ADAPTER_REPORT"; report: ProjectGuideOperationReport }
  | { type: "LISTEN_TICK"; elapsedSeconds: number }
  | { type: "RETRY" }
  | { type: "REWIND"; step: number };

export type ProjectGuideMachineContext = {
  activePath: JourneyId | null;
  completedByPath: Record<JourneyId, number[]>;
  output: ProjectGuideOutputEntry[];
  elapsedListeningSeconds: number;
  operationProgress: number | null;
  error: string | null;
  observedEvent: ProjectGuideEventCard | null;
  attempt: number;
  runId: number;
  pausedFrom: "running" | "waiting";
  errorFrom: "running" | "preparing" | "waiting";
  selectedClient: string | null;
  nextOutputId: number;
  onSignal?: (signal: ProjectGuideOperationSignal) => void;
};

export type ProjectGuideMachineInput = {
  onSignal?: (signal: ProjectGuideOperationSignal) => void;
};

type StepMode = "operation" | "checkpoint" | "listen";

const STEP_MODES: readonly StepMode[] = [
  "operation",
  "operation",
  "checkpoint",
  "checkpoint",
  "listen",
];

const MCP_STEP_MODES: readonly StepMode[] = [
  "operation",
  "checkpoint",
  "checkpoint",
  "listen",
];

const JOURNEY_BY_ID = Object.fromEntries(
  PROJECT_GUIDE_JOURNEYS.map((journey) => [journey.id, journey]),
) as Record<JourneyId, (typeof PROJECT_GUIDE_JOURNEYS)[number]>;

const NARRATIVE_STEP_RESULTS: Record<JourneyId, readonly string[]> = {
  "third-party-mcp": [
    "Server selected",
    "Endpoint verified",
    "Client connected",
    "Governed call recorded",
  ],
  "secret-block": [
    "Secrets policy created",
    "Observability plugin downloaded",
    "Observability plugin installed",
    "Blocked risk event recorded",
    "Blocked risk event recorded",
  ],
};

const NARRATIVE_STEP_LABELS: Partial<Record<JourneyId, readonly string[]>> = {
  "third-party-mcp": [
    "Pick a server",
    "Connect your agent to this server",
    "Prompt agent to list the tools",
    "Watch the first governed call",
  ],
};

export function projectGuideOperationKey(
  scope: ProjectGuideOperationScope,
): string {
  return `${scope.path}:${scope.step}:${scope.attempt}:${scope.runId}`;
}

function initialCoordinatorContext(
  input: ProjectGuideMachineInput,
): ProjectGuideMachineContext {
  return {
    activePath: null,
    completedByPath: {
      "third-party-mcp": [],
      "secret-block": [],
    },
    output: [],
    elapsedListeningSeconds: 0,
    operationProgress: null,
    error: null,
    observedEvent: null,
    attempt: 0,
    runId: 0,
    pausedFrom: "running",
    errorFrom: "running",
    selectedClient: null,
    nextOutputId: 0,
    onSignal: input.onSignal,
  };
}

export function getProjectGuideCurrentStep(
  context: ProjectGuideMachineContext,
): number {
  if (!context.activePath) return 0;
  return context.completedByPath[context.activePath].length;
}

function stepMode(context: ProjectGuideMachineContext, offset = 0): StepMode {
  const step = getProjectGuideCurrentStep(context) + offset;
  return context.activePath
    ? stepModeForPath(context.activePath, step, context.selectedClient)
    : (STEP_MODES[step] ?? "listen");
}

function stepModeForPath(
  path: JourneyId,
  step: number,
  selectedClient: string | null = null,
): StepMode {
  if (path === "third-party-mcp") {
    return MCP_STEP_MODES[step] ?? "listen";
  }
  if (path === "secret-block" && step === 1 && selectedClient === null) {
    return "checkpoint";
  }
  return STEP_MODES[step] ?? "listen";
}

function stepLabel(context: ProjectGuideMachineContext, offset = 0): string {
  if (!context.activePath) return "Journey";
  return (
    JOURNEY_BY_ID[context.activePath].steps[
      getProjectGuideCurrentStep(context) + offset
    ] ?? "Journey complete"
  );
}

function narrativeStepLabel(path: JourneyId, step: number): string {
  return (
    NARRATIVE_STEP_LABELS[path]?.[step] ??
    JOURNEY_BY_ID[path].steps[step] ??
    "Journey complete"
  );
}

function pathStepCount(path: JourneyId): number {
  return JOURNEY_BY_ID[path].steps.length;
}

function currentPathIsComplete(context: ProjectGuideMachineContext): boolean {
  return Boolean(
    context.activePath &&
    getProjectGuideCurrentStep(context) >= pathStepCount(context.activePath),
  );
}

function selectedPathIsComplete(
  context: ProjectGuideMachineContext,
  event: Extract<ProjectGuideEvent, { type: "OPEN" | "SWITCH" }>,
): boolean {
  return (
    Math.max(
      context.completedByPath[event.path].length,
      Math.max(0, event.resumeStep),
    ) >= pathStepCount(event.path)
  );
}

function appendOutput(
  context: ProjectGuideMachineContext,
  entries: Array<Omit<ProjectGuideOutputEntry, "id">>,
): Pick<ProjectGuideMachineContext, "output" | "nextOutputId"> {
  const output = entries.map((entry, index) => ({
    ...entry,
    id: context.nextOutputId + index,
  }));
  return {
    output: [...context.output, ...output].slice(-PROJECT_GUIDE_OUTPUT_LIMIT),
    nextOutputId: context.nextOutputId + entries.length,
  };
}

function narrativeOutputFor(
  path: JourneyId,
  completedStepCount: number,
): Array<Omit<ProjectGuideOutputEntry, "id">> {
  const journey = JOURNEY_BY_ID[path];
  const completedCount = Math.min(completedStepCount, journey.steps.length);
  const entries: Array<Omit<ProjectGuideOutputEntry, "id">> = [];

  for (let index = 0; index < completedCount; index += 1) {
    entries.push({
      kind: "result",
      message:
        NARRATIVE_STEP_RESULTS[path][index] ??
        `Completed · ${journey.steps[index]}`,
    });
    if (journey.steps[index + 1]) {
      entries.push({
        kind: "next",
        message: `Next · ${narrativeStepLabel(path, index + 1)}`,
      });
    }
  }

  entries.push({
    kind: "note",
    message:
      completedCount === 0
        ? "Ready to start"
        : `Ready · ${narrativeStepLabel(path, completedCount)}`,
  });
  return entries;
}

function completedThrough(step: number, stepCount: number): number[] {
  return Array.from(
    { length: Math.min(Math.max(0, step), stepCount) },
    (_, index) => index,
  );
}

function emitCurrentSignal(
  context: ProjectGuideMachineContext,
  type: "start" | "prepare" | "pause" | "resume" | "checkpoint",
): void {
  const scope = currentOperationScope(context);
  if (!scope) return;
  context.onSignal?.({
    type,
    scope,
  });
}

function currentOperationScope(
  context: ProjectGuideMachineContext,
): ProjectGuideOperationScope | null {
  if (!context.activePath) return null;
  return {
    path: context.activePath,
    step: getProjectGuideCurrentStep(context),
    attempt: context.attempt,
    runId: context.runId,
  };
}

function isCurrentReport(
  context: ProjectGuideMachineContext,
  event: ProjectGuideEvent,
  type: ProjectGuideOperationReport["type"],
): boolean {
  if (event.type !== "ADAPTER_REPORT" || event.report.type !== type) {
    return false;
  }
  const scope = currentOperationScope(context);
  return Boolean(
    scope &&
    event.report.scope.path === scope.path &&
    event.report.scope.step === scope.step &&
    event.report.scope.attempt === scope.attempt &&
    event.report.scope.runId === scope.runId,
  );
}

export const projectGuideMachine = setup({
  types: {
    context: {} as ProjectGuideMachineContext,
    events: {} as ProjectGuideEvent,
    input: {} as ProjectGuideMachineInput,
  },
  guards: {
    selectedPathComplete: ({ context, event }) =>
      (event.type === "OPEN" || event.type === "SWITCH") &&
      selectedPathIsComplete(context, event),
    currentOperation: ({ context }) => stepMode(context) === "operation",
    currentCheckpoint: ({ context }) => stepMode(context) === "checkpoint",
    resumeAtCheckpoint: ({ context, event }) => {
      if (event.type !== "OPEN" && event.type !== "SWITCH") return false;
      const step = Math.max(
        context.completedByPath[event.path].length,
        event.resumeStep,
      );
      return stepModeForPath(event.path, step) === "checkpoint";
    },
    selectedSecretAgent: ({ context }) =>
      context.activePath === "secret-block" &&
      getProjectGuideCurrentStep(context) === 1 &&
      context.selectedClient !== null,
    selectingSecretAgent: ({ context, event }) =>
      context.activePath === "secret-block" &&
      getProjectGuideCurrentStep(context) === 1 &&
      event.type === "SELECT_AGENT",
    finalStep: ({ context }) =>
      currentPathIsComplete(context) ||
      Boolean(
        context.activePath &&
        getProjectGuideCurrentStep(context) ===
          pathStepCount(context.activePath) - 1,
      ),
    nextOperation: ({ context }) => stepMode(context, 1) === "operation",
    nextCheckpoint: ({ context }) => stepMode(context, 1) === "checkpoint",
    currentProgressReport: ({ context, event }) =>
      isCurrentReport(context, event, "progress"),
    currentSuccessReport: ({ context, event }) =>
      isCurrentReport(context, event, "success"),
    currentErrorReport: ({ context, event }) =>
      isCurrentReport(context, event, "error"),
    currentEventReport: ({ context, event }) =>
      isCurrentReport(context, event, "event"),
    currentSuccessOnFinalStep: ({ context, event }) =>
      isCurrentReport(context, event, "success") &&
      (currentPathIsComplete(context) ||
        Boolean(
          context.activePath &&
          getProjectGuideCurrentStep(context) ===
            pathStepCount(context.activePath) - 1,
        )),
    currentSuccessBeforeOperation: ({ context, event }) =>
      isCurrentReport(context, event, "success") &&
      stepMode(context, 1) === "operation",
    currentSuccessBeforeCheckpoint: ({ context, event }) =>
      isCurrentReport(context, event, "success") &&
      stepMode(context, 1) === "checkpoint",
    currentSuccessBeforeMcpPreparation: ({ context, event }) =>
      context.activePath === "third-party-mcp" &&
      getProjectGuideCurrentStep(context) === 0 &&
      isCurrentReport(context, event, "success") &&
      stepMode(context, 1) === "checkpoint",
    pausedWhileWaiting: ({ context }) => context.pausedFrom === "waiting",
    erroredWhileWaiting: ({ context }) => context.errorFrom === "waiting",
    erroredWhilePreparing: ({ context }) => context.errorFrom === "preparing",
    listenTimedOut: ({ event }) =>
      event.type === "LISTEN_TICK" &&
      event.elapsedSeconds >= LISTEN_TIMEOUT_SECONDS,
  },
  actions: {
    openPath: assign(({ context, event }) => {
      if (event.type !== "OPEN" && event.type !== "SWITCH") return {};
      const completed = completedThrough(
        Math.max(context.completedByPath[event.path].length, event.resumeStep),
        pathStepCount(event.path),
      );
      const readyOutput = appendOutput(
        { ...context, output: [] },
        narrativeOutputFor(event.path, completed.length),
      );
      return {
        activePath: event.path,
        completedByPath: {
          ...context.completedByPath,
          [event.path]: completed,
        },
        ...readyOutput,
        elapsedListeningSeconds: 0,
        operationProgress: null,
        error: null,
        observedEvent: null,
        attempt: 0,
        selectedClient: null,
      };
    }),
    selectAgent: assign(({ event }) =>
      event.type === "SELECT_AGENT" ? { selectedClient: event.client } : {},
    ),
    recordMcpServerSelected: assign(({ context, event }) => {
      if (event.type !== "SELECT_MCP_SERVER") return {};
      return appendOutput(context, [
        {
          kind: "note",
          message: `${event.name} selected. Ready to start the journey`,
        },
      ]);
    }),
    recordStart: assign(({ context }) => {
      const mode = stepMode(context);
      const message =
        context.activePath === "third-party-mcp" &&
        getProjectGuideCurrentStep(context) === 0
          ? "Starting…"
          : `Started · ${stepLabel(context)}`;
      return {
        ...appendOutput(context, [{ kind: "start", message }]),
        error: null,
        operationProgress: null,
        attempt: 0,
        runId: mode === "checkpoint" ? context.runId : context.runId + 1,
      };
    }),
    recordProgress: assign(({ context, event }) => {
      if (event.type !== "ADAPTER_REPORT" || event.report.type !== "progress") {
        return {};
      }
      const progress = event.report.progress;
      return {
        ...appendOutput(context, [
          { kind: "working", message: event.report.message },
        ]),
        operationProgress:
          progress === undefined
            ? context.operationProgress
            : Math.min(1, Math.max(0, progress)),
      };
    }),
    recordSuccessAndAdvance: assign(({ context, event }) => {
      let result: string;
      if (event.type === "ADAPTER_REPORT" && event.report.type === "success") {
        result = event.report.result;
      } else if (event.type === "USER_CHECKPOINT_COMPLETE") {
        result = event.result;
      } else {
        return {};
      }
      if (!context.activePath) return {};
      const nextStep = getProjectGuideCurrentStep(context) + 1;
      const nextLabel = JOURNEY_BY_ID[context.activePath].steps[nextStep]
        ? narrativeStepLabel(context.activePath, nextStep)
        : undefined;
      const entries: Array<Omit<ProjectGuideOutputEntry, "id">> = [
        {
          kind: "result",
          message: result,
        },
      ];
      if (nextLabel)
        entries.push({ kind: "next", message: `Next · ${nextLabel}` });
      return {
        completedByPath: {
          ...context.completedByPath,
          [context.activePath]: completedThrough(
            nextStep,
            pathStepCount(context.activePath),
          ),
        },
        ...appendOutput(context, entries),
        elapsedListeningSeconds: 0,
        operationProgress: null,
        error: null,
        attempt: 0,
        runId:
          stepMode(context, 1) === "operation" ||
          stepMode(context, 1) === "listen"
            ? context.runId + 1
            : context.runId,
      };
    }),
    recordEventAndComplete: assign(({ context, event }) => {
      if (
        event.type !== "ADAPTER_REPORT" ||
        event.report.type !== "event" ||
        !context.activePath
      ) {
        return {};
      }
      return {
        completedByPath: {
          ...context.completedByPath,
          [context.activePath]: completedThrough(
            pathStepCount(context.activePath),
            pathStepCount(context.activePath),
          ),
        },
        ...appendOutput(context, [
          {
            kind: "result",
            message: `Event received · ${event.report.event.title}`,
          },
        ]),
        observedEvent: event.report.event,
        operationProgress: null,
        error: null,
      };
    }),
    recordError: assign(({ context, event }) => {
      if (event.type !== "ADAPTER_REPORT" || event.report.type !== "error") {
        return {};
      }
      return {
        ...appendOutput(context, [
          { kind: "error", message: event.report.message },
        ]),
        error: event.report.message,
      };
    }),
    recordTimeout: assign(({ context, event }) => {
      if (event.type !== "LISTEN_TICK") return {};
      const message = `No event seen in ${LISTEN_TIMEOUT_SECONDS}s. Check the client, then listen again.`;
      return {
        ...appendOutput(context, [{ kind: "error", message }]),
        elapsedListeningSeconds: event.elapsedSeconds,
        error: message,
        errorFrom: "waiting" as const,
      };
    }),
    recordElapsed: assign(({ context, event }) =>
      event.type === "LISTEN_TICK"
        ? {
            elapsedListeningSeconds: Math.max(
              context.elapsedListeningSeconds,
              event.elapsedSeconds,
            ),
          }
        : {},
    ),
    rememberRunningError: assign({ errorFrom: "running" }),
    rememberPreparingError: assign({ errorFrom: "preparing" }),
    rememberWaitingError: assign({ errorFrom: "waiting" }),
    rememberRunningPause: assign({ pausedFrom: "running" }),
    rememberWaitingPause: assign({ pausedFrom: "waiting" }),
    retry: assign(({ context }) => ({
      ...appendOutput(context, [
        { kind: "note", message: "Retrying · current step" },
      ]),
      attempt: context.attempt + 1,
      elapsedListeningSeconds:
        context.errorFrom === "waiting" ? 0 : context.elapsedListeningSeconds,
      error: null,
    })),
    rewind: assign(({ context, event }) => {
      if (event.type !== "REWIND" || !context.activePath) return {};
      const currentStep = getProjectGuideCurrentStep(context);
      const targetStep = Math.max(0, Math.min(event.step, currentStep));
      const label = JOURNEY_BY_ID[context.activePath].steps[targetStep];
      return {
        completedByPath: {
          ...context.completedByPath,
          [context.activePath]: completedThrough(
            targetStep,
            pathStepCount(context.activePath),
          ),
        },
        ...appendOutput({ ...context, output: [] }, [
          { kind: "note", message: `Ready · ${label ?? "Journey"}` },
        ]),
        elapsedListeningSeconds: 0,
        operationProgress: null,
        error: null,
        observedEvent: null,
      };
    }),
    clearActivePath: assign({
      activePath: null,
      output: [],
      elapsedListeningSeconds: 0,
      operationProgress: null,
      error: null,
      observedEvent: null,
    }),
    signalStart: ({ context }) => emitCurrentSignal(context, "start"),
    signalPrepareMcp: ({ context }) => {
      if (context.activePath === "third-party-mcp") {
        emitCurrentSignal(context, "prepare");
      }
    },
    signalPause: ({ context }) => emitCurrentSignal(context, "pause"),
    signalResume: ({ context }) => emitCurrentSignal(context, "resume"),
    signalCheckpoint: ({ context }) => emitCurrentSignal(context, "checkpoint"),
    signalRetry: ({ context }) => {
      const scope = currentOperationScope(context);
      if (!scope) return;
      context.onSignal?.({
        type: "retry",
        scope,
      });
    },
    signalAbort: ({ context, event }) => {
      const scope = currentOperationScope(context);
      if (!scope) return;
      const reason =
        event.type === "SWITCH"
          ? "switch"
          : event.type === "BACK"
            ? "back"
            : "rewind";
      context.onSignal?.({
        type: "abort",
        scope,
        reason,
      });
    },
  },
}).createMachine({
  id: "projectGuideCoordinator",
  initial: "opening",
  context: ({ input }) => initialCoordinatorContext(input),
  on: {
    SELECT_AGENT: { actions: "selectAgent" },
    SWITCH: [
      {
        guard: "selectedPathComplete",
        target: "#projectGuideCoordinator.complete",
        actions: ["signalAbort", "openPath"],
      },
      {
        guard: "resumeAtCheckpoint",
        target: "#projectGuideCoordinator.checkpoint",
        actions: ["signalAbort", "openPath"],
      },
      {
        target: "#projectGuideCoordinator.ready",
        actions: ["signalAbort", "openPath"],
      },
    ],
    BACK: {
      target: "#projectGuideCoordinator.opening",
      actions: ["signalAbort", "clearActivePath"],
    },
    REWIND: {
      target: "#projectGuideCoordinator.ready",
      actions: ["signalAbort", "rewind"],
    },
  },
  states: {
    opening: {
      on: {
        OPEN: [
          {
            guard: "selectedPathComplete",
            target: "complete",
            actions: "openPath",
          },
          {
            guard: "resumeAtCheckpoint",
            target: "checkpoint",
            actions: "openPath",
          },
          { target: "ready", actions: "openPath" },
        ],
      },
    },
    ready: {
      on: {
        SELECT_MCP_SERVER: { actions: "recordMcpServerSelected" },
        START: [
          {
            guard: "currentOperation",
            target: "running",
            actions: ["recordStart", "signalStart"],
          },
          {
            guard: "currentCheckpoint",
            target: "checkpoint",
            actions: "recordStart",
          },
          {
            target: "waiting",
            actions: ["recordStart", "signalStart"],
          },
        ],
      },
    },
    running: {
      on: {
        ADAPTER_REPORT: [
          {
            guard: "currentProgressReport",
            actions: "recordProgress",
          },
          {
            guard: "currentSuccessBeforeMcpPreparation",
            target: "preparing",
            actions: "signalPrepareMcp",
          },
          {
            guard: "currentErrorReport",
            target: "error",
            actions: ["rememberRunningError", "recordError"],
          },
          {
            guard: "currentSuccessOnFinalStep",
            target: "complete",
            actions: "recordSuccessAndAdvance",
          },
          {
            guard: "currentSuccessBeforeOperation",
            target: "running",
            actions: ["recordSuccessAndAdvance", "signalStart"],
          },
          {
            guard: "currentSuccessBeforeCheckpoint",
            target: "checkpoint",
            actions: "recordSuccessAndAdvance",
          },
          {
            guard: "currentSuccessReport",
            target: "waiting",
            actions: ["recordSuccessAndAdvance", "signalStart"],
          },
        ],
        PAUSE: {
          target: "paused",
          actions: ["rememberRunningPause", "signalPause"],
        },
      },
    },
    preparing: {
      on: {
        ADAPTER_REPORT: [
          {
            guard: "currentSuccessReport",
            target: "checkpoint",
            actions: "recordSuccessAndAdvance",
          },
          {
            guard: "currentErrorReport",
            target: "error",
            actions: ["rememberPreparingError", "recordError"],
          },
        ],
      },
    },
    checkpoint: {
      on: {
        SELECT_AGENT: {
          guard: "selectingSecretAgent",
          target: "running",
          actions: ["selectAgent", "recordStart", "signalStart"],
        },
        START: {
          guard: "selectedSecretAgent",
          target: "running",
          actions: ["recordStart", "signalStart"],
        },
        USER_CHECKPOINT_COMPLETE: [
          {
            guard: "finalStep",
            target: "complete",
            actions: ["signalCheckpoint", "recordSuccessAndAdvance"],
          },
          {
            guard: "nextOperation",
            target: "running",
            actions: [
              "signalCheckpoint",
              "recordSuccessAndAdvance",
              "signalStart",
            ],
          },
          {
            guard: "nextCheckpoint",
            target: "checkpoint",
            actions: ["signalCheckpoint", "recordSuccessAndAdvance"],
          },
          {
            target: "waiting",
            actions: [
              "signalCheckpoint",
              "recordSuccessAndAdvance",
              "signalStart",
            ],
          },
        ],
      },
    },
    waiting: {
      on: {
        LISTEN_TICK: [
          {
            guard: "listenTimedOut",
            target: "error",
            actions: "recordTimeout",
          },
          { actions: "recordElapsed" },
        ],
        ADAPTER_REPORT: [
          {
            guard: "currentEventReport",
            target: "complete",
            actions: "recordEventAndComplete",
          },
          {
            guard: "currentErrorReport",
            target: "error",
            actions: ["rememberWaitingError", "recordError"],
          },
          {
            guard: "currentProgressReport",
            actions: "recordProgress",
          },
        ],
        PAUSE: {
          target: "paused",
          actions: ["rememberWaitingPause", "signalPause"],
        },
      },
    },
    paused: {
      on: {
        RESUME: [
          {
            guard: "pausedWhileWaiting",
            target: "waiting",
            actions: "signalResume",
          },
          { target: "running", actions: "signalResume" },
        ],
      },
    },
    error: {
      on: {
        SELECT_MCP_SERVER: { actions: "recordMcpServerSelected" },
        RETRY: [
          {
            guard: "erroredWhilePreparing",
            target: "preparing",
            actions: ["retry", "signalPrepareMcp"],
          },
          {
            guard: "erroredWhileWaiting",
            target: "waiting",
            actions: ["retry", "signalRetry"],
          },
          {
            target: "running",
            actions: ["retry", "signalRetry"],
          },
        ],
      },
    },
    complete: {},
  },
});
