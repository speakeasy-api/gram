import type { BadgeVariant } from "@/components/ui/lib/types";
import { PluginAssignmentInstallMode } from "@gram/client/models/components/pluginassignment.js";
import { isIndividualMemberPrincipal } from "./principals";

export type InstallMode = PluginAssignmentInstallMode;

export const DEFAULT_INSTALL_MODE: InstallMode =
  PluginAssignmentInstallMode.Default;

// Strictest first: when someone matches several assignments, the device
// agent applies the first of these that matches.
export const INSTALL_MODES: {
  value: InstallMode;
  label: string;
  description: string;
  badge: BadgeVariant;
}[] = [
  {
    value: PluginAssignmentInstallMode.Required,
    label: "Required",
    description: "Installed. Users can't turn it off.",
    badge: "warning",
  },
  {
    value: PluginAssignmentInstallMode.Default,
    label: "On by default",
    description: "Installed. Users can turn it off.",
    badge: "information",
  },
  {
    value: PluginAssignmentInstallMode.Available,
    label: "Available",
    description: "Not installed until a user turns it on.",
    badge: "neutral",
  },
];

export function installModeOption(
  mode: InstallMode,
): (typeof INSTALL_MODES)[number] {
  return INSTALL_MODES.find((option) => option.value === mode)!;
}

// A row in the install mode list: one audience, or every individually
// assigned member when there is more than one, so a long roster shares a
// single control.
export type InstallModeRow = {
  key: string;
  principalUrns: string[];
};

export const MEMBERS_ROW_KEY = "members";

export function installModeRows(principalUrns: string[]): InstallModeRow[] {
  const members = principalUrns.filter(isIndividualMemberPrincipal);
  const rows: InstallModeRow[] = principalUrns
    .filter((urn) => members.length < 2 || !isIndividualMemberPrincipal(urn))
    .map((urn) => ({ key: urn, principalUrns: [urn] }));
  if (members.length >= 2) {
    rows.push({ key: MEMBERS_ROW_KEY, principalUrns: members });
  }
  return rows;
}

// The row's mode, or undefined when its members don't all share one.
export function rowInstallMode(
  row: InstallModeRow,
  modeByUrn: Record<string, InstallMode>,
): InstallMode | undefined {
  const modes = new Set(
    row.principalUrns.map((urn) => modeByUrn[urn] ?? DEFAULT_INSTALL_MODE),
  );
  return modes.size === 1 ? [...modes][0] : undefined;
}

// "Required: SRE · On by default: 12 members · Available: Everyone"
export function summarizeInstallModes(
  rows: { label: string; mode: InstallMode | undefined }[],
): string {
  return INSTALL_MODES.map((option) => {
    const labels = rows
      .filter((row) => row.mode === option.value)
      .map((row) => row.label);
    return labels.length > 0 ? `${option.label}: ${labels.join(", ")}` : null;
  })
    .filter((part) => part !== null)
    .join(" · ");
}
