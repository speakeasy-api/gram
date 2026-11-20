import { Badge } from "@/components/ui/Badge";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Text } from "@/components/ui/Text";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";
import { ChevronRight } from "lucide-react";
import {
  callOutcome,
  findLabel,
  formatValue,
  formatCost,
  OUTCOME_LABEL,
  prettyArguments,
  type CallOutcome,
} from "./model";

const OUTCOME_BADGE: Record<
  CallOutcome,
  "neutral" | "warning" | "destructive"
> = {
  permit: "neutral",
  clarify: "warning",
  block: "destructive",
  failed: "neutral",
};

const RESULT_BADGE: Record<string, "neutral" | "warning" | "destructive"> = {
  permit: "neutral",
  abstain: "neutral",
  clarify: "warning",
  block: "destructive",
};

export function DecisionPanel({
  call,
  guardrail,
}: {
  call: IntentCallAssessment;
  guardrail: IntentGuardrail;
}): JSX.Element {
  const outcome = callOutcome(call);
  const contractName = new Map(guardrail.contracts.map((c) => [c.id, c]));

  return (
    <div className="grid gap-6 border bg-card p-6 @4xl:grid-cols-2">
      <div className="flex min-w-0 flex-col gap-5">
        <div className="flex flex-col gap-1">
          <span className="text-eyebrow">Tool</span>
          <span className="font-mono text-sm break-all">{call.toolName}</span>
          {call.mcpServer && (
            <Text small muted>
              MCP server {call.mcpServer} · function {call.mcpFunction}
            </Text>
          )}
          <Text small muted>
            {call.occurredAt.toLocaleString()} · {call.usage.latencyMs} ms ·{" "}
            {formatCost(call.usage.costUsd)}
          </Text>
        </div>

        <div className="flex flex-col gap-2">
          <span className="text-eyebrow">Decision</span>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={OUTCOME_BADGE[outcome]} size="lg">
              {OUTCOME_LABEL[outcome]}
            </Badge>
            {call.decidedBy.length > 0 && (
              <Text small muted>
                by{" "}
                {call.decidedBy
                  .map((id) => contractName.get(id)?.name ?? id)
                  .join(", ")}
              </Text>
            )}
          </div>
          {call.evaluatorError && (
            <Text small warning className="break-words">
              The evaluator did not answer every label: {call.evaluatorError}
            </Text>
          )}
        </div>

        <Collapsible defaultOpen>
          <CollapsibleTrigger className="group flex cursor-pointer items-center gap-1">
            <ChevronRight className="size-3 transition-transform group-data-[state=open]:rotate-90" />
            <span className="text-eyebrow">Arguments</span>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="mt-2 max-h-80 overflow-auto border bg-muted/30 p-3 font-mono text-xs whitespace-pre-wrap break-all">
              {prettyArguments(call.arguments)}
            </pre>
          </CollapsibleContent>
        </Collapsible>

        <div className="flex flex-col gap-2">
          <span className="text-eyebrow">Labels</span>
          <ul className="flex flex-col divide-y border-y">
            {guardrail.questions.map((q) => {
              const label = findLabel(call, q.key);
              return (
                <li key={q.key} className="flex flex-col gap-0.5 py-2">
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="font-mono text-xs">{q.key}</span>
                    <span className="text-right font-mono text-xs">
                      {label ? formatValue(label.value) : "not answered"}
                      {label && (
                        <span className="text-muted-foreground">
                          {" "}
                          · {Math.round(label.probability * 100)}%
                        </span>
                      )}
                    </span>
                  </div>
                  {label?.rationale && (
                    <Text small muted>
                      {label.rationale}
                    </Text>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      </div>

      <div className="flex min-w-0 flex-col gap-2">
        <span className="text-eyebrow">Contracts</span>
        <ul className="flex flex-col divide-y border-y">
          {call.outcomes.map((o) => {
            const contract = contractName.get(o.contractId);
            const result = o.error ? "error" : (o.result ?? "abstain");
            return (
              <li key={o.contractId} className="flex flex-col gap-1.5 py-3">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-sm">
                    {contract?.name ?? o.contractId}
                  </span>
                  <Badge
                    variant={
                      o.error
                        ? "destructive"
                        : (RESULT_BADGE[result] ?? "neutral")
                    }
                  >
                    {result}
                  </Badge>
                </div>
                <code className="font-mono text-2xs break-all text-muted-foreground">
                  {contract?.expression}
                </code>
                <dl className="flex flex-wrap gap-x-4 gap-y-0.5 font-mono text-xs">
                  {Object.entries(o.inputs).map(([k, v]) => (
                    <div key={k} className="flex min-w-0 gap-1">
                      <dt className="text-muted-foreground">{k}</dt>
                      <dd className="truncate">{formatValue(v)}</dd>
                    </div>
                  ))}
                </dl>
                {o.error && (
                  <Text small destructive className="break-words">
                    {o.error}
                  </Text>
                )}
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
