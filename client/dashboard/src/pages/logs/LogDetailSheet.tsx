import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import { Operator } from "@gram/client/models/components/logfilter";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ChevronDown, CircleAlert, Copy, ShieldAlert } from "lucide-react";
import { useState } from "react";
import { formatNanoTimestamp, getSeverityColorClass } from "./utils";

interface LogDetailSheetProps {
  log: TelemetryLogRecord | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}

export function LogDetailSheet({
  log,
  open,
  onOpenChange,
  onAddFilter,
}: LogDetailSheetProps): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="h-full max-h-screen overflow-y-auto"
        style={{ width: "33vw", minWidth: 500, maxWidth: "none" }}
      >
        {log && <LogDetailContent log={log} onAddFilter={onAddFilter} />}
      </SheetContent>
    </Sheet>
  );
}

/** Keys used to store tool I/O content in telemetry attributes (OTel GenAI semantic conventions). */
const TOOL_IO_ATTR_KEYS = {
  input: "gen_ai.tool.call.arguments",
  output: "gen_ai.tool.call.result",
} as const;

const HOOK_BLOCK_REASON_KEY = "gram.hook.block_reason";

/** Attributes surfaced as a prominent labeled row above Tool Input. Kept
 *  separate from the generic Attributes section to avoid duplication. */
const HIGHLIGHT_ATTR_KEYS = [
  { path: "gram.tool_call.source", label: "Server" },
  { path: "gram.tool.name", label: "Tool" },
  { path: "gram.hook.source", label: "LLM Client" },
  { path: "gram.mcp.server_url", label: "MCP Server URL" },
] as const;

const HOOK_ERROR_KEY = "gram.hook.error";

/**
 * Extract a deeply nested value from an object using a dot-separated path.
 * e.g. getNestedValue(obj, "gram.tool_call.input.content")
 */
