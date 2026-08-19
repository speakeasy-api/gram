import {
  PROJECT_GUIDE_JOURNEYS,
  type JourneyId,
} from "@/components/project-guide/journeys";
import { assign, setup } from "xstate";

export type ProjectGuidePhase =
  | "idle"
  | "await"
  | "running"
  | "waiting"
  | "success"
  | "error"
  | "complete";

export type ProjectGuideStepKind =
  | "pick"
  | "phase"
  | "tabs"
  | "prompt"
  | "listen";

export type ProjectGuideEventCard = {
  kind: string;
  tone: "allow" | "deny";
  title: string;
  rows: Array<{ key: string; value: string }>;
  note: string;
};

export type LegacyProjectGuideEvent =
  | { type: "START" }
  | { type: "PAUSE" }
  | { type: "RESUME" }
  | { type: "RETRY" }
  | { type: "COPY" }
  | { type: "CONFIRM" }
  | { type: "SELECT"; value: string }
  | { type: "OPERATION_SUCCESS"; result: string }
  | { type: "STEP_SUCCESS"; result: string }
  | { type: "EVENT_RECEIVED"; event: ProjectGuideEventCard }
  | { type: "LISTEN" }
  | { type: "TICK" }
  | { type: "FAIL"; message: string }
  | { type: "REWIND"; step: number }
  | { type: "RESET" };

export type LegacyProjectGuideMachineContext = {
  step: number;
  completed: number[];
  selected: string | null;
  copied: boolean;
  elapsed: number;
  progress: number;
  event: ProjectGuideEventCard | null;
  error: string | null;
  attempt: number;
  autorun: boolean;
  pausedFrom: "running" | "waiting";
  stepKinds: ProjectGuideStepKind[];
  logs: string[];
};

export type LegacyProjectGuideMachineInput = {
  initialStep?: number;
  completed?: number[];
  stepKinds?: ProjectGuideStepKind[];
};

const DEFAULT_STEP_KINDS: ProjectGuideStepKind[] = [
  "pick",
  "phase",
  "tabs",
  "prompt",
  "listen",
];

function gateFor(kind: ProjectGuideStepKind): boolean {
  return kind === "pick" || kind === "tabs" || kind === "prompt";
}

