import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { useEffect, useRef, type PointerEvent } from "react";
import {
  eventOutcome,
  eventToolName,
  matchesFilter,
  resultMeta,
  type EventFilter,
  type SessionEvent,
  type TurnGroup,
} from "./events";
import { OUTCOME_LABEL } from "./model";
import { HATCH_STYLE, OUTCOME_FILL, OUTCOME_TEXT, SECRET_BAND } from "./styles";

const CELL_WIDTH: Record<SessionEvent["kind"], string> = {
  user: "w-[200px]",
  assistant: "w-[176px]",
  tool_call: "w-[148px]",
  tool_result: "w-[120px]",
};

const DRAG_THRESHOLD_PX = 4;
const EDGE_PX = 20;

export type SecretContext = {
  since: number;
  sinceCall: number;
  risky: Set<number>;
} | null;

/** Horizontally sliding strip of every event, grouped by turn. */
export function SessionTimeline({
  events,
  groups,
  selected,
  onSelect,
  filter,
  secret,
}: {
  events: SessionEvent[];
  groups: TurnGroup[];
  selected: number;
  onSelect: (index: number) => void;
  filter: EventFilter;
  secret: SecretContext;
}): JSX.Element {
  const scroller = useRef<HTMLDivElement>(null);
  const cells = useRef<(HTMLButtonElement | null)[]>([]);
  const drag = useRef<{ x: number; left: number; moved: boolean } | null>(null);
  const suppressClick = useRef(false);

  useEffect(() => {
    const el = scroller.current;
    const cell = cells.current[selected];
    if (!el || !cell) return;
    const box = el.getBoundingClientRect();
    const rect = cell.getBoundingClientRect();
    if (rect.left >= box.left + EDGE_PX && rect.right <= box.right - EDGE_PX) {
      return;
    }
    el.scrollTo({
      left:
        el.scrollLeft + (rect.left - box.left) - (box.width - rect.width) / 2,
      behavior: "smooth",
    });
  }, [selected]);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.pointerType !== "mouse" || e.button !== 0 || !scroller.current) {
      return;
    }
    drag.current = {
      x: e.clientX,
      left: scroller.current.scrollLeft,
      moved: false,
    };
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    const el = scroller.current;
    if (!d || !el) return;
    const dx = e.clientX - d.x;
    if (!d.moved && Math.abs(dx) <= DRAG_THRESHOLD_PX) return;
    if (!d.moved) {
      d.moved = true;
      el.setPointerCapture(e.pointerId);
    }
    el.scrollLeft = d.left - dx;
  };
  const endDrag = () => {
    suppressClick.current = drag.current?.moved ?? false;
    drag.current = null;
  };

  return (
    <div
      ref={scroller}
      className="cursor-grab overflow-x-auto overflow-y-hidden px-5 pt-[18px] pb-3.5 select-none [scrollbar-width:thin]"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onClickCapture={(e) => {
        if (!suppressClick.current) return;
        suppressClick.current = false;
        e.stopPropagation();
        e.preventDefault();
      }}
    >
      <div className="flex w-max gap-6">
        {groups.map((group) => (
          <div key={group.indexes[0]} className="flex flex-col gap-2">
            <div className="flex items-baseline justify-between gap-3 border-t border-neutral-default pt-1.5 font-mono text-[10px] tracking-[0.08em] uppercase">
              <span className="text-foreground">{group.label}</span>
              <span className="text-muted-foreground tabular-nums">
                {group.occurredAt.toLocaleTimeString()}
              </span>
            </div>
            <div className="flex gap-1.5">
              {group.indexes.map((i) => {
                const event = events[i];
                if (!event) return null;
                return (
                  <div key={event.key} className="flex flex-col gap-1">
                    <EventCell
                      event={event}
                      selected={i === selected}
                      dimmed={!matchesFilter(event, filter)}
                      risky={
                        event.kind === "tool_call" &&
                        (secret?.risky.has(i) ?? false)
                      }
                      cellRef={(el) => {
                        cells.current[i] = el;
                      }}
                      onClick={() => onSelect(i)}
                    />
                    {secret && i >= secret.since ? (
                      <SimpleTooltip
                        tooltip={`Secret in context since #${secret.sinceCall + 1}`}
                      >
                        <span
                          className={cn("block h-1.5", SECRET_BAND)}
                          style={HATCH_STYLE}
                        />
                      </SimpleTooltip>
                    ) : (
                      <span className="block h-1.5" />
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function EventCell({
  event,
  selected,
  dimmed,
  risky,
  cellRef,
  onClick,
}: {
  event: SessionEvent;
  selected: boolean;
  dimmed: boolean;
  risky: boolean;
  cellRef: (el: HTMLButtonElement | null) => void;
  onClick: () => void;
}): JSX.Element {
  const outcome = event.kind === "tool_call" ? eventOutcome(event) : null;
  let kicker: string;
  let body: string;
  let label: string;
  switch (event.kind) {
    case "user":
    case "assistant":
      kicker = event.kind === "user" ? "User" : "Assistant";
      body = event.text;
      label = `${kicker}: ${event.text.slice(0, 80)}`;
      break;
    case "tool_call":
      kicker = `#${event.callIndex + 1} call`;
      body = eventToolName(event);
      label = `Call #${event.callIndex + 1} ${body}, ${outcome ? OUTCOME_LABEL[outcome].toLowerCase() : "not analyzed"}`;
      break;
    case "tool_result":
      kicker = `↳ #${event.callIndex + 1}`;
      body = resultMeta(event);
      label = `Response to call #${event.callIndex + 1}, ${body}`;
      break;
  }

  return (
    <button
      ref={cellRef}
      type="button"
      aria-label={label}
      aria-pressed={selected}
      title={label}
      onClick={onClick}
      className={cn(
        "relative flex h-[84px] cursor-pointer flex-col gap-[7px] overflow-hidden border pb-2 text-left",
        CELL_WIDTH[event.kind],
        event.kind === "tool_result"
          ? "bg-surface-secondary-default"
          : "bg-card",
        selected
          ? "border-foreground shadow-[0_0_0_1px_var(--color-foreground),0_4px_12px_hsla(0_0%_0%/0.08)]"
          : "hover:border-neutral-default",
        dimmed && "opacity-30",
      )}
    >
      <TopBar event={event} />
      <span className="flex items-center justify-between gap-2 px-2 font-mono text-[10px] tracking-[0.08em] text-muted-foreground uppercase">
        <span className="truncate">{kicker}</span>
        {outcome && (
          <span className={cn("shrink-0", OUTCOME_TEXT[outcome])}>
            {outcome === "failed" ? "Failed" : OUTCOME_LABEL[outcome]}
          </span>
        )}
      </span>
      <span
        className={cn(
          "line-clamp-2 px-2 text-xs leading-[1.4] break-words",
          event.kind === "tool_result"
            ? "font-mono text-muted-foreground"
            : "text-foreground",
          event.kind === "tool_call" && "font-mono",
        )}
      >
        {body}
      </span>
      {risky && (
        <span
          aria-hidden
          title="Runs code or sends data while a secret is in context"
          className="absolute right-1.5 bottom-1.5 size-3 rounded-full border-2 border-destructive-default bg-card"
        />
      )}
    </button>
  );
}

function TopBar({ event }: { event: SessionEvent }): JSX.Element {
  switch (event.kind) {
    case "user":
      return <span className="block h-1 w-full shrink-0 bg-foreground" />;
    case "assistant":
      return (
        <span className="block h-1 w-full shrink-0 shadow-[inset_0_-1px_0_var(--border-neutral-default)]" />
      );
    case "tool_result":
      return (
        <span className="block h-1 w-full shrink-0 bg-surface-tertiary-default" />
      );
    case "tool_call": {
      const outcome = eventOutcome(event);
      if (!outcome) {
        return (
          <span className="block h-1 w-full shrink-0 bg-[var(--border)]" />
        );
      }
      return (
        <span
          className={cn("block h-1 w-full shrink-0", OUTCOME_FILL[outcome])}
          style={outcome === "failed" ? HATCH_STYLE : undefined}
        />
      );
    }
  }
}

export function TimelineLegend({
  showSecret,
  showFailed,
}: {
  showSecret: boolean;
  showFailed: boolean;
}): JSX.Element {
  const swatch = "inline-block size-2.5 shrink-0";
  const item = "flex items-center gap-1.5";
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-1 border-t px-5 py-2.5 text-xs text-muted-foreground">
      <span className={item}>
        <span className={cn(swatch, "bg-foreground")} /> User
      </span>
      <span className={item}>
        <span className={cn(swatch, "border border-neutral-default")} />{" "}
        Assistant
      </span>
      <span className={item}>
        <span className={cn(swatch, OUTCOME_FILL.permit)} /> Permit
      </span>
      <span className={item}>
        <span className={cn(swatch, OUTCOME_FILL.clarify)} /> Clarify: asks a
        person first
      </span>
      <span className={item}>
        <span className={cn(swatch, OUTCOME_FILL.block)} /> Block
      </span>
      {showFailed && (
        <span className={item}>
          <span
            className={cn(swatch, "border", OUTCOME_FILL.failed)}
            style={HATCH_STYLE}
          />{" "}
          Evaluator failed
        </span>
      )}
      <span className={item}>
        <span className={cn(swatch, "bg-surface-tertiary-default")} /> Tool
        response
      </span>
      {showSecret && (
        <span className={item}>
          <span className={cn(swatch, SECRET_BAND)} style={HATCH_STYLE} />{" "}
          Secret in context
        </span>
      )}
      <span className="ml-auto font-mono text-[11px]">
        Drag or scroll to slide · ↑ ↓ or j k to step
      </span>
    </div>
  );
}
