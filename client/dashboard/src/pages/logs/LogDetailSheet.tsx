import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { ChevronDown, Copy } from "lucide-react";
import { useState } from "react";
import { formatNanoTimestamp, getSeverityColorClass } from "./utils";

interface LogDetailSheetProps {
  log: TelemetryLogRecord | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function LogDetailSheet({
  log,
  open,
  onOpenChange,
}: LogDetailSheetProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="h-full max-h-screen overflow-y-auto"
        style={{ width: "33vw", minWidth: 500, maxWidth: "none" }}
      >
        {log && <LogDetailContent log={log} />}
      </SheetContent>
    </Sheet>
  );
}

/** Keys used to store tool I/O content in telemetry attributes (OTel GenAI semantic conventions). */
const TOOL_IO_ATTR_KEYS = {
  input: "gen_ai.tool.call.arguments",
  output: "gen_ai.tool.call.result",
} as const;

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
    if (current === null || current === undefined || typeof current !== "object")
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

function LogDetailContent({ log }: { log: TelemetryLogRecord }) {
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
    <div className="flex flex-col gap-6 pt-6 px-5 pb-6">
      {/* Header with span info */}
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <div
            className={`text-xs font-semibold uppercase px-2 py-1 rounded ${severityClass} bg-muted`}
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

        <TabsContent value="details" className="flex flex-col gap-5 mt-5">
          {/* Message */}
          <div className="flex flex-col gap-2">
            <div className="text-xs font-medium uppercase text-muted-foreground tracking-wide">
              Message
            </div>
            <div className="bg-muted border border-border rounded-lg p-4">
              <pre className="font-mono text-sm whitespace-pre-wrap break-words">
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
          {filteredAttrs &&
            Object.keys(filteredAttrs).length > 0 && (
              <AttributesSection
                title="Attributes"
                data={filteredAttrs}
              />
            )}

          {/* Resource */}
          {log.resourceAttributes &&
            Object.keys(log.resourceAttributes as object).length > 0 && (
              <AttributesSection
                title="Resource"
                data={log.resourceAttributes as Record<string, unknown>}
              />
            )}
        </TabsContent>

        <TabsContent value="raw" className="flex flex-col gap-3 mt-5">
          <div className="flex items-center justify-between">
            <div className="text-xs font-medium uppercase text-muted-foreground tracking-wide">
              Full Log Record
            </div>
            <button
              className="p-1.5 rounded hover:bg-muted"
              onClick={() => {
                void navigator.clipboard.writeText(
                  JSON.stringify(log, null, 2),
                );
              }}
            >
              <Copy className="size-4" />
            </button>
          </div>
          <div className="bg-muted border border-border rounded-lg p-4 overflow-y-auto flex-1">
            <pre className="font-mono text-sm whitespace-pre-wrap break-all">
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
        className="flex items-center justify-between group"
      >
        <div className="text-xs font-medium uppercase text-muted-foreground tracking-wide">
          {title}
        </div>
        <div className="flex items-center gap-1">
          <button
            className="p-1.5 rounded hover:bg-muted"
            onClick={(e) => {
              e.stopPropagation();
              void navigator.clipboard.writeText(content);
            }}
          >
            <Copy className="size-4" />
          </button>
          <ChevronDown
            className={cn(
              "size-4 text-muted-foreground transition-transform",
              !isOpen && "-rotate-90",
            )}
          />
        </div>
      </button>
      {isOpen && (
        <div className="bg-muted border border-border rounded-lg p-4 max-h-96 overflow-y-auto">
          <pre className="font-mono text-sm whitespace-pre-wrap break-words">
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
      className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-muted/50 hover:bg-muted transition-colors text-sm"
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

function AttributesSection({
  title,
  data,
}: {
  title: string;
  data: Record<string, unknown>;
}) {
  const flatEntries = flattenObject(data);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="text-xs font-medium uppercase text-muted-foreground tracking-wide">
          {title}
        </div>
        <button
          className="p-1.5 rounded hover:bg-muted"
          onClick={() => {
            void navigator.clipboard.writeText(JSON.stringify(data, null, 2));
          }}
        >
          <Copy className="size-4" />
        </button>
      </div>
      <div className="bg-muted border border-border rounded-lg divide-y divide-border">
        {flatEntries.map(([key, value]) => (
          <div
            key={key}
            className="flex flex-col gap-1 px-4 py-2.5 hover:bg-muted/50 transition-colors"
          >
            <span className="text-xs text-muted-foreground">{key}</span>
            <span className="text-sm font-mono break-all">{value}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * Flatten a nested object into dot-notation keys
 * e.g. { http: { request: { method: "POST" } } } => [["http.request.method", "POST"]]
 */
function flattenObject(
  obj: Record<string, unknown>,
  prefix = "",
): [string, string][] {
  const result: [string, string][] = [];

  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;

    if (value === null || value === undefined) {
      result.push([fullKey, "—"]);
      continue;
    }

    switch (typeof value) {
      case "object":
        if (Array.isArray(value)) {
          result.push([fullKey, JSON.stringify(value)]);
        } else if (Object.keys(value).length > 0) {
          result.push(
            ...flattenObject(value as Record<string, unknown>, fullKey),
          );
        }
        break;
      case "string":
        result.push([fullKey, value || "—"]);
        break;
      case "number":
      case "boolean":
        result.push([fullKey, String(value)]);
        break;
      default:
        result.push([fullKey, JSON.stringify(value)]);
    }
  }

  return result;
}