export const legacyProjectGuideMachine = setup({
  types: {
    context: {} as LegacyProjectGuideMachineContext,
    events: {} as LegacyProjectGuideEvent,
    input: {} as LegacyProjectGuideMachineInput,
  },
  guards: {
    needsGate: ({ context }) =>
      gateFor(context.stepKinds[context.step] ?? "phase"),
    autorun: ({ context }) => context.autorun,
    autorunAndGate: ({ context }) =>
      context.autorun && gateFor(context.stepKinds[context.step] ?? "phase"),
    hasCopied: ({ context }) => context.copied,
    hasNextStep: ({ context }) => context.step < context.stepKinds.length - 1,
    wasWaiting: ({ context }) => context.pausedFrom === "waiting",
  },
}).createMachine({
  id: "projectGuide",
  initial: "idle",
  context: ({ input }) => {
    const completed = input.completed ?? [];
    return {
      step: input.initialStep ?? completed.length,
      completed,
      selected: null,
      copied: false,
      elapsed: 0,
      progress: 0,
      event: null,
      error: null,
      attempt: 0,
      autorun: false,
      pausedFrom: "running",
      stepKinds: input.stepKinds ?? DEFAULT_STEP_KINDS,
      logs: [],
    };
  },
  states: {
    idle: {
      entry: assign({ error: null }),
      always: [
        { guard: "autorunAndGate", target: "await" },
        { guard: "autorun", target: "running" },
      ],
      on: {
        START: [
          {
            guard: "needsGate",
            target: "await",
            actions: assign({ autorun: true }),
          },
          { target: "running", actions: assign({ autorun: true }) },
        ],
        SELECT: {
          target: "running",
          actions: assign(({ event }) => ({
            selected: event.value,
            autorun: true,
            logs: [`▸ selected · ${event.value}`],
          })),
        },
        REWIND: {
          actions: assign(({ context, event }) => ({
            step: event.step,
            completed: context.completed.filter((n) => n < event.step),
            copied: false,
            selected: null,
            event: null,
            logs: [],
            autorun: false,
          })),
        },
        RESET: "idle",
      },
    },
    await: {
      on: {
        SELECT: {
          target: "running",
          actions: assign(({ event }) => ({
            selected: event.value,
            logs: [`▸ selected · ${event.value}`],
          })),
        },
        COPY: { actions: assign({ copied: true }) },
        CONFIRM: [
          {
            guard: "hasCopied",
            target: "running",
            actions: assign({ copied: false }),
          },
          {
            guard: ({ context }) => context.stepKinds[context.step] === "pick",
            target: "running",
          },
        ],
        START: { actions: assign({ autorun: true }) },
        PAUSE: { target: "paused", actions: assign({ pausedFrom: "running" }) },
        RESET: "idle",
      },
    },
    running: {
      entry: assign({ error: null }),
      on: {
        OPERATION_SUCCESS: {
          target: "success",
          actions: assign(({ context, event }) => ({
            progress: 1,
            logs: [...context.logs, `✓ ${event.result}`].slice(-18),
          })),
        },
        STEP_SUCCESS: {
          target: "success",
          actions: assign(({ context, event }) => ({
            progress: 1,
            logs: [...context.logs, `✓ ${event.result}`].slice(-18),
          })),
        },
        LISTEN: { target: "waiting", actions: assign({ elapsed: 0 }) },
        EVENT_RECEIVED: {
          target: "success",
          actions: assign(({ context, event }) => ({
            event: event.event,
            progress: 1,
            logs: [...context.logs, "✓ event received"].slice(-18),
          })),
        },
        FAIL: {
          target: "error",
          actions: assign(({ event }) => ({ error: event.message })),
        },
        PAUSE: { target: "paused", actions: assign({ pausedFrom: "running" }) },
      },
    },
    waiting: {
      entry: assign({ elapsed: 0 }),
      after: {
        60000: {
          target: "error",
          actions: assign({
            error: "No event seen in 60s. Check the client, then listen again.",
          }),
        },
      },
      on: {
        TICK: {
          actions: assign(({ context }) => ({
            elapsed: Math.min(context.elapsed + 0.1, 60),
          })),
        },
        EVENT_RECEIVED: {
          target: "success",
          actions: assign(({ context, event }) => ({
            event: event.event,
            progress: 1,
            logs: [...context.logs, "✓ event received"].slice(-18),
          })),
        },
        FAIL: {
          target: "error",
          actions: assign(({ event }) => ({ error: event.message })),
        },
        PAUSE: { target: "paused", actions: assign({ pausedFrom: "waiting" }) },
      },
    },
    paused: {
      on: {
        RESUME: [
          { guard: "wasWaiting", target: "waiting" },
          { target: "running" },
        ],
        RESET: "idle",
      },
    },
    success: {
      after: {
        1150: [
          {
            guard: "hasNextStep",
            target: "idle",
            actions: assign(({ context }) => ({
              step: context.step + 1,
              completed: [...new Set([...context.completed, context.step])],
              copied: false,
              selected: context.selected,
              event: null,
              progress: 0,
            })),
          },
          {
            target: "complete",
            actions: assign(({ context }) => ({
              completed: [...new Set([...context.completed, context.step])],
            })),
          },
        ],
      },
      on: { RESET: "idle" },
    },
    error: {
      on: {
        RETRY: {
          target: "running",
          actions: assign(({ context }) => ({
            attempt: context.attempt + 1,
            error: null,
            logs: [...context.logs, "→ retrying step"].slice(-18),
          })),
        },
        RESET: "idle",
      },
    },
    complete: {
      on: { RESET: "idle", REWIND: "idle" },
    },
  },
});

export const PROJECT_GUIDE_OUTPUT_LIMIT = 18;

