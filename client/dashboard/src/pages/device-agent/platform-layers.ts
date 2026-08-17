export type EnforcementLayer = "off" | "user" | "managed";

// The `platforms` map is opt-out on the device agent: a tool absent from it is
// managed at the user layer (config.PlatformMode.Layer in the device-agent
// repo). Only an explicit `false` disables one. This UI must resolve an absent
// key the same way, or it reports a tool as Off while the fleet is enforcing
// it — and then writes that false reading back on the next save.
const DEFAULT_LAYER: EnforcementLayer = "user";

export function recordValue(value: unknown): Record<string, unknown> {
  if (typeof value === "object" && value !== null && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return {};
}

export function enforcementLayer(
  platforms: unknown,
  platformKey: string,
): EnforcementLayer {
  const value = recordValue(platforms)[platformKey];
  if (value === "user" || value === "managed") return value;
  if (value === false) return "off";
  return DEFAULT_LAYER;
}
