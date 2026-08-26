import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import type { ExploreDatasetSchema } from "@gram/client/models/components/exploredatasetschema.js";
import type { ExploreFieldMeta } from "@gram/client/models/components/explorefieldmeta.js";
import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import type { ReactNode } from "react";
import {
  CALC_OPS,
  canonicalCalc,
  CHART_TYPE_OPTIONS,
  completeCalcs,
  defaultsForDataset,
  dimensionFields,
  fieldBadgeLetter,
  fieldMeta,
  FILTER_OP_LABELS,
  filterableFields,
  measureFields,
  opsForField,
  pruneGroupExpressionsForSchema,
  valueControlForOp,
  WINDOW_OPTIONS,
  type CalcDraft,
  type CalcOp,
  type ChartType,
  type DatasetName,
  type ExploreSpec,
  type FilterDraft,
  type FilterOp,
  type FilterValueControl,
  type GroupExpressionDraft,
  type WindowPreset,
} from "./exploreModel";
import { useDimensionValues } from "./useRunQuery";

/** Compose one dataset-and-calculations Explore query. */
export function QueryBuilder({
  meta,
  spec,
  onChange,
}: {
  meta: ExploreMetaResult | undefined;
  spec: ExploreSpec;
  onChange: (spec: ExploreSpec) => void;
}): JSX.Element {
  const schema = meta?.datasets.find(
    (dataset) => dataset.name === spec.dataset,
  );
  const dimensionOptions = dimensionFields(schema).map((field) => ({
    label: field.label,
    value: field.name,
  }));

  const changeDataset = (dataset: string) => {
    const datasetName = dataset as DatasetName;
    const defaults = defaultsForDataset(datasetName);
    const nextSchema = meta?.datasets.find(
      (candidate) => candidate.name === datasetName,
    );

    onChange({
      ...spec,
      dataset: datasetName,
      calculations: defaults.calculations,
      filters: [],
      groupBy: defaults.groupBy,
      groupExpressions: pruneGroupExpressionsForSchema(
        spec.groupExpressions,
        nextSchema,
      ).filter(
        (expression) => !defaults.groupBy.includes(expression.name.trim()),
      ),
      orderBy: "",
    });
  };

  const updateCalculation = (index: number, next: CalcDraft) =>
    onChange({
      ...spec,
      calculations: spec.calculations.map((calculation, current) =>
        current === index ? next : calculation,
      ),
    });
  const removeCalculation = (index: number) =>
    onChange({
      ...spec,
      calculations: spec.calculations.filter((_, current) => current !== index),
    });
  const addCalculation = () =>
    onChange({
      ...spec,
      calculations: [...spec.calculations, newCalculationForSchema(schema)],
    });

  const updateFilter = (index: number, next: FilterDraft) =>
    onChange({
      ...spec,
      filters: spec.filters.map((filter, current) =>
        current === index ? next : filter,
      ),
    });
  const removeFilter = (index: number) =>
    onChange({
      ...spec,
      filters: spec.filters.filter((_, current) => current !== index),
    });
  const addFilter = () =>
    onChange({
      ...spec,
      filters: [...spec.filters, { dimension: "", values: [] }],
    });

  const updateGroupExpression = (index: number, next: GroupExpressionDraft) =>
    onChange({
      ...spec,
      groupExpressions: spec.groupExpressions.map((expression, current) =>
        current === index ? next : expression,
      ),
    });
  const removeGroupExpression = (index: number) =>
    onChange({
      ...spec,
      groupExpressions: spec.groupExpressions.filter(
        (_, current) => current !== index,
      ),
    });
  const addGroupExpression = () =>
    onChange({
      ...spec,
      groupExpressions: [
        ...spec.groupExpressions,
        { name: "", dimension: "", values: [] },
      ],
    });

  const orderOptions = completeCalcs(spec.calculations).map(canonicalCalc);

  return (
    <div className="border-border bg-card flex flex-col gap-2.5 border p-3">
      <ClauseRow label="Dataset">
        <div className="flex min-w-0 items-center gap-3">
          <Select value={spec.dataset} onValueChange={changeDataset}>
            <SelectTrigger size="sm" className="w-64">
              <SelectValue />
            </SelectTrigger>
            <SelectContent className="max-h-80">
              <DatasetGroup
                label="Events"
                datasets={(meta?.datasets ?? []).filter(
                  (dataset) => dataset.category === "event",
                )}
              />
              <DatasetGroup
                label="Metrics"
                datasets={(meta?.datasets ?? []).filter(
                  (dataset) => dataset.category === "usage",
                )}
              />
            </SelectContent>
          </Select>
          {schema ? (
            <span className="text-muted-foreground truncate text-xs">
              {schema.description}
            </span>
          ) : null}
        </div>
      </ClauseRow>

      <ClauseRow label="Visualize">
        <div className="flex flex-col gap-1.5">
          {spec.calculations.map((calculation, index) => (
            <CalculationRow
              key={index}
              schema={schema}
              calculation={calculation}
              onChange={(next) => updateCalculation(index, next)}
              onRemove={
                spec.calculations.length > 1
                  ? () => removeCalculation(index)
                  : undefined
              }
              trailing={
                index === spec.calculations.length - 1 ? (
                  <AddRowButton
                    label="Add calculation"
                    onClick={addCalculation}
                  />
                ) : undefined
              }
            />
          ))}
        </div>
      </ClauseRow>

      <ClauseRow label="Where">
        <div className="flex flex-col gap-1.5">
          {spec.filters.map((filter, index) => (
            <FilterRow
              key={index}
              schema={schema}
              dataset={spec.dataset}
              window={spec.window}
              filter={filter}
              onChange={(next) => updateFilter(index, next)}
              onRemove={() => removeFilter(index)}
              trailing={
                index === spec.filters.length - 1 ? (
                  <AddRowButton label="Add filter" onClick={addFilter} />
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

      <ClauseRow label="Group by">
        <div className="flex flex-col gap-1.5">
          <MultiSelect
            key={spec.dataset}
            options={dimensionOptions}
            defaultValue={spec.groupBy}
            onValueChange={(groupBy) =>
              onChange({
                ...spec,
                groupBy,
                groupExpressions: spec.groupExpressions.filter(
                  (expression) => !groupBy.includes(expression.name.trim()),
                ),
              })
            }
            placeholder={
              spec.chartType === "number"
                ? "Not available for number charts"
                : "No breakdown"
            }
            disabled={spec.chartType === "number"}
            className="min-h-8 max-w-xl"
          />
          {spec.groupExpressions.map((expression, index) => (
            <CalculatedGroupRow
              key={index}
              schema={schema}
              dataset={spec.dataset}
              window={spec.window}
              expression={expression}
              onChange={(next) => updateGroupExpression(index, next)}
              onRemove={() => removeGroupExpression(index)}
            />
          ))}
          {spec.groupExpressions.length > 0 ? (
            <span className="text-muted-foreground text-xs">
              Each condition creates a true group for matches and a false group
              for everything else.
            </span>
          ) : null}
          <div>
            <Button
              variant="tertiary"
              size="sm"
              icon="plus"
              onClick={addGroupExpression}
              disabled={spec.chartType === "number"}
            >
              Add condition group
            </Button>
          </div>
        </div>
      </ClauseRow>

      <div className="border-border flex flex-wrap items-end gap-x-4 gap-y-2 border-t pt-2.5">
        <BuilderField label="Chart">
          <SegmentedControl<ChartType>
            value={spec.chartType}
            onChange={(chartType) =>
              onChange({
                ...spec,
                chartType,
                groupBy: chartType === "number" ? [] : spec.groupBy,
                groupExpressions:
                  chartType === "number" ? [] : spec.groupExpressions,
              })
            }
            options={CHART_TYPE_OPTIONS}
            className="h-8"
          />
        </BuilderField>
        <BuilderField label="Time duration">
          <Select
            value={spec.window}
            onValueChange={(window) =>
              onChange({ ...spec, window: window as WindowPreset })
            }
          >
            <SelectTrigger size="sm" className="w-40">
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
            value={spec.orderBy === "" ? "__none__" : spec.orderBy}
            onValueChange={(value) =>
              onChange({
                ...spec,
                orderBy: value === "__none__" ? "" : value,
              })
            }
          >
            <SelectTrigger size="sm" className="w-48">
              <SelectValue placeholder="Group order" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">Group order</SelectItem>
              {orderOptions.map((calculation) => (
                <SelectItem key={calculation} value={calculation}>
                  {calculation} (desc)
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </BuilderField>
        <BuilderField label="Limit">
          <Input
            type="number"
            min={0}
            value={spec.limit === 0 ? "" : String(spec.limit)}
            onChange={(raw) => {
              const parsed = Number(raw);
              onChange({
                ...spec,
                limit: Number.isFinite(parsed) && parsed > 0 ? parsed : 0,
              });
            }}
            placeholder="No limit"
            className="h-8 w-28 px-3 py-1"
          />
        </BuilderField>
      </div>
    </div>
  );
}

function fieldsForOp(
  schema: ExploreDatasetSchema | undefined,
  op: CalcOp,
): ExploreFieldMeta[] {
  if (op === "COUNT") return [];
  if (op === "COUNT_DISTINCT") return dimensionFields(schema);
  return measureFields(schema);
}

function newCalculationForSchema(
  schema: ExploreDatasetSchema | undefined,
): CalcDraft {
  if (schema?.category === "usage") {
    return { op: "SUM", column: "" };
  }
  return { op: "COUNT", column: "" };
}

function FieldOptionLabel({ field }: { field: ExploreFieldMeta }): JSX.Element {
  return (
    <span className="flex items-center gap-2">
      <span className="text-muted-foreground w-3 shrink-0 text-center font-mono text-xs">
        {fieldBadgeLetter(field.type)}
      </span>
      {field.label}
      {field.unit !== "" ? (
        <span className="text-muted-foreground text-xs">· {field.unit}</span>
      ) : null}
    </span>
  );
}

function ClauseRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:gap-3">
      <span className="text-eyebrow w-20 shrink-0 sm:pt-2">{label}</span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

function AddRowButton({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}): JSX.Element {
  return (
    <Button
      variant="tertiary"
      size="sm"
      icon="plus"
      aria-label={label}
      onClick={onClick}
    />
  );
}

function BuilderField({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-eyebrow">{label}</span>
      {children}
    </div>
  );
}

function CalculationRow({
  schema,
  calculation,
  onChange,
  onRemove,
  trailing,
}: {
  schema: ExploreDatasetSchema | undefined;
  calculation: CalcDraft;
  onChange: (next: CalcDraft) => void;
  onRemove: (() => void) | undefined;
  trailing?: ReactNode;
}): JSX.Element {
  const operations = CALC_OPS;
  const targets = fieldsForOp(schema, calculation.op);

  const changeOperation = (op: CalcOp) => {
    const valid = fieldsForOp(schema, op).some(
      (field) => field.name === calculation.column,
    );
    onChange({ op, column: valid ? calculation.column : "" });
  };

  return (
    <div className="flex items-center gap-1.5">
      <Select
        value={calculation.op}
        onValueChange={(value) => changeOperation(value as CalcOp)}
      >
        <SelectTrigger size="sm" className="w-40">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {operations.map((operation) => (
            <SelectItem key={operation} value={operation}>
              {operation}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {calculation.op === "COUNT" ? (
        <span className="text-muted-foreground text-xs">all activity</span>
      ) : (
        <>
          <span className="text-muted-foreground text-xs">of</span>
          <Select
            value={calculation.column === "" ? undefined : calculation.column}
            onValueChange={(column) => onChange({ ...calculation, column })}
          >
            <SelectTrigger size="sm" className="w-48">
              <SelectValue placeholder="Select a field" />
            </SelectTrigger>
            <SelectContent className="max-h-80">
              {targets.map((field) => (
                <SelectItem
                  key={field.name}
                  value={field.name}
                  textValue={field.label}
                >
                  <FieldOptionLabel field={field} />
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </>
      )}
      {onRemove ? (
        <Button
          variant="tertiary"
          size="sm"
          icon="x"
          aria-label="Remove calculation"
          onClick={onRemove}
        />
      ) : null}
      {trailing}
    </div>
  );
}

function DatasetGroup({
  label,
  datasets,
}: {
  label: string;
  datasets: ExploreDatasetSchema[];
}): JSX.Element | null {
  if (datasets.length === 0) return null;
  return (
    <SelectGroup>
      <SelectLabel>{label}</SelectLabel>
      {datasets.map((dataset) => (
        <SelectItem
          key={dataset.name}
          value={dataset.name}
          description={dataset.description}
        >
          {dataset.label}
        </SelectItem>
      ))}
    </SelectGroup>
  );
}

function FilterRow({
  schema,
  dataset,
  window,
  filter,
  onChange,
  onRemove,
  trailing,
}: {
  schema: ExploreDatasetSchema | undefined;
  dataset: string;
  window: WindowPreset;
  filter: FilterDraft;
  onChange: (next: FilterDraft) => void;
  onRemove: () => void;
  trailing?: ReactNode;
}): JSX.Element {
  return (
    <div className="flex items-center gap-1.5">
      <PredicateControls
        schema={schema}
        dataset={dataset}
        filter={filter}
        window={window}
        onChange={onChange}
      />
      <Button
        variant="tertiary"
        size="sm"
        icon="x"
        aria-label="Remove filter"
        onClick={onRemove}
      />
      {trailing}
    </div>
  );
}

function CalculatedGroupRow({
  schema,
  dataset,
  window,
  expression,
  onChange,
  onRemove,
}: {
  schema: ExploreDatasetSchema | undefined;
  dataset: string;
  window: WindowPreset;
  expression: GroupExpressionDraft;
  onChange: (next: GroupExpressionDraft) => void;
  onRemove: () => void;
}): JSX.Element {
  const filter: FilterDraft = {
    dimension: expression.dimension,
    op: expression.op,
    values: expression.values,
  };
  return (
    <div className="flex items-center gap-1.5">
      <Input
        value={expression.name}
        onChange={(name) => onChange({ ...expression, name })}
        placeholder="Group name"
        aria-label="Condition group name"
        className="h-8 w-40 px-3 py-1"
      />
      <span className="text-muted-foreground text-xs">when</span>
      <PredicateControls
        schema={schema}
        dataset={dataset}
        window={window}
        filter={filter}
        onChange={(next) => onChange({ ...expression, ...next })}
      />
      <Button
        variant="tertiary"
        size="sm"
        icon="x"
        aria-label="Remove condition group"
        onClick={onRemove}
      />
    </div>
  );
}

function PredicateControls({
  schema,
  dataset,
  window,
  filter,
  onChange,
}: {
  schema: ExploreDatasetSchema | undefined;
  dataset: string;
  window: WindowPreset;
  filter: FilterDraft;
  onChange: (next: FilterDraft) => void;
}): JSX.Element {
  const field = fieldMeta(schema, filter.dimension);
  const op = filter.op ?? "in";
  const control = valueControlForOp(op);
  const valuesQuery = useDimensionValues(
    dataset,
    control === "multi" ? filter.dimension : "",
    window,
  );
  const options = (valuesQuery.data ?? []).map((value) => ({
    label: value,
    value,
  }));

  const changeField = (dimension: string) => {
    const firstOperation = opsForField(fieldMeta(schema, dimension))[0] ?? "in";
    onChange({ dimension, op: firstOperation, values: [] });
  };
  const changeOperation = (next: FilterOp) => {
    const keepValues = valueControlForOp(next) === control;
    onChange({
      ...filter,
      op: next,
      values: keepValues ? filter.values : [],
    });
  };

  return (
    <>
      <Select
        value={filter.dimension === "" ? undefined : filter.dimension}
        onValueChange={changeField}
      >
        <SelectTrigger size="sm" className="w-48">
          <SelectValue placeholder="Field" />
        </SelectTrigger>
        <SelectContent className="max-h-80">
          {filterableFields(schema).map((candidate) => (
            <SelectItem
              key={candidate.name}
              value={candidate.name}
              textValue={candidate.label}
            >
              <FieldOptionLabel field={candidate} />
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {field ? (
        <Select
          value={op}
          onValueChange={(value) => changeOperation(value as FilterOp)}
        >
          <SelectTrigger size="sm" className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {opsForField(field).map((operation) => (
              <SelectItem key={operation} value={operation}>
                {FILTER_OP_LABELS[operation]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}
      <FilterValue
        control={control}
        dataset={dataset}
        filter={filter}
        unit={field?.unit ?? ""}
        options={options}
        loading={valuesQuery.isPending}
        onChange={onChange}
      />
    </>
  );
}

function filterValuePlaceholder(filter: FilterDraft, loading: boolean): string {
  if (filter.dimension === "") return "Select a field first";
  if (loading) return "Loading values…";
  return "Select values";
}

function FilterValue({
  control,
  dataset,
  filter,
  unit,
  options,
  loading,
  onChange,
}: {
  control: FilterValueControl;
  dataset: string;
  filter: FilterDraft;
  unit: string;
  options: { label: string; value: string }[];
  loading: boolean;
  onChange: (next: FilterDraft) => void;
}): JSX.Element | null {
  const setOperand = (value: string) =>
    onChange({ ...filter, values: value === "" ? [] : [value] });

  switch (control) {
    case "multi":
      return (
        <MultiSelect
          key={`${dataset}:${filter.dimension}:${filter.op ?? "in"}`}
          options={options}
          defaultValue={filter.values}
          onValueChange={(values) => onChange({ ...filter, values })}
          placeholder={filterValuePlaceholder(filter, loading)}
          disabled={filter.dimension === ""}
          creatable
          className="min-h-8 max-w-xl flex-1"
        />
      );
    case "text":
      return (
        <Input
          value={filter.values[0] ?? ""}
          onChange={setOperand}
          placeholder="Text to match"
          className="h-8 w-56 px-3 py-1"
        />
      );
    case "number":
      return (
        <div className="flex items-center gap-1.5">
          <Input
            type="number"
            value={filter.values[0] ?? ""}
            onChange={setOperand}
            placeholder="Value"
            className="h-8 w-32 px-3 py-1"
          />
          {unit !== "" ? (
            <span className="text-muted-foreground text-xs">{unit}</span>
          ) : null}
        </div>
      );
    case "none":
      return null;
  }
}
