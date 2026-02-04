import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { Filter, X } from "lucide-react";
import { useState } from "react";

export type AuthType = "none" | "apikey" | "oauth" | "other";
export type ToolBehavior = "readonly" | "write";
export type PopularityThreshold = 0 | 100 | 1000 | 10000;
export type UpdatedRange = "any" | "week" | "month" | "year";
export type ToolCountThreshold = 0 | 5 | 10;

export interface FilterValues {
  authTypes: AuthType[];
  toolBehaviors: ToolBehavior[];
  minUsers: PopularityThreshold;
  updatedRange: UpdatedRange;
  minTools: ToolCountThreshold;
}

export const defaultFilterValues: FilterValues = {
  authTypes: [],
  toolBehaviors: [],
  minUsers: 0,
  updatedRange: "any",
  minTools: 0,
};

interface FilterSidebarProps {
  values: FilterValues;
  onChange: (values: FilterValues) => void;
  onClear: () => void;
}

const AUTH_OPTIONS: { value: AuthType; label: string }[] = [
  { value: "none", label: "No Auth" },
  { value: "apikey", label: "API Key" },
  { value: "oauth", label: "OAuth 2.1" },
  { value: "other", label: "Other" },
];

const BEHAVIOR_OPTIONS: { value: ToolBehavior; label: string }[] = [
  { value: "readonly", label: "Read-only only" },
  { value: "write", label: "Can modify data" },
];

const POPULARITY_OPTIONS: { value: PopularityThreshold; label: string }[] = [
  { value: 0, label: "Any" },
  { value: 100, label: "100+ users" },
  { value: 1000, label: "1k+ users" },
  { value: 10000, label: "10k+ users" },
];

const UPDATED_OPTIONS: { value: UpdatedRange; label: string }[] = [
  { value: "any", label: "Any time" },
  { value: "week", label: "This week" },
  { value: "month", label: "This month" },
  { value: "year", label: "This year" },
];

const TOOL_COUNT_OPTIONS: { value: ToolCountThreshold; label: string }[] = [
  { value: 0, label: "Any" },
  { value: 5, label: "5+ tools" },
  { value: 10, label: "10+ tools" },
];

function countActiveFilters(values: FilterValues): number {
  let count = 0;
  if (values.authTypes.length > 0) count++;
  if (values.toolBehaviors.length > 0) count++;
  if (values.minUsers > 0) count++;
  if (values.updatedRange !== "any") count++;
  if (values.minTools > 0) count++;
  return count;
}

/**
 * Filter popover with granular filter controls.
 * Shows a "Filters (N)" button that opens a dropdown with all filter options.
 */
