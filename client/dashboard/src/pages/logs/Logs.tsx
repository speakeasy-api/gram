import { Page } from "@/components/page-layout";
import { SearchBar } from "@/components/ui/search-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Icon } from "@speakeasy-api/moonshine";
import { FileCode, SquareFunction } from "lucide-react";
import { useState } from "react";

// Dummy data for logs (using real data structure)
const DUMMY_LOGS = [
  {
    id: "1",
    ts: new Date("2025-10-01T11:00:00Z"),
    toolUrn: "tools:http:convoy:convoy_create_event_type",
    httpMethod: "POST",
    httpRoute: "/api/register",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "2",
    ts: new Date("2025-10-01T11:01:30Z"),
    toolUrn: "tools:http:taskmaster:taskmaster_create_project",
    httpMethod: "POST",
    httpRoute: "/api/refund",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "3",
    ts: new Date("2025-10-01T11:03:00Z"),
    toolUrn: "tools:function:analytics:calculate_metrics",
    httpMethod: "PUT",
    httpRoute: "/api/sync",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 300,
  },
  {
    id: "4",
    ts: new Date("2025-10-01T11:05:00Z"),
    toolUrn: "tools:function:data_processing:transform_data",
    httpMethod: "GET",
    httpRoute: "/api/inventory",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "5",
    ts: new Date("2025-10-01T11:07:00Z"),
    toolUrn: "tools:http:convoy:convoy_get_event_types",
    httpMethod: "GET",
    httpRoute: "/api/orders/preview",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "6",
    ts: new Date("2025-10-01T11:09:00Z"),
    toolUrn: "tools:http:taskmaster:taskmaster_create_task",
    httpMethod: "POST",
    httpRoute: "/api/content/share",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "7",
    ts: new Date("2025-10-01T11:09:00Z"),
    toolUrn: "tools:function:notifications:send_notification",
    httpMethod: "GET",
    httpRoute: "/api/plugin/keystore",
    statusCode: 404,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "8",
    ts: new Date("2025-10-01T11:10:15Z"),
    toolUrn: "tools:http:convoy:convoy_update_event_type",
    httpMethod: "POST",
    httpRoute: "/api/feedback",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 300,
  },
  {
    id: "9",
    ts: new Date("2025-10-01T11:11:30Z"),
    toolUrn: "tools:http:taskmaster:taskmaster_get_tasks",
    httpMethod: "GET",
    httpRoute: "/api/logs",
    statusCode: 500,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "10",
    ts: new Date("2025-10-01T11:13:00Z"),
    toolUrn: "tools:http:taskmaster:taskmaster_update_task",
    httpMethod: "GET",
    httpRoute: "/api/products/search",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1200,
  },
];

export default function Logs() {
  const [searchQuery, setSearchQuery] = useState("");
  const [toolTypeFilter, setToolTypeFilter] = useState<string>("");
  const [serverNameFilter, setServerNameFilter] = useState<string>("");
  const [statusFilter, setStatusFilter] = useState<string>("");

  const logs = DUMMY_LOGS;

  const getToolIcon = (toolUrn: string) => {
    // Parse URN format: tools:{kind}:{source}:{name}
    const parts = toolUrn.split(":");
    if (parts.length >= 2 && parts[1] === "http") {
      return FileCode;
    }
    // Otherwise it's a function tool
    return SquareFunction;
  };

  const getSourceFromUrn = (toolUrn: string) => {
    // Parse URN format: tools:{kind}:{source}:{name}
    const parts = toolUrn.split(":");
    if (parts.length >= 3) {
      return parts[2]; // Return the source (e.g., "convoy", "taskmaster", "con")
    }
    return toolUrn;
  };

  const isSuccessfulCall = (log: typeof DUMMY_LOGS[0]) => {
    // For HTTP tools, check status code
    if (log.httpMethod && log.statusCode) {
      return log.statusCode >= 200 && log.statusCode < 300;
    }
    // For function tools, check success field (when available)
    // For now, default to success for functions
    return true;
  };

  const formatTimestamp = (date: Date) => {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, "0");
    const day = String(date.getDate()).padStart(2, "0");
    const hours = String(date.getHours()).padStart(2, "0");
    const minutes = String(date.getMinutes()).padStart(2, "0");
    const seconds = String(date.getSeconds()).padStart(2, "0");
    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
  };

  const formatDuration = (ms: number) => {
    if (ms < 1000) {
      return `${ms.toFixed(0)}ms`;
    }
    return `${(ms / 1000).toFixed(1)}s`;
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>Logs</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Body>
            <div className="flex flex-col gap-4">
              {/* Search and Filters Row */}
              <div className="flex items-center justify-between gap-4">{/* Search Input */}
                <SearchBar
                    value={searchQuery}
                    onChange={setSearchQuery}
                    placeholder="Search"
                    className="w-1/3"
                />

                {/* Filters */}
                <div className="flex items-center gap-2">
                  <Select value={toolTypeFilter} onValueChange={setToolTypeFilter}>
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Tool Type" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Types</SelectItem>
                      {/* Add more tool type options here */}
                    </SelectContent>
                  </Select>

                  <Select
                    value={serverNameFilter}
                    onValueChange={setServerNameFilter}
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Server Name" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Servers</SelectItem>
                      {/* Add more server name options here */}
                    </SelectContent>
                  </Select>

                  <Select value={statusFilter} onValueChange={setStatusFilter}>
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Status" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Statuses</SelectItem>
                      {/* Add more status options here */}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {/* Table */}
              <div className="border border-neutral-softest rounded-lg overflow-hidden w-full">
                <Table>
                  <TableHeader>
                    <TableRow className="bg-surface-secondary-default border-b border-neutral-softest">
                      <TableHead className="font-mono">TIMESTAMP</TableHead>
                      <TableHead className="font-mono">SERVER NAME</TableHead>
                      <TableHead className="font-mono">TOOL NAME</TableHead>
                      <TableHead className="font-mono">STATUS</TableHead>
                      <TableHead className="font-mono">CLIENT</TableHead>
                      <TableHead className="font-mono">DURATION</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {logs.map((log) => {
                      const ToolIcon = getToolIcon(log.toolUrn);
                      const sourceName = getSourceFromUrn(log.toolUrn);
                      return (
                        <TableRow key={log.id}>
                          <TableCell className="text-muted-foreground font-mono">
                            {formatTimestamp(log.ts)}
                          </TableCell>
                          <TableCell className="font-medium">
                            <div className="flex items-center gap-2">
                              <ToolIcon className="size-4 shrink-0" strokeWidth={1.5} />
                              <span>{sourceName}</span>
                            </div>
                          </TableCell>
                        <TableCell className="font-mono">
                          <span className="text-sm">
                            {log.httpMethod} {log.httpRoute}
                          </span>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center justify-center">
                            {isSuccessfulCall(log) ? (
                              <Icon name="check" className="text-success size-4" />
                            ) : (
                              <Icon name="x" className="text-destructive size-4" />
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="text-muted-foreground text-sm">
                          {log.userAgent}
                        </TableCell>
                        <TableCell className="text-muted-foreground font-mono">
                          {formatDuration(log.durationMs)}
                        </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}