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
  rowFor,
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
  /** Rebuilds a list row from an edited record, so the list shows edits. */
  rowFor?: (rec: Rec) => Row;
}): ResourceData<Row, Rec> {
  const overrides = new Map<string, Rec>();
  // The id each served record was looked up by, so actions write back under
  // that id whatever shape the record has.
  const idOf = new Map<Rec, string>();
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

  /** `id` is the record's lookup id, or undefined for list actions. */
  const handlersFor = (key: string, id?: string): ActionHandlers => {
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
            // Apply to the latest copy, so quick successive edits stack.
            const latest = id != null ? lookup(id) : undefined;
            if (id != null && latest && apply) {
              overrides.set(id, apply(latest, action.id, input));
            }
            emit();
          }, 600);
        },
      };
    }
    return out;
  };

  return {
    useList: () => {
      useVersion();
      if (state === "loading") return { status: "loading" };
      if (state === "error") return errorQuery();
      if (state === "empty") return { status: "ready", data: [] };
      const data = rowFor
        ? rows.map((row) => {
            const edited = overrides.get(definition.list.rowId(row));
            return edited ? rowFor(edited) : row;
          })
        : rows;
      return { status: "ready", data };
    },
    useRecord: (id) => {
      useVersion();
      if (state === "loading") return { status: "loading" };
      if (state === "error") return errorQuery();
      const rec = lookup(id);
      if (rec) idOf.set(rec, id);
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
      return handlersFor("list");
    },
    useRecordActions: (rec) => {
      useVersion();
      const id = rec ? idOf.get(rec) : undefined;
      return handlersFor(id ?? "none", id);
    },
  };
}
