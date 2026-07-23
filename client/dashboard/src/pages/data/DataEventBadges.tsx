import { SimpleTooltip } from "@/components/ui/tooltip";
import { Badge } from "@speakeasy-api/moonshine";
import { Activity, ChartLine } from "lucide-react";
import {
  type DataEvent,
  type DataEventKind,
  type EventQuality,
  ORIGIN_LABELS,
} from "./data-events";

/**
 * Kind badge with the same icon treatment as the AI Integrations streams
 * table: Activity for event/log streams, ChartLine for metrics.
 */
export function KindBadge({ kind }: { kind: DataEventKind }): JSX.Element {
  const KindIcon = kind === "metric" ? ChartLine : Activity;
  return (
    <Badge size="sm" variant="neutral" background className="shrink-0">
      <Badge.LeftIcon>
        <KindIcon className="h-3 w-3" />
      </Badge.LeftIcon>
      <Badge.Text>{kind === "metric" ? "Metric" : "Log"}</Badge.Text>
    </Badge>
  );
}

/**
 * The actual producer name (claude-code, codex, gram-server, ...) instead of
 * a generic channel label — the event should be understandable at a glance.
 * The observation channel moves to the tooltip.
 */
export function SourceBadge({ event }: { event: DataEvent }): JSX.Element {
  const isUnknown = event.producer === "unknown";
  return (
    <SimpleTooltip tooltip={`Observed via ${ORIGIN_LABELS[event.origin]}`}>
      <Badge
        size="sm"
        variant={isUnknown ? "destructive" : "neutral"}
        background={isUnknown}
      >
        <Badge.Text>{event.producer}</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

function qualityTooltip(quality: EventQuality): string {
  if (quality.grade === "unclassified") {
    return "No ingest rule could classify this event";
  }
  if (quality.missing.length === 0) {
    return "All expected attributes are present";
  }
  const missing = quality.missing.map((check) => check.label).join(", ");
  return `Missing: ${missing}`;
}

function qualityVariant(
  quality: EventQuality,
): "success" | "warning" | "destructive" {
  switch (quality.grade) {
    case "complete":
      return "success";
    case "partial":
      return "warning";
    case "unclassified":
      return "destructive";
  }
}

function qualityLabel(quality: EventQuality): string {
  const presentCount = quality.checks.length - quality.missing.length;

  switch (quality.grade) {
    case "complete":
      return "Complete";
    case "partial":
      return `${presentCount}/${quality.checks.length} attrs`;
    case "unclassified":
      return "Unclassified";
  }
}

export function QualityPill({
  quality,
}: {
  quality: EventQuality;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={qualityTooltip(quality)}>
      <Badge size="sm" variant={qualityVariant(quality)} background>
        <Badge.Text>{qualityLabel(quality)}</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}
