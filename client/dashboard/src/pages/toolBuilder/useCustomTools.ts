import { PromptTemplateKind } from "@gram/client/models/components";
import { useTemplates } from "@gram/client/react-query";

export function useCustomTools() {
  const { data, isLoading } = useTemplates();
  return {
    customTools: data?.templates.filter(
      (template) => template.kind === PromptTemplateKind.HigherOrderTool,
    ),
    isLoading,
  };
}
