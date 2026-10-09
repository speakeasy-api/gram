// PROTOTYPE — throwaway. Explore v2: a Datadog-style compact query builder,
// on dummy data, to get a feel for the layout before changing the real
// builder. Nothing here calls the server or saves anything. Delete this file
// and its tab in Explore.tsx once the decision is made.
//
// Three places for the filters, switchable via `?variant=` (A, B, C) from the
// bar at the bottom right or the arrow keys:
//   A — inline in each query's sentence (Datadog metrics' "from" scope)
//   B — one search-style box, aligned with the query rows (Datadog log search)
//   C — a facet panel down the left (Datadog's log explorer facets)
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
import { useEffect, useMemo, useState, type JSX, type ReactNode } from "react";
import { useSearchParams } from "react-router";
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
const LIMITS = ["5", "10", "25", "100"];
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
  agg: string;
  field: string;
  groupBy: string[];
  limit: string;
  alias: string;
  /** Variant A: the filters live on the query. */
  filters: Filter[];
}

interface State {
  filters: Filter[];
  queries: Query[];
  formulas: string[];
  chart: ChartKind;
  window: WindowPreset;
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
      filters: [
        { field: "surface", op: "is", values: ["claude_code", "cursor"] },
      ],
    },
  ],
  formulas: [],
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
  // Every dimension any query can be filtered by.
  const filterFields = [
    ...new Set(state.queries.flatMap((q) => datasetOf(q.dataset).dimensions)),
  ];

  const variant = useVariant();
  const setFilters = (filters: Filter[]) => patch({ filters });

  const builder = (
    <section className="border-border bg-card flex flex-col border">
      <div className="flex flex-col gap-1.5 px-3 py-2">
        {variant === "B" ? (
          <SearchFilterBox
            fields={filterFields}
            filters={state.filters}
            onChange={setFilters}
          />
        ) : null}
        {state.queries.map((query, index) => (
          <QueryRow
            key={query.letter}
            query={query}
            inlineFilters={variant === "A"}
            canRemove={state.queries.length > 1}
            onChange={(next) => setQuery(index, next)}
            onRemove={() =>
              patch({ queries: state.queries.filter((_, i) => i !== index) })
            }
          />
        ))}
        {state.formulas.map((formula, index) => (
          <div key={index} className="flex items-center gap-1.5">
            <span className="text-muted-foreground flex size-8 items-center justify-center font-mono text-xs italic">
              ƒ
            </span>
            <Input
              value={formula}
              aria-label="Formula"
              onChange={(value) =>
                patch({
                  formulas: state.formulas.map((f, i) =>
                    i === index ? value : f,
                  ),
                })
              }
              className="h-8 w-64 font-mono"
            />
            <Button
              variant="tertiary"
              size="sm"
              icon="x"
              aria-label="Remove formula"
              onClick={() =>
                patch({
                  formulas: state.formulas.filter((_, i) => i !== index),
                })
              }
            />
          </div>
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
  );

  const results = (
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
  );

  return (
    <>
      {variant === "C" ? (
        <div className="grid grid-cols-[14rem_minmax(0,1fr)] items-start gap-4">
          <FacetPanel
            fields={filterFields}
            filters={state.filters}
            onChange={setFilters}
          />
          <div className="flex min-w-0 flex-col gap-4">
            {builder}
            {results}
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          {builder}
          {results}
        </div>
      )}
      <VariantSwitcher current={variant} />
    </>
  );
}

// ── Variants ──────────────────────────────────────────────────────────────

type Variant = "A" | "B" | "C";
const VARIANTS: { key: Variant; name: string }[] = [
  { key: "A", name: "Filters inline per query" },
  { key: "B", name: "Search box" },
  { key: "C", name: "Facet panel" },
];

function useVariant(): Variant {
  const [params] = useSearchParams();
  const named = params.get("variant");
  return named === "B" || named === "C" ? named : "A";
}

// Not part of the design: flips between the variants, with ← and → too.
function VariantSwitcher({ current }: { current: Variant }): JSX.Element {
  const [, setParams] = useSearchParams();
  const index = VARIANTS.findIndex((v) => v.key === current);
  const go = (step: number) => {
    const next = VARIANTS[(index + step + VARIANTS.length) % VARIANTS.length]!;
    setParams(
      (prev) => {
        const out = new URLSearchParams(prev);
        out.set("variant", next.key);
        return out;
      },
      { replace: true },
    );
  };
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable)
      ) {
        return;
      }
      if (event.key === "ArrowLeft") go(-1);
      if (event.key === "ArrowRight") go(1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });
  return (
    <div className="fixed right-6 bottom-6 z-50 flex items-center gap-1 rounded-full bg-neutral-900 px-2 py-1.5 font-mono text-xs text-white shadow-lg">
      <button
        type="button"
        aria-label="Previous variant"
        onClick={() => go(-1)}
        className="px-2 hover:opacity-70"
      >
        ←
      </button>
      <span className="px-1">
        {current} · {VARIANTS[index]!.name}
      </span>
      <button
        type="button"
        aria-label="Next variant"
        onClick={() => go(1)}
        className="px-2 hover:opacity-70"
      >
        →
      </button>
    </div>
  );
}

