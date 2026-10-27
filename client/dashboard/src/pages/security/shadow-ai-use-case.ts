import type { RiskPreset } from "@gram/client/models/components/riskpreset.js";

/** The server's "Unapproved MCP servers" use case, recognised by what it
 *  turns on rather than by its id, the way `isShadowAIPolicy` recognises the
 *  policy it creates. */
export function isShadowAIUseCase(
  preset: Pick<RiskPreset, "policyType" | "sources">,
): boolean {
  return (
    preset.policyType === "standard" &&
    preset.sources.length === 1 &&
    preset.sources[0] === "shadow_mcp"
  );
}

/** That use case is the Shadow AI policy: it
 *  opens the Shadow AI editor, which pins the action to deny, so the row says
 *  so instead of the server's flag-only wording. Everything else about the
 *  preset is left alone. */
export function presentShadowAIUseCase(preset: RiskPreset): RiskPreset {
  if (!isShadowAIUseCase(preset)) return preset;
  return {
    ...preset,
    label: "Shadow AI",
    description:
      "Detect unsanctioned AI tools and MCP servers across your devices and agent traffic, and decide which may connect. Unreviewed tools connect until you deny them.",
    action: "block",
  };
}