export type ProjectGuideDisplayState =
  | "opening"
  | "ready"
  | "running"
  | "checkpoint"
  | "waiting"
  | "paused"
  | "error"
  | "complete"
  | "exited";

export type ProjectGuideOutputEntry = {
  id: number;
  kind: "start" | "note" | "next" | "result";
  message: string;
};

export type ProjectGuideCheckpoint = {
  step: number;
  label: string;
};

export type ProjectGuideOperationSignal =
  | { type: "start"; path: JourneyId; step: number }
  | { type: "pause"; path: JourneyId; step: number }
  | { type: "resume"; path: JourneyId; step: number }
  | {
      type: "retry";
      path: JourneyId;
      step: number;
      attempt: number;
    }
  | { type: "checkpoint"; path: JourneyId; step: number }
  | {
      type: "abort";
      path: JourneyId;
      step: number;
      reason: "switch" | "back" | "exit" | "rewind" | "reset";
    };

export type ProjectGuideEvent =
  | { type: "OPEN"; path: JourneyId; resumeStep: number }
  | { type: "SWITCH"; path: JourneyId; resumeStep: number }
  | { type: "BACK" }
  | { type: "START" }
  | { type: "PAUSE" }
  | { type: "RESUME" }
  | { type: "USER_CHECKPOINT_COMPLETE"; result: string }
  | { type: "OPERATION_PROGRESS"; message: string; progress?: number }
  | { type: "OPERATION_SUCCESS"; result: string }
  | { type: "OPERATION_ERROR"; message: string }
  | { type: "EVENT_RECEIVED"; event: ProjectGuideEventCard }
  | { type: "LISTEN_TICK"; elapsedSeconds: number }
  | { type: "RETRY" }
  | { type: "REWIND"; step: number }
  | { type: "EXIT" }
  | { type: "RESET" };

export type ProjectGuideMachineContext = {
  activePath: JourneyId | null;
  completedByPath: Record<JourneyId, number[]>;
  output: ProjectGuideOutputEntry[];
  checkpoint: ProjectGuideCheckpoint | null;
  elapsedListeningSeconds: number;
  operationProgress: number | null;
  error: string | null;
  observedEvent: ProjectGuideEventCard | null;
  attempt: number;
  pausedFrom: "running" | "waiting";
  errorFrom: "running" | "waiting";
  nextOutputId: number;
  listenTimeoutSeconds: number;
  onSignal?: (signal: ProjectGuideOperationSignal) => void;
};

export type ProjectGuideMachineInput = {
  listenTimeoutSeconds?: number;
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

const JOURNEY_BY_ID = Object.fromEntries(
  PROJECT_GUIDE_JOURNEYS.map((journey) => [journey.id, journey]),
) as Record<JourneyId, (typeof PROJECT_GUIDE_JOURNEYS)[number]>;

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
    checkpoint: null,
    elapsedListeningSeconds: 0,
    operationProgress: null,
    error: null,
    observedEvent: null,
    attempt: 0,
    pausedFrom: "running",
    errorFrom: "running",
    nextOutputId: 0,
    listenTimeoutSeconds: input.listenTimeoutSeconds ?? 60,
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
  return STEP_MODES[getProjectGuideCurrentStep(context) + offset] ?? "listen";
}

