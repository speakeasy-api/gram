import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  defaultSelection,
  eventOutcome,
  groupTurns,
  matchesFilter,
  secretSinceEvent,
  stepEvent,
  turnNumbers,
  type EventFilter,
  type SessionEvent,
} from "./events";
import { Inspector, type AnalysisState } from "./Inspector";
import { SessionTimeline, TimelineLegend } from "./SessionTimeline";
import { Transcript } from "./Transcript";

/** A request from another view to select a call; a new object re-applies it. */
export type ReplayFocus = { key: string };

/** One card: timeline strip, transcript window and inspector over one selection. */
export function SessionViewer({
  events,
  guardrail,
  analysisState,
  focus,
}: {
  events: SessionEvent[];
  guardrail: IntentGuardrail | undefined;
  analysisState: AnalysisState;
  focus?: ReplayFocus | null;
}): JSX.Element {
  const [selected, setSelected] = useState(() => defaultSelection(events));
  const [filter, setFilter] = useState<EventFilter>("all");
  const callEvents = events.filter((e) => e.kind === "tool_call");
  const groups = useMemo(() => groupTurns(events), [events]);
  const turns = useMemo(() => turnNumbers(events, groups), [events, groups]);
  const secret = useMemo(() => secretSinceEvent(events), [events]);
  const root = useRef<HTMLElement>(null);
  const appliedFocus = useRef<ReplayFocus | null>(null);

  const visible = events.flatMap((e, i) =>
    matchesFilter(e, filter) ? [i] : [],
  );
  const reviewCount = events.filter((e) => matchesFilter(e, "review")).length;
  const position = visible.indexOf(selected);

  const step = (dir: 1 | -1) => {
    const next = stepEvent(events, selected, dir, filter);
    if (next !== undefined) setSelected(next);
  };

  const changeFilter = (next: EventFilter) => {
    setFilter(next);
    const current = events[selected];
    if (current && !matchesFilter(current, next)) {
      const first = events.findIndex((e) => matchesFilter(e, next));
      if (first !== -1) setSelected(first);
    }
  };

  // Applied once per request, so later batches don't pull the selection back.
  useEffect(() => {
    if (!focus || appliedFocus.current === focus) return;
    const index = events.findIndex((e) => e.key === focus.key);
    if (index === -1) return;
    appliedFocus.current = focus;
    setSelected(index);
  }, [focus, events]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      // The Replay tab stays mounted while hidden.
      if (!root.current?.offsetParent) return;
      const t = e.target as HTMLElement | null;
      if (
        t &&
        (t.isContentEditable ||
          ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))
      ) {
        return;
      }
      const dir =
        e.key === "ArrowDown" || e.key === "j"
          ? 1
          : e.key === "ArrowUp" || e.key === "k"
            ? -1
            : 0;
      if (!dir) return;
      const next = stepEvent(events, selected, dir, filter);
      if (next === undefined) return;
      e.preventDefault();
      setSelected(next);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [events, selected, filter]);

  const first = events[0]?.occurredAt;
  const last = events[events.length - 1]?.occurredAt;

  return (
    <section
      ref={root}
      aria-labelledby="session-viewer"
      className="@container border bg-card"
    >
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
        <div className="flex flex-wrap items-baseline gap-3">
          <h2 id="session-viewer" className="text-eyebrow">
            Session
          </h2>
          <span className="text-xs text-muted-foreground">
            {events.length} events · {callEvents.length} tool calls
            {first && last
              ? ` · ${first.toLocaleTimeString()}–${last.toLocaleTimeString()}`
              : ""}
          </span>
        </div>
        <div className="ml-auto flex items-center gap-2 whitespace-nowrap">
          <SegmentedControl
            ariaLabel="Show"
            value={filter}
            onChange={changeFilter}
            options={[
              { value: "all", label: "All" },
              { value: "calls", label: "Tool calls" },
              {
                value: "review",
                label: `Needs review · ${reviewCount}`,
                disabled: reviewCount === 0,
              },
            ]}
          />
          <Button
            variant="secondary"
            className="size-10 p-0"
            aria-label="Previous event"
            onClick={() => step(-1)}
          >
            <ChevronLeft />
          </Button>
          <span className="min-w-16 text-center font-mono text-xs tabular-nums">
            {position === -1 ? "–" : position + 1} / {visible.length}
          </span>
          <Button
            variant="secondary"
            className="size-10 p-0"
            aria-label="Next event"
            onClick={() => step(1)}
          >
            <ChevronRight />
          </Button>
        </div>
      </div>

      <SessionTimeline
        events={events}
        groups={groups}
        selected={selected}
        onSelect={setSelected}
        filter={filter}
        secret={secret}
      />

      <TimelineLegend
        showSecret={secret !== null}
        showFailed={callEvents.some((e) => eventOutcome(e) === "failed")}
      />

      <div className="grid border-t @[900px]:h-[min(760px,85vh)] @[900px]:grid-cols-[minmax(0,5fr)_minmax(0,6fr)]">
        <Transcript
          events={events}
          selected={selected}
          onSelect={setSelected}
          filter={filter}
        />
        <Inspector
          events={events}
          selected={selected}
          onSelect={setSelected}
          guardrail={guardrail}
          analysisState={analysisState}
          turns={turns}
          totalCalls={callEvents.length}
          secret={secret}
        />
      </div>
    </section>
  );
}
