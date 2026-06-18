import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import type { Operator } from "@gram/client/models/components/logfilter";
import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import { FilterControl } from "./FilterControl";
import { CustomFilterChip } from "./FilterChip";
import { defaultValueForDimension } from "./filter-schema";
import type {
  FilterDimension,
  FilterValue,
  OptionsById,
} from "./filter-schema";

interface FilterSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  schema: readonly FilterDimension[];
  values: Record<string, FilterValue>;
  optionsById: OptionsById;
  onChange: (id: string, value: FilterValue) => void;
  onClearAll: () => void;
  projectSlug?: string;
  /** Arbitrary attribute filters + a page-supplied builder (e.g. LogFilterBar). */
  customFilters?: ActiveLogFilter[];
  onEditCustomFilter?: (id: string, op: Operator, value?: string) => void;
  onRemoveCustomFilter?: (id: string) => void;
  customBuilder?: ReactNode;
}

/**
 * Right-side sheet holding the full filter set for a page: every schema
 * dimension as a labeled control plus an optional arbitrary-attribute builder.
 * The bar shows pinned controls + active chips; the sheet is the complete form.
 */
export function FilterSheet({
  open,
  onOpenChange,
  schema,
  values,
  optionsById,
  onChange,
  onClearAll,
  projectSlug,
  customFilters,
  onEditCustomFilter,
  onRemoveCustomFilter,
  customBuilder,
}: FilterSheetProps): JSX.Element {
  const showCustomSection =
    customBuilder !== undefined || (customFilters?.length ?? 0) > 0;

  // The date control is the Elements `TimeRangePicker`, which bundles its OWN
  // copy of Radix Popover — a separate Radix context from this Sheet's Dialog.
  // A modal Dialog blocks outside pointer events, traps focus, and `hideOthers`
  // anything outside its content; because the picker's popover is a foreign
  // Radix instance it isn't recognized as a nested layer, so all three of those
  // make it impossible to open/use inside the sheet. Running the Dialog
  // non-modal disables that lockout. The trade-off is that outside interactions
  // now reach the dismiss handler, so any popover-content click (the picker, or
  // a select/multiselect) would close the sheet — we suppress that below.
  const keepOpenOnPopoverInteraction = (
    event: { detail?: { originalEvent?: Event } } & Event,
  ) => {
    const target = (event.detail?.originalEvent?.target ??
      event.target) as HTMLElement | null;
    if (target?.closest?.("[data-radix-popper-content-wrapper]")) {
      event.preventDefault();
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange} modal={false}>
      <SheetContent
        className="bg-card w-full overflow-y-auto sm:max-w-md"
        onInteractOutside={keepOpenOnPopoverInteraction}
        onPointerDownOutside={keepOpenOnPopoverInteraction}
      >
        <SheetHeader>
          <SheetTitle>Filters</SheetTitle>
          <SheetDescription>
            Narrow results by any combination of attributes.
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-col gap-4 px-4">
          {schema.map((dim) => (
            <div key={dim.id} className="flex flex-col gap-1.5">
              <label className="text-foreground text-sm font-medium">
                {dim.label}
              </label>
              <FilterControl
                dim={dim}
                value={values[dim.id] ?? defaultValueForDimension(dim)}
                onChange={(value) => onChange(dim.id, value)}
                options={optionsById[dim.id]}
                projectSlug={projectSlug}
                className="w-full"
              />
            </div>
          ))}

          {showCustomSection && (
            <div className="flex flex-col gap-2 border-t pt-4">
              <label className="text-foreground text-sm font-medium">
                Custom attributes
              </label>
              {customBuilder}
              {(customFilters?.length ?? 0) > 0 && (
                <div className="flex flex-wrap gap-1.5">
                  {customFilters!.map((filter) => (
                    <CustomFilterChip
                      key={filter.id}
                      filter={filter}
                      onEdit={onEditCustomFilter ?? (() => {})}
                      onRemove={onRemoveCustomFilter ?? (() => {})}
                    />
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        <SheetFooter>
          <Button variant="outline" onClick={onClearAll}>
            Reset to default
          </Button>
          <Button onClick={() => onOpenChange(false)}>Done</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
