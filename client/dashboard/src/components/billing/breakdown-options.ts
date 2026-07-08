import { Dimension } from "@gram/client/models/components/queryfilter.js";
import {
  Bot,
  CircleUser,
  Cloud,
  Cpu,
  FolderOpen,
  Layers,
  type LucideIcon,
  Network,
  Server,
  Shield,
  ShieldAlert,
  Sigma,
  Sparkles,
  UserRound,
  Wrench,
} from "lucide-react";
import { type StackMode } from "./token-usage-panel";

// The token-usage panel's breakdown catalog: every group-by dimension plus the
// two special stackings (token type, risk), organized into scannable groups
// for the picker. Kept in a non-component module so the picker component file
// satisfies the react-refresh "only export components" rule.

// Sentinel values for the non-dimension modes. Dimension values are
// snake_case attribute keys, so these can't collide.
export const BREAKDOWN_TOTAL = "total";
export const BREAKDOWN_TOKEN_TYPE = "tokenType";
export const BREAKDOWN_RISK = "risk";

export type BreakdownOption = {
  value: string;
  label: string;
  icon: LucideIcon;
};

export type BreakdownGroup = {
  heading: string;
  options: BreakdownOption[];
};

export const BREAKDOWN_GROUPS: BreakdownGroup[] = [
  {
    // Ungrouped: the no-breakdown view leads the list, above every category.
    heading: "",
    options: [{ value: BREAKDOWN_TOTAL, label: "Total", icon: Sigma }],
  },
  {
    heading: "Model & account",
    options: [
      { value: Dimension.Model, label: "Model", icon: Cpu },
      { value: Dimension.Provider, label: "Provider", icon: Cloud },
      {
        value: Dimension.AccountType,
        label: "Account type",
        icon: CircleUser,
      },
    ],
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
      { value: Dimension.ProjectId, label: "Project", icon: FolderOpen },
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
    heading: "Agents & tools",
    options: [
      { value: Dimension.HookSource, label: "Agent", icon: Bot },
      { value: Dimension.SkillName, label: "Skill", icon: Sparkles },
      { value: Dimension.McpServerName, label: "MCP server", icon: Server },
      { value: Dimension.McpToolName, label: "MCP tool", icon: Wrench },
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
