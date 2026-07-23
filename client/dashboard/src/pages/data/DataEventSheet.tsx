import { CopyButton } from "@/components/ui/copy-button";
import { Separator } from "@/components/ui/separator";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { CheckIcon, XIcon } from "lucide-react";
import {
  evaluateQuality,
  eventUrn,
  type DataEvent,
  type EventQuality,
} from "./data-events";
import { KindBadge, OriginBadge, QualityPill } from "./DataEventBadges";

/**
 * The measures a metric event can carry, in display order. Only the keys
 * present on the event are rendered.
 */
const MEASURE_KEYS: { key: string; label: string }[] = [
  { key: "gen_ai.usage.input_tokens", label: "Input tokens" },
  { key: "gen_ai.usage.output_tokens", label: "Output tokens" },
  { key: "gen_ai.usage.cache_read_tokens", label: "Cache read tokens" },
  { key: "gen_ai.usage.reasoning_tokens", label: "Reasoning tokens" },
  { key: "gen_ai.usage.cost_usd", label: "Cost (USD)" },
  { key: "gram.billing.total_cents", label: "Model cost (cents)" },
  { key: "gram.billing.charged_cents", label: "Charged (cents)" },
];

function formatMeasure(value: string | number | boolean): string {
  if (typeof value === "number") {
    return value.toLocaleString();
  }
  return String(value);
}

function SectionTitle({ children }: { children: string }): JSX.Element {
  return (
    <Type small className="text-muted-foreground font-medium uppercase">
      {children}
    </Type>
  );
}

function QualitySection({ quality }: { quality: EventQuality }): JSX.Element {
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <SectionTitle>Data quality</SectionTitle>
        <QualityPill quality={quality} />
      </div>
      {quality.grade === "unclassified" && (
        <Type muted small>
          No ingest rule recognized this event, so it cannot be attributed to a
          known producer class. It is kept visible instead of dropped.
        </Type>
      )}
      <ul className="space-y-1">
        {quality.checks.map((check) => (
          <li key={check.key} className="flex items-center gap-2">
            {check.present ? (
              <CheckIcon className="text-success size-4 shrink-0" />
            ) : (
              <XIcon className="text-destructive size-4 shrink-0" />
            )}
            <Type small className={cn(!check.present && "text-destructive")}>
              {check.label}
            </Type>
            <Type muted small className="ml-auto font-mono">
              {check.key}
            </Type>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Metric events render as a measurement: a grid of the values it reported. */
function MeasurementPanel({ event }: { event: DataEvent }): JSX.Element {
  const measures = MEASURE_KEYS.filter(
    (measure) => event.attributes[measure.key] !== undefined,
  );

  return (
    <div className="space-y-2">
      <SectionTitle>Measurement</SectionTitle>
      {measures.length === 0 && (
        <Type muted small>
          This metric event reported no recognized measures.
        </Type>
      )}
      <div className="grid grid-cols-2 gap-2">
        {measures.map((measure) => (
          <div key={measure.key} className="rounded-md border p-3">
            <Type muted small className="block">
              {measure.label}
            </Type>
            <Type className="font-mono font-medium">
              {formatMeasure(event.attributes[measure.key]!)}
            </Type>
          </div>
        ))}
      </div>
    </div>
  );
}

/** Log events render their body verbatim, the way the producer sent it. */
function LogBodyPanel({ event }: { event: DataEvent }): JSX.Element {
  return (
    <div className="space-y-2">
      <SectionTitle>Body</SectionTitle>
      <pre className="bg-muted overflow-x-auto rounded-md border p-3 font-mono text-xs whitespace-pre-wrap">
        {event.body}
      </pre>
    </div>
  );
}

function RawAttributes({ event }: { event: DataEvent }): JSX.Element {
  const json = JSON.stringify(event.attributes, null, 2);

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <SectionTitle>Attributes</SectionTitle>
        <CopyButton text={json} size="inline" tooltip="Copy attributes" />
      </div>
      <pre className="bg-muted max-h-80 overflow-auto rounded-md border p-3 font-mono text-xs">
        {json}
      </pre>
    </div>
  );
}

export function DataEventSheet({
  event,
  onClose,
}: {
  event: DataEvent | null;
  onClose: () => void;
}): JSX.Element {
  return (
    <Sheet
      open={event !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        {event && (
          <>
            <SheetHeader>
              <div className="flex items-center gap-2">
                <SheetTitle className="font-mono">{event.type}</SheetTitle>
                <KindBadge kind={event.kind} />
                <OriginBadge origin={event.origin} />
              </div>
              <SheetDescription>
                Observed from {event.producer} —{" "}
                {dateTimeFormatters.logTimestamp.format(event.timestamp)}
              </SheetDescription>
              <div className="flex items-center gap-1">
                <Type muted small className="truncate font-mono">
                  {eventUrn(event)}
                </Type>
                <CopyButton
                  text={eventUrn(event)}
                  size="inline"
                  tooltip="Copy event URN"
                />
              </div>
            </SheetHeader>
            <div className="space-y-6 px-4 pb-8">
              <QualitySection quality={evaluateQuality(event)} />
              <Separator />
              {event.kind === "metric" ? (
                <MeasurementPanel event={event} />
              ) : (
                <LogBodyPanel event={event} />
              )}
              <RawAttributes event={event} />
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
