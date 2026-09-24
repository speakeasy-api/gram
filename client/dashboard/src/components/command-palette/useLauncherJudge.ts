import type { LauncherCandidate as JudgeCandidate } from "@gram/client/models/components/launchercandidate.js";
import type { LauncherJudgment } from "@gram/client/models/components/launcherjudgment.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildLauncherJudgeMutation } from "@gram/client/react-query/launcherJudge.js";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  isSendable,
  type Judgment,
  type LauncherCandidate,
} from "./candidates/types";

export interface JudgeState {
  /** The most recent applied judgment, or null while in fuzzy order. */
  judgment: Judgment | null;
  /** True when `judgment` answers the latest keystroke; false while dimmed. */
  fresh: boolean;
  inFlight: boolean;
  latencyMs: number | null;
  /**
   * Latched once the server reports no intent service, for the scope the hook
   * was asked about; a different organization or project asks again.
   */
  disabled: boolean;
}

const IDLE: JudgeState = {
  judgment: null,
  fresh: false,
  inFlight: false,
  latencyMs: null,
  disabled: false,
};

/**
 * The stored state plus the scope it was written under. Only the initial
 * state and the scope-change effect write `scope`; every other update keeps
 * it, so a stored state always names the tenant its judgment was made for.
 */
interface ScopedState extends JudgeState {
  scope: string;
}

function idleFor(scope: string): ScopedState {
  return { ...IDLE, scope };
}

// Length limits the `launcher.judge` design enforces (MaxLength in
// server/design/launcher/design.go). Goa counts runes, so the clamp counts
// code points: one overlong field would otherwise fail the whole request at
// decode and drop the palette back to fuzzy order for that keystroke.
const MAX_QUERY_LENGTH = 200;
const MAX_TITLE_LENGTH = 120;
const MAX_DETAIL_LENGTH = 160;

function clamp(text: string, max: number): string {
  return Array.from(text).slice(0, max).join("");
}

/** People are fuzzy-only: their names never leave the tenant. */
function toJudgeCandidates(sent: LauncherCandidate[]): JudgeCandidate[] {
  return sent.filter(isSendable).map(({ id, kind, title, detail, verbs }) => ({
    id,
    kind,
    title: clamp(title, MAX_TITLE_LENGTH),
    detail: clamp(detail, MAX_DETAIL_LENGTH),
    verbs,
  }));
}

function toJudgment(result: LauncherJudgment): Judgment {
  return {
    target: result.target ?? {},
    action: result.action ?? { unclear: 1 },
    ready: result.ready ?? 0,
  };
}

/**
 * Drives `launcher.judge` for the command palette. Every call aborts the
 * previous request; answers apply only when newer than the newest one already
 * applied, so a slow early keystroke can never overwrite a later one.
 *
 * `scopeKey` names the tenant the calls are made for (organization and
 * project). The intent service is provisioned per tenant, so "no service" is
 * latched for that key alone: when the key changes, the latch and any
 * judgment made under the old key are dropped and the next keystroke asks
 * again.
 */
export function useLauncherJudge(scopeKey: string): {
  state: JudgeState;
  judge: (query: string, sent: LauncherCandidate[], route?: string) => void;
  reset: () => void;
} {
  // Called directly rather than through react-query's mutation cache: the
  // palette aborts the previous request on every keystroke, and the
  // dashboard's global mutation handler would toast each cancellation as a
  // failed request. Judging is a read, not a mutation.
  const client = useGramContext();
  const mutateAsync = useMemo(
    () => buildLauncherJudgeMutation(client).mutationFn,
    [client],
  );
  const [stored, setState] = useState<ScopedState>(() => idleFor(scopeKey));
  // Derived, not effect-driven: the reset effect below runs after paint, so
  // the very first render for a new tenant would otherwise still rank rows
  // and pick a highlight by the previous tenant's judgment.
  const state: JudgeState = stored.scope === scopeKey ? stored : IDLE;

  // Refs, not state: a re-render must never rewind the sequence.
  const sequence = useRef(0);
  const newestApplied = useRef(0);
  const controller = useRef<AbortController | null>(null);
  // The scope the server reported no intent service for, if any.
  const disabledScope = useRef<string | null>(null);
  const scope = useRef(scopeKey);

  const abortInFlight = useCallback(() => {
    controller.current?.abort();
    controller.current = null;
  }, []);

  // A new tenant starts clean: nothing latched, nothing judged, nothing in
  // flight from the previous one allowed to land.
  useEffect(() => {
    if (scope.current === scopeKey) return;
    scope.current = scopeKey;
    disabledScope.current = null;
    sequence.current += 1;
    newestApplied.current = sequence.current;
    abortInFlight();
    setState(idleFor(scopeKey));
  }, [scopeKey, abortInFlight]);

  const judge = useCallback(
    (query: string, sent: LauncherCandidate[], route?: string) => {
      if (disabledScope.current === scopeKey) return;

      const seq = ++sequence.current;
      abortInFlight();

      const candidates = toJudgeCandidates(sent);
      if (query.trim() === "" || candidates.length === 0) {
        // Nothing to ask; make sure no older answer lands on this keystroke.
        newestApplied.current = seq;
        setState((prev) => ({
          ...prev,
          judgment: null,
          fresh: false,
          inFlight: false,
          latencyMs: null,
        }));
        return;
      }

      const abort = new AbortController();
      controller.current = abort;
      setState((prev) => ({ ...prev, fresh: false, inFlight: true }));

      mutateAsync({
        request: {
          judgeRequestBody: {
            query: clamp(query, MAX_QUERY_LENGTH),
            candidates,
            // Jev uses the current route only to break ties between candidates.
            ...(route ? { context: { route } } : {}),
          },
        },
        options: { fetchOptions: { signal: abort.signal } },
      })
        .then((result) => {
          if (seq <= newestApplied.current) return; // stale: dropped
          newestApplied.current = seq;
          const current = seq === sequence.current;

          if (result.disabled) {
            disabledScope.current = scopeKey;
            abortInFlight();
            setState((prev) => ({ ...idleFor(prev.scope), disabled: true }));
            return;
          }

          setState((prev) => ({
            ...prev,
            judgment: toJudgment(result),
            fresh: current,
            inFlight: !current,
            latencyMs: result.latencyMs ?? null,
          }));
        })
        .catch(() => {
          // Aborted or failed. Only the latest keystroke falls back to fuzzy;
          // a newer request in flight keeps the dimmed judgment it inherited.
          if (seq !== sequence.current) return;
          if (seq <= newestApplied.current) return;
          newestApplied.current = seq;
          setState((prev) => ({
            ...prev,
            judgment: null,
            fresh: false,
            inFlight: false,
            latencyMs: null,
          }));
        });
    },
    [abortInFlight, mutateAsync, scopeKey],
  );

  const reset = useCallback(() => {
    sequence.current += 1;
    newestApplied.current = sequence.current;
    abortInFlight();
    setState((prev) => ({ ...idleFor(prev.scope), disabled: prev.disabled }));
  }, [abortInFlight]);

  return { state, judge, reset };
}
