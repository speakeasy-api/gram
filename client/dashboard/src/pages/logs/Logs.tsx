import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { SearchBar } from "@/components/ui/search-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
} from "@/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Icon } from "@speakeasy-api/moonshine";
import { Copy, ExternalLink, FileCode, SquareFunction } from "lucide-react";
import { useState } from "react";

function StatusIcon({ isSuccess }: { isSuccess: boolean }) {
  if (isSuccess) {
    return (
      <div style={{ color: "var(--fill-success-default, #5a8250)" }}>
        <Icon name="check" className="items-center size-4" />
      </div>
    );
  }
  return (
    <div style={{ color: "var(--fill-destructive-default, #c83228)" }}>
      <Icon name="x" className="size-4" />
    </div>
  );
}

// Real data from ClickHouse http_requests_raw table
const DUMMY_LOGS = [
  {
    id: "019a00f1-d533-74ce-be34-0e78f5b5f89a",
    ts: new Date("2025-10-20T09:27:20Z"),
    toolUrn: "tools:http:convoy:convoy_get_event_types",
    httpMethod: "GET",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types",
    statusCode: 200,
    userAgent: "",
    durationMs: 421.016708,
    requestHeaders: {},
    responseHeaders: {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Credentials": "true",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Access-Control-Max-Age": "1728000",
      "Date": "Mon, 20 Oct 2025 09:27:19 GMT",
      "Content-Type": "application/json",
    },
  },
  {
    id: "019a00f1-ac71-75eb-9643-534e88459d6d",
    ts: new Date("2025-10-20T09:27:09Z"),
    toolUrn: "tools:http:convoy:convoy_update_event_type",
    httpMethod: "PUT",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types/{eventTypeId}",
    statusCode: 400,
    userAgent: "",
    durationMs: 422.14858300000003,
    requestHeaders: {
      "Content-Type": "application/json",
    },
    responseHeaders: {
      "Access-Control-Allow-Credentials": "true",
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Access-Control-Max-Age": "1728000",
      "Date": "Mon, 20 Oct 2025 09:27:09 GMT",
      "Content-Type": "application/json",
      "Content-Length": "50",
    },
  },
  {
    id: "019a00f1-6dab-7989-b688-5f6a863d3cb2",
    ts: new Date("2025-10-20T09:26:53Z"),
    toolUrn: "tools:http:convoy:convoy_create_event_type",
    httpMethod: "POST",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types",
    statusCode: 201,
    userAgent: "",
    durationMs: 403.456125,
    requestHeaders: {
      "Content-Type": "application/json",
    },
    responseHeaders: {
      "Content-Type": "application/json",
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Credentials": "true",
      "Content-Length": "210",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Access-Control-Max-Age": "1728000",
      "Date": "Mon, 20 Oct 2025 09:26:53 GMT",
    },
  },
  {
    id: "019a00f1-3e5c-79b0-9547-c504b1e44c6c",
    ts: new Date("2025-10-20T09:26:41Z"),
    toolUrn: "tools:http:convoy:convoy_deprecate_event_type",
    httpMethod: "POST",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types/01K6YQ618QJ1B3EWW126WK5973/deprecate",
    statusCode: 200,
    userAgent: "",
    durationMs: 424.365625,
    requestHeaders: {},
    responseHeaders: {
      "Date": "Mon, 20 Oct 2025 09:26:41 GMT",
      "Content-Type": "application/json",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Credentials": "true",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Content-Length": "241",
      "Access-Control-Max-Age": "1728000",
    },
  },
  {
    id: "019a00f0-e735-7b90-b349-64d575ee7d44",
    ts: new Date("2025-10-20T09:26:19Z"),
    toolUrn: "tools:http:convoy:convoy_get_event_types",
    httpMethod: "GET",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types",
    statusCode: 200,
    userAgent: "",
    durationMs: 413.30962500000004,
    requestHeaders: {},
    responseHeaders: {
      "Access-Control-Max-Age": "1728000",
      "Access-Control-Allow-Credentials": "true",
      "Content-Type": "application/json",
      "Date": "Mon, 20 Oct 2025 09:26:19 GMT",
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
    },
  },
  {
    id: "0199eea1-a2c4-7cd5-8e73-8f2e0d2ac3bc",
    ts: new Date("2025-10-16T20:06:34Z"),
    toolUrn: "tools:http:convoy:convoy_get_event_types",
    httpMethod: "GET",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types",
    statusCode: 200,
    userAgent: "",
    durationMs: 402.313333,
    requestHeaders: {},
    responseHeaders: {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Max-Age": "1728000",
      "Date": "Thu, 16 Oct 2025 20:06:34 GMT",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Content-Type": "application/json",
      "Access-Control-Allow-Credentials": "true",
    },
  },
  {
    id: "0199ec6c-66cb-74af-8aa5-02798eeeb499",
    ts: new Date("2025-10-16T09:49:11Z"),
    toolUrn: "tools:http:convoy:convoy_get_event_types",
    httpMethod: "GET",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types",
    statusCode: 200,
    userAgent: "",
    durationMs: 420.721667,
    requestHeaders: {},
    responseHeaders: {
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Access-Control-Allow-Credentials": "true",
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Max-Age": "1728000",
      "Date": "Thu, 16 Oct 2025 09:49:11 GMT",
      "Content-Type": "application/json",
    },
  },
  {
    id: "0199ec6c-57a8-7da0-ad01-fc7fe4f3e91a",
    ts: new Date("2025-10-16T09:49:07Z"),
    toolUrn: "tools:http:convoy:convoy_update_event_type",
    httpMethod: "PUT",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types/{eventTypeId}",
    statusCode: 400,
    userAgent: "",
    durationMs: 405.02187499999997,
    requestHeaders: {
      "Content-Type": "application/json",
    },
    responseHeaders: {
      "Content-Type": "application/json",
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Credentials": "true",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Access-Control-Max-Age": "1728000",
      "Content-Length": "50",
      "Date": "Thu, 16 Oct 2025 09:49:07 GMT",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
    },
  },
  {
    id: "0199ec6c-3dae-7cfc-80e2-ace159ad1361",
    ts: new Date("2025-10-16T09:49:00Z"),
    toolUrn: "tools:http:convoy:convoy_create_event_type",
    httpMethod: "POST",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types",
    statusCode: 201,
    userAgent: "",
    durationMs: 444.025958,
    requestHeaders: {
      "Content-Type": "application/json",
    },
    responseHeaders: {
      "Content-Type": "application/json",
      "Access-Control-Allow-Credentials": "true",
      "Access-Control-Max-Age": "1728000",
      "Content-Length": "214",
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Date": "Thu, 16 Oct 2025 09:49:00 GMT",
    },
  },
  {
    id: "0199ec6c-20be-735f-b905-4371edb9abaf",
    ts: new Date("2025-10-16T09:48:53Z"),
    toolUrn: "tools:http:convoy:convoy_create_event_type",
    httpMethod: "POST",
    httpRoute: "/api/v1/projects/01J9M0QHWYSCST790SYNWM8ABQ/event-types",
    statusCode: 400,
    userAgent: "",
    durationMs: 410.610708,
    requestHeaders: {
      "Content-Type": "application/json",
    },
    responseHeaders: {
      "Content-Length": "136",
      "Access-Control-Max-Age": "1728000",
      "Access-Control-Allow-Origin": "*",
      "Date": "Thu, 16 Oct 2025 09:48:53 GMT",
      "Access-Control-Allow-Credentials": "true",
      "Access-Control-Allow-Headers": "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization",
      "Access-Control-Allow-Methods": "GET, PUT, POST, DELETE, PATCH, OPTIONS",
      "Content-Type": "application/json",
    },
  },
];

