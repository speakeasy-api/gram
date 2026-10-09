import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { Info, XIcon } from "lucide-react";
import { useState, type JSX, type ReactNode } from "react";
import {
  CHART_TYPE_OPTIONS,
  completeMeasures,
  DEFAULT_LIMIT,
  dimensionFields,
  fieldsForOp,
  findDataset,
  hasChartShape,
  isRowsMode,
  MAX_DIMENSIONS,
  MAX_LIMIT,
  MAX_ROWS_LIMIT,
  measureAlias,
  measureLabel,
  opsForDataset,
  parseLimit,
  specForDataset,
  specGrain,
  WINDOW_PRESETS,
  type ChartType,
  type ExploreSpec,
  type FilterDraft,
  type Grain,
  type MeasureDraft,
  type MeasureOp,
} from "./exploreModel";
import { FilterRow } from "./FilterRow";

// The builder is laid out as Datadog's query editor is. The first line is
// what is asked of: the dataset, and the filters as one search field. The
// second hangs from the search icon and reads as a sentence of joined
// segments: Show | Count of | all rows — by | user | + — limit to top | 100
// | by | count — or, for a timeseries, rollup | every | 1h (auto). How the
// answer is drawn, over which window, and Run sit on the results panel's
// header (ResultsToolbar), outside the question.

// Radix Select cannot carry "" as an item value, so the "keep group order"
// choice gets a sentinel that never collides with a measure alias.
const GROUP_ORDER = "__group_order__";

/** How an aggregation reads at the start of the sentence: "Count of". */
const OP_LABELS: Record<MeasureOp, string> = {
  count: "Count of",
  count_distinct: "Unique count of",
  sum: "Sum of",
  avg: "Avg of",
  min: "Min of",
  max: "Max of",
  p50: "P50 of",
  p95: "P95 of",
  p99: "P99 of",
};

/** A timeseries bucket as the rollup segment names it. */
const GRAIN_LABELS: Record<Grain, string> = {
  none: "none",
  hour: "1h",
  day: "1d",
  week: "1w",
  month: "1mo",
};

const CHART_ICONS: Record<ChartType, IconName> = {
  line: "chart-line",
  area: "chart-area",
  bar: "chart-column",
  ranked: "list-ordered",
  table: "table",
  number: "hash",
};

function replaceAt<T>(list: T[], index: number, next: T): T[] {
  return list.map((item, current) => (current === index ? next : item));
}

function removeAt<T>(list: T[], index: number): T[] {
  return list.filter((_, current) => current !== index);
}

/**
 * Compose one Explore query. Every control is generated from the catalog:
 * the dataset picked reconfigures the fields, aggregations, and operators
 * the other clauses offer.
 */
