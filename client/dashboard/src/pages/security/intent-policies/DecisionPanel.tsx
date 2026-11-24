import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import type { IntentContractOutcome } from "@gram/client/models/components/intentcontractoutcome.js";
import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";
import type { IntentLabelQuestion } from "@gram/client/models/components/intentlabelquestion.js";
import { Fragment, useState, type ReactNode } from "react";
import { Link } from "react-router";
import {
  callOutcome,
  decisionReason,
  findLabel,
  formatValue,
  LOW_CONFIDENCE,
  OUTCOME_LABEL,
  prettyArguments,
  shellCommand,
  shellSteps,
  type CallOutcome,
} from "./model";
import { OUTCOME_BADGE } from "./styles";

const VERDICT_BOX: Record<CallOutcome, string> = {
  permit: "bg-card",
  clarify: "border-warning-default bg-warning-softest",
  block: "border-destructive-default bg-destructive-softest",
  failed: "bg-card",
};

const RESULT_BADGE: Record<string, "neutral" | "warning" | "destructive"> = {
  permit: "neutral",
  abstain: "neutral",
  clarify: "warning",
  block: "destructive",
};

/** True when a label flags something: a true boolean or an uncertain choice. */
function raised(question: IntentLabelQuestion, value: unknown): boolean {
  if (question.kind === "choice") {
    return question.options.find((o) => o.key === value)?.uncertain ?? false;
  }
  return value === true;
}

