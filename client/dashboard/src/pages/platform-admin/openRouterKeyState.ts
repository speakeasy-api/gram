export const DISABLE_CAUSE_LABELS = {
  admin_lock: "Admin lock",
  trial_demotion: "Trial demotion",
  billing_inactive: "Billing inactive",
} as const;

export function causeLabels(causes: readonly string[]): string[] {
  return causes.map(
    (cause) =>
      DISABLE_CAUSE_LABELS[cause as keyof typeof DISABLE_CAUSE_LABELS] ?? cause,
  );
}

export function keyAction(
  causes: readonly string[],
): "disable" | "remove-admin-lock" {
  return causes.includes("admin_lock") ? "remove-admin-lock" : "disable";
}
