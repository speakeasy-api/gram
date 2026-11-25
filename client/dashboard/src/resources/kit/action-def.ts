import type { ActionDef, ResourceDefinition } from "./types";

/** Looks up an action by id; a missing id is a definition bug. */
export function actionDef<Row, Rec>(
  definition: ResourceDefinition<Row, Rec>,
  id: string,
): ActionDef {
  const def = definition.actions.find((a) => a.id === id);
  if (!def) {
    throw new Error(`Resource "${definition.id}" has no action "${id}"`);
  }
  return def;
}
