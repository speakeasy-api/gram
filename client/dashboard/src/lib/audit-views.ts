/**
 * The audit log page's views and the API query each one maps to.
 */
export type AuditView = "changes" | "system" | "security" | "all";

export const VIEW_OPTIONS: {
  value: AuditView;
  label: string;
  tooltip: string;
}[] = [
  {
    value: "changes",
    label: "Team changes",
    tooltip:
      "Changes people, API keys and agents made to settings, without Speakeasy's own background activity",
  },
  {
    value: "system",
    label: "Automated changes",
    tooltip:
      "Changes Speakeasy made on its own, such as publishing plugins and creating the keys they use",
  },
  {
    value: "security",
    label: "Access",
    tooltip:
      "Machine access, such as an agent's runtime admitting its workload identity",
  },
  {
    value: "all",
    label: "All changes",
    tooltip: "Every configuration change, from your team and automated",
  },
];

export const VIEW_EMPTY: Record<
  AuditView,
  { title: string; description: string }
> = {
  changes: {
    title: "No audit logs found",
    description:
      "Activity will appear here as changes are made across your organization.",
  },
  system: {
    title: "No automated changes found",
    description:
      "Changes Speakeasy makes on its own appear here, such as publishing plugins.",
  },
  security: {
    title: "No access activity found",
    description:
      "Machine access appears here, such as an agent's runtime admitting its workload identity.",
  },
  all: {
    title: "No audit logs found",
    description:
      "Activity will appear here as changes are made across your organization.",
  },
};

export const VIEW_INSIGHTS: Record<AuditView, string> = {
  changes:
    "Showing configuration changes made by people, API keys and agents. Speakeasy's own background activity is in the Automated changes view.",
  system:
    "Showing automated changes: changes Speakeasy made on its own, such as publishing plugins.",
  security:
    "Showing access activity: machine access such as agents' runtimes admitting their workload identity.",
  all: "Showing all configuration changes, from the team and automated.",
};

export function parseAuditView(value: string): AuditView {
  switch (value) {
    case "system":
    case "security":
    case "all":
      return value;
    default:
      // Covers "changes" and the older "configuration" links.
      return "changes";
  }
}

export function auditViewQuery(view: AuditView): {
  category: "configuration" | "security";
  origin?: "user" | "system";
} {
  switch (view) {
    case "security":
      return { category: "security" };
    case "system":
      return { category: "configuration", origin: "system" };
    case "all":
      return { category: "configuration" };
    case "changes":
      return { category: "configuration", origin: "user" };
  }
}

export function resourceFilterValue(log: {
  subjectType: string;
  subjectId: string;
}): string {
  return `${log.subjectType}:${log.subjectId}`;
}

export function parseResourceFilter(
  value: string | null,
): { subjectType: string; subjectId: string } | null {
  if (!value) return null;
  const sep = value.indexOf(":");
  if (sep <= 0 || sep === value.length - 1) return null;
  return { subjectType: value.slice(0, sep), subjectId: value.slice(sep + 1) };
}