function getNestedValue(
  obj: Record<string, unknown>,
  path: string,
): string | undefined {
  const parts = path.split(".");
  let current: unknown = obj;
  for (const part of parts) {
    if (
      current === null ||
      current === undefined ||
      typeof current !== "object"
    )
      return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return typeof current === "string" ? current : undefined;
}

/**
 * Remove a deeply nested key from an object (mutates a cloned copy).
 * Returns a shallow-cloned object with the leaf key removed.
 */
function removeNestedKey(
  obj: Record<string, unknown>,
  path: string,
): Record<string, unknown> {
  const clone = structuredClone(obj);
  const parts = path.split(".");
  let current: Record<string, unknown> = clone;
  for (let i = 0; i < parts.length - 1; i++) {
    const next = current[parts[i]!];
    if (next === null || next === undefined || typeof next !== "object")
      return clone;
    current = next as Record<string, unknown>;
  }
  delete current[parts[parts.length - 1]!];
  return clone;
}

function LogDetailContent({
  log,
  onAddFilter,
}: {
  log: TelemetryLogRecord;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}) {
  const severityClass = getSeverityColorClass(log.severityText);
  const resourceAttrs = log.resourceAttributes as
    | { gram?: { tool?: { urn?: string } } }
    | undefined;
  const gramUrn = resourceAttrs?.gram?.tool?.urn;

  // Extract tool I/O content from attributes
  const attrs = log.attributes as Record<string, unknown> | undefined;
  const toolInput = attrs
    ? getNestedValue(attrs, TOOL_IO_ATTR_KEYS.input)
    : undefined;
  const toolOutput = attrs
    ? getNestedValue(attrs, TOOL_IO_ATTR_KEYS.output)
    : undefined;
  const blockReason = attrs
    ? getNestedValue(attrs, HOOK_BLOCK_REASON_KEY)
    : undefined;
  const toolError = attrs ? getNestedValue(attrs, HOOK_ERROR_KEY) : undefined;
  const toolCallID = attrs ? getNestedValue(attrs, "gen_ai.tool.call.id") : "";
  const toolName = attrs ? getNestedValue(attrs, "gram.tool.name") : "";
  const showToolIOHiddenMessage = Boolean(
    (toolCallID || toolName) && !toolInput,
  );
  const highlights = attrs
    ? HIGHLIGHT_ATTR_KEYS.map(({ path, label }) => ({
        path,
        label,
        value: getNestedValue(attrs, path),
      })).filter((h): h is typeof h & { value: string } => Boolean(h.value))
    : [];

  // Remove surfaced keys from attributes to avoid duplication in the generic section
  let filteredAttrs = attrs;
  if (filteredAttrs && toolInput) {
    filteredAttrs = removeNestedKey(filteredAttrs, TOOL_IO_ATTR_KEYS.input);
  }
  if (filteredAttrs && toolOutput) {
    filteredAttrs = removeNestedKey(filteredAttrs, TOOL_IO_ATTR_KEYS.output);
  }
  if (filteredAttrs && blockReason) {
    filteredAttrs = removeNestedKey(filteredAttrs, HOOK_BLOCK_REASON_KEY);
  }
  if (filteredAttrs && toolError) {
    filteredAttrs = removeNestedKey(filteredAttrs, HOOK_ERROR_KEY);
  }
  for (const h of highlights) {
    if (filteredAttrs) {
      filteredAttrs = removeNestedKey(filteredAttrs, h.path);
    }
  }

  return (
    <div className="flex flex-col gap-6 px-5 pt-6 pb-6">
      {/* Header with span info */}
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          {blockReason ? (
            <div className="bg-warning/10 text-warning inline-flex items-center gap-1.5 px-2 py-1 text-xs font-semibold uppercase">
              <ShieldAlert className="size-3" />
              Blocked
            </div>
          ) : (
            <div
              className={`px-2 py-1 text-xs font-semibold uppercase ${severityClass} bg-muted`}
            >
              {log.severityText || "INFO"}
            </div>
          )}
          <SheetTitle className="text-base font-medium tracking-tight">
            {log.body?.slice(0, 80) || "(no message)"}
          </SheetTitle>
        </div>

        {blockReason && (
          <div className="border-warning/40 bg-warning/10 flex items-start gap-3 border p-3">
            <ShieldAlert className="text-warning mt-0.5 size-4 shrink-0" />
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="text-warning text-xs font-semibold tracking-wide uppercase">
                Block Reason
              </div>
              <div className="text-foreground text-sm break-words">
                {blockReason}
              </div>
            </div>
          </div>
        )}

        {/* Metadata badges */}
        <div className="flex flex-wrap gap-2">
          <MetadataBadge
            label="Service"
            value={log.service?.name || "Unknown"}
          />
          {gramUrn && (
            <MetadataBadge
              label="Platform URN"
              value={gramUrn}
              mono
              copyValue={gramUrn}
            />
          )}
          {log.traceId && (
            <MetadataBadge
              label="Trace ID"
              value={log.traceId}
              mono
              copyValue={log.traceId}
            />
          )}
          {log.spanId && (
            <MetadataBadge
              label="Span ID"
              value={log.spanId}
              mono
              copyValue={log.spanId}
            />
          )}
          <MetadataBadge
            label="Time"
            value={formatNanoTimestamp(log.timeUnixNano)}
          />
        </div>
      </div>

      {/* Tabs: Details / Raw Data */}
      <Tabs defaultValue="details" className="w-full flex-1">
        <TabsList className="w-full">
          <TabsTrigger value="details" className="flex-1">
            Details
          </TabsTrigger>
          <TabsTrigger value="raw" className="flex-1">
            Raw Data
          </TabsTrigger>
        </TabsList>

        <TabsContent value="details" className="mt-5 flex flex-col gap-5">
          {/* Tool Error — destructive styling so failures pop visually */}
          {toolError && (
            <div className="border-destructive/40 bg-destructive/10 flex items-start gap-3 border p-3">
              <CircleAlert className="text-destructive mt-0.5 size-4 shrink-0" />
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <div className="text-destructive text-xs font-semibold tracking-wide uppercase">
                  Tool Error
                </div>
                <div className="text-foreground text-sm break-words">
                  {toolError}
                </div>
              </div>
            </div>
          )}

          {/* Highlights — prominent labeled rows pulled out of attributes */}
          {highlights.length > 0 && (
            <div className="border-border bg-muted/40 grid grid-cols-1 gap-x-4 gap-y-2 border p-4 sm:grid-cols-[max-content_minmax(0,1fr)]">
              {highlights.map((h) => (
                <div
                  key={h.path}
                  className="[&>div:first-child]:text-muted-foreground contents [&>div:first-child]:self-center [&>div:first-child]:text-xs [&>div:first-child]:font-medium [&>div:first-child]:tracking-wide [&>div:first-child]:uppercase"
                >
                  <div>{h.label}</div>
                  <div className="text-foreground font-mono text-sm break-all">
                    {h.value}
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Tool Input */}
          {toolInput && (
            <CollapsibleBodySection title="Tool Input" content={toolInput} />
          )}
          {showToolIOHiddenMessage && (
            <div className="text-muted-foreground bg-muted/30 border-border border px-3 py-2 text-sm">
              Tool arguments are not shown when tool_io_logs are disabled.
            </div>
          )}

          {/* Tool Output */}
          {toolOutput && (
            <CollapsibleBodySection title="Tool Output" content={toolOutput} />
          )}

          {/* Attributes (with tool I/O + highlighted keys removed) */}
          {filteredAttrs && Object.keys(filteredAttrs).length > 0 && (
            <AttributesSection
              title="Attributes"
              data={filteredAttrs}
              onAddFilter={onAddFilter}
            />
          )}

          {/* Resource — no onAddFilter: the backend's attribute filter
              resolves paths against `attributes.*`, not `resource_attributes.*`,
              so resource-derived filters would silently return no results. */}
          {log.resourceAttributes &&
            Object.keys(log.resourceAttributes as object).length > 0 && (
              <AttributesSection
                title="Resource"
                data={log.resourceAttributes as Record<string, unknown>}
              />
            )}

          {/* Message — demoted to a collapsed section below attributes. The
              body is the OTEL log body, which for tool-call events is just a
              "Tool: X, Hook: Y" stub that duplicates info now shown above. */}
          {log.body && (
            <CollapsibleBodySection
              title="Message"
              content={log.body}
              defaultOpen={false}
            />
          )}
        </TabsContent>

        <TabsContent value="raw" className="mt-5 flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
              Full Log Record
            </div>
            <button
              className="hover:bg-muted p-1.5"
              onClick={() => {
                void navigator.clipboard.writeText(
                  JSON.stringify(log, null, 2),
                );
              }}
            >
              <Copy className="size-4" />
            </button>
          </div>
          <div className="bg-muted/40 border-border flex-1 overflow-y-auto border p-4">
            <pre className="font-mono text-sm break-all whitespace-pre-wrap">
              {JSON.stringify(log, null, 2)}
            </pre>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function CollapsibleBodySection({
  title,
  content,
  defaultOpen = true,
}: {
  title: string;
  content: string;
  defaultOpen?: boolean;
}) {
  const [isOpen, setIsOpen] = useState(defaultOpen);

  // Try to pretty-print JSON content
  let displayContent = content;
  try {
    const parsed = JSON.parse(content);
    displayContent = JSON.stringify(parsed, null, 2);
  } catch {
    // Not JSON, use as-is
  }

  return (
    <div className="flex flex-col gap-2">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="group flex items-center justify-between"
      >
        <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
          {title}
        </div>
        <div className="flex items-center gap-1">
          <button
            className="hover:bg-muted p-1.5"
            onClick={(e) => {
              e.stopPropagation();
              void navigator.clipboard.writeText(content);
            }}
          >
            <Copy className="size-4" />
          </button>
          <ChevronDown
            className={cn(
              "text-muted-foreground size-4 transition-transform",
              !isOpen && "-rotate-90",
            )}
          />
        </div>
      </button>
      {isOpen && (
        <div className="bg-muted/40 border-border max-h-96 overflow-y-auto border p-4">
          <pre className="font-mono text-sm break-words whitespace-pre-wrap">
            {displayContent}
          </pre>
        </div>
      )}
    </div>
  );
}

function MetadataBadge({
  label,
  value,
  mono = false,
  copyValue,
}: {
  label: string;
  value: string;
  mono?: boolean;
  copyValue?: string;
}) {
  return (
    <button
      className="bg-muted/50 hover:bg-muted flex items-center gap-2 px-3 py-1.5 text-sm transition-colors"
      onClick={() => {
        if (copyValue) {
          void navigator.clipboard.writeText(copyValue);
        }
      }}
      disabled={!copyValue}
    >
      <span className="text-muted-foreground shrink-0">{label}:</span>
      <span className={mono ? "font-mono text-xs" : "font-medium"}>
        {value}
      </span>
    </button>
  );
}

interface AttributeEntry {
  key: string;
  displayValue: string;
  filterValue: string | null;
}

/**
 * Flatten a nested object into dot-notation keys with filterability metadata.
 * e.g. { http: { request: { method: "POST" } } } =>
 *   [{ key: "http.request.method", displayValue: "POST", filterValue: "POST" }]
 */
function flattenObject(
  obj: Record<string, unknown>,
  prefix = "",
): AttributeEntry[] {
  const result: AttributeEntry[] = [];

  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;

    if (value === null || value === undefined) {
      result.push({ key: fullKey, displayValue: "\u2014", filterValue: null });
      continue;
    }

    switch (typeof value) {
      case "object":
        if (Array.isArray(value)) {
          result.push({
            key: fullKey,
            displayValue: JSON.stringify(value),
            filterValue: null,
          });
        } else if (Object.keys(value).length > 0) {
          result.push(
            ...flattenObject(value as Record<string, unknown>, fullKey),
          );
        }
        break;
      case "string":
        // Empty strings are treated as non-filterable (same as null/undefined):
        // a `key = ""` filter matches empty and missing values equivalently
        // server-side, so surfacing it as a filter option would be misleading.
        result.push({
          key: fullKey,
          displayValue: value || "\u2014",
          filterValue: value || null,
        });
        break;
      case "number":
      case "boolean":
        result.push({
          key: fullKey,
          displayValue: String(value),
          filterValue: String(value),
        });
        break;
      case "bigint":
      case "function":
      case "symbol":
      case "undefined":
      default:
        result.push({
          key: fullKey,
          displayValue: JSON.stringify(value),
          filterValue: JSON.stringify(value),
        });
    }
  }

  return result;
}

function AttributesSection({
  title,
  data,
  onAddFilter,
}: {
  title: string;
  data: Record<string, unknown>;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}) {
  const flatEntries = flattenObject(data);
  // Controlled state: track which single row's menu is open. Each row has its
  // own DropdownMenu instance, so we need a coordinator to ensure only one is
  // open at a time (Radix's default uncontrolled behavior opened each one
  // independently inside the Sheet modal stack).
  const [openMenuKey, setOpenMenuKey] = useState<string | null>(null);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
          {title}
        </div>
        <button
          className="hover:bg-muted p-1.5"
          onClick={() => {
            void navigator.clipboard.writeText(JSON.stringify(data, null, 2));
          }}
        >
          <Copy className="size-4" />
        </button>
      </div>
      <div className="bg-muted/40 border-border divide-border divide-y border">
        {flatEntries.map((entry) => {
          const isFilterable = entry.filterValue !== null;

          const rowContent = (
            <>
              <span className="text-muted-foreground text-xs">{entry.key}</span>
              <span className="font-mono text-sm break-all">
                {entry.displayValue}
              </span>
            </>
          );

          if (!onAddFilter) {
            return (
              <div
                key={entry.key}
                className="hover:bg-muted/50 flex flex-col gap-1 px-4 py-2.5 transition-colors"
              >
                {rowContent}
              </div>
            );
          }

          return (
            <DropdownMenu
              key={entry.key}
              open={openMenuKey === entry.key}
              onOpenChange={(open) => setOpenMenuKey(open ? entry.key : null)}
            >
              <DropdownMenuTrigger asChild>
                <button
                  className="hover:bg-muted/50 flex w-full cursor-pointer flex-col gap-1 px-4 py-2.5 text-left transition-colors"
                  aria-label={`Attribute actions for ${entry.key}`}
                >
                  {rowContent}
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() => {
                    // Runtime guard: Radix `disabled` sets aria-disabled but
                    // does not suppress onClick, so re-check filterValue here.
                    if (entry.filterValue !== null) {
                      onAddFilter(entry.key, Operator.Eq, entry.filterValue);
                    }
                  }}
                >
                  <span className="flex items-center gap-1">
                    Filter by
                    <span className="max-w-[200px] truncate font-mono text-xs">
                      {entry.key} = {entry.filterValue ?? ""}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() => {
                    if (entry.filterValue !== null) {
                      onAddFilter(entry.key, Operator.NotEq, entry.filterValue);
                    }
                  }}
                >
                  <span className="flex items-center gap-1">
                    Exclude
                    <span className="max-w-[200px] truncate font-mono text-xs">
                      {entry.key} != {entry.filterValue ?? ""}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() => {
                    if (entry.filterValue !== null) {
                      onAddFilter(
                        entry.key,
                        Operator.Contains,
                        entry.filterValue,
                      );
                    }
                  }}
                >
                  <span className="flex items-center gap-1">
                    Contains
                    <span className="max-w-[200px] truncate font-mono text-xs">
                      {entry.filterValue ?? ""}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() => {
                    void navigator.clipboard.writeText(entry.displayValue);
                  }}
                >
                  Copy value
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          );
        })}
      </div>
    </div>
  );
}
