import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { Operator } from "@gram/client/models/components/logfilter";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@speakeasy-api/moonshine";
import { ChevronDown, Copy } from "lucide-react";
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
}: LogDetailSheetProps) {
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

function truncateValue(value: string, maxLen = 24): string {
  return value.length > maxLen ? `${value.slice(0, maxLen)}\u2026` : value;
}

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
    const next = current[parts[i]];
    if (next === null || next === undefined || typeof next !== "object")
      return clone;
    current = next as Record<string, unknown>;
  }
  delete current[parts[parts.length - 1]];
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

  // Remove tool I/O keys from attributes to avoid duplication in the generic section
  let filteredAttrs = attrs;
  if (filteredAttrs && toolInput) {
    filteredAttrs = removeNestedKey(filteredAttrs, TOOL_IO_ATTR_KEYS.input);
  }
  if (filteredAttrs && toolOutput) {
    filteredAttrs = removeNestedKey(filteredAttrs, TOOL_IO_ATTR_KEYS.output);
  }

  return (
    <div className="flex flex-col gap-6 px-5 pt-6 pb-6">
      {/* Header with span info */}
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <div
            className={`rounded px-2 py-1 text-xs font-semibold uppercase ${severityClass} bg-muted`}
          >
            {log.severityText || "INFO"}
          </div>
          <SheetTitle className="text-base font-medium tracking-tight">
            {log.body?.slice(0, 80) || "(no message)"}
          </SheetTitle>
        </div>

        {/* Metadata badges */}
        <div className="flex flex-wrap gap-2">
          <MetadataBadge
            label="Service"
            value={log.service?.name || "Unknown"}
          />
          {gramUrn && (
            <MetadataBadge
              label="Gram URN"
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
          {/* Message */}
          <div className="flex flex-col gap-2">
            <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
              Message
            </div>
            <div className="bg-muted border-border rounded-lg border p-4">
              <pre className="font-mono text-sm break-words whitespace-pre-wrap">
                {log.body || "(no message)"}
              </pre>
            </div>
          </div>

          {/* Tool Input */}
          {toolInput && (
            <CollapsibleBodySection title="Tool Input" content={toolInput} />
          )}

          {/* Tool Output */}
          {toolOutput && (
            <CollapsibleBodySection title="Tool Output" content={toolOutput} />
          )}

          {/* Attributes (with tool I/O keys removed) */}
          {filteredAttrs && Object.keys(filteredAttrs).length > 0 && (
            <AttributesSection
              title="Attributes"
              data={filteredAttrs}
              onAddFilter={onAddFilter}
            />
          )}

          {/* Resource */}
          {log.resourceAttributes &&
            Object.keys(log.resourceAttributes as object).length > 0 && (
              <AttributesSection
                title="Resource"
                data={log.resourceAttributes as Record<string, unknown>}
                onAddFilter={onAddFilter}
              />
            )}
        </TabsContent>

        <TabsContent value="raw" className="mt-5 flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
              Full Log Record
            </div>
            <button
              className="hover:bg-muted rounded p-1.5"
              onClick={() => {
                void navigator.clipboard.writeText(
                  JSON.stringify(log, null, 2),
                );
              }}
            >
              <Copy className="size-4" />
            </button>
          </div>
          <div className="bg-muted border-border flex-1 overflow-y-auto rounded-lg border p-4">
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
}: {
  title: string;
  content: string;
}) {
  const [isOpen, setIsOpen] = useState(true);

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
            className="hover:bg-muted rounded p-1.5"
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
        <div className="bg-muted border-border max-h-96 overflow-y-auto rounded-lg border p-4">
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
      className="bg-muted/50 hover:bg-muted flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors"
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
          className="hover:bg-muted rounded p-1.5"
          onClick={() => {
            void navigator.clipboard.writeText(JSON.stringify(data, null, 2));
          }}
        >
          <Copy className="size-4" />
        </button>
      </div>
      <div className="bg-muted border-border divide-border divide-y rounded-lg border">
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
                  <span>
                    Filter by{" "}
                    <span className="font-mono text-xs">
                      {entry.key} = {truncateValue(entry.filterValue ?? "")}
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
                  <span>
                    Exclude{" "}
                    <span className="font-mono text-xs">
                      {entry.key} != {truncateValue(entry.filterValue ?? "")}
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
                  <span>
                    Contains{" "}
                    <span className="font-mono text-xs">
                      {truncateValue(entry.filterValue ?? "")}
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