// Variant B: one box that reads as a search field, pills inside it, on the
// query rows' grid so it lines up with the sentences under it.
function SearchFilterBox({
  fields,
  filters,
  onChange,
}: {
  fields: string[];
  filters: Filter[];
  onChange: (filters: Filter[]) => void;
}): JSX.Element {
  return (
    <div className="flex items-center gap-1.5">
      <span className="text-muted-foreground flex size-8 shrink-0 items-center justify-center">
        <Icon name="search" className="size-4" />
      </span>
      <div className="border-input flex min-h-8 flex-1 flex-wrap items-center gap-1.5 border px-1.5 py-1">
        {filters.map((filter, index) => (
          <FilterEditor
            key={index}
            fields={fields}
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
          fields={fields}
          onDone={(filter) => onChange([...filters, filter])}
          trigger={
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground min-w-40 flex-1 px-1 text-left text-sm"
            >
              {filters.length === 0
                ? "Filter every query: field is value…"
                : "Add a filter…"}
            </button>
          }
          stretch
        />
      </div>
    </div>
  );
}

// Variant C: every field, its values ticked to filter, down the left.
function FacetPanel({
  fields,
  filters,
  onChange,
}: {
  fields: string[];
  filters: Filter[];
  onChange: (filters: Filter[]) => void;
}): JSX.Element {
  const toggle = (field: string, value: string) => {
    const current = filters.find((f) => f.field === field);
    const values = current?.values.includes(value)
      ? current.values.filter((v) => v !== value)
      : [...(current?.values ?? []), value];
    const rest = filters.filter((f) => f.field !== field);
    onChange(
      values.length === 0
        ? rest
        : [...rest, { field, op: current?.op ?? "is", values }],
    );
  };
  return (
    <aside className="border-border bg-card sticky top-0 flex flex-col border">
      <div className="border-border flex items-center justify-between border-b px-3 py-2">
        <span className="text-eyebrow">Filters</span>
        {filters.length > 0 ? (
          <button
            type="button"
            onClick={() => onChange([])}
            className="text-muted-foreground hover:text-foreground text-xs"
          >
            Clear
          </button>
        ) : null}
      </div>
      {fields.map((field) => {
        const picked = filters.find((f) => f.field === field)?.values ?? [];
        return (
          <details
            key={field}
            open={picked.length > 0 || field === fields[0]}
            className="border-border border-b last:border-b-0"
          >
            <summary className="hover:bg-muted flex cursor-pointer items-center justify-between px-3 py-2 font-mono text-sm">
              {field}
              {picked.length > 0 ? (
                <Badge variant="neutral" size="md">
                  {picked.length}
                </Badge>
              ) : null}
            </summary>
            <div className="flex flex-col pb-2">
              {(VALUES[field] ?? []).map((value) => (
                <label
                  key={value}
                  className="hover:bg-muted flex cursor-pointer items-center gap-2 px-3 py-1 font-mono text-sm"
                >
                  <Checkbox
                    checked={picked.includes(value)}
                    onCheckedChange={() => toggle(field, value)}
                  />
                  <span className="flex-1 truncate">{value}</span>
                  <span className="text-muted-foreground text-xs tabular-nums">
                    {facetCount(field, value)}
                  </span>
                </label>
              ))}
            </div>
          </details>
        );
      })}
    </aside>
  );
}

// A made-up row count beside a facet value.
function facetCount(field: string, value: string): string {
  return Math.round(50 + seeded(field + value)() * 950).toLocaleString();
}

function FilterPill({
  filter,
  onRemove,
}: {
  filter: Filter;
  onRemove: () => void;
}): JSX.Element {
  return (
    <Pill onRemove={onRemove} removeLabel={`Remove ${filter.field} filter`}>
      {filter.field}
      <span className="text-muted-foreground">
        {filter.op === "is" ? " : " : " !: "}
      </span>
      {filter.values.join(", ")}
    </Pill>
  );
}

// A value in the builder: a neutral badge with its own remove control, as
// the filter value picker draws a picked value.
function Pill({
  children,
  onRemove,
  removeLabel,
  ...rest
}: {
  children: ReactNode;
  onRemove: () => void;
  removeLabel: string;
}): JSX.Element {
  return (
    <Badge
      variant="neutral"
      size="lg"
      className="max-w-80 cursor-pointer normal-case"
      {...rest}
    >
      <Badge.Text className="min-w-0 truncate font-mono text-xs [text-box-trim:none]">
        {children}
      </Badge.Text>
      <Badge.RightIcon>
        <button
          type="button"
          aria-label={removeLabel}
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

// ── One query, read as a sentence ─────────────────────────────────────────

function QueryRow({
  query,
  inlineFilters,
  canRemove,
  onChange,
  onRemove,
}: {
  query: Query;
  inlineFilters: boolean;
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
        "flex flex-wrap items-center gap-1.5",
        !query.visible && "opacity-50",
      )}
    >
      <button
        type="button"
        title={query.visible ? "Hide this query" : "Show this query"}
        onClick={() => onChange({ visible: !query.visible })}
        className={cn(
          "border-border flex size-8 shrink-0 items-center justify-center border font-mono text-xs uppercase",
          query.visible
            ? "bg-primary text-primary-foreground"
            : "bg-card text-muted-foreground",
        )}
      >
        {query.letter}
      </button>
      <Token
        label="Aggregation"
        value={query.agg}
        options={AGGS}
        className="font-mono uppercase"
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
          label="Measure field"
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
        label="Dataset"
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
      {inlineFilters ? (
        <>
          <Word>where</Word>
          {query.filters.map((filter, index) => (
            <FilterEditor
              key={index}
              fields={dataset.dimensions}
              initial={filter}
              onDone={(next) =>
                onChange({
                  filters: query.filters.map((f, i) =>
                    i === index ? next : f,
                  ),
                })
              }
              trigger={
                <FilterPill
                  filter={filter}
                  onRemove={() =>
                    onChange({
                      filters: query.filters.filter((_, i) => i !== index),
                    })
                  }
                />
              }
            />
          ))}
          <FilterEditor
            fields={dataset.dimensions}
            onDone={(filter) =>
              onChange({ filters: [...query.filters, filter] })
            }
            trigger={
              <Button
                variant="tertiary"
                size="sm"
                icon="plus"
                aria-label="Add a filter"
              >
                {query.filters.length === 0 ? "everything" : undefined}
              </Button>
            }
          />
        </>
      ) : null}
      <Word>by</Word>
      {query.groupBy.length === 0 ? <Word>everything</Word> : null}
      {query.groupBy.map((group) => (
        <Pill
          key={group}
          removeLabel={`Stop grouping by ${group}`}
          onRemove={() =>
            onChange({ groupBy: query.groupBy.filter((g) => g !== group) })
          }
        >
          {group}
        </Pill>
      ))}
      {free.length > 0 && query.groupBy.length < 3 ? (
        <Token
          label="Add a group"
          value=""
          placeholder="+ group"
          options={free}
          onSelect={(group) => onChange({ groupBy: [...query.groupBy, group] })}
        />
      ) : null}
      {query.groupBy.length > 0 ? (
        <>
          <Word>top</Word>
          <Token
            label="Limit"
            value={query.limit}
            options={LIMITS}
            onSelect={(limit) => onChange({ limit })}
          />
        </>
      ) : null}
      <span className="flex items-center gap-0.5">
        <Button
          variant="tertiary"
          size="sm"
          icon="sigma"
          aria-label="Functions (not in the prototype)"
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

// ── Token: the design system's small select, sized to its value ───────────

function Token({
  label,
  value,
  placeholder,
  options,
  className,
  onSelect,
}: {
  label: string;
  value: string;
  placeholder?: string;
  options: string[];
  className?: string;
  onSelect: (value: string) => void;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onSelect}>
      <SelectTrigger
        size="sm"
        aria-label={label}
        className={cn("w-auto min-w-0 gap-1.5", className)}
      >
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem key={option} value={option} className={className}>
            {option}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function Word({ children }: { children: ReactNode }): JSX.Element {
  return <span className="text-muted-foreground text-xs">{children}</span>;
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
  const colors = seriesForTheme(useIsDarkTheme());
  const grid = useIsDarkTheme() ? AXIS.gridDark : AXIS.grid;
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
  const filterKey = JSON.stringify([
    state.filters,
    state.queries.map((q) => q.filters),
  ]);
  const visible = state.queries.filter((q) => q.visible);
  const series: string[] = [];
  for (const query of visible) {
    const name =
      query.alias ||
      `${query.letter}: ${query.agg}${query.field ? ` ${query.field}` : ""} of ${query.dataset}`;
    const group = query.groupBy[0];
    if (!group) {
      series.push(name);
      continue;
    }
    for (const value of (VALUES[group] ?? []).slice(0, Number(query.limit))) {
      series.push(visible.length > 1 ? `${query.letter} · ${value}` : value);
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
