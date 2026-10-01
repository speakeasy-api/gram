import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useLocation, useSearchParams } from "react-router";
import { specProblem, type ExploreSpec } from "./exploreModel";
import {
  encodeSpec,
  parseSpec,
  QUERY_PARAM,
  TAB_PARAM,
  WIDGET_PARAM,
} from "./exploreUrl";

// What a history entry holds, kept in its state rather than its URL so a
// link someone shares carries the query and nothing about how it was made.
// "draft" is a query being composed; "ran" is one that ran and returned. An
// entry with neither arrived from outside — a pasted link, a reload, a widget
// opened — and is opened by running it.
type EntryState = { explore?: "draft" | "ran" } | null;

/**
 * The builder's query, kept in the URL. Edits replace the current history
 * entry, so composing a query costs no history; once a query has run and
 * returned, the next edit starts a new entry instead, leaving that question
 * behind it. Back then steps through questions someone looked at rather than
 * every keystroke.
 */
export function useQueryUrl(
  datasets: AnalyticsDataset[],
  onOpen: (spec: ExploreSpec) => void,
): {
  /** The query the URL carries, or null for the default view. */
  spec: ExploreSpec | null;
  /**
   * A query the URL carries that the catalog can no longer answer, and what
   * it names that is gone. It is not run.
   */
  stale: { spec: ExploreSpec; problem: string } | null;
  /** The widget the builder has open, if any. */
  widgetId: string | null;
  /** Record an edit to the query. */
  edit: (next: ExploreSpec) => void;
  /** Record that a query ran and returned. */
  ran: (spec: ExploreSpec) => void;
  /**
   * Open a widget in the builder as a new entry, switching to the Explore
   * tab; a null spec keeps the builder's.
   */
  open: (spec: ExploreSpec | null, widgetId: string) => void;
  /** Change which widget the builder has open, in place. */
  setWidgetId: (widgetId: string | null) => void;
} {
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const entry = (location.state as EntryState)?.explore;

  const raw = params.get(QUERY_PARAM);
  const widgetId = params.get(WIDGET_PARAM);
  // Memoized on the text, so the same query keeps the same reference until
  // an edit changes it.
  const { spec, stale } = useMemo(() => {
    const parsed = parseSpec(raw);
    const problem = parsed ? specProblem(datasets, parsed) : "";
    return parsed && problem !== ""
      ? { spec: null, stale: { spec: parsed, problem } }
      : { spec: parsed, stale: null };
  }, [raw, datasets]);
  const current = spec ?? stale?.spec ?? null;

  const write = useCallback(
    (next: ExploreSpec, state: "draft" | "ran", replace: boolean) => {
      setParams(
        (prev) => {
          const out = new URLSearchParams(prev);
          out.set(QUERY_PARAM, encodeSpec(next));
          return out;
        },
        { replace, state: { explore: state } },
      );
    },
    [setParams],
  );

  const edit = useCallback(
    (next: ExploreSpec) => write(next, "draft", entry !== "ran"),
    [write, entry],
  );

  const ran = useCallback(
    (answered: ExploreSpec) => {
      // A query edited while it was in flight is no longer the one in the
      // URL, so the answer arriving does not claim the entry.
      const showing = current === null ? null : encodeSpec(current);
      const target = encodeSpec(answered);
      if (showing !== null && showing !== target) return;
      if (entry === "ran" && showing === target) return;
      write(answered, "ran", true);
    },
    [current, entry, write],
  );

  const open = useCallback(
    (next: ExploreSpec | null, id: string) => {
      setParams((prev) => {
        const out = new URLSearchParams(prev);
        if (next) out.set(QUERY_PARAM, encodeSpec(next));
        out.set(WIDGET_PARAM, id);
        out.delete(TAB_PARAM);
        return out;
      });
    },
    [setParams],
  );

  const setWidgetId = useCallback(
    (id: string | null) => {
      setParams(
        (prev) => {
          const out = new URLSearchParams(prev);
          if (id === null) out.delete(WIDGET_PARAM);
          else out.set(WIDGET_PARAM, id);
          return out;
        },
        { replace: true, state: location.state as EntryState },
      );
    },
    [setParams, location.state],
  );

  // An entry arriving from outside or from back and forward is opened by
  // running it; a draft is shown as it was left, and a query the catalog can
  // no longer answer is shown but never run.
  const openRef = useRef(onOpen);
  openRef.current = onOpen;
  useEffect(() => {
    if (entry !== "draft" && spec !== null) openRef.current(spec);
    // Keyed on the entry: a render of the same entry is not an arrival. A
    // link that only resolves once a refetched catalog names what it asks
    // for arrives then, so that counts too.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.key, spec === null]);

  return { spec, stale, widgetId, edit, ran, open, setWidgetId };
}
