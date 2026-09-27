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
  fieldsForOp,
  opsForDataset,
  type MeasureDraft,
  type MeasureOp,
} from "./exploreModel";

/** One VISUALIZE row: an aggregation and, unless it counts rows, its field. */
export function MeasureRow({
  dataset,
  measure,
  onChange,
  onRemove,
  trailing,
}: {
  dataset: AnalyticsDataset | undefined;
  measure: MeasureDraft;
  onChange: (next: MeasureDraft) => void;
  onRemove: (() => void) | undefined;
  trailing?: ReactNode;
}): JSX.Element {
  const targets = fieldsForOp(dataset, measure.op);

  const changeOp = (op: MeasureOp) => {
    const keepsField = fieldsForOp(dataset, op).some(
      (field) => field.name === measure.field,
    );
    onChange({ op, field: keepsField ? measure.field : "" });
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        value={measure.op}
        onValueChange={(value) => changeOp(value as MeasureOp)}
      >
        <SelectTrigger
          className="w-44 font-mono uppercase"
          aria-label="Aggregation"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {opsForDataset(dataset).map((op) => (
            <SelectItem key={op} value={op} className="font-mono uppercase">
              {op}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {measure.op === "count" ? (
        <span className="text-muted-foreground text-sm">of all rows</span>
      ) : (
        <>
          <span className="text-muted-foreground text-sm">of</span>
          <Select
            value={measure.field}
            onValueChange={(field) => onChange({ ...measure, field })}
          >
            <SelectTrigger className="w-56" aria-label="Measure field">
              <SelectValue placeholder="Pick a field" />
            </SelectTrigger>
            <SelectContent>
              {targets.map((field) => (
                <SelectItem
                  key={field.name}
                  value={field.name}
                  description={field.unit ? `in ${field.unit}` : undefined}
                >
                  {field.name}
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
          aria-label="Remove measure"
          onClick={onRemove}
        />
      ) : null}
      {trailing}
    </div>
  );
}
