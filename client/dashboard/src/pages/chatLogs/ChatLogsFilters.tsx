import { SearchBar } from "@/components/ui/search-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useState, useEffect } from "react";

const hasRiskOptions = [
  {
    value: "all",
    label: "Any Risk",
    description: "Show sessions regardless of risk findings",
  },
  {
    value: "true",
    label: "With Risk",
    description: "Only sessions where a policy flagged at least one finding",
  },
  {
    value: "false",
    label: "No Risk",
    description: "Only sessions with zero policy findings",
  },
];

interface ChatLogsFiltersProps {
  searchQuery: string;
  onSearchQueryChange: (value: string) => void;
  hasRisk: string;
  onHasRiskChange: (value: string) => void;
  disabled?: boolean;
}

export function ChatLogsFilters({
  searchQuery,
  onSearchQueryChange,
  hasRisk,
  onHasRiskChange,
  disabled,
}: ChatLogsFiltersProps) {
  const [localSearch, setLocalSearch] = useState(searchQuery);

  // Sync local state when prop changes externally (e.g., browser back/forward)
  useEffect(() => {
    setLocalSearch(searchQuery);
  }, [searchQuery]);

  // Debounced auto-submit
  useEffect(() => {
    if (localSearch === searchQuery) return;

    const timer = setTimeout(() => {
      onSearchQueryChange(localSearch);
    }, 500);

    return () => clearTimeout(timer);
  }, [localSearch, searchQuery, onSearchQueryChange]);

  const handleHasRiskChange = (value: string) => {
    onHasRiskChange(value === "all" ? "" : value);
  };

  return (
    <div className="flex flex-1 items-center gap-3">
      <SearchBar
        value={localSearch}
        onChange={setLocalSearch}
        placeholder="Search by chat ID, user ID, or title..."
        className="!h-10 flex-1"
        disabled={disabled}
      />

      <Select
        value={hasRisk || "all"}
        onValueChange={handleHasRiskChange}
        disabled={disabled}
      >
        <SelectTrigger
          className="border-border !h-10 w-[140px]"
          disabled={disabled}
        >
          <SelectValue placeholder="Any Risk" />
        </SelectTrigger>
        <SelectContent className="w-[280px]">
          {hasRiskOptions.map((option) => (
            <SelectItem
              key={option.value}
              value={option.value}
              description={option.description}
            >
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
