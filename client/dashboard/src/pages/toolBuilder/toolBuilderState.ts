import { v7 as uuidv7 } from "uuid";

export type Input = {
  name: string;
  description?: string;
};

export type SerializableStep = {
  id: string;
  tool?: string;
  canonicalTool?: string;
  toolUrn?: string;
  instructions: string;
  inputs?: string[];
};

export type Step = SerializableStep & {
  update: (step: Step) => void;
};

// Needs to stay aligned with server/internal/templates/impl.go:CustomToolJSONV1
export type CustomTool = {
  toolName: string;
  purpose: string;
  inputs: Input[];
  steps: SerializableStep[];
};

type ParsedCustomTool = Omit<CustomTool, "steps"> & {
  steps: Array<Omit<SerializableStep, "id"> & { id?: string }>;
};

export function parsePrompt(prompt: string): {
  purpose: string;
  inputs: Input[];
  steps: SerializableStep[];
} {
  const customTool = JSON.parse(prompt) as ParsedCustomTool;

  const steps: SerializableStep[] = customTool.steps.map((step) => ({
    ...step,
    id: step.id || uuidv7(),
  }));

  return {
    purpose: customTool.purpose,
    inputs: customTool.inputs,
    steps,
  };
}