export function FilterSidebar({
  values,
  onChange,
  onClear,
}: FilterSidebarProps) {
  const [open, setOpen] = useState(false);
  const activeCount = countActiveFilters(values);

  const handleAuthChange = (authType: AuthType, checked: boolean) => {
    const newTypes = checked
      ? [...values.authTypes, authType]
      : values.authTypes.filter((t) => t !== authType);
    onChange({ ...values, authTypes: newTypes });
  };

  const handleBehaviorChange = (behavior: ToolBehavior, checked: boolean) => {
    const newBehaviors = checked
      ? [...values.toolBehaviors, behavior]
      : values.toolBehaviors.filter((b) => b !== behavior);
    onChange({ ...values, toolBehaviors: newBehaviors });
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className={cn(
            "gap-2",
            activeCount > 0 && "border-primary text-primary",
          )}
        >
          <Filter className="w-4 h-4" />
          <span>Filters</span>
          {activeCount > 0 && (
            <span className="ml-1 px-1.5 py-0.5 rounded-full bg-primary text-primary-foreground text-xs">
              {activeCount}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-80 p-0" align="start">
        <div className="flex items-center justify-between p-4 border-b">
          <span className="font-medium">Filters</span>
          {activeCount > 0 && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                onClear();
              }}
              className="h-auto p-1 text-muted-foreground hover:text-foreground"
            >
              <X className="w-4 h-4 mr-1" />
              Clear all
            </Button>
          )}
        </div>

        <div className="p-4 space-y-6 max-h-[400px] overflow-y-auto">
          {/* Auth Type */}
          <div className="space-y-3">
            <Label className="text-sm font-medium">Auth Type</Label>
            <div className="space-y-2">
              {AUTH_OPTIONS.map((option) => (
                <div key={option.value} className="flex items-center gap-2">
                  <Checkbox
                    id={`auth-${option.value}`}
                    checked={values.authTypes.includes(option.value)}
                    onCheckedChange={(checked) =>
                      handleAuthChange(option.value, checked === true)
                    }
                  />
                  <Label
                    htmlFor={`auth-${option.value}`}
                    className="text-sm font-normal cursor-pointer"
                  >
                    {option.label}
                  </Label>
                </div>
              ))}
            </div>
          </div>

          <Separator />

          {/* Tool Behavior */}
          <div className="space-y-3">
            <Label className="text-sm font-medium">Tool Behavior</Label>
            <div className="space-y-2">
              {BEHAVIOR_OPTIONS.map((option) => (
                <div key={option.value} className="flex items-center gap-2">
                  <Checkbox
                    id={`behavior-${option.value}`}
                    checked={values.toolBehaviors.includes(option.value)}
                    onCheckedChange={(checked) =>
                      handleBehaviorChange(option.value, checked === true)
                    }
                  />
                  <Label
                    htmlFor={`behavior-${option.value}`}
                    className="text-sm font-normal cursor-pointer"
                  >
                    {option.label}
                  </Label>
                </div>
              ))}
            </div>
          </div>

          <Separator />

          {/* Popularity */}
          <div className="space-y-3">
            <Label className="text-sm font-medium">Popularity</Label>
            <RadioGroup
              value={String(values.minUsers)}
              onValueChange={(v) =>
                onChange({
                  ...values,
                  minUsers: Number(v) as PopularityThreshold,
                })
              }
            >
              {POPULARITY_OPTIONS.map((option) => (
                <div key={option.value} className="flex items-center gap-2">
                  <RadioGroupItem
                    value={String(option.value)}
                    id={`popularity-${option.value}`}
                  />
                  <Label
                    htmlFor={`popularity-${option.value}`}
                    className="text-sm font-normal cursor-pointer"
                  >
                    {option.label}
                  </Label>
                </div>
              ))}
            </RadioGroup>
          </div>

          <Separator />

          {/* Last Updated */}
          <div className="space-y-3">
            <Label className="text-sm font-medium">Last Updated</Label>
            <RadioGroup
              value={values.updatedRange}
              onValueChange={(v) =>
                onChange({ ...values, updatedRange: v as UpdatedRange })
              }
            >
              {UPDATED_OPTIONS.map((option) => (
                <div key={option.value} className="flex items-center gap-2">
                  <RadioGroupItem
                    value={option.value}
                    id={`updated-${option.value}`}
                  />
                  <Label
                    htmlFor={`updated-${option.value}`}
                    className="text-sm font-normal cursor-pointer"
                  >
                    {option.label}
                  </Label>
                </div>
              ))}
            </RadioGroup>
          </div>

          <Separator />

          {/* Tool Count */}
          <div className="space-y-3">
            <Label className="text-sm font-medium">Tool Count</Label>
            <RadioGroup
              value={String(values.minTools)}
              onValueChange={(v) =>
                onChange({
                  ...values,
                  minTools: Number(v) as ToolCountThreshold,
                })
              }
            >
              {TOOL_COUNT_OPTIONS.map((option) => (
                <div key={option.value} className="flex items-center gap-2">
                  <RadioGroupItem
                    value={String(option.value)}
                    id={`tools-${option.value}`}
                  />
                  <Label
                    htmlFor={`tools-${option.value}`}
                    className="text-sm font-normal cursor-pointer"
                  >
                    {option.label}
                  </Label>
                </div>
              ))}
            </RadioGroup>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
