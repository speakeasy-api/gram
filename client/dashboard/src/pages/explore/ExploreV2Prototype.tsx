// PROTOTYPE — throwaway. Explore v2: a Datadog-style compact query builder,
// on dummy data, to get a feel for the layout before changing the real
// builder. Nothing here calls the server or saves anything. Delete this file
// and its tab in Explore.tsx once the decision is made.
import { Button } from "@/components/ui/Button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import { Icon } from "@/components/ui/Icon";
import type { IconName } from "@/components/ui/Icon/names";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { cn } from "@/lib/utils";
import { useMemo, useState, type JSX, type ReactNode } from "react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

// ── Dummy catalog ─────────────────────────────────────────────────────────

interface DummyDataset {
  name: string;
  dimensions: string[];
  measures: string[];
}

const DATASETS: DummyDataset[] = [
  {
    name: "sessions",
    dimensions: ["user", "surface", "model", "repo"],
    measures: ["duration_ms", "tokens", "cost_usd"],
  },
  {
    name: "tool_calls",
    dimensions: ["tool", "server", "user", "status"],
    measures: ["latency_ms", "payload_bytes"],
  },
];

const VALUES: Record<string, string[]> = {
  user: ["alice", "bob", "carol", "dave", "erin", "frank"],
  surface: ["claude_code", "cursor", "codex", "desktop"],
  model: ["opus", "sonnet", "haiku", "gpt-5"],
  repo: ["web", "api", "infra", "docs"],
  tool: ["read_file", "search", "run_tests", "create_pr", "query_db"],
  server: ["github", "linear", "postgres", "slack"],
  status: ["ok", "error", "timeout"],
};

const AGGS = ["count", "count distinct", "sum", "avg", "p50", "p95", "max"];
const WINDOWS = ["Past 15m", "Past 1h", "Past 1d", "Past 7d", "Past 30d"];
const LIMITS = ["5", "10", "25", "100"];
const LETTERS = "abcdefgh";
const COLORS = [
  "#4f46e5",
  "#0891b2",
  "#d97706",
  "#db2777",
  "#16a34a",
  "#64748b",
];

type ChartKind =
  | "line"
  | "area"
  | "bar"
  | "stacked_bar"
  | "ranked"
  | "table"
  | "number";

const CHARTS: { kind: ChartKind; label: string; icon: IconName }[] = [
  { kind: "line", label: "Line", icon: "chart-line" },
  { kind: "area", label: "Area", icon: "chart-area" },
  { kind: "bar", label: "Bar", icon: "chart-column" },
  { kind: "stacked_bar", label: "Stacked bar", icon: "chart-column-stacked" },
  { kind: "ranked", label: "Ranked", icon: "list-ordered" },
  { kind: "table", label: "Table", icon: "table" },
  { kind: "number", label: "Number", icon: "hash" },
];

// ── State ─────────────────────────────────────────────────────────────────

interface Filter {
  field: string;
  op: "is" | "is not";
  values: string[];
}

interface Query {
  letter: string;
  visible: boolean;
  dataset: string;
  agg: string;
  field: string;
  groupBy: string[];
  limit: string;
  alias: string;
}

interface State {
  filters: Filter[];
  queries: Query[];
  formulas: string[];
  chart: ChartKind;
  window: string;
}

const INITIAL: State = {
  filters: [{ field: "surface", op: "is", values: ["claude_code", "cursor"] }],
  queries: [
    {
      letter: "a",
      visible: true,
      dataset: "sessions",
      agg: "count",
      field: "",
      groupBy: ["user"],
      limit: "10",
      alias: "",
    },
  ],
  formulas: [],
  chart: "line",
  window: "Past 7d",
};

function datasetOf(name: string): DummyDataset {
  return DATASETS.find((d) => d.name === name) ?? DATASETS[0]!;
}

// ── Page ──────────────────────────────────────────────────────────────────

