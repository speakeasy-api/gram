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
import { useState } from "react";

// Dummy data for logs (using real data structure)
const DUMMY_LOGS = [
  {
    id: "1",
    ts: new Date("2025-10-01T11:00:00Z"),
    toolUrn: "UserRegistration",
    httpMethod: "POST",
    httpRoute: "/api/register",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "2",
    ts: new Date("2025-10-01T11:01:30Z"),
    toolUrn: "PayoutRequest",
    httpMethod: "POST",
    httpRoute: "/api/refund",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "3",
    ts: new Date("2025-10-01T11:03:00Z"),
    toolUrn: "UserSettings",
    httpMethod: "PUT",
    httpRoute: "/api/sync",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 300,
  },
  {
    id: "4",
    ts: new Date("2025-10-01T11:05:00Z"),
    toolUrn: "InventoryCheck",
    httpMethod: "GET",
    httpRoute: "/api/inventory",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "5",
    ts: new Date("2025-10-01T11:07:00Z"),
    toolUrn: "EmailDispatch",
    httpMethod: "GET",
    httpRoute: "/api/orders/preview",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "6",
    ts: new Date("2025-10-01T11:09:00Z"),
    toolUrn: "AssetUploadView",
    httpMethod: "POST",
    httpRoute: "/api/content/share",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "7",
    ts: new Date("2025-10-01T11:09:00Z"),
    toolUrn: "InventoryCheck",
    httpMethod: "GET",
    httpRoute: "/api/plugin/keystore",
    statusCode: 404,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "8",
    ts: new Date("2025-10-01T11:10:15Z"),
    toolUrn: "EmailDispatch",
    httpMethod: "POST",
    httpRoute: "/api/feedback",
    statusCode: 200,
    userAgent: "Mozilla/5.0",
    durationMs: 300,
  },
  {
    id: "9",
    ts: new Date("2025-10-01T11:11:30Z"),
    toolUrn: "OrderValidation",
    httpMethod: "GET",
    httpRoute: "/api/logs",
    statusCode: 500,
    userAgent: "Mozilla/5.0",
    durationMs: 1500,
  },
  {
    id: "10",
    ts: new Date("2025-10-01T11:13:00Z"),
    toolUrn: "PaymentRequest",
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

  const getStatusColor = (statusCode: number) => {
    if (statusCode >= 200 && statusCode < 300) {
      return "bg-success";
    } else if (statusCode >= 400 && statusCode < 500) {
      return "bg-warning";
    } else if (statusCode >= 500) {
      return "bg-destructive";
    }
    return "bg-muted";
  };

  const formatTimestamp = (date: Date) => {
    return date.toLocaleString("en-US", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    });
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
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>TIMESTAMP</TableHead>
                    <TableHead>SERVER_NAME</TableHead>
                    <TableHead>TOOL_NAME</TableHead>
                    <TableHead>STATUS</TableHead>
                    <TableHead>CLIENT</TableHead>
                    <TableHead>DURATION</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {logs.map((log) => (
                    <TableRow key={log.id}>
                      <TableCell className="text-muted-foreground">
                        {formatTimestamp(log.ts)}
                      </TableCell>
                      <TableCell className="font-medium">
                        {log.toolUrn || log.httpRoute}
                      </TableCell>
                      <TableCell>
                        <code className="text-sm">
                          {log.httpMethod} {log.httpRoute}
                        </code>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <div
                            className={`w-2 h-2 rounded-full ${getStatusColor(
                              log.statusCode
                            )}`}
                          />
                          <span className="text-sm">{log.statusCode}</span>
                        </div>
                      </TableCell>
                      <TableCell className="text-muted-foreground text-sm">
                        {log.userAgent}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {formatDuration(log.durationMs)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}