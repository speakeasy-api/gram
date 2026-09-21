import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { JSX, ReactNode } from "react";
import {
  FILTER_OPERATOR_LABELS,
  fieldByName,
  filterForField,
  filterableFields,
  operatorsForField,
  type FilterDraft,
  type FilterOperator,
  type WindowPreset,
} from "./exploreModel";
import { FilterValuePicker } from "./FilterValuePicker";

/**
 * One WHERE row: a field, an operator the catalog allows on it, and values
 * picked from what the dimension holds inside the builder's window.
 */
export function FilterRow({
  dataset,
  window,
  filter,
  onChange,
  onRemove,
  trailing,
}: {
  dataset: AnalyticsDataset | undefined;
  /** The builder's window: the picker lists values seen inside it. */
  window: WindowPreset;
  filter: FilterDraft;
  onChange: (next: FilterDraft) => void;
  onRemove: () => void;
  trailing?: ReactNode;
}): JSX.Element {
  const field = fieldByName(dataset, filter.field);

  const changeField = (name: string) => onChange(filterForField(dataset, name));
  const changeOperator = (operator: FilterOperator) =>
    onChange({
      ...filter,
      operator,
      values: operator === "equals" ? filter.values.slice(0, 1) : filter.values,
    });

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select value={filter.field} onValueChange={changeField}>
        <SelectTrigger className="w-56" aria-label="Filter field">
          <SelectValue placeholder="Pick a field" />
        </SelectTrigger>
        <SelectContent>
          {filterableFields(dataset).map((candidate) => (
            <SelectItem key={candidate.name} value={candidate.name}>
              {candidate.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {field ? (
        <Select
          value={filter.operator}
          onValueChange={(value) => changeOperator(value as FilterOperator)}
        >
          <SelectTrigger className="w-36" aria-label="Filter operator">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {operatorsForField(field).map((operator) => (
              <SelectItem key={operator} value={operator}>
                {FILTER_OPERATOR_LABELS[operator]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}
      {field && dataset ? (
        <FilterValuePicker
          dataset={dataset.name}
          dimension={field.name}
          window={window}
          operator={filter.operator}
          values={filter.values}
          onChange={(values) => onChange({ ...filter, values })}
        />
      ) : null}
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
