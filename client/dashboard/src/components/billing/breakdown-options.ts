import { Dimension } from "@gram/client/models/components/queryfilter.js";
import {
  Bot,
  Cpu,
  Layers,
  type LucideIcon,
  Network,
  Shield,
  ShieldAlert,
  Sigma,
  UserRound,
} from "lucide-react";

// The token-usage panel's breakdown catalog: every group-by dimension plus the
// two special stackings (token type, risk), organized into scannable groups
// for the picker. Kept in a non-component module so the picker component file
// satisfies the react-refresh "only export components" rule.

// How the chart's bars stack: by the selected dimension's groups, by token
// type, by risk involvement, or as a single un-broken-down total. Lives here
// (not in the panel component) so this module stays import-cycle-free.
export type StackMode = "group" | "tokenType" | "risk" | "total";

// Sentinel values for the non-dimension modes. Dimension values are
// snake_case attribute keys, so these can't collide.
// Exported as the picker's default: the total view plots the billed per-day
// series, so the billing page opens on the number that matches the usage card.
export const BREAKDOWN_TOTAL = "total";
const BREAKDOWN_TOKEN_TYPE = "tokenType";
export const BREAKDOWN_RISK = "risk";

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
export const RISKY_COLOR = "#fb7185"; // rose — tokens from sessions with risk findings
export const CLEAN_COLOR = "#60a5fa"; // blue — everything else

type BreakdownOption = {
  value: string;
  label: string;
  icon: LucideIcon;
};

export type BreakdownGroup = {
  heading: string;
  options: BreakdownOption[];
};

// Only dimensions billed completion rows genuinely carry: the model, the
// user identity snapshot hydrated at emit time (user, division, roles), and
// the consuming surface. Fleet-only concepts (provider, account type, skill,
// MCP server/tool, cache token types) live on the costs/insights pages,
// whose analytics aggregate is scoped to agent-fleet provenance.
export const BREAKDOWN_GROUPS: BreakdownGroup[] = [
  {
    // Ungrouped: the no-breakdown view leads the list, above every category.
    heading: "",
    options: [{ value: BREAKDOWN_TOTAL, label: "Total", icon: Sigma }],
  },
  {
    heading: "Model",
    options: [{ value: Dimension.Model, label: "Model", icon: Cpu }],
  },
  {
    heading: "Usage",
    options: [
      { value: BREAKDOWN_TOKEN_TYPE, label: "Token type", icon: Layers },
      { value: BREAKDOWN_RISK, label: "Risk findings", icon: ShieldAlert },
    ],
  },
  {
    heading: "Organization",
    options: [
      { value: Dimension.DivisionName, label: "Division", icon: Network },
    ],
  },
  {
    heading: "People",
    options: [
      { value: Dimension.Email, label: "User", icon: UserRound },
      { value: Dimension.Role, label: "Role", icon: Shield },
    ],
  },
  {
    heading: "Surfaces",
    options: [
      // hook_source: for billed completions this is the Gram surface the
      // request ran through (playground, MCP chat, …).
      { value: Dimension.HookSource, label: "Source", icon: Bot },
    ],
  },
];

export function stackModeFor(breakdown: string): StackMode {
  switch (breakdown) {
    case BREAKDOWN_TOTAL:
      return "total";
    case BREAKDOWN_TOKEN_TYPE:
      return "tokenType";
    case BREAKDOWN_RISK:
      return "risk";
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
