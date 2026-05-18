import { SearchBar } from "@/components/ui/search-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { AGENT_PROVIDERS } from "@/lib/agent-providers";
import { useState, useEffect, useMemo } from "react";

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

type SourceOption = {
  value: string;
  label: string;
  description?: string;
};

const ALL_SOURCES_OPTION: SourceOption = {
  value: "all",
  label: "All Sources",
  description: "Show sessions from any client",
};

// First-party callers that set X-Gram-Source from inside Gram itself.
// External agent clients (claude-code, cursor, cowork, …) come from
// AGENT_PROVIDERS so the chat-logs filter and the install dialog stay
// in lockstep as new providers are added.
const FIRST_PARTY_SOURCES: SourceOption[] = [
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

const AGENT_CLIENT_SOURCES: SourceOption[] = AGENT_PROVIDERS.map((p) => ({
  value: p.source,
  label: p.label,
  description: p.available
    ? `Sessions originating from ${p.label}`
    : `Sessions originating from ${p.label} (coming soon)`,
}));

function buildSourceOptions(
  selectedSource: string,
  observedSources: readonly string[],
): SourceOption[] {
  const byValue = new Map<string, SourceOption>();
  byValue.set(ALL_SOURCES_OPTION.value, ALL_SOURCES_OPTION);

  for (const opt of [...FIRST_PARTY_SOURCES, ...AGENT_CLIENT_SOURCES]) {
    if (!byValue.has(opt.value)) byValue.set(opt.value, opt);
  }

  // Anything actually seen in chat traffic that we don't already curate —
  // surface it with the raw value as both label and value.
  for (const value of observedSources) {
    if (!value || byValue.has(value)) continue;
    byValue.set(value, {
      value,
      label: value,
      description: "Custom source detected from chat traffic",
    });
  }

  // Round-trip an unknown URL value (e.g. a deep-linked filter) so the
  // dropdown doesn't blank out before observedSources resolves.
  if (selectedSource && !byValue.has(selectedSource)) {
    byValue.set(selectedSource, {
      value: selectedSource,
      label: selectedSource,
      description: "Custom source detected from chat traffic",
    });
  }

  return Array.from(byValue.values());
}

interface ChatLogsFiltersProps {
  searchQuery: string;
  onSearchQueryChange: (value: string) => void;
  resolutionStatus: string;
  onResolutionStatusChange: (value: string) => void;
  source: string;
  onSourceChange: (value: string) => void;
  observedSources?: readonly string[];
  disabled?: boolean;
}

export function ChatLogsFilters({
  searchQuery,
  onSearchQueryChange,
  resolutionStatus,
  onResolutionStatusChange,
  source,
  onSourceChange,
  observedSources,
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

  const sourceItems = useMemo(
    () => buildSourceOptions(source, observedSources ?? []),
    [source, observedSources],
  );

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