export function ExploreV2Prototype(): JSX.Element {
  const [state, setState] = useState<State>(INITIAL);
  const [ran, setRan] = useState<State>(INITIAL);
  const changed = JSON.stringify(state) !== JSON.stringify(ran);

  const patch = (next: Partial<State>) => setState({ ...state, ...next });
  const setQuery = (index: number, next: Partial<Query>) =>
    patch({
      queries: state.queries.map((q, i) =>
        i === index ? { ...q, ...next } : q,
      ),
    });
  // Every dimension any query can be filtered by.
  const filterFields = [
    ...new Set(state.queries.flatMap((q) => datasetOf(q.dataset).dimensions)),
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="border-warning-default bg-warning-softest text-warning-default border border-dashed px-3 py-1.5 font-mono text-xs uppercase">
        Prototype · dummy data · nothing is saved
      </div>

      <section className="border-border bg-card flex flex-col border">
        <FilterBar
          fields={filterFields}
          filters={state.filters}
          onChange={(filters) => patch({ filters })}
        />
        <div className="flex flex-col gap-1 px-3 py-2">
          {state.queries.map((query, index) => (
            <QueryRow
              key={query.letter}
              query={query}
              canRemove={state.queries.length > 1}
              onChange={(next) => setQuery(index, next)}
              onRemove={() =>
                patch({ queries: state.queries.filter((_, i) => i !== index) })
              }
            />
          ))}
          {state.formulas.map((formula, index) => (
            <div key={index} className="flex items-center gap-2">
              <span className="text-muted-foreground flex size-6 items-center justify-center font-mono text-xs italic">
                ƒ
              </span>
              <input
                value={formula}
                onChange={(e) =>
                  patch({
                    formulas: state.formulas.map((f, i) =>
                      i === index ? e.target.value : f,
                    ),
                  })
                }
                className="bg-muted/50 focus:border-border h-7 w-64 border border-transparent px-2 font-mono text-sm outline-none"
              />
              <IconButton
                icon="x"
                label="Remove formula"
                onClick={() =>
                  patch({
                    formulas: state.formulas.filter((_, i) => i !== index),
                  })
                }
              />
            </div>
          ))}
          <div className="flex items-center gap-1 pt-1">
            <Button
              variant="tertiary"
              size="sm"
              icon="plus"
              onClick={() => {
                const last = state.queries[state.queries.length - 1]!;
                patch({
                  queries: [
                    ...state.queries,
                    {
                      ...last,
                      letter: LETTERS[state.queries.length] ?? "z",
                      alias: "",
                    },
                  ],
                });
              }}
            >
              Add query
            </Button>
            <Button
              variant="tertiary"
              size="sm"
              icon="plus"
              onClick={() =>
                patch({
                  formulas: [
                    ...state.formulas,
                    state.queries.length > 1 ? "a / b" : "a * 100",
                  ],
                })
              }
            >
              Add formula
            </Button>
          </div>
        </div>
      </section>

      <Results
        state={ran}
        draft={state}
        changed={changed}
        onChart={(chart) => {
          patch({ chart });
          // Chart and window are presentation: they apply at once.
          setRan({ ...ran, chart });
        }}
        onWindow={(window) => {
          patch({ window });
          setRan({ ...ran, window });
        }}
        onRun={() => setRan(state)}
      />

      <details className="text-muted-foreground text-xs">
        <summary className="cursor-pointer font-mono uppercase">
          Prototype state
        </summary>
        <pre className="bg-muted/40 mt-2 overflow-x-auto p-3">
          {JSON.stringify(state, null, 2)}
        </pre>
      </details>
    </div>
  );
}

// ── Filter bar: pills in one line ─────────────────────────────────────────

