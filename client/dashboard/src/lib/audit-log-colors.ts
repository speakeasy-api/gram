export type ActionCategory = "create" | "update" | "deploy" | "destructive";

export function getActionCategory(action: string): ActionCategory {
  const [resource, verb] = action.split(":");

  if (
    verb?.includes("delete") ||
    verb?.includes("detach") ||
    verb?.includes("revoke") ||
    verb?.includes("remove")
  ) {
    return "destructive";
  }

  if (resource === "deployments") {
    return "deploy";
  }

  if (verb?.includes("create") || verb?.includes("upload")) {
    return "create";
  }

  return "update";
}

const colorConfigs = {
  create: {
    dot: "bg-emerald-500",
    text: "text-emerald-700",
    bg: "bg-emerald-50",
  },
  update: {
    dot: "bg-yellow-500",
    text: "text-yellow-700",
    bg: "bg-yellow-50",
  },
  deploy: {
    dot: "bg-blue-500",
    text: "text-blue-700",
    bg: "bg-blue-50",
  },
  destructive: {
    dot: "bg-red-500",
    text: "text-red-700",
    bg: "bg-red-50",
  },
} as const;

export type ActionColorConfig = (typeof colorConfigs)[ActionCategory];

export function getActionColorConfig(
  category: ActionCategory,
): ActionColorConfig {
  return colorConfigs[category];
}
