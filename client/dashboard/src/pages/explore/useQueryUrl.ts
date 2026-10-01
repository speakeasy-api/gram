import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useLocation, useSearchParams } from "react-router";
import type { ExploreSpec } from "./exploreModel";
import { decodeSpec, encodeSpec, QUERY_PARAM } from "./exploreUrl";

// What a history entry holds, kept in its state rather than its URL so a
// link someone shares carries the query and nothing about how it was made.
// "draft" is a query being composed; "ran" is one that ran and returned. An
// entry with neither arrived from outside — a pasted link, a reload — and is
// opened by running it.
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
  /** Record an edit to the query. */
  edit: (next: ExploreSpec) => void;
  /** Record that a query ran and returned. */
  ran: (spec: ExploreSpec) => void;
} {
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const entry = (location.state as EntryState)?.explore;

  const raw = params.get(QUERY_PARAM);
  // Memoized on the text, so the same query keeps the same reference until
  // an edit changes it.
  const spec = useMemo(() => decodeSpec(raw, datasets), [raw, datasets]);

  const write = useCallback(
    (next: ExploreSpec, state: "draft" | "ran", replace: boolean) => {
      setParams(
        (current) => {
          const out = new URLSearchParams(current);
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
      const current = spec === null ? null : encodeSpec(spec);
      const target = encodeSpec(answered);
      if (current !== null && current !== target) return;
      if (entry === "ran" && current === target) return;
      write(answered, "ran", true);
    },
    [spec, entry, write],
  );

  // An entry arriving from outside or from back and forward is opened by
  // running it; a draft is shown as it was left.
  const openRef = useRef(onOpen);
  openRef.current = onOpen;
  useEffect(() => {
    if (entry !== "draft" && spec !== null) openRef.current(spec);
    // Keyed on the entry: a render of the same entry is not an arrival.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.key]);

  return { spec, edit, ran };
}
