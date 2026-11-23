import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";
import type { ReactNode } from "react";
import { ArgumentsBlock, Block, DecisionPanel } from "./DecisionPanel";
import {
  eventOutcome,
  eventToolName,
  resultMeta,
  type CallEvent,
  type SessionEvent,
} from "./events";
import {
  callOutcome,
  formatClock,
  formatCost,
  OUTCOME_LABEL,
  type CallOutcome,
} from "./model";
import type { SecretContext } from "./SessionTimeline";
import { HATCH_STYLE } from "./styles";
import { CallRow } from "./Transcript";

const ACCENT: Record<CallOutcome, string> = {
  permit: "bg-[var(--border-neutral-default)]",
  clarify: "bg-warning-default",
  block: "bg-destructive-default",
  failed: "text-muted-foreground",
};

export type AnalysisState = "none" | "running" | "done";

const NOT_ANALYZED: Record<AnalysisState, string> = {
  none: "Press Analyze to see what the guardrail decides for this call.",
  running: "Labeling… this call's decision appears when its batch lands.",
  done: "Not analyzed: the analysis stopped or hit its limit before this call.",
};

/** Detail for whatever event is selected. */
export function Inspector({
  events,
  selected,
  onSelect,
  guardrail,
  analysisState,
  turns,
  totalCalls,
  secret,
}: {
  events: SessionEvent[];
  selected: number;
  onSelect: (index: number) => void;
  guardrail: IntentGuardrail | undefined;
  analysisState: AnalysisState;
  turns: number[];
  totalCalls: number;
  secret: SecretContext;
}): JSX.Element | null {
  const event = events[selected];
  if (!event) return null;
  const start = events[0]?.occurredAt.getTime() ?? 0;
  const elapsed = formatClock((event.occurredAt.getTime() - start) / 1000);
  const when = `${event.occurredAt.toLocaleTimeString()} · +${elapsed} into the session`;
  const inSecret = secret !== null && selected >= secret.since;
  const secretText = inSecret ? `Yes, since #${secret.sinceCall + 1}` : "No";
  const describeCall = (e: CallEvent) => {
    const outcome = eventOutcome(e);
    const name = `#${e.callIndex + 1} ${eventToolName(e)}`;
    return outcome ? `${name} · ${OUTCOME_LABEL[outcome].toLowerCase()}` : name;
  };

  let header: ReactNode;
  let body: ReactNode;

  switch (event.kind) {
    case "tool_call": {
      const response = events.findIndex(
        (e) => e.kind === "tool_result" && e.callIndex === event.callIndex,
      );
      const viewResponse =
        response === -1 ? undefined : () => onSelect(response);
      if (!event.call || !guardrail) {
        header = (
          <Header
            accent="bg-[var(--border)]"
            eyebrow={`Call #${event.callIndex + 1} of ${totalCalls}`}
            title={event.toolName}
            meta={[`${when} · turn ${turns[selected] ?? "-"}`]}
          />
        );
        body = (
          <>
            <p className="border bg-card px-4 py-3.5 text-sm text-muted-foreground">
              {NOT_ANALYZED[analysisState]}
            </p>
            <ArgumentsBlock raw={event.arguments} />
            {viewResponse && (
              <div>
                <Button variant="secondary" size="sm" onClick={viewResponse}>
                  <Button.Text>View response ↓</Button.Text>
                </Button>
              </div>
            )}
          </>
        );
        break;
      }
      const outcome = callOutcome(event.call);
      const { usage, mcpServer, mcpFunction } = event.call;
      header = (
        <Header
          accent={ACCENT[outcome]}
          hatched={outcome === "failed"}
          eyebrow={`Call #${event.callIndex + 1} of ${totalCalls}`}
          title={event.call.toolName}
          meta={[
            `${when} · turn ${turns[selected] ?? "-"} · labeled in ${usage.latencyMs} ms · ${formatCost(usage.costUsd)}`,
            mcpServer
              ? `MCP server ${mcpServer} · function ${mcpFunction}`
              : "",
          ]}
        />
      );
      body = (
        <DecisionPanel
          key={event.key}
          call={event.call}
          guardrail={guardrail}
          onViewResponse={viewResponse}
        />
      );
      break;
    }
    case "tool_result": {
      const callAt = events.findIndex(
        (e) => e.kind === "tool_call" && e.callIndex === event.callIndex,
      );
      const call = events[callAt];
      const next = events
        .slice(selected + 1)
        .find((e): e is CallEvent => e.kind === "tool_call");
      const laterReply = events
        .slice(selected + 1)
        .some((e) => e.kind === "assistant");
      header = (
        <Header
          accent="bg-[var(--border)]"
          eyebrow={`Response to call #${event.callIndex + 1}`}
          title={
            call?.kind === "tool_call" ? `${call.toolName} result` : "Result"
          }
          meta={[`${when} · ${resultMeta(event)}`]}
        />
      );
      body = (
        <>
          <Block title="Output">
            <pre className="max-h-[420px] overflow-x-hidden overflow-y-auto border bg-card p-3 font-mono text-xs leading-[1.6] break-all whitespace-pre-wrap">
              {event.output || "(empty)"}
            </pre>
          </Block>
          <Signals
            rows={[
              [
                "Produced by",
                call?.kind === "tool_call" ? describeCall(call) : "Unknown",
              ],
              ["Secret in context", secretText],
              [
                "Next action",
                next ? describeCall(next) : laterReply ? "Final reply" : "None",
              ],
            ]}
          />
          {callAt !== -1 && (
            <div>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => onSelect(callAt)}
              >
                <Button.Text>↑ View call #{event.callIndex + 1}</Button.Text>
              </Button>
            </div>
          )}
        </>
      );
      break;
    }
    case "user":
    case "assistant": {
      const isUser = event.kind === "user";
      const turn = turns[selected];
      const related = events.flatMap((e, i) =>
        e.kind === "tool_call" && turns[i] === turn ? [i] : [],
      );
      const blocked = related.filter((i) => {
        const e = events[i];
        return e?.kind === "tool_call" && eventOutcome(e) === "block";
      }).length;
      header = (
        <Header
          accent={
            isUser ? "bg-foreground" : "bg-[var(--border-neutral-default)]"
          }
          eyebrow={
            isUser
              ? `User prompt · turn ${turn}`
              : `Assistant message · turn ${turn}`
          }
          title={isUser ? "User" : "Assistant"}
          meta={[when]}
        />
      );
      body = (
        <>
          <Signals
            rows={[
              [
                "Calls in this turn",
                blocked > 0
                  ? `${related.length} · ${blocked} blocked`
                  : String(related.length),
              ],
              ["Secret in context", secretText],
            ]}
          />
          {related.length > 0 && (
            <Block title="Calls in this turn">
              <ul className="flex flex-col divide-y border bg-card">
                {related.map((i) => {
                  const e = events[i];
                  return e?.kind === "tool_call" ? (
                    <li key={e.key}>
                      <CallRow
                        event={e}
                        onClick={() => onSelect(i)}
                        className="cursor-pointer px-3 py-2 hover:bg-surface-secondary-default"
                      />
                    </li>
                  ) : null;
                })}
              </ul>
            </Block>
          )}
        </>
      );
      break;
    }
  }

  return (
    <div className="flex min-h-0 min-w-0 flex-col gap-6 overflow-y-auto border-l border-neutral-default bg-muted px-6 pt-5 pb-8">
      {header}
      {body}
    </div>
  );
}

function Header({
  accent,
  hatched,
  eyebrow,
  title,
  meta,
}: {
  accent: string;
  hatched?: boolean;
  eyebrow: string;
  title: string;
  meta: string[];
}): JSX.Element {
  return (
    <div className="border bg-card shadow-[0_1px_2px_hsla(0_0%_0%/0.06)]">
      <span
        className={cn("block h-1", accent)}
        style={hatched ? HATCH_STYLE : undefined}
      />
      <div className="flex flex-col gap-1 px-5 py-4">
        <span className="text-eyebrow">{eyebrow}</span>
        <h3 className="font-mono text-xl break-all text-foreground">{title}</h3>
        {meta.filter(Boolean).map((m) => (
          <span key={m} className="text-xs text-muted-foreground">
            {m}
          </span>
        ))}
      </div>
    </div>
  );
}

function Signals({ rows }: { rows: [string, string][] }): JSX.Element {
  return (
    <Block title="Signals">
      <dl className="flex flex-col divide-y border-y text-xs">
        {rows.map(([k, v]) => (
          <div key={k} className="grid grid-cols-[160px_1fr] gap-3 py-2">
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="text-foreground">{v}</dd>
          </div>
        ))}
      </dl>
    </Block>
  );
}
