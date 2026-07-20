import { unsetLabel } from "@/components/observe/account-display-utils";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import {
  BadgeCheck,
  Bot,
  Building2,
  Cloud,
  Cpu,
  FolderKanban,
  Layers,
  type LucideIcon,
  Network,
  Shield,
  Sigma,
  UserRound,
} from "lucide-react";

// The token-usage panel's breakdown catalog: every group-by dimension plus the
// special token-type stacking, organized into scannable groups for the picker.
// Kept in a non-component module so the picker component file satisfies the
// react-refresh "only export components" rule.

// How the chart's bars stack: by the selected dimension's groups, by token
// type, or as a single un-broken-down total. Lives here (not in the panel
// component) so this module stays import-cycle-free.
export type StackMode = "group" | "tokenType" | "total";

// Sentinel values for the non-dimension modes. Dimension values are
// snake_case attribute keys, so these can't collide.
// Exported as the picker's default: the total view plots the billed per-day
// series, so the billing page opens on the number that matches the usage card.
export const BREAKDOWN_TOTAL = "total";
const BREAKDOWN_TOKEN_TYPE = "tokenType";

// The chart series palette, shared with the usage details table so a metric's
// dot color matches its chart legend color.
export const CHART_COLORS = [
  "#60a5fa", // blue
  "#34d399", // emerald
  "#f97316", // orange
  "#a78bfa", // violet
  "#fb7185", // rose
  "#facc15", // yellow
  "#38bdf8", // sky
  "#c084fc", // purple
  "#4ade80", // green
  "#f472b6", // pink
];
export const OTHER_COLOR = "#94a3b8"; // slate — the top-N remainder rollup

type BreakdownOption = {
  value: string;
  label: string;
  icon: LucideIcon;
};

export type BreakdownGroup = {
  heading: string;
  options: BreakdownOption[];
};

// The dimensions the observed agent traffic — the tokens-under-management
// population — genuinely carries (see the server's tumBreakdownDims): the
// session's model and agent surface, the AI account's provider and
// team/personal classification, and the user-identity snapshot hydrated at
// emit time. Gram-hosted surfaces (playground, risk-analysis inference) are
// not tokens under management and never appear here.
export const BREAKDOWN_GROUPS: BreakdownGroup[] = [
  {
    // Ungrouped: the no-breakdown view leads the list, above every category.
    heading: "",
    options: [{ value: BREAKDOWN_TOTAL, label: "Total", icon: Sigma }],
  },
  {
    heading: "Usage",
    options: [
      { value: Dimension.Model, label: "Model", icon: Cpu },
      { value: BREAKDOWN_TOKEN_TYPE, label: "Token type", icon: Layers },
    ],
  },
  {
    heading: "Agents",
    options: [
      // hook_source: the observed agent surface the session ran on
      // (claude-code, cursor, codex, …).
      { value: Dimension.HookSource, label: "Agent", icon: Bot },
      { value: Dimension.Provider, label: "Provider", icon: Cloud },
      {
        value: Dimension.AccountType,
        label: "Account type",
        icon: BadgeCheck,
      },
    ],
  },
  {
    heading: "Organization",
    options: [
      // project_id values are project UUIDs; the section maps them to names.
      { value: Dimension.ProjectId, label: "Project", icon: FolderKanban },
      { value: Dimension.DivisionName, label: "Division", icon: Network },
      {
        value: Dimension.DepartmentName,
        label: "Department",
        icon: Building2,
      },
    ],
  },
  {
    heading: "People",
    options: [
      { value: Dimension.Email, label: "User", icon: UserRound },
      { value: Dimension.Role, label: "Role", icon: Shield },
    ],
  },
];

export function stackModeFor(breakdown: string): StackMode {
  switch (breakdown) {
    case BREAKDOWN_TOTAL:
      return "total";
    case BREAKDOWN_TOKEN_TYPE:
      return "tokenType";
    default:
      return "group";
  }
}

export function breakdownLabel(value: string): string {
  for (const group of BREAKDOWN_GROUPS) {
    const hit = group.options.find((o) => o.value === value);
    if (hit) return hit.label;
  }
  return value;
}

// Display label for one breakdown row value: "" is observed traffic that
// lacks the attribute ("(unset)", or "Team-wide API Usage" on the user
// dimension — see unsetLabel), and project_id values are UUIDs mapped to
// project names (a deleted project falls back to its raw id).
export function breakdownValueLabel(
  dimension: string,
  value: string,
  projectNames: Map<string, string>,
): string {
  if (value === "") return unsetLabel(dimension as Dimension);
  if (dimension === Dimension.ProjectId) {
    return projectNames.get(value) ?? value;
  }
  return value;
}
