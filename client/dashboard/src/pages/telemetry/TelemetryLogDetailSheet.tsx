import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { Copy } from "lucide-react";
import { formatNanoTimestamp, getSeverityColorClass } from "./utils";

interface TelemetryLogDetailSheetProps {
  log: TelemetryLogRecord | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function TelemetryLogDetailSheet({
  log,
  open,
  onOpenChange,
}: TelemetryLogDetailSheetProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="!w-[33vw] !min-w-[400px] !max-w-none sm:!max-w-none h-full max-h-screen overflow-y-auto">
        {log && <LogDetailContent log={log} />}
      </SheetContent>
    </Sheet>
  );
}

function LogDetailContent({ log }: { log: TelemetryLogRecord }) {
  const severityClass = getSeverityColorClass(log.severityText);

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
              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <div className="text-xs font-medium uppercase text-muted-foreground tracking-wide">
                    Attributes
                  </div>
                  <button
                    className="p-1.5 rounded hover:bg-surface-secondary-default"
                    onClick={() => {
                      void navigator.clipboard.writeText(
                        JSON.stringify(log.attributes, null, 2),
                      );
                    }}
                  >
                    <Copy className="size-4" />
                  </button>
                </div>
                <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4 max-h-[300px] overflow-y-auto">
                  <pre className="font-mono text-sm whitespace-pre-wrap break-all">
                    {JSON.stringify(log.attributes, null, 2)}
                  </pre>
                </div>
              </div>
            )}

          {/* Resource */}
          {log.resourceAttributes &&
            Object.keys(log.resourceAttributes as object).length > 0 && (
              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <div className="text-xs font-medium uppercase text-muted-foreground tracking-wide">
                    Resource
                  </div>
                  <button
                    className="p-1.5 rounded hover:bg-surface-secondary-default"
                    onClick={() => {
                      void navigator.clipboard.writeText(
                        JSON.stringify(log.resourceAttributes, null, 2),
                      );
                    }}
                  >
                    <Copy className="size-4" />
                  </button>
                </div>
                <div className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4 max-h-[250px] overflow-y-auto">
                  <pre className="font-mono text-sm whitespace-pre-wrap break-all">
                    {JSON.stringify(log.resourceAttributes, null, 2)}
                  </pre>
                </div>
              </div>
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
      className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-surface-tertiary-default hover:bg-surface-secondary-default transition-colors text-sm"
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
