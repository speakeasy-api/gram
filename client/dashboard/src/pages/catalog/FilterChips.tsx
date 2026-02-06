import { Button } from "@/components/ui/button";
import { X } from "lucide-react";
import type {
  AuthType,
  FilterValues,
  PopularityThreshold,
  ToolBehavior,
  ToolCountThreshold,
  UpdatedRange,
} from "./FilterSidebar";

interface FilterChipsProps {
  values: FilterValues;
  onChange: (values: FilterValues) => void;
  onClearAll: () => void;
}

interface ChipConfig {
  key: string;
  label: string;
  onRemove: () => void;
}

const AUTH_LABELS: Record<AuthType, string> = {
  none: "No Auth",
  apikey: "API Key",
  oauth: "OAuth 2.1",
  other: "Other Auth",
};

const BEHAVIOR_LABELS: Record<ToolBehavior, string> = {
  readonly: "Read-only",
  write: "Can modify",
};

const POPULARITY_LABELS: Record<PopularityThreshold, string> = {
  0: "",
  100: "100+ users",
  1000: "1k+ users",
  10000: "10k+ users",
};

const UPDATED_LABELS: Record<UpdatedRange, string> = {
  any: "",
  week: "This week",
  month: "This month",
  year: "This year",
};

const TOOL_COUNT_LABELS: Record<ToolCountThreshold, string> = {
  0: "",
  5: "5+ tools",
  10: "10+ tools",
};

/**
 * Dismissible filter chips showing active filters.
 * Appears when any filter is active, allowing quick removal.
 */
export function FilterChips({
  values,
  onChange,
  onClearAll,
}: FilterChipsProps) {
  const chips: ChipConfig[] = [];

  // Auth type chips
  values.authTypes.forEach((type) => {
    chips.push({
      key: `auth-${type}`,
      label: AUTH_LABELS[type],
      onRemove: () =>
        onChange({
          ...values,
          authTypes: values.authTypes.filter((t) => t !== type),
        }),
    });
  });

  // Tool behavior chips
  values.toolBehaviors.forEach((behavior) => {
    chips.push({
      key: `behavior-${behavior}`,
      label: BEHAVIOR_LABELS[behavior],
      onRemove: () =>
        onChange({
          ...values,
          toolBehaviors: values.toolBehaviors.filter((b) => b !== behavior),
        }),
    });
  });

  // Popularity chip
  if (values.minUsers > 0) {
    chips.push({
      key: "popularity",
      label: POPULARITY_LABELS[values.minUsers],
      onRemove: () => onChange({ ...values, minUsers: 0 }),
    });
  }

  // Updated range chip
  if (values.updatedRange !== "any") {
    chips.push({
      key: "updated",
      label: UPDATED_LABELS[values.updatedRange],
      onRemove: () => onChange({ ...values, updatedRange: "any" }),
    });
  }

  // Tool count chip
  if (values.minTools > 0) {
    chips.push({
      key: "tools",
      label: TOOL_COUNT_LABELS[values.minTools],
      onRemove: () => onChange({ ...values, minTools: 0 }),
    });
  }

  if (chips.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-sm text-muted-foreground">Active:</span>
      {chips.map((chip) => (
        <button
          key={chip.key}
          onClick={chip.onRemove}
          className="inline-flex items-center gap-1 px-2 py-1 rounded-md bg-secondary text-secondary-foreground text-sm hover:bg-secondary/80 transition-colors group"
        >
          <span>{chip.label}</span>
          <X className="w-3 h-3 text-muted-foreground group-hover:text-foreground" />
        </button>
      ))}
      <Button
        variant="ghost"
        size="sm"
        onClick={onClearAll}
        className="h-7 text-xs text-muted-foreground hover:text-foreground"
      >
        Clear all
      </Button>
    </div>
  );
}
