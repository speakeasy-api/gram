export type ActionCategory = "create" | "update" | "deploy" | "destructive";

export function getActionCategory(action: string): ActionCategory {
  const [resource, verb] = action.split(":");

  if (
    verb?.includes("delete") ||
    verb?.includes("disable") ||
    verb?.includes("detach") ||
    verb?.includes("revoke") ||
    verb?.includes("remove")
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

const colorConfigs = {
  create: {
    dot: "bg-success-default",
    text: "text-default-success",
    bg: "bg-success-softest",
  },
  update: {
    dot: "bg-warning-default",
    text: "text-default-warning",
    bg: "bg-warning-softest",
  },
  deploy: {
    dot: "bg-information-default",
    text: "text-default-information",
    bg: "bg-information-softest",
  },
  destructive: {
    dot: "bg-destructive-default",
    text: "text-default-destructive",
    bg: "bg-destructive-softest",
  },
} as const;

export type ActionColorConfig = (typeof colorConfigs)[ActionCategory];

export function getActionColorConfig(
  category: ActionCategory,
): ActionColorConfig {
  return colorConfigs[category];
}
