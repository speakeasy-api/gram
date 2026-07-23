import { CopyButton } from "@/components/ui/copy-button";
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
import {
  CodeSnippet,
  Grid,
  Icon,
  Separator,
  Stack,
} from "@speakeasy-api/moonshine";
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
    <Stack gap={2}>
      <Stack direction="horizontal" justify="space-between" align="center">
        <SectionTitle>Data quality</SectionTitle>
        <QualityPill quality={quality} />
      </Stack>
      {quality.grade === "unclassified" && (
        <Type muted small>
          No ingest rule recognized this event, so it cannot be attributed to a
          known producer class. It is kept visible instead of dropped.
        </Type>
      )}
      <ul className="space-y-1">
        {quality.checks.map((check) => (
          <li key={check.key} className="flex items-center gap-2">
            <Icon
              name={check.present ? "check" : "x"}
              className={cn(
                "size-4 shrink-0",
                check.present ? "text-success" : "text-destructive",
              )}
            />
            <Type small className={cn(!check.present && "text-destructive")}>
              {check.label}
            </Type>
            <Type muted small className="ml-auto font-mono">
              {check.key}
            </Type>
          </li>
        ))}
      </ul>
    </Stack>
  );
}

/** Metric events render as a measurement: a grid of the values it reported. */
function MeasurementPanel({ event }: { event: DataEvent }): JSX.Element {
  const measures = MEASURE_KEYS.filter(
    (measure) => event.attributes[measure.key] !== undefined,
  );

  return (
    <Stack gap={2}>
      <SectionTitle>Measurement</SectionTitle>
      {measures.length === 0 && (
        <Type muted small>
          This metric event reported no recognized measures.
        </Type>
      )}
      <Grid columns={2} gap={2}>
        {measures.map((measure) => (
          <Grid.Item key={measure.key}>
            <div className="border-border rounded-md border p-3">
              <Type muted small className="block">
                {measure.label}
              </Type>
              <Type className="font-mono font-medium">
                {formatMeasure(event.attributes[measure.key]!)}
              </Type>
            </div>
          </Grid.Item>
        ))}
      </Grid>
    </Stack>
  );
}

/** Log events render their body verbatim, the way the producer sent it. */
function LogBodyPanel({ event }: { event: DataEvent }): JSX.Element {
  return (
    <Stack gap={2}>
      <SectionTitle>Body</SectionTitle>
      <CodeSnippet code={event.body} language="text" copyable={false} />
    </Stack>
  );
}

function RawAttributes({ event }: { event: DataEvent }): JSX.Element {
  return (
    <Stack gap={2}>
      <SectionTitle>Attributes</SectionTitle>
      <CodeSnippet
        code={JSON.stringify(event.attributes, null, 2)}
        language="json"
        copyable
      />
    </Stack>
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
              <Stack direction="horizontal" align="center" gap={2}>
                <SheetTitle className="font-mono">{event.type}</SheetTitle>
                <KindBadge kind={event.kind} />
                <OriginBadge origin={event.origin} />
              </Stack>
              <SheetDescription>
                Observed from {event.producer} —{" "}
                {dateTimeFormatters.logTimestamp.format(event.timestamp)}
              </SheetDescription>
              <Stack direction="horizontal" align="center" gap={1}>
                <Type muted small className="truncate font-mono">
                  {eventUrn(event)}
                </Type>
                <CopyButton
                  text={eventUrn(event)}
                  size="inline"
                  tooltip="Copy event URN"
                />
              </Stack>
            </SheetHeader>
            <Stack gap={6} className="px-4 pb-8">
              <QualitySection quality={evaluateQuality(event)} />
              <Separator />
              {event.kind === "metric" ? (
                <MeasurementPanel event={event} />
              ) : (
                <LogBodyPanel event={event} />
              )}
              <RawAttributes event={event} />
            </Stack>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
