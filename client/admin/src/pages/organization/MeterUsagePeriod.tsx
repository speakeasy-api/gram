import { type JSX } from "react";
import { useForm } from "@tanstack/react-form";
import { CalendarDays } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { inclusiveEnd, usageRangeError } from "./billingUsageSearch";
import { meterDateLabel, type AdminMeterUsage } from "./meterUsage";

type Window = AdminMeterUsage["window"];

const cycleMonth = new Intl.DateTimeFormat("en-US", {
  month: "long",
  year: "numeric",
  timeZone: "UTC",
});
export function MeterUsagePeriod({
  window,
  cycles,
  onChange,
}: {
  window: Window;
  cycles: Window[];
  onChange: (from: string, to: string) => void;
}): JSX.Element {
  const selectedCycle = cycles.find(
    (cycle) => +cycle.from === +window.from && +cycle.to === +window.to,
  );
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        value={selectedCycle?.from.toISOString() ?? "custom"}
        onValueChange={(value) => {
          const cycle = cycles.find(
            (item) => item.from.toISOString() === value,
          );
          if (cycle)
            onChange(
              cycle.from.toISOString().slice(0, 10),
              inclusiveEnd(cycle.to),
            );
        }}
      >
        <SelectTrigger aria-label="Billing cycle" className="w-auto min-w-60">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {!selectedCycle && (
            <SelectItem value="custom" disabled>
              Custom date range
            </SelectItem>
          )}
          {cycles.toReversed().map((cycle, index) => (
            <SelectItem
              key={cycle.from.toISOString()}
              value={cycle.from.toISOString()}
            >
              {cycleMonth.format(cycle.from)} billing cycle
              {index === 0 ? " (current)" : ""}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="outline">
            <CalendarDays />
            {meterDateLabel(window.from)} –{" "}
            {meterDateLabel(inclusiveEnd(window.to))}
            <span className="text-muted-foreground text-xs">UTC</span>
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-80">
          <RangeForm
            key={`${window.from.toISOString()}:${window.to.toISOString()}`}
            window={window}
            onChange={onChange}
          />
        </PopoverContent>
      </Popover>
    </div>
  );
}

function RangeForm({
  window,
  onChange,
}: {
  window: Window;
  onChange: (from: string, to: string) => void;
}): JSX.Element {
  const form = useForm({
    defaultValues: {
      from: window.from.toISOString().slice(0, 10),
      to: inclusiveEnd(window.to),
    },
    validators: {
      onChange: ({ value }) => usageRangeError(value.from, value.to),
    },
    onSubmit: ({ value }) => onChange(value.from, value.to),
  });
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <div>
        <h4 className="text-sm font-medium">Date range (UTC)</h4>
        <p className="text-muted-foreground text-xs">
          Up to three calendar months. Both dates included.
        </p>
      </div>
      {(["from", "to"] as const).map((name) => (
        <form.Field key={name} name={name}>
          {(field) => (
            <label className="grid gap-1 text-sm">
              {name === "from" ? "Start date" : "End date"}
              <Input
                type="date"
                name={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
                required
              />
            </label>
          )}
        </form.Field>
      ))}
      <form.Subscribe
        selector={(state) => [state.canSubmit, state.errors] as const}
      >
        {([canSubmit, errors]) => (
          <>
            {errors.length > 0 && (
              <p role="alert" className="text-destructive text-sm">
                {errors.join(" ")}
              </p>
            )}
            <Button type="submit" disabled={!canSubmit}>
              Apply range
            </Button>
          </>
        )}
      </form.Subscribe>
    </form>
  );
}