function FilterBar({
  fields,
  filters,
  onChange,
}: {
  fields: string[];
  filters: Filter[];
  onChange: (filters: Filter[]) => void;
}): JSX.Element {
  return (
    <div className="border-border flex min-h-10 flex-wrap items-center gap-1.5 border-b px-3 py-1.5">
      <Icon name="filter" className="text-muted-foreground size-3.5" />
      {filters.map((filter, index) => (
        <FilterPill
          key={index}
          fields={fields}
          filter={filter}
          onChange={(next) =>
            onChange(filters.map((f, i) => (i === index ? next : f)))
          }
          onRemove={() => onChange(filters.filter((_, i) => i !== index))}
        />
      ))}
      <FilterEditor
        fields={fields}
        onDone={(filter) => onChange([...filters, filter])}
        trigger={
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground inline-flex h-7 items-center gap-1 px-1.5 text-sm"
          >
            <Icon name="plus" className="size-3.5" />
            {filters.length === 0 ? "Filter everything…" : "Filter"}
          </button>
        }
      />
    </div>
  );
}

function FilterPill({
  fields,
  filter,
  onChange,
  onRemove,
}: {
  fields: string[];
  filter: Filter;
  onChange: (next: Filter) => void;
  onRemove: () => void;
}): JSX.Element {
  return (
    <span className="bg-muted inline-flex h-7 items-center font-mono text-xs">
      <FilterEditor
        fields={fields}
        initial={filter}
        onDone={onChange}
        trigger={
          <button type="button" className="flex h-full items-center gap-1 pl-2">
            <span>{filter.field}</span>
            <span className="text-muted-foreground">
              {filter.op === "is" ? ":" : "!:"}
            </span>
            <span className="max-w-48 truncate">
              {filter.values.join(", ")}
            </span>
          </button>
        }
      />
      <button
        type="button"
        aria-label={`Remove ${filter.field} filter`}
        onClick={onRemove}
        className="text-muted-foreground hover:text-foreground flex h-full items-center px-1.5"
      >
        <Icon name="x" className="size-3" />
      </button>
    </span>
  );
}

