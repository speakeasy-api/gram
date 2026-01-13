import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { Copy } from "lucide-react";
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

function LogDetailContent({ log }: { log: TelemetryLogRecord }) {
  const severityClass = getSeverityColorClass(log.severityText);
  const resourceAttrs = log.resourceAttributes as
    | { gram?: { tool?: { urn?: string } } }
    | undefined;
  const gramUrn = resourceAttrs?.gram?.tool?.urn;

  return (
    <div className="flex flex-col gap-6 pt-6 px-5 pb-6">
      {/* Header with span info */}
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <div
            className={`text-xs font-semibold uppercase px-2 py-1 rounded ${severityClass} bg-surface-secondary-default`}
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
            <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4">
              <pre className="font-mono text-sm whitespace-pre-wrap break-words">
                {log.body || "(no message)"}
              </pre>
            </div>
          </div>

          {/* Attributes */}
          {log.attributes &&
            Object.keys(log.attributes as object).length > 0 && (
              <AttributesSection
                title="Attributes"
                data={log.attributes as Record<string, unknown>}
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
              className="p-1.5 rounded hover:bg-surface-secondary-default"
              onClick={() => {
                void navigator.clipboard.writeText(
                  JSON.stringify(log, null, 2),
                );
              }}
            >
              <Copy className="size-4" />
            </button>
          </div>
          <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4 overflow-y-auto flex-1">
            <pre className="font-mono text-sm whitespace-pre-wrap break-all">
              {JSON.stringify(log, null, 2)}
            </pre>
          </div>
        </TabsContent>
      </Tabs>
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
      className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-surface-tertiary hover:bg-surface-secondary-default transition-colors text-sm"
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
          className="p-1.5 rounded hover:bg-surface-secondary-default"
          onClick={() => {
            void navigator.clipboard.writeText(JSON.stringify(data, null, 2));
          }}
        >
          <Copy className="size-4" />
        </button>
      </div>
      <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg divide-y divide-neutral-softest">
        {flatEntries.map(([key, value]) => (
          <div
            key={key}
            className="flex flex-col gap-1 px-4 py-2.5 hover:bg-surface-tertiary transition-colors"
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
