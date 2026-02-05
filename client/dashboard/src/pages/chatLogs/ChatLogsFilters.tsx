import { SearchBar } from "@/components/ui/search-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";

interface ChatLogsFiltersProps {
  externalCustomerId: string;
  onExternalCustomerIdChange: (value: string) => void;
  resolutionStatus: string;
  onResolutionStatusChange: (value: string) => void;
}

export function ChatLogsFilters({
  externalCustomerId,
  onExternalCustomerIdChange,
  resolutionStatus,
  onResolutionStatusChange,
}: ChatLogsFiltersProps) {
  const [localSearch, setLocalSearch] = useState(externalCustomerId);

  const handleSearch = () => {
    onExternalCustomerIdChange(localSearch);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      handleSearch();
    }
  };

  const handleStatusChange = (value: string) => {
    // Convert "all" back to empty string for the API
    onResolutionStatusChange(value === "all" ? "" : value);
  };

  return (
    <Stack direction="horizontal" gap={3} align="center">
      <div className="flex-1 flex gap-2">
        <SearchBar
          value={localSearch}
          onChange={setLocalSearch}
          placeholder="Search traces by user, ID, or summary..."
          className="flex-1"
          onKeyDown={handleKeyDown}
        />
        <Button onClick={handleSearch} variant="secondary" size="sm">
          <Icon name="search" className="size-4" />
        </Button>
      </div>

      <Select
        value={resolutionStatus || "all"}
        onValueChange={handleStatusChange}
      >
        <SelectTrigger className="w-[180px]">
          <SelectValue placeholder="All Statuses" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All Statuses</SelectItem>
          <SelectItem value="success">Success</SelectItem>
          <SelectItem value="failure">Failure</SelectItem>
          <SelectItem value="partial">Partial</SelectItem>
          <SelectItem value="unresolved">Unresolved</SelectItem>
        </SelectContent>
      </Select>
    </Stack>
  );
}