function stepLabel(context: ProjectGuideMachineContext, offset = 0): string {
  if (!context.activePath) return "Journey";
  return (
    JOURNEY_BY_ID[context.activePath].steps[
      getProjectGuideCurrentStep(context) + offset
    ] ?? "Journey complete"
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

function completedThrough(step: number, stepCount: number): number[] {
  return Array.from(
    { length: Math.min(Math.max(0, step), stepCount) },
    (_, index) => index,
  );
}

function emitCurrentSignal(
  context: ProjectGuideMachineContext,
  type: "start" | "pause" | "resume" | "checkpoint",
  offset = 0,
): void {
  if (!context.activePath) return;
  context.onSignal?.({
    type,
    path: context.activePath,
    step: getProjectGuideCurrentStep(context) + offset,
  });
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
    finalStep: ({ context }) =>
      currentPathIsComplete(context) ||
      Boolean(
        context.activePath &&
        getProjectGuideCurrentStep(context) ===
          pathStepCount(context.activePath) - 1,
      ),
    nextOperation: ({ context }) => stepMode(context, 1) === "operation",
    nextCheckpoint: ({ context }) => stepMode(context, 1) === "checkpoint",
    pausedWhileWaiting: ({ context }) => context.pausedFrom === "waiting",
    erroredWhileWaiting: ({ context }) => context.errorFrom === "waiting",
    listenTimedOut: ({ context, event }) =>
      event.type === "LISTEN_TICK" &&
      event.elapsedSeconds >= context.listenTimeoutSeconds,
  },
  actions: {
    openPath: assign(({ context, event }) => {
      if (event.type !== "OPEN" && event.type !== "SWITCH") return {};
      const completed = completedThrough(
        Math.max(context.completedByPath[event.path].length, event.resumeStep),
        pathStepCount(event.path),
      );
      const currentLabel =
        JOURNEY_BY_ID[event.path].steps[completed.length] ?? "Journey complete";
      const readyOutput = appendOutput({ ...context, output: [] }, [
        { kind: "note", message: `Ready · ${currentLabel}` },
      ]);
      return {
        activePath: event.path,
        completedByPath: {
          ...context.completedByPath,
          [event.path]: completed,
        },
        ...readyOutput,
        checkpoint: null,
        elapsedListeningSeconds: 0,
        operationProgress: null,
        error: null,
        observedEvent: null,
        attempt: 0,
      };
    }),
    recordStart: assign(({ context }) => ({
      ...appendOutput(context, [
        { kind: "start", message: `Started · ${stepLabel(context)}` },
      ]),
      checkpoint:
        stepMode(context) === "checkpoint"
          ? {
              step: getProjectGuideCurrentStep(context),
              label: stepLabel(context),
            }
          : null,
      error: null,
      operationProgress: 0,
    })),
    recordProgress: assign(({ context, event }) => {
      if (event.type !== "OPERATION_PROGRESS") return {};
      const progress = event.progress;
      return {
        ...appendOutput(context, [{ kind: "note", message: event.message }]),
        operationProgress:
          progress === undefined
            ? context.operationProgress
            : Math.min(1, Math.max(0, progress)),
      };
    }),
    recordSuccessAndAdvance: assign(({ context, event }) => {
      if (
        event.type !== "OPERATION_SUCCESS" &&
        event.type !== "USER_CHECKPOINT_COMPLETE"
      ) {
        return {};
      }
      if (!context.activePath) return {};
      const nextStep = getProjectGuideCurrentStep(context) + 1;
      const nextLabel = JOURNEY_BY_ID[context.activePath].steps[nextStep];
      const entries: Array<Omit<ProjectGuideOutputEntry, "id">> = [
        { kind: "result", message: event.result },
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
        checkpoint:
          STEP_MODES[nextStep] === "checkpoint"
            ? { step: nextStep, label: nextLabel ?? "Checkpoint" }
            : null,
        elapsedListeningSeconds: 0,
        operationProgress: null,
        error: null,
      };
    }),
    recordEventAndComplete: assign(({ context, event }) => {
      if (event.type !== "EVENT_RECEIVED" || !context.activePath) return {};
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
            message: `Event received · ${event.event.title}`,
          },
        ]),
        observedEvent: event.event,
        checkpoint: null,
        operationProgress: null,
        error: null,
      };
    }),
    recordError: assign(({ context, event }) => {
      if (event.type !== "OPERATION_ERROR") return {};
      return {
        ...appendOutput(context, [
          { kind: "note", message: `Error · ${event.message}` },
        ]),
        error: event.message,
      };
    }),
    recordTimeout: assign(({ context, event }) => {
      if (event.type !== "LISTEN_TICK") return {};
      const message = `No event seen in ${context.listenTimeoutSeconds}s. Check the client, then listen again.`;
      return {
        ...appendOutput(context, [{ kind: "note", message }]),
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
        checkpoint: null,
        elapsedListeningSeconds: 0,
        operationProgress: null,
        error: null,
        observedEvent: null,
      };
    }),
    clearActivePath: assign({
      activePath: null,
      output: [],
      checkpoint: null,
      elapsedListeningSeconds: 0,
      operationProgress: null,
      error: null,
      observedEvent: null,
    }),
    reset: assign(({ context }) =>
      initialCoordinatorContext({
        listenTimeoutSeconds: context.listenTimeoutSeconds,
        onSignal: context.onSignal,
      }),
    ),
    signalStart: ({ context }) => emitCurrentSignal(context, "start"),
    signalNextStart: ({ context }) => emitCurrentSignal(context, "start", 1),
    signalPause: ({ context }) => emitCurrentSignal(context, "pause"),
    signalResume: ({ context }) => emitCurrentSignal(context, "resume"),
    signalCheckpoint: ({ context }) => emitCurrentSignal(context, "checkpoint"),
    signalRetry: ({ context }) => {
      if (!context.activePath) return;
      context.onSignal?.({
        type: "retry",
        path: context.activePath,
        step: getProjectGuideCurrentStep(context),
        attempt: context.attempt + 1,
      });
    },
    signalAbort: ({ context, event }) => {
      if (!context.activePath) return;
      const reason =
        event.type === "SWITCH"
          ? "switch"
          : event.type === "BACK"
            ? "back"
            : event.type === "EXIT"
              ? "exit"
              : event.type === "REWIND"
                ? "rewind"
                : "reset";
      context.onSignal?.({
        type: "abort",
        path: context.activePath,
        step: getProjectGuideCurrentStep(context),
        reason,
      });
    },
  },
}).createMachine({
  id: "projectGuideCoordinator",
  initial: "opening",
  context: ({ input }) => initialCoordinatorContext(input),
  on: {
    SWITCH: [
      {
        guard: "selectedPathComplete",
        target: "#projectGuideCoordinator.complete",
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
    EXIT: {
      target: "#projectGuideCoordinator.exited",
      actions: ["signalAbort", "clearActivePath"],
    },
    RESET: {
      target: "#projectGuideCoordinator.opening",
      actions: ["signalAbort", "reset"],
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
          { target: "ready", actions: "openPath" },
        ],
      },
    },
    ready: {
      on: {
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
        OPERATION_PROGRESS: { actions: "recordProgress" },
        OPERATION_SUCCESS: [
          {
            guard: "finalStep",
            target: "complete",
            actions: "recordSuccessAndAdvance",
          },
          {
            guard: "nextOperation",
            target: "running",
            actions: ["signalNextStart", "recordSuccessAndAdvance"],
          },
          {
            guard: "nextCheckpoint",
            target: "checkpoint",
            actions: "recordSuccessAndAdvance",
          },
          {
            target: "waiting",
            actions: ["signalNextStart", "recordSuccessAndAdvance"],
          },
        ],
        OPERATION_ERROR: {
          target: "error",
          actions: ["rememberRunningError", "recordError"],
        },
        PAUSE: {
          target: "paused",
          actions: ["rememberRunningPause", "signalPause"],
        },
      },
    },
    checkpoint: {
      on: {
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
              "signalNextStart",
              "recordSuccessAndAdvance",
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
              "signalNextStart",
              "recordSuccessAndAdvance",
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
        EVENT_RECEIVED: {
          target: "complete",
          actions: "recordEventAndComplete",
        },
        OPERATION_ERROR: {
          target: "error",
          actions: ["rememberWaitingError", "recordError"],
        },
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
        RETRY: [
          {
            guard: "erroredWhileWaiting",
            target: "waiting",
            actions: ["signalRetry", "retry"],
          },
          {
            target: "running",
            actions: ["signalRetry", "retry"],
          },
        ],
      },
    },
    complete: {},
    exited: {},
  },
});
