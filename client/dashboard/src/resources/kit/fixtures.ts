import { useSyncExternalStore } from "react";
import type {
  ActionHandlers,
  Query,
  ResourceData,
  ResourceDefinition,
} from "./types";

export type FixtureState = "ready" | "loading" | "error" | "empty";

/**
 * A data layer backed by in-memory fixtures, for Storybook and tests. Every
 * action the definition declares gets a handler that shows a short pending
 * state and then applies `apply`, so stories respond like the real thing
 * without a server.
 */
export function fixtureData<Row, Rec>({
  definition,
  rows,
  records,
  recordFor,
  state = "ready",
  readOnly = false,
  apply,
}: {
  definition: ResourceDefinition<Row, Rec>;
  rows: Row[];
  /** Full records keyed by id. Missing ids fall back to `recordFor`. */
  records?: Record<string, Rec>;
  recordFor?: (id: string) => Rec | undefined;
  state?: FixtureState;
  /** The viewer lacks write access: every action handler is withheld. */
  readOnly?: boolean;
  /** How an action changes a record, so stories react to clicks. */
  apply?: (rec: Rec, actionId: string, input?: string) => Rec;
}): ResourceData<Row, Rec> {
  const overrides = new Map<string, Rec>();
  const pending = new Set<string>();
  const listeners = new Set<() => void>();
  let version = 0;
  const emit = () => {
    version++;
    listeners.forEach((l) => l());
  };
  const subscribe = (l: () => void) => {
    listeners.add(l);
    return () => listeners.delete(l);
  };
  const useVersion = () => useSyncExternalStore(subscribe, () => version);

  const lookup = (id: string): Rec | undefined =>
    overrides.get(id) ?? records?.[id] ?? recordFor?.(id);

  const errorQuery = <T>(): Query<T> => ({
    status: "error",
    message: "The server didn't respond. Check your connection and try again.",
    retry: () => {},
  });

  const handlersFor = (key: string, rec: Rec | undefined): ActionHandlers => {
    if (readOnly) return {};
    const out: ActionHandlers = {};
    for (const action of definition.actions) {
      const pendingKey = `${key}:${action.id}`;
      out[action.id] = {
        pending: pending.has(pendingKey),
        run: (input) => {
          pending.add(pendingKey);
          emit();
          setTimeout(() => {
            pending.delete(pendingKey);
            if (rec && apply) {
              const id = recordKey(rec);
              if (id) overrides.set(id, apply(rec, action.id, input));
            }
            emit();
          }, 600);
        },
      };
    }
    return out;
  };

  // Records carry their id under `id` in every fixture we write.
  const recordKey = (rec: Rec): string | undefined =>
    (rec as { id?: string }).id;

  return {
    useList: () => {
      useVersion();
      if (state === "loading") return { status: "loading" };
      if (state === "error") return errorQuery();
      return { status: "ready", data: state === "empty" ? [] : rows };
    },
    useRecord: (id) => {
      useVersion();
      if (state === "loading") return { status: "loading" };
      if (state === "error") return errorQuery();
      const rec = lookup(id);
      return rec
        ? { status: "ready", data: rec }
        : {
            status: "error",
            message: "It may have been removed.",
            retry: () => {},
          };
    },
    useListActions: () => {
      useVersion();
      return handlersFor("list", undefined);
    },
    useRecordActions: (rec) => {
      useVersion();
      return handlersFor(rec ? (recordKey(rec) ?? "record") : "none", rec);
    },
  };
}
