import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { JSX } from "react";
import { AddRowButton, BuilderField, ClauseRow } from "./ClauseRow";
import {
  CHART_TYPE_OPTIONS,
  completeMeasures,
  DEFAULT_LIMIT,
  dimensionFields,
  findDataset,
  MAX_DIMENSIONS,
  MAX_LIMIT,
  measureAlias,
  measureLabel,
  parseLimit,
  specForDataset,
  WINDOW_OPTIONS,
  type ChartType,
  type ExploreSpec,
  type FilterDraft,
  type MeasureDraft,
  type WindowPreset,
} from "./exploreModel";
import { FilterRow } from "./FilterRow";
import { MeasureRow } from "./MeasureRow";

// Radix Select cannot carry "" as an item value, so the "keep group order"
// choice gets a sentinel that never collides with a measure alias.
const GROUP_ORDER = "__group_order__";

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
  onRun,
  changed,
}: {
  datasets: AnalyticsDataset[];
  spec: ExploreSpec;
  onChange: (spec: ExploreSpec) => void;
  /** Run the query the builder currently describes. */
  onRun: () => void;
  /** Whether the builder has moved on from the query the results answer. */
  changed: boolean;
}): JSX.Element {
  const dataset = findDataset(datasets, spec.dataset);
  const grouped = spec.chartType !== "number";
  const dimensionOptions = dimensionFields(dataset).map((field) => ({
    label: field.name,
    value: field.name,
  }));
  const orderOptions = completeMeasures(spec.measures).map((measure) => ({
    value: measureAlias(measure),
    label: measureLabel(measure),
  }));

  const patch = (next: Partial<ExploreSpec>) => onChange({ ...spec, ...next });
  // Order by names a measure alias. A measure edited away or removed would
  // leave the select holding a value its options no longer offer, so it drops
  // back to group order with the measure it named.
  const withMeasures = (measures: MeasureDraft[]): Partial<ExploreSpec> => ({
    measures,
    orderBy: completeMeasures(measures).map(measureAlias).includes(spec.orderBy)
      ? spec.orderBy
      : "",
  });
  const changeDataset = (name: string) => {
    const next = findDataset(datasets, name);
    if (next) onChange(specForDataset(next, spec));
  };
  const setMeasure = (index: number, next: MeasureDraft) =>
    patch(withMeasures(replaceAt(spec.measures, index, next)));
  const setFilter = (index: number, next: FilterDraft) =>
    patch({ filters: replaceAt(spec.filters, index, next) });
  const addMeasure = () =>
    patch({ measures: [...spec.measures, { op: "count", field: "" }] });
  const addFilter = () =>
    patch({
      filters: [...spec.filters, { field: "", operator: "in", values: [] }],
    });

  return (
    <div className="border-border bg-card flex flex-col gap-5 border p-5">
      <ClauseRow label="Dataset">
        <div className="flex min-w-0 flex-wrap items-center gap-3">
          <Select value={spec.dataset} onValueChange={changeDataset}>
            <SelectTrigger className="w-64" aria-label="Dataset">
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
          {dataset ? <DatasetSummary dataset={dataset} /> : null}
        </div>
      </ClauseRow>

      <ClauseRow label="Where">
        <div className="flex flex-col gap-2">
          {spec.filters.map((filter, index) => (
            <FilterRow
              key={index}
              dataset={dataset}
              filter={filter}
              onChange={(next) => setFilter(index, next)}
              onRemove={() => patch({ filters: removeAt(spec.filters, index) })}
              trailing={
                index === spec.filters.length - 1 ? (
                  <AddRowButton
                    label="Add another filter"
                    onClick={addFilter}
                  />
                ) : undefined
              }
            />
          ))}
          {spec.filters.length === 0 ? (
            <div>
              <Button
                variant="tertiary"
                size="sm"
                icon="plus"
                onClick={addFilter}
              >
                Add filter
              </Button>
            </div>
          ) : null}
        </div>
      </ClauseRow>

      <ClauseRow label="Visualize">
        <div className="flex flex-col gap-2">
          {spec.measures.map((measure, index) => (
            <MeasureRow
              key={index}
              dataset={dataset}
              measure={measure}
              onChange={(next) => setMeasure(index, next)}
              onRemove={() =>
                patch(withMeasures(removeAt(spec.measures, index)))
              }
              trailing={
                index === spec.measures.length - 1 ? (
                  <AddRowButton
                    label="Add another measure"
                    onClick={addMeasure}
                  />
                ) : undefined
              }
            />
          ))}
          {spec.measures.length === 0 ? (
            // Nothing measured is still a question: the rows themselves.
            <div className="flex flex-wrap items-center gap-3">
              <Button
                variant="tertiary"
                size="sm"
                icon="plus"
                onClick={addMeasure}
              >
                Add measure
              </Button>
              <span className="text-muted-foreground text-xs">
                Nothing measured, so the results are rows at the dataset's
                grain.
              </span>
            </div>
          ) : null}
        </div>
      </ClauseRow>

      <ClauseRow label="Group by">
        <MultiSelect
          key={spec.dataset}
          options={dimensionOptions}
          value={spec.dimensions}
          onValueChange={(dimensions) =>
            patch({ dimensions: dimensions.slice(0, MAX_DIMENSIONS) })
          }
          placeholder={
            grouped ? "No breakdown" : "Not available for number charts"
          }
          disabled={!grouped}
          className="max-w-3xl"
        />
      </ClauseRow>

      <div className="border-border flex flex-wrap items-end gap-x-6 gap-y-3 border-t pt-5">
        <BuilderField label="Chart">
          <SegmentedControl<ChartType>
            value={spec.chartType}
            onChange={(chartType) => patch({ chartType })}
            options={CHART_TYPE_OPTIONS}
          />
        </BuilderField>
        <BuilderField label="Window">
          <Select
            value={spec.window}
            onValueChange={(window) =>
              patch({ window: window as WindowPreset })
            }
          >
            <SelectTrigger className="w-44" aria-label="Window">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {WINDOW_OPTIONS.map((window) => (
                <SelectItem key={window.value} value={window.value}>
                  {window.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </BuilderField>
        <BuilderField label="Order by">
          <Select
            value={spec.orderBy === "" ? GROUP_ORDER : spec.orderBy}
            onValueChange={(value) =>
              patch({ orderBy: value === GROUP_ORDER ? "" : value })
            }
          >
            <SelectTrigger className="w-52" aria-label="Order by">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={GROUP_ORDER}>Group order</SelectItem>
              {orderOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label} (desc)
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </BuilderField>
        <BuilderField label="Limit">
          <Input
            type="number"
            min={1}
            max={MAX_LIMIT}
            value={spec.limit === 0 ? "" : String(spec.limit)}
            onChange={(raw) => patch({ limit: parseLimit(raw) })}
            placeholder={`${DEFAULT_LIMIT} rows`}
            aria-label="Limit"
            className="w-32"
          />
        </BuilderField>
        <div className="ml-auto flex items-center gap-3">
          {changed ? (
            <span className="text-muted-foreground text-xs">
              Changed since the last run.
            </span>
          ) : null}
          <Button variant="primary" size="sm" icon="play" onClick={onRun}>
            Run query
          </Button>
        </div>
      </div>
      <p className="text-muted-foreground text-xs">
        Buckets are sized from the window. Order and limit shape the summary
        table.
      </p>
    </div>
  );
}

// The dataset's one-line description, with its grain as a mono tag.
function DatasetSummary({
  dataset,
}: {
  dataset: AnalyticsDataset;
}): JSX.Element {
  return (
    <span className="text-muted-foreground min-w-0 text-sm">
      {dataset.description}
      <span className="font-mono text-xs"> · {dataset.grain} grain</span>
    </span>
  );
}
