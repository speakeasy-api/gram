import { SearchBar } from "@/components/ui/search-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useState, useEffect } from "react";

const resolutionStatusOptions = [
  {
    value: "all",
    label: "All Statuses",
    description: "Show all agent sessions regardless of outcome",
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

const sourceOptions = [
  {
    value: "all",
    label: "All Sources",
    description: "Show sessions from any client",
  },
  {
    value: "claude-code",
    label: "Claude Code",
    description: "Sessions originating from the Claude Code CLI",
  },
  {
    value: "dashboard-ai-insights",
    label: "AI Insights",
    description: "Sessions from the dashboard AI Insights sidebar",
  },
  {
    value: "playground",
    label: "Playground",
    description: "Sessions from the in-dashboard MCP Playground",
  },
  {
    value: "elements",
    label: "Elements",
    description: "Sessions from the embeddable Elements chat",
  },
  {
    value: "assistant",
    label: "Assistant",
    description: "Sessions from the embedded assistant onboarding",
  },
  {
    value: "gram",
    label: "Gram",
    description: "Sessions from the Gram product itself",
  },
  {
    value: "slack",
    label: "Slack",
    description: "Sessions originating from the Slack integration",
  },
];

interface ChatLogsFiltersProps {
  searchQuery: string;
  onSearchQueryChange: (value: string) => void;
  resolutionStatus: string;
  onResolutionStatusChange: (value: string) => void;
  source: string;
  onSourceChange: (value: string) => void;
  disabled?: boolean;
}

export function ChatLogsFilters({
  searchQuery,
  onSearchQueryChange,
  resolutionStatus,
  onResolutionStatusChange,
  source,
  onSourceChange,
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

  const handleSourceChange = (value: string) => {
    onSourceChange(value === "all" ? "" : value);
  };

  // If the current `source` value isn't one of the predefined options
  // (e.g. a custom X-Gram-Source value set by an external client), inject it
  // as an extra option so it round-trips through the dropdown.
  const sourceItems =
    source && !sourceOptions.some((o) => o.value === source)
      ? [
          ...sourceOptions,
          {
            value: source,
            label: source,
            description: "Custom source detected from chat traffic",
          },
        ]
      : sourceOptions;

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
        value={resolutionStatus || "all"}
        onValueChange={handleStatusChange}
        disabled={disabled}
      >
        <SelectTrigger
          className="border-border !h-10 w-[150px]"
          disabled={disabled}
        >
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

      <Select
        value={source || "all"}
        onValueChange={handleSourceChange}
        disabled={disabled}
      >
        <SelectTrigger
          className="border-border !h-10 w-[170px]"
          disabled={disabled}
        >
          <SelectValue placeholder="All Sources" />
        </SelectTrigger>
        <SelectContent className="w-[280px]">
          {sourceItems.map((option) => (
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
