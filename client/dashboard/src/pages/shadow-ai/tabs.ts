// Shadow MCP is one part of Shadow AI, not a feature beside it: a shadow MCP
// server is reached by some AI tool, and the same admin answers "what are
// people running?" and "what is it talking to?" in one sitting.
//
// Four routed tabs rather than one page with a toggle, so a link to any of
// them survives being pasted into a ticket. The order is deliberate:
// harnesses and assistants are what people run and the only things blocking
// applies to; models are local runtimes that never speak MCP to Gram, so they
// are inventory only; MCP servers are what those tools reached.
const SHADOW_AI_SEGMENT = "shadow-ai";

export const TAB_VALUES = {
  harnesses: "harnesses",
  assistants: "assistants",
  models: "models",
  mcps: "mcps",
} as const;

export type ShadowAITab = (typeof TAB_VALUES)[keyof typeof TAB_VALUES];

export const shadowAIBreadcrumbSubstitutions: Record<string, string> = {
  [SHADOW_AI_SEGMENT]: "Shadow AI",
  [TAB_VALUES.harnesses]: "Harnesses",
  [TAB_VALUES.assistants]: "Assistants",
  [TAB_VALUES.models]: "Local Models",
  [TAB_VALUES.mcps]: "MCPs",
};
