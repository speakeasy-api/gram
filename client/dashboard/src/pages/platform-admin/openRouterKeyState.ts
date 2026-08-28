const DISABLE_CAUSE_LABELS = {
  admin_lock: "Admin lock",
  trial_demotion: "Trial demotion",
  billing_inactive: "Billing inactive",
} as const;

type OpenRouterKeyState = {
  disabled: boolean;
  disableCauses?: readonly string[] | null;
};

export function causeLabels(
  causes: readonly string[] | null | undefined,
): string[] {
  return (causes ?? []).map(
    (cause) =>
      DISABLE_CAUSE_LABELS[cause as keyof typeof DISABLE_CAUSE_LABELS] ?? cause,
  );
}

export function effectiveDisabled(key: OpenRouterKeyState): boolean {
  return key.disableCauses == null
    ? key.disabled
    : key.disableCauses.length > 0;
}

export function keyAction(
  causes: readonly string[] | null | undefined,
): "disable" | "remove-admin-lock" | null {
  if (causes == null) return null;
  return causes.includes("admin_lock") ? "remove-admin-lock" : "disable";
}
