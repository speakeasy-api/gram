import {
  Bot,
  Building2,
  Cloud,
  Cpu,
  FolderKanban,
  Gauge,
  Network,
  Route,
  ScanSearch,
  Server,
  Shield,
  Sigma,
  Tags,
  UserRound,
  UsersRound,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import {
  type Family,
  type ReadingKind,
} from "@gram/client/models/components/meterusageresponse.js";

export type MeterFamily = Family;
export type MeterReadingKind = ReadingKind;

export type MeterFamilyDefinition = {
  label: string;
  description: string;
  defaultBreakdown: string;
  groups: { heading: string; options: MeterBreakdownOption[] }[];
};

export type MeterBreakdownOption = {
  value: string;
  label: string;
  icon: LucideIcon;
};

const TOTAL: MeterBreakdownOption = {
  value: "total",
  label: "Total",
  icon: Sigma,
};
const PROJECT: MeterBreakdownOption = {
  value: "project",
  label: "Project",
  icon: FolderKanban,
};

export const METER_FAMILIES: Record<MeterFamily, MeterFamilyDefinition> = {
  agent_session_storage: {
    label: "Tokens under management",
    description:
      "Canonical stored-message workload measured in stored tokens (s-tokens), not provider input or output tokens.",
    defaultBreakdown: "total",
    groups: [
      { heading: "", options: [TOTAL] },
      {
        heading: "Workload",
        options: [
          PROJECT,
          { value: "model", label: "Model", icon: Cpu },
          { value: "provider", label: "Provider", icon: Cloud },
          { value: "billing_mode", label: "Billing mode", icon: Gauge },
          { value: "assistant", label: "Assistant", icon: Bot },
        ],
      },
      {
        heading: "People and organization",
        options: [
          { value: "billing_user", label: "Billing user", icon: UserRound },
          { value: "division", label: "Division", icon: Network },
          { value: "department", label: "Department", icon: Building2 },
          { value: "job_title", label: "Job title", icon: Tags },
          { value: "employee_type", label: "Employee type", icon: UsersRound },
          { value: "cost_center", label: "Cost center", icon: Building2 },
          {
            value: "directory_group_set",
            label: "Directory group set",
            icon: UsersRound,
          },
        ],
      },
    ],
  },
  mcp_bandwidth: {
    label: "MCP bandwidth",
    description:
      "Application-visible request and response body bytes, excluding headers, framing, and upstream fanout.",
    defaultBreakdown: "direction",
    groups: [
      {
        heading: "Traffic",
        options: [
          TOTAL,
          { value: "direction", label: "Direction", icon: Route },
          PROJECT,
          { value: "mcp_server", label: "MCP server", icon: Server },
          { value: "server_type", label: "Server type", icon: Server },
        ],
      },
    ],
  },
  risk_content_scans: {
    label: "Risk content scans",
    description:
      "Content volume scanned by the six risk scanners, measured in s-tokens. Content scanned by multiple scanners contributes to each.",
    defaultBreakdown: "scanner",
    groups: [
      {
        heading: "Scan",
        options: [
          TOTAL,
          { value: "scanner", label: "Scanner", icon: ScanSearch },
          PROJECT,
          { value: "policy", label: "Policy", icon: Shield },
        ],
      },
      {
        heading: "Judge and tool",
        options: [
          { value: "judge_model", label: "Judge model", icon: Cpu },
          { value: "judge_provider", label: "Judge provider", icon: Cloud },
          { value: "tool_name", label: "Tool name", icon: Wrench },
        ],
      },
    ],
  },
};

export function meterBreakdownLabel(
  family: MeterFamily,
  value: string,
): string {
  for (const group of METER_FAMILIES[family].groups) {
    const option = group.options.find((candidate) => candidate.value === value);
    if (option) return option.label;
  }
  return value;
}
