import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import type { Column } from "@/components/ui/Table";
import { SignalBadges } from "@/components/shadow-ai/SignalBadges";
import { Text } from "@/components/ui/Text";

// The evidence one detection carries, whichever way the inventory is cut:
// per tool for one person on the identity page, per person for one tool on
// the Shadow AI tool page. Both tables read these columns off the same
// fields, so the two answers agree wherever they overlap.
export type DetectionEvidence = {
  deviceCount: number;
  signals: string[];
  versions: string[];
  firstSeen: Date;
  lastSeen: Date;
};

export function detectionEvidenceColumns<
  T extends DetectionEvidence,
>(): Column<T>[] {
  return [
    {
      key: "devices",
      header: "Devices",
      width: "0.55fr",
      render: (row) => (
        <Text small>
          {row.deviceCount} {row.deviceCount === 1 ? "device" : "devices"}
        </Text>
      ),
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
}
