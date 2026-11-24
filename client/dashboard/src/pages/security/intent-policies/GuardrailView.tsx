import type { IntentCondition } from "@gram/client/models/components/intentcondition.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { cn } from "@/lib/utils";
import type { IntentContract } from "@gram/client/models/components/intentcontract.js";
import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";
import type { IntentLabelQuestion } from "@gram/client/models/components/intentlabelquestion.js";
import type { IntentSessionAnalysis } from "@gram/client/models/components/intentsessionanalysis.js";
import { X } from "lucide-react";
import { useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import {
  checkedOptions,
  KIND_LABEL,
  labelsRead,
  OUTCOME_ACTION,
  ruleHits,
  ruleOutcome,
  tokenizeExpression,
  type RuleFilter,
  type RuleHit,
  type RuleOutcome,
} from "./guardrail";
import { toolDisplayName } from "./model";
import { OUTCOME_FILL, OUTCOME_TEXT } from "./styles";

const RULE_BADGE: Record<RuleOutcome, "destructive" | "warning"> = {
  block: "destructive",
  clarify: "warning",
};

const TOKEN_CLASS: Record<string, string> = {
  ref: "bg-surface-tertiary-default text-foreground",
  block: "text-destructive",
  clarify: "text-warning",
  abstain: "text-muted-foreground",
  string: "text-[color:var(--text-link-primary)]",
  plain: "text-[color:var(--text-default)]",
};

/** The built-in guardrail as rules: statement, contract, labels, and session hits. */
export function GuardrailView({
  guardrail,
  analysis,
  sessionName,
  selectedRuleId,
  onSelectRule,
  onOpenCall,
}: {
  guardrail: IntentGuardrail;
  analysis: IntentSessionAnalysis | null;
  sessionName: string | undefined;
  selectedRuleId: string | null;
  onSelectRule: (id: string) => void;
  /** Switches to Replay with the call selected. */
  onOpenCall: (call: { messageId: string; toolCallId: string }) => void;
}): JSX.Element {
  const [filter, setFilter] = useState<RuleFilter>("all");
  const [labelFilter, setLabelFilter] = useState<string | null>(null);
  const rulesCard = useRef<HTMLElement>(null);
  const { contracts, questions } = guardrail;
  const selected =
    contracts.find((c) => c.id === selectedRuleId) ?? contracts[0];
  const selectedIndex = selected ? contracts.indexOf(selected) : -1;
  const counts = {
    block: contracts.filter((c) => ruleOutcome(c) === "block").length,
    clarify: contracts.filter((c) => ruleOutcome(c) === "clarify").length,
  };
  const toggleLabel = (key: string) =>
    setLabelFilter((current) => (current === key ? null : key));
  const matches = (c: IntentContract) =>
    (filter === "all" || ruleOutcome(c) === filter) &&
    (!labelFilter || labelsRead(c).includes(labelFilter));

  const onKeyDown = (e: KeyboardEvent<HTMLUListElement>) => {
    const dir = e.key === "ArrowDown" ? 1 : e.key === "ArrowUp" ? -1 : 0;
    if (!dir) return;
    e.preventDefault();
    const next = contracts[selectedIndex + dir];
    if (next) onSelectRule(next.id);
  };

  return (
    <div className="flex flex-col gap-6">
      <GuardrailSummary guardrail={guardrail} />

      <section
        ref={rulesCard}
        aria-labelledby="guardrail-rules"
        className="@container scroll-mt-4 border bg-card"
      >
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
          <div className="flex flex-wrap items-baseline gap-3">
            <h2 id="guardrail-rules" className="text-eyebrow">
              Rules
            </h2>
            <span className="text-xs text-muted-foreground">
              Each statement is enforced by one contract over the labels below.
            </span>
          </div>
          <div className="ml-auto flex items-center gap-2 whitespace-nowrap">
            {labelFilter && (
              <button
                type="button"
                onClick={() => setLabelFilter(null)}
                aria-label={`Clear filter: reads ${labelFilter}`}
                className="flex h-7 cursor-pointer items-center gap-1.5 border border-foreground px-2 font-mono text-xs"
              >
                reads {labelFilter}
                <X className="size-3" />
              </button>
            )}
            <SegmentedControl
              ariaLabel="Show rules"
              value={filter}
              onChange={setFilter}
              options={[
                { value: "all", label: "All" },
                { value: "block", label: `Blocks · ${counts.block}` },
                {
                  value: "clarify",
                  label: `Asks a person · ${counts.clarify}`,
                },
              ]}
            />
          </div>
        </div>

        <div className="grid @[900px]:min-h-[640px] @[900px]:grid-cols-[minmax(0,5fr)_minmax(0,6fr)]">
          <ul
            role="listbox"
            aria-label="Rules"
            aria-activedescendant={
              selected ? ruleOptionId(selected.id) : undefined
            }
            tabIndex={0}
            onKeyDown={onKeyDown}
            className="focus-visible:outline-1 focus-visible:outline-[var(--border-focus)]"
          >
            {contracts.map((c, i) => (
              <RuleRow
                key={c.id}
                contract={c}
                number={i + 1}
                selected={c.id === selected?.id}
                dimmed={!matches(c)}
                hits={analysis ? ruleHits(c, analysis.assessments) : null}
                onClick={() => onSelectRule(c.id)}
              />
            ))}
          </ul>
          {selected && (
            <RuleDetail
              contract={selected}
              number={selectedIndex + 1}
              total={contracts.length}
              questions={questions}
              analysis={analysis}
              sessionName={sessionName}
              contractNames={
                new Map(contracts.map((c) => [c.id, c.name] as const))
              }
              onToggleLabel={toggleLabel}
              onOpenCall={onOpenCall}
            />
          )}
        </div>
      </section>

      <LabelsTable
        contracts={contracts}
        questions={questions}
        activeLabel={labelFilter}
        onToggleLabel={toggleLabel}
        onSelectRule={(id) => {
          onSelectRule(id);
          rulesCard.current?.scrollIntoView({
            behavior: "smooth",
            block: "start",
          });
        }}
      />
    </div>
  );
}

function GuardrailSummary({
  guardrail,
}: {
  guardrail: IntentGuardrail;
}): JSX.Element {
  const steps = [
    {
      text: "Any rule blocks, or a rule errors →",
      result: "Block",
      className: "border-destructive-default bg-destructive-softest",
      accent: "text-destructive",
    },
    {
      text: "Otherwise any rule clarifies →",
      result: "Ask a person",
      className: "border-warning-default bg-warning-softest",
      accent: "text-warning",
    },
    {
      text: "Otherwise →",
      result: "Permit",
      className: "bg-surface-secondary-default",
      accent: "text-foreground",
    },
  ];
  return (
    <section
      aria-label="Guardrail"
      className="flex flex-wrap gap-x-8 gap-y-4 border bg-card px-5 py-4"
    >
      <div className="flex flex-col gap-1.5">
        <div className="flex items-center gap-2">
          <span className="font-mono text-sm text-foreground">
            {guardrail.id}
          </span>
          <Badge
            variant="neutral"
            className="font-mono whitespace-nowrap uppercase"
          >
            Built in · read-only
          </Badge>
        </div>
        <span className="font-mono text-[10px] text-muted-foreground">
          version {guardrail.version} · {guardrail.contracts.length} rules ·{" "}
          {guardrail.questions.length} labels
        </span>
      </div>
      <div className="flex min-w-0 flex-[1_1_32rem] flex-col gap-2">
        <span className="text-eyebrow">How a call is decided</span>
        <ol className="flex flex-wrap gap-1.5">
          {steps.map((s, i) => (
            <li
              key={s.result}
              className={cn(
                "flex flex-[1_1_10rem] items-baseline gap-2 border px-2.5 py-1.5 text-xs leading-[1.4] text-foreground",
                s.className,
              )}
            >
              <span className="font-mono text-[10px]">{i + 1}</span>
              <span>
                {s.text}{" "}
                <span className={cn("font-semibold", s.accent)}>
                  {s.result}
                </span>
              </span>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

const ruleOptionId = (id: string) => `guardrail-rule-${id}`;

function RuleRow({
  contract,
  number,
  selected,
  dimmed,
  hits,
  onClick,
}: {
  contract: IntentContract;
  number: number;
  selected: boolean;
  dimmed: boolean;
  hits: RuleHit[] | null;
  onClick: () => void;
}): JSX.Element {
  const outcome = ruleOutcome(contract);
  const first = hits?.[0];
  return (
    <li
      id={ruleOptionId(contract.id)}
      role="option"
      aria-selected={selected}
      onClick={onClick}
      className={cn(
        "grid cursor-pointer grid-cols-[28px_1fr_auto] gap-x-3 gap-y-2 border-b border-neutral-softest px-5 py-3.5",
        selected
          ? "bg-muted shadow-[inset_0_0_0_1px_var(--color-foreground)]"
          : "hover:bg-surface-secondary-default",
        dimmed && "opacity-30",
      )}
    >
      <span className="font-mono text-[11px] text-muted-foreground">
        {String(number).padStart(2, "0")}
      </span>
      <span className="text-sm leading-normal text-pretty text-foreground">
        {contract.statement}
      </span>
      <span className="row-span-2 text-right font-mono text-[11px]">
        {hits === null ? null : first ? (
          <span
            className={
              first.overridden
                ? "text-muted-foreground"
                : OUTCOME_TEXT[first.result === "clarify" ? "clarify" : "block"]
            }
          >
            #{first.callIndex + 1} ·{" "}
            {first.overridden ? "overridden" : first.result}
            {hits.length > 1 ? ` +${hits.length - 1}` : ""}
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </span>
      <span className="col-start-2 flex flex-wrap items-center gap-1.5 font-mono text-[11px]">
        <Badge variant={RULE_BADGE[outcome]} className="uppercase">
          {outcome}
        </Badge>
        <span className="text-muted-foreground">{contract.id}</span>
        <span className="text-muted-foreground">·</span>
        {labelsRead(contract).map((k) => (
          <span
            key={k}
            className="text-[color:var(--text-default)] underline decoration-neutral-default decoration-dotted underline-offset-2"
          >
            {k}
          </span>
        ))}
      </span>
    </li>
  );
}

function RuleDetail({
  contract,
  number,
  total,
  questions,
  analysis,
  sessionName,
  contractNames,
  onToggleLabel,
  onOpenCall,
}: {
  contract: IntentContract;
  number: number;
  total: number;
  questions: IntentLabelQuestion[];
  analysis: IntentSessionAnalysis | null;
  sessionName: string | undefined;
  contractNames: Map<string, string>;
  onToggleLabel: (key: string) => void;
  onOpenCall: (call: { messageId: string; toolCallId: string }) => void;
}): JSX.Element {
  const outcome = ruleOutcome(contract);
  const read = contract.conditions.flatMap((c) => (c.label ? [c.label] : []));
  const readQuestions = questions.filter((q) => read.includes(q.key));

  return (
    <div className="flex min-w-0 flex-col gap-6 border-l border-neutral-default bg-muted px-6 pt-5 pb-8">
      <div className="border bg-card shadow-[0_1px_2px_hsla(0_0%_0%/0.06)]">
        <span className={cn("block h-1", OUTCOME_FILL[outcome])} />
        <div className="flex flex-col gap-2 px-5 py-4">
          <div className="flex items-center justify-between gap-3">
            <span className="text-eyebrow">
              Rule {number} of {total} · {contract.id}
            </span>
            <Badge
              variant={RULE_BADGE[outcome]}
              size="lg"
              className="font-mono text-sm uppercase"
            >
              {outcome}
            </Badge>
          </div>
          <h3 className="font-mono text-xl break-all text-foreground">
            {contract.name}
          </h3>
          <p className="text-[15px] leading-[1.55] text-foreground">
            {contract.statement}
          </p>
        </div>
      </div>

      <Section title="Fires when">
        <div className="flex flex-col gap-1.5 border bg-card px-3.5 py-3">
          {conditionGroups(contract.conditions).map((group, g) => (
            <div key={g} className="flex items-baseline gap-2">
              <span className="w-7 shrink-0 font-mono text-[10px] text-muted-foreground uppercase">
                {g > 0 ? "and" : ""}
              </span>
              <div
                className={cn(
                  "flex min-w-0 flex-1 flex-col gap-1.5",
                  group.length > 1 &&
                    "border-l-2 border-neutral-default pl-2.5",
                )}
              >
                {group.map((c, i) => (
                  <div key={i} className="flex flex-wrap items-baseline gap-2">
                    {group.length > 1 && (
                      <span className="w-5 shrink-0 font-mono text-[10px] text-muted-foreground uppercase">
                        {i > 0 ? "or" : ""}
                      </span>
                    )}
                    <ConditionClause
                      condition={c}
                      onToggleLabel={onToggleLabel}
                    />
                  </div>
                ))}
              </div>
            </div>
          ))}
          <div className="mt-1 flex flex-wrap items-baseline gap-2 border-t border-neutral-softest pt-2">
            <span className="w-7 shrink-0 font-mono text-[10px] text-muted-foreground uppercase">
              Then
            </span>
            <span className={cn("text-sm", OUTCOME_TEXT[outcome])}>
              {OUTCOME_ACTION[outcome]}
            </span>
            <span className="text-sm text-muted-foreground">
              · otherwise abstains
            </span>
          </div>
        </div>
      </Section>

      {readQuestions.length > 0 && (
        <Section
          title="Labels it reads"
          aside="The evaluator answers these for every call"
        >
          {readQuestions.map((q) => {
            const checked = checkedOptions(contract, q);
            return (
              <div
                key={q.key}
                className="flex flex-col gap-2 border bg-card p-3.5"
              >
                <div className="flex items-baseline justify-between gap-3">
                  <span className="font-mono text-[13px] text-foreground">
                    {q.key}
                  </span>
                  <span className="font-mono text-[10px] text-muted-foreground uppercase">
                    {KIND_LABEL[q.kind] ?? q.kind}
                  </span>
                </div>
                <p className="text-[13px] leading-[1.55] text-[color:var(--text-default)]">
                  {q.instructions}
                </p>
                {q.options.length > 0 && (
                  <ul className="flex flex-col divide-y border-t text-xs">
                    {q.options.map((o) => {
                      const on = checked.size === 0 || checked.has(o.key);
                      return (
                        <li
                          key={o.key}
                          className={cn(
                            "grid grid-cols-[150px_1fr] gap-3 py-1.5",
                            !on && "opacity-45",
                          )}
                        >
                          <span className="flex items-center gap-1.5 font-mono">
                            {checked.has(o.key) && (
                              <span
                                className={cn(
                                  "inline-block size-1.5",
                                  OUTCOME_FILL[outcome],
                                )}
                              />
                            )}
                            {o.key}
                          </span>
                          <span className="text-muted-foreground">
                            {o.description}
                          </span>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </div>
            );
          })}
        </Section>
      )}

      <Section
        title="Expression"
        aside={
          <span className="font-mono text-[10px] text-muted-foreground">
            CEL
          </span>
        }
      >
        <pre className="border bg-card p-3 font-mono text-xs leading-[1.7] break-all whitespace-pre-wrap">
          {tokenizeExpression(contract.expression).map((t, i) => (
            <span key={i} className={TOKEN_CLASS[t.kind]}>
              {t.text}
            </span>
          ))}
        </pre>
      </Section>

      {analysis && (
        <SessionHits
          contract={contract}
          analysis={analysis}
          sessionName={sessionName}
          contractNames={contractNames}
          onOpenCall={onOpenCall}
        />
      )}
    </div>
  );
}

function SessionHits({
  contract,
  analysis,
  sessionName,
  contractNames,
  onOpenCall,
}: {
  contract: IntentContract;
  analysis: IntentSessionAnalysis;
  sessionName: string | undefined;
  contractNames: Map<string, string>;
  onOpenCall: (call: { messageId: string; toolCallId: string }) => void;
}): JSX.Element {
  const calls = analysis.assessments;
  const total = analysis.totals.calls;
  const partial = calls.length < total;
  const hits = ruleHits(contract, calls);
  const first = hits[0];
  const name = (h: RuleHit) => `#${h.callIndex + 1} ${toolDisplayName(h.call)}`;
  const refs = (hs: RuleHit[]) =>
    hs.map((h) => `#${h.callIndex + 1}`).join(", ");

  let sentence: string;
  if (!first) {
    sentence = partial
      ? `Abstained on the ${calls.length} of ${total} calls analyzed.`
      : `Abstained on all ${calls.length} ${calls.length === 1 ? "call" : "calls"}.`;
  } else if (hits.length > 1) {
    const fired = hits.filter((h) => h.result !== "error");
    const errored = hits.filter((h) => h.result === "error");
    sentence = [
      fired.length > 0 &&
        `Fired on ${fired.length} ${fired.length === 1 ? "call" : "calls"}: ${refs(fired)}.`,
      errored.length > 0 &&
        `Errored on ${refs(errored)}, which blocks ${errored.length === 1 ? "it" : "them"}.`,
    ]
      .filter(Boolean)
      .join(" ");
  } else if (first.result === "error") {
    sentence = `Errored on ${name(first)}, which blocks it.`;
  } else if (first.overridden) {
    const winners = first.call.decidedBy
      .map((id) => contractNames.get(id) ?? id)
      .join(", ");
    sentence = `Fired on ${name(first)}; the ${first.call.decision} from ${winners} took precedence.`;
  } else {
    sentence = `Fired on ${name(first)} and ${first.result === "block" ? "blocked it" : "asked a person"}.`;
  }

  return (
    <Section title="In the loaded session">
      <div className="flex flex-wrap items-center justify-between gap-3 border bg-card px-3.5 py-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="text-[13px] text-foreground">{sentence}</span>
          <span className="font-mono text-[11px] text-muted-foreground">
            {sessionName ?? "Session"} ·{" "}
            {partial
              ? `${calls.length} of ${total} calls analyzed`
              : `${calls.length} ${calls.length === 1 ? "call" : "calls"}`}
          </span>
        </div>
        {first && (
          <Button
            variant="secondary"
            size="sm"
            onClick={() =>
              onOpenCall({
                messageId: first.call.messageId,
                toolCallId: first.call.toolCallId,
              })
            }
          >
            <Button.Text>Open #{first.callIndex + 1} in replay →</Button.Text>
          </Button>
        )}
      </div>
    </Section>
  );
}

function LabelsTable({
  contracts,
  questions,
  activeLabel,
  onToggleLabel,
  onSelectRule,
}: {
  contracts: IntentContract[];
  questions: IntentLabelQuestion[];
  activeLabel: string | null;
  onToggleLabel: (key: string) => void;
  onSelectRule: (id: string) => void;
}): JSX.Element {
  const grid =
    "grid gap-x-6 gap-y-3 @[900px]:grid-cols-[240px_1fr_220px] grid-cols-[minmax(0,1fr)]";
  return (
    <section
      aria-labelledby="guardrail-labels"
      className="@container border bg-card"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-3 border-b px-5 py-3">
        <div className="flex flex-wrap items-baseline gap-3">
          <h2 id="guardrail-labels" className="text-eyebrow">
            Labels
          </h2>
          <span className="text-xs text-muted-foreground">
            Questions the evaluator answers for every tool call. Contracts read
            them as label.&lt;key&gt;.
          </span>
        </div>
        <span className="text-xs text-muted-foreground">
          Click a label to filter rules
        </span>
      </div>
      <div
        className={cn(
          grid,
          "hidden border-b px-5 py-2 @[900px]:grid [&>span]:text-eyebrow",
        )}
      >
        <span>Label</span>
        <span>Question</span>
        <span>Used by</span>
      </div>
      <ul>
        {questions.map((q) => {
          const usedBy = contracts.filter((c) => labelsRead(c).includes(q.key));
          const active = activeLabel === q.key;
          return (
            <li
              key={q.key}
              onClick={() => onToggleLabel(q.key)}
              className={cn(
                grid,
                "cursor-pointer border-b border-neutral-softest px-5 py-3.5",
                active
                  ? "bg-muted shadow-[inset_0_0_0_1px_var(--color-foreground)]"
                  : "hover:bg-surface-secondary-default",
              )}
            >
              <button
                type="button"
                aria-pressed={active}
                aria-label={`Filter rules that read ${q.key}`}
                onClick={(e) => {
                  e.stopPropagation();
                  onToggleLabel(q.key);
                }}
                className="flex cursor-pointer flex-col items-start gap-0.5 self-start text-left"
              >
                <span className="font-mono text-[13px] text-foreground">
                  {q.key}
                </span>
                <span className="font-mono text-[10px] text-muted-foreground uppercase">
                  {KIND_LABEL[q.kind] ?? q.kind}
                </span>
              </button>
              <span className="flex flex-col gap-2">
                <span className="text-[13px] leading-[1.55] text-[color:var(--text-default)]">
                  {q.instructions}
                </span>
                {q.options.length > 0 && (
                  <span className="flex flex-col gap-0.5 text-xs">
                    {q.options.map((o) => (
                      <span
                        key={o.key}
                        className="grid grid-cols-[170px_1fr] gap-3"
                      >
                        <span className="font-mono text-foreground">
                          {o.key}
                        </span>
                        <span className="text-muted-foreground">
                          {o.description}
                        </span>
                      </span>
                    ))}
                  </span>
                )}
              </span>
              <span className="flex flex-col gap-1">
                {usedBy.map((c) => (
                  <button
                    key={c.id}
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      onSelectRule(c.id);
                    }}
                    className="flex cursor-pointer items-center gap-1.5 text-left text-xs text-foreground hover:underline"
                  >
                    <span
                      className={cn(
                        "inline-block size-2 shrink-0",
                        OUTCOME_FILL[ruleOutcome(c)],
                      )}
                    />
                    {c.name}
                  </button>
                ))}
              </span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function Section({
  title,
  aside,
  children,
}: {
  title: string;
  aside?: ReactNode;
  children: ReactNode;
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

// Clauses joined by "or" form a group; groups are joined by "and".
function conditionGroups(conditions: IntentCondition[]): IntentCondition[][] {
  const groups: IntentCondition[][] = [];
  for (const c of conditions) {
    const last = groups.at(-1);
    if (last && c.join === "or") last.push(c);
    else groups.push([c]);
  }
  return groups;
}

function ConditionClause({
  condition: c,
  onToggleLabel,
}: {
  condition: IntentCondition;
  onToggleLabel: (label: string) => void;
}) {
  return c.pattern ? (
    <>
      <span className="text-sm text-muted-foreground">
        shell command matches
      </span>
      <code className="bg-muted px-1 font-mono text-xs break-all">
        {c.pattern}
      </code>
    </>
  ) : (
    <>
      <button
        type="button"
        onClick={() => onToggleLabel(c.label)}
        className="cursor-pointer font-mono text-[13px] text-foreground underline decoration-neutral-default decoration-dotted underline-offset-2"
      >
        {c.label}
      </button>
      <span className="text-sm text-muted-foreground">{c.op}</span>
      <span className="font-mono text-[13px] text-foreground">
        {c.values.join(" or ")}
      </span>
    </>
  );
}
