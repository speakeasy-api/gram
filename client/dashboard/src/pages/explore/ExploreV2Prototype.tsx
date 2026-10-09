// PROTOTYPE — throwaway. Explore v2: the query builder laid out as Datadog's
// is, on dummy data, to get a feel for it before changing the real builder.
// Nothing here calls the server or saves anything. Delete this file and its
// tab in Explore.tsx once the decision is made.
//
// Each query is two lines. The first is what is asked of: the query's letter,
// its dataset, and a search field holding its filters. The second hangs off
// the letter and reads as a sentence of joined segments: Show | Count of |
// all rows — by | user | + — limit to top | 10 — rollup | every | auto.
// Rollup is the time bucket a line, area or bar chart draws a point per:
// the query's grain (hour, day, week, month), picked from the window on auto.
import { AXIS, seriesForTheme, TOOLTIP } from "@/components/chart/palette";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
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
import { Input } from "@/components/ui/Input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { useIsDarkTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { XIcon } from "lucide-react";
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
import { WINDOW_PRESETS, type WindowPreset } from "./exploreModel";

// ── Dummy catalog ─────────────────────────────────────────────────────────

interface DummyDataset {
  name: string;
  /** What a row is, for "all <noun>" and the search field's placeholder. */
  noun: string;
  dimensions: string[];
  measures: string[];
}

