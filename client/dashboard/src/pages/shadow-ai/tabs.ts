// The Shadow AI section's index pages redirect to Guardrails, but the tool
// and server detail pages under them still render, and their breadcrumbs
// still walk through the old URL segments. These name those segments.
export const shadowAIBreadcrumbSubstitutions: Record<string, string> = {
  "shadow-ai": "Shadow AI",
  harnesses: "Harnesses",
  assistants: "Assistants",
  models: "Local Models",
  mcps: "MCPs",
};