export function QueryBuilder({
  datasets,
  spec,
  onChange,
  actions,
}: {
  datasets: AnalyticsDataset[];
  spec: ExploreSpec;
  onChange: (spec: ExploreSpec) => void;
  /** Controls at the end of the first line: the widget's save. */
  actions?: ReactNode;
}): JSX.Element {
  const dataset = findDataset(datasets, spec.dataset);
  const patch = (next: Partial<ExploreSpec>) => onChange({ ...spec, ...next });
  const changeDataset = (name: string) => {
    const next = findDataset(datasets, name);
    if (next) onChange(specForDataset(next, spec));
  };

  return (
    <section className="border-border bg-card flex items-start border p-3">
      {/* What is asked of: the query's letter and its dataset. */}
      <div className="flex shrink-0 items-stretch">
        <span
          aria-hidden
          className="bg-primary text-primary-foreground flex size-8 items-center justify-center font-mono text-sm"
        >
          a
        </span>
        <Select value={spec.dataset} onValueChange={changeDataset}>
          <SelectTrigger size="sm" className="w-44" aria-label="Dataset">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {datasets.map((candidate) => (
              <SelectItem key={candidate.name} value={candidate.name}>
                {candidate.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {dataset ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label="About this dataset"
                className="border-input text-muted-foreground hover:text-foreground focus-visible:ring-ring flex w-8 shrink-0 items-center justify-center border-y focus-visible:ring-2 focus-visible:outline-none"
              >
                <Info className="size-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom" align="start">
              <DatasetSummary dataset={dataset} />
            </TooltipContent>
          </Tooltip>
        ) : null}
      </div>

      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex items-center gap-2">
          <SearchField
            dataset={dataset}
            spec={spec}
            onChange={(filters) => patch({ filters })}
          />
          {actions ? (
            <div className="flex shrink-0 items-center gap-2">{actions}</div>
          ) : null}
        </div>
        {/* The sentence hangs from the search icon, so it starts where the
            filter text does. */}
        <div className="flex">
          <span
            aria-hidden
            className="border-border ml-4 h-6 w-4 shrink-0 border-b border-l"
          />
          <Sentence dataset={dataset} spec={spec} patch={patch} />
        </div>
      </div>
    </section>
  );
}

// ── The sentence: Show … by … then limit, or rollup ───────────────────────

function Sentence({
  dataset,
  spec,
  patch,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  patch: (next: Partial<ExploreSpec>) => void;
}): JSX.Element {
  const grouped = spec.chartType !== "number";
  const timeseries = hasChartShape(spec);
  const free = dimensionFields(dataset).filter(
    (field) => !spec.dimensions.includes(field.name),
  );
  const orderOptions = completeMeasures(spec.measures).map((measure) => ({
    value: measureAlias(measure),
    label: measureLabel(measure),
  }));

  // Order by names a measure alias. A measure edited away or removed would
  // leave the select holding a value its options no longer offer, so it
  // drops back to group order with the measure it named.
  const withMeasures = (measures: MeasureDraft[]): Partial<ExploreSpec> => ({
    measures,
    orderBy: completeMeasures(measures).map(measureAlias).includes(spec.orderBy)
      ? spec.orderBy
      : "",
  });
  const changeOp = (index: number, op: MeasureOp) => {
    const current = spec.measures[index]!;
    const keepsField = fieldsForOp(dataset, op).some(
      (field) => field.name === current.field,
    );
    patch(
      withMeasures(
        replaceAt(spec.measures, index, {
          op,
          field: keepsField ? current.field : "",
        }),
      ),
    );
  };
  const addMeasure = () =>
    patch({ measures: [...spec.measures, { op: "count", field: "" }] });

  return (
    <div className="flex flex-wrap items-center gap-y-2 pt-2">
      <Segments>
        <Word>Show</Word>
        {spec.measures.map((measure, index) => (
          <MeasureCells
            key={index}
            dataset={dataset}
            measure={measure}
            onOp={(op) => changeOp(index, op)}
            onField={(field) =>
              patch(
                withMeasures(
                  replaceAt(spec.measures, index, { ...measure, field }),
                ),
              )
            }
            onRemove={() => patch(withMeasures(removeAt(spec.measures, index)))}
          />
        ))}
        {spec.measures.length === 0 ? (
          // Nothing measured is still a question: the rows themselves.
          <Cell tone="measure">rows</Cell>
        ) : null}
        <AddCell
          label={
            spec.measures.length === 0 ? "Add measure" : "Add another measure"
          }
          onClick={addMeasure}
        />
      </Segments>

      <Joint />
      <Segments>
        <Word>by</Word>
        {!grouped ? (
          <Cell tone="group" title="A number chart has no breakdown">
            (Everything)
          </Cell>
        ) : spec.dimensions.length === 0 ? (
          <Cell tone="group">(Everything)</Cell>
        ) : null}
        {grouped
          ? spec.dimensions.map((dimension) => (
              <Cell key={dimension} tone="group" mono>
                {dimension}
                <button
                  type="button"
                  aria-label={`Stop grouping by ${dimension}`}
                  onClick={() =>
                    patch({
                      dimensions: spec.dimensions.filter(
                        (name) => name !== dimension,
                      ),
                    })
                  }
                  className="text-muted-foreground hover:text-foreground ml-1.5 flex items-center"
                >
                  <XIcon className="size-3" />
                </button>
              </Cell>
            ))
          : null}
        {grouped &&
        free.length > 0 &&
        spec.dimensions.length < MAX_DIMENSIONS ? (
          <Select
            key={spec.dimensions.join(",")}
            value=""
            onValueChange={(name) =>
              patch({ dimensions: [...spec.dimensions, name] })
            }
          >
            <SelectTrigger
              size="sm"
              aria-label="Group by"
              className="h-full w-8 justify-center border-0 px-0 [&>svg:last-child]:hidden"
            >
              <Icon name="plus" className="size-4" />
            </SelectTrigger>
            <SelectContent>
              {free.map((field) => (
                <SelectItem
                  key={field.name}
                  value={field.name}
                  description={field.description}
                  className="font-mono"
                >
                  {field.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
      </Segments>

      <Joint />
      {timeseries ? (
        // The bucket is chosen from the window, so the segment says which.
        <Segments>
          <Word>rollup</Word>
          <Cell>every</Cell>
          <Cell>{GRAIN_LABELS[specGrain(spec)]} (auto)</Cell>
        </Segments>
      ) : (
        // A timeseries is drawn in time order up to the server's cap, so
        // order and limit only apply to whole-window charts.
        <Segments>
          <Word>limit to top</Word>
          <Input
            type="number"
            min={1}
            max={isRowsMode(spec) ? MAX_ROWS_LIMIT : MAX_LIMIT}
            value={spec.limit === 0 ? "" : String(spec.limit)}
            onChange={(raw) =>
              patch({ limit: parseLimit(raw, isRowsMode(spec)) })
            }
            placeholder={String(DEFAULT_LIMIT)}
            aria-label="Limit"
            className="h-full w-20 border-0"
          />
          <Word>by</Word>
          <Select
            value={spec.orderBy === "" ? GROUP_ORDER : spec.orderBy}
            onValueChange={(value) =>
              patch({ orderBy: value === GROUP_ORDER ? "" : value })
            }
          >
            <SelectTrigger
              size="sm"
              aria-label="Order by"
              className="h-full w-auto gap-1.5 border-0 px-2.5"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={GROUP_ORDER}>group order</SelectItem>
              {orderOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label} (desc)
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Segments>
      )}
    </div>
  );
}

/** One measure in the Show segment: "Count of | all rows", or a field. */
function MeasureCells({
  dataset,
  measure,
  onOp,
  onField,
  onRemove,
}: {
  dataset: AnalyticsDataset | undefined;
  measure: MeasureDraft;
  onOp: (op: MeasureOp) => void;
  onField: (field: string) => void;
  onRemove: () => void;
}): JSX.Element {
  const targets = fieldsForOp(dataset, measure.op);
  const remove = (
    <button
      type="button"
      aria-label="Remove measure"
      onClick={onRemove}
      className="text-muted-foreground hover:text-foreground flex items-center pr-2"
    >
      <XIcon className="size-3" />
    </button>
  );
  return (
    <>
      <Select value={measure.op} onValueChange={(op) => onOp(op as MeasureOp)}>
        <SelectTrigger
          size="sm"
          aria-label="Aggregation"
          className={cn(VALUE_TRIGGER, TONES.measure)}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {opsForDataset(dataset).map((op) => (
            <SelectItem key={op} value={op}>
              {OP_LABELS[op]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {measure.op === "count" ? (
        <Cell tone="measure">
          all rows
          <span className="ml-1.5 flex">{remove}</span>
        </Cell>
      ) : (
        <span className={cn("flex items-stretch", TONES.measure)}>
          <Select value={measure.field} onValueChange={onField}>
            <SelectTrigger
              size="sm"
              aria-label="Measure field"
              className={cn(VALUE_TRIGGER, "font-mono", TONES.measure)}
            >
              <SelectValue placeholder="pick a field" />
            </SelectTrigger>
            <SelectContent>
              {targets.map((field) => (
                <SelectItem
                  key={field.name}
                  value={field.name}
                  description={field.unit ? `in ${field.unit}` : undefined}
                  className="font-mono"
                >
                  {field.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {remove}
        </span>
      )}
    </>
  );
}

// ── Segment pieces ────────────────────────────────────────────────────────

type Tone = "plain" | "measure" | "group";

const TONES: Record<Tone, string> = {
  plain: "bg-card",
  measure: "bg-warning-softest",
  group: "bg-destructive-softest",
};

/** A select drawn as a cell of a segment. */
const VALUE_TRIGGER = "h-full w-auto gap-1.5 border-0 px-2.5";

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
function Word({ children }: { children: ReactNode }): JSX.Element {
  return (
    <span className="bg-muted text-muted-foreground flex items-center px-2.5 text-sm whitespace-nowrap">
      {children}
    </span>
  );
}

/** A value that is not a choice of its own: "all rows", "every". */
function Cell({
  children,
  tone = "plain",
  mono = false,
  title,
}: {
  children: ReactNode;
  tone?: Tone;
  mono?: boolean;
  title?: string;
}): JSX.Element {
  return (
    <span
      title={title}
      className={cn(
        "flex items-center px-2.5 text-sm whitespace-nowrap",
        TONES[tone],
        mono && "font-mono",
      )}
    >
      {children}
    </span>
  );
}

/** The "+" closing a segment, which adds another of what it holds. */
function AddCell({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className="text-muted-foreground hover:text-foreground hover:bg-muted flex w-8 items-center justify-center"
    >
      <Icon name="plus" className="size-4" />
    </button>
  );
}

// ── The filters, as one search field of pills ─────────────────────────────

function SearchField({
  dataset,
  spec,
  onChange,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  onChange: (filters: FilterDraft[]) => void;
}): JSX.Element {
  const filters = spec.filters;
  // The pill whose editor is open; a filter just added opens on its own.
  const [open, setOpen] = useState<number | null>(null);
  const remove = (index: number) => {
    setOpen(null);
    onChange(removeAt(filters, index));
  };

  return (
    <div className="border-input flex min-h-8 min-w-0 flex-1 items-stretch border">
      <span className="border-input text-muted-foreground flex w-8 shrink-0 items-center justify-center border-r">
        <Icon name="search" className="size-3.5" />
      </span>
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1 px-1.5 py-0.5">
        {filters.map((filter, index) => (
          <FilterPill
            key={index}
            filter={filter}
            open={open === index}
            onOpenChange={(next) => setOpen(next ? index : null)}
            onRemove={() => remove(index)}
          >
            <FilterRow
              dataset={dataset}
              span={spec}
              filter={filter}
              onChange={(next) => onChange(replaceAt(filters, index, next))}
              onRemove={() => remove(index)}
            />
          </FilterPill>
        ))}
        <button
          type="button"
          aria-label="Add filter"
          onClick={() => {
            onChange([...filters, { field: "", operator: "in", values: [] }]);
            setOpen(filters.length);
          }}
          className="text-muted-foreground hover:text-foreground h-6 min-w-40 flex-1 px-1 text-left text-sm"
        >
          {filters.length === 0
            ? `Filter ${dataset?.name ?? "the dataset"}`
            : ""}
        </button>
      </div>
    </div>
  );
}

/**
 * A filter as a search term: field:value, or field:a,b for any of. The term
 * opens the filter's editor and the × beside it removes the filter: two
 * buttons side by side, since a control nested in another cannot be reached
 * by keyboard on its own.
 */
function FilterPill({
  filter,
  open,
  onOpenChange,
  onRemove,
  children,
}: {
  filter: FilterDraft;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onRemove: () => void;
  /** The filter's editor, shown under the pill while it is open. */
  children: ReactNode;
}): JSX.Element {
  const values = filter.values.length > 0 ? filter.values.join(",") : "…";
  const text = filter.field === "" ? "new filter" : `${filter.field}:${values}`;
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <Badge variant="neutral" size="lg" className="max-w-80 normal-case">
        <Badge.Text className="min-w-0 truncate font-mono text-xs [text-box-trim:none]">
          <PopoverTrigger asChild>
            <button
              type="button"
              className="w-full cursor-pointer truncate text-left focus:outline-none focus-visible:ring-1"
            >
              {text}
            </button>
          </PopoverTrigger>
        </Badge.Text>
        <Badge.RightIcon>
          <button
            type="button"
            aria-label={`Remove ${filter.field || "new"} filter`}
            onClick={onRemove}
            className="flex size-3 cursor-pointer items-center justify-center hover:opacity-70 focus:outline-none focus-visible:ring-1"
          >
            <XIcon className="size-3" />
          </button>
        </Badge.RightIcon>
      </Badge>
      <PopoverContent className="w-auto max-w-[44rem] p-2" align="start">
        {children}
      </PopoverContent>
    </Popover>
  );
}

// The dataset's one-line description, behind an info icon beside the picker
// so the picker itself carries no hover text.
function DatasetSummary({
  dataset,
}: {
  dataset: AnalyticsDataset;
}): JSX.Element {
  return <span className="block max-w-sm">{dataset.description}</span>;
}

// ── The results panel's header: how it is drawn, over when, and Run ───────

/**
 * Chart type, window and Run, drawn on the results panel's header: they
 * decide how and over when the question is answered, not what it asks.
 */
export function ResultsToolbar({
  spec,
  onChange,
  onRun,
  changed,
}: {
  spec: ExploreSpec;
  onChange: (spec: ExploreSpec) => void;
  /** Run the query the builder currently describes. */
  onRun: () => void;
  /** Whether the builder has moved on from the query the results answer. */
  changed: boolean;
}): JSX.Element {
  const patch = (next: Partial<ExploreSpec>) => onChange({ ...spec, ...next });
  return (
    <>
      <SegmentedControl<ChartType>
        value={spec.chartType}
        onChange={(chartType) => patch({ chartType })}
        className="h-8"
        options={CHART_TYPE_OPTIONS.map((option) => ({
          value: option.value,
          tooltip: option.label,
          label: (
            <>
              <Icon
                name={CHART_ICONS[option.value]}
                className="size-4"
                aria-hidden
              />
              <span className="sr-only">{option.label}</span>
            </>
          ),
        }))}
      />
      <div className="ml-auto flex items-center gap-2">
        {changed ? (
          <span className="text-muted-foreground text-xs">
            Changed since the last run.
          </span>
        ) : null}
        {/* The dashboard's own date picker: its presets, and a custom range
            typed, picked on the calendar, or brought by a page or a dragged
            chart. Picking a preset drops the range. The picker has no name
            of its own, so the group carries it. */}
        <div role="group" aria-label="Window">
          <TimeRangePicker
            className="h-8 py-1"
            preset={spec.range ? null : spec.window}
            customRange={
              spec.range
                ? {
                    from: new Date(spec.range.from),
                    to: new Date(spec.range.to),
                  }
                : null
            }
            customRangeLabel={spec.range?.label ?? null}
            availablePresets={WINDOW_PRESETS}
            onPresetChange={(window) => patch({ window, range: undefined })}
            onCustomRangeChange={(from, to, label) =>
              patch({
                range: {
                  from: from.getTime(),
                  to: to.getTime(),
                  ...(label ? { label } : {}),
                },
              })
            }
            onClearCustomRange={() => patch({ range: undefined })}
          />
        </div>
        <Button variant="primary" size="sm" icon="play" onClick={onRun}>
          Run query
        </Button>
      </div>
    </>
  );
}
