import { Badge } from "@/components/ui/Badge";
import { ToggleButton } from "@/components/ui/SegmentedControl";
import { cn } from "@/lib/utils";
import { useEffect, useRef, useState } from "react";
import {
  argsSummary,
  eventOutcome,
  eventToolName,
  describeGap,
  matchesFilter,
  resultMeta,
  transcriptWindow,
  type CallEvent,
  type EventFilter,
  type SessionEvent,
} from "./events";
import { OUTCOME_LABEL } from "./model";
import { OUTCOME_BADGE } from "./styles";

const WINDOW_SIZE = 3;
const WINDOW_STEP = 5;

type Mode = "window" | "all";

/** Transcript pane: a sliding window of events around the selection. */
export function Transcript({
  events,
  selected,
  onSelect,
  filter,
}: {
  events: SessionEvent[];
  selected: number;
  onSelect: (index: number) => void;
  filter: EventFilter;
}): JSX.Element {
  const [mode, setMode] = useState<Mode>("window");
  const [extra, setExtra] = useState({ before: 0, after: 0, for: selected });
  // Extensions belong to one selection; the window follows the next one.
  const { before, after } =
    extra.for === selected ? extra : { before: 0, after: 0 };
  const list = useRef<HTMLOListElement>(null);
  const rows = useRef<(HTMLLIElement | null)[]>([]);

  const range =
    mode === "all"
      ? { from: 0, to: events.length - 1 }
      : transcriptWindow(events.length, selected, WINDOW_SIZE, before, after);

  useEffect(() => {
    const el = list.current;
    const row = rows.current[selected];
    if (!el || !row) return;
    const box = el.getBoundingClientRect();
    const rect = row.getBoundingClientRect();
    el.scrollTo({
      top: el.scrollTop + (rect.top - box.top) - (box.height - rect.height) / 2,
      behavior: "smooth",
    });
  }, [selected, mode]);

  const extend = (side: "before" | "after") =>
    setExtra({
      before: before + (side === "before" ? WINDOW_STEP : 0),
      after: after + (side === "after" ? WINDOW_STEP : 0),
      for: selected,
    });

  const shown = range.to - range.from + 1;

  return (
    <div className="flex min-h-0 min-w-0 flex-col">
      <div className="flex items-center gap-3 border-b px-5 py-2.5">
        <span className="text-eyebrow">Transcript</span>
        <span className="flex-1 truncate text-xs text-muted-foreground">
          {mode === "all" || shown === events.length
            ? `All ${events.length} events`
            : `${shown} of ${events.length} events around the selection`}
        </span>
        <div
          role="group"
          aria-label="Transcript range"
          className="flex h-[26px] items-center border"
        >
          <ToggleButton
            active={mode === "window"}
            onClick={() => setMode("window")}
          >
            Window
          </ToggleButton>
          <ToggleButton active={mode === "all"} onClick={() => setMode("all")}>
            All
          </ToggleButton>
        </div>
      </div>
      <ol ref={list} className="relative min-h-0 flex-1 overflow-y-auto">
        {range.from > 0 && (
          <GapRow
            text={describeGap(events, 0, range.from - 1, "earlier")}
            action={
              range.from <= WINDOW_STEP ? "Show" : `Show ${WINDOW_STEP} more`
            }
            onClick={() => extend("before")}
          />
        )}
        {events.slice(range.from, range.to + 1).map((event, offset) => {
          const i = range.from + offset;
          return (
            <li
              key={event.key}
              ref={(el) => {
                rows.current[i] = el;
              }}
            >
              <TranscriptRow
                event={event}
                selected={i === selected}
                dimmed={!matchesFilter(event, filter)}
                onClick={() => onSelect(i)}
              />
            </li>
          );
        })}
        {range.to < events.length - 1 && (
          <GapRow
            text={describeGap(events, range.to + 1, events.length - 1, "later")}
            action={
              events.length - 1 - range.to <= WINDOW_STEP
                ? "Show"
                : `Show ${WINDOW_STEP} more`
            }
            onClick={() => extend("after")}
          />
        )}
      </ol>
    </div>
  );
}

