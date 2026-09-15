export function policyEnabledActionLabel(
  enabled: boolean,
): "Disable" | "Enable" {
  return enabled ? "Disable" : "Enable";
}

export function policyStatusLabel(enabled: boolean): "Enforcing" | "Inactive" {
  return enabled ? "Enforcing" : "Inactive";
}
