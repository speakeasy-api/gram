import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAudience } from "@gram/client/models/components/pluginaudience.js";
import { Users } from "lucide-react";
import { PrincipalIconTile } from "./PrincipalIconTile";
import {
  INSTALL_MODES,
  installModeRows,
  MEMBERS_ROW_KEY,
  rowInstallMode,
  summarizeInstallModes,
  type InstallMode,
  type InstallModeRow,
} from "./install-modes";
import { describePrincipal, principalIcon } from "./principals";

function rowLabel(
  row: InstallModeRow,
  memberByUrn: Map<string, AccessMember>,
  audienceByUrn: Map<string, PluginAudience>,
): string {
  if (row.key === MEMBERS_ROW_KEY) {
    return `${row.principalUrns.length} members`;
  }
  return describePrincipal(row.key, new Map(), memberByUrn, audienceByUrn)
    .label;
}

function rowIcon(
  row: InstallModeRow,
  audienceByUrn: Map<string, PluginAudience>,
): ReturnType<typeof principalIcon> {
  if (row.key === MEMBERS_ROW_KEY) return Users;
  return principalIcon(
    describePrincipal(row.key, new Map(), new Map(), audienceByUrn).kind,
  );
}

// InstallModeList sets how the device agent installs the plugin for each
// selected audience. Individually assigned members share one control.
export function InstallModeList({
  principalUrns,
  modeByUrn,
  onModeChange,
  memberByUrn,
  audienceByUrn,
  disabled,
}: {
  principalUrns: string[];
  modeByUrn: Record<string, InstallMode>;
  onModeChange: (principalUrns: string[], mode: InstallMode) => void;
  memberByUrn: Map<string, AccessMember>;
  audienceByUrn: Map<string, PluginAudience>;
  disabled: boolean;
}): JSX.Element | null {
  if (principalUrns.length === 0) return null;

  const rows = installModeRows(principalUrns).map((row) => ({
    row,
    label: rowLabel(row, memberByUrn, audienceByUrn),
    icon: rowIcon(row, audienceByUrn),
    mode: rowInstallMode(row, modeByUrn),
  }));

  return (
    <div className="mt-6">
      <div className="mb-2 block text-sm font-medium">Install mode</div>
      <div className="border-border divide-border divide-y border px-4">
        {rows.map(({ row, label, icon, mode }) => (
          <div key={row.key} className="flex items-center gap-3 py-3">
            <PrincipalIconTile icon={icon} />
            <Text as="span" className="min-w-0 flex-1 truncate font-medium">
              {label}
            </Text>
            <Select
              value={mode ?? ""}
              onValueChange={(value) =>
                onModeChange(row.principalUrns, value as InstallMode)
              }
              disabled={disabled}
            >
              <SelectTrigger
                size="sm"
                className="w-[150px] shrink-0"
                aria-label={`Install mode for ${label}`}
              >
                <SelectValue placeholder="Mixed" />
              </SelectTrigger>
              <SelectContent>
                {INSTALL_MODES.map((option) => (
                  <SelectItem
                    key={option.value}
                    value={option.value}
                    description={option.description}
                  >
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ))}
      </div>
      <Text muted small className="mt-2">
        {summarizeInstallModes(rows)}. When someone is in more than one
        audience, the strictest mode applies.
      </Text>
    </div>
  );
}
