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

export type ProjectGuideEvent =
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

export type ProjectGuideMachineContext = {
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

export type ProjectGuideMachineInput = {
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

export const projectGuideMachine = setup({
  types: {
    context: {} as ProjectGuideMachineContext,
    events: {} as ProjectGuideEvent,
    input: {} as ProjectGuideMachineInput,
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