function GapRow({
  text,
  action,
  onClick,
}: {
  text: string;
  action: string;
  onClick: () => void;
}): JSX.Element {
  return (
    <li className="border-b border-neutral-softest bg-surface-secondary-default">
      <button
        type="button"
        onClick={onClick}
        className="flex w-full cursor-pointer items-center gap-3 px-5 py-2.5 font-mono text-[11px] text-muted-foreground"
      >
        <span className="flex-1 border-t border-dashed border-neutral-default" />
        <span>{text}</span>
        <span className="text-foreground underline-offset-2 hover:underline">
          {action}
        </span>
        <span className="flex-1 border-t border-dashed border-neutral-default" />
      </button>
    </li>
  );
}

function Time({ at }: { at: Date }): JSX.Element {
  return (
    <span className="font-mono text-[11px] text-muted-foreground tabular-nums">
      {at.toLocaleTimeString()}
    </span>
  );
}

function TranscriptRow({
  event,
  selected,
  dimmed,
  onClick,
}: {
  event: SessionEvent;
  selected: boolean;
  dimmed?: boolean;
  onClick: () => void;
}): JSX.Element {
  const base = cn(
    "flex w-full cursor-pointer flex-col gap-1.5 border-b border-neutral-softest py-3 pr-5 text-left hover:bg-surface-secondary-default",
    event.kind === "tool_result" ? "pl-[50px]" : "pl-5",
    event.kind === "user" && "bg-surface-secondary-default",
    selected &&
      "bg-muted shadow-[inset_0_0_0_1px_var(--color-foreground)] hover:bg-muted",
    dimmed && "opacity-30",
  );

  switch (event.kind) {
    case "user":
    case "assistant":
      return (
        <button
          type="button"
          aria-pressed={selected}
          onClick={onClick}
          className={base}
        >
          <span className="flex items-center justify-between gap-3">
            <span
              className={cn(
                "text-eyebrow",
                event.kind === "user" && "text-foreground",
              )}
            >
              {event.kind === "user" ? "User" : "Assistant"}
            </span>
            <Time at={event.occurredAt} />
          </span>
          <span
            className={cn(
              "text-sm leading-normal break-words whitespace-pre-wrap",
              event.kind === "user"
                ? "text-foreground"
                : "text-[color:var(--text-default)]",
            )}
          >
            {event.text}
          </span>
        </button>
      );
    case "tool_call":
      return (
        <CallRow
          event={event}
          onClick={onClick}
          className={cn(base, "flex-row items-center gap-2.5")}
          selected={selected}
        />
      );
    case "tool_result":
      return (
        <button
          type="button"
          aria-pressed={selected}
          onClick={onClick}
          className={base}
        >
          <span className="flex items-center justify-between gap-3">
            <span className="text-eyebrow">
              ↳ Response · #{event.callIndex + 1}
            </span>
            <span className="font-mono text-[11px] text-muted-foreground">
              {resultMeta(event)}
            </span>
          </span>
          <span className="line-clamp-2 font-mono text-xs leading-normal break-all whitespace-pre-wrap text-[color:var(--text-default)]">
            {event.output || "(empty)"}
          </span>
        </button>
      );
  }
}

/** One-line call row, shared by the transcript and the inspector's call lists. */
export function CallRow({
  event,
  onClick,
  className,
  selected,
}: {
  event: CallEvent;
  onClick: () => void;
  className?: string;
  selected?: boolean;
}): JSX.Element {
  const outcome = eventOutcome(event);
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onClick}
      className={cn(
        "flex w-full min-w-0 items-center gap-2.5 text-left",
        className,
      )}
    >
      <span className="w-5 shrink-0 font-mono text-[11px] text-muted-foreground">
        #{event.callIndex + 1}
      </span>
      <span className="shrink-0 font-mono text-[13px] text-foreground">
        {eventToolName(event)}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">
        {argsSummary(event.arguments)}
      </span>
      {outcome && (
        <Badge
          variant={OUTCOME_BADGE[outcome]}
          className="shrink-0 font-mono uppercase"
        >
          {OUTCOME_LABEL[outcome]}
        </Badge>
      )}
    </button>
  );
}