// Field, then operator and values, in one popover.
function FilterEditor({
  fields,
  initial,
  onDone,
  trigger,
}: {
  fields: string[];
  initial?: Filter;
  onDone: (filter: Filter) => void;
  trigger: ReactNode;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<Filter | null>(initial ?? null);
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) setDraft(initial ?? null);
      }}
    >
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent className="w-72 p-0" align="start">
        {draft === null ? (
          <Command>
            <CommandInput placeholder="Filter by…" />
            <CommandList>
              <CommandEmpty>No field.</CommandEmpty>
              <CommandGroup>
                {fields.map((field) => (
                  <CommandItem
                    key={field}
                    onSelect={() => setDraft({ field, op: "is", values: [] })}
                    className="font-mono"
                  >
                    {field}
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        ) : (
          <div className="flex flex-col gap-2 p-2">
            <div className="flex items-center gap-2 font-mono text-sm">
              <span>{draft.field}</span>
              {(["is", "is not"] as const).map((op) => (
                <button
                  key={op}
                  type="button"
                  onClick={() => setDraft({ ...draft, op })}
                  className={cn(
                    "px-1.5 py-0.5 text-xs",
                    draft.op === op
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted",
                  )}
                >
                  {op}
                </button>
              ))}
            </div>
            <div className="flex max-h-56 flex-col overflow-y-auto">
              {(VALUES[draft.field] ?? []).map((value) => {
                const picked = draft.values.includes(value);
                return (
                  <label
                    key={value}
                    className="hover:bg-muted flex cursor-pointer items-center gap-2 px-1.5 py-1 font-mono text-sm"
                  >
                    <input
                      type="checkbox"
                      checked={picked}
                      onChange={() =>
                        setDraft({
                          ...draft,
                          values: picked
                            ? draft.values.filter((v) => v !== value)
                            : [...draft.values, value],
                        })
                      }
                    />
                    {value}
                  </label>
                );
              })}
            </div>
            <Button
              size="sm"
              variant="primary"
              disabled={draft.values.length === 0}
              onClick={() => {
                onDone(draft);
                setOpen(false);
              }}
            >
              Apply
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

// ── One query, read as a sentence ─────────────────────────────────────────

function QueryRow({
  query,
  canRemove,
  onChange,
  onRemove,
}: {
  query: Query;
  canRemove: boolean;
  onChange: (next: Partial<Query>) => void;
  onRemove: () => void;
}): JSX.Element {
  const dataset = datasetOf(query.dataset);
  const counts = query.agg === "count";
  const free = dataset.dimensions.filter((d) => !query.groupBy.includes(d));
  return (
    <div
      className={cn(
        "flex flex-wrap items-center gap-1",
        !query.visible && "opacity-50",
      )}
    >
      <button
        type="button"
        title={query.visible ? "Hide this query" : "Show this query"}
        onClick={() => onChange({ visible: !query.visible })}
        className={cn(
          "flex size-6 items-center justify-center font-mono text-xs",
          query.visible
            ? "bg-primary text-primary-foreground"
            : "bg-muted text-muted-foreground",
        )}
      >
        {query.letter}
      </button>
      <Token
        value={query.agg}
        options={AGGS}
        onSelect={(agg) =>
          onChange({
            agg,
            field:
              agg === "count"
                ? ""
                : agg === "count distinct"
                  ? (dataset.dimensions[0] ?? "")
                  : query.field || (dataset.measures[0] ?? ""),
          })
        }
      />
      {counts ? null : (
        <Token
          value={query.field}
          options={
            query.agg === "count distinct"
              ? dataset.dimensions
              : dataset.measures
          }
          onSelect={(field) => onChange({ field })}
        />
      )}
      <Word>of</Word>
      <Token
        value={query.dataset}
        options={DATASETS.map((d) => d.name)}
        onSelect={(name) => {
          const next = datasetOf(name);
          onChange({
            dataset: name,
            field: counts ? "" : (next.measures[0] ?? ""),
            groupBy: query.groupBy.filter((g) => next.dimensions.includes(g)),
          });
        }}
      />
      <Word>by</Word>
      {query.groupBy.length === 0 ? (
        <span className="text-muted-foreground px-1 font-mono text-sm">
          everything
        </span>
      ) : null}
      {query.groupBy.map((group) => (
        <span
          key={group}
          className="bg-muted inline-flex h-7 items-center gap-1 pl-2 font-mono text-sm"
        >
          {group}
          <button
            type="button"
            aria-label={`Stop grouping by ${group}`}
            onClick={() =>
              onChange({ groupBy: query.groupBy.filter((g) => g !== group) })
            }
            className="text-muted-foreground hover:text-foreground flex h-full items-center px-1.5"
          >
            <Icon name="x" className="size-3" />
          </button>
        </span>
      ))}
      {free.length > 0 && query.groupBy.length < 3 ? (
        <Token
          value=""
          placeholder="+"
          options={free}
          onSelect={(group) => onChange({ groupBy: [...query.groupBy, group] })}
        />
      ) : null}
      {query.groupBy.length > 0 ? (
        <>
          <Word>top</Word>
          <Token
            value={query.limit}
            options={LIMITS}
            onSelect={(limit) => onChange({ limit })}
          />
        </>
      ) : null}
      <span className="ml-1 flex items-center gap-0.5">
        <IconButton icon="sigma" label="Functions (not in the prototype)" />
        <AliasInput
          value={query.alias}
          onChange={(alias) => onChange({ alias })}
        />
        {canRemove ? (
          <IconButton icon="x" label="Remove query" onClick={onRemove} />
        ) : null}
      </span>
    </div>
  );
}

function AliasInput({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  const [editing, setEditing] = useState(false);
  if (!editing && value === "") {
    return (
      <button
        type="button"
        onClick={() => setEditing(true)}
        className="text-muted-foreground hover:text-foreground h-7 px-1.5 font-mono text-xs"
      >
        as…
      </button>
    );
  }
  return (
    <input
      autoFocus={editing}
      value={value}
      placeholder="name"
      onChange={(e) => onChange(e.target.value)}
      onBlur={() => setEditing(false)}
      className="bg-muted/50 focus:border-border h-7 w-28 border border-transparent px-2 font-mono text-xs outline-none"
    />
  );
}

// ── Token: a select that reads as a word ──────────────────────────────────

function Token({
  value,
  placeholder,
  options,
  onSelect,
}: {
  value: string;
  placeholder?: string;
  options: string[];
  onSelect: (value: string) => void;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="bg-muted/50 hover:border-border hover:bg-muted inline-flex h-7 items-center gap-1 border border-transparent px-2 font-mono text-sm"
        >
          {value || (
            <span className="text-muted-foreground">{placeholder}</span>
          )}
          <Icon name="chevron-down" className="text-muted-foreground size-3" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-56 p-0" align="start">
        <Command>
          {options.length > 6 ? <CommandInput placeholder="Search…" /> : null}
          <CommandList>
            <CommandEmpty>Nothing matches.</CommandEmpty>
            <CommandGroup>
              {options.map((option) => (
                <CommandItem
                  key={option}
                  onSelect={() => {
                    onSelect(option);
                    setOpen(false);
                  }}
                  className="font-mono"
                >
                  {option}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function Word({ children }: { children: ReactNode }): JSX.Element {
  return (
    <span className="text-muted-foreground px-0.5 text-xs">{children}</span>
  );
}

function IconButton({
  icon,
  label,
  onClick,
}: {
  icon: IconName;
  label: string;
  onClick?: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      className="text-muted-foreground hover:text-foreground hover:bg-muted flex size-7 items-center justify-center"
    >
      <Icon name={icon} className="size-3.5" />
    </button>
  );
}

// ── Results: chart type, window and Run on the panel's header ─────────────

function Results({
  state,
  draft,
  changed,
  onChart,
  onWindow,
  onRun,
}: {
  state: State;
  draft: State;
  changed: boolean;
  onChart: (chart: ChartKind) => void;
  onWindow: (window: string) => void;
  onRun: () => void;
}): JSX.Element {
  const data = useMemo(() => dummyResult(state), [state]);
  return (
    <section className="border-border bg-card flex flex-col border">
      <div className="border-border flex flex-wrap items-center gap-2 border-b px-3 py-1.5">
        <div className="flex items-center">
          {CHARTS.map((chart) => (
            <button
              key={chart.kind}
              type="button"
              title={chart.label}
              aria-label={chart.label}
              aria-pressed={draft.chart === chart.kind}
              onClick={() => onChart(chart.kind)}
              className={cn(
                "flex size-7 items-center justify-center",
                draft.chart === chart.kind
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted",
              )}
            >
              <Icon name={chart.icon} className="size-4" />
            </button>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-2">
          {changed ? (
            <span className="text-muted-foreground text-xs">
              Query changed since the last run.
            </span>
          ) : null}
          <Token value={draft.window} options={WINDOWS} onSelect={onWindow} />
          <Button variant="primary" size="sm" icon="play" onClick={onRun}>
            Run
          </Button>
        </div>
      </div>
      <div className="h-80 p-3">
        <ResultBody chart={state.chart} data={data} />
      </div>
    </section>
  );
}

interface DummyResult {
  series: string[];
  points: Record<string, number | string>[];
  totals: { name: string; value: number }[];
}

function ResultBody({
  chart,
  data,
}: {
  chart: ChartKind;
  data: DummyResult;
}): JSX.Element {
  if (data.series.length === 0) {
    return (
      <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
        Every query is hidden.
      </div>
    );
  }
  if (chart === "number") {
    const total = data.totals.reduce((sum, t) => sum + t.value, 0);
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1">
        <span className="text-display-lg font-thin tabular-nums">
          {total.toLocaleString()}
        </span>
        <span className="text-muted-foreground text-xs">{data.series[0]}</span>
      </div>
    );
  }
  if (chart === "table" || chart === "ranked") {
    const max = Math.max(...data.totals.map((t) => t.value));
    return (
      <div className="flex h-full flex-col overflow-y-auto">
        {data.totals.map((row) => (
          <div
            key={row.name}
            className="border-border flex items-center gap-3 border-b py-1.5 text-sm"
          >
            <span className="w-40 truncate font-mono">{row.name}</span>
            {chart === "ranked" ? (
              <div className="bg-muted h-3 flex-1">
                <div
                  className="h-full bg-indigo-600"
                  style={{ width: `${(row.value / max) * 100}%` }}
                />
              </div>
            ) : (
              <span className="flex-1" />
            )}
            <span className="w-20 text-right tabular-nums">
              {row.value.toLocaleString()}
            </span>
          </div>
        ))}
      </div>
    );
  }
  const common = {
    data: data.points,
    margin: { top: 4, right: 8, bottom: 0, left: -12 },
  };
  const axes = (
    <>
      <CartesianGrid strokeDasharray="2 4" vertical={false} />
      <XAxis dataKey="t" tick={{ fontSize: 11 }} />
      <YAxis tick={{ fontSize: 11 }} />
      <Tooltip />
    </>
  );
  return (
    <ResponsiveContainer width="100%" height="100%">
      {chart === "line" ? (
        <LineChart {...common}>
          {axes}
          {data.series.map((s, i) => (
            <Line
              key={s}
              dataKey={s}
              dot={false}
              stroke={COLORS[i % COLORS.length]}
              strokeWidth={1.5}
            />
          ))}
        </LineChart>
      ) : chart === "area" ? (
        <AreaChart {...common}>
          {axes}
          {data.series.map((s, i) => (
            <Area
              key={s}
              dataKey={s}
              stroke={COLORS[i % COLORS.length]}
              fill={COLORS[i % COLORS.length]}
              fillOpacity={0.15}
            />
          ))}
        </AreaChart>
      ) : (
        <BarChart {...common}>
          {axes}
          {data.series.map((s, i) => (
            <Bar
              key={s}
              dataKey={s}
              fill={COLORS[i % COLORS.length]}
              stackId={chart === "stacked_bar" ? "stack" : undefined}
            />
          ))}
        </BarChart>
      )}
    </ResponsiveContainer>
  );
}

// Deterministic fake numbers, so the same query always draws the same chart.
function seeded(seed: string): () => number {
  let h = 2166136261;
  for (const c of seed) h = Math.imul(h ^ c.charCodeAt(0), 16777619);
  return () => {
    h = Math.imul(h ^ (h >>> 15), 2246822507);
    h = Math.imul(h ^ (h >>> 13), 3266489909);
    return ((h ^= h >>> 16) >>> 0) / 4294967296;
  };
}

function dummyResult(state: State): DummyResult {
  const filterKey = JSON.stringify(state.filters);
  const series: string[] = [];
  for (const query of state.queries) {
    if (!query.visible) continue;
    const name =
      query.alias ||
      `${query.letter}: ${query.agg}${query.field ? ` ${query.field}` : ""} of ${query.dataset}`;
    const group = query.groupBy[0];
    if (!group) {
      series.push(name);
      continue;
    }
    const values = VALUES[group] ?? [];
    for (const value of values.slice(0, Number(query.limit))) {
      series.push(
        state.queries.filter((q) => q.visible).length > 1
          ? `${query.letter} · ${value}`
          : value,
      );
    }
  }
  for (const formula of state.formulas) series.push(`ƒ ${formula}`);

  const buckets = 24;
  const points: Record<string, number | string>[] = [];
  const randoms = series.map((s) => seeded(s + filterKey + state.window));
  for (let b = 0; b < buckets; b++) {
    const point: Record<string, number | string> = { t: `${b}` };
    series.forEach((s, i) => {
      const base = 40 + 60 * (1 - i / Math.max(series.length, 1));
      point[s] = Math.round(
        base * (0.6 + 0.8 * randoms[i]!()) * (1 + 0.3 * Math.sin(b / 3)),
      );
    });
    points.push(point);
  }
  const totals = series
    .map((name) => ({
      name,
      value: points.reduce((sum, p) => sum + Number(p[name]), 0),
    }))
    .sort((a, b) => b.value - a.value);
  return { series, points, totals };
}
