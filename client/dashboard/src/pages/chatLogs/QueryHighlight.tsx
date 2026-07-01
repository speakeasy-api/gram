import { type ReactNode, useEffect, useMemo, useRef } from "react";
import { findQueryRanges } from "./transcript";

/** Browser-find style yellow wash for a search-query hit. The single active
 * occurrence (the one the global navigator is on) is bright; every other
 * occurrence is pale so the current one stands out. (Search and risk are
 * mutually exclusive modes, so sharing the yellow family is unambiguous.) */
const SEARCH_MARK_ACTIVE =
  "rounded-sm bg-yellow-300/70 px-0.5 text-foreground ring-1 ring-yellow-500/40";
const SEARCH_MARK_INACTIVE =
  "rounded-sm bg-yellow-200/30 px-0.5 text-foreground ring-1 ring-yellow-400/20";

/** Renders `text` with every case-insensitive occurrence of `query` wrapped in a
 * search highlight (original casing preserved; ILIKE-equivalent so "foo" lights
 * up "Foo"/"FOO"). Exactly the occurrence at `activeIndex` is bright and scrolls
 * into view — it's the unified navigator's current target within this field;
 * `activeIndex` is null when this field doesn't hold the active occurrence. */
export function QueryHighlight({
  text,
  query,
  activeIndex,
}: {
  text: string;
  query: string;
  activeIndex: number | null;
}): ReactNode {
  const activeRef = useRef<HTMLElement | null>(null);
  // Bring the active occurrence into view when navigation lands on it (or steps
  // within this field). `block: "nearest"` is a no-op when it's already visible,
  // so it doesn't fight the transcript's row-level centering for short messages.
  useEffect(() => {
    if (activeIndex == null) return;
    activeRef.current?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);

  const ranges = useMemo(() => findQueryRanges(text, query), [text, query]);
  if (ranges.length === 0) return text;

  const nodes: ReactNode[] = [];
  let pos = 0;
  ranges.forEach((r, i) => {
    if (r.start > pos) nodes.push(text.slice(pos, r.start));
    const isActive = i === activeIndex;
    nodes.push(
      <mark
        key={i}
        ref={isActive ? activeRef : undefined}
        className={isActive ? SEARCH_MARK_ACTIVE : SEARCH_MARK_INACTIVE}
      >
        {text.slice(r.start, r.end)}
      </mark>,
    );
    pos = r.end;
  });
  if (pos < text.length) nodes.push(text.slice(pos));
  return nodes;
}
