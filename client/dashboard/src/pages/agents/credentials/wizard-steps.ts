/**
 * The agent wizard's steps, in order. The id is what `?step=` carries, so a
 * link names a step rather than its position: reordering the steps keeps old
 * links on the step they meant. The server links to these steps too
 * (src/server-links.json), so renaming an id breaks a test, not a link.
 */
export const WIZARD_STEPS = [
  { id: "name", label: "Name", short: "Name" },
  { id: "servers", label: "MCP Selection", short: "MCPs" },
  { id: "mcp-sign-in", label: "MCP sign-in", short: "MCP sign-in" },
  { id: "agent-sign-in", label: "Agent sign-in", short: "Agent sign-in" },
  { id: "install", label: "Install", short: "Install" },
  { id: "verify", label: "Verify", short: "Verify" },
] as const;

export type WizardStepId = (typeof WIZARD_STEPS)[number]["id"];

export const WIZARD_STEP_IDS: readonly string[] = WIZARD_STEPS.map(
  (step) => step.id,
);

/**
 * The step index a `?step=` value opens. A step id is read first; an index
 * is still read so links made before the ids keep working. Anything else,
 * or no value, opens the default step. An index past the last step is
 * returned as is: how many steps there are is only known once a platform's
 * own setup steps are counted, and the wizard corrects it then.
 */
export function wizardStepIndex(
  raw: string | null,
  defaultStep: number,
): number {
  if (raw === null || raw.trim() === "") return defaultStep;
  const byId = WIZARD_STEP_IDS.indexOf(raw);
  if (byId >= 0) return byId;
  const index = Number(raw);
  if (Number.isInteger(index) && index >= 0) return index;
  return defaultStep;
}

/**
 * The `?step=` value for a step index. A platform's own setup steps follow
 * the fixed ones and have no id, so they keep their index.
 */
export function wizardStepParam(index: number): string {
  return WIZARD_STEPS[index]?.id ?? String(index);
}
