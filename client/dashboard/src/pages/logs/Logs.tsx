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
import { useListToolLogs } from "@gram/client/react-query";
import { HTTPToolLog } from "@gram/client/models/components";
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

export default function Logs() {
  const [searchQuery, setSearchQuery] = useState("");
  const [toolTypeFilter, setToolTypeFilter] = useState<string>("");
  const [serverNameFilter, setServerNameFilter] = useState<string>("");
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [selectedLog, setSelectedLog] = useState<HTTPToolLog | null>(null);

  const { data, isLoading } = useListToolLogs(
    {
      perPage: 100,
    },
    undefined,
    {
      staleTime: 30000, // Cache for 30 seconds
      refetchOnWindowFocus: false,
    },
  );

  const logs = data?.logs ?? [];

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

  const isSuccessfulCall = (log: HTTPToolLog) => {
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
          {null}
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
                    {isLoading ? (
                      <TableRow>
                        <TableCell colSpan={6} className="text-center py-8 text-muted-foreground">
                          Loading logs...
                        </TableCell>
                      </TableRow>
                    ) : logs.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={6} className="text-center py-8 text-muted-foreground">
                          No logs found
                        </TableCell>
                      </TableRow>
                    ) : logs.map((log) => {
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
        <SheetContent className="w-[1040px] max-w-[1040px]">
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
                      <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4 flex items-center gap-3 max-h-[100px] overflow-y-auto border-hidden">
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
                        {selectedLog.requestHeaders && Object.keys(selectedLog.requestHeaders).length > 0 && (
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
                      <div className="bg-surface-secondary-default border border-neutral-softest flex rounded-lg p-4 max-h-[400px] overflow-y-auto overflow-x-hidden">
                        {selectedLog.requestHeaders && Object.keys(selectedLog.requestHeaders).length > 0 ? (
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
                        {selectedLog.responseHeaders && Object.keys(selectedLog.responseHeaders).length > 0 && (
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
                      <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4 max-h-[400px] overflow-y-auto overflow-x-hidden">
                        {selectedLog.responseHeaders && Object.keys(selectedLog.responseHeaders).length > 0 ? (
                          <pre className="font-mono text-xs text-default whitespace-pre-wrap break-all">
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
                  <button className="hidden items-center gap-1 text-sm hover:underline">
                    <ExternalLink className="size-3" />
                    <span>View in new tab</span>
                  </button>
                  {selectedLog.id && (
                    <button
                      className="flex items-center gap-1 text-sm hover:underline"
                      onClick={() => {
                        void navigator.clipboard.writeText(selectedLog.id!);
                      }}
                    >
                      <Copy className="size-3" />
                      <span>Copy log ID</span>
                    </button>
                  )}
                </div>
              </div>
            </div>
          )}
        </SheetContent>
      </Sheet>
    </Page>
  );
}