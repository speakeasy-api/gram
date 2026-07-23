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
import { CodeSnippet, Icon, Separator, Stack } from "@speakeasy-api/moonshine";
import {
  evaluateQuality,
  eventUrn,
  ORIGIN_LABELS,
  type DataEvent,
  type QualityCheck,
} from "./data-events";
import { KindBadge, QualityPill, SourceBadge } from "./DataEventBadges";

function formatValue(value: string | number | boolean): string {
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

function ExpectedItemRow({
  check,
  event,
}: {
  check: QualityCheck;
  event: DataEvent;
}): JSX.Element {
  const value = event.attributes[check.key];

  return (
    <li className="flex items-center gap-2 px-3 py-2">
      <Icon
        name={check.present ? "check" : "x"}
        className={cn(
          "size-4 shrink-0",
          check.present ? "text-success-foreground" : "text-destructive",
        )}
      />
      <div className="min-w-0">
        <Type small className="block">
          {check.label}
        </Type>
        <Type muted small className="block truncate font-mono">
          {check.key}
        </Type>
      </div>
      {check.present && value !== undefined ? (
        <Type small className="ml-auto max-w-40 truncate text-right font-mono">
          {formatValue(value)}
        </Type>
      ) : (
        <Type small className="text-destructive ml-auto text-right">
          Missing
        </Type>
      )}
    </li>
  );
}

/**
 * The one section that matters: every item this event class is expected to
 * carry for its kind, with the actual value when present and an explicit
 * missing marker when not.
 */
function ExpectedItemsSection({ event }: { event: DataEvent }): JSX.Element {
  const quality = evaluateQuality(event);

  return (
    <Stack gap={2}>
      <Stack direction="horizontal" justify="space-between" align="center">
        <SectionTitle>
          {event.kind === "metric" ? "Expected measures" : "Expected fields"}
        </SectionTitle>
        <QualityPill quality={quality} />
      </Stack>
      {quality.grade === "unclassified" && (
        <Type muted small>
          No ingest rule recognized this event, so it cannot be attributed to a
          known producer class. It is kept visible instead of dropped.
        </Type>
      )}
      {quality.checks.length > 0 && (
        <ul className="divide-border border-border divide-y rounded-md border">
          {quality.checks.map((check) => (
            <ExpectedItemRow key={check.key} check={check} event={event} />
          ))}
        </ul>
      )}
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
                <SourceBadge event={event} />
              </Stack>
              <SheetDescription>
                {ORIGIN_LABELS[event.origin]} · {event.project} ·{" "}
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
              <ExpectedItemsSection event={event} />
              {event.kind === "log" && (
                <>
                  <Separator />
                  <LogBodyPanel event={event} />
                </>
              )}
              <Separator />
              <RawAttributes event={event} />
            </Stack>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
