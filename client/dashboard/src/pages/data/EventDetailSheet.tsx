import { unixNanoToDate } from "@/components/chart/chartUtils";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/Sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { useOrganization } from "@/contexts/Auth";
import { formatPlatform } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import type { EventLogEntry } from "@gram/client/models/components/eventlogentry.js";
import { Copy } from "lucide-react";
import { EventKindBadge, EventSourceIcon } from "./event-display";

interface EventDetailSheetProps {
  event: EventLogEntry | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * Detail sheet for a single event-feed entry (an ingested OpenTelemetry log
 * record or span). Two tabs: Parsed (flattened attribute key/value rows with
 * per-value copy) and Raw (the pretty-printed full record). Modeled on
 * `LogDetailSheet`.
 */
export function EventDetailSheet({
  event,
  open,
  onOpenChange,
}: EventDetailSheetProps): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="h-full max-h-screen overflow-y-auto"
        style={{
          width: "33vw",
          minWidth: "min(500px, 100vw)",
          maxWidth: "none",
        }}
      >
        {event && <EventDetailContent event={event} />}
      </SheetContent>
    </Sheet>
  );
}

function EventDetailContent({ event }: { event: EventLogEntry }) {
  const organization = useOrganization();
  const projectLabel =
    organization.projects.find((p) => p.id === event.projectId)?.name ??
    event.projectId;
  const timeLabel = unixNanoToDate(event.timeUnixNano).toLocaleString();
  const rawJson = JSON.stringify(event, null, 2);

  return (
    <div className="flex flex-col gap-6 px-5 pt-6 pb-6">
      {/* Header — kind + source marks over the name, then a meta subtitle */}
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <EventKindBadge kind={event.kind} />
            <EventSourceIcon
              source={event.source}
              className="size-4 shrink-0"
            />
          </div>
          <SheetTitle className="font-display text-xl font-light tracking-tight break-words">
            {event.name || "(unnamed event)"}
          </SheetTitle>
          <p className="text-muted-foreground text-sm">
            {formatPlatform(event.source)} &middot; {projectLabel} &middot;{" "}
            {timeLabel}
          </p>
        </div>

        {/* Meta — definition-list rows with eyebrow keys and copyable mono values */}
        <div className="border-border divide-border flex flex-col divide-y border-y">
          <MetadataRow label="Record ID" value={event.recordId} />
          {event.traceId && (
            <MetadataRow label="Trace ID" value={event.traceId} />
          )}
          {event.spanId && <MetadataRow label="Span ID" value={event.spanId} />}
        </div>
      </div>

      {/* Tabs: Parsed / Raw */}
      <Tabs defaultValue="parsed" className="w-full flex-1">
        <TabsList className="w-full">
          <TabsTrigger value="parsed" className="flex-1">
            Parsed
          </TabsTrigger>
          <TabsTrigger value="raw" className="flex-1">
            Raw
          </TabsTrigger>
        </TabsList>

        <TabsContent value="parsed" className="mt-5 flex flex-col gap-5">
          {event.bodyPreview && <BodySection body={event.bodyPreview} />}
          <AttributeSection
            title="Attributes"
            data={asRecord(event.attributes)}
          />
          <AttributeSection
            title="Resource"
            data={asRecord(event.resourceAttributes)}
          />
        </TabsContent>

        <TabsContent value="raw" className="mt-5 flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="text-eyebrow">Full Event Record</div>
            <CopyButton value={rawJson} label="Copy full event record" />
          </div>
          <div className="border-border flex-1 overflow-y-auto border p-4">
            <pre className="font-mono text-sm break-all whitespace-pre-wrap">
              {rawJson}
            </pre>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

// The Clipboard API is undefined in insecure contexts, so guard before
// writing rather than throwing from the click handler.
function copyToClipboard(value: string) {
  void navigator.clipboard?.writeText(value);
}

function CopyButton({
  value,
  label,
  className,
}: {
  value: string;
  label: string;
  className?: string;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      className={cn("hover:bg-muted p-1.5", className)}
      onClick={() => copyToClipboard(value)}
    >
      <Copy aria-hidden="true" className="size-4" />
    </button>
  );
}

function MetadataRow({ label, value }: { label: string; value: string }) {
  return (
    <button
      type="button"
      className="hover:bg-muted/50 flex items-center justify-between gap-4 py-2 text-left transition-colors"
      onClick={() => copyToClipboard(value)}
      title={`Copy ${label}`}
    >
      <span className="text-eyebrow shrink-0">{label}</span>
      <span className="min-w-0 truncate font-mono text-xs">{value}</span>
    </button>
  );
}

function BodySection({ body }: { body: string }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="text-eyebrow">Body</div>
        <CopyButton value={body} label="Copy body" />
      </div>
      <div className="border-border max-h-96 overflow-y-auto border p-4">
        <pre className="font-mono text-sm break-words whitespace-pre-wrap">
          {body}
        </pre>
      </div>
    </div>
  );
}

interface FlatAttribute {
  key: string;
  value: string;
}

// Flatten a nested attribute object into dot-notation key/value rows,
// e.g. { http: { method: "POST" } } => [{ key: "http.method", value: "POST" }].
function flattenAttributes(
  obj: Record<string, unknown>,
  prefix = "",
): FlatAttribute[] {
  const result: FlatAttribute[] = [];
  for (const [key, raw] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    if (raw !== null && typeof raw === "object" && !Array.isArray(raw)) {
      const nested = raw as Record<string, unknown>;
      if (Object.keys(nested).length > 0) {
        result.push(...flattenAttributes(nested, fullKey));
      }
      continue;
    }
    result.push({ key: fullKey, value: stringifyAttributeValue(raw) });
  }
  return result;
}

function stringifyAttributeValue(value: unknown): string {
  if (value === null || value === undefined) return "\u2014";
  if (typeof value === "string") return value || "\u2014";
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return JSON.stringify(value);
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (value !== null && typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return null;
}

function AttributeSection({
  title,
  data,
}: {
  title: string;
  data: Record<string, unknown> | null;
}) {
  if (!data || Object.keys(data).length === 0) return null;
  const entries = flattenAttributes(data);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="text-eyebrow">{title}</div>
        <CopyButton
          value={JSON.stringify(data, null, 2)}
          label={`Copy ${title.toLowerCase()}`}
        />
      </div>
      <div className="border-border divide-border divide-y border-y">
        {entries.map((entry) => (
          <div
            key={entry.key}
            className="group hover:bg-muted/50 relative flex flex-col gap-1 px-2 py-2.5 transition-colors"
          >
            <span className="text-muted-foreground pr-8 text-xs break-all">
              {entry.key}
            </span>
            <span className="pr-8 font-mono text-sm break-all">
              {entry.value}
            </span>
            <CopyButton
              value={entry.value}
              label={`Copy ${entry.key}`}
              className="absolute top-1.5 right-1.5 opacity-0 group-hover:opacity-100"
            />
          </div>
        ))}
      </div>
    </div>
  );
}
