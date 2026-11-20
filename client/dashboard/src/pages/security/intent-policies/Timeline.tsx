import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import type { IntentLabelQuestion } from "@gram/client/models/components/intentlabelquestion.js";
import { useEffect, useRef, type KeyboardEvent } from "react";
import {
  callOutcome,
  findLabel,
  formatValue,
  OUTCOME_LABEL,
  toolDisplayName,
  type CallOutcome,
} from "./model";

const OUTCOME_CELL: Record<CallOutcome, string> = {
  permit: "bg-muted",
  clarify: "bg-warning-default",
  block: "bg-destructive-default",
  failed: "bg-muted text-muted-foreground",
};

// Diagonal hatching marks an evaluator failure so it never reads as a block.
const HATCH_STYLE = {
  backgroundImage:
    "repeating-linear-gradient(135deg, currentColor 0 1px, transparent 1px 5px)",
};

export function Timeline({
  calls,
  questions,
  selected,
  onSelect,
}: {
  calls: IntentCallAssessment[];
  questions: IntentLabelQuestion[];
  selected: number;
  onSelect: (index: number) => void;
}): JSX.Element {
  const columnRefs = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    columnRefs.current[selected]?.scrollIntoView({
      block: "nearest",
      inline: "nearest",
    });
  }, [selected]);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "ArrowRight") {
      e.preventDefault();
      onSelect(Math.min(calls.length - 1, selected + 1));
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      onSelect(Math.max(0, selected - 1));
    }
  };

  return (
    <div
      role="listbox"
      aria-label="Tool calls"
      tabIndex={0}
      onKeyDown={onKeyDown}
      className="flex overflow-x-auto border bg-card focus-visible:outline-1 focus-visible:outline-[var(--border-focus)]"
    >
      <div className="sticky left-0 z-10 flex shrink-0 flex-col border-r bg-card">
        <div className="text-eyebrow flex h-28 items-end px-3 pb-2">
          Tool call
        </div>
        {questions.map((q) => (
          <div
            key={q.key}
            className="flex h-7 items-center px-3 font-mono text-xs"
          >
            {q.key}
          </div>
        ))}
        <div className="text-eyebrow flex h-8 items-center border-t px-3">
          Decision
        </div>
      </div>
      {calls.map((call, i) => {
        const outcome = callOutcome(call);
        const isSelected = i === selected;
        return (
          <button
            key={`${call.messageId}:${call.toolCallId}`}
            ref={(el) => {
              columnRefs.current[i] = el;
            }}
            type="button"
            role="option"
            aria-selected={isSelected}
            tabIndex={-1}
            onClick={() => onSelect(i)}
            className={cn(
              "flex w-8 shrink-0 cursor-pointer flex-col border-r border-l border-transparent hover:bg-muted/40",
              isSelected && "border-foreground bg-muted/40",
            )}
          >
            <SimpleTooltip tooltip={call.toolName}>
              <span className="flex h-28 items-end justify-center overflow-hidden pb-2 font-mono text-xs [writing-mode:vertical-rl] rotate-180">
                <span className="truncate">{toolDisplayName(call)}</span>
              </span>
            </SimpleTooltip>
            {questions.map((q) => (
              <LabelCell key={q.key} call={call} question={q} />
            ))}
            <SimpleTooltip tooltip={OUTCOME_LABEL[outcome]}>
              <span className="flex h-8 items-center justify-center border-t">
                <span
                  aria-label={OUTCOME_LABEL[outcome]}
                  className={cn("size-5", OUTCOME_CELL[outcome])}
                  style={outcome === "failed" ? HATCH_STYLE : undefined}
                />
              </span>
            </SimpleTooltip>
          </button>
        );
      })}
    </div>
  );
}

function LabelCell({
  call,
  question,
}: {
  call: IntentCallAssessment;
  question: IntentLabelQuestion;
}): JSX.Element {
  const label = findLabel(call, question.key);
  if (!label) {
    return (
      <span className="flex h-7 items-center justify-center">
        <span
          className="size-4 text-muted-foreground"
          style={HATCH_STYLE}
          aria-label="Not answered"
        />
      </span>
    );
  }

  if (question.kind === "multi_noul") {
    const members = Array.isArray(label.value) ? (label.value as string[]) : [];
    return (
      <SimpleTooltip tooltip={formatValue(label.value)}>
        <span className="flex h-7 items-center justify-center gap-px">
          {question.options.map((o) => (
            <span
              key={o.key}
              className={cn(
                "h-4 w-1",
                members.includes(o.key) ? "bg-foreground" : "bg-muted",
              )}
            />
          ))}
        </span>
      </SimpleTooltip>
    );
  }

  if (question.kind === "choice") {
    const option = question.options.find((o) => o.key === label.value);
    const maxOrdinal = Math.max(1, ...question.options.map((o) => o.ordinal));
    const shade = option ? 1 - option.ordinal / maxOrdinal : 1;
    return (
      <SimpleTooltip tooltip={formatValue(label.value)}>
        <span className="flex h-7 items-center justify-center">
          <span
            className="size-4 border bg-warning-default"
            style={{ opacity: Math.max(0.08, shade) }}
          />
        </span>
      </SimpleTooltip>
    );
  }

  const on = label.value === true;
  return (
    <SimpleTooltip
      tooltip={`${on ? "yes" : "no"} · ${Math.round(label.probability * 100)}%`}
    >
      <span className="flex h-7 items-center justify-center">
        <span
          className={cn("size-4 border", on ? "bg-foreground" : "bg-card")}
          style={
            on ? { opacity: Math.max(0.25, label.probability) } : undefined
          }
        />
      </span>
    </SimpleTooltip>
  );
}