export default function Logs() {
  const [searchQuery, setSearchQuery] = useState("");
  const [toolTypeFilter, setToolTypeFilter] = useState<string>("");
  const [serverNameFilter, setServerNameFilter] = useState<string>("");
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [selectedLog, setSelectedLog] = useState<typeof DUMMY_LOGS[0] | null>(null);

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

  const getToolNameFromUrn = (toolUrn: string) => {
    // Parse URN format: tools:{kind}:{source}:{name}
    const parts = toolUrn.split(":");
    if (parts.length >= 4) {
      return parts[3]; // Return the name (e.g., "convoy_create_event_type")
    }
    return toolUrn;
  };

  const formatDetailTimestamp = (date: Date) => {
    return date.toLocaleString("en-US", {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "numeric",
      minute: "2-digit",
      second: "2-digit",
      timeZoneName: "short",
    });
  };

  const getHttpMethodVariant = (method: string): "default" | "secondary" => {
    if (method === "POST") return "default";
    return "secondary";
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
                        <TableRow
                          key={log.id}
                          className="cursor-pointer hover:bg-surface-secondary-default"
                          onClick={() => setSelectedLog(log)}
                        >
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
                            {getToolNameFromUrn(log.toolUrn)}
                          </span>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center justify-center">
                            <StatusIcon isSuccess={isSuccessfulCall(log)} />
                          </div>
                        </TableCell>
                        <TableCell className="text-muted-foreground text-sm">
                          {log.userAgent || "-"}
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

      {/* Log Detail Sheet */}
      <Sheet open={!!selectedLog} onOpenChange={(open) => !open && setSelectedLog(null)}>
        <SheetContent className="w-[540px] min-w-[540px] max-w-[540px] overflow-y-auto p-0">
          {selectedLog && (
            <div className="flex flex-col gap-8 pt-8 px-6 pb-6">
              {/* Header */}
              <div className="flex flex-col gap-6">
                <div className="flex items-center gap-3">
                  {(() => {
                    const ToolIcon = getToolIcon(selectedLog.toolUrn);
                    return <ToolIcon className="size-5 shrink-0" strokeWidth={1.5} />;
                  })()}
                  <h2 className="text-2xl font-light tracking-tight">
                    {getToolNameFromUrn(selectedLog.toolUrn)}
                  </h2>
                  <div className="flex items-center justify-center rounded-full size-6">
                    <StatusIcon isSuccess={isSuccessfulCall(selectedLog)} />
                  </div>
                </div>

                {/* Tabs */}
                <Tabs defaultValue="request" className="w-full">
                  <TabsList className="w-full">
                    <TabsTrigger value="request" className="flex-1">Request</TabsTrigger>
                    <TabsTrigger value="response" className="flex-1">Response</TabsTrigger>
                  </TabsList>
                  <TabsContent value="request" className="flex flex-col gap-6 mt-6">
                    {/* Endpoint */}
                    <div className="flex flex-col gap-3">
                      <h3 className="text-sm">Endpoint</h3>
                      <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4 flex items-center gap-3">
                        <Badge variant={getHttpMethodVariant(selectedLog.httpMethod)}>
                          {selectedLog.httpMethod}
                        </Badge>
                        <span className="font-mono text-xs">{selectedLog.httpRoute}</span>
                      </div>
                    </div>

                    {/* Request Headers */}
                    <div className="flex flex-col gap-3">
                      <div className="flex items-center justify-between">
                        <h3 className="text-sm">Request Headers</h3>
                        {Object.keys(selectedLog.requestHeaders).length > 0 && (
                          <button
                            className="p-1 rounded hover:bg-surface-secondary-default"
                            onClick={() => {
                              void navigator.clipboard.writeText(JSON.stringify(selectedLog.requestHeaders, null, 2));
                            }}
                          >
                            <Copy className="size-4" />
                          </button>
                        )}
                      </div>
                      <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4">
                        {Object.keys(selectedLog.requestHeaders).length > 0 ? (
                          <pre className="font-mono text-xs text-default whitespace-pre-wrap">
                            {JSON.stringify(selectedLog.requestHeaders, null, 2)}
                          </pre>
                        ) : (
                          <div className="text-sm text-muted-foreground">No request headers logged</div>
                        )}
                      </div>
                    </div>
                  </TabsContent>
                  <TabsContent value="response" className="flex flex-col gap-6 mt-6">
                    {/* Response Headers */}
                    <div className="flex flex-col gap-3">
                      <div className="flex items-center justify-between">
                        <h3 className="text-sm">Response Headers</h3>
                        {Object.keys(selectedLog.responseHeaders).length > 0 && (
                          <button
                            className="p-1 rounded hover:bg-surface-secondary-default"
                            onClick={() => {
                              void navigator.clipboard.writeText(JSON.stringify(selectedLog.responseHeaders, null, 2));
                            }}
                          >
                            <Copy className="size-4" />
                          </button>
                        )}
                      </div>
                      <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4">
                        {Object.keys(selectedLog.responseHeaders).length > 0 ? (
                          <pre className="font-mono text-xs text-default whitespace-pre-wrap">
                            {JSON.stringify(selectedLog.responseHeaders, null, 2)}
                          </pre>
                        ) : (
                          <div className="text-sm text-muted-foreground">No response headers logged</div>
                        )}
                      </div>
                    </div>
                  </TabsContent>
                </Tabs>
              </div>

              {/* Properties */}
              <div className="flex flex-col gap-4 border-t border-neutral-softest pt-4">
                <h3 className="text-sm">Properties</h3>
                <div className="flex flex-col gap-4">
                  <div className="flex flex-col gap-1.5">
                    <div className="text-xs font-mono uppercase text-muted-foreground">Created</div>
                    <div className="text-sm">{formatDetailTimestamp(selectedLog.ts)}</div>
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <div className="text-xs font-mono uppercase text-muted-foreground">Duration</div>
                    <div className="text-sm">{formatDuration(selectedLog.durationMs)}</div>
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <div className="text-xs font-mono uppercase text-muted-foreground">Server</div>
                    <div className="text-sm">{getSourceFromUrn(selectedLog.toolUrn)}</div>
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <div className="text-xs font-mono uppercase text-muted-foreground">Tool Type</div>
                    <div className="flex items-center gap-2">
                      {(() => {
                        const ToolIcon = getToolIcon(selectedLog.toolUrn);
                        return <ToolIcon className="size-4 shrink-0" strokeWidth={1.5} />;
                      })()}
                      <span className="text-sm">
                        {selectedLog.toolUrn.includes(":http:") ? "OpenAPI" : "Function"}
                      </span>
                    </div>
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <div className="text-xs font-mono uppercase text-muted-foreground">Status</div>
                    <div className="flex items-center gap-2">
                      <StatusIcon isSuccess={isSuccessfulCall(selectedLog)} />
                      <span className="text-sm">
                        {isSuccessfulCall(selectedLog) ? "Success" : "Failed"}
                      </span>
                    </div>
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <div className="text-xs font-mono uppercase text-muted-foreground">Status Code</div>
                    <div className="text-sm">{selectedLog.statusCode}</div>
                  </div>
                </div>
              </div>

              {/* Actions */}
              <div className="flex flex-col gap-3 border-t border-neutral-softest pt-4">
                <h3 className="text-sm">Actions</h3>
                <div className="flex flex-col gap-2">
                  <button className="flex items-center gap-1 text-sm hover:underline">
                    <ExternalLink className="size-3" />
                    <span>View in new tab</span>
                  </button>
                  <button
                    className="flex items-center gap-1 text-sm hover:underline"
                    onClick={() => {
                      void navigator.clipboard.writeText(selectedLog.id);
                    }}
                  >
                    <Copy className="size-3" />
                    <span>Copy log ID</span>
                  </button>
                </div>
              </div>
            </div>
          )}
        </SheetContent>
      </Sheet>
    </Page>
  );
}