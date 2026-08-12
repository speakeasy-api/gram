export type ActionCategory = "create" | "update" | "deploy" | "destructive";

export function getActionCategory(action: string): ActionCategory {
  const [resource, verb] = action.split(":");

  if (
    verb?.includes("delete") ||
    verb?.includes("disable") ||
    verb?.includes("detach") ||
    verb?.includes("revoke") ||
    verb?.includes("remove") ||
    verb?.includes("demot")
  ) {
    return "destructive";
  }

  if (resource === "deployments") {
    return "deploy";
  }

  if (
    verb?.includes("create") ||
    verb?.includes("upload") ||
    verb?.includes("enable")
  ) {
    return "create";
  }

  return "update";
}

// Editorial treatment: semantic color lives in the dot and the mono action
// text only — chips are neutral hairline boxes, no tinted washes.
const colorConfigs = {
  create: {
    dot: "bg-success-default",
    text: "text-default-success",
    bg: "border-border bg-card border",
  },
  update: {
    dot: "bg-warning-default",
    text: "text-default-warning",
    bg: "border-border bg-card border",
  },
  deploy: {
    dot: "bg-information-default",
    text: "text-default-information",
    bg: "border-border bg-card border",
  },
  destructive: {
    dot: "bg-destructive-default",
    text: "text-default-destructive",
    bg: "border-border bg-card border",
  },
} as const;

export type ActionColorConfig = (typeof colorConfigs)[ActionCategory];

export function getActionColorConfig(
  category: ActionCategory,
): ActionColorConfig {
  return colorConfigs[category];
}