/** The inspector's view of one tool call: verdict, arguments, labels, contracts. */
export function DecisionPanel({
  call,
  guardrail,
  onViewResponse,
}: {
  call: IntentCallAssessment;
  guardrail: IntentGuardrail;
  onViewResponse?: () => void;
}): JSX.Element {
  const [showExpressions, setShowExpressions] = useState(false);
  const outcome = callOutcome(call);
  const contracts = new Map(guardrail.contracts.map((c) => [c.id, c]));
  const names = new Map(guardrail.contracts.map((c) => [c.id, c.name]));

  const deciding = call.outcomes.filter(
    (o) => call.decidedBy.includes(o.contractId) || o.error,
  );
  const abstained = call.outcomes.filter((o) => !deciding.includes(o));
  const raisedCount = guardrail.questions.filter((q) =>
    raised(q, findLabel(call, q.key)?.value),
  ).length;

  return (
    <>
      <div
        className={cn(
          "flex flex-col gap-1.5 border px-4 py-3.5",
          VERDICT_BOX[outcome],
        )}
      >
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={OUTCOME_BADGE[outcome]} size="lg">
            {OUTCOME_LABEL[outcome]}
          </Badge>
          {call.decidedBy.length > 0 && (
            <Text small muted>
              by{" "}
              {call.decidedBy.map((id, i) => (
                <Fragment key={id}>
                  {i > 0 && ", "}
                  <RuleLink id={id}>{names.get(id) ?? id}</RuleLink>
                </Fragment>
              ))}
            </Text>
          )}
        </div>
        <Text className="text-sm leading-normal">
          {decisionReason(call, names)}
        </Text>
        {call.evaluatorError && (
          <Text small warning className="break-words">
            The evaluator did not answer every label: {call.evaluatorError}
          </Text>
        )}
      </div>

      <ArgumentsBlock raw={call.arguments} />

      <Block
        title="Labels"
        aside={`${raisedCount} of ${guardrail.questions.length} raised`}
      >
        <ul className="flex flex-col divide-y border-y">
          {guardrail.questions.map((q) => {
            const label = findLabel(call, q.key);
            const pct = label ? Math.round(label.probability * 100) : 0;
            const isRaised = label ? raised(q, label.value) : false;
            return (
              <li key={q.key} className="flex flex-col gap-1 py-2">
                <div className="grid grid-cols-[minmax(0,1fr)_auto_80px_36px] items-center gap-3">
                  <span className="truncate font-mono text-xs">{q.key}</span>
                  <span className="flex items-center gap-2">
                    {label && label.probability <= LOW_CONFIDENCE && (
                      <Badge variant="warning">Low confidence</Badge>
                    )}
                    <span
                      className={cn(
                        "font-mono text-xs",
                        isRaised
                          ? "text-destructive"
                          : q.kind === "multi_noul"
                            ? "text-foreground"
                            : "text-muted-foreground",
                      )}
                    >
                      {label ? formatValue(label.value) : "not answered"}
                    </span>
                  </span>
                  <span className="block h-1 bg-surface-tertiary-default">
                    <span
                      className="block h-1 bg-foreground"
                      style={{ width: `${pct}%` }}
                    />
                  </span>
                  <span className="text-right font-mono text-[10px] text-muted-foreground tabular-nums">
                    {label ? `${pct}%` : ""}
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
      </Block>

      <Block
        title="Contracts"
        aside={
          <Button
            variant="tertiary"
            size="xs"
            aria-pressed={showExpressions}
            onClick={() => setShowExpressions((v) => !v)}
          >
            <Button.Text>
              {showExpressions ? "Hide rules" : "Show rules"}
            </Button.Text>
          </Button>
        }
      >
        {deciding.map((o) => (
          <ContractRow
            key={o.contractId}
            outcome={o}
            name={contracts.get(o.contractId)?.name ?? o.contractId}
            expression={contracts.get(o.contractId)?.expression}
            showExpression={showExpressions}
            emphasized
          />
        ))}
        {abstained.length > 0 && (
          <div className="flex flex-col">
            <Text small muted className="py-1.5">
              {abstained.length}{" "}
              {abstained.length === 1 ? "contract" : "contracts"} abstained
            </Text>
            <ul className="flex flex-col divide-y border-t">
              {abstained.map((o) => (
                <li key={o.contractId}>
                  <ContractRow
                    outcome={o}
                    name={contracts.get(o.contractId)?.name ?? o.contractId}
                    expression={contracts.get(o.contractId)?.expression}
                    showExpression={showExpressions}
                  />
                </li>
              ))}
            </ul>
          </div>
        )}
      </Block>

      {onViewResponse && (
        <div>
          <Button variant="secondary" size="sm" onClick={onViewResponse}>
            <Button.Text>View response ↓</Button.Text>
          </Button>
        </div>
      )}
    </>
  );
}

/** A call's arguments: shell commands one step per line, else pretty JSON. */
export function ArgumentsBlock({ raw }: { raw: string }): JSX.Element {
  const command = shellCommand(raw);
  return (
    <Block title={command ? "Command, one step per line" : "Arguments"}>
      {command ? (
        <ol className="max-h-80 overflow-x-hidden overflow-y-auto border bg-card px-3 py-2.5 font-mono text-xs leading-[1.65]">
          {shellSteps(command).map((s, i) => (
            <li key={i} className="flex gap-2.5">
              <span aria-hidden className="text-muted-foreground select-none">
                $
              </span>
              <span className="break-all whitespace-pre-wrap">{s}</span>
            </li>
          ))}
        </ol>
      ) : (
        <pre className="max-h-80 overflow-x-hidden overflow-y-auto border bg-card p-3 font-mono text-xs leading-[1.65] break-all whitespace-pre-wrap">
          {prettyArguments(raw)}
        </pre>
      )}
    </Block>
  );
}

/** A titled inspector section. */
export function Block({
  title,
  aside,
  children,
}: {
  title: string;
  aside?: React.ReactNode;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-eyebrow">{title}</span>
        {typeof aside === "string" ? (
          <span className="text-xs text-muted-foreground">{aside}</span>
        ) : (
          aside
        )}
      </div>
      {children}
    </div>
  );
}

function ContractRow({
  outcome,
  name,
  expression,
  showExpression,
  emphasized,
}: {
  outcome: IntentContractOutcome;
  name: string;
  expression: string | undefined;
  showExpression: boolean;
  emphasized?: boolean;
}): JSX.Element {
  const result = outcome.error ? "error" : (outcome.result ?? "abstain");
  const inputs = Object.entries(outcome.inputs).filter(
    ([k]) => !k.startsWith("tool."),
  );
  return (
    <div
      className={cn(
        "flex flex-col gap-1.5",
        emphasized ? "border bg-card p-3.5" : "py-2.5",
      )}
    >
      <div className="flex items-center justify-between gap-3">
        <RuleLink
          id={outcome.contractId}
          className={cn("text-sm", emphasized && "font-medium")}
        >
          {name}
        </RuleLink>
        {emphasized ? (
          <Badge
            variant={
              outcome.error
                ? "destructive"
                : (RESULT_BADGE[result] ?? "neutral")
            }
          >
            {result}
          </Badge>
        ) : (
          <dl className="flex min-w-0 flex-wrap justify-end gap-x-3 font-mono text-2xs text-muted-foreground">
            {inputs.map(([k, v]) => (
              <div key={k} className="flex min-w-0 gap-1">
                <dt>{k.replace(/^(label|action)\./, "")}</dt>
                <dd className="truncate text-foreground">{formatValue(v)}</dd>
              </div>
            ))}
          </dl>
        )}
      </div>
      {emphasized && inputs.length > 0 && (
        <dl className="flex flex-wrap gap-x-4 gap-y-0.5 font-mono text-xs">
          {inputs.map(([k, v]) => (
            <div key={k} className="flex min-w-0 gap-1">
              <dt className="text-muted-foreground">
                {k.replace(/^(label|action)\./, "")}
              </dt>
              <dd className="truncate">{formatValue(v)}</dd>
            </div>
          ))}
        </dl>
      )}
      {showExpression && expression && (
        <code className="font-mono text-2xs break-all text-muted-foreground">
          {expression}
        </code>
      )}
      {outcome.error && (
        <Text small destructive className="break-words">
          {outcome.error}
        </Text>
      )}
    </div>
  );
}

/** Opens the contract's rule on the Guardrail tab. */
function RuleLink({
  id,
  className,
  children,
}: {
  id: string;
  className?: string;
  children: ReactNode;
}): JSX.Element {
  const search = new URLSearchParams({ tab: "guardrail", rule: id });
  return (
    <Link
      to={{ search: `?${search.toString()}` }}
      className={cn(
        "underline decoration-neutral-default decoration-dotted underline-offset-2 hover:decoration-foreground",
        className,
      )}
    >
      {children}
    </Link>
  );
}
