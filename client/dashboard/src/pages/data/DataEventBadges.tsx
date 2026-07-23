import { SimpleTooltip } from "@/components/ui/tooltip";
import { Badge } from "@speakeasy-api/moonshine";
import {
  type DataEventKind,
  type DataEventOrigin,
  type EventQuality,
  ORIGIN_LABELS,
} from "./data-events";

export function KindBadge({ kind }: { kind: DataEventKind }): JSX.Element {
  return (
    <Badge size="sm" variant={kind === "metric" ? "information" : "neutral"}>
      <Badge.Text>{kind}</Badge.Text>
    </Badge>
  );
}

export function OriginBadge({
  origin,
}: {
  origin: DataEventOrigin;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip="Which channel observed this event">
      <Badge
        size="sm"
        variant={origin === "unknown" ? "destructive" : "neutral"}
        background={origin === "unknown"}
      >
        <Badge.Text>{ORIGIN_LABELS[origin]}</Badge.Text>
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
