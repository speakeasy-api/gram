import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { TagInput } from "@/components/ui/TagInput";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { JSX, ReactNode } from "react";
import {
  FILTER_OPERATOR_LABELS,
  fieldByName,
  filterableFields,
  operatorsForField,
  type FilterDraft,
  type FilterOperator,
} from "./exploreModel";

/** One WHERE row: a field, an operator the catalog allows on it, and values. */
export function FilterRow({
  dataset,
  filter,
  onChange,
  onRemove,
  trailing,
}: {
  dataset: AnalyticsDataset | undefined;
  filter: FilterDraft;
  onChange: (next: FilterDraft) => void;
  onRemove: () => void;
  trailing?: ReactNode;
}): JSX.Element {
  const field = fieldByName(dataset, filter.field);

  const changeField = (name: string) => {
    const operator = operatorsForField(fieldByName(dataset, name))[0] ?? "in";
    onChange({ field: name, operator, values: [] });
  };
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
      {field ? <FilterValues filter={filter} onChange={onChange} /> : null}
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

// Values are typed for now. The picker that offers the values the project
// has actually seen replaces this once dimension values are wired in.
function FilterValues({
  filter,
  onChange,
}: {
  filter: FilterDraft;
  onChange: (next: FilterDraft) => void;
}): JSX.Element {
  if (filter.operator === "equals") {
    return (
      <Input
        value={filter.values[0] ?? ""}
        onChange={(value) =>
          onChange({ ...filter, values: value === "" ? [] : [value] })
        }
        placeholder="Value"
        aria-label="Filter value"
        className="w-56"
      />
    );
  }
  return (
    <TagInput
      value={filter.values}
      onChange={(values) => onChange({ ...filter, values })}
      ariaLabel="Filter values"
      placeholder="Type a value, then Enter"
      className="min-w-64 flex-1"
    />
  );
}