const DATASETS: DummyDataset[] = [
  {
    name: "Sessions",
    noun: "sessions",
    dimensions: ["user", "surface", "model", "repo"],
    measures: ["duration_ms", "tokens", "cost_usd"],
  },
  {
    name: "Tool calls",
    noun: "tool calls",
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

// What is measured, as Datadog words it: the aggregation reads "<op> of".
const AGGS: { value: string; label: string }[] = [
  { value: "count", label: "Count of" },
  { value: "count_distinct", label: "Unique count of" },
  { value: "sum", label: "Sum of" },
  { value: "avg", label: "Avg of" },
  { value: "p95", label: "P95 of" },
  { value: "max", label: "Max of" },
];
const LIMITS = ["5", "10", "25", "100"];
// The grains the analytics API buckets by; auto picks one from the window.
const ROLLUPS: { value: string; label: string }[] = [
  { value: "auto", label: "auto" },
  { value: "hour", label: "1h" },
  { value: "day", label: "1d" },
  { value: "week", label: "1w" },
  { value: "month", label: "1mo" },
];
const LETTERS = "abcdefgh";

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
  filters: Filter[];
  agg: string;
  field: string;
  groupBy: string[];
  limit: string;
  rollup: string;
  alias: string;
}

interface State {
  queries: Query[];
  chart: ChartKind;
  window: WindowPreset;
}

const INITIAL: State = {
  queries: [
    {
      letter: "a",
      visible: true,
      dataset: "Sessions",
      filters: [
        { field: "surface", op: "is", values: ["claude_code", "cursor"] },
      ],
      agg: "count",
      field: "",
      groupBy: ["user"],
      limit: "10",
      rollup: "auto",
      alias: "",
    },
  ],
  chart: "line",
  window: "7d",
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

  return (
    <div className="flex flex-col gap-4">
      <section className="border-border bg-card flex flex-col gap-4 border p-3">
        {state.queries.map((query, index) => (
          <QueryBlock
            key={query.letter}
            query={query}
            canRemove={state.queries.length > 1}
            onChange={(next) => setQuery(index, next)}
            onRemove={() =>
              patch({ queries: state.queries.filter((_, i) => i !== index) })
            }
          />
        ))}
        <div className="flex items-center gap-1">
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
    </div>
  );
}

// ── One query: what is asked of, then the sentence under it ───────────────

function QueryBlock({
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
  const targets =
    query.agg === "count_distinct" ? dataset.dimensions : dataset.measures;

  return (
    <div className={cn("flex flex-col", !query.visible && "opacity-50")}>
      {/* Line one: letter, dataset, and the query's filters as a search. */}
      <div className="flex items-center gap-2">
        <div className="flex min-w-0 flex-1 items-stretch">
          <button
            type="button"
            title={query.visible ? "Hide this query" : "Show this query"}
            onClick={() => onChange({ visible: !query.visible })}
            className={cn(
              "flex size-8 shrink-0 items-center justify-center font-mono text-sm",
              query.visible
                ? "bg-primary text-primary-foreground"
                : "bg-muted text-muted-foreground",
            )}
          >
            {query.letter}
          </button>
          <Select
            value={query.dataset}
            onValueChange={(name) => {
              const next = datasetOf(name);
              onChange({
                dataset: name,
                field: counts ? "" : (next.measures[0] ?? ""),
                groupBy: query.groupBy.filter((g) =>
                  next.dimensions.includes(g),
                ),
                filters: query.filters.filter((f) =>
                  next.dimensions.includes(f.field),
                ),
              });
            }}
          >
            <SelectTrigger
              size="sm"
              aria-label="Dataset"
              className="w-36 shrink-0 border-l-0"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {DATASETS.map((d) => (
                <SelectItem key={d.name} value={d.name}>
                  {d.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <SearchField
            dataset={dataset}
            filters={query.filters}
            onChange={(filters) => onChange({ filters })}
          />
        </div>
        <Button
          variant="tertiary"
          size="sm"
          icon="code"
          aria-label="Edit as text (not in the prototype)"
        />
        <AliasInput
          value={query.alias}
          onChange={(alias) => onChange({ alias })}
        />
        {canRemove ? (
          <Button
            variant="tertiary"
            size="sm"
            icon="x"
            aria-label="Remove query"
            onClick={onRemove}
          />
        ) : null}
      </div>

      {/* Line two hangs off the letter: the sentence of joined segments. */}
      <div className="flex">
        <span
          aria-hidden
          className="border-border ml-4 h-6 w-4 shrink-0 border-b border-l"
        />
        <div className="flex flex-wrap items-center pt-2">
          <Segments>
            <Label>Show</Label>
            <ValueSelect
              label="Aggregation"
              value={query.agg}
              options={AGGS}
              tone="measure"
              onSelect={(agg) =>
                onChange({
                  agg,
                  field:
                    agg === "count"
                      ? ""
                      : agg === "count_distinct"
                        ? (dataset.dimensions[0] ?? "")
                        : dataset.measures.includes(query.field)
                          ? query.field
                          : (dataset.measures[0] ?? ""),
                })
              }
            />
            {counts ? (
              <Cell tone="measure">all {dataset.noun}</Cell>
            ) : (
              <ValueSelect
                label="Measure field"
                value={query.field}
                options={targets.map((t) => ({ value: t, label: t }))}
                tone="measure"
                mono
                onSelect={(field) => onChange({ field })}
              />
            )}
          </Segments>
          <Joint />
          <Segments>
            <Label>by</Label>
            {query.groupBy.length === 0 ? (
              <Cell tone="group">(Everything)</Cell>
            ) : null}
            {query.groupBy.map((group) => (
              <Cell key={group} tone="group" mono>
                {group}
                <button
                  type="button"
                  aria-label={`Stop grouping by ${group}`}
                  onClick={() =>
                    onChange({
                      groupBy: query.groupBy.filter((g) => g !== group),
                    })
                  }
                  className="text-muted-foreground hover:text-foreground ml-1.5 flex items-center"
                >
                  <XIcon className="size-3" />
                </button>
              </Cell>
            ))}
            {free.length > 0 && query.groupBy.length < 3 ? (
              <Select
                value=""
                onValueChange={(group) =>
                  onChange({ groupBy: [...query.groupBy, group] })
                }
              >
                <SelectTrigger
                  size="sm"
                  aria-label="Add a group"
                  className="h-full w-8 justify-center border-0 px-0 [&>svg:last-child]:hidden"
                >
                  <Icon name="plus" className="size-4" />
                </SelectTrigger>
                <SelectContent>
                  {free.map((d) => (
                    <SelectItem key={d} value={d} className="font-mono">
                      {d}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}
          </Segments>
          {query.groupBy.length > 0 ? (
            <>
              <Joint />
              <Segments>
                <Label>limit to top</Label>
                <ValueSelect
                  label="Limit"
                  value={query.limit}
                  options={LIMITS.map((l) => ({ value: l, label: l }))}
                  onSelect={(limit) => onChange({ limit })}
                />
              </Segments>
            </>
          ) : null}
          <Joint />
          <Segments>
            <Label>rollup</Label>
            <Cell>every</Cell>
            <ValueSelect
              label="Rollup"
              value={query.rollup}
              options={ROLLUPS.map((r) =>
                r.value === "auto" ? { ...r, label: "1h (auto)" } : r,
              )}
              onSelect={(rollup) => onChange({ rollup })}
            />
          </Segments>
        </div>
      </div>
    </div>
  );
}

// ── Segment pieces ────────────────────────────────────────────────────────

type Tone = "plain" | "measure" | "group";

const TONES: Record<Tone, string> = {
  plain: "bg-card",
  measure: "bg-warning-softest",
  group: "bg-destructive-softest",
};

/** Joined cells: one bordered group, divided. */
function Segments({ children }: { children: ReactNode }): JSX.Element {
  return (
    <div className="border-border divide-border flex h-8 items-stretch divide-x border">
      {children}
    </div>
  );
}

/** The short line joining two segment groups. */
function Joint(): JSX.Element {
  return <span aria-hidden className="bg-border h-px w-2.5 shrink-0" />;
}

/** A word of the sentence: muted, not editable. */
function Label({ children }: { children: ReactNode }): JSX.Element {
  return (
    <span className="bg-muted text-muted-foreground flex items-center px-2.5 text-sm">
      {children}
    </span>
  );
}

/** A value that is not a choice of its own: "all sessions", "every". */
function Cell({
  children,
  tone = "plain",
  mono = false,
}: {
  children: ReactNode;
  tone?: Tone;
  mono?: boolean;
}): JSX.Element {
  return (
    <span
      className={cn(
        "flex items-center px-2.5 text-sm",
        TONES[tone],
        mono && "font-mono",
      )}
    >
      {children}
    </span>
  );
}

/** A value picked from a list: the design system's select, as a cell. */
function ValueSelect({
  label,
  value,
  options,
  tone = "plain",
  mono = false,
  onSelect,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  tone?: Tone;
  mono?: boolean;
  onSelect: (value: string) => void;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onSelect}>
      <SelectTrigger
        size="sm"
        aria-label={label}
        className={cn(
          "h-full w-auto gap-1.5 border-0 px-2.5 hover:brightness-95",
          TONES[tone],
          mono && "font-mono",
        )}
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem
            key={option.value}
            value={option.value}
            className={mono ? "font-mono" : undefined}
          >
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// ── The query's filters, as a search field ────────────────────────────────

function SearchField({
  dataset,
  filters,
  onChange,
}: {
  dataset: DummyDataset;
  filters: Filter[];
  onChange: (filters: Filter[]) => void;
}): JSX.Element {
  return (
    <div className="border-input flex min-h-8 min-w-0 flex-1 items-stretch border border-l-0">
      <span className="border-input text-muted-foreground flex w-8 shrink-0 items-center justify-center border-r">
        <Icon name="search" className="size-3.5" />
      </span>
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1 px-1.5 py-0.5">
        {filters.map((filter, index) => (
          <FilterEditor
            key={index}
            fields={dataset.dimensions}
            initial={filter}
            onDone={(next) =>
              onChange(filters.map((f, i) => (i === index ? next : f)))
            }
            trigger={
              <FilterPill
                filter={filter}
                onRemove={() => onChange(filters.filter((_, i) => i !== index))}
              />
            }
          />
        ))}
        <FilterEditor
          fields={dataset.dimensions}
          onDone={(filter) => onChange([...filters, filter])}
          stretch
          trigger={
            <button
              type="button"
              aria-label="Add a filter"
              className="text-muted-foreground hover:text-foreground h-6 w-full min-w-40 px-1 text-left text-sm"
            >
              {filters.length === 0 ? `Filter your ${dataset.noun}` : ""}
            </button>
          }
        />
      </div>
    </div>
  );
}

function FilterPill({
  filter,
  onRemove,
}: {
  filter: Filter;
  onRemove: () => void;
}): JSX.Element {
  return (
    <Badge
      variant="neutral"
      size="lg"
      className="max-w-80 cursor-pointer normal-case"
    >
      <Badge.Text className="min-w-0 truncate font-mono text-xs [text-box-trim:none]">
        {filter.op === "is not" ? "-" : ""}
        {filter.field}:{filter.values.join(",")}
      </Badge.Text>
      <Badge.RightIcon>
        <button
          type="button"
          aria-label={`Remove ${filter.field} filter`}
          onClick={(event) => {
            event.stopPropagation();
            onRemove();
          }}
          className="flex size-3 cursor-pointer items-center justify-center hover:opacity-70 focus:outline-none focus-visible:ring-1"
        >
          <XIcon className="size-3" />
        </button>
      </Badge.RightIcon>
    </Badge>
  );
}

// Field, then operator and values, in one popover.
function FilterEditor({
  fields,
  initial,
  onDone,
  trigger,
  stretch = false,
}: {
  fields: string[];
  initial?: Filter;
  onDone: (filter: Filter) => void;
  trigger: ReactNode;
  stretch?: boolean;
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
      <PopoverTrigger asChild>
        <span className={cn("inline-flex", stretch && "flex-1")}>
          {trigger}
        </span>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0" align="start">
        {draft === null ? (
          <Command>
            <CommandInput placeholder="Filter by…" />
            <CommandList>
              <CommandEmpty>No field matches.</CommandEmpty>
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
          <div className="flex flex-col gap-3 p-3">
            <div className="flex items-center justify-between gap-2">
              <span className="font-mono text-sm">{draft.field}</span>
              <SegmentedControl<Filter["op"]>
                value={draft.op}
                onChange={(op) => setDraft({ ...draft, op })}
                options={[
                  { value: "is", label: "is" },
                  { value: "is not", label: "is not" },
                ]}
                className="h-7"
              />
            </div>
            <div className="flex max-h-56 flex-col gap-0.5 overflow-y-auto">
              {(VALUES[draft.field] ?? []).map((value) => {
                const picked = draft.values.includes(value);
                return (
                  <label
                    key={value}
                    className="hover:bg-muted flex cursor-pointer items-center gap-2 px-1.5 py-1 font-mono text-sm"
                  >
                    <Checkbox
                      checked={picked}
                      onCheckedChange={() =>
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
      <Button variant="tertiary" size="sm" onClick={() => setEditing(true)}>
        as…
      </Button>
    );
  }
  return (
    <Input
      autoFocus={editing}
      value={value}
      placeholder="Name"
      aria-label="Query name"
      onChange={onChange}
      onBlur={() => setEditing(false)}
      className="h-8 w-32"
    />
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
  onWindow: (window: WindowPreset) => void;
  onRun: () => void;
}): JSX.Element {
  const data = useMemo(() => dummyResult(state), [state]);
  return (
    <section className="border-border bg-card flex flex-col border">
      <div className="border-border flex flex-wrap items-center gap-3 border-b px-3 py-2">
        <span className="text-eyebrow">Results</span>
        <SegmentedControl<ChartKind>
          value={draft.chart}
          onChange={onChart}
          className="h-8"
          options={CHARTS.map((chart) => ({
            value: chart.kind,
            tooltip: chart.label,
            label: (
              <Icon
                name={chart.icon}
                className="size-4"
                aria-label={chart.label}
              />
            ),
          }))}
        />
        <div className="ml-auto flex items-center gap-2">
          {changed ? (
            <span className="text-muted-foreground text-xs">
              Changed since the last run.
            </span>
          ) : null}
          <TimeRangePicker
            preset={draft.window}
            availablePresets={WINDOW_PRESETS}
            onPresetChange={onWindow}
            className="h-8 py-1"
          />
          <Button variant="primary" size="sm" icon="play" onClick={onRun}>
            Run query
          </Button>
        </div>
      </div>
      <div className="h-80 p-4">
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
  const dark = useIsDarkTheme();
  const colors = seriesForTheme(dark);
  const grid = dark ? AXIS.gridDark : AXIS.grid;
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
        <span className="text-display-sm font-thin tabular-nums">
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
                  className="h-full"
                  style={{
                    width: `${(row.value / max) * 100}%`,
                    background: colors[0],
                  }}
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
  const tick = { fontSize: 11, fill: AXIS.label };
  const axes = (
    <>
      <CartesianGrid stroke={grid} vertical={false} />
      <XAxis dataKey="t" tick={tick} stroke={grid} />
      <YAxis tick={tick} stroke={grid} />
      <Tooltip
        contentStyle={{
          background: TOOLTIP.backgroundColor,
          border: `1px solid ${TOOLTIP.borderColor}`,
          borderRadius: TOOLTIP.cornerRadius,
          fontSize: 12,
        }}
        labelStyle={{ color: TOOLTIP.titleColor }}
        itemStyle={{ padding: 0 }}
      />
    </>
  );
  const color = (i: number) => colors[i % colors.length];
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
              stroke={color(i)}
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
              stroke={color(i)}
              fill={color(i)}
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
              fill={color(i)}
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
  const visible = state.queries.filter((q) => q.visible);
  const series: string[] = [];
  const seeds: string[] = [];
  for (const query of visible) {
    const agg = AGGS.find((a) => a.value === query.agg)?.label ?? query.agg;
    const name =
      query.alias ||
      `${query.letter}: ${agg} ${query.field || `all ${datasetOf(query.dataset).noun}`}`;
    const key = JSON.stringify(query.filters) + query.rollup;
    const group = query.groupBy[0];
    if (!group) {
      series.push(name);
      seeds.push(name + key);
      continue;
    }
    for (const value of (VALUES[group] ?? []).slice(0, Number(query.limit))) {
      const label = visible.length > 1 ? `${query.letter} · ${value}` : value;
      series.push(label);
      seeds.push(label + key);
    }
  }

  const buckets = 24;
  const points: Record<string, number | string>[] = [];
  const randoms = seeds.map((s) => seeded(s + state.window));
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
