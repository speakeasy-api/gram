import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import type { Column } from "@/components/ui/Table";
import { SignalBadges } from "@/components/shadow-ai/SignalBadges";
import { Text } from "@/components/ui/Text";
import { pluralize } from "@/lib/format";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";

// The evidence one detection carries, whichever way the inventory is cut:
// per tool for one person on the identity page, per person for one tool on
// the Shadow AI tool page. Both tables read these columns off the same
// fields, so the two answers agree wherever they overlap. A column only
// reads its row, so these spread into a table of any row type that carries
// the fields, AIDetection and AIDetectionUser alike.
export type DetectionEvidence = Pick<
  AIDetection,
  "deviceCount" | "signals" | "versions" | "firstSeen" | "lastSeen"
>;

export const DETECTION_EVIDENCE_COLUMNS: Column<DetectionEvidence>[] = [
  {
    key: "devices",
    header: "Devices",
    width: "0.55fr",
    render: (row) => <Text small>{pluralize(row.deviceCount, "device")}</Text>,
  },
  {
    key: "signals",
    header: "Signals",
    width: "0.9fr",
    render: (row) => <SignalBadges signals={row.signals} />,
  },
  {
    key: "versions",
    header: "Versions",
    width: "1fr",
    render: (row) => (
      <Text small mono className="truncate" title={row.versions.join(", ")}>
        {row.versions.length > 0 ? row.versions.join(" · ") : "—"}
      </Text>
    ),
  },
  {
    key: "firstSeen",
    header: "First seen",
    width: "0.85fr",
    render: (row) => (
      <Text small className="whitespace-nowrap">
        {formatShortDate(row.firstSeen)}
      </Text>
    ),
  },
  {
    key: "lastSeen",
    header: "Last seen",
    width: "0.85fr",
    render: (row) => (
      <Text small className="whitespace-nowrap">
        {formatShortDate(row.lastSeen)}
      </Text>
    ),
  },
];
