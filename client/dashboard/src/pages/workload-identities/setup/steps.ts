import type { CatalogEntry, SetupDefinition, SetupStep } from "./definition";
import { tagsProblem } from "../tagLimits";
import { variableProblem, type VariableValues } from "./template";

export interface StepInputs {
  entry: CatalogEntry;
  values: VariableValues;
  agentId: string;
  tags: string[];
}

export function stepIndexById(
  definition: SetupDefinition,
  id: string | null,
): number {
  if (id === null) {
    return -1;
  }
  return definition.steps.findIndex((step) => step.id === id);
}

/** Whether a step has everything it asks for. */
export function stepComplete(step: SetupStep, inputs: StepInputs): boolean {
  return step.blocks.every((block) => {
    switch (block.type) {
      case "field": {
        const variable = inputs.entry.variables.find(
          (v) => v.key === block.variable,
        );
        return (
          variable !== undefined &&
          variableProblem(variable, inputs.values[variable.key] ?? "") === null
        );
      }
      case "agent_picker":
        return inputs.agentId !== "";
      case "tags":
        return tagsProblem(inputs.tags) === null;
      case "text":
      case "image":
      case "link":
      case "subject_rule":
      case "computed_status":
      case "computed":
        return true;
    }
  });
}

/**
 * Whether a step can be shown. Before the platform is trusted, steps after
 * create describe a connection that does not exist yet, and a step after an
 * incomplete one would act on missing values.
 */
export function stepReachable(
  definition: SetupDefinition,
  index: number,
  inputs: StepInputs,
  created: boolean,
): boolean {
  const target = definition.steps[index];
  if (target === undefined) {
    return false;
  }
  // Once the platform is trusted every step can be revisited: the earlier
  // ones describe what was set up, and nothing is written again unless asked.
  if (created) {
    return true;
  }
  if (target.phase === "connect") {
    return false;
  }
  return definition.steps
    .slice(0, index)
    .every((step) => step.phase !== "collect" || stepComplete(step, inputs));
}
