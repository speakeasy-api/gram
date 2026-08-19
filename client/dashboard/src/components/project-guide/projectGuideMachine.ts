import { assign, fromPromise, setup } from "xstate";

export type ProjectGuidePhase =
  | "idle"
  | "await"
  | "running"
  | "waiting"
  | "success"
  | "error"
  | "complete";

export type ProjectGuideEvent =
  | { type: "START" }
  | { type: "PAUSE" }
  | { type: "RESUME" }
  | { type: "RETRY" }
  | { type: "COPY" }
  | { type: "CONFIRM" }
  | { type: "SELECT"; value: string }
  | { type: "STEP_SUCCESS"; result: string }
  | { type: "EVENT_RECEIVED"; event: ProjectGuideEventCard }
  | { type: "LISTEN" }
  | { type: "TICK" }
  | { type: "FAIL"; message: string }
  | { type: "REWIND"; step: number }
  | { type: "SYNC"; step: number }
  | { type: "RESET" };

export type ProjectGuideEventCard = {
  kind: string;
  tone: "allow" | "deny";
  title: string;
  rows: Array<{ key: string; value: string }>;
  note: string;
};

export type ProjectGuideMachineContext = {
  step: number;
  completed: number[];
  phase: ProjectGuidePhase;
  logs: string[];
  selected: string | null;
  copied: boolean;
  elapsed: number;
  progress: number;
  event: ProjectGuideEventCard | null;
  error: string | null;
  attempt: number;
};

export type ProjectGuideMachineInput = {
  initialStep?: number;
  completed?: number[];
};

const placeholderActor = fromPromise<void, { duration: number }>(
  async ({ input }) => {
    await new Promise<void>((resolve) => {
      setTimeout(resolve, input.duration);
    });
  },
);

export const projectGuideMachine = setup({
  types: {
    context: {} as ProjectGuideMachineContext,
    events: {} as ProjectGuideEvent,
    input: {} as ProjectGuideMachineInput,
  },
  actors: { stepTimer: placeholderActor },
  guards: {
    hasSelection: ({ context }) => context.selected !== null,
    hasCopied: ({ context }) => context.copied,
    hasNextStep: ({ context }) => context.step < 4,
  },
}).createMachine({
  id: "projectGuide",
  initial: "idle",
  context: ({ input }) => {
    const completed = input.completed ?? [];
    return {
      step: input.initialStep ?? completed.length,
      completed,
      phase: "idle",
      logs: [],
      selected: null,
      copied: false,
      elapsed: 0,
      progress: 0,
      event: null,
      error: null,
      attempt: 0,
    };
  },
  states: {
    idle: {
      entry: assign({ phase: "idle", error: null }),
      on: {
        SYNC: {
          actions: assign({
            step: ({ event }) => event.step,
            completed: ({ context, event }) => [
              ...new Set([
                ...context.completed,
                ...Array.from({ length: event.step }, (_, i) => i),
              ]),
            ],
            phase: "idle",
          }),
        },
        START: "await",
        LISTEN: {
          target: "waiting",
          actions: assign({ elapsed: 0 }),
        },
        SELECT: {
          target: "running",
          actions: assign(({ event }) => ({
            selected: event.value,
            logs: ["▸ selected · " + event.value],
          })),
        },
        REWIND: {
          actions: assign({
            step: ({ event }) => event.step,
            completed: ({ context, event }) =>
              context.completed.filter((n) => n < event.step),
            phase: "idle",
            logs: [],
            copied: false,
            event: null,
          }),
        },
        RESET: {
          actions: assign({
            step: 0,
            completed: [],
            logs: [],
            selected: null,
            copied: false,
            event: null,
            progress: 0,
          }),
        },
      },
    },
    await: {
      entry: assign({ phase: "await" }),
      on: {
        SYNC: {
          actions: assign({
            step: ({ event }) => event.step,
            completed: ({ context, event }) => [
              ...new Set([
                ...context.completed,
                ...Array.from({ length: event.step }, (_, i) => i),
              ]),
            ],
          }),
        },
        SELECT: {
          target: "running",
          actions: assign(({ event }) => ({
            selected: event.value,
            logs: ["▸ selected · " + event.value],
          })),
        },
        COPY: { actions: assign({ copied: true }) },
        CONFIRM: "running",
        LISTEN: {
          target: "waiting",
          actions: assign({ elapsed: 0 }),
        },
        PAUSE: "paused",
        RESET: "idle",
      },
    },
    running: {
      entry: assign({ phase: "running", error: null }),
      invoke: {
        src: "stepTimer",
        input: () => ({ duration: 900 }),
        onDone: "success",
        onError: {
          target: "error",
          actions: assign({ error: "The step could not complete." }),
        },
      },
      on: {
        STEP_SUCCESS: {
          target: "success",
          actions: assign({
            logs: ({ context, event }) =>
              [...context.logs, "✓ " + event.result].slice(-18),
          }),
        },
        LISTEN: {
          target: "waiting",
          actions: assign({ elapsed: 0 }),
        },
        EVENT_RECEIVED: {
          target: "success",
          actions: assign({
            event: ({ event }) => event.event,
            logs: ({ context }) =>
              [...context.logs, "✓ event received"].slice(-18),
          }),
        },
        FAIL: {
          target: "error",
          actions: assign(({ event }) => ({ error: event.message })),
        },
        PAUSE: "paused",
      },
    },
    waiting: {
      entry: assign({ phase: "waiting" }),
      on: {
        TICK: {
          actions: assign({
            elapsed: ({ context }) => Math.min(context.elapsed + 0.1, 99.9),
          }),
        },
        EVENT_RECEIVED: {
          target: "success",
          actions: assign({
            event: ({ event }) => event.event,
            logs: ({ context }) =>
              [...context.logs, "✓ event received"].slice(-18),
          }),
        },
        FAIL: {
          target: "error",
          actions: assign(({ event }) => ({ error: event.message })),
        },
        PAUSE: "paused",
      },
    },
    paused: {
      entry: assign({ phase: "idle" }),
      on: { RESUME: "running", RESET: "idle" },
    },
    success: {
      entry: assign({ phase: "success", progress: 1 }),
      after: {
        1150: [
          {
            guard: "hasNextStep",
            target: "idle",
            actions: assign(({ context }) => ({
              step: context.step + 1,
              completed: [...new Set([...context.completed, context.step])],
              copied: false,
              selected: null,
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
      entry: assign({ phase: "error" }),
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
      entry: assign({ phase: "complete" }),
      on: { RESET: "idle", REWIND: "idle" },
    },
  },
});
