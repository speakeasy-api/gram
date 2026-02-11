import { SearchBar } from "@/components/ui/search-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Stack } from "@speakeasy-api/moonshine";
import { useState, useEffect } from "react";

const resolutionStatusOptions = [
  {
    value: "all",
    label: "All Statuses",
    description: "Show all chat sessions regardless of outcome",
  },
  {
    value: "success",
    label: "Success",
    description: "AI analysis confirmed the user completed their task",
  },
  {
    value: "failure",
    label: "Failure",
    description: "AI analysis found the user did not complete their task",
  },
  {
    value: "partial",
    label: "Partial",
    description: "User completed some but not all of their intended goals",
  },
  {
    value: "unresolved",
    label: "Unresolved",
    description: "Chat not yet analyzed or outcome could not be determined",
  },
];

interface ChatLogsFiltersProps {
  searchQuery: string;
  onSearchQueryChange: (value: string) => void;
  resolutionStatus: string;
  onResolutionStatusChange: (value: string) => void;
  disabled?: boolean;
}

export function ChatLogsFilters({
  searchQuery,
  onSearchQueryChange,
  resolutionStatus,
  onResolutionStatusChange,
  disabled,
}: ChatLogsFiltersProps) {
  const [localSearch, setLocalSearch] = useState(searchQuery);

  // Sync local state when prop changes externally (e.g., browser back/forward)
  useEffect(() => {
    setLocalSearch(searchQuery);
  }, [searchQuery]);

  // Debounced auto-submit
  useEffect(() => {
    // Skip if already in sync to avoid unnecessary updates
    if (localSearch === searchQuery) return;

    const timer = setTimeout(() => {
      onSearchQueryChange(localSearch);
    }, 500);

    return () => clearTimeout(timer);
  }, [localSearch, searchQuery, onSearchQueryChange]);

  const handleStatusChange = (value: string) => {
    // Convert "all" back to empty string for the API
    onResolutionStatusChange(value === "all" ? "" : value);
  };

  return (
    <Stack direction="horizontal" gap={3} align="center">
      <SearchBar
        value={localSearch}
        onChange={setLocalSearch}
        placeholder="Search chats..."
        className="flex-1"
        disabled={disabled}
      />

      <Select
        value={resolutionStatus || "all"}
        onValueChange={handleStatusChange}
        disabled={disabled}
      >
        <SelectTrigger className="w-[180px]" disabled={disabled}>
          <SelectValue placeholder="All Statuses" />
        </SelectTrigger>
        <SelectContent className="w-[280px]">
          {resolutionStatusOptions.map((option) => (
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
    </Stack>
  );
}
