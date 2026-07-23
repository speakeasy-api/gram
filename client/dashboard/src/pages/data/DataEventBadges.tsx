import { Badge } from "@/components/ui/badge";
import {
  type DataEventKind,
  type DataEventOrigin,
  type EventQuality,
  ORIGIN_LABELS,
} from "./data-events";

export function KindBadge({ kind }: { kind: DataEventKind }): JSX.Element {
  return (
    <Badge
      variant={kind === "metric" ? "secondary" : "outline"}
      size="sm"
      className="font-mono uppercase"
    >
      {kind}
    </Badge>
  );
}

export function OriginBadge({
  origin,
}: {
  origin: DataEventOrigin;
}): JSX.Element {
  return (
    <Badge
      variant={origin === "unknown" ? "destructive" : "outline"}
      size="sm"
      tooltip="Which channel observed this event"
    >
      {ORIGIN_LABELS[origin]}
    </Badge>
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

export function QualityPill({
  quality,
}: {
  quality: EventQuality;
}): JSX.Element {
  const presentCount = quality.checks.length - quality.missing.length;

  switch (quality.grade) {
    case "complete":
      return (
        <Badge variant="outline" size="sm" tooltip={qualityTooltip(quality)}>
          Complete
        </Badge>
      );
    case "partial":
      return (
        <Badge variant="warning" size="sm" tooltip={qualityTooltip(quality)}>
          {presentCount}/{quality.checks.length} attrs
        </Badge>
      );
    case "unclassified":
      return (
        <Badge
          variant="destructive"
          size="sm"
          tooltip={qualityTooltip(quality)}
        >
          Unclassified
        </Badge>
      );
  }
}
