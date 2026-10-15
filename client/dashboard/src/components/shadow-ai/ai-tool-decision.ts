/** What a decision control shows for one tool. `inventory_only` is a tool
 *  with no gateway identity (every local model, and any tool with no client
 *  metadata document): nothing could enforce a decision, so none is offered. */
export type AIToolDecision =
  | "approved"
  | "blocked"
  | "unreviewed"
  | "inventory_only";

/** The decision to record when `clicked` is pressed while `current` is shown.
 *  Pressing the option already in force clears it back to unreviewed, which
 *  is how a decision is withdrawn without a third option in the control. */
export function nextAIToolDecision(
  current: AIToolDecision,
  clicked: "approved" | "blocked",
): "approved" | "blocked" | "unreviewed" {
  return current === clicked ? "unreviewed" : clicked;
}
